package compositions

import (
	"context"
	"fmt"
	"resource-tree-handler/apis"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

func SetCompositionStatus(compositionObj *unstructured.Unstructured, compositionReference apis.Reference, resourceTree *apis.ResourceTree, dynClient *dynamic.DynamicClient) error {
	if compositionReference.Kind == "" {
		return fmt.Errorf("compositionReference does not contain Kind field")
	}
	isReady := IsCompositionReady(resourceTree)
	status := cases.Title(language.English, cases.Compact).String(strconv.FormatBool(isReady))

	unstructured.SetNestedSlice(compositionObj.Object, []interface{}{
		map[string]interface{}{
			"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
			"message":            "",
			"reason":             "Available",
			"status":             status,
			"type":               "Ready",
		},
	}, "status", "conditions")

	gv, err := schema.ParseGroupVersion(compositionReference.ApiVersion)
	if err != nil {
		return fmt.Errorf("unable to parse GroupVersion from reference ApiVersion: %v", err)
	}
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: compositionReference.Resource,
	}

	_, err = dynClient.Resource(gvr).
		Namespace(compositionReference.Namespace).
		UpdateStatus(context.Background(), compositionObj, v1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("there was an error updating the composition status for the composition id %s: %v", compositionObj.GetUID(), err)
	}

	return nil

}

func IsCompositionReady(resourceTree *apis.ResourceTree) bool {
	positives := []string{
		"", "ready", "complete", "healthy", "active", "able",
	}
	for _, status := range resourceTree.Resources.Status {
		if !has(positives, status.Health.Type) {
			return false
		}
	}
	return true
}

func has(s []string, str string) bool {
	for _, v := range s {
		if strings.Contains(strings.ToLower(str), v) {
			return true
		}
	}

	return false
}
