package handler

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// UserMsgQueueHelper
// + SSE ping
type UserMsgQueueHelper struct {
	queueService *service.UserMessageQueueService
	pingFormat   SSEPingFormat
	pingInterval time.Duration
}

// NewUserMsgQueueHelper
func NewUserMsgQueueHelper(
	queueService *service.UserMessageQueueService,
	pingFormat SSEPingFormat,
	pingInterval time.Duration,
) *UserMsgQueueHelper {
	if pingInterval <= 0 {
		pingInterval = defaultPingInterval
	}
	return &UserMsgQueueHelper{
		queueService: queueService,
		pingFormat:   pingFormat,
		pingInterval: pingInterval,
	}
}

// AcquireWithWait
//
func (h *UserMsgQueueHelper) AcquireWithWait(
	c *gin.Context,
	accountID int64,
	baseRPM int,
	isStream bool,
	streamStarted *bool,
	timeout time.Duration,
	reqLog *zap.Logger,
) (releaseFunc func(), err error) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	result, err := h.queueService.TryAcquire(ctx, accountID)
	if err != nil {
		return nil, err // fail-open already handled at service layer
	}

	if result.Acquired {
		//
		if err := h.queueService.EnforceDelay(ctx, accountID, baseRPM); err != nil {
			if ctx.Err() != nil {
				//
				bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = h.queueService.Release(bgCtx, accountID, result.RequestID)
				bgCancel()
				return nil, ctx.Err()
			}
		}
		reqLog.Debug("gateway.umq_lock_acquired", zap.Int64("account_id", accountID))
		return h.makeReleaseFunc(accountID, result.RequestID, reqLog), nil
	}

	return h.waitForLockWithPing(c, ctx, accountID, baseRPM, isStream, streamStarted, reqLog)
}

// waitForLockWithPing
func (h *UserMsgQueueHelper) waitForLockWithPing(
	c *gin.Context,
	ctx context.Context,
	accountID int64,
	baseRPM int,
	isStream bool,
	streamStarted *bool,
	reqLog *zap.Logger,
) (func(), error) {
	needPing := isStream && h.pingFormat != ""

	var flusher http.Flusher
	if needPing {
		var ok bool
		flusher, ok = c.Writer.(http.Flusher)
		if !ok {
			needPing = false
		}
	}

	var pingCh <-chan time.Time
	if needPing {
		pingTicker := time.NewTicker(h.pingInterval)
		defer pingTicker.Stop()
		pingCh = pingTicker.C
	}

	backoff := initialBackoff
	timer := time.NewTimer(backoff)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("umq wait timeout for account %d", accountID)

		case <-pingCh:
			if !*streamStarted {
				c.Header("Content-Type", "text/event-stream")
				c.Header("Cache-Control", "no-cache")
				c.Header("Connection", "keep-alive")
				c.Header("X-Accel-Buffering", "no")
				*streamStarted = true
			}
			if _, err := fmt.Fprint(c.Writer, string(h.pingFormat)); err != nil {
				return nil, err
			}
			flusher.Flush()

		case <-timer.C:
			result, err := h.queueService.TryAcquire(ctx, accountID)
			if err != nil {
				return nil, err
			}
			if result.Acquired {
				//
				if delayErr := h.queueService.EnforceDelay(ctx, accountID, baseRPM); delayErr != nil {
					if ctx.Err() != nil {
						bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
						_ = h.queueService.Release(bgCtx, accountID, result.RequestID)
						bgCancel()
						return nil, ctx.Err()
					}
				}
				reqLog.Debug("gateway.umq_lock_acquired", zap.Int64("account_id", accountID))
				return h.makeReleaseFunc(accountID, result.RequestID, reqLog), nil
			}
			backoff = nextBackoff(backoff)
			timer.Reset(backoff)
		}
	}
}

// makeReleaseFunc
func (h *UserMsgQueueHelper) makeReleaseFunc(accountID int64, requestID string, reqLog *zap.Logger) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer bgCancel()
			if err := h.queueService.Release(bgCtx, accountID, requestID); err != nil {
				reqLog.Warn("gateway.umq_release_failed",
					zap.Int64("account_id", accountID),
					zap.Error(err),
				)
			} else {
				reqLog.Debug("gateway.umq_lock_released", zap.Int64("account_id", accountID))
			}
		})
	}
}

// ThrottleWithPing
func (h *UserMsgQueueHelper) ThrottleWithPing(
	c *gin.Context,
	accountID int64,
	baseRPM int,
	isStream bool,
	streamStarted *bool,
	timeout time.Duration,
	reqLog *zap.Logger,
) error {
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	delay := h.queueService.CalculateRPMAwareDelay(ctx, accountID, baseRPM)
	if delay <= 0 {
		return nil
	}

	reqLog.Debug("gateway.umq_throttle_delay",
		zap.Int64("account_id", accountID),
		zap.Duration("delay", delay),
	)

	//
	needPing := isStream && h.pingFormat != ""
	var flusher http.Flusher
	if needPing {
		flusher, _ = c.Writer.(http.Flusher)
		if flusher == nil {
			needPing = false
		}
	}

	var pingCh <-chan time.Time
	if needPing {
		pingTicker := time.NewTicker(h.pingInterval)
		defer pingTicker.Stop()
		pingCh = pingTicker.C
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pingCh:
			// SSE ping
			if !*streamStarted {
				c.Header("Content-Type", "text/event-stream")
				c.Header("Cache-Control", "no-cache")
				c.Header("Connection", "keep-alive")
				c.Header("X-Accel-Buffering", "no")
				*streamStarted = true
			}
			if _, err := fmt.Fprint(c.Writer, string(h.pingFormat)); err != nil {
				return err
			}
			flusher.Flush()
		case <-timer.C:
			return nil
		}
	}
}
