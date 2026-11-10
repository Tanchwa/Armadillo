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
	"fmt"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	armadillov1alpha1 "github.com/tanchwa/armadillo/api/v1alpha1"
	"github.com/tanchwa/armadillo/internal/apparmor"
)

// log is for logging in this package.
var podlog = logf.Log.WithName("pod-resource")

const (
	// GateContainerName is the name of the injected startup gate.
	GateContainerName = "armadillo-gate"

	// The annotation Kubernetes used for AppArmor before the securityContext field.
	legacyAnnotationPrefix = "container.apparmor.security.beta.kubernetes.io/"
)

// PodWebhookOptions configures what the webhook injects.
type PodWebhookOptions struct {
	// GateImage is the image of the startup gate. It must contain /gate.
	GateImage string
	// LoaderPort is the host port of the loader's gate endpoint.
	LoaderPort int
}

// SetupPodWebhookWithManager registers the webhook for Pod in the manager.
func SetupPodWebhookWithManager(mgr ctrl.Manager, opts PodWebhookOptions) error {
	if opts.GateImage == "" {
		return fmt.Errorf("the gate image is required")
	}
	return ctrl.NewWebhookManagedBy(mgr, &corev1.Pod{}).
		WithDefaulter(&PodCustomDefaulter{Client: mgr.GetClient(), Options: opts}).
		Complete()
}

// Pods cannot change their containers after creation, so only creates are
// intercepted. failurePolicy is fail: if the webhook is down, a bound pod must
// not start without its profile. config/default limits the webhook to
// namespaces that opted in, so an outage cannot block the rest of the cluster.
// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=pods.armadillo.andrewsutliff.com,admissionReviewVersions=v1,reinvocationPolicy=IfNeeded,timeoutSeconds=5

// PodCustomDefaulter puts pods selected by a ProfileBinding under the bound
// profile and injects the startup gate.
type PodCustomDefaulter struct {
	Client  client.Reader
	Options PodWebhookOptions
}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Pod.
func (d *PodCustomDefaulter) Default(ctx context.Context, pod *corev1.Pod) error {
	// On create the namespace is often only in the request, not in the object.
	namespace := pod.Namespace
	if req, err := admission.RequestFromContext(ctx); err == nil && req.Namespace != "" {
		namespace = req.Namespace
	}

	var bindings armadillov1alpha1.ProfileBindingList
	if err := d.Client.List(ctx, &bindings, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("listing ProfileBindings: %w", err)
	}
	// A fixed order, so two bindings that collide are always reported the same way.
	slices.SortFunc(bindings.Items, func(a, b armadillov1alpha1.ProfileBinding) int {
		return strings.Compare(a.Name, b.Name)
	})

	boundBy := map[string]string{} // container name -> binding name
	var profiles []string
	for _, binding := range bindings.Items {
		// A nil selector selects nothing; an empty one selects every pod.
		selector, err := metav1.LabelSelectorAsSelector(binding.Spec.PodSelector)
		if err != nil {
			return fmt.Errorf("ProfileBinding %s has an invalid podSelector: %w", binding.Name, err)
		}
		if !selector.Matches(labels.Set(pod.Labels)) {
			continue
		}
		profile := apparmor.ProfileName(namespace, binding.Spec.ProfileRef.Name)
		bound := false
		for _, containers := range [][]corev1.Container{pod.Spec.InitContainers, pod.Spec.Containers} {
			for i := range containers {
				c := &containers[i]
				if c.Name == GateContainerName ||
					(len(binding.Spec.Containers) > 0 && !slices.Contains(binding.Spec.Containers, c.Name)) {
					continue
				}
				if other, ok := boundBy[c.Name]; ok {
					return fmt.Errorf("container %q is selected by ProfileBindings %q and %q; a container can have one profile",
						c.Name, other, binding.Name)
				}
				boundBy[c.Name] = binding.Name
				if c.SecurityContext == nil {
					c.SecurityContext = &corev1.SecurityContext{}
				}
				c.SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{
					Type:             corev1.AppArmorProfileTypeLocalhost,
					LocalhostProfile: ptr.To(profile),
				}
				// The API server rejects a pod whose legacy annotation disagrees with the field.
				delete(pod.Annotations, legacyAnnotationPrefix+c.Name)
				bound = true
			}
		}
		if bound && !slices.Contains(profiles, profile) {
			profiles = append(profiles, profile)
		}
	}
	if len(profiles) == 0 {
		return nil
	}

	// Replace rather than append, so a second invocation does not add a second gate.
	pod.Spec.InitContainers = slices.DeleteFunc(pod.Spec.InitContainers, func(c corev1.Container) bool {
		return c.Name == GateContainerName
	})
	// First, so it also gates the pod's own init containers.
	pod.Spec.InitContainers = append([]corev1.Container{d.gate(profiles)}, pod.Spec.InitContainers...)
	podlog.V(1).Info("Bound Pod to profiles", "namespace", namespace, "name", pod.Name,
		"generateName", pod.GenerateName, "profiles", profiles)
	return nil
}

// gate is the native sidecar that holds the pod's containers back until the
// loader on the node has loaded every profile the pod needs.
func (d *PodCustomDefaulter) gate(profiles []string) corev1.Container {
	return corev1.Container{
		Name:    GateContainerName,
		Image:   d.Options.GateImage,
		Command: []string{"/gate"},
		// restartPolicy Always on an init container makes it a native sidecar:
		// the kubelet waits for its startup probe before starting anything else.
		RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways),
		Env: []corev1.EnvVar{{
			Name:      "HOST_IP",
			ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}},
		}},
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{
				"/gate", "check",
				"--loader-port=" + strconv.Itoa(d.Options.LoaderPort),
				"--profiles=" + strings.Join(profiles, ","),
			}}},
			PeriodSeconds:  1,
			TimeoutSeconds: 3,
			// Ten minutes before the kubelet restarts the gate and starts backing off.
			FailureThreshold: 600,
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("5m"),
				corev1.ResourceMemory: resource.MustParse("8Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
		},
		// Passes the "restricted" Pod Security Standard, so the gate never makes
		// a pod inadmissible in a namespace that enforces it.
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
			RunAsNonRoot:             ptr.To(true),
			RunAsUser:                ptr.To[int64](65532),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
	}
}
