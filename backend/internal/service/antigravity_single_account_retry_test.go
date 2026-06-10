//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
//
// ---------------------------------------------------------------------------

func ctxWithSingleAccountRetry() context.Context {
	return context.WithValue(context.Background(), ctxkey.SingleAccountRetry, true)
}

// ---------------------------------------------------------------------------
// 1. isSingleAccountRetry
// ---------------------------------------------------------------------------

func TestIsSingleAccountRetry_True(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.SingleAccountRetry, true)
	require.True(t, isSingleAccountRetry(ctx))
}

func TestIsSingleAccountRetry_False_NoValue(t *testing.T) {
	require.False(t, isSingleAccountRetry(context.Background()))
}

func TestIsSingleAccountRetry_False_ExplicitFalse(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.SingleAccountRetry, false)
	require.False(t, isSingleAccountRetry(ctx))
}

func TestIsSingleAccountRetry_False_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.SingleAccountRetry, "true")
	require.False(t, isSingleAccountRetry(ctx))
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

func TestSingleAccountRetryConstants(t *testing.T) {
	require.Equal(t, 3, antigravitySingleAccountSmartRetryMaxAttempts,
		"单账号原地retry最多 3 次")
	require.Equal(t, 15*time.Second, antigravitySingleAccountSmartRetryMaxWait,
		"单次最大等待 15s")
	require.Equal(t, 30*time.Second, antigravitySingleAccountSmartRetryTotalMaxWait,
		"总累计等待不超过 30s")
}

// ---------------------------------------------------------------------------
// 3. handleSmartRetry + 503 + SingleAccountRetry →
// ---------------------------------------------------------------------------

// TestHandleSmartRetry_503_LongDelay_SingleAccountRetry_RetryInPlace
// + retryDelay >= 7s + SingleAccountRetry
func TestHandleSmartRetry_503_LongDelay_SingleAccountRetry_RetryInPlace(t *testing.T) {
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{successResp},
		errors:    []error{nil},
	}

	repo := &stubAntigravityAccountRepo{}
	account := &Account{
		ID:          1,
		Name:        "acc-single",
		Type:        AccountTypeOAuth,
		Platform:    PlatformAntigravity,
		Concurrency: 1,
	}

	// 503 + 39s >= 7s + MODEL_CAPACITY_EXHAUSTED
	respBody := []byte(`{
		"error": {
			"code": 503,
			"status": "UNAVAILABLE",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro-high"}, "reason": "MODEL_CAPACITY_EXHAUSTED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "39s"}
			],
			"message": "No capacity available for model gemini-3-pro-high on the server"
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := antigravityRetryLoopParams{
		ctx:          ctxWithSingleAccountRetry(), // 关键：设置单账号标记
		prefix:       "[test]",
		account:      account,
		accessToken:  "token",
		action:       "generateContent",
		body:         []byte(`{"input":"test"}`),
		httpUpstream: upstream,
		accountRepo:  repo,
		handleError: func(ctx context.Context, prefix string, account *Account, statusCode int, headers http.Header, body []byte, requestedModel string, groupID int64, sessionHash string, isStickySession bool) *handleModelRateLimitResult {
			return nil
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := &AntigravityGatewayService{}
	result := svc.handleSmartRetry(params, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	//
	require.NotNil(t, result.resp, "should return successful response from in-place retry")
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
	require.Nil(t, result.switchError, "should NOT return switchError in single account mode")
	require.Nil(t, result.err)

	require.Len(t, repo.modelRateLimitCalls, 0,
		"should NOT set model rate limit in single account retry mode")

	//
	require.GreaterOrEqual(t, len(upstream.calls), 1, "should have made at least one retry call")
}

// TestHandleSmartRetry_503_LongDelay_NoSingleAccountRetry_StillSwitches
// + retryDelay >= 7s +
// → +
func TestHandleSmartRetry_503_LongDelay_NoSingleAccountRetry_StillSwitches(t *testing.T) {
	repo := &stubAntigravityAccountRepo{}
	account := &Account{
		ID:       2,
		Name:     "acc-multi",
		Type:     AccountTypeOAuth,
		Platform: PlatformAntigravity,
	}

	// 503 + 39s >= 7s
	//
	respBody := []byte(`{
		"error": {
			"code": 503,
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro-high"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "39s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := antigravityRetryLoopParams{
		ctx:         context.Background(), // 关键：无单账号标记
		prefix:      "[test]",
		account:     account,
		accessToken: "token",
		action:      "generateContent",
		body:        []byte(`{"input":"test"}`),
		accountRepo: repo,
		handleError: func(ctx context.Context, prefix string, account *Account, statusCode int, headers http.Header, body []byte, requestedModel string, groupID int64, sessionHash string, isStickySession bool) *handleModelRateLimitResult {
			return nil
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := &AntigravityGatewayService{}
	result := svc.handleSmartRetry(params, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	//
	require.NotNil(t, result.switchError, "multi-account mode should return switchError for 503")
	require.Nil(t, result.resp, "should not return resp when switchError is set")

	require.Len(t, repo.modelRateLimitCalls, 2,
		"multi-account mode SHOULD set model rate limit")
	require.Equal(t, "gemini-3-pro-high", repo.modelRateLimitCalls[0].modelKey)
	require.Equal(t, antigravityGeminiModelRateLimitKey, repo.modelRateLimitCalls[1].modelKey)
}

// TestHandleSmartRetry_429_LongDelay_SingleAccountRetry_StillSwitches
// + SingleAccountRetry
// →
func TestHandleSmartRetry_429_LongDelay_SingleAccountRetry_StillSwitches(t *testing.T) {
	repo := &stubAntigravityAccountRepo{}
	account := &Account{
		ID:       3,
		Name:     "acc-429",
		Type:     AccountTypeOAuth,
		Platform: PlatformAntigravity,
	}

	// 429 + 15s >= 7s
	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-sonnet-4-5"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "15s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests, // 429，不是 503
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := antigravityRetryLoopParams{
		ctx:         ctxWithSingleAccountRetry(), // 有单账号标记
		prefix:      "[test]",
		account:     account,
		accessToken: "token",
		action:      "generateContent",
		body:        []byte(`{"input":"test"}`),
		accountRepo: repo,
		handleError: func(ctx context.Context, prefix string, account *Account, statusCode int, headers http.Header, body []byte, requestedModel string, groupID int64, sessionHash string, isStickySession bool) *handleModelRateLimitResult {
			return nil
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := &AntigravityGatewayService{}
	result := svc.handleSmartRetry(params, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	// 429
	require.NotNil(t, result.switchError, "429 should still return switchError even with SingleAccountRetry")
	require.Len(t, repo.modelRateLimitCalls, 1,
		"429 should still set model rate limit even with SingleAccountRetry")
}

// ---------------------------------------------------------------------------
// 4. handleSmartRetry + 503 + + SingleAccountRetry →
// ---------------------------------------------------------------------------

// TestHandleSmartRetry_503_ShortDelay_SingleAccountRetry_NoRateLimit
// 503 + retryDelay < 7s + SingleAccountRetry →
//
func TestHandleSmartRetry_503_ShortDelay_SingleAccountRetry_NoRateLimit(t *testing.T) {
	//
	failRespBody := `{
		"error": {
			"code": 503,
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`
	failResp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(failRespBody)),
	}
	upstream := &mockSmartRetryUpstream{
		responses:  []*http.Response{failResp},
		errors:     []error{nil},
		repeatLast: true,
	}

	repo := &stubAntigravityAccountRepo{}
	account := &Account{
		ID:       4,
		Name:     "acc-short-503",
		Type:     AccountTypeOAuth,
		Platform: PlatformAntigravity,
	}

	// 0.1s < 7s
	respBody := []byte(`{
		"error": {
			"code": 503,
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := antigravityRetryLoopParams{
		ctx:          ctxWithSingleAccountRetry(),
		prefix:       "[test]",
		account:      account,
		accessToken:  "token",
		action:       "generateContent",
		body:         []byte(`{"input":"test"}`),
		httpUpstream: upstream,
		accountRepo:  repo,
		handleError: func(ctx context.Context, prefix string, account *Account, statusCode int, headers http.Header, body []byte, requestedModel string, groupID int64, sessionHash string, isStickySession bool) *handleModelRateLimitResult {
			return nil
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := &AntigravityGatewayService{}
	result := svc.handleSmartRetry(params, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	//
	require.NotNil(t, result.resp, "should return 503 response directly for single account mode")
	require.Equal(t, http.StatusServiceUnavailable, result.resp.StatusCode)
	require.Nil(t, result.switchError, "should NOT switch account in single account mode")

	require.Len(t, repo.modelRateLimitCalls, 0,
		"should NOT set model rate limit for 503 in single account mode")
}

// TestHandleSmartRetry_503_ShortDelay_NoSingleAccountRetry_SetsRateLimit
// + retryDelay < 7s + →
//
func TestHandleSmartRetry_503_ShortDelay_NoSingleAccountRetry_SetsRateLimit(t *testing.T) {
	failRespBody := `{
		"error": {
			"code": 503,
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`
	failResp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(failRespBody)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{failResp},
		errors:    []error{nil},
	}

	repo := &stubAntigravityAccountRepo{}
	account := &Account{
		ID:       5,
		Name:     "acc-multi-503",
		Type:     AccountTypeOAuth,
		Platform: PlatformAntigravity,
	}

	respBody := []byte(`{
		"error": {
			"code": 503,
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := antigravityRetryLoopParams{
		ctx:          context.Background(), // 无单账号标记
		prefix:       "[test]",
		account:      account,
		accessToken:  "token",
		action:       "generateContent",
		body:         []byte(`{"input":"test"}`),
		httpUpstream: upstream,
		accountRepo:  repo,
		handleError: func(ctx context.Context, prefix string, account *Account, statusCode int, headers http.Header, body []byte, requestedModel string, groupID int64, sessionHash string, isStickySession bool) *handleModelRateLimitResult {
			return nil
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := &AntigravityGatewayService{}
	result := svc.handleSmartRetry(params, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	//
	require.NotNil(t, result.switchError, "multi-account mode should return switchError for 503")
	require.Len(t, repo.modelRateLimitCalls, 2,
		"multi-account mode should set model rate limit")
	require.Equal(t, "gemini-3-flash", repo.modelRateLimitCalls[0].modelKey)
	require.Equal(t, antigravityGeminiModelRateLimitKey, repo.modelRateLimitCalls[1].modelKey)
}

// ---------------------------------------------------------------------------
// 5. handleSingleAccountRetryInPlace
// ---------------------------------------------------------------------------

// TestHandleSingleAccountRetryInPlace_Success
func TestHandleSingleAccountRetryInPlace_Success(t *testing.T) {
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{successResp},
		errors:    []error{nil},
	}

	account := &Account{
		ID:          10,
		Name:        "acc-inplace-ok",
		Type:        AccountTypeOAuth,
		Platform:    PlatformAntigravity,
		Concurrency: 1,
	}

	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
	}

	params := antigravityRetryLoopParams{
		ctx:          ctxWithSingleAccountRetry(),
		prefix:       "[test]",
		account:      account,
		accessToken:  "token",
		action:       "generateContent",
		body:         []byte(`{"input":"test"}`),
		httpUpstream: upstream,
	}

	svc := &AntigravityGatewayService{}
	result := svc.handleSingleAccountRetryInPlace(params, resp, nil, "https://ag-1.test", 1*time.Second, "gemini-3-pro")

	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	require.NotNil(t, result.resp, "should return successful response")
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
	require.Nil(t, result.switchError, "should not switch account on success")
	require.Nil(t, result.err)
}

// TestHandleSingleAccountRetryInPlace_AllRetriesFail
func TestHandleSingleAccountRetryInPlace_AllRetriesFail(t *testing.T) {
	//
	var responses []*http.Response
	var errors []error
	for i := 0; i < antigravitySingleAccountSmartRetryMaxAttempts; i++ {
		responses = append(responses, &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{},
			Body: io.NopCloser(strings.NewReader(`{
				"error": {
					"code": 503,
					"status": "UNAVAILABLE",
					"details": [
						{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro"}, "reason": "MODEL_CAPACITY_EXHAUSTED"},
						{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
					]
				}
			}`)),
		})
		errors = append(errors, nil)
	}
	upstream := &mockSmartRetryUpstream{
		responses: responses,
		errors:    errors,
	}

	account := &Account{
		ID:          11,
		Name:        "acc-inplace-fail",
		Type:        AccountTypeOAuth,
		Platform:    PlatformAntigravity,
		Concurrency: 1,
	}

	origBody := []byte(`{"error":{"code":503,"status":"UNAVAILABLE"}}`)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{"X-Test": {"original"}},
	}

	params := antigravityRetryLoopParams{
		ctx:          ctxWithSingleAccountRetry(),
		prefix:       "[test]",
		account:      account,
		accessToken:  "token",
		action:       "generateContent",
		body:         []byte(`{"input":"test"}`),
		httpUpstream: upstream,
	}

	svc := &AntigravityGatewayService{}
	result := svc.handleSingleAccountRetryInPlace(params, resp, origBody, "https://ag-1.test", 1*time.Second, "gemini-3-pro")

	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	//
	require.NotNil(t, result.resp, "should return 503 response directly")
	require.Equal(t, http.StatusServiceUnavailable, result.resp.StatusCode)
	require.Nil(t, result.switchError, "should NOT return switchError - let Handler handle it")
	require.Nil(t, result.err)

	require.Len(t, upstream.calls, antigravitySingleAccountSmartRetryMaxAttempts,
		"should have made exactly maxAttempts retry calls")
}

// TestHandleSingleAccountRetryInPlace_WaitDurationClamped [min, max]
func TestHandleSingleAccountRetryInPlace_WaitDurationClamped(t *testing.T) {
	//
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{successResp},
		errors:    []error{nil},
	}

	account := &Account{
		ID:          12,
		Name:        "acc-clamp",
		Type:        AccountTypeOAuth,
		Platform:    PlatformAntigravity,
		Concurrency: 1,
	}

	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
	}

	params := antigravityRetryLoopParams{
		ctx:          ctxWithSingleAccountRetry(),
		prefix:       "[test]",
		account:      account,
		accessToken:  "token",
		action:       "generateContent",
		body:         []byte(`{"input":"test"}`),
		httpUpstream: upstream,
	}

	svc := &AntigravityGatewayService{}

	// waitDuration=0 =1s。
	// ~1s。
	result := svc.handleSingleAccountRetryInPlace(params, resp, nil, "https://ag-1.test", 0, "gemini-3-pro")
	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	require.NotNil(t, result.resp)
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
}

// TestHandleSingleAccountRetryInPlace_ContextCanceled context
func TestHandleSingleAccountRetryInPlace_ContextCanceled(t *testing.T) {
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{nil},
		errors:    []error{nil},
	}

	account := &Account{
		ID:          13,
		Name:        "acc-cancel",
		Type:        AccountTypeOAuth,
		Platform:    PlatformAntigravity,
		Concurrency: 1,
	}

	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, ctxkey.SingleAccountRetry, true)
	cancel() // immediately cancelled

	params := antigravityRetryLoopParams{
		ctx:          ctx,
		prefix:       "[test]",
		account:      account,
		accessToken:  "token",
		action:       "generateContent",
		body:         []byte(`{"input":"test"}`),
		httpUpstream: upstream,
	}

	svc := &AntigravityGatewayService{}
	result := svc.handleSingleAccountRetryInPlace(params, resp, nil, "https://ag-1.test", 1*time.Second, "gemini-3-pro")

	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	require.Error(t, result.err, "should return context error")
	//
	require.Len(t, upstream.calls, 0, "should not call upstream when context is canceled")
}

// TestHandleSingleAccountRetryInPlace_NetworkError_ContinuesRetry
func TestHandleSingleAccountRetryInPlace_NetworkError_ContinuesRetry(t *testing.T) {
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}
	upstream := &mockSmartRetryUpstream{
		//
		responses: []*http.Response{nil, successResp},
		errors:    []error{nil, nil},
	}

	account := &Account{
		ID:          14,
		Name:        "acc-net-retry",
		Type:        AccountTypeOAuth,
		Platform:    PlatformAntigravity,
		Concurrency: 1,
	}

	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
	}

	params := antigravityRetryLoopParams{
		ctx:          ctxWithSingleAccountRetry(),
		prefix:       "[test]",
		account:      account,
		accessToken:  "token",
		action:       "generateContent",
		body:         []byte(`{"input":"test"}`),
		httpUpstream: upstream,
	}

	svc := &AntigravityGatewayService{}
	result := svc.handleSingleAccountRetryInPlace(params, resp, nil, "https://ag-1.test", 1*time.Second, "gemini-3-pro")

	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	require.NotNil(t, result.resp, "should return successful response after network error recovery")
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
	require.Len(t, upstream.calls, 2, "first call fails (network error), second succeeds")
}

// ---------------------------------------------------------------------------
// 6. antigravityRetryLoop
// ---------------------------------------------------------------------------

// TestAntigravityRetryLoop_PreCheck_SingleAccountRetry_SkipsRateLimit
//
func TestAntigravityRetryLoop_PreCheck_SingleAccountRetry_SkipsRateLimit(t *testing.T) {
	upstream := &recordingOKUpstream{}
	account := &Account{
		ID:          20,
		Name:        "acc-rate-limited",
		Type:        AccountTypeOAuth,
		Platform:    PlatformAntigravity,
		Schedulable: true,
		Status:      StatusActive,
		Concurrency: 1,
		Extra: map[string]any{
			modelRateLimitsKey: map[string]any{
				"claude-sonnet-4-5": map[string]any{
					"rate_limit_reset_at": time.Now().Add(30 * time.Second).Format(time.RFC3339),
				},
			},
		},
	}

	svc := &AntigravityGatewayService{}
	result, err := svc.antigravityRetryLoop(antigravityRetryLoopParams{
		ctx:            ctxWithSingleAccountRetry(),
		prefix:         "[test]",
		account:        account,
		accessToken:    "token",
		action:         "generateContent",
		body:           []byte(`{"input":"test"}`),
		httpUpstream:   upstream,
		requestedModel: "claude-sonnet-4-5",
		handleError: func(ctx context.Context, prefix string, account *Account, statusCode int, headers http.Header, body []byte, requestedModel string, groupID int64, sessionHash string, isStickySession bool) *handleModelRateLimitResult {
			return nil
		},
	})

	require.NoError(t, err, "should not return error")
	require.NotNil(t, result, "should return result")
	require.NotNil(t, result.resp, "should have response")
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
	//
	require.Equal(t, 1, upstream.calls, "should have reached upstream despite rate limit")
}

// TestAntigravityRetryLoop_PreCheck_NoSingleAccountRetry_SwitchesOnRateLimit
// + →
func TestAntigravityRetryLoop_PreCheck_NoSingleAccountRetry_SwitchesOnRateLimit(t *testing.T) {
	upstream := &recordingOKUpstream{}
	account := &Account{
		ID:          21,
		Name:        "acc-rate-limited-multi",
		Type:        AccountTypeOAuth,
		Platform:    PlatformAntigravity,
		Schedulable: true,
		Status:      StatusActive,
		Concurrency: 1,
		Extra: map[string]any{
			modelRateLimitsKey: map[string]any{
				"claude-sonnet-4-5": map[string]any{
					"rate_limit_reset_at": time.Now().Add(30 * time.Second).Format(time.RFC3339),
				},
			},
		},
	}

	svc := &AntigravityGatewayService{}
	result, err := svc.antigravityRetryLoop(antigravityRetryLoopParams{
		ctx:            context.Background(), // 无单账号标记
		prefix:         "[test]",
		account:        account,
		accessToken:    "token",
		action:         "generateContent",
		body:           []byte(`{"input":"test"}`),
		httpUpstream:   upstream,
		requestedModel: "claude-sonnet-4-5",
		handleError: func(ctx context.Context, prefix string, account *Account, statusCode int, headers http.Header, body []byte, requestedModel string, groupID int64, sessionHash string, isStickySession bool) *handleModelRateLimitResult {
			return nil
		},
	})

	require.Nil(t, result, "should not return result on rate limit switch")
	require.NotNil(t, err, "should return error")

	var switchErr *AntigravityAccountSwitchError
	require.ErrorAs(t, err, &switchErr, "should return AntigravityAccountSwitchError")
	require.Equal(t, account.ID, switchErr.OriginalAccountID)
	require.Equal(t, "claude-sonnet-4-5", switchErr.RateLimitedModel)

	// upstream
	require.Equal(t, 0, upstream.calls, "upstream should NOT be called when pre-check blocks")
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

// TestHandleSmartRetry_503_SingleAccount_RetryInPlace_ThenSuccess_E2E
// + +
func TestHandleSmartRetry_503_SingleAccount_RetryInPlace_ThenSuccess_E2E(t *testing.T) {
	//
	fail503Body := `{
		"error": {
			"code": 503,
			"status": "UNAVAILABLE",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro"}, "reason": "MODEL_CAPACITY_EXHAUSTED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`
	resp503 := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(fail503Body)),
	}
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}

	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{resp503, successResp},
		errors:    []error{nil, nil},
	}

	account := &Account{
		ID:          30,
		Name:        "acc-e2e",
		Type:        AccountTypeOAuth,
		Platform:    PlatformAntigravity,
		Concurrency: 1,
	}

	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
	}

	params := antigravityRetryLoopParams{
		ctx:          ctxWithSingleAccountRetry(),
		prefix:       "[test]",
		account:      account,
		accessToken:  "token",
		action:       "generateContent",
		body:         []byte(`{"input":"test"}`),
		httpUpstream: upstream,
	}

	svc := &AntigravityGatewayService{}
	result := svc.handleSingleAccountRetryInPlace(params, resp, nil, "https://ag-1.test", 1*time.Second, "gemini-3-pro")

	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	require.NotNil(t, result.resp, "should return successful response after 2nd attempt")
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
	require.Nil(t, result.switchError)
	require.Len(t, upstream.calls, 2, "first 503, second OK")
}

// TestAntigravityRetryLoop_503_SingleAccount_InPlaceRetryUsed_E2E
// → handleSmartRetry → handleSingleAccountRetryInPlace
func TestAntigravityRetryLoop_503_SingleAccount_InPlaceRetryUsed_E2E(t *testing.T) {
	// +
	initial503Body := []byte(`{
		"error": {
			"code": 503,
			"status": "UNAVAILABLE",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro"}, "reason": "MODEL_CAPACITY_EXHAUSTED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "10s"}
			],
			"message": "No capacity available"
		}
	}`)
	initial503Resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(initial503Body)),
	}

	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}

	upstream := &mockSmartRetryUpstream{
		//
		//
		responses: []*http.Response{initial503Resp, successResp},
		errors:    []error{nil, nil},
	}

	repo := &stubAntigravityAccountRepo{}
	account := &Account{
		ID:          31,
		Name:        "acc-e2e-loop",
		Type:        AccountTypeOAuth,
		Platform:    PlatformAntigravity,
		Schedulable: true,
		Status:      StatusActive,
		Concurrency: 1,
	}

	svc := &AntigravityGatewayService{}
	result, err := svc.antigravityRetryLoop(antigravityRetryLoopParams{
		ctx:          ctxWithSingleAccountRetry(),
		prefix:       "[test]",
		account:      account,
		accessToken:  "token",
		action:       "generateContent",
		body:         []byte(`{"input":"test"}`),
		httpUpstream: upstream,
		accountRepo:  repo,
		handleError: func(ctx context.Context, prefix string, account *Account, statusCode int, headers http.Header, body []byte, requestedModel string, groupID int64, sessionHash string, isStickySession bool) *handleModelRateLimitResult {
			return nil
		},
	})

	require.NoError(t, err, "should not return error on successful retry")
	require.NotNil(t, result, "should return result")
	require.NotNil(t, result.resp, "should return response")
	require.Equal(t, http.StatusOK, result.resp.StatusCode)

	require.Len(t, repo.modelRateLimitCalls, 0,
		"should NOT set model rate limit in single account retry mode")
}
