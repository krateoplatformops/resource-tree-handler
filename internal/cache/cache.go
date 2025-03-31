package cache

import (
	"fmt"
	types "resource-tree-handler/apis"
	kubehelper "resource-tree-handler/internal/helpers/kube/client"
	compositionhelper "resource-tree-handler/internal/helpers/kube/compositions"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type ResourceTreeUpdate struct {
	LastUpdate           time.Time
	ResourceTree         types.ResourceTree
	CompositionReference types.Reference
	Filters              types.Filters
}

type operation int

// UpdateOperation represents a function that modifies a ResourceTreeUpdate
type UpdateOperation func(*ResourceTreeUpdate) error

const (
	opAdd operation = iota
	opUpdate
	opGet
	opGetResourceTree
	opDelete
	opListKeys
	opIsUidInCache
	opQueuedUpdate
	// opWaitForResource: When received, checks if resource exists in cache and returns immediately if found.
	// If not found, adds caller's response channel to waiters list for that composition ID.
	// Caller will be notified through the channel when resource is added via opAdd, or will timeout.
	opWaitForResource
	opCleanupWaiter
)

type request struct {
	op            operation
	compositionId string
	eventObjectId string
	resourceTree  types.ResourceTree
	compReference types.Reference
	filters       types.Filters
	updateOp      UpdateOperation
	responseChan  chan interface{}
	errorChan     chan error
}

type waitResult struct {
	update    *ResourceTreeUpdate
	ok        bool
	discarded bool
}

type ThreadSafeCache struct {
	requestChan  chan request
	cache        map[string]*ResourceTreeUpdate
	waiters      map[string]map[string]chan interface{}
	waitersMutex sync.Mutex
}

func NewThreadSafeCache() *ThreadSafeCache {
	c := &ThreadSafeCache{
		requestChan: make(chan request),
		cache:       make(map[string]*ResourceTreeUpdate),
		waiters:     make(map[string]map[string]chan interface{}),
	}
	go c.run()
	return c
}

func (c *ThreadSafeCache) run() {
	for req := range c.requestChan {
		switch req.op {
		case opAdd:
			delete(c.cache, req.compositionId)
			c.cache[req.compositionId] = &ResourceTreeUpdate{
				LastUpdate:           time.Now(),
				ResourceTree:         req.resourceTree,
				CompositionReference: req.compReference,
				Filters:              req.filters,
			}
			req.responseChan <- struct{}{}
			c.notifyWaiters(req.compositionId)

		case opUpdate:
			if _, ok := c.cache[req.compositionId]; ok {
				c.cache[req.compositionId].LastUpdate = time.Now()
				c.cache[req.compositionId].ResourceTree = req.resourceTree
			}
			req.responseChan <- struct{}{}

		case opGet:
			obj, ok := c.cache[req.compositionId]
			if ok {
				req.responseChan <- struct {
					status *ResourceTreeUpdate
					ok     bool
				}{obj, true}
			} else {
				req.responseChan <- struct {
					status *ResourceTreeUpdate
					ok     bool
				}{nil, false}
			}

		case opGetResourceTree:
			obj, ok := c.cache[req.compositionId]
			if ok {
				req.responseChan <- struct {
					update *ResourceTreeUpdate
					ok     bool
				}{obj, true}
			} else {
				req.responseChan <- struct {
					update *ResourceTreeUpdate
					ok     bool
				}{&ResourceTreeUpdate{}, false}
			}

		case opDelete:
			delete(c.cache, req.compositionId)
			req.responseChan <- struct{}{}

		case opListKeys:
			keys := make([]string, 0, len(c.cache))
			for k := range c.cache {
				keys = append(keys, k)
			}
			req.responseChan <- keys

		case opIsUidInCache:
			_, exists := c.cache[req.compositionId]
			req.responseChan <- exists

		case opQueuedUpdate:
			if obj, ok := c.cache[req.compositionId]; ok {
				if err := req.updateOp(obj); err != nil {
					// Send error to error channel
					req.errorChan <- err
					// Also send an empty response to ensure the response channel is not blocked
					req.responseChan <- struct{}{}
				} else {
					obj.LastUpdate = time.Now()
					req.responseChan <- struct{}{}
				}
			} else {
				// Send error to error channel
				req.errorChan <- fmt.Errorf("resource tree for composition id %s not found", req.compositionId)
				// Also send an empty response to ensure the response channel is not blocked
				req.responseChan <- struct{}{}
			}

		case opWaitForResource:
			if obj, exists := c.cache[req.compositionId]; exists {
				req.responseChan <- waitResult{update: obj, ok: true, discarded: false}
			} else {
				log.Warn().Msgf("Composition not ready %s, setting up waiter %s", req.compositionId, req.eventObjectId)
				c.waitersMutex.Lock()
				if _, exists := c.waiters[req.compositionId]; !exists {
					c.waiters[req.compositionId] = make(map[string]chan interface{})
				}
				if responseChan, ok := c.waiters[req.compositionId][req.eventObjectId]; ok {
					log.Warn().Msgf("Sending discard to %s %s", req.compositionId, req.eventObjectId)
					responseChan <- waitResult{update: &ResourceTreeUpdate{}, ok: true, discarded: true}
				}
				c.waiters[req.compositionId][req.eventObjectId] = req.responseChan
				c.waitersMutex.Unlock()
			}

		case opCleanupWaiter:
			c.waitersMutex.Lock()
			if innerMap, exists := c.waiters[req.compositionId]; exists {
				if _, exists := innerMap[req.eventObjectId]; exists {
					delete(innerMap, req.eventObjectId)
					if len(innerMap) == 0 {
						delete(c.waiters, req.compositionId)
					}
				}
			}
			c.waitersMutex.Unlock()
			req.responseChan <- struct{}{}
		}
	}
}

func (c *ThreadSafeCache) AddToCache(resourceTree types.ResourceTree, compositionId string, compositionReference types.Reference, filters types.Filters) {
	responseChan := make(chan interface{})
	c.requestChan <- request{
		op:            opAdd,
		compositionId: compositionId,
		resourceTree:  resourceTree,
		compReference: compositionReference,
		filters:       filters,
		responseChan:  responseChan,
	}
	<-responseChan
}

func (c *ThreadSafeCache) UpdateCacheEntry(resourceTree types.ResourceTree, compositionId string, compositionReference types.Reference) {
	responseChan := make(chan interface{})
	c.requestChan <- request{
		op:            opUpdate,
		compositionId: compositionId,
		resourceTree:  resourceTree,
		compReference: compositionReference,
		responseChan:  responseChan,
	}
	<-responseChan
}

func (c *ThreadSafeCache) GetJSONFromCache(compositionId string) ([]*types.ResourceNodeStatus, bool) {
	responseChan := make(chan interface{})
	c.requestChan <- request{
		op:            opGet,
		compositionId: compositionId,
		responseChan:  responseChan,
	}
	response := <-responseChan
	result := response.(struct {
		status *ResourceTreeUpdate
		ok     bool
	})

	final_array := []*types.ResourceNodeStatus{}
	if result.ok {
		excludes := result.status.Filters.Exclude
		for _, managedResource := range result.status.ResourceTree.Resources.Status {
			skip := false
			gr := kubehelper.InferGroupResource(managedResource.Version, managedResource.Kind)
			reference := types.Reference{
				ApiVersion: managedResource.Version,
				Kind:       managedResource.Kind,
				Resource:   gr.Resource,
				Name:       managedResource.Name,
				Namespace:  managedResource.Namespace,
			}
			for _, exclude := range excludes {
				if compositionhelper.ShouldItSkip(exclude, reference) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			final_array = append(final_array, managedResource)
		}
	}

	return final_array, result.ok
}

func (c *ThreadSafeCache) GetResourceTreeFromCache(compositionId string) (*ResourceTreeUpdate, bool) {
	responseChan := make(chan interface{})
	c.requestChan <- request{
		op:            opGetResourceTree,
		compositionId: compositionId,
		responseChan:  responseChan,
	}
	response := <-responseChan
	result := response.(struct {
		update *ResourceTreeUpdate
		ok     bool
	})
	return result.update, result.ok
}

func (c *ThreadSafeCache) GetResourceTreeFromCacheWithTimeout(compositionId string, eventObjectId string, timeout time.Duration) (*ResourceTreeUpdate, bool, bool) {
	responseChan := make(chan interface{})

	c.requestChan <- request{
		op:            opWaitForResource,
		compositionId: compositionId,
		eventObjectId: eventObjectId,
		responseChan:  responseChan,
	}

	select {
	case result := <-responseChan:
		if r, ok := result.(waitResult); ok {
			return r.update, r.ok, r.discarded
		}
		return &ResourceTreeUpdate{}, false, false
	case <-time.After(timeout):
		cleanupResponseChan := make(chan interface{})
		c.requestChan <- request{
			op:            opCleanupWaiter,
			compositionId: compositionId,
			eventObjectId: eventObjectId,
			responseChan:  cleanupResponseChan,
		}
		// Make sure to consume the response to avoid goroutine leaks
		<-cleanupResponseChan
		return &ResourceTreeUpdate{}, false, false
	}
}

func (c *ThreadSafeCache) notifyWaiters(compositionId string) {
	c.waitersMutex.Lock()
	defer c.waitersMutex.Unlock()

	log.Info().Msgf("Notifying eventsse waiters for composition id %s", compositionId)

	if waiters, exists := c.waiters[compositionId]; exists {
		obj := c.cache[compositionId]
		for key, objectWaiters := range waiters {
			log.Info().Msgf("\tNotifying eventsse waiter for object id %s", key)
			objectWaiters <- waitResult{update: obj, ok: true, discarded: false}
		}
		delete(c.waiters, compositionId)
	}
}

// QueueUpdate allows atomic updates to the ResourceTreeUpdate
func (c *ThreadSafeCache) QueueUpdate(compositionId string, updateOp UpdateOperation) error {
	responseChan := make(chan interface{})
	errorChan := make(chan error)

	c.requestChan <- request{
		op:            opQueuedUpdate,
		compositionId: compositionId,
		updateOp:      updateOp,
		responseChan:  responseChan,
		errorChan:     errorChan,
	}

	// Use select to handle both channels
	select {
	case err := <-errorChan:
		// Consume the response channel to avoid goroutine leak
		<-responseChan
		return err
	case <-responseChan:
		// Check if there's also an error
		select {
		case err := <-errorChan:
			return err
		default:
			// No error, continue
			return nil
		}
	}
}

func (c *ThreadSafeCache) DeleteFromCache(compositionId string) {
	responseChan := make(chan interface{})
	c.requestChan <- request{
		op:            opDelete,
		compositionId: compositionId,
		responseChan:  responseChan,
	}
	<-responseChan
}

func (c *ThreadSafeCache) ListKeysFromCache() []string {
	responseChan := make(chan interface{})
	c.requestChan <- request{
		op:           opListKeys,
		responseChan: responseChan,
	}
	return (<-responseChan).([]string)
}

func (c *ThreadSafeCache) IsUidInCache(compositionId string) bool {
	responseChan := make(chan interface{})
	c.requestChan <- request{
		op:            opIsUidInCache,
		compositionId: compositionId,
		responseChan:  responseChan,
	}
	return (<-responseChan).(bool)
}
