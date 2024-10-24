package cache

import (
	types "resource-tree-handler/apis"
	"sync"
	"time"
)

type ResourceTreeUpdate struct {
	LastUpdate           time.Time
	ResourceTree         types.ResourceTree
	CompositionReference types.Reference
	Filters              types.Filters
}

type ThreadSafeCache struct {
	mu    sync.RWMutex
	cache map[string]*ResourceTreeUpdate
}

var (
	cache *ThreadSafeCache
)

func init() {
	cache = &ThreadSafeCache{
		cache: make(map[string]*ResourceTreeUpdate),
	}
}

func (c *ThreadSafeCache) AddToCache(resourceTree types.ResourceTree,
	compositionId string,
	compositionReference types.Reference,
	filters types.Filters) {

	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, compositionId)
	c.cache[compositionId] = &ResourceTreeUpdate{
		LastUpdate:           time.Now(),
		ResourceTree:         resourceTree,
		CompositionReference: compositionReference,
		Filters:              filters,
	}
}

func (c *ThreadSafeCache) UpdateCacheEntry(resourceTree types.ResourceTree,
	compositionId string,
	compositionReference types.Reference) {

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.cache[compositionId]; ok {
		c.cache[compositionId].LastUpdate = time.Now()
		c.cache[compositionId].ResourceTree = resourceTree
	}
}

func (c *ThreadSafeCache) GetJSONFromCache(compositionId string) ([]*types.ResourceNodeStatus, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	obj, ok := c.cache[compositionId]
	if ok {
		return obj.ResourceTree.Resources.Status, ok
	}
	return []*types.ResourceNodeStatus{}, ok
}

func (c *ThreadSafeCache) GetResourceTreeFromCache(compositionId string) (*ResourceTreeUpdate, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	obj, ok := c.cache[compositionId]
	if ok {
		return obj, ok
	}
	return &ResourceTreeUpdate{}, ok
}

func (c *ThreadSafeCache) DeleteFromCache(compositionId string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, compositionId)
}

func (c *ThreadSafeCache) ListKeysFromCache() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys := make([]string, 0, len(c.cache))
	for k := range c.cache {
		keys = append(keys, k)
	}
	return keys
}

func (c *ThreadSafeCache) IsUidInCache(compositionId string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for k := range c.cache {
		if k == compositionId {
			return true
		}
	}
	return false
}

// These functions are kept for backwards compatibility
func AddToCache(resourceTree types.ResourceTree, compositionId string, compositionReference types.Reference, filters types.Filters) {
	cache.AddToCache(resourceTree, compositionId, compositionReference, filters)
}

func UpdateCacheEntry(resourceTree types.ResourceTree, compositionId string, compositionReference types.Reference) {
	cache.UpdateCacheEntry(resourceTree, compositionId, compositionReference)
}

func GetJSONFromCache(compositionId string) ([]*types.ResourceNodeStatus, bool) {
	return cache.GetJSONFromCache(compositionId)
}

func GetResourceTreeFromCache(compositionId string) (*ResourceTreeUpdate, bool) {
	return cache.GetResourceTreeFromCache(compositionId)
}

func DeleteFromCache(compositionId string) {
	cache.DeleteFromCache(compositionId)
}

func ListKeysFromCache() []string {
	return cache.ListKeysFromCache()
}

func IsUidInCache(compositionId string) bool {
	return cache.IsUidInCache(compositionId)
}
