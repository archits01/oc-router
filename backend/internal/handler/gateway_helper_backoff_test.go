package handler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Task 6.2

func TestNextBackoff_ExponentialGrowth(t *testing.T) {
	//
	// ±20%），
	current := initialBackoff // 100ms

	for i := 0; i < 10; i++ {
		next := nextBackoff(current)

		// [initialBackoff, maxBackoff]
		assert.GreaterOrEqual(t, int64(next), int64(initialBackoff),
			"backoff %d should not be less than initial value %v", i, initialBackoff)
		assert.LessOrEqual(t, int64(next), int64(maxBackoff),
			"backoff %d should not exceed max value %v", i, maxBackoff)

		current = next
	}
}

func TestNextBackoff_BoundedByMaxBackoff(t *testing.T) {
	//
	for i := 0; i < 100; i++ {
		result := nextBackoff(10 * time.Second)
		assert.LessOrEqual(t, int64(result), int64(maxBackoff),
			"backoff value should not exceed maxBackoff")
	}
}

func TestNextBackoff_BoundedByInitialBackoff(t *testing.T) {
	//
	for i := 0; i < 100; i++ {
		result := nextBackoff(1 * time.Millisecond)
		assert.GreaterOrEqual(t, int64(result), int64(initialBackoff),
			"backoff value should not be less than initialBackoff")
	}
}

func TestNextBackoff_HasJitter(t *testing.T) {
	results := make(map[time.Duration]bool)
	current := 500 * time.Millisecond

	for i := 0; i < 50; i++ {
		result := nextBackoff(current)
		results[result] = true
	}

	// 50
	require.Greater(t, len(results), 1,
		"nextBackoff should produce random jitter, but all 50 calls returned identical results")
}

func TestNextBackoff_InitialValueGrows(t *testing.T) {
	current := initialBackoff
	var sum time.Duration

	runs := 100
	for i := 0; i < runs; i++ {
		next := nextBackoff(current)
		sum += next
		current = next
	}

	avg := sum / time.Duration(runs)
	assert.Greater(t, int64(avg), int64(initialBackoff),
		"average backoff time should be greater than initial backoff value")
}

func TestNextBackoff_ConvergesToMaxBackoff(t *testing.T) {
	//
	current := initialBackoff
	for i := 0; i < 20; i++ {
		current = nextBackoff(current)
	}

	//
	// ±20%
	lowerBound := time.Duration(float64(maxBackoff) * 0.8)
	assert.GreaterOrEqual(t, int64(current), int64(lowerBound),
		"after multiple backoffs should converge near maxBackoff")
}

func BenchmarkNextBackoff(b *testing.B) {
	current := initialBackoff
	for i := 0; i < b.N; i++ {
		current = nextBackoff(current)
		if current > maxBackoff {
			current = initialBackoff
		}
	}
}
