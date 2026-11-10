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
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	armadillov1alpha1 "github.com/tanchwa/armadillo/api/v1alpha1"
	"github.com/tanchwa/armadillo/internal/apparmor"
	"github.com/tanchwa/armadillo/internal/gitsource"
)

// fakeFetcher stands in for the git repository.
type fakeFetcher struct {
	content  string
	revision string
	err      error
	calls    int
	last     gitsource.Repo
}

func (f *fakeFetcher) Fetch(_ context.Context, repo gitsource.Repo) (string, string, error) {
	f.calls++
	f.last = repo
	return f.content, f.revision, f.err
}

var _ = Describe("ManagedProfile Controller", func() {
	const validProfile = "profile myapp flags=(attach_disconnected) {\n  /tmp/x r,\n}\n"

	var (
		ctx        = context.Background()
		namespace  string
		fetcher    *fakeFetcher
		reconciler *ManagedProfileReconciler
		key        client.ObjectKey
	)

	createSecret := func(secretLabels map[string]string) {
		GinkgoHelper()
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "repo", Namespace: namespace, Labels: secretLabels},
			StringData: map[string]string{
				"url":      "https://github.com/Tanchwa/app.git",
				"username": "bot",
				"password": "token",
			},
		})).To(Succeed())
	}
	repositoryLabel := map[string]string{
		armadillov1alpha1.SecretTypeLabel: armadillov1alpha1.SecretTypeRepository,
	}
	reconcileOnce := func() armadillov1alpha1.ManagedProfile {
		GinkgoHelper()
		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Minute), "the repository must be polled again")
		var mp armadillov1alpha1.ManagedProfile
		Expect(k8sClient.Get(ctx, key, &mp)).To(Succeed())
		return mp
	}
	published := func(mp armadillov1alpha1.ManagedProfile) *metav1.Condition {
		return meta.FindStatusCondition(mp.Status.Conditions, ConditionPublished)
	}

	BeforeEach(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "managedprofile-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.Name

		fetcher = &fakeFetcher{content: validProfile, revision: "abc123"}
		reconciler = &ManagedProfileReconciler{
			Client:       k8sClient,
			Scheme:       k8sClient.Scheme(),
			Fetcher:      fetcher,
			SyncInterval: time.Minute,
		}
		mp := &armadillov1alpha1.ManagedProfile{
			ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: namespace},
			Spec: armadillov1alpha1.ManagedProfileSpec{
				GitOptions: &armadillov1alpha1.GitOptions{
					RepositorySecret: corev1.LocalObjectReference{Name: "repo"},
					Path:             "apparmor/myapp.profile",
					Branch:           "main",
				},
			},
		}
		Expect(k8sClient.Create(ctx, mp)).To(Succeed())
		key = client.ObjectKeyFromObject(mp)
	})

	It("publishes the profile committed in git", func() {
		createSecret(repositoryLabel)
		mp := reconcileOnce()

		Expect(mp.Status.Profile).NotTo(BeNil())
		Expect(mp.Status.Profile.Content).To(Equal(validProfile))
		Expect(mp.Status.Profile.Hash).To(Equal(apparmor.Hash(validProfile)))
		Expect(mp.Status.Profile.Revision).To(Equal("abc123"))
		Expect(published(mp).Status).To(Equal(metav1.ConditionTrue))
		Expect(fetcher.last).To(Equal(gitsource.Repo{
			URL:      "https://github.com/Tanchwa/app.git",
			Username: "bot",
			Password: "token",
			Branch:   "main",
			Path:     "apparmor/myapp.profile",
		}))
	})

	It("keeps the published profile when a later commit is invalid", func() {
		createSecret(repositoryLabel)
		reconcileOnce()

		fetcher.content = "profile myapp /usr/bin/kubelet {\n}\n"
		fetcher.revision = "bad456"
		mp := reconcileOnce()

		Expect(mp.Status.Profile.Content).To(Equal(validProfile), "the invalid commit must not replace the profile")
		Expect(mp.Status.Profile.Revision).To(Equal("abc123"))
		Expect(published(mp).Status).To(Equal(metav1.ConditionFalse))
		Expect(published(mp).Reason).To(Equal("InvalidProfile"))
	})

	It("keeps the published profile when the repository cannot be read", func() {
		createSecret(repositoryLabel)
		reconcileOnce()

		fetcher.err = errors.New("connection refused")
		mp := reconcileOnce()

		Expect(mp.Status.Profile.Revision).To(Equal("abc123"))
		Expect(published(mp).Reason).To(Equal("FetchFailed"))
		Expect(published(mp).Message).To(ContainSubstring("connection refused"))
	})

	It("does not read a Secret without the repository label", func() {
		createSecret(nil)
		mp := reconcileOnce()

		Expect(mp.Status.Profile).To(BeNil())
		Expect(published(mp).Reason).To(Equal("SecretNotFound"))
		Expect(published(mp).Message).To(ContainSubstring(armadillov1alpha1.SecretTypeLabel))
		Expect(fetcher.calls).To(BeZero(), "the Secret's URL and credentials must not be used")
	})

	It("reports a missing Secret", func() {
		mp := reconcileOnce()
		Expect(mp.Status.Profile).To(BeNil())
		Expect(published(mp).Reason).To(Equal("SecretNotFound"))
	})
})
