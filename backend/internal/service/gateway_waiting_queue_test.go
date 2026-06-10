//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDecrementWaitCount_NilCache
func TestDecrementWaitCount_NilCache(t *testing.T) {
	svc := &ConcurrencyService{cache: nil}
	//
	svc.DecrementWaitCount(context.Background(), 1)
}

// TestDecrementWaitCount_CacheError
func TestDecrementWaitCount_CacheError(t *testing.T) {
	cache := &stubConcurrencyCacheForTest{}
	svc := NewConcurrencyService(cache)
	// DecrementWaitCount
	svc.DecrementWaitCount(context.Background(), 1)
}

// TestDecrementAccountWaitCount_NilCache
func TestDecrementAccountWaitCount_NilCache(t *testing.T) {
	svc := &ConcurrencyService{cache: nil}
	svc.DecrementAccountWaitCount(context.Background(), 1)
}

// TestDecrementAccountWaitCount_CacheError
func TestDecrementAccountWaitCount_CacheError(t *testing.T) {
	cache := &stubConcurrencyCacheForTest{}
	svc := NewConcurrencyService(cache)
	svc.DecrementAccountWaitCount(context.Background(), 1)
}

// TestWaitingQueueFlow_IncrementThenDecrement
func TestWaitingQueueFlow_IncrementThenDecrement(t *testing.T) {
	cache := &stubConcurrencyCacheForTest{waitAllowed: true}
	svc := NewConcurrencyService(cache)

	allowed, err := svc.IncrementWaitCount(context.Background(), 1, 25)
	require.NoError(t, err)
	require.True(t, allowed)

	//
	svc.DecrementWaitCount(context.Background(), 1)
}

// TestWaitingQueueFlow_AccountLevel
func TestWaitingQueueFlow_AccountLevel(t *testing.T) {
	cache := &stubConcurrencyCacheForTest{waitAllowed: true}
	svc := NewConcurrencyService(cache)

	allowed, err := svc.IncrementAccountWaitCount(context.Background(), 42, 10)
	require.NoError(t, err)
	require.True(t, allowed)

	svc.DecrementAccountWaitCount(context.Background(), 42)
}

// TestWaitingQueueFull_Returns429Signal
func TestWaitingQueueFull_Returns429Signal(t *testing.T) {
	// waitAllowed=false
	cache := &stubConcurrencyCacheForTest{waitAllowed: false}
	svc := NewConcurrencyService(cache)

	allowed, err := svc.IncrementWaitCount(context.Background(), 1, 25)
	require.NoError(t, err)
	require.False(t, allowed, "should return false when wait queue is full (caller returns 429 based on this)")

	allowed, err = svc.IncrementAccountWaitCount(context.Background(), 1, 10)
	require.NoError(t, err)
	require.False(t, allowed, "should return false when account wait queue is full")
}

// TestWaitingQueue_FailOpen_OnCacheError
func TestWaitingQueue_FailOpen_OnCacheError(t *testing.T) {
	cache := &stubConcurrencyCacheForTest{waitErr: errors.New("redis connection refused")}
	svc := NewConcurrencyService(cache)

	//
	allowed, err := svc.IncrementWaitCount(context.Background(), 1, 25)
	require.NoError(t, err, "Redis error should not propagate to caller")
	require.True(t, allowed, "should fail-open when Redis is down")

	//
	allowed, err = svc.IncrementAccountWaitCount(context.Background(), 1, 10)
	require.NoError(t, err, "Redis error should not propagate to caller")
	require.True(t, allowed, "should fail-open when Redis is down")
}

// TestCalculateMaxWait_Scenarios
func TestCalculateMaxWait_Scenarios(t *testing.T) {
	tests := []struct {
		concurrency int
		expected    int
	}{
		{5, 25},    // 5 + 20
		{10, 30},   // 10 + 20
		{1, 21},    // 1 + 20
		{0, 21},    // min(1) + 20
		{-1, 21},   // min(1) + 20
		{-10, 21},  // min(1) + 20
		{100, 120}, // 100 + 20
	}
	for _, tt := range tests {
		result := CalculateMaxWait(tt.concurrency)
		require.Equal(t, tt.expected, result, "CalculateMaxWait(%d)", tt.concurrency)
	}
}
