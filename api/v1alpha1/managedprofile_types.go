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
	k8s "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ManagedProfileSpec defines the desired state of ManagedProfile
type ManagedProfileSpec struct {
	// PromotionPolicyRef is a reference to the PromotionPolicy resource that defines the promotion lifecycle for this ManagedProfile.
	// +optional
	// +kubebuilder:validation:AtLeastOneOf=PromotionPolicyRef,DemotionPolicyRef
	PromotionPolicyRef *PolicyReferenceObject `json:"promotionPolicyRef,omitempty"`

	// DemotionPolicyRef is a reference to the DemotionPolicy resource that defines the demotion lifecycle for this ManagedProfile.
	// +optional
	// +kubebuilder:validation:AtLeastOneOf=PromotionPolicyRef,DemotionPolicyRef
	DemotionPolicyRef *PolicyReferenceObject `json:"demotionPolicyRef,omitempty"`

	// GitOptions defines the Git repository and branch to use for this ManagedProfile.
	// +optional
	// +kubebuilder:validation:AtLeastOneOf=GitOptions,ImageBuilderOptions
	GitOptions *GitOptions `json:"gitOptions,omitempty"`

	// ImageBuilderOptions defines the image builder options to use to use to
	// reincorperaote changes of the managed profile into the VM image.
	// not implimented yet
	// +optional
	// +kubebuilder:validation:AtLeastOneOf=GitOptions,ImageBuilderOptions
	// ImageBuilderOptions *ImageBuilderOptions `json:"imageBuilderOptions,omitempty"`
}

type PolicyReferenceObject struct {
	// APIVersion is the API version of the PromotionPolicy resource.
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind is the kind of the PromotionPolicy resource.
	// +optional
	Kind string `json:"kind"`
	// Name is the name of the PromotionPolicy resource.
	// The policy must be in the same namespace as the ManagedProfile: a cross-namespace
	// reference would let one namespace promote or demote another namespace's profile.
	// +required
	Name string `json:"name"`
}

type GitOptions struct {
	// OpenPR indicates whether to open a pull request for changes to the ManagedProfile.
	// +optional
	OpenPR bool `json:"openPR,omitempty"`
	// PRTitle is the title of the pull request to open for changes to the ManagedProfile.
	// +optional
	PRTitle string `json:"prTitle,omitempty"`
	// PRBody is the body of the pull request to open for changes to the ManagedProfile.
	// +optional
	PRBody string `json:"prBody,omitempty"`
	// RepositorySecret is a reference to a Kubernetes Secret that contains the credentials, url, and other information for accessing the Git repository.
	// We use LocalObjectReference here to avoid the need for a full Secret reference, since we want to restrict to secrets in the same namespace as the ManagedProfile.
	// +required
	RepositorySecret k8s.LocalObjectReference `json:"repositorySecret,omitempty"`
}

// ManagedProfileStatus defines the observed state of ManagedProfile.
type ManagedProfileStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the ManagedProfile resource.
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

	// The profile is installed on the target system and is ready for use by consumers.
	// +optional
	Available string `json:"available,omitempty"`

	// profile is in the process of being installed on the target system.
	// either the profile is being installed for the first time, or it is being reinstalled due to a change in the profile.
	// +optional
	Installing string `json:"processing,omitempty"`

	// profile has passed the promotion threshold and is in the pocess of being promoted and commited back to the source repository.
	// +optional
	Promoting string `json:"promoting,omitempty"`

	// profile has passed the demotion threshold and is in the process of being demoted and removed from the target system.
	// +optional
	Demoting string `json:"demoting,omitempty"`

	// EnforcementLevel indicates that the enforcement level of the ManagedProfile is set to "Enforce" or "Inform".
	// +optional
	EnforcementLevel string `json:"enforcementLevel,omitempty"`

	// Probation indicates whether the profile is on probation after a promotion,
	// as defined by the probation of its DemotionPolicy. It is empty when the profile
	// has not been promoted or has no DemotionPolicy.
	// +optional
	Probation ProbationState `json:"probation,omitempty"`
}

// ProbationState describes where a promoted profile is in its probation.
// +kubebuilder:validation:Enum=Active;Completed
type ProbationState string

const (
	// ProbationActive means the profile is on probation: a matching demotion rule
	// demotes it automatically.
	ProbationActive ProbationState = "Active"

	// ProbationCompleted means the profile has left probation: a matching demotion
	// rule triggers the DemotionPolicy's afterProbation policy.
	ProbationCompleted ProbationState = "Completed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ManagedProfile is the Schema for the managedprofiles API
type ManagedProfile struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ManagedProfile
	// +required
	Spec ManagedProfileSpec `json:"spec"`

	// status defines the observed state of ManagedProfile
	// +optional
	Status ManagedProfileStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ManagedProfileList contains a list of ManagedProfile
type ManagedProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ManagedProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ManagedProfile{}, &ManagedProfileList{})
		return nil
	})
}
