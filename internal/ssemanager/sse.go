package ssemanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	"k8s.io/client-go/rest"

	types "resource-tree-handler/apis"
	cachehelper "resource-tree-handler/internal/cache"
	kubehelper "resource-tree-handler/internal/helpers/kube/client"
	filtershelper "resource-tree-handler/internal/helpers/kube/filters"
	resourcetreehelper "resource-tree-handler/internal/helpers/resourcetree"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tmaxmax/go-sse"
	corev1 "k8s.io/api/core/v1"
)

type SSE struct {
	Config        *rest.Config
	connection    *sse.Connection
	unsubscribe   map[string]sse.EventCallbackRemover
	unsubscribeMu sync.Mutex
	Cache         *cachehelper.ThreadSafeCache

	// Connection management
	isConnected   bool
	isConnectedMu sync.RWMutex // Mutex for thread-safe access to isConnected
	ctx           context.Context
}

const (
	initialRetryDelay = 1 * time.Second
	maxRetryDelay     = 30 * time.Second
	maxRetryAttempts  = 10
)

func (r *SSE) Spinup(endpoint string) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		log.Error().Err(err).Msg("error while initializing request with http package")
	}
	r.ctx = context.Background()
	r.connection = sse.DefaultClient.NewConnection(req)
	r.setConnected(false)
	go r.maintainConnection()

	r.unsubscribe = make(map[string]sse.EventCallbackRemover)
	logger_instance := log.Output(zerolog.ConsoleWriter{Out: os.Stderr}).With().Str("Client", "SSE Spinup").Logger()
	logger_instance.Debug().Msg("End of spinup")
}

func (r *SSE) maintainConnection() {
	logger_instance := log.Output(zerolog.ConsoleWriter{Out: os.Stderr}).With().Str("Client", "SSE Connection Checker").Logger()
	retryAttempt := 0
	for {
		logger_instance.Debug().Msg("Connection checker loop")
		r.setConnected(true)
		err := r.connection.Connect()
		if err != nil {
			r.setConnected(false)
			if errors.Is(err, context.Canceled) {
				logger_instance.Error().Msg("Connection context canceled, stopping reconnection attempts")
				return
			}

			retryAttempt++
			if retryAttempt > maxRetryAttempts {
				logger_instance.Error().Err(fmt.Errorf("maximum number of retry attempts (%d) reached, stopping reconnection attempts", maxRetryAttempts)).Msg("the resource tree will NOT be updated with managed resources' events, use the /refresh endpoint manually to update the resource tree or restart the service")
				return
			}

			// Calculate delay with exponential backoff
			delay := time.Duration(math.Min(
				float64(initialRetryDelay)*math.Pow(2, float64(retryAttempt-1)),
				float64(maxRetryDelay),
			))

			logger_instance.Warn().Err(err).Msgf("Connection attempt %d failed. Retrying in %v...", retryAttempt, delay)

			select {
			case <-r.ctx.Done():
				return
			case <-time.After(delay):
				continue
			}
		}

		// Connection successful, reset retry counter
		retryAttempt = 0
		r.setConnected(true)
		logger_instance.Info().Msg("Successfully connected to SSE server")

		// Wait for context cancellation before attempting to reconnect
		<-r.ctx.Done()
		logger_instance.Info().Msg("Connection context done, attempting to reconnect...")
	}
}

func (r *SSE) SubscribeTo(compositionId string) {
	log.Info().Msgf("Subscribing to notificaitons for compositionId %s", compositionId)

	callback := func(event sse.Event) {
		sseEventHandlerFunction(event, r.Config, r.Cache)
	}

	if !r.IsConnected() {
		log.Warn().Msg("Detected: SSE client not connected. Registering subscription anyway. You might not receive managed resources' events")
	}

	r.unsubscribeMu.Lock()
	r.unsubscribe[compositionId] = r.connection.SubscribeEvent(compositionId, callback)
	r.unsubscribeMu.Unlock()
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

func sseEventHandlerFunction(eventObj sse.Event, config *rest.Config, cacheObj *cachehelper.ThreadSafeCache) {
	logger := log.Output(zerolog.ConsoleWriter{Out: os.Stderr}).With().Str("Client", "SSE Connection Checker").Logger()
	logger.Info().Msgf("Function callback for event %s", eventObj.LastEventID)

	var event Event
	err := json.Unmarshal([]byte(eventObj.Data), &event)
	if err != nil {
		logger.Error().Err(err).Msgf("there was an error unmarshaling the event %s", eventObj.Data)
		return
	}

	gr := kubehelper.InferGroupResource(event.InvolvedObject.APIVersion, event.InvolvedObject.Kind)
	objectReference := &types.Reference{
		ApiVersion: event.InvolvedObject.APIVersion,
		Kind:       event.InvolvedObject.Kind,
		Resource:   gr.Resource,
		Name:       event.InvolvedObject.Name,
		Namespace:  event.InvolvedObject.Namespace,
	}

	objectUnstructured, err := kubehelper.GetObj(context.TODO(), objectReference, config)
	if err != nil {
		logger.Error().Err(err).Msgf("retrieving event object, stopping event handling")
		return
	}
	labels := objectUnstructured.GetLabels()
	if compositionId, ok := labels["krateo.io/composition-id"]; ok {
		resourceTree, ok, discarded := cacheObj.GetResourceTreeFromCacheWithTimeout(compositionId, string(event.InvolvedObject.UID), 30*time.Second)
		if !ok {
			logger.Error().Msgf("timeout waiting for resource tree for compositionId: %s", compositionId)
			return
		}
		if discarded {
			logger.Warn().Msgf("Discarded function callback for event %s, object uid %s, event obsolete...", eventObj.LastEventID, string(event.InvolvedObject.UID))
			return
		}
		exclude := filtershelper.GetFilters(config, resourceTree.CompositionReference)
		// If the filters did not change, then update the resource tree entry
		if filtershelper.CompareFilters(types.Filters{Exclude: exclude}, resourceTree.Filters) {
			logger.Info().Msgf("Handling object update for object %s %s %s %s and composition id %s", objectReference.Resource, objectReference.ApiVersion, objectReference.Name, objectReference.Namespace, compositionId)
			resourcetreehelper.HandleUpdate(*objectReference, event.InvolvedObject.Kind, compositionId, cacheObj, config)
		} else { // If the filters did change, then rebuild the entire resource tree
			logger.Info().Msgf("Filter update detected, updating resource tree for composition id %s", compositionId)
			compositionUnstructured, err := kubehelper.GetObj(context.TODO(), &resourceTree.CompositionReference, config)
			if err != nil {
				logger.Error().Err(err).Msgf("retrieving composition object")
				return
			}
			resourcetreehelper.HandleCreate(compositionUnstructured, resourceTree.CompositionReference, cacheObj, config)
		}

	}
}

type Event struct {
	// The object that this event is about.
	InvolvedObject corev1.ObjectReference `json:"involvedObject"`
}

func (r *SSE) IsConnected() bool {
	r.isConnectedMu.RLock()
	defer r.isConnectedMu.RUnlock()
	return r.isConnected
}

func (r *SSE) setConnected(connected bool) {
	r.isConnectedMu.Lock()
	defer r.isConnectedMu.Unlock()
	r.isConnected = connected
}
