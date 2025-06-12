package compositions

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	types "resource-tree-handler/apis"
	filtershelper "resource-tree-handler/internal/helpers/kube/filters"
)

func SetCompositionReferenceStatus(compositionObj *unstructured.Unstructured, compositionReference types.Reference, resourceTree *types.ResourceTree, dynClient *dynamic.DynamicClient) error {
	_, unstructuredCompositionReference, err := filtershelper.GetCompositionReference(dynClient, compositionReference)
	if err != nil {
		return fmt.Errorf("could not obtain compositionReference: %v", err)
	}

	isReady, message := IsCompositionReady(resourceTree)
	log.Info().Msgf("Composition %s status %t", compositionReference.Name, isReady)
	status := cases.Title(language.English, cases.NoLower).String(strconv.FormatBool(isReady))

	reason := "Available"
	if !isReady {
		reason = "Degraded"
	}

	// THIS IS PROBABLY WRONG, HOWEVER, IT'S LIKE THIS FOR THE FRONTEND
	// status and reason are inverted on purpose
	unstructured.SetNestedSlice(unstructuredCompositionReference.Object, []interface{}{
		map[string]interface{}{
			"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
			"message":            message,
			"reason":             status,
			"status":             reason,
			"type":               "CompositionStatus",
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
	_, compositionReference_referenceJsonStatus, err := GetObjectStatus(dynClient, *compositionReference_reference, types.Reference{}, &types.ResourceNodeStatus{})
	if err != nil {
		return fmt.Errorf("could not obtain CompositionReference status while building resource tree: %w", err)
	}

	resourceTree.RootElementStatus = compositionReference_referenceJsonStatus

	return nil

}

func IsCompositionReady(resourceTree *types.ResourceTree) (bool, string) {
	log.Info().Msg("Checking composition status...")
	logging := "\n"
	positives := []string{
		"", "ready", "complete", "healthy", "active", "able",
	}
	for _, status := range resourceTree.Resources.Status {
		if status.Kind == "CompositionReference" {
			continue
		}
		for _, health := range *status.Health {
			logging += fmt.Sprintf("resource %s health type %s value %s\n", status.Kind, health.Type, health.Status)
			if has(positives, health.Type) {
				if strings.ToLower(health.Status) != "true" && health.Type != "" {
					log.Debug().Msg(logging)
					log.Warn().Msgf("Object not positive >> Kind: %s - Name: %s - Namespace: %s - Message: %s", status.Kind, status.Name, status.Namespace, health.Message)
					return false, fmt.Sprintf("Kind: %s - Name: %s - Namespace: %s - Message: %s", status.Kind, status.Name, status.Namespace, health.Message)
				}
			}
		}
	}
	log.Debug().Msg(logging)
	return true, ""
}

func has(s []string, str string) bool {
	for _, v := range s {
		if strings.Contains(strings.ToLower(str), v) {
			return true
		}
	}

	return false
}
