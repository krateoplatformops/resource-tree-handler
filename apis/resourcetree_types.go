package apis

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type ResourceTree struct {
	CompositionId     string              `json:"compositionId"`
	RootElementStatus *ResourceNodeStatus `json:"rootElementStatus"`
	Resources         ResourceTreeJson    `json:"resources"`
}

type ResourceNode struct {
	ResourceRef `json:",inline"`
	ParentRefs  []Reference `json:"parentRefs,omitempty"`
}

type Reference struct {
	ApiVersion string `json:"apiVersion"`
	Resource   string `json:"resource"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Kind       string `json:"kind"`
}

type Health struct {
	Status  string `json:"status,omitempty"`
	Type    string `json:"type,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type ResourceRef struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Resource   string `json:"resource,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
}

type ResourceRefStatus struct {
	Version   string `json:"version,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

type ResourceNodeStatus struct {
	ResourceRefStatus `json:",inline"`
	ParentRefs        []*ResourceNodeStatus `json:"parentRefs,omitempty"`
	UID               *string               `json:"uid,omitempty"`
	ResourceVersion   *string               `json:"resourceVersion,omitempty"`
	Health            *[]Health             `json:"health,omitempty"`
	CreatedAt         *metav1.Time          `json:"createdAt,omitempty"`
}

type ResourceTreeSpec struct {
	Tree []ResourceNode `json:"tree,omitempty"`
}

type ResourceTreeJson struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceTreeSpec      `json:"spec,omitempty"`
	Status []*ResourceNodeStatus `json:"status,omitempty"`
}
