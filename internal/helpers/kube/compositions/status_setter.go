package compositions

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	types "resource-tree-handler/apis"
	filtersHelper "resource-tree-handler/internal/helpers/kube/filters"
)

func SetCompositionReferenceStatus(compositionObj *unstructured.Unstructured, compositionReference types.Reference, resourceTree *types.ResourceTree, dynClient *dynamic.DynamicClient) error {
	_, unstructuredCompositionReference, err := filtersHelper.GetCompositionReference(dynClient, compositionReference)
	if err != nil {
		return fmt.Errorf("could not obtain compositionReference: %v", err)
	}

	isReady := IsCompositionReady(resourceTree)
	status := cases.Title(language.English, cases.Compact).String(strconv.FormatBool(isReady))

	unstructured.SetNestedSlice(unstructuredCompositionReference.Object, []interface{}{
		map[string]interface{}{
			"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
			"message":            "",
			"reason":             "Available",
			"status":             status,
			"type":               "Ready",
		},
	}, "status", "conditions")

	gvr := schema.GroupVersionResource{
		Group:    "resourcetrees.krateo.io",
		Version:  "v1",
		Resource: "compositionreferences",
	}

	_, err = dynClient.Resource(gvr).
		Namespace(unstructuredCompositionReference.GetNamespace()).
		UpdateStatus(context.Background(), unstructuredCompositionReference, v1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("there was an error updating the composition status for the composition id %s in compositionreference with labels %s: %v", compositionObj.GetUID(), unstructuredCompositionReference.GetLabels(), err)
	}

	// Retrieve the new object, with the updated status, and update the root element of the tree
	compositionReference_reference := &types.Reference{
		ApiVersion: "resourcetrees.krateo.io/v1",
		Kind:       "CompositionReference",
		Resource:   "compositionreferences",
		Name:       unstructuredCompositionReference.GetName(),
		Namespace:  unstructuredCompositionReference.GetNamespace(),
	}
	_, compositionReference_referenceJsonStatus, err := GetObjectStatus(dynClient, *compositionReference_reference, compositionReference, types.Reference{}, &types.ResourceNodeStatus{})
	if err != nil {
		return fmt.Errorf("could not obtain CompositionReference status while building resource tree: %w", err)
	}

	resourceTree.RootElementStatus = compositionReference_referenceJsonStatus

	return nil

}

func IsCompositionReady(resourceTree *types.ResourceTree) bool {
	positives := []string{
		"", "ready", "complete", "healthy", "active", "able",
	}
	for _, status := range resourceTree.Resources.Status {
		if has(positives, status.Health.Type) {
			if strings.ToLower(status.Health.Status) != "true" {
				return false
			}
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
