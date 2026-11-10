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

package v1

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	armadillov1alpha1 "github.com/tanchwa/armadillo/api/v1alpha1"
)

const (
	testGateImage = "example.com/armadillo:test"
	// appName is the app label of the test pods and the name of their binding.
	appName = "web"
)

// These tests create pods through a real API server with the webhook
// registered, so they cover the whole admission path, not just Default().
var _ = Describe("Pod Webhook", func() {
	var namespace string

	newPod := func(podLabels map[string]string, containers ...string) *corev1.Pod {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "pod-", Namespace: namespace, Labels: podLabels},
		}
		for _, name := range containers {
			pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: name, Image: "app:1"})
		}
		return pod
	}
	bind := func(name, profile string, selector *metav1.LabelSelector, containers ...string) {
		GinkgoHelper()
		Expect(k8sClient.Create(ctx, &armadillov1alpha1.ProfileBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: armadillov1alpha1.ProfileBindingSpec{
				ProfileRef:  corev1.LocalObjectReference{Name: profile},
				PodSelector: selector,
				Containers:  containers,
			},
		})).To(Succeed())
	}
	profileOf := func(c corev1.Container) string {
		if c.SecurityContext == nil || c.SecurityContext.AppArmorProfile == nil ||
			c.SecurityContext.AppArmorProfile.LocalhostProfile == nil {
			return ""
		}
		return *c.SecurityContext.AppArmorProfile.LocalhostProfile
	}
	appLabels := map[string]string{"app": appName}
	appSelector := &metav1.LabelSelector{MatchLabels: appLabels}

	BeforeEach(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "webhook-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.Name
	})

	It("binds a selected pod to the profile and injects the gate", func() {
		bind(appName, "myapp", appSelector)

		// The webhook reads bindings from a cache, so the first creates may precede it.
		var pod *corev1.Pod
		Eventually(func(g Gomega) {
			pod = newPod(appLabels, "app", "helper")
			g.Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			g.Expect(pod.Spec.InitContainers).To(HaveLen(1))
		}).Should(Succeed())

		want := "armadillo." + namespace + ".myapp"
		for _, c := range pod.Spec.Containers {
			Expect(profileOf(c)).To(Equal(want), "container %s", c.Name)
			Expect(c.SecurityContext.AppArmorProfile.Type).To(Equal(corev1.AppArmorProfileTypeLocalhost))
		}

		gate := pod.Spec.InitContainers[0]
		Expect(gate.Name).To(Equal(GateContainerName))
		Expect(gate.Image).To(Equal(testGateImage))
		Expect(gate.RestartPolicy).To(HaveValue(Equal(corev1.ContainerRestartPolicyAlways)))
		Expect(gate.StartupProbe.Exec.Command).To(Equal(
			[]string{gateBinary, "check", "--loader-port=9099", "--profiles=" + want}))
		Expect(profileOf(gate)).To(BeEmpty(), "the gate itself must not depend on the profile it waits for")
		Expect(pod.Spec.Volumes).To(BeEmpty(), "the gate must not add volumes, hostPath least of all")
	})

	It("leaves a pod alone that no binding selects", func() {
		bind(appName, "myapp", appSelector)
		// Wait until the webhook sees the binding, then check a pod it does not select.
		Eventually(func(g Gomega) {
			probe := newPod(appLabels, "app")
			g.Expect(k8sClient.Create(ctx, probe)).To(Succeed())
			g.Expect(probe.Spec.InitContainers).To(HaveLen(1))
		}).Should(Succeed())

		pod := newPod(map[string]string{"app": "other"}, "app")
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		Expect(pod.Spec.InitContainers).To(BeEmpty())
		Expect(profileOf(pod.Spec.Containers[0])).To(BeEmpty())
	})

	It("binds only the named containers, each to its own profile", func() {
		bind("a-app", "app-profile", appSelector, "app")
		bind("b-helper", "helper-profile", appSelector, "helper")

		var pod *corev1.Pod
		Eventually(func(g Gomega) {
			pod = newPod(appLabels, "app", "helper", "unbound")
			g.Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			g.Expect(profileOf(pod.Spec.Containers[0])).NotTo(BeEmpty())
			g.Expect(profileOf(pod.Spec.Containers[1])).NotTo(BeEmpty())
		}).Should(Succeed())

		Expect(profileOf(pod.Spec.Containers[0])).To(Equal("armadillo." + namespace + ".app-profile"))
		Expect(profileOf(pod.Spec.Containers[1])).To(Equal("armadillo." + namespace + ".helper-profile"))
		Expect(profileOf(pod.Spec.Containers[2])).To(BeEmpty())
		Expect(pod.Spec.InitContainers).To(HaveLen(1))
		Expect(pod.Spec.InitContainers[0].StartupProbe.Exec.Command[3]).To(Equal(
			"--profiles=armadillo." + namespace + ".app-profile,armadillo." + namespace + ".helper-profile"))
	})

	It("binds without a gate when the binding disables it", func() {
		Expect(k8sClient.Create(ctx, &armadillov1alpha1.ProfileBinding{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace},
			Spec: armadillov1alpha1.ProfileBindingSpec{
				ProfileRef:  corev1.LocalObjectReference{Name: "myapp"},
				PodSelector: appSelector,
				StartupGate: armadillov1alpha1.StartupGateDisabled,
			},
		})).To(Succeed())

		var pod *corev1.Pod
		Eventually(func(g Gomega) {
			pod = newPod(appLabels, "app")
			g.Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			g.Expect(profileOf(pod.Spec.Containers[0])).NotTo(BeEmpty())
		}).Should(Succeed())

		Expect(profileOf(pod.Spec.Containers[0])).To(Equal("armadillo." + namespace + ".myapp"))
		Expect(pod.Spec.InitContainers).To(BeEmpty())
	})

	It("gates only on the profiles whose binding wants the gate", func() {
		bind("a-app", "app-profile", appSelector, "app")
		Expect(k8sClient.Create(ctx, &armadillov1alpha1.ProfileBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "b-helper", Namespace: namespace},
			Spec: armadillov1alpha1.ProfileBindingSpec{
				ProfileRef:  corev1.LocalObjectReference{Name: "helper-profile"},
				PodSelector: appSelector,
				Containers:  []string{"helper"},
				StartupGate: armadillov1alpha1.StartupGateDisabled,
			},
		})).To(Succeed())

		var pod *corev1.Pod
		Eventually(func(g Gomega) {
			pod = newPod(appLabels, "app", "helper")
			g.Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			g.Expect(profileOf(pod.Spec.Containers[0])).NotTo(BeEmpty())
			g.Expect(profileOf(pod.Spec.Containers[1])).NotTo(BeEmpty())
		}).Should(Succeed())

		Expect(pod.Spec.InitContainers).To(HaveLen(1))
		Expect(pod.Spec.InitContainers[0].StartupProbe.Exec.Command[3]).To(Equal(
			"--profiles=armadillo." + namespace + ".app-profile"))
	})

	It("defaults a binding to the gate being enabled", func() {
		bind(appName, "myapp", appSelector)
		var binding armadillov1alpha1.ProfileBinding
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: appName}, &binding)).To(Succeed())
		Expect(binding.Spec.StartupGate).To(Equal(armadillov1alpha1.StartupGateEnabled))
	})

	It("rejects a pod whose container two bindings select", func() {
		bind("first", "one", appSelector)
		bind("second", "two", appSelector)
		Eventually(func() error {
			return k8sClient.Create(ctx, newPod(appLabels, "app"))
		}).Should(MatchError(ContainSubstring(`selected by ProfileBindings "first" and "second"`)))
	})

	It("overrides a profile the pod asked for itself", func() {
		bind(appName, "myapp", appSelector)
		var pod *corev1.Pod
		Eventually(func(g Gomega) {
			pod = newPod(appLabels, "app")
			pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
				AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined},
			}
			g.Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			g.Expect(pod.Spec.InitContainers).To(HaveLen(1))
		}).Should(Succeed())
		Expect(profileOf(pod.Spec.Containers[0])).To(Equal("armadillo." + namespace + ".myapp"))
	})

	It("does not let a binding with no selector match anything", func() {
		// The CRD requires podSelector, so a nil selector cannot be created through
		// the API; Default() must still treat one as matching nothing.
		d := &PodCustomDefaulter{
			Client:  nilSelectorBindings{namespace: namespace},
			Options: PodWebhookOptions{GateImage: testGateImage, LoaderPort: 9099},
		}
		pod := newPod(appLabels, "app")
		Expect(d.Default(ctx, pod)).To(Succeed())
		Expect(pod.Spec.InitContainers).To(BeEmpty())
		Expect(profileOf(pod.Spec.Containers[0])).To(BeEmpty())
	})
})

// nilSelectorBindings is a reader that returns one binding without a podSelector.
type nilSelectorBindings struct {
	client.Reader
	namespace string
}

func (n nilSelectorBindings) List(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
	list.(*armadillov1alpha1.ProfileBindingList).Items = []armadillov1alpha1.ProfileBinding{{
		ObjectMeta: metav1.ObjectMeta{Name: "nil-selector", Namespace: n.namespace},
		Spec:       armadillov1alpha1.ProfileBindingSpec{ProfileRef: corev1.LocalObjectReference{Name: "myapp"}},
	}}
	return nil
}
