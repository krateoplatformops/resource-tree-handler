package client

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"k8s.io/gengo/namer"
	"k8s.io/gengo/types"

	api_types "resource-tree-handler/apis"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func New(rc *rest.Config) (*dynamic.DynamicClient, error) {
	config := *rc
	config.APIPath = "/api"
	config.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)
	config.UserAgent = rest.DefaultKubernetesUserAgent()
	//config.QPS = 1000
	//config.Burst = 3000

	return dynamic.NewForConfig(&config)
}

func GetObj(ctx context.Context, cr *api_types.Reference, dynClient *dynamic.DynamicClient) (*unstructured.Unstructured, error) {
	gv, err := schema.ParseGroupVersion(cr.ApiVersion)
	if err != nil {
		return nil, fmt.Errorf("unable to parse GroupVersion from composition reference ApiVersion: %w", err)
	}
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: cr.Resource,
	}
	// Get structure to send to webservice
	res, err := dynClient.Resource(gvr).Namespace(cr.Namespace).Get(ctx, cr.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve resource %s with name %s in namespace %s, with apiVersion %s: %w", cr.Resource, cr.Name, cr.Namespace, cr.ApiVersion, err)
	}
	return res, nil
}

func InferGroupResource(g, k string) schema.GroupResource {
	gk := schema.GroupKind{
		Group: g,
		Kind:  k,
	}

	// The namer does not work with the kind "Repo" with plural resources "repoes"
	if (g == "git.krateo.io" || g == "github.krateo.io") && k == "Repo" {
		return schema.GroupResource{
			Group:    gk.Group,
			Resource: "repoes",
		}
	}

	kind := types.Type{Name: types.Name{Name: gk.Kind}}
	namer := namer.NewPrivatePluralNamer(nil)
	return schema.GroupResource{
		Group:    gk.Group,
		Resource: strings.ToLower(namer.Name(&kind)),
	}
}
