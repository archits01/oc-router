package service

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
)

// UserMsgQueueCache
type UserMsgQueueCache interface {
	// AcquireLock
	AcquireLock(ctx context.Context, accountID int64, requestID string, lockTtlMs int) (acquired bool, err error)
	// ReleaseLock
	ReleaseLock(ctx context.Context, accountID int64, requestID string) (released bool, err error)
	// GetLastCompletedMs
	GetLastCompletedMs(ctx context.Context, accountID int64) (int64, error)
	// GetCurrentTimeMs
	GetCurrentTimeMs(ctx context.Context) (int64, error)
	// ForceReleaseLock
	ForceReleaseLock(ctx context.Context, accountID int64) error
	// ScanLockKeys == -1
	ScanLockKeys(ctx context.Context, maxCount int) ([]int64, error)
}

// QueueLockResult
type QueueLockResult struct {
	Acquired  bool
	RequestID string
}

// UserMessageQueueService
// + RPM
type UserMessageQueueService struct {
	cache    UserMsgQueueCache
	rpmCache RPMCache
	cfg      *config.UserMessageQueueConfig
	stopCh   chan struct{} // graceful shutdown
	stopOnce sync.Once     // 确保 Stop() 并发安全
}

// NewUserMessageQueueService
func NewUserMessageQueueService(cache UserMsgQueueCache, rpmCache RPMCache, cfg *config.UserMessageQueueConfig) *UserMessageQueueService {
	return &UserMessageQueueService{
		cache:    cache,
		rpmCache: rpmCache,
		cfg:      cfg,
		stopCh:   make(chan struct{}),
	}
}

// IsRealUserMessage
//
// 1. messages
// 2. == "user"
// 3. "tool_result" / "tool_use_result"
func IsRealUserMessage(parsed *ParsedRequest) bool {
	if parsed == nil {
		return false
	}
	messagesRaw := parsed.MessagesRaw()
	if len(messagesRaw) == 0 {
		return false
	}

	messages := gjson.ParseBytes(messagesRaw)
	if !messages.IsArray() {
		return false
	}
	lastMsg := gjson.Result{}
	messages.ForEach(func(_, msg gjson.Result) bool {
		lastMsg = msg
		return true
	})
	if !lastMsg.Exists() || !lastMsg.IsObject() {
		return false
	}
	if lastMsg.Get("role").String() != "user" {
		return false
	}

	content := lastMsg.Get("content")
	if !content.Exists() {
		return true
	}
	if !content.IsArray() {
		return true
	}

	isReal := true
	content.ForEach(func(_, item gjson.Result) bool {
		itemType := item.Get("type").String()
		if itemType == "tool_result" || itemType == "tool_use_result" {
			isReal = false
			return false
		}
		return true
	})
	return isReal
}

// TryAcquire
func (s *UserMessageQueueService) TryAcquire(ctx context.Context, accountID int64) (*QueueLockResult, error) {
	if s.cache == nil {
		return &QueueLockResult{Acquired: true}, nil // fail-open
	}

	requestID := generateUMQRequestID()
	lockTTL := s.cfg.LockTTLMs
	if lockTTL <= 0 {
		lockTTL = 120000
	}

	acquired, err := s.cache.AcquireLock(ctx, accountID, requestID, lockTTL)
	if err != nil {
		logger.LegacyPrintf("service.umq", "AcquireLock failed for account %d: %v", accountID, err)
		return &QueueLockResult{Acquired: true}, nil // fail-open
	}

	return &QueueLockResult{
		Acquired:  acquired,
		RequestID: requestID,
	}, nil
}

// Release
func (s *UserMessageQueueService) Release(ctx context.Context, accountID int64, requestID string) error {
	if s.cache == nil || requestID == "" {
		return nil
	}
	released, err := s.cache.ReleaseLock(ctx, accountID, requestID)
	if err != nil {
		logger.LegacyPrintf("service.umq", "ReleaseLock failed for account %d: %v", accountID, err)
		return err
	}
	if !released {
		logger.LegacyPrintf("service.umq", "ReleaseLock no-op for account %d (requestID mismatch or expired)", accountID)
	}
	return nil
}

// EnforceDelay
//
func (s *UserMessageQueueService) EnforceDelay(ctx context.Context, accountID int64, baseRPM int) error {
	if s.cache == nil {
		return nil
	}

	//
	lastMs, err := s.cache.GetLastCompletedMs(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("service.umq", "GetLastCompletedMs failed for account %d: %v", accountID, err)
		return nil // fail-open
	}
	if lastMs == 0 {
		return nil // 没有历史记录，无需延迟
	}

	delay := s.CalculateRPMAwareDelay(ctx, accountID, baseRPM)
	if delay <= 0 {
		return nil
	}

	//
	nowMs, err := s.cache.GetCurrentTimeMs(ctx)
	if err != nil {
		logger.LegacyPrintf("service.umq", "GetCurrentTimeMs failed: %v", err)
		return nil // fail-open
	}

	elapsed := time.Duration(nowMs-lastMs) * time.Millisecond
	if elapsed < 0 {
		//
		return nil
	}
	remaining := delay - elapsed
	if remaining <= 0 {
		return nil
	}

	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// CalculateRPMAwareDelay
// ratio = currentRPM / baseRPM
// ratio < 0.5  → MinDelay
// 0.5 ≤ ratio < 0.8 →
// ratio ≥ 0.8 → MaxDelay
// ±15% +
func (s *UserMessageQueueService) CalculateRPMAwareDelay(ctx context.Context, accountID int64, baseRPM int) time.Duration {
	minDelay := time.Duration(s.cfg.MinDelayMs) * time.Millisecond
	maxDelay := time.Duration(s.cfg.MaxDelayMs) * time.Millisecond

	if minDelay <= 0 {
		minDelay = 200 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 2000 * time.Millisecond
	}
	// > maxDelay
	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	var baseDelay time.Duration

	if baseRPM <= 0 || s.rpmCache == nil {
		baseDelay = minDelay
	} else {
		currentRPM, err := s.rpmCache.GetRPM(ctx, accountID)
		if err != nil {
			logger.LegacyPrintf("service.umq", "GetRPM failed for account %d: %v", accountID, err)
			baseDelay = minDelay // fail-open
		} else {
			ratio := float64(currentRPM) / float64(baseRPM)
			if ratio < 0.5 {
				baseDelay = minDelay
			} else if ratio >= 0.8 {
				baseDelay = maxDelay
			} else {
				// → minDelay, 0.8 → maxDelay
				t := (ratio - 0.5) / 0.3
				interpolated := float64(minDelay) + t*(float64(maxDelay)-float64(minDelay))
				baseDelay = time.Duration(math.Round(interpolated))
			}
		}
	}

	// ±15%
	return applyJitter(baseDelay, 0.15)
}

// StartCleanupWorker
// *:lock == -1
func (s *UserMessageQueueService) StartCleanupWorker(interval time.Duration) {
	if s == nil || s.cache == nil || interval <= 0 {
		return
	}

	runCleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		accountIDs, err := s.cache.ScanLockKeys(ctx, 1000)
		if err != nil {
			logger.LegacyPrintf("service.umq", "Cleanup scan failed: %v", err)
			return
		}

		cleaned := 0
		for _, accountID := range accountIDs {
			cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Second)
			if err := s.cache.ForceReleaseLock(cleanCtx, accountID); err != nil {
				logger.LegacyPrintf("service.umq", "Cleanup force release failed for account %d: %v", accountID, err)
			} else {
				cleaned++
			}
			cleanCancel()
		}

		if cleaned > 0 {
			logger.LegacyPrintf("service.umq", "Cleanup completed: released %d orphaned locks", cleaned)
		}
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				runCleanup()
			}
		}
	}()
}

// Stop
func (s *UserMessageQueueService) Stop() {
	if s != nil && s.stopCh != nil {
		s.stopOnce.Do(func() {
			close(s.stopCh)
		})
	}
}

// applyJitter ±jitterPct
// +
// (200ms, 0.15) ~ 230ms
func applyJitter(d time.Duration, jitterPct float64) time.Duration {
	if d <= 0 || jitterPct <= 0 {
		return d
	}
	// [-jitterPct, +jitterPct]
	jitter := (rand.Float64()*2 - 1) * jitterPct
	return time.Duration(float64(d) * (1 + jitter))
}

// generateUMQRequestID
func generateUMQRequestID() string {
	b := make([]byte, 16)
	if _, err := cryptorand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
