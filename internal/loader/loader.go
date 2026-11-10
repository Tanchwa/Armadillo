/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package loader is the per-node half of Armadillo. It loads the profiles the
// manager published onto its own node and reports what it loaded. It applies;
// it never decides.
//
// Do not put +kubebuilder:rbac markers in this package: controller-gen merges
// every marker under ./... into the manager's role. The loader's RBAC is
// hand-written in config/loader/rbac.yaml.
package loader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	armadillov1alpha1 "github.com/tanchwa/armadillo/api/v1alpha1"
	"github.com/tanchwa/armadillo/internal/apparmor"
)

// ConditionLoaded is the ProfileNodeStatus condition that says whether the
// published profile is loaded on the node.
const ConditionLoaded = "Loaded"

// Parser loads profile files into the kernel and removes them again.
type Parser interface {
	Load(ctx context.Context, path string) error
	Remove(ctx context.Context, path string) error
}

// ExecParser runs apparmor_parser.
type ExecParser struct {
	// SecurityFSDir is the AppArmor securityfs directory. It is passed
	// explicitly because libapparmor cannot find it when only that directory is
	// mounted into the container.
	SecurityFSDir string
}

func (p ExecParser) run(ctx context.Context, action, path string) error {
	// --skip-cache: the root filesystem is read-only and the cache is not needed.
	out, err := exec.CommandContext(ctx, "apparmor_parser",
		action, "--skip-cache", "--subdomainfs", p.SecurityFSDir, path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("apparmor_parser %s: %w: %s", action, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Load loads the profile file, replacing a loaded profile of the same name.
func (p ExecParser) Load(ctx context.Context, path string) error {
	return p.run(ctx, "--replace", path)
}

// Remove unloads the profile the file defines.
func (p ExecParser) Remove(ctx context.Context, path string) error {
	return p.run(ctx, "--remove", path)
}

type loaded struct {
	hash string
	mode apparmor.Mode
}

// State is what this loader has loaded since it started. The startup gate
// asks it whether a pod's profile is on the node yet.
type State struct {
	mu       sync.RWMutex
	profiles map[string]loaded
}

// NewState returns an empty State.
func NewState() *State {
	return &State{profiles: map[string]loaded{}}
}

func (s *State) get(name string) (loaded, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.profiles[name]
	return l, ok
}

func (s *State) set(name string, l loaded) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles[name] = l
}

func (s *State) delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.profiles, name)
}

// Handler serves GET /v1/loaded/<profile name>: 200 when the profile is loaded
// on this node, 404 when it is not. It reveals nothing else.
func (s *State) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/loaded/{name}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.get(r.PathValue("name")); !ok {
			http.Error(w, "not loaded", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

// StatusName is the name of the ProfileNodeStatus for a profile on a node.
func StatusName(node, namespace, name string) string {
	full := node + "." + namespace + "." + name
	if len(full) <= 253 {
		return full
	}
	sum := sha256.Sum256([]byte(full))
	return full[:236] + "-" + hex.EncodeToString(sum[:8])
}

// Reconciler loads every published ManagedProfile onto this node.
type Reconciler struct {
	client.Client
	NodeName   string
	ProfileDir string
	Parser     Parser
	State      *State
}

// Reconcile brings this node in line with one ManagedProfile.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	name := apparmor.ProfileName(req.Namespace, req.Name)
	path := filepath.Join(r.ProfileDir, name)

	var mp armadillov1alpha1.ManagedProfile
	if err := r.Get(ctx, req.NamespacedName, &mp); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, r.unload(ctx, req, name, path)
	}
	if !mp.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.unload(ctx, req, name, path)
	}
	published := mp.Status.Profile
	if published == nil {
		// Nothing published yet. A profile that was loaded before stays loaded:
		// unloading it would break every pod that references it.
		return ctrl.Result{}, nil
	}

	// The manager decides the mode. Anything but an explicit Enforce is complain.
	mode := apparmor.Complain
	if mp.Status.EnforcementLevel == string(apparmor.Enforce) {
		mode = apparmor.Enforce
	}

	if got, ok := r.State.get(name); ok && got.hash == published.Hash && got.mode == mode {
		return ctrl.Result{}, r.report(ctx, &mp, published.Hash, mode, nil)
	}

	loadErr := r.load(ctx, published, name, path, mode)
	if loadErr != nil {
		log.Error(loadErr, "Failed to load profile", "profile", name)
	} else {
		r.State.set(name, loaded{hash: published.Hash, mode: mode})
		log.Info("Loaded profile", "profile", name, "mode", mode, "hash", published.Hash)
	}
	if err := r.report(ctx, &mp, published.Hash, mode, loadErr); err != nil {
		return ctrl.Result{}, err
	}
	// A profile that failed to load is retried with backoff.
	return ctrl.Result{}, loadErr
}

func (r *Reconciler) load(
	ctx context.Context, published *armadillov1alpha1.PublishedProfile, name, path string, mode apparmor.Mode,
) error {
	if got := apparmor.Hash(published.Content); got != published.Hash {
		return fmt.Errorf("published content hashes to %s, not the published %s", got, published.Hash)
	}
	text, err := apparmor.Render(published.Content, name, mode)
	if err != nil {
		return fmt.Errorf("invalid profile: %w", err)
	}
	// Write next to the target and rename, so a crash never leaves half a profile.
	tmp, err := os.CreateTemp(r.ProfileDir, ".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if _, err := tmp.WriteString(text); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Parse into the kernel before replacing the file on disk: if the parser
	// rejects the new text, the file keeps describing what is actually loaded.
	if err := r.Parser.Load(ctx, tmp.Name()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (r *Reconciler) unload(ctx context.Context, req ctrl.Request, name, path string) error {
	if _, err := os.Stat(path); err == nil {
		if err := r.Parser.Remove(ctx, path); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		logf.FromContext(ctx).Info("Removed profile", "profile", name)
	}
	r.State.delete(name)
	pns := &armadillov1alpha1.ProfileNodeStatus{
		ObjectMeta: metav1.ObjectMeta{Name: StatusName(r.NodeName, req.Namespace, req.Name)},
	}
	return client.IgnoreNotFound(r.Delete(ctx, pns))
}

// report records what happened on this node in its ProfileNodeStatus.
func (r *Reconciler) report(
	ctx context.Context, mp *armadillov1alpha1.ManagedProfile, hash string, mode apparmor.Mode, loadErr error,
) error {
	key := client.ObjectKey{Name: StatusName(r.NodeName, mp.Namespace, mp.Name)}
	var pns armadillov1alpha1.ProfileNodeStatus
	if err := r.Get(ctx, key, &pns); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		pns = armadillov1alpha1.ProfileNodeStatus{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name},
			Spec: armadillov1alpha1.ProfileNodeStatusSpec{
				NodeName:   r.NodeName,
				ProfileRef: armadillov1alpha1.ManagedProfileReference{Namespace: mp.Namespace, Name: mp.Name},
			},
		}
		if err := r.Create(ctx, &pns); err != nil {
			return err
		}
	}

	before := pns.DeepCopy()
	cond := metav1.Condition{
		Type:               ConditionLoaded,
		Status:             metav1.ConditionTrue,
		Reason:             "Loaded",
		Message:            "The published profile is loaded on the node",
		ObservedGeneration: pns.Generation,
	}
	if loadErr != nil {
		// What was loaded before, if anything, is still loaded: keep reporting it.
		cond.Status = metav1.ConditionFalse
		cond.Reason = "LoadFailed"
		cond.Message = loadErr.Error()
	} else {
		pns.Status.LoadedHash = hash
		pns.Status.Mode = armadillov1alpha1.ProfileMode(mode)
	}
	meta.SetStatusCondition(&pns.Status.Conditions, cond)
	if equalStatus(before.Status, pns.Status) {
		return nil
	}
	return r.Status().Update(ctx, &pns)
}

func equalStatus(a, b armadillov1alpha1.ProfileNodeStatusStatus) bool {
	if a.LoadedHash != b.LoadedHash || a.Mode != b.Mode || len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for i := range a.Conditions {
		if a.Conditions[i] != b.Conditions[i] {
			return false
		}
	}
	return true
}

// SetupWithManager sets up the loader with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&armadillov1alpha1.ManagedProfile{}).
		Named("loader").
		Complete(r)
}
