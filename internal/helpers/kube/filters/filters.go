package filters

import (
	"context"
	"fmt"
	types "resource-tree-handler/apis"

	"github.com/rs/zerolog/log"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	kubehelper "resource-tree-handler/internal/helpers/kube/client"
)

const (
	compositionGroup     = "krateo.io/composition-group"
	compositionVersion   = "krateo.io/composition-installed-version"
	compositionResource  = "krateo.io/composition-resource"
	compositionKind      = "krateo.io/composition-kind"
	compositionName      = "krateo.io/composition-name"
	compositionNamespace = "krateo.io/composition-namespace"
	compositionId        = "krateo.io/composition-id"
)

func GetCompositionReference(dynClient *dynamic.DynamicClient, composition types.Reference) (*types.CompositionReference, *unstructured.Unstructured, error) {
	gvr := schema.GroupVersionResource{
		Group:    "resourcetrees.krateo.io",
		Version:  "v1",
		Resource: "compositionreferences",
	}

	gv, err := schema.ParseGroupVersion(composition.ApiVersion)
	if err != nil {
		return &types.CompositionReference{}, &unstructured.Unstructured{}, fmt.Errorf("could not parse group version for composition: %v", err)
	}

	if composition.Resource == "" {
		composition.Resource = kubehelper.InferGroupResource(composition.ApiVersion, composition.Kind).Resource
	}

	labels := fmt.Sprintf(
		"%s=%s,%s=%s",
		compositionId,
		composition.Uid,
		compositionVersion,
		gv.Version,
	)

	listOptions := v1.ListOptions{
		LabelSelector: labels,
	}

	log.Debug().Msgf("filters: looking for labels: %s", labels)

	unstructuredCompositionReference, err := dynClient.Resource(gvr).List(context.TODO(), listOptions)
	if err != nil {
		return &types.CompositionReference{}, &unstructured.Unstructured{}, fmt.Errorf("could not get composition reference for labels %s: %v", labels, err)
	}

	if len(unstructuredCompositionReference.Items) == 0 {
		return &types.CompositionReference{}, &unstructured.Unstructured{}, fmt.Errorf("no composition reference found for labels %s", labels)
	}

	// Get the first item since we expect only one
	item := unstructuredCompositionReference.Items[0]

	compositionRef := &types.CompositionReference{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, compositionRef)
	if err != nil {
		log.Error().Err(err).Msgf("could not convert unstructured to CompositionReference (filters for labels %s)", labels)
		return &types.CompositionReference{}, &unstructured.Unstructured{}, err
	}

	log.Debug().Msgf("Obtained list of filters, with length %d", len(compositionRef.Spec.Filters.Exclude))

	return compositionRef, &item, nil
}

func GetFilters(config *rest.Config, composition types.Reference) []types.Exclude {
	dynClient, err := kubehelper.NewDynamicClient(config)
	if err != nil {
		log.Error().Err(err).Msgf("obtaining dynamic client for kubernetes")
		return []types.Exclude{}
	}

	compositionRef, _, err := GetCompositionReference(dynClient, composition)
	if err != nil {
		log.Error().Err(err).Msgf("error while retrieving filters, could not retrieve composition reference, continuing without filters")
		return []types.Exclude{}
	}

	return compositionRef.Spec.Filters.Exclude
}

func CompareFilters(old types.Filters, new types.Filters) bool {
	matchFound := make([]bool, len(old.Exclude))
	for i, oldExclude := range old.Exclude {
		for _, newExclude := range new.Exclude {
			if oldExclude.ApiVersion == newExclude.ApiVersion && oldExclude.Name == newExclude.Name && oldExclude.Resource == newExclude.Resource {
				matchFound[i] = true
				break // Go to next position in old.Exclude
			}
		}
	}
	for i := range matchFound {
		if !matchFound[i] {
			return false
		}
	}
	return true
}
