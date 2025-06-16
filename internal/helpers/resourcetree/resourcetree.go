package resourcetree

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"

	types "resource-tree-handler/apis"
	cacheHelper "resource-tree-handler/internal/cache"
	kubeHelper "resource-tree-handler/internal/helpers/kube/client"
	compositionHelper "resource-tree-handler/internal/helpers/kube/compositions"
	filtersHelper "resource-tree-handler/internal/helpers/kube/filters"
)

func HandleCreate(obj *unstructured.Unstructured, composition types.Reference, cacheObj *cacheHelper.ThreadSafeCache, config *rest.Config) error {
	dynClient, err := kubeHelper.NewDynamicClient(config)
	if err != nil {
		return fmt.Errorf("obtaining dynamic client for kubernetes: %w", err)
	}

	exclude := filtersHelper.GetFilters(config, composition)
	resourceTree, err := compositionHelper.GetCompositionResourcesStatus(config, obj, composition, exclude)
	if err != nil {
		log.Error().Err(err).Msg("retrieving managed array statuses")
		return fmt.Errorf("error while retrieving managed array statuses: %w", err)
	}

	err = compositionHelper.SetCompositionReferenceStatus(obj, composition, &resourceTree, dynClient)
	if err != nil {
		return fmt.Errorf("error while updating the composition status for composition id %s: %v", string(obj.GetUID()), err)
	}

	cacheObj.AddToCache(resourceTree, string(obj.GetUID()), composition, types.Filters{Exclude: exclude})
	log.Info().Msgf("Resource tree for composition_id %s cached and ready", obj.GetUID())
	return nil
}

func HandleUpdate(newObjectReference types.Reference, newObjectKind string, compositionId string, cacheObj *cacheHelper.ThreadSafeCache, config *rest.Config) {
	dynClient, err := kubeHelper.NewDynamicClient(config)
	if err != nil {
		log.Error().Err(err).Msgf("obtaining dynamic client for kubernetes")
		return
	}

	updateOp := func(resourceTree *cacheHelper.ResourceTreeUpdate) error {
		// Get the resource tree root element: CompositionReference, through labels
		_, unstructuredCompositionReference, err := filtersHelper.GetCompositionReference(dynClient, resourceTree.CompositionReference)
		if err != nil {
			return fmt.Errorf("could not obtain CompositionReference while building resource tree: %w", err)
		}

		compositionReference_reference := types.Reference{
			ApiVersion: "resourcetrees.krateo.io/v1",
			Kind:       "CompositionReference",
			Resource:   "compositionreferences",
			Name:       unstructuredCompositionReference.GetName(),
			Namespace:  unstructuredCompositionReference.GetNamespace(),
		}

		resourceNodeJsonSpec, resourceNodeJsonStatus, err := compositionHelper.GetObjectStatus(dynClient, newObjectReference, compositionReference_reference, resourceTree.ResourceTree.RootElementStatus)
		if err != nil {
			return fmt.Errorf("error retrieving object status: %w", err)
		}

		// Update spec
		found := false
		for i, obj := range resourceTree.ResourceTree.Resources.Spec.Tree {
			if obj.APIVersion == newObjectReference.ApiVersion &&
				obj.Resource == newObjectReference.Resource &&
				obj.Name == newObjectReference.Name &&
				obj.Namespace == newObjectReference.Namespace {

				resourceNodeJsonSpec.ParentRefs = obj.ParentRefs
				resourceTree.ResourceTree.Resources.Spec.Tree = append(
					resourceTree.ResourceTree.Resources.Spec.Tree[:i],
					append([]types.ResourceNode{resourceNodeJsonSpec},
						resourceTree.ResourceTree.Resources.Spec.Tree[i+1:]...)...)
				found = true
				break
			}
		}
		if !found {
			resourceTree.ResourceTree.Resources.Spec.Tree = append(
				resourceTree.ResourceTree.Resources.Spec.Tree,
				resourceNodeJsonSpec)
			log.Info().Msgf("Object missing in data spec, adding object %s %s %s %s in composition_id %s", newObjectReference.ApiVersion, newObjectReference.Resource, newObjectReference.Name, newObjectReference.Namespace, compositionId)
		}

		// Update status (similar pattern)
		found = false
		for i, obj := range resourceTree.ResourceTree.Resources.Status {
			if obj.Kind == newObjectKind &&
				obj.Version == newObjectReference.ApiVersion &&
				obj.Name == newObjectReference.Name &&
				obj.Namespace == newObjectReference.Namespace {

				resourceNodeJsonStatus.ParentRefs = obj.ParentRefs
				resourceTree.ResourceTree.Resources.Status = append(
					resourceTree.ResourceTree.Resources.Status[:i],
					append([]*types.ResourceNodeStatus{resourceNodeJsonStatus},
						resourceTree.ResourceTree.Resources.Status[i+1:]...)...)
				found = true
				break
			}
		}
		if !found {
			resourceTree.ResourceTree.Resources.Status = append(
				resourceTree.ResourceTree.Resources.Status,
				resourceNodeJsonStatus)
			log.Info().Msgf("Object missing in data status, adding object %s %s %s %s in composition_id %s", newObjectReference.ApiVersion, newObjectReference.Resource, newObjectReference.Name, newObjectReference.Namespace, compositionId)
		}

		for _, obj := range resourceTree.ResourceTree.Resources.Status {
			log.Debug().Msgf("objects in resource tree status %s %s %s %s for composition_id %s", obj.Version, obj.Kind, obj.Name, obj.Namespace, compositionId)
		}

		// Update composition status
		compositionUnstructured, err := kubeHelper.GetObj(context.Background(), &resourceTree.CompositionReference, config)
		if err != nil {
			return fmt.Errorf("retrieving object, could not update composition status: %w", err)
		}

		err = compositionHelper.SetCompositionReferenceStatus(compositionUnstructured, resourceTree.CompositionReference, &resourceTree.ResourceTree, dynClient)
		if err != nil {
			return fmt.Errorf("error while updating the composition status: %w", err)
		}

		return nil
	}

	if err := cacheObj.QueueUpdate(compositionId, updateOp); err != nil {
		log.Error().Err(err).Msgf("failed to update resource tree for composition id %s", compositionId)
	}
}
