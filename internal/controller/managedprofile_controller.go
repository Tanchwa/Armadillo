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

package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	armadillov1alpha1 "github.com/tanchwa/armadillo/api/v1alpha1"
	"github.com/tanchwa/armadillo/internal/apparmor"
	"github.com/tanchwa/armadillo/internal/gitsource"
)

// ConditionPublished is the ManagedProfile condition that says whether the
// profile in status.profile is the one currently committed in git.
const ConditionPublished = "Published"

// ManagedProfileReconciler reconciles a ManagedProfile object
type ManagedProfileReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Fetcher reads the profile from the application's repository.
	Fetcher gitsource.Fetcher
	// SyncInterval is how often the repository is read again.
	SyncInterval time.Duration
}

// +kubebuilder:rbac:groups=armadillo.andrewsutliff.com,resources=managedprofiles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=armadillo.andrewsutliff.com,resources=managedprofiles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=armadillo.andrewsutliff.com,resources=managedprofiles/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile fetches the profile from git, validates it, and publishes it in
// the ManagedProfile's status, which is where the loaders read it from.
//
// A profile that cannot be fetched or does not validate is never published,
// and the profile published before stays: a bad commit must not unload a
// profile that pods are running under.
func (r *ManagedProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var mp armadillov1alpha1.ManagedProfile
	if err := r.Get(ctx, req.NamespacedName, &mp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !mp.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	before := mp.Status.DeepCopy()

	content, revision, reason, err := r.fetch(ctx, &mp)
	if err == nil {
		// Validate as the loaders will, so a profile they would refuse is caught
		// here, once, instead of on every node.
		if _, renderErr := apparmor.Render(content, apparmor.ProfileName(mp.Namespace, mp.Name), apparmor.Complain); renderErr != nil {
			reason, err = "InvalidProfile", renderErr
		}
	}

	cond := metav1.Condition{Type: ConditionPublished, ObservedGeneration: mp.Generation}
	if err != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = reason
		cond.Message = err.Error()
		log.Info("Could not publish profile", "reason", reason, "error", err.Error())
	} else {
		mp.Status.Profile = &armadillov1alpha1.PublishedProfile{
			Content:  content,
			Hash:     apparmor.Hash(content),
			Revision: revision,
		}
		cond.Status = metav1.ConditionTrue
		cond.Reason = "Fetched"
		cond.Message = "The profile is published as committed at " + revision
	}
	meta.SetStatusCondition(&mp.Status.Conditions, cond)

	if !apiequality.Semantic.DeepEqual(*before, mp.Status) {
		if err := r.Status().Update(ctx, &mp); err != nil {
			return ctrl.Result{}, err
		}
		if cond.Status == metav1.ConditionTrue {
			log.Info("Published profile", "revision", revision, "hash", mp.Status.Profile.Hash)
		}
	}
	// Poll rather than return the error: every failure here is waiting on
	// someone to fix a Secret, a repository or a commit, not on a retry.
	return ctrl.Result{RequeueAfter: r.SyncInterval}, nil
}

// fetch reads the profile from the repository the ManagedProfile points at.
// On failure it returns the condition reason to report.
func (r *ManagedProfileReconciler) fetch(
	ctx context.Context, mp *armadillov1alpha1.ManagedProfile,
) (content, revision, reason string, err error) {
	if mp.Spec.GitOptions == nil {
		return "", "", "NoSource", fmt.Errorf("spec.gitOptions is not set")
	}
	git := mp.Spec.GitOptions

	var secret corev1.Secret
	key := client.ObjectKey{Namespace: mp.Namespace, Name: git.RepositorySecret.Name}
	err = r.Get(ctx, key, &secret)
	if err == nil && secret.Labels[armadillov1alpha1.SecretTypeLabel] != armadillov1alpha1.SecretTypeRepository {
		// The manager's cache only holds labeled Secrets, so an unlabeled one
		// normally shows up as NotFound. Treat both the same.
		err = apierrors.NewNotFound(corev1.Resource("secrets"), key.Name)
	}
	if apierrors.IsNotFound(err) {
		return "", "", "SecretNotFound", fmt.Errorf("no Secret %q with the label %s=%s in namespace %s",
			key.Name, armadillov1alpha1.SecretTypeLabel, armadillov1alpha1.SecretTypeRepository, mp.Namespace)
	}
	if err != nil {
		return "", "", "SecretNotFound", err
	}
	url := string(secret.Data["url"])
	if url == "" {
		return "", "", "InvalidSecret", fmt.Errorf("Secret %q has no \"url\" key", key.Name)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	content, revision, err = r.Fetcher.Fetch(fetchCtx, gitsource.Repo{
		URL:      url,
		Username: string(secret.Data["username"]),
		Password: string(secret.Data["password"]),
		Branch:   git.Branch,
		Path:     git.Path,
	})
	if err != nil {
		return "", "", "FetchFailed", err
	}
	return content, revision, "", nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&armadillov1alpha1.ManagedProfile{}).
		// A repository Secret that appears or changes is a reason to fetch again.
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.profilesForSecret)).
		Named("managedprofile").
		Complete(r)
}

func (r *ManagedProfileReconciler) profilesForSecret(ctx context.Context, secret client.Object) []reconcile.Request {
	var profiles armadillov1alpha1.ManagedProfileList
	if err := r.List(ctx, &profiles, client.InNamespace(secret.GetNamespace())); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list ManagedProfiles for Secret", "secret", secret.GetName())
		return nil
	}
	var requests []reconcile.Request
	for i := range profiles.Items {
		git := profiles.Items[i].Spec.GitOptions
		if git != nil && git.RepositorySecret.Name == secret.GetName() {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&profiles.Items[i])})
		}
	}
	return requests
}
