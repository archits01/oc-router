package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock
// ---------------------------------------------------------------------------

// mockTempUnscheduler
type mockTempUnscheduler struct {
	calls []tempUnscheduleCall
}

type tempUnscheduleCall struct {
	accountID   int64
	failoverErr *service.UpstreamFailoverError
}

func (m *mockTempUnscheduler) TempUnscheduleRetryableError(_ context.Context, accountID int64, failoverErr *service.UpstreamFailoverError) {
	m.calls = append(m.calls, tempUnscheduleCall{accountID: accountID, failoverErr: failoverErr})
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func newTestFailoverErr(statusCode int, retryable, forceBilling bool) *service.UpstreamFailoverError {
	return &service.UpstreamFailoverError{
		StatusCode:             statusCode,
		RetryableOnSameAccount: retryable,
		ForceCacheBilling:      forceBilling,
	}
}

// ---------------------------------------------------------------------------
// NewFailoverState
// ---------------------------------------------------------------------------

func TestNewFailoverState(t *testing.T) {
	t.Run("initialization fields correct", func(t *testing.T) {
		fs := NewFailoverState(5, true)
		require.Equal(t, 5, fs.MaxSwitches)
		require.Equal(t, 0, fs.SwitchCount)
		require.NotNil(t, fs.FailedAccountIDs)
		require.Empty(t, fs.FailedAccountIDs)
		require.NotNil(t, fs.SameAccountRetryCount)
		require.Empty(t, fs.SameAccountRetryCount)
		require.Nil(t, fs.LastFailoverErr)
		require.False(t, fs.ForceCacheBilling)
		require.True(t, fs.hasBoundSession)
	})

	t.Run("no bound session", func(t *testing.T) {
		fs := NewFailoverState(3, false)
		require.Equal(t, 3, fs.MaxSwitches)
		require.False(t, fs.hasBoundSession)
	})

	t.Run("zero max switch count", func(t *testing.T) {
		fs := NewFailoverState(0, false)
		require.Equal(t, 0, fs.MaxSwitches)
	})
}

// ---------------------------------------------------------------------------
// sleepWithContext
// ---------------------------------------------------------------------------

func TestSleepWithContext(t *testing.T) {
	t.Run("zero duration immediately returns true", func(t *testing.T) {
		start := time.Now()
		ok := sleepWithContext(context.Background(), 0)
		require.True(t, ok)
		require.Less(t, time.Since(start), 50*time.Millisecond)
	})

	t.Run("negative duration immediately returns true", func(t *testing.T) {
		start := time.Now()
		ok := sleepWithContext(context.Background(), -1*time.Second)
		require.True(t, ok)
		require.Less(t, time.Since(start), 50*time.Millisecond)
	})

	t.Run("returns true after normal wait", func(t *testing.T) {
		start := time.Now()
		ok := sleepWithContext(context.Background(), 50*time.Millisecond)
		elapsed := time.Since(start)
		require.True(t, ok)
		require.GreaterOrEqual(t, elapsed, 40*time.Millisecond)
		require.Less(t, elapsed, 500*time.Millisecond)
	})

	t.Run("already cancelled context immediately returns false", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		start := time.Now()
		ok := sleepWithContext(ctx, 5*time.Second)
		require.False(t, ok)
		require.Less(t, time.Since(start), 50*time.Millisecond)
	})

	t.Run("context cancelled during wait returns false", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(30 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		ok := sleepWithContext(ctx, 5*time.Second)
		elapsed := time.Since(start)
		require.False(t, ok)
		require.Less(t, elapsed, 500*time.Millisecond)
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError —
// ---------------------------------------------------------------------------

func TestHandleFailoverError_BasicSwitch(t *testing.T) {
	t.Run("non-retry error - non-Antigravity - direct switch", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, false, false)

		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)

		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 1, fs.SwitchCount)
		require.Contains(t, fs.FailedAccountIDs, int64(100))
		require.Equal(t, err, fs.LastFailoverErr)
		require.False(t, fs.ForceCacheBilling)
		require.Empty(t, mock.calls, "should not call TempUnschedule")
	})

	t.Run("non-retry error - Antigravity - first switch no delay", func(t *testing.T) {
		// switchCount →1 (ctx, 1) = (1-1)*1s = 0
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, false, false)

		start := time.Now()
		action := fs.HandleFailoverError(context.Background(), mock, 100, service.PlatformAntigravity, err)
		elapsed := time.Since(start)

		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 1, fs.SwitchCount)
		require.Less(t, elapsed, 200*time.Millisecond, "first switch delay should be 0")
	})

	t.Run("non-retry error - Antigravity - second switch has 1s delay", func(t *testing.T) {
		// switchCount →2 (ctx, 2) = (2-1)*1s = 1s
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		fs.SwitchCount = 1 // simulate already switched once

		err := newTestFailoverErr(500, false, false)
		start := time.Now()
		action := fs.HandleFailoverError(context.Background(), mock, 200, service.PlatformAntigravity, err)
		elapsed := time.Since(start)

		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 2, fs.SwitchCount)
		require.GreaterOrEqual(t, elapsed, 800*time.Millisecond, "second switch delay should be about 1s")
		require.Less(t, elapsed, 3*time.Second)
	})

	t.Run("continuous switching until exhausted", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(2, false)

		// →1
		err1 := newTestFailoverErr(500, false, false)
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", err1)
		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 1, fs.SwitchCount)

		// →2
		err2 := newTestFailoverErr(502, false, false)
		action = fs.HandleFailoverError(context.Background(), mock, 200, "openai", err2)
		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 2, fs.SwitchCount)

		// (2) >= MaxSwitches(2)
		err3 := newTestFailoverErr(503, false, false)
		action = fs.HandleFailoverError(context.Background(), mock, 300, "openai", err3)
		require.Equal(t, FailoverExhausted, action)
		require.Equal(t, 2, fs.SwitchCount, "should not continue incrementing when exhausted")

		require.Len(t, fs.FailedAccountIDs, 3)
		require.Contains(t, fs.FailedAccountIDs, int64(100))
		require.Contains(t, fs.FailedAccountIDs, int64(200))
		require.Contains(t, fs.FailedAccountIDs, int64(300))

		// LastFailoverErr
		require.Equal(t, err3, fs.LastFailoverErr)
	})

	t.Run("MaxSwitches=0 means exhausted on first attempt", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(0, false)
		err := newTestFailoverErr(500, false, false)

		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		require.Equal(t, FailoverExhausted, action)
		require.Equal(t, 0, fs.SwitchCount)
		require.Contains(t, fs.FailedAccountIDs, int64(100))
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — (ForceCacheBilling)
// ---------------------------------------------------------------------------

func TestHandleFailoverError_CacheBilling(t *testing.T) {
	t.Run("set ForceCacheBilling when hasBoundSession is true", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, true) // hasBoundSession=true
		err := newTestFailoverErr(500, false, false)

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		require.True(t, fs.ForceCacheBilling)
	})

	t.Run("set when failoverErr.ForceCacheBilling is true", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, false, true) // ForceCacheBilling=true

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		require.True(t, fs.ForceCacheBilling)
	})

	t.Run("not set when both are false", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, false, false)

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		require.False(t, fs.ForceCacheBilling)
	})

	t.Run("once set, not reset by subsequent errors", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)

		// =true →
		err1 := newTestFailoverErr(500, false, true)
		fs.HandleFailoverError(context.Background(), mock, 100, "openai", err1)
		require.True(t, fs.ForceCacheBilling)

		// =false →
		err2 := newTestFailoverErr(502, false, false)
		fs.HandleFailoverError(context.Background(), mock, 200, "openai", err2)
		require.True(t, fs.ForceCacheBilling, "ForceCacheBilling should not be reset once set")
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — (RetryableOnSameAccount)
// ---------------------------------------------------------------------------

func TestHandleFailoverError_SameAccountRetry(t *testing.T) {
	t.Run("first retry returns FailoverContinue", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(400, true, false)

		start := time.Now()
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		elapsed := time.Since(start)

		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 1, fs.SameAccountRetryCount[100])
		require.Equal(t, 0, fs.SwitchCount, "same account retry should not increment switch count")
		require.NotContains(t, fs.FailedAccountIDs, int64(100), "same account retry should not add to failed list")
		require.Empty(t, mock.calls, "should not call TempUnschedule during same account retry")
		// (500ms)
		require.GreaterOrEqual(t, elapsed, 400*time.Millisecond)
		require.Less(t, elapsed, 2*time.Second)
	})

	t.Run("returns FailoverContinue before reaching max retry count", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(400, true, false)

		for i := 1; i <= maxSameAccountRetries; i++ {
			action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
			require.Equal(t, FailoverContinue, action)
			require.Equal(t, i, fs.SameAccountRetryCount[100])
		}

		require.Empty(t, mock.calls, "should not call TempUnschedule before reaching max retry count")
	})

	t.Run("triggers TempUnschedule and switches after exceeding max retry count", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(400, true, false)

		for i := 0; i < maxSameAccountRetries; i++ {
			fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		}
		require.Equal(t, maxSameAccountRetries, fs.SameAccountRetryCount[100])

		// +1
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 1, fs.SwitchCount)
		require.Contains(t, fs.FailedAccountIDs, int64(100))

		//
		require.Len(t, mock.calls, 1)
		require.Equal(t, int64(100), mock.calls[0].accountID)
		require.Equal(t, err, mock.calls[0].failoverErr)
	})

	t.Run("different accounts track retry count independently", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(5, false)
		err := newTestFailoverErr(400, true, false)

		//
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 1, fs.SameAccountRetryCount[100])

		//
		action = fs.HandleFailoverError(context.Background(), mock, 200, "openai", err)
		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 1, fs.SameAccountRetryCount[200])
		require.Equal(t, 1, fs.SameAccountRetryCount[100], "account 100 count should not be affected")
	})

	t.Run("directly switches when encountering same account after retry exhausted", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(5, false)
		err := newTestFailoverErr(400, true, false)

		//
		for i := 0; i < maxSameAccountRetries; i++ {
			fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		}
		// +1 →
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		require.Equal(t, FailoverContinue, action)

		// →
		action = fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		require.Equal(t, FailoverContinue, action)
		require.Len(t, mock.calls, 2, "second exhaustion should also call TempUnschedule")
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — TempUnschedule
// ---------------------------------------------------------------------------

func TestHandleFailoverError_TempUnschedule(t *testing.T) {
	t.Run("non-retry error does not call TempUnschedule", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, false, false) // RetryableOnSameAccount=false

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		require.Empty(t, mock.calls)
	})

	t.Run("calls TempUnschedule with correct params after retry error exhausted", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(502, true, false)

		for i := 0; i < maxSameAccountRetries; i++ {
			fs.HandleFailoverError(context.Background(), mock, 42, "openai", err)
		}
		// +
		fs.HandleFailoverError(context.Background(), mock, 42, "openai", err)

		require.Len(t, mock.calls, 1)
		require.Equal(t, int64(42), mock.calls[0].accountID)
		require.Equal(t, 502, mock.calls[0].failoverErr.StatusCode)
		require.True(t, mock.calls[0].failoverErr.RetryableOnSameAccount)
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — Context
// ---------------------------------------------------------------------------

func TestHandleFailoverError_ContextCanceled(t *testing.T) {
	t.Run("context cancelled during same account retry sleep", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(400, true, false)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // immediately cancelled

		start := time.Now()
		action := fs.HandleFailoverError(ctx, mock, 100, "openai", err)
		elapsed := time.Since(start)

		require.Equal(t, FailoverCanceled, action)
		require.Less(t, elapsed, 100*time.Millisecond, "should return immediately")
		require.Equal(t, 1, fs.SameAccountRetryCount[100])
	})

	t.Run("context cancelled during Antigravity delay", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		fs.SwitchCount = 1 // next switchCount=2 -> delay = 1s
		err := newTestFailoverErr(500, false, false)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // immediately cancelled

		start := time.Now()
		action := fs.HandleFailoverError(ctx, mock, 100, service.PlatformAntigravity, err)
		elapsed := time.Since(start)

		require.Equal(t, FailoverCanceled, action)
		require.Less(t, elapsed, 100*time.Millisecond, "should return immediately rather than wait 1s")
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — FailedAccountIDs
// ---------------------------------------------------------------------------

func TestHandleFailoverError_FailedAccountIDs(t *testing.T) {
	t.Run("added to failed list on switch", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", newTestFailoverErr(500, false, false))
		require.Contains(t, fs.FailedAccountIDs, int64(100))

		fs.HandleFailoverError(context.Background(), mock, 200, "openai", newTestFailoverErr(502, false, false))
		require.Contains(t, fs.FailedAccountIDs, int64(200))
		require.Len(t, fs.FailedAccountIDs, 2)
	})

	t.Run("also added to failed list on exhaustion", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(0, false)

		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", newTestFailoverErr(500, false, false))
		require.Equal(t, FailoverExhausted, action)
		require.Contains(t, fs.FailedAccountIDs, int64(100))
	})

	t.Run("not added to failed list during same account retry", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)

		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", newTestFailoverErr(400, true, false))
		require.Equal(t, FailoverContinue, action)
		require.NotContains(t, fs.FailedAccountIDs, int64(100))
	})

	t.Run("duplicate switches of same account not added twice", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(5, false)

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", newTestFailoverErr(500, false, false))
		fs.HandleFailoverError(context.Background(), mock, 100, "openai", newTestFailoverErr(500, false, false))
		require.Len(t, fs.FailedAccountIDs, 1, "map naturally deduplicates")
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — LastFailoverErr
// ---------------------------------------------------------------------------

func TestHandleFailoverError_LastFailoverErr(t *testing.T) {
	t.Run("every call updates LastFailoverErr", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)

		err1 := newTestFailoverErr(500, false, false)
		fs.HandleFailoverError(context.Background(), mock, 100, "openai", err1)
		require.Equal(t, err1, fs.LastFailoverErr)

		err2 := newTestFailoverErr(502, false, false)
		fs.HandleFailoverError(context.Background(), mock, 200, "openai", err2)
		require.Equal(t, err2, fs.LastFailoverErr)
	})

	t.Run("same account retry also updates LastFailoverErr", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)

		err := newTestFailoverErr(400, true, false)
		fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		require.Equal(t, err, fs.LastFailoverErr)
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError —
// ---------------------------------------------------------------------------

func TestHandleFailoverError_IntegrationScenario(t *testing.T) {
	t.Run("simulate full failover flow - multi-account mixed retry and switch", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, true) // hasBoundSession=true

		// 1.
		retryErr := newTestFailoverErr(400, true, false)
		for i := 0; i < maxSameAccountRetries; i++ {
			action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", retryErr)
			require.Equal(t, FailoverContinue, action)
		}
		require.True(t, fs.ForceCacheBilling, "hasBoundSession=true should set ForceCacheBilling")

		// 2. → TempUnschedule +
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", retryErr)
		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 1, fs.SwitchCount)
		require.Len(t, mock.calls, 1)

		// 3. →
		switchErr := newTestFailoverErr(500, false, false)
		action = fs.HandleFailoverError(context.Background(), mock, 200, "openai", switchErr)
		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 2, fs.SwitchCount)

		// 4. →
		action = fs.HandleFailoverError(context.Background(), mock, 300, "openai", switchErr)
		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 3, fs.SwitchCount)

		// 5. → (SwitchCount=3 >= MaxSwitches=3)
		action = fs.HandleFailoverError(context.Background(), mock, 400, "openai", switchErr)
		require.Equal(t, FailoverExhausted, action)

		require.Equal(t, 3, fs.SwitchCount, "no longer increments when exhausted")
		require.Len(t, fs.FailedAccountIDs, 4, "all 4 different accounts are in the failed list")
		require.True(t, fs.ForceCacheBilling)
		require.Len(t, mock.calls, 1, "only account 100 triggered TempUnschedule")
	})

	t.Run("simulate Antigravity platform full flow", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(2, false)

		err := newTestFailoverErr(500, false, false)

		// = 0s
		start := time.Now()
		action := fs.HandleFailoverError(context.Background(), mock, 100, service.PlatformAntigravity, err)
		elapsed := time.Since(start)
		require.Equal(t, FailoverContinue, action)
		require.Less(t, elapsed, 200*time.Millisecond, "first switch delay is 0")

		// = 1s
		start = time.Now()
		action = fs.HandleFailoverError(context.Background(), mock, 200, service.PlatformAntigravity, err)
		elapsed = time.Since(start)
		require.Equal(t, FailoverContinue, action)
		require.GreaterOrEqual(t, elapsed, 800*time.Millisecond, "second switch delay about 1s")

		start = time.Now()
		action = fs.HandleFailoverError(context.Background(), mock, 300, service.PlatformAntigravity, err)
		elapsed = time.Since(start)
		require.Equal(t, FailoverExhausted, action)
		require.Less(t, elapsed, 200*time.Millisecond, "should not have delay on exhaustion")
	})

	t.Run("ForceCacheBilling set via error flag", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false) // hasBoundSession=false

		// =false
		err1 := newTestFailoverErr(500, false, false)
		fs.HandleFailoverError(context.Background(), mock, 100, "openai", err1)
		require.False(t, fs.ForceCacheBilling)

		// =true（Antigravity
		err2 := newTestFailoverErr(500, false, true)
		fs.HandleFailoverError(context.Background(), mock, 200, "openai", err2)
		require.True(t, fs.ForceCacheBilling, "error flag should trigger ForceCacheBilling")

		// =false，
		err3 := newTestFailoverErr(500, false, false)
		fs.HandleFailoverError(context.Background(), mock, 300, "openai", err3)
		require.True(t, fs.ForceCacheBilling, "should not be reset")
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError —
// ---------------------------------------------------------------------------

func TestHandleFailoverError_EdgeCases(t *testing.T) {
	t.Run("error with StatusCode=0 also handled correctly", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(0, false, false)

		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", err)
		require.Equal(t, FailoverContinue, action)
	})

	t.Run("AccountID=0 also tracked correctly", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, true, false)

		action := fs.HandleFailoverError(context.Background(), mock, 0, "openai", err)
		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 1, fs.SameAccountRetryCount[0])
	})

	t.Run("negative AccountID also tracked correctly", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, true, false)

		action := fs.HandleFailoverError(context.Background(), mock, -1, "openai", err)
		require.Equal(t, FailoverContinue, action)
		require.Equal(t, 1, fs.SameAccountRetryCount[-1])
	})

	t.Run("empty platform name does not trigger Antigravity delay", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		fs.SwitchCount = 1
		err := newTestFailoverErr(500, false, false)

		start := time.Now()
		action := fs.HandleFailoverError(context.Background(), mock, 100, "", err)
		elapsed := time.Since(start)

		require.Equal(t, FailoverContinue, action)
		require.Less(t, elapsed, 200*time.Millisecond, "empty platform should not trigger Antigravity delay")
	})
}

// ---------------------------------------------------------------------------
// HandleSelectionExhausted
// ---------------------------------------------------------------------------

func TestHandleSelectionExhausted(t *testing.T) {
	t.Run("returns Exhausted when no LastFailoverErr", func(t *testing.T) {
		fs := NewFailoverState(3, false)
		// LastFailoverErr

		action := fs.HandleSelectionExhausted(context.Background())
		require.Equal(t, FailoverExhausted, action)
	})

	t.Run("non-503 error returns Exhausted", func(t *testing.T) {
		fs := NewFailoverState(3, false)
		fs.LastFailoverErr = newTestFailoverErr(500, false, false)

		action := fs.HandleSelectionExhausted(context.Background())
		require.Equal(t, FailoverExhausted, action)
	})

	t.Run("503 and not exhausted - returns Continue after wait and clears failed list", func(t *testing.T) {
		fs := NewFailoverState(3, false)
		fs.LastFailoverErr = newTestFailoverErr(503, false, false)
		fs.FailedAccountIDs[100] = struct{}{}
		fs.SwitchCount = 1

		start := time.Now()
		action := fs.HandleSelectionExhausted(context.Background())
		elapsed := time.Since(start)

		require.Equal(t, FailoverContinue, action)
		require.Empty(t, fs.FailedAccountIDs, "should clear failed account list")
		require.GreaterOrEqual(t, elapsed, 1500*time.Millisecond, "should wait about 2s")
		require.Less(t, elapsed, 5*time.Second)
	})

	t.Run("503 but SwitchCount exceeds MaxSwitches - returns Exhausted", func(t *testing.T) {
		fs := NewFailoverState(2, false)
		fs.LastFailoverErr = newTestFailoverErr(503, false, false)
		fs.SwitchCount = 3 // > MaxSwitches(2)

		start := time.Now()
		action := fs.HandleSelectionExhausted(context.Background())
		elapsed := time.Since(start)

		require.Equal(t, FailoverExhausted, action)
		require.Less(t, elapsed, 100*time.Millisecond, "should not wait")
	})

	t.Run("503 but context cancelled - returns Canceled", func(t *testing.T) {
		fs := NewFailoverState(3, false)
		fs.LastFailoverErr = newTestFailoverErr(503, false, false)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		start := time.Now()
		action := fs.HandleSelectionExhausted(ctx)
		elapsed := time.Since(start)

		require.Equal(t, FailoverCanceled, action)
		require.Less(t, elapsed, 100*time.Millisecond, "should return immediately")
	})

	t.Run("503 and SwitchCount equals MaxSwitches - can still retry", func(t *testing.T) {
		fs := NewFailoverState(2, false)
		fs.LastFailoverErr = newTestFailoverErr(503, false, false)
		fs.SwitchCount = 2 // == MaxSwitches, condition is <=, can still retry

		action := fs.HandleSelectionExhausted(context.Background())
		require.Equal(t, FailoverContinue, action)
	})
}
