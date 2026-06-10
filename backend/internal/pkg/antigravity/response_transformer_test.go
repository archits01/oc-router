//go:build unit

package antigravity

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Task 7:

func TestGenerateRandomID_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id := generateRandomID()
		require.Len(t, id, 12, "ID length should be 12")
		_, dup := seen[id]
		require.False(t, dup, "call %d generated a duplicate ID: %s", i, id)
		seen[id] = struct{}{}
	}
}

func TestFallbackCounter_Increments(t *testing.T) {
	//
	before := atomic.LoadUint64(&fallbackCounter)
	cnt1 := atomic.AddUint64(&fallbackCounter, 1)
	cnt2 := atomic.AddUint64(&fallbackCounter, 1)
	require.Equal(t, before+1, cnt1, "first increment should be before+1")
	require.Equal(t, before+2, cnt2, "second increment should be before+2")
	require.NotEqual(t, cnt1, cnt2, "two consecutive increments should produce different counter values")
}

func TestFallbackCounter_ConcurrentIncrements(t *testing.T) {
	const goroutines = 50
	results := make([]uint64, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = atomic.AddUint64(&fallbackCounter, 1)
		}(i)
	}
	wg.Wait()

	seen := make(map[uint64]bool, goroutines)
	for _, v := range results {
		assert.False(t, seen[v], "concurrent increments produced duplicate value: %d", v)
		seen[v] = true
	}
}

func TestGenerateRandomID_Charset(t *testing.T) {
	const validChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	validSet := make(map[byte]struct{}, len(validChars))
	for i := 0; i < len(validChars); i++ {
		validSet[validChars[i]] = struct{}{}
	}

	for i := 0; i < 50; i++ {
		id := generateRandomID()
		for j := 0; j < len(id); j++ {
			_, ok := validSet[id[j]]
			require.True(t, ok, "ID contains invalid character: %c (ID=%s)", id[j], id)
		}
	}
}

func TestGenerateRandomID_Length(t *testing.T) {
	for i := 0; i < 100; i++ {
		id := generateRandomID()
		assert.Len(t, id, 12, "generated ID length should be 12")
	}
}

func TestGenerateRandomID_ConcurrentUniqueness(t *testing.T) {
	const goroutines = 100
	results := make([]string, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = generateRandomID()
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, goroutines)
	for _, id := range results {
		assert.False(t, seen[id], "concurrent calls produced duplicate ID: %s", id)
		seen[id] = true
	}
}
