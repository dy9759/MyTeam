// Package middleware — per-sender rate limit for agent-to-agent messaging.
//
// See issue #78: POST /api/interactions was unbounded, so a misbehaving
// agent (compromised PAT, runaway loop, etc.) could flood inboxes and
// the WS broadcast fan-out. This middleware applies a per-sender token
// bucket in front of the send endpoint only — inbox reads and ack
// writes stay unthrottled since they don't amplify load the same way.
//
// Keyed by `{from_type}:{from_id}` so an agent and a user that happen
// to share an id (they don't, but the compound key is cheap defense)
// get independent budgets. In-memory only — fine for single-node; a
// horizontal deploy needs a shared store (Redis, etc.).
package middleware

import (
	"net/http"
	"sync"
	"time"
)

// Bucket parameters. Sized for the issue #78 default of ~60 msgs/min
// per sender, with a small burst allowance so a real agent that
// batches a handful of fan-out calls doesn't hit 429 on the second
// message. Tune here, not at call sites.
const (
	// interactionRefillPerSec is the steady-state allowance. 1.0 token
	// per second == 60 messages per minute.
	interactionRefillPerSec = 1.0
	// interactionBurst is the bucket capacity — how many tokens a
	// fresh sender starts with and the max it can accumulate.
	interactionBurst = 20.0
	// interactionIdleTTL is how long a bucket sticks around with no
	// activity before the GC sweeps it. Long enough that a sender
	// pausing briefly doesn't lose its partial refill; short enough
	// that abandoned buckets don't leak memory.
	interactionIdleTTL = 5 * time.Minute
	// interactionGCInterval is how often the GC runs.
	interactionGCInterval = 5 * time.Minute
)

// bucket is a simple token bucket. Access is serialized via the
// limiter mutex — no per-bucket lock because the critical section is
// a few arithmetic ops.
type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// RateLimiter holds the shared bucket map. A single instance is
// created at router setup time and reused across all requests.
type RateLimiter struct {
	mu          sync.Mutex
	buckets     map[string]*bucket
	refillRate  float64 // tokens per second
	burst       float64 // max tokens
	idleTTL     time.Duration
	now         func() time.Time // override in tests
	stopGC      chan struct{}
}

// NewInteractionRateLimiter builds a limiter with the constants above
// and starts the background GC goroutine. Callers should hold a
// reference for the server lifetime; there's no Close() because the
// process exits when the server stops.
func NewInteractionRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		buckets:    make(map[string]*bucket),
		refillRate: interactionRefillPerSec,
		burst:      interactionBurst,
		idleTTL:    interactionIdleTTL,
		now:        time.Now,
		stopGC:     make(chan struct{}),
	}
	go rl.gcLoop(interactionGCInterval)
	return rl
}

// allow returns true if the sender has a token to spend. If true, the
// token is deducted and the bucket timestamp is refreshed.
func (rl *RateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	b, ok := rl.buckets[key]
	if !ok {
		// New sender — full bucket minus the one token this request
		// spends.
		rl.buckets[key] = &bucket{
			tokens:   rl.burst - 1,
			lastSeen: now,
		}
		return true
	}

	// Refill based on elapsed time since lastSeen, capped at burst.
	elapsed := now.Sub(b.lastSeen).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * rl.refillRate
		if b.tokens > rl.burst {
			b.tokens = rl.burst
		}
	}
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// gcLoop periodically evicts buckets idle longer than idleTTL.
func (rl *RateLimiter) gcLoop(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			rl.sweep()
		case <-rl.stopGC:
			return
		}
	}
}

func (rl *RateLimiter) sweep() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := rl.now().Add(-rl.idleTTL)
	for k, b := range rl.buckets {
		if b.lastSeen.Before(cutoff) {
			delete(rl.buckets, k)
		}
	}
}

// InteractionRateLimit returns an http middleware that enforces the
// limiter against the sender resolved from request headers.
//
// Sender key priority:
//   1. X-Agent-ID — set when the caller is acting as an agent. This
//      matches handler.resolveActor's source of truth.
//   2. X-User-ID — set by Auth middleware for every authenticated req.
//   3. Remote address — fallback for the impossible case where both
//      headers are missing; keeps the limiter from becoming a bypass
//      if auth ordering ever regresses.
//
// On throttle: 429 + `Retry-After: 1` + JSON body matching the rest
// of the API's error shape.
func InteractionRateLimit(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := senderKey(r)
			if !rl.allow(key) {
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func senderKey(r *http.Request) string {
	if agentID := r.Header.Get("X-Agent-ID"); agentID != "" {
		return "agent:" + agentID
	}
	if userID := r.Header.Get("X-User-ID"); userID != "" {
		return "user:" + userID
	}
	return "addr:" + r.RemoteAddr
}
