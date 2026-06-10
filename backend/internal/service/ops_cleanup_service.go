package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
)

const (
	opsCleanupJobName = "ops_cleanup"

	opsCleanupLeaderLockKeyDefault = "ops:cleanup:leader"
	opsCleanupLeaderLockTTLDefault = 30 * time.Minute
)

var opsCleanupCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

var opsCleanupReleaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// OpsCleanupService periodically deletes old ops data to prevent unbounded DB growth.
//
// - Scheduling: 5-field cron spec (minute hour dom month dow).
// - Multi-instance: best-effort Redis leader lock so only one node runs cleanup.
// - Safety: deletes in batches to avoid long transactions.
//
//
// + leader lock + heartbeat，
type OpsCleanupService struct {
	opsRepo           OpsRepository
	db                *sql.DB
	redisClient       *redis.Client
	cfg               *config.Config
	channelMonitorSvc *ChannelMonitorService
	settingRepo       SettingRepository

	instanceID string

	// mu + effective
	// ""，
	//
	mu        sync.Mutex
	cron      *cron.Cron
	started   bool
	stopped   bool
	effective config.OpsCleanupConfig

	warnNoRedisOnce sync.Once
}

func NewOpsCleanupService(
	opsRepo OpsRepository,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
	channelMonitorSvc *ChannelMonitorService,
	settingRepo SettingRepository,
) *OpsCleanupService {
	return &OpsCleanupService{
		opsRepo:           opsRepo,
		db:                db,
		redisClient:       redisClient,
		cfg:               cfg,
		channelMonitorSvc: channelMonitorSvc,
		settingRepo:       settingRepo,
		instanceID:        uuid.NewString(),
	}
}

// Start
func (s *OpsCleanupService) Start() {
	if s == nil {
		return
	}
	if s.cfg != nil && !s.cfg.Ops.Enabled {
		return
	}
	if s.opsRepo == nil || s.db == nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] not started (missing deps)")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped {
		return
	}
	s.started = true
	if err := s.applyScheduleLocked(context.Background()); err != nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] not started: %v", err)
	}
}

// Stop
func (s *OpsCleanupService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	s.stopCronLocked()
}

// stopCronLocked
func (s *OpsCleanupService) stopCronLocked() {
	if s.cron == nil {
		return
	}
	ctx := s.cron.Stop()
	select {
	case <-ctx.Done():
	case <-time.After(opsCleanupCronStopTimeout):
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cron stop timed out")
	}
	s.cron = nil
}

// applyScheduleLocked
// =false（
func (s *OpsCleanupService) applyScheduleLocked(ctx context.Context) error {
	s.computeEffectiveLocked(ctx)
	s.stopCronLocked()

	if !s.effective.Enabled {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cron disabled by settings")
		return nil
	}

	schedule := strings.TrimSpace(s.effective.Schedule)
	if schedule == "" {
		schedule = opsCleanupDefaultSchedule
	}

	loc := time.Local
	if s.cfg != nil && strings.TrimSpace(s.cfg.Timezone) != "" {
		if parsed, err := time.LoadLocation(strings.TrimSpace(s.cfg.Timezone)); err == nil && parsed != nil {
			loc = parsed
		}
	}

	c := cron.New(cron.WithParser(opsCleanupCronParser), cron.WithLocation(loc))
	if _, err := c.AddFunc(schedule, func() { s.runScheduled() }); err != nil {
		return fmt.Errorf("invalid schedule %q: %w", schedule, err)
	}
	c.Start()
	s.cron = c
	logger.LegacyPrintf("service.ops_cleanup",
		"[OpsCleanup] scheduled (schedule=%q tz=%s retention_days=err:%d/min:%d/hour:%d)",
		schedule, loc.String(),
		s.effective.ErrorLogRetentionDays,
		s.effective.MinuteMetricsRetentionDays,
		s.effective.HourlyMetricsRetentionDays,
	)
	return nil
}

// Reload
//
// retention
//
func (s *OpsCleanupService) Reload(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return nil
	}
	return s.applyScheduleLocked(ctx)
}

// computeEffectiveLocked ""
//
//
//   - Enabled：settings
//   - Schedule：settings
//   - *RetentionDays：settings >=0 =TRUNCATE），<0
//
//
func (s *OpsCleanupService) computeEffectiveLocked(ctx context.Context) {
	base := config.OpsCleanupConfig{}
	if s.cfg != nil {
		base = s.cfg.Ops.Cleanup
	}
	defer func() { s.effective = base }()

	if s.settingRepo == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyOpsAdvancedSettings)
	if err != nil {
		if !errors.Is(err, ErrSettingNotFound) {
			logger.LegacyPrintf("service.ops_cleanup",
				"[OpsCleanup] read advanced settings failed, using cfg: %v", err)
		}
		return
	}
	var adv OpsAdvancedSettings
	if err := json.Unmarshal([]byte(raw), &adv); err != nil {
		logger.LegacyPrintf("service.ops_cleanup",
			"[OpsCleanup] parse advanced settings failed, using cfg: %v", err)
		return
	}
	dr := adv.DataRetention
	base.Enabled = dr.CleanupEnabled
	if sched := strings.TrimSpace(dr.CleanupSchedule); sched != "" {
		base.Schedule = sched
	}
	if dr.ErrorLogRetentionDays >= 0 {
		base.ErrorLogRetentionDays = dr.ErrorLogRetentionDays
	}
	if dr.MinuteMetricsRetentionDays >= 0 {
		base.MinuteMetricsRetentionDays = dr.MinuteMetricsRetentionDays
	}
	if dr.HourlyMetricsRetentionDays >= 0 {
		base.HourlyMetricsRetentionDays = dr.HourlyMetricsRetentionDays
	}
}

// snapshotEffective
func (s *OpsCleanupService) snapshotEffective() config.OpsCleanupConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effective
}

// refreshEffectiveBeforeRun
// schedule
func (s *OpsCleanupService) refreshEffectiveBeforeRun(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.computeEffectiveLocked(ctx)
}

func (s *OpsCleanupService) runScheduled() {
	if s == nil || s.db == nil || s.opsRepo == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), opsCleanupRunTimeout)
	defer cancel()

	//
	s.refreshEffectiveBeforeRun(ctx)

	release, ok := s.tryAcquireLeaderLock(ctx)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}

	startedAt := time.Now().UTC()
	runAt := startedAt

	counts, err := s.runCleanupOnce(ctx)
	if err != nil {
		s.recordHeartbeatError(runAt, time.Since(startedAt), err)
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cleanup failed: %v", err)
		return
	}
	s.recordHeartbeatSuccess(runAt, time.Since(startedAt), counts)
	logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cleanup complete: %s", counts)
}

func (s *OpsCleanupService) runCleanupOnce(ctx context.Context) (opsCleanupDeletedCounts, error) {
	out := opsCleanupDeletedCounts{}
	if s == nil || s.db == nil || s.cfg == nil {
		return out, nil
	}

	effective := s.snapshotEffective()
	now := time.Now().UTC()

	targets := []opsCleanupTarget{
		{effective.ErrorLogRetentionDays, "ops_error_logs", "created_at", false, &out.errorLogs},
		{effective.ErrorLogRetentionDays, "ops_alert_events", "created_at", false, &out.alertEvents},
		{effective.ErrorLogRetentionDays, "ops_system_logs", "created_at", false, &out.systemLogs},
		{effective.ErrorLogRetentionDays, "ops_system_log_cleanup_audits", "created_at", false, &out.logAudits},
		{effective.MinuteMetricsRetentionDays, "ops_system_metrics", "created_at", false, &out.systemMetrics},
		{effective.HourlyMetricsRetentionDays, "ops_metrics_hourly", "bucket_start", false, &out.hourlyPreagg},
		{effective.HourlyMetricsRetentionDays, "ops_metrics_daily", "bucket_date", true, &out.dailyPreagg},
	}

	for _, t := range targets {
		cutoff, truncate, ok := opsCleanupPlan(now, t.retentionDays)
		if !ok {
			continue
		}
		n, err := opsCleanupRunOne(ctx, s.db, truncate, cutoff, t.table, t.timeCol, t.castDate, opsCleanupBatchSize)
		if err != nil {
			return out, err
		}
		*t.counter = n
	}

	// Channel monitor +
	//
	//
	if s.channelMonitorSvc != nil {
		if err := s.channelMonitorSvc.RunDailyMaintenance(ctx); err != nil {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] channel monitor maintenance failed: %v", err)
		}
	}

	return out, nil
}

func (s *OpsCleanupService) tryAcquireLeaderLock(ctx context.Context) (func(), bool) {
	if s == nil {
		return nil, false
	}
	// In simple run mode, assume single instance.
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return nil, true
	}

	key := opsCleanupLeaderLockKeyDefault
	ttl := opsCleanupLeaderLockTTLDefault

	// Prefer Redis leader lock when available, but avoid stampeding the DB when Redis is flaky by
	// falling back to a DB advisory lock.
	if s.redisClient != nil {
		ok, err := s.redisClient.SetNX(ctx, key, s.instanceID, ttl).Result()
		if err == nil {
			if !ok {
				return nil, false
			}
			return func() {
				_, _ = opsCleanupReleaseScript.Run(ctx, s.redisClient, []string{key}, s.instanceID).Result()
			}, true
		}
		// Redis error: fall back to DB advisory lock.
		s.warnNoRedisOnce.Do(func() {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] leader lock SetNX failed; falling back to DB advisory lock: %v", err)
		})
	} else {
		s.warnNoRedisOnce.Do(func() {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] redis not configured; using DB advisory lock")
		})
	}

	release, ok := tryAcquireDBAdvisoryLock(ctx, s.db, hashAdvisoryLockID(key))
	if !ok {
		return nil, false
	}
	return release, true
}

func (s *OpsCleanupService) recordHeartbeatSuccess(runAt time.Time, duration time.Duration, counts opsCleanupDeletedCounts) {
	if s == nil || s.opsRepo == nil {
		return
	}
	now := time.Now().UTC()
	durMs := duration.Milliseconds()
	result := truncateString(counts.String(), 2048)
	ctx, cancel := context.WithTimeout(context.Background(), opsCleanupHeartbeatTimeout)
	defer cancel()
	_ = s.opsRepo.UpsertJobHeartbeat(ctx, &OpsUpsertJobHeartbeatInput{
		JobName:        opsCleanupJobName,
		LastRunAt:      &runAt,
		LastSuccessAt:  &now,
		LastDurationMs: &durMs,
		LastResult:     &result,
	})
}

func (s *OpsCleanupService) recordHeartbeatError(runAt time.Time, duration time.Duration, err error) {
	if s == nil || s.opsRepo == nil || err == nil {
		return
	}
	now := time.Now().UTC()
	durMs := duration.Milliseconds()
	msg := truncateString(err.Error(), 2048)
	ctx, cancel := context.WithTimeout(context.Background(), opsCleanupHeartbeatTimeout)
	defer cancel()
	_ = s.opsRepo.UpsertJobHeartbeat(ctx, &OpsUpsertJobHeartbeatInput{
		JobName:        opsCleanupJobName,
		LastRunAt:      &runAt,
		LastErrorAt:    &now,
		LastError:      &msg,
		LastDurationMs: &durMs,
	})
}
