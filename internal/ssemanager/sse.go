package ssemanager

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	types "resource-tree-handler/apis"
	cacheHelper "resource-tree-handler/internal/cache"
	kubeHelper "resource-tree-handler/internal/helpers/kube/client"
	filtersHelper "resource-tree-handler/internal/helpers/kube/filters"
	resourcetreeHelper "resource-tree-handler/internal/helpers/resourcetree"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tmaxmax/go-sse"
	corev1 "k8s.io/api/core/v1"
)

type SSE struct {
	Config        *rest.Config
	connection    map[string]*sse.Connection
	connMu        sync.Mutex
	unsubscribe   map[string]sse.EventCallbackRemover
	unsubscribeMu sync.Mutex
	request       *http.Request
	logger        *zerolog.Logger
}

func (r *SSE) Spinup(endpoint string) {
	logger_instance := log.Output(zerolog.ConsoleWriter{Out: os.Stderr}).With().Str("Client", "SSE").Logger()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		log.Error().Err(err).Msg("error while initializing request with http package")
	}
	r.request = req
	r.connection = make(map[string]*sse.Connection)
	r.unsubscribe = make(map[string]sse.EventCallbackRemover)
	r.logger = &logger_instance
	logger_instance.Debug().Msg("End of spinup")
}

func (r *SSE) SubscribeTo(compositionId string) {
	log.Info().Msgf("Subscribing to notificaitons for compositionId %s", compositionId)

	r.connMu.Lock()
	defer r.connMu.Unlock()

	callback := func(event sse.Event) {
		sseEventHandlerFunction(event, r.Config, r.logger)
	}

	r.connection[compositionId] = sse.DefaultClient.NewConnection(r.request)

	r.unsubscribeMu.Lock()
	r.unsubscribe[compositionId] = r.connection[compositionId].SubscribeEvent(compositionId, callback)
	r.unsubscribeMu.Unlock()

	go func() {
		if err := r.connection[compositionId].Connect(); !errors.Is(err, context.Canceled) {
			r.logger.Error().Err(err).Msgf("error while instantiating conenction to eventsse")
		}
	}()
}

func (r *SSE) UnsubscribeFrom(compositionId string) {
	log.Info().Msgf("Unsubscribing from notificaitons for compositionId %s", compositionId)

	r.unsubscribeMu.Lock()
	if unsubscribe, exists := r.unsubscribe[compositionId]; exists {
		unsubscribe()
		delete(r.unsubscribe, compositionId)
	}
	r.unsubscribeMu.Unlock()
}

func sseEventHandlerFunction(eventObj sse.Event, config *rest.Config, logger *zerolog.Logger) {
	logger.Info().Msgf("Function callback for event %s", eventObj.LastEventID)
	dynClient, err := kubeHelper.New(config)
	if err != nil {
		logger.Error().Err(err).Msgf("there was an error obtaining the dynamic client")
		return
	}

	var event Event
	err = json.Unmarshal([]byte(eventObj.Data), &event)
	if err != nil {
		logger.Error().Err(err).Msgf("there was an error unmarshaling the event %s", eventObj.Data)
		return
	}
	gv, err := schema.ParseGroupVersion(event.InvolvedObject.APIVersion)
	if err != nil {
		logger.Error().Err(err).Msgf("could not parse Group Version from ApiVersion")
		return
	}

	gr := kubeHelper.InferGroupResource(gv.Group, event.InvolvedObject.Kind)
	objectReference := &types.Reference{
		ApiVersion: event.InvolvedObject.APIVersion,
		Resource:   gr.Resource,
		Name:       event.InvolvedObject.Name,
		Namespace:  event.InvolvedObject.Namespace,
	}

	objectUnstructured, err := kubeHelper.GetObj(context.TODO(), objectReference, dynClient)
	if err != nil {
		logger.Error().Err(err).Msgf("retrieving event object, stopping event handling")
		return
	}
	labels := objectUnstructured.GetLabels()
	for _, compositionId := range cacheHelper.ListKeysFromCache() {
		if label, ok := labels["krateo.io/composition-id"]; ok {
			if label == compositionId {
				resourceTree, ok := cacheHelper.GetResourceTreeFromCache(compositionId)
				if !ok {
					logger.Error().Err(err).Msgf("could not obtain resource tree for compositionId: %s", compositionId)
					return
				}
				exclude := filtersHelper.Get(dynClient, resourceTree.CompositionReference)
				// If the filters did not change, then update the resource tree entry
				if filtersHelper.CompareFilters(types.Filters{Exclude: exclude}, resourceTree.Filters) {
					logger.Info().Msgf("Handling object update for object %s %s %s %s and composition id %s", objectReference.Resource, objectReference.ApiVersion, objectReference.Name, objectReference.Namespace, compositionId)
					resourcetreeHelper.HandleUpdate(*objectReference, event.InvolvedObject.Kind, compositionId, dynClient)
				} else { // If the filters did change, then rebuild the entire resource tree
					logger.Info().Msgf("Filter update detected, updating resource tree for composition id %s", compositionId)
					compositionUnstructured, err := kubeHelper.GetObj(context.TODO(), &resourceTree.CompositionReference, dynClient)
					if err != nil {
						logger.Error().Err(err).Msgf("retrieving composition object")
						return
					}
					resourcetreeHelper.HandleCreate(compositionUnstructured, resourceTree.CompositionReference, dynClient)
				}
			}
		}
	}

}

type Event struct {
	// The object that this event is about.
	InvolvedObject corev1.ObjectReference `json:"involvedObject"`
}
