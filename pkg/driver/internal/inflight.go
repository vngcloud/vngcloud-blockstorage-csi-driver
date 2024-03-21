package internal

// InFlight is a struct used to manage in flight requests for a unique identifier.
type InFlight struct {
	mux      *sync.Mutex
	inFlight map[string]bool
}
