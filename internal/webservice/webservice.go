package webservice

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	corev1 "k8s.io/api/core/v1"

	types "resource-tree-handler/apis"
	cacheHelper "resource-tree-handler/internal/cache"
	kubeHelper "resource-tree-handler/internal/helpers/kube/client"
	compositionHelper "resource-tree-handler/internal/helpers/kube/compositions"
	filtersHelper "resource-tree-handler/internal/helpers/kube/filters"
	resourcetreeHelper "resource-tree-handler/internal/helpers/resourcetree"
	sseHelper "resource-tree-handler/internal/ssemanager"
)

const (
	homeEndpoint      = "/"
	listEndpoint      = "/list"
	allEventsEndpoint = "/handle"
	requestEndpoint   = "/compositions/:compositionId"
	refreshEndpoint   = "/refresh/:compositionId"
)

type Webservice struct {
	WebservicePort int
	Config         *rest.Config
	DynClient      *dynamic.DynamicClient
	SSE            *sseHelper.SSE
}

func (r *Webservice) handleHome(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (r *Webservice) handleAllEvents(c *gin.Context) {
	log.Debug().Msg("received event on /handle")
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Error().Err(err).Msg("error reading request body")
		return
	}
	defer c.Request.Body.Close()

	var event corev1.Event
	err = json.Unmarshal(body, &event)
	if err != nil {
		log.Error().Err(err).Msg("error parsing JSON")
		return
	}

	gv, err := schema.ParseGroupVersion(event.InvolvedObject.APIVersion)
	if err != nil {
		log.Error().Err(err).Msg("could not parse Group Version from ApiVersion")
		return
	}

	if gv.Group != "composition.krateo.io" {
		return
	}

	log.Debug().Msgf("event.Reason: %s", event.Reason)

	// Composition GVK
	gr := kubeHelper.InferGroupResource(gv.Group, event.InvolvedObject.Kind)
	composition := &types.Reference{
		ApiVersion: event.InvolvedObject.APIVersion,
		Resource:   gr.Resource,
		Name:       event.InvolvedObject.Name,
		Namespace:  event.InvolvedObject.Namespace,
	}

	obj, err := kubeHelper.GetObj(c.Request.Context(), composition, r.DynClient)
	if err != nil {
		log.Error().Err(err).Msg("retrieving object")
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error while retrieving object: %s", err)})
		return
	}

	if event.Reason == "CompositionCreated" || !cacheHelper.IsUidInCache(string(obj.GetUID())) {
		log.Info().Msgf("'CompositionCreated' event for composition %s %s %s %s", composition.ApiVersion, composition.Resource, composition.Name, composition.Namespace)
		// Build resource tree for composition
		err := resourcetreeHelper.HandleCreate(obj, *composition, r.DynClient)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error while handling CREATE event: %s", err)})
		}
		r.SSE.SubscribeTo(string(obj.GetUID()))
	} else if event.Reason == "CompositionDeleted" {
		log.Info().Msgf("'CompositionDeleted' event for composition %s %s %s %s", composition.ApiVersion, composition.Resource, composition.Name, composition.Namespace)
		err := resourcetreeHelper.HandleDelete(c, string(obj.GetUID()))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error while handling DELETE event: %s", err)})
		}
		r.SSE.UnsubscribeFrom(string(obj.GetUID()))
	}

	log.Debug().Msg("End of handler for Events endpoint")
}

func (r *Webservice) handleRefresh(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Error().Err(err).Msg("error reading request body")
		return
	}
	defer c.Request.Body.Close()

	var reference *types.Reference
	err = json.Unmarshal(body, &reference)
	if err != nil {
		log.Error().Err(err).Msg("error parsing JSON")
		return
	}

	log.Info().Msgf("'CompositionCreated' event for composition %s %s %s %s", reference.ApiVersion, reference.Resource, reference.Name, reference.Namespace)

	obj, err := kubeHelper.GetObj(c.Request.Context(), reference, r.DynClient)
	if err != nil {
		log.Error().Err(err).Msg("retrieving object")
	}
	exclude := filtersHelper.Get(r.DynClient, *reference)
	resourceTree, err := compositionHelper.GetCompositionResourcesStatus(r.DynClient, obj, *reference, exclude)
	if err != nil {
		log.Error().Err(err).Msg("retrieving managed array statuses")
	}

	resourceTreeJsonStatus, err := json.Marshal(resourceTree.Resources.Status)
	if err != nil {
		log.Error().Err(err).Msg("error marshaling resource tree into JSON")
	}

	cacheHelper.AddToCache(string(resourceTreeJsonStatus), resourceTree, string(obj.GetUID()), *reference, types.Filters{Exclude: exclude})
}

func (r *Webservice) handleList(c *gin.Context) {
	keys := cacheHelper.ListKeysFromCache()
	c.JSON(http.StatusOK, gin.H{"composition_ids": strings.Join(keys, " ")})
}

func (r *Webservice) handleRequest(c *gin.Context) {
	compositionId := c.Param("compositionId")
	resourceTreeString, ok := cacheHelper.GetJSONFromCache(compositionId)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Error parsing GET request: %s", fmt.Errorf("could not find resource tree for CompositionId %s", compositionId))})

	}
	c.JSON(http.StatusOK, resourceTreeString)
}

func (r *Webservice) Spinup() {
	var c *gin.Engine
	// gin.New() instead of gin.Default() to avoid default logging
	if zerolog.GlobalLevel() == zerolog.DebugLevel {
		c = gin.New()
		c.Use(gin.Recovery())
		c.Use(debugLoggerMiddleware())
	} else {
		gin.SetMode(gin.ReleaseMode)
		c = gin.Default()
	}

	c.GET(homeEndpoint, r.handleHome)
	c.GET(requestEndpoint, r.handleRequest)
	c.GET(listEndpoint, r.handleList)
	c.POST(refreshEndpoint, r.handleRefresh)
	c.POST(allEventsEndpoint, r.handleAllEvents)

	c.Run(fmt.Sprintf(":%d", r.WebservicePort))
}
