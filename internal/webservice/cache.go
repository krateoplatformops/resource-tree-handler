package webservice

import (
	"time"
)

type ResourceTreeUpdate struct {
	LastUpdate   time.Time
	ResourceTree string
}

var (
	cache map[string]ResourceTreeUpdate
)

func init() {
	cache = map[string]ResourceTreeUpdate{}
}

func AddToCache(resourceTree string, compositionId string) {
	delete(cache, compositionId)
	cache[compositionId] = ResourceTreeUpdate{
		LastUpdate:   time.Now(),
		ResourceTree: resourceTree,
	}
}

func GetFromCache(compositionId string) (string, bool) {
	obj, ok := cache[compositionId]
	if ok {
		return obj.ResourceTree, ok
	} else {
		return "", ok
	}
}

func DeleteFromCache(compositionId string) {
	delete(cache, compositionId)
}
