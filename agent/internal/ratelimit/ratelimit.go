// internal/ratelimit/ratelimit.go
// In-memory per-service rate limiter.
//
// Prevents more than one remediation per service within a configurable window.
// The state is in-memory, so it resets when the agent restarts. This is
// acceptable for Step 9; a persistent rate limiter (Redis or MongoDB) can
// replace it in a later step without changing the interface.

package ratelimit

import (
	"fmt"
	"sync"
	"time"
)

// Limiter tracks the last remediation time per service.
type Limiter struct {
	mu      sync.Mutex
	window  time.Duration
	lastRun map[string]time.Time
}

// New creates a Limiter with the given window duration.
// A window of 15 minutes means: after one remediation, the next is blocked
// until 15 minutes have passed.
func New(window time.Duration) *Limiter {
	return &Limiter{
		window:  window,
		lastRun: make(map[string]time.Time),
	}
}

// Allow returns nil if a remediation is allowed for the given service.
// Returns an error with a retry-after duration if the rate limit is active.
func (l *Limiter) Allow(service string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	last, ok := l.lastRun[service]
	if !ok {
		return nil // no previous run — always allowed
	}

	elapsed := time.Since(last)
	if elapsed < l.window {
		remaining := l.window - elapsed
		return fmt.Errorf(
			"rate limit active for service %q: last remediation was %s ago, retry after %s",
			service,
			elapsed.Round(time.Second),
			remaining.Round(time.Second),
		)
	}
	return nil
}

// Record marks that a remediation has been applied for the given service.
// Call this immediately after a successful remediation, not before.
func (l *Limiter) Record(service string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastRun[service] = time.Now()
}

// Reset clears the rate limit for a service. Used in tests and after a
// successful rollback (the service is considered reset).
func (l *Limiter) Reset(service string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.lastRun, service)
}
