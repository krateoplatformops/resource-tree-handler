package resourcetree

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	types "resource-tree-handler/apis"
	cacheHelper "resource-tree-handler/internal/cache"
	compositionHelper "resource-tree-handler/internal/helpers/kube/compositions"
	filtersHelper "resource-tree-handler/internal/helpers/kube/filters"
)

func HandleDelete(c *gin.Context, compositionId string) error {
	cacheHelper.DeleteFromCache(compositionId)
	c.String(http.StatusOK, "DELETE for CompositionId %s executed", compositionId)
	return nil
}

func HandleCreate(obj *unstructured.Unstructured, composition types.Reference, dynClient *dynamic.DynamicClient) error {
	exclude := filtersHelper.Get(dynClient, composition)
	resourceTree, err := compositionHelper.GetCompositionResourcesStatus(dynClient, obj, composition, exclude)
	if err != nil {
		log.Error().Err(err).Msg("retrieving managed array statuses")
		return fmt.Errorf("error while retrieving managed array statuses: %w", err)
	}

	resourceTreeJsonStatus, err := json.Marshal(resourceTree.Resources.Status)
	if err != nil {
		log.Error().Err(err).Msg("error marshaling resource tree into JSON")
		return fmt.Errorf("error marshaling resource tree into JSON: %w", err)
	}

	cacheHelper.AddToCache(string(resourceTreeJsonStatus), resourceTree, string(obj.GetUID()), composition, types.Filters{Exclude: exclude})
	return nil
}

func HandleUpdate(newObjectReference types.Reference, newObjectKind string, compositionId string, dynClient *dynamic.DynamicClient) {
	resourceTree, ok := cacheHelper.GetResourceTreeFromCache(compositionId)
	if !ok {
		log.Error().Msgf("resource tree for composition id %s not found", compositionId)
		return
	}

	log.Info().Msgf("Update event for object %s %s %s %s in composition_id %s", newObjectReference.ApiVersion, newObjectReference.Resource, newObjectReference.Name, newObjectReference.Namespace, compositionId)

	resourceNodeJsonSpec, resourceNodeJsonStatus, err := compositionHelper.GetObjectStatus(dynClient, newObjectReference, resourceTree.CompositionReference)
	if err != nil {
		log.Error().Err(err).Msg("error retrieving object status")
		return
	}
	for i, obj := range resourceTree.ResourceTree.Resources.Spec.Tree {
		if obj.APIVersion == newObjectReference.ApiVersion && obj.Resource == newObjectReference.Resource && obj.Name == newObjectReference.Name && obj.Namespace == newObjectReference.Namespace {
			resourceNodeJsonSpec.ParentRefs = obj.ParentRefs
		}
		// Delete old object from spec array
		resourceTree.ResourceTree.Resources.Spec.Tree = append(resourceTree.ResourceTree.Resources.Spec.Tree[:i], resourceTree.ResourceTree.Resources.Spec.Tree[i+1:]...)
		// Append new object to spec array
		resourceTree.ResourceTree.Resources.Spec.Tree = append(resourceTree.ResourceTree.Resources.Spec.Tree, resourceNodeJsonSpec)
	}
	for i, obj := range resourceTree.ResourceTree.Resources.Status {
		if obj.Kind == newObjectKind && obj.Version == newObjectReference.ApiVersion && obj.Name == newObjectReference.Name && obj.Namespace == newObjectReference.Namespace {
			resourceNodeJsonStatus.ParentRefs = obj.ParentRefs
		}
		// Delete old object from status array
		resourceTree.ResourceTree.Resources.Status = append(resourceTree.ResourceTree.Resources.Status[:i], resourceTree.ResourceTree.Resources.Status[i+1:]...)
		// Append new object to status array
		resourceTree.ResourceTree.Resources.Status = append(resourceTree.ResourceTree.Resources.Status, &resourceNodeJsonStatus)
	}

	resourceTreeJsonStatus, err := json.Marshal(resourceTree.ResourceTree.Resources.Status)
	if err != nil {
		log.Error().Err(err).Msg("error marshaling resource tree into JSON")
	}

	cacheHelper.UpdateCacheEntry(compositionId, resourceTree.ResourceTree, string(resourceTreeJsonStatus), resourceTree.CompositionReference)
}
