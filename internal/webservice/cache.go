package webservice

import (
	"time"
)

type Cache struct {
	Compositions map[string]CompositionId
}

type CompositionId struct {
	LastUpdate time.Time
	Resources  []Resource
}
