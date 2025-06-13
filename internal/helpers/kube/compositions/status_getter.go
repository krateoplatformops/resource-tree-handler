package compositions

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/rs/zerolog/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	types "resource-tree-handler/apis"
	kubehelper "resource-tree-handler/internal/helpers/kube/client"
	filtershelper "resource-tree-handler/internal/helpers/kube/filters"
)

func GetCompositionResourcesStatus(config *rest.Config, obj *unstructured.Unstructured, compositionReference types.Reference, excludes []types.Exclude) (types.ResourceTree, error) {
	dynClient, err := kubehelper.NewDynamicClient(config)
	if err != nil {
		return types.ResourceTree{}, fmt.Errorf("obtaining dynamic client for kubernetes: %w", err)
	}

	// Get the resource tree root element: CompositionReference, through labels
	_, unstructuredCompositionReference, err := filtershelper.GetCompositionReference(dynClient, compositionReference)
	if err != nil {
		return types.ResourceTree{}, fmt.Errorf("could not obtain CompositionReference while building resource tree: %w", err)
	}
	compositionReference_reference := &types.Reference{
		ApiVersion: "resourcetrees.krateo.io/v1",
		Kind:       "CompositionReference",
		Resource:   "compositionreferences",
		Name:       unstructuredCompositionReference.GetName(),
		Namespace:  unstructuredCompositionReference.GetNamespace(),
	}
	compositionReference_referenceJsonSpec, compositionReference_referenceJsonStatus, err := GetObjectStatus(dynClient, *compositionReference_reference, types.Reference{}, &types.ResourceNodeStatus{})
	if err != nil {
		return types.ResourceTree{}, fmt.Errorf("could not obtain CompositionReference status while building resource tree: %w", err)
	}

	// Create data structures
	resourceTreeJson := types.ResourceTreeJson{}
	resourceTreeJson.CreationTimestamp = metav1.Now()

	resourceTreeJson.Spec.Tree = make([]types.ResourceNode, 0)
	resourceTreeJson.Status = make([]*types.ResourceNodeStatus, 0)

	// Assign root element
	resourceTreeJson.Spec.Tree = append(resourceTreeJson.Spec.Tree, compositionReference_referenceJsonSpec)
	resourceTreeJson.Status = append(resourceTreeJson.Status, compositionReference_referenceJsonStatus)

	status, found, err := unstructured.NestedMap(obj.Object, "status")
	if err != nil {
		return types.ResourceTree{}, fmt.Errorf("error accessing 'status' field: %w", err)
	}
	if !found {
		return types.ResourceTree{}, fmt.Errorf("could not find 'status' field in composition object")
	}

	managed, found := status["managed"]
	if !found {
		return types.ResourceTree{}, fmt.Errorf("could not find 'managed' field in composition object")
	}

	var managedResourceList []types.Reference

	// Check if managed is a slice
	managedSlice, ok := managed.([]interface{})
	if !ok {
		return types.ResourceTree{}, fmt.Errorf("'managed' field is not a slice as expected")
	}

	for _, m := range managedSlice {
		if mMap, ok := m.(map[string]interface{}); ok {
			ref := types.Reference{
				ApiVersion: mMap["apiVersion"].(string),
				Resource:   mMap["resource"].(string),
				Name:       mMap["name"].(string),
				Namespace:  mMap["namespace"].(string),
			}
			managedResourceList = append(managedResourceList, ref)
		}
	}

	managedResourceList = append(managedResourceList, compositionReference)

	for _, managedResource := range managedResourceList {
		resourceNodeJsonSpec, resourceNodeJsonStatus, err := GetObjectStatus(dynClient, managedResource, *compositionReference_reference, compositionReference_referenceJsonStatus)
		if err != nil {
			log.Warn().Err(err).Msg("error retrieving object status, continuing...")
			continue
		}

		resourceTreeJson.Spec.Tree = append(resourceTreeJson.Spec.Tree, resourceNodeJsonSpec)
		resourceTreeJson.Status = append(resourceTreeJson.Status, resourceNodeJsonStatus)
	}

	resourceTree := types.ResourceTree{
		CompositionId:     string(obj.GetUID()),
		Resources:         resourceTreeJson,
		RootElementStatus: compositionReference_referenceJsonStatus,
	}
	return resourceTree, nil
}

func GetObjectStatus(dynClient *dynamic.DynamicClient, reference types.Reference, rootSpecReference types.Reference, rootStatusReference *types.ResourceNodeStatus) (types.ResourceNode, *types.ResourceNodeStatus, error) {
	gv, err := schema.ParseGroupVersion(reference.ApiVersion)
	if err != nil {
		return types.ResourceNode{}, &types.ResourceNodeStatus{}, fmt.Errorf("could not parse Group/Version of managed resource: %w", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: reference.Resource,
	}

	unstructuredRes, err := dynClient.Resource(gvr).Namespace(reference.Namespace).Get(context.TODO(), reference.Name, metav1.GetOptions{})
	if err != nil {
		log.Debug().Msgf("error fetching resource status, trying with cluster-scoped %s %s, %s %s, %s %s, %s %s, %s %s, %s %s", "error", err, "group", gvr.Group, "version", gvr.Version, "resource", gvr.Resource, "name", reference.Name, "namespace", reference.Namespace)
		unstructuredRes, err = dynClient.Resource(gvr).Get(context.TODO(), reference.Name, metav1.GetOptions{})
		if err != nil {
			return types.ResourceNode{}, &types.ResourceNodeStatus{}, fmt.Errorf("error fetching resource status %v %s, %s %s, %s %s, %s %s, %s %s, %s %s", "error", err, "group", gvr.Group, "version", gvr.Version, "resource", gvr.Resource, "name", reference.Name, "namespace", "")
		}

	}

	var healths types.Health

	// Extract status if available
	if unstructuredStatus, found, _ := unstructured.NestedMap(unstructuredRes.Object, "status"); found {
		if conditions, ok := unstructuredStatus["conditions"].([]interface{}); ok && len(conditions) > 0 {
			useOnlyReady := false
			readyIndex := -1
			// To represent the object status, use first the Type Ready
			for i := range conditions {
				if conditions[i].(map[string]interface{})["type"] == "Ready" {
					useOnlyReady = true
					readyIndex = i
					break
				}
			}
			if useOnlyReady {
				if value, ok := conditions[readyIndex].(map[string]interface{})["status"]; ok {
					healths.Status = value.(string)
				}
				if value, ok := conditions[readyIndex].(map[string]interface{})["type"]; ok {
					healths.Type = value.(string)
				}
				if value, ok := conditions[readyIndex].(map[string]interface{})["reason"]; ok {
					healths.Reason = value.(string)
				}
				if value, ok := conditions[readyIndex].(map[string]interface{})["message"]; ok {
					healths.Message = value.(string)
				}
				// If the Type Ready is not present, use the most recent condition
			} else {
				latestIndex := -1
				latest := 999999 * time.Hour
				for i, condition := range conditions {
					if conditionTimestamp, err := time.Parse("2006-01-02T15:04:05Z", condition.(map[string]interface{})["lastTransitionTime"].(string)); err == nil {
						if time.Since(conditionTimestamp) < latest {
							latestIndex = i
							latest = time.Since(conditionTimestamp)
						}
					} else {
						log.Warn().Err(err).Msgf("could not parse condition with layout 2006-01-02T15:04:05Z lastTransitionTime: %s", condition.(map[string]interface{})["lastTransitionTime"].(string))
					}
				}
				if latestIndex == -1 {
					log.Warn().Msg("could not find latest condition, using conidition in first position")
					latestIndex = 0
				}
				if value, ok := conditions[latestIndex].(map[string]interface{})["status"]; ok {
					healths.Status = value.(string)
				}
				if value, ok := conditions[latestIndex].(map[string]interface{})["type"]; ok {
					healths.Type = value.(string)
				}
				if value, ok := conditions[latestIndex].(map[string]interface{})["reason"]; ok {
					healths.Reason = value.(string)
				}
				if value, ok := conditions[latestIndex].(map[string]interface{})["message"]; ok {
					healths.Message = value.(string)
				}
			}
		}
	}

	resourceNodeJsonSpec := types.ResourceNode{}
	resourceNodeJsonSpec.APIVersion = reference.ApiVersion
	resourceNodeJsonSpec.Resource = reference.Resource
	resourceNodeJsonSpec.Name = reference.Name
	resourceNodeJsonSpec.Namespace = reference.Namespace
	skipParent := false
	if resourceNodeJsonSpec.Resource == rootSpecReference.Resource &&
		resourceNodeJsonSpec.APIVersion == rootSpecReference.ApiVersion &&
		resourceNodeJsonSpec.Name == rootSpecReference.Name &&
		resourceNodeJsonSpec.Namespace == rootSpecReference.Namespace {
		skipParent = true
	} else {
		resourceNodeJsonSpec.ParentRefs = []types.Reference{rootSpecReference}
	}

	resourceNodeJsonStatus := &types.ResourceNodeStatus{}
	time := unstructuredRes.GetCreationTimestamp()
	resourceNodeJsonStatus.CreatedAt = &time
	resourceNodeJsonStatus.Kind = unstructuredRes.GetKind()
	resourceNodeJsonStatus.Version = unstructuredRes.GetAPIVersion()
	resourceNodeJsonStatus.Name = reference.Name
	resourceNodeJsonStatus.Namespace = reference.Namespace
	resourceNodeJsonStatus.Health = &healths
	uidString := string(unstructuredRes.GetUID())
	resourceNodeJsonStatus.UID = &uidString
	resourceVersionString := unstructuredRes.GetResourceVersion()
	resourceNodeJsonStatus.ResourceVersion = &resourceVersionString
	if !skipParent {
		resourceNodeJsonStatus.ParentRefs = []*types.ResourceNodeStatus{rootStatusReference}
	}

	return resourceNodeJsonSpec, resourceNodeJsonStatus, nil
}
