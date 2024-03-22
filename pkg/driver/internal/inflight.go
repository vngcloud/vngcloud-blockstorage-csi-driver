package internal

import "sync"

// InFlight is a struct used to manage in flight requests for a unique identifier.
type InFlight struct {
	mux      *sync.Mutex
	inFlight map[string]bool
}

// NewInFlight instanciates a InFlight structures.
func NewInFlight() *InFlight {
	return &InFlight{
		mux:      &sync.Mutex{},
		inFlight: make(map[string]bool),
	}
}
