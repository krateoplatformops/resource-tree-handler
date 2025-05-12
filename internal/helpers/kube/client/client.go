package client

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"github.com/krateoplatformops/plumbing/kubeutil/plurals"

	apitypes "resource-tree-handler/apis"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewDynamicClient(rc *rest.Config) (*dynamic.DynamicClient, error) {
	config := *rc
	config.APIPath = "/api"
	config.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)
	config.UserAgent = rest.DefaultKubernetesUserAgent()
	// config.QPS = 100
	// config.Burst = 1000

	return dynamic.NewForConfig(&config)
}

func GetObj(ctx context.Context, cr *apitypes.Reference, config *rest.Config) (*unstructured.Unstructured, error) {
	dynClient, err := NewDynamicClient(config)
	if err != nil {
		return nil, fmt.Errorf("obtaining dynamic client for kubernetes: %w", err)
	}

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

func InferGroupResource(a, k string) schema.GroupResource {
	gv, err := schema.ParseGroupVersion(a)
	if err != nil {
		log.Error().Err(err).Msg("could not parse apiVersion for pluralizer")
		return schema.GroupResource{}
	}

	gvk := schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    k,
	}

	tmp, err := plurals.Get(gvk, plurals.GetOptions{
		ResolverFunc: plurals.ResolveAPINames,
	})
	if err != nil {
		log.Error().Err(err).Msgf("could not obtain plural for %s %s %s", gvk.Group, gvk.Kind, gvk.Version)
		return schema.GroupResource{}
	}

	return schema.GroupResource{
		Resource: tmp.Plural,
		Group:    gv.Group,
	}
}
