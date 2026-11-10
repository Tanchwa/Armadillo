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

// PromotionPolicySpec defines the desired state of PromotionPolicy
type PromotionPolicySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// PromotionRules is a list of promotion rules that define the promotion policy
	// Proting referes to promotion from Apparmor complain mode to Apparmor enforce mode, or from a lower security level to a higher security level
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	PromotionRules []PromotionRule `json:"promotionRules,omitempty"`
}

type PromotionRule struct {
	// requirements are ANDed. Rules in promotionRules are ORed.
	// +required
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	Requirements []PromotionRequirement `json:"requirements,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!(self.operator in ['Gt','Lt']) || (size(self.values) == 1 && self.values[0].matches('^[0-9]+$'))",message="Gt and Lt take exactly one integer value"
// +kubebuilder:validation:XValidation:rule="!(self.operator in ['In','NotIn']) || size(self.values) > 0",message="In and NotIn take at least one value"
// +kubebuilder:validation:XValidation:rule="!(self.operator in ['Exists','DoesNotExist']) || !has(self.values)",message="Exists and DoesNotExist take no values"
type PromotionRequirement struct {
	// +required
	Key PromotionKey `json:"key,omitempty"`
	// +required
	Operator PromotionOperator `json:"operator,omitempty"`
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:items:MaxLength=63
	Values []string `json:"values,omitempty"`
}

// +kubebuilder:validation:Enum=DeploysObserved;RestartsObserved;RollbacksObserved;UpstreamErrorsObserved;CertRotationsObserved;QuietBusinessCycles;RouteCoveragePercent
type PromotionKey string

// +kubebuilder:validation:Enum=In;NotIn;Exists;DoesNotExist;Gt;Lt
type PromotionOperator string

type ApplyBehavior struct {
	// +optional
}

// PromotionPolicyStatus defines the observed state of PromotionPolicy.
type PromotionPolicyStatus struct {
	// PromotionPolicy object is similar to ConfigMap as it's a difition of actions and not an object
	// therefore there is no need to have conditions for this object
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PromotionPolicy is the Schema for the promotionpolicies API
type PromotionPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of PromotionPolicy
	// +required
	Spec PromotionPolicySpec `json:"spec"`

	// status defines the observed state of PromotionPolicy
	// +optional
	Status PromotionPolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// PromotionPolicyList contains a list of PromotionPolicy
type PromotionPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []PromotionPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &PromotionPolicy{}, &PromotionPolicyList{})
		return nil
	})
}
