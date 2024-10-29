package cache

import (
	types "resource-tree-handler/apis"
	kubeHelper "resource-tree-handler/internal/helpers/kube/client"
	compositionHelper "resource-tree-handler/internal/helpers/kube/compositions"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ResourceTreeUpdate struct {
	LastUpdate           time.Time
	ResourceTree         types.ResourceTree
	CompositionReference types.Reference
	Filters              types.Filters
}

type operation int

const (
	opAdd operation = iota
	opUpdate
	opGet
	opGetResourceTree
	opDelete
	opListKeys
	opIsUidInCache
)

type request struct {
	op            operation
	compositionId string
	resourceTree  types.ResourceTree
	compReference types.Reference
	filters       types.Filters
	responseChan  chan interface{}
}

type ThreadSafeCache struct {
	requestChan chan request
	cache       map[string]*ResourceTreeUpdate
}

func NewThreadSafeCache() *ThreadSafeCache {
	c := &ThreadSafeCache{
		requestChan: make(chan request),
		cache:       make(map[string]*ResourceTreeUpdate),
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
			gv, _ := schema.ParseGroupVersion(managedResource.Version)
			gr := kubeHelper.InferGroupResource(gv.Group, managedResource.Kind)
			reference := types.Reference{
				ApiVersion: managedResource.Version,
				Kind:       managedResource.Kind,
				Resource:   gr.Resource,
				Name:       managedResource.Name,
				Namespace:  managedResource.Namespace,
			}
			for _, exclude := range excludes {
				if compositionHelper.ShouldItSkip(exclude, reference) {
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
