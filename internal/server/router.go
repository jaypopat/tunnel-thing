package server

import (
	"fmt"
	"sync"
)

// Router maps subdomain names to sessions. Thread-safe for concurrent
// lookups from the HTTP listener with occasional register/unregister
// from session goroutines.
type Router struct {
	mu     sync.RWMutex
	routes map[string]*session
}

func NewRouter() *Router {
	return &Router{
		routes: make(map[string]*session),
	}
}

// Register claims a subdomain name for a session. Returns an error if
// the name is already taken.
func (r *Router) Register(name string, s *session) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.routes[name]; exists {
		return fmt.Errorf("subdomain %q already in use", name)
	}
	r.routes[name] = s
	return nil
}

// Unregister releases a subdomain name.
func (r *Router) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.routes, name)
}

// Lookup finds the session registered for a subdomain name.
func (r *Router) Lookup(name string) (*session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.routes[name]
	return s, ok
}
