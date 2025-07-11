package compositions

import (
	"context"
	"fmt"
	"regexp"
	types "resource-tree-handler/apis"
	"strings"

	"github.com/rs/zerolog/log"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	kubehelper "resource-tree-handler/internal/helpers/kube/client"
	"slices"
)

func isFullMatch(pattern, str string) (bool, error) {
	if !strings.HasSuffix(pattern, "$") {
		pattern = pattern + "$"
	}
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return regex.MatchString(str), nil
}

func ShouldItSkip(exclude types.Exclude, managedResource types.Reference) bool {
	match := []bool{false, false, false}
	// Check ApiGroup
	if exclude.ApiVersion == "" {
		match[0] = true
	} else if exclude.ApiVersion == managedResource.ApiVersion {
		match[0] = true
	} else { // Check if ApiGroup is regex
		fullMatch, _ := isFullMatch(exclude.ApiVersion, managedResource.ApiVersion)
		if fullMatch {
			match[0] = true
		}
	}

	// Check Resource
	if exclude.Resource == "" {
		match[1] = true
	} else if exclude.Resource == managedResource.Resource {
		match[1] = true
	} else { // Check if ApiGroup is regex
		fullMatch, _ := isFullMatch(exclude.Resource, managedResource.Resource)
		if fullMatch {
			match[1] = true
		}
	}

	// Check Name
	if exclude.Name == "" {
		match[2] = true
	} else if exclude.Name == managedResource.Name {
		match[2] = true
	} else { // Check if ApiGroup is regex
		fullMatch, _ := isFullMatch(exclude.Name, managedResource.Name)
		if fullMatch {
			match[2] = true
		}
	}

	if match[0] && match[1] && match[2] {
		return true
	}
	return false
}

func GetCompositionById(compositionId string, config *rest.Config) (*unstructured.Unstructured, *types.Reference, error) {
	dynClient, err := kubehelper.NewDynamicClient(config)
	if err != nil {
		return nil, nil, fmt.Errorf("obtaining dynamic client for kubernetes: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create discovery client: %v", err)
	}

	// Get list of preferred versions for the group
	groups, err := discoveryClient.ServerGroups()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get server groups: %v", err)
	}

	// Find all versions for our group
	var versions []string
	for _, group := range groups.Groups {
		if group.Name == "composition.krateo.io" {
			for _, version := range group.Versions {
				versions = append(versions, version.Version)
			}
		}
	}

	if len(versions) == 0 {
		return nil, nil, fmt.Errorf("no versions found for group composition.krateo.io")
	}

	// Try each version
	for _, version := range versions {
		resources, err := discoveryClient.ServerResourcesForGroupVersion(fmt.Sprintf("composition.krateo.io/%s", version))
		if err != nil {
			log.Warn().Err(err).Msgf("error getting resources for version %s", version)
			continue
		}

		// Search through each resource type in the group
		for _, r := range resources.APIResources {
			// Skip resources that can't be listed
			if !slices.Contains(r.Verbs, "list") {
				continue
			}

			gvr := schema.GroupVersionResource{
				Group:    "composition.krateo.io",
				Version:  version,
				Resource: r.Name,
			}

			// List objects of this resource type
			list, err := dynClient.Resource(gvr).List(context.TODO(), v1.ListOptions{})
			if err != nil {
				log.Warn().Err(err).Msgf("error listing resources of type %s", r.Name)
				continue
			}

			// Search for the object with matching UID
			for _, item := range list.Items {
				if string(item.GetUID()) == compositionId {
					conditions, ok, err := unstructured.NestedSlice(item.Object, "status", "conditions")
					if !ok {
						return nil, nil, fmt.Errorf("could not get status.Reason of composition %s: %v", compositionId, err)
					}
					if conditions[0].(map[string]interface{})["reason"].(string) == "Creating" {
						return nil, nil, fmt.Errorf("composition is creating")
					}
					installedVersionString, ok := item.GetLabels()["krateo.io/composition-version"]
					if !ok {
						return nil, nil, fmt.Errorf("could not get label 'krateo.io/composition-version' of composition uid %s", compositionId)
					}

					gv, err := schema.ParseGroupVersion(item.GetAPIVersion())
					if err != nil {
						return nil, nil, fmt.Errorf("could not parse group version for composition uid %s", compositionId)
					}
					gv.Version = installedVersionString
					ref := &types.Reference{
						ApiVersion: gv.String(),
						Kind:       item.GetKind(),
						Resource:   kubehelper.InferGroupResource(item.GetAPIVersion(), item.GetKind()).Resource,
						Name:       item.GetName(),
						Namespace:  item.GetNamespace(),
						Uid:        compositionId,
					}

					return &item, ref, nil
				}
			}
		}
	}

	return nil, nil, fmt.Errorf("did not find composition with id %s in any version or resource type", compositionId)
}
