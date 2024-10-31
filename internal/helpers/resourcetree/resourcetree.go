package resourcetree

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	types "resource-tree-handler/apis"
	cacheHelper "resource-tree-handler/internal/cache"
	kubeHelper "resource-tree-handler/internal/helpers/kube/client"
	compositionHelper "resource-tree-handler/internal/helpers/kube/compositions"
	filtersHelper "resource-tree-handler/internal/helpers/kube/filters"
)

func GetUidByCompositionReference(composition *types.Reference, cacheObj *cacheHelper.ThreadSafeCache) string {
	keys := cacheObj.ListKeysFromCache()
	for _, compositionId := range keys {
		resourceTree, ok := cacheObj.GetResourceTreeFromCache(compositionId)
		if !ok {
			return ""
		}
		if resourceTree.CompositionReference.ApiVersion == composition.ApiVersion &&
			resourceTree.CompositionReference.Resource == composition.Resource &&
			resourceTree.CompositionReference.Namespace == composition.Namespace &&
			resourceTree.CompositionReference.Name == composition.Name {
			return compositionId
		}
	}
	return ""
}

func HandleCreate(obj *unstructured.Unstructured, composition types.Reference, cacheObj *cacheHelper.ThreadSafeCache, dynClient *dynamic.DynamicClient) error {
	exclude := filtersHelper.GetFilters(dynClient, composition)
	resourceTree, err := compositionHelper.GetCompositionResourcesStatus(dynClient, obj, composition, exclude)
	if err != nil {
		log.Error().Err(err).Msg("retrieving managed array statuses")
		return fmt.Errorf("error while retrieving managed array statuses: %w", err)
	}

	err = compositionHelper.SetCompositionReferenceStatus(obj, composition, &resourceTree, dynClient)
	if err != nil {
		return fmt.Errorf("error while updating the composition status for composition id %s: %v", string(obj.GetUID()), err)
	}

	cacheObj.AddToCache(resourceTree, string(obj.GetUID()), composition, types.Filters{Exclude: exclude})
	return nil
}

func HandleUpdate(newObjectReference types.Reference, newObjectKind string, compositionId string, cacheObj *cacheHelper.ThreadSafeCache, dynClient *dynamic.DynamicClient) {
	resourceTree, ok := cacheObj.GetResourceTreeFromCache(compositionId)
	if !ok {
		log.Error().Msgf("resource tree for composition id %s not found", compositionId)
		return
	}

	log.Info().Msgf("Update event for object %s %s %s %s in composition_id %s", newObjectReference.ApiVersion, newObjectReference.Resource, newObjectReference.Name, newObjectReference.Namespace, compositionId)

	// Get the resource tree root element: CompositionReference, through labels
	_, unstructuredCompositionReference, err := filtersHelper.GetCompositionReference(dynClient, resourceTree.CompositionReference)
	if err != nil {
		log.Error().Err(err).Msg("could not obtain CompositionReference while building resource tree")
		return
	}
	compositionReference_reference := types.Reference{
		ApiVersion: "resourcetrees.krateo.io/v1",
		Kind:       "CompositionReference",
		Resource:   "compositionreferences",
		Name:       unstructuredCompositionReference.GetName(),
		Namespace:  unstructuredCompositionReference.GetNamespace(),
	}

	resourceNodeJsonSpec, resourceNodeJsonStatus, err := compositionHelper.GetObjectStatus(dynClient, newObjectReference, resourceTree.CompositionReference, compositionReference_reference, resourceTree.ResourceTree.RootElementStatus)
	if err != nil {
		log.Error().Err(err).Msg("error retrieving object status")
		return
	}
	found := false
	for i, obj := range resourceTree.ResourceTree.Resources.Spec.Tree {
		if obj.APIVersion == newObjectReference.ApiVersion && obj.Resource == newObjectReference.Resource && obj.Name == newObjectReference.Name && obj.Namespace == newObjectReference.Namespace {
			resourceNodeJsonSpec.ParentRefs = obj.ParentRefs

			// Delete old object from spec array
			resourceTree.ResourceTree.Resources.Spec.Tree = append(resourceTree.ResourceTree.Resources.Spec.Tree[:i], resourceTree.ResourceTree.Resources.Spec.Tree[i+1:]...)
			// Append new object to spec array
			resourceTree.ResourceTree.Resources.Spec.Tree = append(resourceTree.ResourceTree.Resources.Spec.Tree, resourceNodeJsonSpec)

			found = true
		}
	}
	if !found {
		resourceTree.ResourceTree.Resources.Spec.Tree = append(resourceTree.ResourceTree.Resources.Spec.Tree, resourceNodeJsonSpec)
	}

	found = false
	for i, obj := range resourceTree.ResourceTree.Resources.Status {
		if obj.Kind == newObjectKind && obj.Version == newObjectReference.ApiVersion && obj.Name == newObjectReference.Name && obj.Namespace == newObjectReference.Namespace {
			resourceNodeJsonStatus.ParentRefs = obj.ParentRefs

			// Delete old object from status array
			resourceTree.ResourceTree.Resources.Status = append(resourceTree.ResourceTree.Resources.Status[:i], resourceTree.ResourceTree.Resources.Status[i+1:]...)
			// Append new object to status array
			resourceTree.ResourceTree.Resources.Status = append(resourceTree.ResourceTree.Resources.Status, resourceNodeJsonStatus)

			found = true
		}
	}
	if !found {
		resourceTree.ResourceTree.Resources.Status = append(resourceTree.ResourceTree.Resources.Status, resourceNodeJsonStatus)
	}

	cacheObj.UpdateCacheEntry(resourceTree.ResourceTree, compositionId, resourceTree.CompositionReference)

	compositionUnstructured, err := kubeHelper.GetObj(context.Background(), &resourceTree.CompositionReference, dynClient)
	if err != nil {
		log.Error().Err(err).Msg("retrieving object, could not update composition status")
		return
	}

	err = compositionHelper.SetCompositionReferenceStatus(compositionUnstructured, resourceTree.CompositionReference, &resourceTree.ResourceTree, dynClient)
	if err != nil {
		log.Error().Err(err).Msgf("error while updating the composition status for composition id %s (update)", compositionId)
	}
}
