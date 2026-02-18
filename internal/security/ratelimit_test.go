package security

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateLimiter_AllowsBurst(t *testing.T) {
	rl := NewRateLimiter(10, 3)

	// Burst of 3 should be allowed
	assert.True(t, rl.Allow("space-1"))
	assert.True(t, rl.Allow("space-1"))
	assert.True(t, rl.Allow("space-1"))

	// Fourth should be blocked (burst exhausted, not enough time for refill)
	assert.False(t, rl.Allow("space-1"))
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	rl := NewRateLimiter(60, 1) // 1 per second, burst of 1

	assert.True(t, rl.Allow("space-1"))
	assert.False(t, rl.Allow("space-1"))

	// Wait for refill
	time.Sleep(1100 * time.Millisecond)

	assert.True(t, rl.Allow("space-1"))
}

func TestRateLimiter_IndependentSpaces(t *testing.T) {
	rl := NewRateLimiter(10, 2)

	// Each space has independent limits
	assert.True(t, rl.Allow("space-a"))
	assert.True(t, rl.Allow("space-a"))
	assert.False(t, rl.Allow("space-a"))

	// space-b should still be allowed
	assert.True(t, rl.Allow("space-b"))
	assert.True(t, rl.Allow("space-b"))
	assert.False(t, rl.Allow("space-b"))
}

func TestRateLimiter_Concurrent(t *testing.T) {
	rl := NewRateLimiter(1000, 100)

	var wg sync.WaitGroup
	allowed := make(chan bool, 200)

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- rl.Allow("space-concurrent")
		}()
	}

	wg.Wait()
	close(allowed)

	// Count allowed requests - should be around 100 (burst size)
	count := 0
	for a := range allowed {
		if a {
			count++
		}
	}

	assert.LessOrEqual(t, count, 101, "should not exceed burst + 1")
	assert.GreaterOrEqual(t, count, 90, "most of burst should succeed")
}
