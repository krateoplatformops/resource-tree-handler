package apis

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CompositionReference struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CompositionReferenceSpec `json:"spec,omitempty"`
}

type CompositionReferenceSpec struct {
	Filters Filters `json:"filters"`
}

type CompositionReferenceStatus struct {
}

type Filters struct {
	Exclude []Exclude `json:"exclude"`
}

type Exclude struct {
	ApiVersion string `json:"apiVersion"`
	Resource   string `json:"resource"`
	Name       string `json:"name"`
}
