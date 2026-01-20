package lcsc

import (
	"context"
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter.
type RateLimiter struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

// NewRateLimiter creates a new rate limiter with the specified requests per second.
func NewRateLimiter(requestsPerSecond float64) *RateLimiter {
	return &RateLimiter{
		tokens:     requestsPerSecond,
		maxTokens:  requestsPerSecond,
		refillRate: requestsPerSecond,
		lastRefill: time.Now(),
	}
}

// Wait blocks until a token is available or the context is cancelled.
func (r *RateLimiter) Wait(ctx context.Context) error {
	for {
		r.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(r.lastRefill).Seconds()
		r.tokens += elapsed * r.refillRate
		if r.tokens > r.maxTokens {
			r.tokens = r.maxTokens
		}
		r.lastRefill = now

		if r.tokens >= 1 {
			r.tokens--
			r.mu.Unlock()
			return nil
		}

		waitTime := time.Duration((1-r.tokens)/r.refillRate*1000) * time.Millisecond
		r.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			// continue loop
		}
	}
}
