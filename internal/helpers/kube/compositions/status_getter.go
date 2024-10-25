package compositions

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/rs/zerolog/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	types "resource-tree-handler/apis"
)

func GetCompositionResourcesStatus(dynClient *dynamic.DynamicClient, obj *unstructured.Unstructured, compositionReference types.Reference, excludes []types.Exclude) (types.ResourceTree, error) {
	resourceTreeJson := types.ResourceTreeJson{}
	resourceTreeJson.CreationTimestamp = metav1.Now()

	resourceTreeJson.Spec.Tree = make([]types.ResourceNode, 0)
	resourceTreeJson.Status = make([]*types.ResourceNodeStatus, 0)

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
		skip := false
		for _, exclude := range excludes {
			if ShouldItSkip(exclude, managedResource) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		resourceNodeJsonSpec, resourceNodeJsonStatus, err := GetObjectStatus(dynClient, managedResource, compositionReference)
		if err != nil {
			log.Warn().Err(err).Msg("error retrieving object status, continuing...")
			continue
		}

		resourceTreeJson.Spec.Tree = append(resourceTreeJson.Spec.Tree, resourceNodeJsonSpec)
		resourceTreeJson.Status = append(resourceTreeJson.Status, &resourceNodeJsonStatus)
	}

	compositionStatus := &types.ResourceNodeStatus{}
	skipValue := -1
	// Find the composition in the resourceTreeJson.Status
	// Copy its pointer and position
	for i, status := range resourceTreeJson.Status {
		if obj.GetKind() == status.Kind && obj.GetAPIVersion() == status.Version {
			skipValue = i
			compositionStatus = status
			break
		}
	}

	// Add the composition status to each resourceTreeJson.Status, except the one of the composition
	for i := range resourceTreeJson.Status {
		if skipValue != i {
			resourceTreeJson.Status[i].ParentRefs = append(resourceTreeJson.Status[i].ParentRefs, compositionStatus)
		}

	}

	resourceTree := types.ResourceTree{
		CompositionId: string(obj.GetUID()),
		Resources:     resourceTreeJson,
	}
	return resourceTree, nil
}

func GetObjectStatus(dynClient *dynamic.DynamicClient, reference types.Reference, compositionReference types.Reference) (types.ResourceNode, types.ResourceNodeStatus, error) {
	gv, err := schema.ParseGroupVersion(reference.ApiVersion)
	if err != nil {
		return types.ResourceNode{}, types.ResourceNodeStatus{}, fmt.Errorf("could not parse Group/Version of managed resource: %w", err)
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
			return types.ResourceNode{}, types.ResourceNodeStatus{}, fmt.Errorf("error fetching resource status %s %s, %s %s, %s %s, %s %s, %s %s, %s %s", "error", err, "group", gvr.Group, "version", gvr.Version, "resource", gvr.Resource, "name", reference.Name, "namespace", "")
		}

	}

	var health types.Health

	// Extract status if available
	if unstructuredStatus, found, _ := unstructured.NestedMap(unstructuredRes.Object, "status"); found {
		if conditions, ok := unstructuredStatus["conditions"].([]interface{}); ok && len(conditions) > 0 {
			lastCondition := conditions[len(conditions)-1].(map[string]interface{})
			if value, ok := lastCondition["status"]; ok {
				health.Status = value.(string)
			}
			if value, ok := lastCondition["type"]; ok {
				health.Type = value.(string)
			}
			if value, ok := lastCondition["reason"]; ok {
				health.Reason = value.(string)
			}
			if value, ok := lastCondition["message"]; ok {
				health.Message = value.(string)
			}
		}
	}

	resourceNodeJsonSpec := types.ResourceNode{}
	resourceNodeJsonSpec.APIVersion = reference.ApiVersion
	resourceNodeJsonSpec.Resource = reference.Resource
	resourceNodeJsonSpec.Name = reference.Name
	resourceNodeJsonSpec.Namespace = reference.Namespace
	resourceNodeJsonSpec.ParentRefs = []types.Reference{compositionReference}

	resourceNodeJsonStatus := types.ResourceNodeStatus{}
	time := unstructuredRes.GetCreationTimestamp()
	resourceNodeJsonStatus.CreatedAt = &time
	resourceNodeJsonStatus.Kind = unstructuredRes.GetKind()
	resourceNodeJsonStatus.Version = unstructuredRes.GetAPIVersion()
	resourceNodeJsonStatus.Name = reference.Name
	resourceNodeJsonStatus.Namespace = reference.Namespace
	resourceNodeJsonStatus.Health = &health
	uidString := string(unstructuredRes.GetUID())
	resourceNodeJsonStatus.UID = &uidString
	resourceVersionString := unstructuredRes.GetResourceVersion()
	resourceNodeJsonStatus.ResourceVersion = &resourceVersionString
	resourceNodeJsonStatus.ParentRefs = []*types.ResourceNodeStatus{}

	return resourceNodeJsonSpec, resourceNodeJsonStatus, nil
}