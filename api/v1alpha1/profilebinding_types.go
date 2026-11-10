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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ProfileBindingSpec defines the desired state of ProfileBinding
type ProfileBindingSpec struct {
	// ProfileRef is the ManagedProfile, in the same namespace, to bind the selected pods to.
	// +required
	ProfileRef corev1.LocalObjectReference `json:"profileRef,omitzero"`

	// PodSelector selects the pods in this namespace that run under the profile.
	// A nil selector matches no pods; an empty selector ({}) matches every pod in the namespace.
	// +required
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty"`

	// Containers limits the binding to these container names. Empty means every container in the pod.
	// +optional
	// +listType=set
	// +kubebuilder:validation:items:MaxLength=63
	Containers []string `json:"containers,omitempty"`

	// StartupGate controls whether the selected pods get the startup gate.
	// +optional
	// +default="Enabled"
	StartupGate StartupGatePolicy `json:"startupGate,omitempty"`
}

// StartupGatePolicy describes whether pods wait for their profile before starting.
// Only one of the following policies may be specified.
// If none of the following policies is specified, the default one
// is StartupGateEnabled.
// +kubebuilder:validation:Enum=Enabled;Disabled
type StartupGatePolicy string

const (
	// StartupGateEnabled injects a sidecar that holds the pod's containers back
	// until the profile is loaded on the pod's node. The pod waits instead of
	// failing when it lands on a new node, or when the profile is new.
	StartupGateEnabled StartupGatePolicy = "Enabled"

	// StartupGateDisabled injects no sidecar. A pod that starts on a node where
	// the profile is not loaded yet fails with CreateContainerError and is
	// retried by the kubelet with increasing backoff.
	StartupGateDisabled StartupGatePolicy = "Disabled"
)

// ProfileBindingStatus defines the observed state of ProfileBinding.
type ProfileBindingStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the ProfileBinding resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ProfileBinding is the Schema for the profilebindings API
type ProfileBinding struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ProfileBinding
	// +required
	Spec ProfileBindingSpec `json:"spec"`

	// status defines the observed state of ProfileBinding
	// +optional
	Status ProfileBindingStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ProfileBindingList contains a list of ProfileBinding
type ProfileBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ProfileBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ProfileBinding{}, &ProfileBindingList{})
		return nil
	})
}
