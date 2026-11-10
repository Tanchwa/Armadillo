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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ProfileNodeStatusSpec defines the desired state of ProfileNodeStatus
// One ProfileNodeStatus exists per ManagedProfile per node.
type ProfileNodeStatusSpec struct {
	// NodeName is the node this status reports on.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="nodeName is immutable"
	NodeName string `json:"nodeName,omitempty"`

	// ProfileRef is the ManagedProfile this status reports on.
	// +required
	ProfileRef ManagedProfileReference `json:"profileRef,omitzero"`
}

// ManagedProfileReference points at a namespaced ManagedProfile from a cluster-scoped object.
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="profileRef is immutable"
type ManagedProfileReference struct {
	// Namespace of the ManagedProfile.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`
	// Name of the ManagedProfile.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// +kubebuilder:validation:Enum=Complain;Enforce
type ProfileMode string

// ProfileNodeStatusStatus defines the observed state of ProfileNodeStatus.
type ProfileNodeStatusStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the ProfileNodeStatus resource.
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

	// LoadedHash is the content hash of the profile actually loaded on the node.
	// Profiles load by name, so a node can hold a stale profile under the right name;
	// comparing this to the ManagedProfile's hash is how that is caught.
	// +optional
	// +kubebuilder:validation:MaxLength=128
	LoadedHash string `json:"loadedHash,omitempty"`

	// Mode is the AppArmor mode the profile is loaded in on the node.
	// +optional
	Mode ProfileMode `json:"mode,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ProfileNodeStatus is the Schema for the profilenodestatuses API
type ProfileNodeStatus struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ProfileNodeStatus
	// +required
	Spec ProfileNodeStatusSpec `json:"spec"`

	// status defines the observed state of ProfileNodeStatus
	// +optional
	Status ProfileNodeStatusStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ProfileNodeStatusList contains a list of ProfileNodeStatus
type ProfileNodeStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ProfileNodeStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ProfileNodeStatus{}, &ProfileNodeStatusList{})
		return nil
	})
}
