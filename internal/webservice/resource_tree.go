package webservice

import (
	"bytes"
	"fmt"
	"text/template"
	"time"
)

type ResourceTree struct {
	CompositionId string     `json:"compositionId"`
	Resources     []Resource `json:"resources"`
}

type Resource struct {
	APIVersion string      `json:"apiVersion"`
	Resource   string      `json:"resource"`
	Name       string      `json:"name"`
	Namespace  string      `json:"namespace,omitempty"`
	ParentRefs []ParentRef `json:"parentRefs,omitempty"`
	Status     Status      `json:"status,omitempty"`
}

type ParentRef struct {
	APIVersion string `json:"apiVersion"`
	Resource   string `json:"resource"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
}

type Status struct {
	CreatedAt       time.Time `json:"createdAt"`
	ResourceVersion string    `json:"resourceVersion"`
	UID             string    `json:"uid"`
	Health          Health    `json:"health,omitempty"`
}

type Health struct {
	Status  string `json:"status,omitempty"`
	Type    string `json:"type,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

func createResourceTreeString(resources []Resource) (string, error) {
	tmpl := `
apiVersion: resourcetrees.krateo.io/v1alpha1
kind: ResourceTree
metadata:
  name: composition-resourcetree-{{ .Release.Name }}
  namespace: {{ .Release.Namespace }}
spec:
  tree:
{{- range .Resources }}
  - apiVersion: {{ .APIVersion }}
    resource: {{ .Resource }}
    name: {{ .Name }}
    {{- if .Namespace }}
    namespace: {{ .Namespace }}
    {{- end }}
    {{- if .ParentRefs }}
    parentRefs:
    {{- range .ParentRefs }}
    - apiVersion: {{ .APIVersion }}
      resource: {{ .Resource }}
      name: {{ .Name }}
      {{- if .Namespace }}
      namespace: {{ .Namespace }}
      {{- end }}
    {{- end }}
    {{- end }}
{{- end }}
status:
{{- range .Resources }}
- createdAt: "{{ .Status.CreatedAt.Format "2006-01-02T15:04:05Z07:00" }}"
  {{- if .Status.Health }}
  health:
    {{- if .Status.Health.Status }}
    status: "{{ .Status.Health.Status }}"
    {{- end }}
    {{- if .Status.Health.Type }}
    type: {{ .Status.Health.Type }}
    {{- end }}
    {{- if .Status.Health.Reason }}
    reason: {{ .Status.Health.Reason }}
    {{- end }}
    {{- if .Status.Health.Message }}
    message: {{ .Status.Health.Message }}
    {{- end }}
  {{- end }}
  kind: {{ .Resource }}
  name: {{ .Name }}
  {{- if .Namespace }}
  namespace: {{ .Namespace }}
  {{- end }}
  {{- if .ParentRefs }}
  parentRefs:
  {{- range .ParentRefs }}
  - apiVersion: {{ .APIVersion }}
    resource: {{ .Resource }}
    name: {{ .Name }}
    {{- if .Namespace }}
    namespace: {{ .Namespace }}
    {{- end }}
  {{- end }}
  {{- end }}
  resourceVersion: "{{ .Status.ResourceVersion }}"
  uid: {{ .Status.UID }}
  version: {{ .APIVersion }}
{{- end }}
`

	t, err := template.New("resourcetree").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]interface{}{
		"Resources": resources,
		"Release": map[string]string{
			"Name":      "{{ .Release.Name }}",
			"Namespace": "{{ .Release.Namespace }}",
		},
	})
	if err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}
