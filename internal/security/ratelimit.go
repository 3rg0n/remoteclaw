package security

import (
	"sync"
	"time"
)

// RateLimiter implements per-space token-bucket rate limiting.
// Automatically cleans up stale entries.
type RateLimiter struct {
	maxPerMinute int
	burstSize    int
	buckets      sync.Map // map[string]*bucket
}

type bucket struct {
	mu        sync.Mutex
	tokens    float64
	lastCheck time.Time
}

// NewRateLimiter creates a rate limiter allowing maxPerMinute requests per minute
// with an initial burst of burstSize.
func NewRateLimiter(maxPerMinute int, burstSize int) *RateLimiter {
	rl := &RateLimiter{
		maxPerMinute: maxPerMinute,
		burstSize:    burstSize,
	}
	// Start a background cleanup goroutine
	go rl.cleanupLoop()
	return rl
}

// Allow checks whether a request for the given spaceID is permitted.
// Returns true if allowed, false if rate-limited.
func (rl *RateLimiter) Allow(spaceID string) bool {
	val, _ := rl.buckets.LoadOrStore(spaceID, &bucket{
		tokens:    float64(rl.burstSize),
		lastCheck: time.Now(),
	})
	b := val.(*bucket)

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.lastCheck = now

	// Refill tokens based on elapsed time
	rate := float64(rl.maxPerMinute) / 60.0
	b.tokens += elapsed * rate
	if b.tokens > float64(rl.burstSize) {
		b.tokens = float64(rl.burstSize)
	}

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

// cleanupLoop removes entries that haven't been used for over 10 minutes.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cutoff := time.Now().Add(-10 * time.Minute)
		rl.buckets.Range(func(key, value any) bool {
			b := value.(*bucket)
			b.mu.Lock()
			stale := b.lastCheck.Before(cutoff)
			b.mu.Unlock()
			if stale {
				rl.buckets.Delete(key)
			}
			return true
		})
	}
}
