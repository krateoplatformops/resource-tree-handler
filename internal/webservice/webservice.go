package webservice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	corev1 "k8s.io/api/core/v1"

	types "resource-tree-handler/apis"
	cachehelper "resource-tree-handler/internal/cache"
	kubehelper "resource-tree-handler/internal/helpers/kube/client"
	compositionhelper "resource-tree-handler/internal/helpers/kube/compositions"
	filtershelper "resource-tree-handler/internal/helpers/kube/filters"
	resourcetreehelper "resource-tree-handler/internal/helpers/resourcetree"
	ssehelper "resource-tree-handler/internal/ssemanager"
)

const (
	homeEndpoint      = "/"
	listEndpoint      = "/list"
	allEventsEndpoint = "/handle"
	requestEndpoint   = "/compositions/:compositionId"
	refreshEndpoint   = "/refresh/:compositionId"

	busyString   = "busy"
	freeString   = "free"
	queuedString = "queued"

	// Maximum number of concurrent resource tree creations
	maxConcurrentJobs = 10
)

// CreateJobRequest represents a job to create a resource tree
type CreateJobRequest struct {
	CompositionUnstructured *unstructured.Unstructured
	CompositionReference    types.Reference
	CompositionID           string
}

type Webservice struct {
	WebservicePort      int
	Config              *rest.Config
	SSE                 *ssehelper.SSE
	Cache               *cachehelper.ThreadSafeCache
	compositionStatus   map[string]string
	compositionStatusMu sync.Mutex

	// Job queue for resource tree creation
	jobQueue  chan CreateJobRequest
	workersWg sync.WaitGroup
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

	compositionId := string(event.InvolvedObject.UID)

	log.Info().Msgf("Event %s received for composition id %s", event.Reason, compositionId)
	log.Info().Msgf("IsUidInCache(%s): %t", compositionId, r.Cache.IsUidInCache(compositionId))

	if !r.continueOperationsWithComposition(compositionId) {
		c.String(http.StatusTooManyRequests, "composition id %s is busy or queued", compositionId)
		return
	}
	r.setContinueOperationsWithComposition(compositionId, busyString)

	if event.Reason == "CompositionDeleted" {
		if compositionId == "" {
			log.Error().Err(fmt.Errorf("could not find composition id in cache by composition reference")).Msgf("error deleting composition resources")
			c.String(http.StatusInternalServerError, "DELETE for CompositionId %s not executed", compositionId)
			return
		}
		r.Cache.DeleteFromCache(compositionId)
		r.SSE.UnsubscribeFrom(compositionId)
		c.String(http.StatusOK, "DELETE for CompositionId %s executed", compositionId)
		return
	}

	compositionUnstructured, compositionReferece, err := compositionhelper.GetCompositionById(compositionId, r.Config)
	if err != nil {
		log.Error().Err(err).Msgf("could not get composition with id %s", compositionId)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error while handling %s event: %s", event.Reason, err)})
		r.setContinueOperationsWithComposition(compositionId, freeString)
		return
	}

	if event.Reason == "CompositionCreated" || event.Reason == "CompositionUpdated" || !r.Cache.IsUidInCache(string(compositionUnstructured.GetUID())) {
		log.Info().Msgf("'%s' event for composition %s %s %s %s %s", event.Reason, compositionReferece.Uid, compositionReferece.ApiVersion, compositionReferece.Resource, compositionReferece.Name, compositionReferece.Namespace)

		r.SSE.SubscribeTo(string(compositionUnstructured.GetUID()))

		// Set status to queued before submitting to the queue
		r.setContinueOperationsWithComposition(compositionId, queuedString)

		// Create the job and submit it to the queue asynchronously
		job := CreateJobRequest{
			CompositionUnstructured: compositionUnstructured,
			CompositionReference:    *compositionReferece,
			CompositionID:           compositionId,
		}

		// Respond to client immediately with 202 Accepted
		c.JSON(http.StatusAccepted, gin.H{"message": fmt.Sprintf("Job for composition %s has been queued", compositionId)})

		// Submit job to queue after responding to client
		go func() {
			r.jobQueue <- job
		}()

		log.Info().Msgf("Job for composition %s has been queued", compositionId)
	} else {
		// If we got here, nothing needed to be done
		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("No action needed for composition %s", compositionId)})
		// Free the resource if there is nothing to do
		if !r.continueOperationsWithComposition(compositionId) {
			r.setContinueOperationsWithComposition(compositionId, freeString)
		}
	}
}

func (r *Webservice) handleRefresh(c *gin.Context) {
	compositionId := c.Param("compositionId")
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
	reference.Uid = compositionId

	log.Info().Msgf("'CompositionCreated' event for composition %s %s %s %s", reference.ApiVersion, reference.Resource, reference.Name, reference.Namespace)

	obj, err := kubehelper.GetObj(c.Request.Context(), reference, r.Config)
	if err != nil {
		log.Error().Err(err).Msg("retrieving object")
	}
	exclude := filtershelper.GetFilters(r.Config, *reference)
	if r.continueOperationsWithComposition(compositionId) {
		r.setContinueOperationsWithComposition(compositionId, busyString)
		resourceTree, err := compositionhelper.GetCompositionResourcesStatus(r.Config, obj, *reference, exclude)
		if err != nil {
			log.Error().Err(err).Msg("retrieving managed array statuses")
		}

		r.Cache.AddToCache(resourceTree, string(obj.GetUID()), *reference, types.Filters{Exclude: exclude})
		r.setContinueOperationsWithComposition(compositionId, freeString)
	}
}

func (r *Webservice) handleList(c *gin.Context) {
	keys := r.Cache.ListKeysFromCache()
	c.JSON(http.StatusOK, gin.H{"composition_ids": strings.Join(keys, " ")})
}

func (r *Webservice) handleRequest(c *gin.Context) {
	compositionId := c.Param("compositionId")
	resourceTreeStatusObj, okJSON := r.Cache.GetJSONFromCache(compositionId)

	if !okJSON {
		log.Warn().Msgf("could not find resource tree for CompositionId %s", compositionId)
		compositionUnstructured, compositionReferece, err := compositionhelper.GetCompositionById(compositionId, r.Config)
		if err != nil {
			log.Error().Err(err).Msgf("could not obtain composition object with composition id %s", compositionId)
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Error parsing GET request: %s", fmt.Errorf("could not obtain composition object with composition id %s: %v", compositionId, err))})
			return
		}

		if !r.continueOperationsWithComposition(compositionId) {
			log.Warn().Msgf("composition id %s is busy or queued", compositionId)
			c.String(http.StatusTooManyRequests, "composition id %s is busy or queued", compositionId)
			return
		}

		// Set status to queued
		r.setContinueOperationsWithComposition(compositionId, queuedString)

		log.Info().Msgf("Queuing CREATE job from GET request for composition id %s: ", compositionId)

		// Subscribe to SSE before queueing the job
		r.SSE.SubscribeTo(compositionId)

		// Create the job and submit it to the queue asynchronously
		job := CreateJobRequest{
			CompositionUnstructured: compositionUnstructured,
			CompositionReference:    *compositionReferece,
			CompositionID:           compositionId,
		}

		// Respond to client immediately with 202 Accepted
		c.JSON(http.StatusAccepted, gin.H{"message": fmt.Sprintf("Job for composition %s has been queued", compositionId)})

		// Submit job to queue after responding to client
		go func() {
			r.jobQueue <- job
		}()

		log.Info().Msgf("Job for composition %s has been queued", compositionId)
		return
	}

	// Resource tree exists in cache, return it
	log.Info().Msgf("Resouce tree for composition id %s ready", compositionId)
	c.JSON(http.StatusOK, resourceTreeStatusObj)

	resourceTreeUpdate, okData := r.Cache.GetResourceTreeFromCache(compositionId)
	if !okData {
		log.Error().Msgf("could not obtain resource tree data structure, this should not happen")
		return
	}

	if time.Since(resourceTreeUpdate.LastUpdate) > time.Duration(8*time.Hour) {
		log.Warn().Msgf("Updating resource tree for CompositionId %s, current resource tree may not be up to date if controllers do not report events...", compositionId)

		compositionUnstructured, compositionReferece, err := compositionhelper.GetCompositionById(compositionId, r.Config)
		if err != nil {
			log.Error().Err(err).Msgf("could not obtain composition object with composition id %s", compositionId)
			return
		}

		if !r.continueOperationsWithComposition(compositionId) {
			log.Warn().Msgf("composition id %s is busy or queued", compositionId)
			return
		}

		// Set status to queued
		r.setContinueOperationsWithComposition(compositionId, queuedString)

		log.Info().Msgf("Queuing CREATE job from UPDATE request for composition id %s: ", compositionId)

		// Create the job and submit it to the queue asynchronously
		job := CreateJobRequest{
			CompositionUnstructured: compositionUnstructured,
			CompositionReference:    *compositionReferece,
			CompositionID:           compositionId,
		}

		// Submit job to queue after responding to client
		go func() {
			r.jobQueue <- job
		}()

		log.Info().Msgf("Job UPDATE for composition %s has been queued", compositionId)
		return
	}
}

func (r *Webservice) continueOperationsWithComposition(compositionId string) bool {
	r.compositionStatusMu.Lock()
	defer r.compositionStatusMu.Unlock()
	val, ok := r.compositionStatus[compositionId]
	if !ok {
		return true
	}
	if val == busyString || val == queuedString {
		return false
	}
	return true
}

func (r *Webservice) setContinueOperationsWithComposition(compositionId string, value string) {
	r.compositionStatusMu.Lock()
	defer r.compositionStatusMu.Unlock()
	r.compositionStatus[compositionId] = value
}

// startWorker starts a worker that processes jobs from the queue
func (r *Webservice) startWorker(workerId int) {
	defer r.workersWg.Done()

	log.Debug().Msgf("Starting worker %d", workerId)

	for job := range r.jobQueue {
		compositionId := job.CompositionID
		log.Info().Msgf("Worker %d processing job for composition %s", workerId, compositionId)

		// Mark as busy while processing
		r.setContinueOperationsWithComposition(compositionId, busyString)

		// Execute the actual job
		err := resourcetreehelper.HandleCreate(job.CompositionUnstructured, job.CompositionReference, r.Cache, r.Config)

		if err != nil {
			log.Error().Err(err).Msgf("Worker %d failed to create resource tree for composition %s", workerId, compositionId)
		} else {
			log.Info().Msgf("Worker %d successfully created resource tree for composition %s", workerId, compositionId)
		}

		// Mark as free after processing is done
		r.setContinueOperationsWithComposition(compositionId, freeString)
	}
}

// initWorkerPool initializes the worker pool
func (r *Webservice) initWorkerPool() {
	r.jobQueue = make(chan CreateJobRequest, 1000) // Buffer for pending jobs

	// Start the worker pool
	r.workersWg.Add(maxConcurrentJobs)
	for i := range maxConcurrentJobs {
		go r.startWorker(i)
	}

	log.Info().Msgf("Started worker pool with %d workers", maxConcurrentJobs)
}

func (r *Webservice) Spinup(ctx context.Context) {
	r.compositionStatus = make(map[string]string)

	// Initialize the worker pool
	r.initWorkerPool()

	var c *gin.Engine
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

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", r.WebservicePort),
		Handler: c.Handler(),
	}
	go func() {
		// service connections
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Logger.Error().Err(err).Msgf("listen on %d", r.WebservicePort)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with
	// a timeout of 5 seconds.
	quit := make(chan os.Signal, 1)
	// kill (no params) by default sends syscall.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall.SIGKILL but can't be caught, so don't need add it
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Logger.Debug().Msg("Shutdown Server ...")

	if err := srv.Shutdown(ctx); err != nil {
		log.Logger.Warn().Err(err).Msg("Server Shutdown")
	}
	// catching ctx.Done(). timeout of 5 seconds.
	<-ctx.Done()
	log.Logger.Debug().Msg("timeout of 5 seconds.")
	log.Logger.Debug().Msg("Server exiting")

}
