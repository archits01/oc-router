package service

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// quotaDirtyCache
type quotaDirtyCache interface {
	PopDirtyUserPlatformQuotaKeys(ctx context.Context, n int) ([]UserPlatformQuotaKey, error)
	ReaddDirtyUserPlatformQuotaKeys(ctx context.Context, keys []UserPlatformQuotaKey) error
	BatchGetUserPlatformQuotaCache(ctx context.Context, keys []UserPlatformQuotaKey) ([]*UserPlatformQuotaCacheEntry, error)
}

// quotaSnapshotWriter
//
//
type quotaSnapshotWriter interface {
	BatchSnapshotUsage(ctx context.Context, snapshots []UserPlatformQuotaSnapshot, now time.Time) error
}

// FlusherMetrics
type FlusherMetrics struct {
	FlushSuccessTotal   atomic.Int64
	FlushErrorTotal     atomic.Int64
	FlushBatchSizeTotal atomic.Int64
	FlushLatencyMsMax   atomic.Int64
	DirtyReaddTotal     atomic.Int64
	// DirtyLostTotal：Readd ——++Readd
	// Redis
	DirtyLostTotal        atomic.Int64
	FlushFKViolationTotal atomic.Int64
}

// flusherMaxBatchesPerTick
const flusherMaxBatchesPerTick = 16

// maxFlushBatchSize ≤ repository.BatchSnapshotUsage (6000),
// ()。
const maxFlushBatchSize = 6000

// defaultFlushBatchSize (≤0)
const defaultFlushBatchSize = 1000

// UserPlatformQuotaUsageFlusher
//
type UserPlatformQuotaUsageFlusher struct {
	cache       quotaDirtyCache
	quotaRepo   quotaSnapshotWriter
	timingWheel *TimingWheelService
	// enabled ()
	enabled      bool
	interval     time.Duration
	batchSize    int
	flushTimeout time.Duration
	metrics      *FlusherMetrics
	stopped      atomic.Bool
}

// NewUserPlatformQuotaUsageFlusher
// cache(BillingCache) (UserPlatformQuotaRepository)
func NewUserPlatformQuotaUsageFlusher(cfg *config.Config, cache BillingCache, quotaRepo UserPlatformQuotaRepository, tw *TimingWheelService) *UserPlatformQuotaUsageFlusher {
	batchSize := cfg.Database.UserPlatformQuotaFlushBatchSize
	if batchSize <= 0 {
		batchSize = defaultFlushBatchSize
	}
	if batchSize > maxFlushBatchSize {
		logger.LegacyPrintf("quota_flusher",
			"[QuotaFlusher] flush_batch_size %d 超过上限 %d,已 clamp(避免 BatchSnapshotUsage 多子批非原子)",
			cfg.Database.UserPlatformQuotaFlushBatchSize, maxFlushBatchSize)
		batchSize = maxFlushBatchSize
	}
	interval := time.Duration(cfg.Database.UserPlatformQuotaFlushIntervalMs) * time.Millisecond
	if interval <= 0 {
		logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] flush_interval_ms %d 非法,fallback 2000ms", cfg.Database.UserPlatformQuotaFlushIntervalMs)
		interval = 2 * time.Second
	}
	return &UserPlatformQuotaUsageFlusher{
		cache:        cache,
		quotaRepo:    quotaRepo,
		timingWheel:  tw,
		enabled:      cfg.Database.UserPlatformQuotaFlusherEnabled,
		interval:     interval,
		batchSize:    batchSize,
		flushTimeout: 3 * time.Second,
		metrics:      &FlusherMetrics{},
	}
}

// updateLatencyMax
func (s *UserPlatformQuotaUsageFlusher) updateLatencyMax(ms int64) {
	for {
		old := s.metrics.FlushLatencyMsMax.Load()
		if ms <= old {
			return
		}
		if s.metrics.FlushLatencyMsMax.CompareAndSwap(old, ms) {
			return
		}
	}
}

// readdOrCountLost
func (s *UserPlatformQuotaUsageFlusher) readdOrCountLost(ctx context.Context, keys []UserPlatformQuotaKey, stage string) {
	if err := s.cache.ReaddDirtyUserPlatformQuotaKeys(ctx, keys); err != nil {
		s.metrics.DirtyLostTotal.Add(int64(len(keys)))
		logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] ALERT: Readd after %s failed, %d keys dropped from dirty set (DB mirror missing this batch, Redis still authoritative, active keys self-heal on next SADD): %v", stage, len(keys), err)
		return
	}
	s.metrics.DirtyReaddTotal.Add(int64(len(keys)))
}

// flushOneBatch → BatchGet → → BatchSnapshotUsage。
// (shouldContinue bool)：false
//
func (s *UserPlatformQuotaUsageFlusher) flushOneBatch(parentCtx context.Context) bool {
	ctx, cancel := context.WithTimeout(parentCtx, s.flushTimeout)
	defer cancel()

	// 1. Pop
	keys, err := s.cache.PopDirtyUserPlatformQuotaKeys(ctx, s.batchSize)
	if err != nil {
		s.metrics.FlushErrorTotal.Add(1)
		logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] PopDirty error: %v", err)
		return false
	}
	if len(keys) == 0 {
		return false
	}

	// 2.
	entries, err := s.cache.BatchGetUserPlatformQuotaCache(ctx, keys)
	if err != nil {
		s.metrics.FlushErrorTotal.Add(1)
		s.readdOrCountLost(ctx, keys, "BatchGet")
		logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] BatchGet error: %v", err)
		return false
	}

	// 3. ==nil →
	snaps := make([]UserPlatformQuotaSnapshot, 0, len(keys))
	for i, key := range keys {
		e := entries[i]
		if e == nil {
			continue
		}
		if e.DailyWindowStart == nil || e.WeeklyWindowStart == nil || e.MonthlyWindowStart == nil {
			continue
		}
		snaps = append(snaps, UserPlatformQuotaSnapshot{
			UserID:             key.UserID,
			Platform:           key.Platform,
			DailyUsageUSD:      e.DailyUsageUSD,
			WeeklyUsageUSD:     e.WeeklyUsageUSD,
			MonthlyUsageUSD:    e.MonthlyUsageUSD,
			DailyWindowStart:   *e.DailyWindowStart,
			WeeklyWindowStart:  *e.WeeklyWindowStart,
			MonthlyWindowStart: *e.MonthlyWindowStart,
		})
	}

	// 4.
	if len(snaps) == 0 {
		//
		if len(keys) < s.batchSize {
			return false
		}
		//
		return true
	}

	// (admin × flusher =true ):
	// admin ResetExpiredWindow/UpsertForUser ""。+ BatchGet
	// (),
	//
	// ()。
	//   - UpsertForUser → limit
	//   - ResetExpiredWindow
	//     ""
	//   - + =false。(DB ),
	//

	// 5.
	start := time.Now()
	writeErr := s.quotaRepo.BatchSnapshotUsage(ctx, snaps, time.Now().UTC())
	s.updateLatencyMax(time.Since(start).Milliseconds())

	if writeErr != nil {
		if errors.Is(writeErr, ErrUserPlatformQuotaFKViolation) {
			// → ()
			//
			// flusher
			// (Redis )。
			// FK
			s.metrics.FlushFKViolationTotal.Add(1)
			s.metrics.FlushErrorTotal.Add(1)
			logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] FK violation (dropped %d snaps): %v", len(snaps), writeErr)
		} else {
			s.metrics.FlushErrorTotal.Add(1)
			s.readdOrCountLost(ctx, keys, "BatchSnapshotUsage")
			logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] BatchSnapshotUsage error: %v", writeErr)
		}
		return false
	}

	s.metrics.FlushSuccessTotal.Add(1)
	s.metrics.FlushBatchSizeTotal.Add(int64(len(snaps)))

	//
	if len(keys) < s.batchSize {
		return false
	}
	return true
}

// flush
func (s *UserPlatformQuotaUsageFlusher) flush() {
	if s == nil {
		return
	}
	parentCtx := context.Background()
	for b := 0; b < flusherMaxBatchesPerTick; b++ {
		if !s.flushOneBatch(parentCtx) {
			return
		}
	}
	//
	// ×batchSize(DB );
	//
	logger.LegacyPrintf("quota_flusher",
		"[QuotaFlusher] 单 tick 达到 max batches 上限(%d × batchSize=%d),dirty set still non-empty, backlog deferred to next tick",
		flusherMaxBatchesPerTick, s.batchSize)
}

// tick
func (s *UserPlatformQuotaUsageFlusher) tick() {
	if s == nil || s.stopped.Load() {
		return
	}
	s.flush()
}

// Start =false
func (s *UserPlatformQuotaUsageFlusher) Start() {
	if s == nil || !s.enabled {
		return
	}
	s.timingWheel.ScheduleRecurring("deferred:platform_quota", s.interval, s.tick)
}

// Stop → Cancel →
func (s *UserPlatformQuotaUsageFlusher) Stop() {
	if s == nil {
		return
	}
	s.stopped.Store(true)
	s.timingWheel.Cancel("deferred:platform_quota")
	s.flush()
}
