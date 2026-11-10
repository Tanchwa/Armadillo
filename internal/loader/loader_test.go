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

package loader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	armadillov1alpha1 "github.com/tanchwa/armadillo/api/v1alpha1"
	"github.com/tanchwa/armadillo/internal/apparmor"
)

// profileName is the name the test ManagedProfile shop/myapp is loaded under.
const profileName = "armadillo.shop.myapp"

const profileText = "profile myapp flags=(complain) {\n  /tmp/x r,\n}\n"

// fakeParser records the text of every file it is asked to load.
type fakeParser struct {
	loads   []string
	removes int
	fail    error
}

func (p *fakeParser) Load(_ context.Context, path string) error {
	if p.fail != nil {
		return p.fail
	}
	b, err := os.ReadFile(path)
	p.loads = append(p.loads, string(b))
	return err
}

func (p *fakeParser) Remove(context.Context, string) error {
	p.removes++
	return nil
}

func setup(t *testing.T, content string) (*Reconciler, *fakeParser, client.Client, ctrl.Request) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := armadillov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	mp := &armadillov1alpha1.ManagedProfile{
		ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "myapp"},
		Status: armadillov1alpha1.ManagedProfileStatus{
			Profile: &armadillov1alpha1.PublishedProfile{Content: content, Hash: apparmor.Hash(content)},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mp).
		WithStatusSubresource(&armadillov1alpha1.ProfileNodeStatus{}, &armadillov1alpha1.ManagedProfile{}).Build()
	parser := &fakeParser{}
	r := &Reconciler{Client: c, NodeName: "node-a", ProfileDir: t.TempDir(), Parser: parser, State: NewState()}
	return r, parser, c, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(mp)}
}

func nodeStatus(t *testing.T, c client.Client) armadillov1alpha1.ProfileNodeStatus {
	t.Helper()
	var pns armadillov1alpha1.ProfileNodeStatus
	if err := c.Get(context.Background(), client.ObjectKey{Name: "node-a.shop.myapp"}, &pns); err != nil {
		t.Fatal(err)
	}
	return pns
}

func TestLoadsInComplainThenEnforce(t *testing.T) {
	ctx := context.Background()
	r, parser, c, req := setup(t, profileText)

	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(parser.loads) != 1 ||
		!strings.Contains(parser.loads[0], "profile armadillo.shop.myapp flags=(attach_disconnected,mediate_deleted,complain) {") {
		t.Fatalf("loads: %q", parser.loads)
	}
	pns := nodeStatus(t, c)
	if pns.Spec.NodeName != "node-a" || pns.Status.LoadedHash != apparmor.Hash(profileText) ||
		pns.Status.Mode != "Complain" || !meta.IsStatusConditionTrue(pns.Status.Conditions, ConditionLoaded) {
		t.Errorf("status after first load: %+v %+v", pns.Spec, pns.Status)
	}
	onDisk, err := os.ReadFile(filepath.Join(r.ProfileDir, profileName))
	if err != nil || string(onDisk) != parser.loads[0] {
		t.Errorf("file on disk differs from what was loaded: %v", err)
	}

	// Reconciling again with nothing changed must not reload.
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(parser.loads) != 1 {
		t.Errorf("reloaded an unchanged profile: %d loads", len(parser.loads))
	}

	// The manager promotes: same content, same name, new mode.
	var mp armadillov1alpha1.ManagedProfile
	if err := c.Get(ctx, req.NamespacedName, &mp); err != nil {
		t.Fatal(err)
	}
	mp.Status.EnforcementLevel = string(apparmor.Enforce)
	if err := c.Status().Update(ctx, &mp); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(parser.loads) != 2 ||
		!strings.Contains(parser.loads[1], "profile armadillo.shop.myapp flags=(attach_disconnected,mediate_deleted) {") {
		t.Fatalf("loads after promotion: %q", parser.loads)
	}
	if got := nodeStatus(t, c).Status.Mode; got != armadillov1alpha1.ProfileMode(apparmor.Enforce) {
		t.Errorf("mode after promotion: %s", got)
	}
}

func TestInvalidProfileIsNotLoaded(t *testing.T) {
	ctx := context.Background()
	r, parser, c, req := setup(t, "profile x /usr/bin/kubelet {\n}\n")
	if _, err := r.Reconcile(ctx, req); err == nil {
		t.Fatal("expected an error for a profile with an attachment")
	}
	if len(parser.loads) != 0 {
		t.Errorf("loaded an invalid profile: %q", parser.loads)
	}
	pns := nodeStatus(t, c)
	if meta.IsStatusConditionTrue(pns.Status.Conditions, ConditionLoaded) || pns.Status.LoadedHash != "" {
		t.Errorf("status claims the profile is loaded: %+v", pns.Status)
	}
	if _, ok := r.State.get(profileName); ok {
		t.Error("state claims the profile is loaded")
	}
}

func TestTamperedContentIsNotLoaded(t *testing.T) {
	ctx := context.Background()
	r, parser, c, req := setup(t, profileText)
	var mp armadillov1alpha1.ManagedProfile
	if err := c.Get(ctx, req.NamespacedName, &mp); err != nil {
		t.Fatal(err)
	}
	mp.Status.Profile.Content += "\n"
	if err := c.Status().Update(ctx, &mp); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, req); err == nil || len(parser.loads) != 0 {
		t.Errorf("loaded content that does not match its hash: err=%v loads=%d", err, len(parser.loads))
	}
}

func TestParserFailureKeepsPreviousFile(t *testing.T) {
	ctx := context.Background()
	r, parser, c, req := setup(t, profileText)
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.ProfileDir, profileName)
	before, _ := os.ReadFile(path)

	var mp armadillov1alpha1.ManagedProfile
	if err := c.Get(ctx, req.NamespacedName, &mp); err != nil {
		t.Fatal(err)
	}
	mp.Status.EnforcementLevel = string(apparmor.Enforce)
	if err := c.Status().Update(ctx, &mp); err != nil {
		t.Fatal(err)
	}
	parser.fail = errors.New("parser said no")
	if _, err := r.Reconcile(ctx, req); err == nil {
		t.Fatal("expected the parser failure to surface")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("file on disk changed although the kernel still holds the old profile")
	}
	pns := nodeStatus(t, c)
	if pns.Status.Mode != "Complain" || meta.IsStatusConditionTrue(pns.Status.Conditions, ConditionLoaded) {
		t.Errorf("status after failed promotion: %+v", pns.Status)
	}
}

func TestDeletedProfileIsUnloaded(t *testing.T) {
	ctx := context.Background()
	r, parser, c, req := setup(t, profileText)
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	var mp armadillov1alpha1.ManagedProfile
	if err := c.Get(ctx, req.NamespacedName, &mp); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, &mp); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	if parser.removes != 1 {
		t.Errorf("removes: %d", parser.removes)
	}
	if _, err := os.Stat(filepath.Join(r.ProfileDir, profileName)); !os.IsNotExist(err) {
		t.Errorf("profile file still on disk: %v", err)
	}
	var pns armadillov1alpha1.ProfileNodeStatus
	if err := c.Get(ctx, client.ObjectKey{Name: "node-a.shop.myapp"}, &pns); err == nil {
		t.Error("ProfileNodeStatus still exists")
	}
}

func TestGateEndpoint(t *testing.T) {
	ctx := context.Background()
	r, _, _, req := setup(t, profileText)
	srv := httptest.NewServer(r.State.Handler())
	defer srv.Close()

	get := func(path string) int {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if got := get("/v1/loaded/" + profileName); got != http.StatusNotFound {
		t.Errorf("before load: %d", got)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	if got := get("/v1/loaded/" + profileName); got != http.StatusOK {
		t.Errorf("after load: %d", got)
	}
	if got := get("/v1/loaded/armadillo.shop.other"); got != http.StatusNotFound {
		t.Errorf("other profile: %d", got)
	}
}

func TestStatusNameFitsLimit(t *testing.T) {
	long := StatusName(strings.Repeat("n", 253), "ns", strings.Repeat("p", 253))
	if len(long) > 253 {
		t.Errorf("name is %d characters", len(long))
	}
}
