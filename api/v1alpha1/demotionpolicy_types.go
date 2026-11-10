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

// DemotionPolicySpec defines the desired state of DemotionPolicy
type DemotionPolicySpec struct {
	// Probation defines the period after a promotion during which the profile is demoted
	// automatically, and what happens once that period is over.
	// A denial is both what a too-narrow profile produces and what a blocked attack produces.
	// Demoting automatically forever would let an attacker switch enforcement off by
	// triggering one denial, so automatic demotion is limited to probation.
	// +required
	Probation Probation `json:"probation,omitzero"`

	// DemotionRules is a list of demotion rules that define the demotion policy
	// Demoting refers to moving a profile from AppArmor enforce mode back to complain mode.
	// Demotion applies to every node at once: a denial on one node is evidence the profile
	// is wrong for the workload, not for that node.
	// While the profile is on probation, a matching rule demotes it. After probation,
	// a matching rule triggers probation.afterProbation.
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	DemotionRules []DemotionRule `json:"demotionRules,omitempty"`
}

// Probation defines when a promoted profile is on probation and what follows it.
type Probation struct {
	// Requirements define when the profile is on probation: it is on probation while
	// every requirement holds, and leaves probation the first time one does not.
	// Requirements count events observed since the promotion, never elapsed time,
	// for the same reason promotion is not gated on a silence timer.
	// +required
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	Requirements []ProbationRequirement `json:"requirements,omitempty"`

	// AfterProbation is what happens when a demotion rule matches after probation.
	// +optional
	// +default="Alert"
	AfterProbation AfterProbationPolicy `json:"afterProbation,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!(self.operator in ['Gt','Lt']) || (size(self.values) == 1 && self.values[0].matches('^[0-9]+$'))",message="Gt and Lt take exactly one integer value"
// +kubebuilder:validation:XValidation:rule="!(self.operator in ['In','NotIn']) || size(self.values) > 0",message="In and NotIn take at least one value"
// +kubebuilder:validation:XValidation:rule="!(self.operator in ['Exists','DoesNotExist']) || !has(self.values)",message="Exists and DoesNotExist take no values"
type ProbationRequirement struct {
	// +required
	Key ProbationKey `json:"key,omitempty"`
	// +required
	Operator PromotionOperator `json:"operator,omitempty"`
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:items:MaxLength=63
	Values []string `json:"values,omitempty"`
}

// ProbationKey names a count of events observed since the profile was promoted.
// +kubebuilder:validation:Enum=DeploysSincePromotion;RestartsSincePromotion;BusinessCyclesSincePromotion
type ProbationKey string

const (
	// DeploysSincePromotion counts deploys of the workload observed under the
	// enforcing profile.
	DeploysSincePromotion ProbationKey = "DeploysSincePromotion"

	// RestartsSincePromotion counts restarts of the workload observed under the
	// enforcing profile.
	RestartsSincePromotion ProbationKey = "RestartsSincePromotion"

	// BusinessCyclesSincePromotion counts full business cycles the workload has
	// completed under the enforcing profile.
	BusinessCyclesSincePromotion ProbationKey = "BusinessCyclesSincePromotion"
)

// AfterProbationPolicy describes what happens when a demotion rule matches
// after the profile has left probation.
// Only one of the following policies may be specified.
// If none of the following policies is specified, the default one
// is AlertAfterProbation.
// +kubebuilder:validation:Enum=Alert;Demote
type AfterProbationPolicy string

const (
	// AlertAfterProbation keeps the profile enforcing and reports the denial,
	// leaving demotion to a human.
	AlertAfterProbation AfterProbationPolicy = "Alert"

	// DemoteAfterProbation keeps demoting automatically after probation. Anyone who
	// can trigger a denial in the workload can then switch its enforcement off.
	// Use sparingly.
	DemoteAfterProbation AfterProbationPolicy = "Demote"
)

type DemotionRule struct {
	// requirements are ANDed. Rules in demotionRules are ORed.
	// +required
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	Requirements []DemotionRequirement `json:"requirements,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!(self.operator in ['Gt','Lt']) || (size(self.values) == 1 && self.values[0].matches('^[0-9]+$'))",message="Gt and Lt take exactly one integer value"
// +kubebuilder:validation:XValidation:rule="!(self.operator in ['In','NotIn']) || size(self.values) > 0",message="In and NotIn take at least one value"
// +kubebuilder:validation:XValidation:rule="!(self.operator in ['Exists','DoesNotExist']) || !has(self.values)",message="Exists and DoesNotExist take no values"
type DemotionRequirement struct {
	// +required
	Key DemotionKey `json:"key,omitempty"`
	// +required
	Operator PromotionOperator `json:"operator,omitempty"`
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:items:MaxLength=63
	Values []string `json:"values,omitempty"`
}

// +kubebuilder:validation:Enum=DenialsObserved;NodesWithDenials
type DemotionKey string

// DemotionPolicyStatus defines the observed state of DemotionPolicy.
type DemotionPolicyStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the DemotionPolicy resource.
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

// DemotionPolicy is the Schema for the demotionpolicies API
type DemotionPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of DemotionPolicy
	// +required
	Spec DemotionPolicySpec `json:"spec"`

	// status defines the observed state of DemotionPolicy
	// +optional
	Status DemotionPolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// DemotionPolicyList contains a list of DemotionPolicy
type DemotionPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []DemotionPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &DemotionPolicy{}, &DemotionPolicyList{})
		return nil
	})
}
