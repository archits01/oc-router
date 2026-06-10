package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"golang.org/x/sync/singleflight"
)

//
//
// errBillingCacheUnavailable ==nil
// "Redis "+ DB
var errBillingCacheUnavailable = fmt.Errorf("billing cache unavailable")

var (
	ErrSubscriptionInvalid       = infraerrors.Forbidden("SUBSCRIPTION_INVALID", "subscription is invalid or expired")
	ErrBillingServiceUnavailable = infraerrors.ServiceUnavailable("BILLING_SERVICE_ERROR", "Billing service temporarily unavailable. Please retry later.")
	// RPM
	ErrGroupRPMExceeded = infraerrors.TooManyRequests("GROUP_RPM_EXCEEDED", "group requests-per-minute limit exceeded")
	ErrUserRPMExceeded  = infraerrors.TooManyRequests("USER_RPM_EXCEEDED", "user requests-per-minute limit exceeded")

	// user × platform quota（HTTP 429 Too Many Requests + Retry-After header）。
	// ""
	//
	// ""
	ErrUserPlatformDailyQuotaExhausted   = infraerrors.TooManyRequests("USER_PLATFORM_DAILY_QUOTA_EXHAUSTED", "Daily usage quota exhausted for this platform.")
	ErrUserPlatformWeeklyQuotaExhausted  = infraerrors.TooManyRequests("USER_PLATFORM_WEEKLY_QUOTA_EXHAUSTED", "Weekly usage quota exhausted for this platform.")
	ErrUserPlatformMonthlyQuotaExhausted = infraerrors.TooManyRequests("USER_PLATFORM_MONTHLY_QUOTA_EXHAUSTED", "Monthly usage quota exhausted for this platform.")
)

// subscriptionCacheData
type subscriptionCacheData struct {
	Status       string
	ExpiresAt    time.Time
	DailyUsage   float64
	WeeklyUsage  float64
	MonthlyUsage float64
	Version      int64
}

type cacheWriteKind int

const (
	cacheWriteSetBalance cacheWriteKind = iota
	cacheWriteSetSubscription
	cacheWriteUpdateSubscriptionUsage
	cacheWriteDeductBalance
	cacheWriteUpdateRateLimitUsage
)

//
//
// 1.
// 2.
// 3. goroutine
//
// 1.
// 2.
const (
	cacheWriteWorkerCount     = 10              // 工作协程数量
	cacheWriteBufferSize      = 1000            // 任务队列缓冲大小
	cacheWriteTimeout         = 2 * time.Second // 单个写入操作timeout
	cacheWriteDropLogInterval = 5 * time.Second // 丢弃日志节流间隔
	balanceLoadTimeout        = 3 * time.Second
)

// cacheWriteTask
type cacheWriteTask struct {
	kind             cacheWriteKind
	userID           int64
	groupID          int64
	apiKeyID         int64
	balance          float64
	amount           float64
	subscriptionData *subscriptionCacheData
}

// apiKeyRateLimitLoader defines the interface for loading rate limit data from DB.
type apiKeyRateLimitLoader interface {
	GetRateLimitData(ctx context.Context, keyID int64) (*APIKeyRateLimitData, error)
}

// BillingCacheService
type BillingCacheService struct {
	cache                 BillingCache
	userRepo              UserRepository
	subRepo               UserSubscriptionRepository
	apiKeyRateLimitLoader apiKeyRateLimitLoader
	userRPMCache          UserRPMCache
	userGroupRateRepo     UserGroupRateRepository
	cfg                   *config.Config
	circuitBreaker        *billingCircuitBreaker
	userPlatformQuotaRepo UserPlatformQuotaRepository

	cacheWriteChan     chan cacheWriteTask
	cacheWriteWg       sync.WaitGroup
	cacheWriteStopOnce sync.Once
	cacheWriteMu       sync.RWMutex
	stopped            atomic.Bool
	balanceLoadSF      singleflight.Group
	quotaLoadSF        singleflight.Group
	cacheWriteDropFullCount     uint64
	cacheWriteDropFullLastLog   int64
	cacheWriteDropClosedCount   uint64
	cacheWriteDropClosedLastLog int64
}

// NewBillingCacheService
func NewBillingCacheService(
	cache BillingCache,
	userRepo UserRepository,
	subRepo UserSubscriptionRepository,
	apiKeyRepo APIKeyRepository,
	userRPMCache UserRPMCache,
	userGroupRateRepo UserGroupRateRepository,
	cfg *config.Config,
	userPlatformQuotaRepo UserPlatformQuotaRepository,
) *BillingCacheService {
	svc := &BillingCacheService{
		cache:                 cache,
		userRepo:              userRepo,
		subRepo:               subRepo,
		apiKeyRateLimitLoader: apiKeyRepo,
		userRPMCache:          userRPMCache,
		userGroupRateRepo:     userGroupRateRepo,
		cfg:                   cfg,
		userPlatformQuotaRepo: userPlatformQuotaRepo,
	}
	svc.circuitBreaker = newBillingCircuitBreaker(cfg.Billing.CircuitBreaker)
	svc.startCacheWriteWorkers()
	return svc
}

// Stop
func (s *BillingCacheService) Stop() {
	s.cacheWriteStopOnce.Do(func() {
		s.stopped.Store(true)

		s.cacheWriteMu.Lock()
		ch := s.cacheWriteChan
		if ch != nil {
			close(ch)
		}
		s.cacheWriteMu.Unlock()

		if ch == nil {
			return
		}
		s.cacheWriteWg.Wait()

		s.cacheWriteMu.Lock()
		if s.cacheWriteChan == ch {
			s.cacheWriteChan = nil
		}
		s.cacheWriteMu.Unlock()
	})
}

func (s *BillingCacheService) startCacheWriteWorkers() {
	ch := make(chan cacheWriteTask, cacheWriteBufferSize)
	s.cacheWriteChan = ch
	for i := 0; i < cacheWriteWorkerCount; i++ {
		s.cacheWriteWg.Add(1)
		go s.cacheWriteWorker(ch)
	}
}

// enqueueCacheWrite
func (s *BillingCacheService) enqueueCacheWrite(task cacheWriteTask) (enqueued bool) {
	if s.stopped.Load() {
		s.logCacheWriteDrop(task, "closed")
		return false
	}

	s.cacheWriteMu.RLock()
	defer s.cacheWriteMu.RUnlock()

	if s.cacheWriteChan == nil {
		s.logCacheWriteDrop(task, "closed")
		return false
	}

	select {
	case s.cacheWriteChan <- task:
		return true
	default:
		s.logCacheWriteDrop(task, "full")
		return false
	}
}

func (s *BillingCacheService) cacheWriteWorker(ch <-chan cacheWriteTask) {
	defer s.cacheWriteWg.Done()
	for task := range ch {
		ctx, cancel := context.WithTimeout(context.Background(), cacheWriteTimeout)
		switch task.kind {
		case cacheWriteSetBalance:
			s.setBalanceCache(ctx, task.userID, task.balance)
		case cacheWriteSetSubscription:
			s.setSubscriptionCache(ctx, task.userID, task.groupID, task.subscriptionData)
		case cacheWriteUpdateSubscriptionUsage:
			if s.cache != nil {
				if err := s.cache.UpdateSubscriptionUsage(ctx, task.userID, task.groupID, task.amount); err != nil {
					logger.LegacyPrintf("service.billing_cache", "Warning: update subscription cache failed for user %d group %d: %v", task.userID, task.groupID, err)
				}
			}
		case cacheWriteDeductBalance:
			if s.cache != nil {
				if err := s.cache.DeductUserBalance(ctx, task.userID, task.amount); err != nil {
					logger.LegacyPrintf("service.billing_cache", "Warning: deduct balance cache failed for user %d: %v", task.userID, err)
				}
			}
		case cacheWriteUpdateRateLimitUsage:
			if s.cache != nil {
				if err := s.cache.UpdateAPIKeyRateLimitUsage(ctx, task.apiKeyID, task.amount); err != nil {
					logger.LegacyPrintf("service.billing_cache", "Warning: update rate limit usage cache failed for api key %d: %v", task.apiKeyID, err)
				}
			}
		}
		cancel()
	}
}

// cacheWriteKindName
func cacheWriteKindName(kind cacheWriteKind) string {
	switch kind {
	case cacheWriteSetBalance:
		return "set_balance"
	case cacheWriteSetSubscription:
		return "set_subscription"
	case cacheWriteUpdateSubscriptionUsage:
		return "update_subscription_usage"
	case cacheWriteDeductBalance:
		return "deduct_balance"
	case cacheWriteUpdateRateLimitUsage:
		return "update_rate_limit_usage"
	default:
		return "unknown"
	}
}

// logCacheWriteDrop
func (s *BillingCacheService) logCacheWriteDrop(task cacheWriteTask, reason string) {
	var (
		countPtr *uint64
		lastPtr  *int64
	)
	switch reason {
	case "full":
		countPtr = &s.cacheWriteDropFullCount
		lastPtr = &s.cacheWriteDropFullLastLog
	case "closed":
		countPtr = &s.cacheWriteDropClosedCount
		lastPtr = &s.cacheWriteDropClosedLastLog
	default:
		return
	}

	atomic.AddUint64(countPtr, 1)
	now := time.Now().UnixNano()
	last := atomic.LoadInt64(lastPtr)
	if now-last < int64(cacheWriteDropLogInterval) {
		return
	}
	if !atomic.CompareAndSwapInt64(lastPtr, last, now) {
		return
	}
	dropped := atomic.SwapUint64(countPtr, 0)
	if dropped == 0 {
		return
	}
	logger.LegacyPrintf("service.billing_cache", "Warning: cache write queue %s, dropped %d tasks in last %s (latest kind=%s user %d group %d)",
		reason,
		dropped,
		cacheWriteDropLogInterval,
		cacheWriteKindName(task.kind),
		task.userID,
		task.groupID,
	)
}

// ============================================
// ============================================

// GetUserBalance
func (s *BillingCacheService) GetUserBalance(ctx context.Context, userID int64) (float64, error) {
	if s.cache == nil {
		// Redis
		return s.getUserBalanceFromDB(ctx, userID)
	}

	balance, err := s.cache.GetUserBalance(ctx, userID)
	if err == nil {
		return balance, nil
	}

	//
	value, err, _ := s.balanceLoadSF.Do(strconv.FormatInt(userID, 10), func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.Background(), balanceLoadTimeout)
		defer cancel()

		balance, err := s.getUserBalanceFromDB(loadCtx, userID)
		if err != nil {
			return nil, err
		}

		_ = s.enqueueCacheWrite(cacheWriteTask{
			kind:    cacheWriteSetBalance,
			userID:  userID,
			balance: balance,
		})
		return balance, nil
	})
	if err != nil {
		return 0, err
	}
	balance, ok := value.(float64)
	if !ok {
		return 0, fmt.Errorf("unexpected balance type: %T", value)
	}
	return balance, nil
}

// getUserBalanceFromDB
func (s *BillingCacheService) getUserBalanceFromDB(ctx context.Context, userID int64) (float64, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("get user balance: %w", err)
	}
	return user.Balance, nil
}

// setBalanceCache
func (s *BillingCacheService) setBalanceCache(ctx context.Context, userID int64, balance float64) {
	if s.cache == nil {
		return
	}
	if err := s.cache.SetUserBalance(ctx, userID, balance); err != nil {
		logger.LegacyPrintf("service.billing_cache", "Warning: set balance cache failed for user %d: %v", userID, err)
	}
}

// DeductBalanceCache
func (s *BillingCacheService) DeductBalanceCache(ctx context.Context, userID int64, amount float64) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.DeductUserBalance(ctx, userID, amount)
}

// QueueDeductBalance
func (s *BillingCacheService) QueueDeductBalance(userID int64, amount float64) {
	if s.cache == nil {
		return
	}
	if s.enqueueCacheWrite(cacheWriteTask{
		kind:   cacheWriteDeductBalance,
		userID: userID,
		amount: amount,
	}) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cacheWriteTimeout)
	defer cancel()
	if err := s.DeductBalanceCache(ctx, userID, amount); err != nil {
		logger.LegacyPrintf("service.billing_cache", "Warning: deduct balance cache fallback failed for user %d: %v", userID, err)
	}
}

// InvalidateUserBalance
func (s *BillingCacheService) InvalidateUserBalance(ctx context.Context, userID int64) error {
	if s.cache == nil {
		return nil
	}
	if err := s.cache.InvalidateUserBalance(ctx, userID); err != nil {
		logger.LegacyPrintf("service.billing_cache", "Warning: invalidate balance cache failed for user %d: %v", userID, err)
		return err
	}
	return nil
}

// ============================================
// ============================================

// GetSubscriptionStatus
func (s *BillingCacheService) GetSubscriptionStatus(ctx context.Context, userID, groupID int64) (*subscriptionCacheData, error) {
	if s.cache == nil {
		return s.getSubscriptionFromDB(ctx, userID, groupID)
	}

	cacheData, err := s.cache.GetSubscriptionCache(ctx, userID, groupID)
	if err == nil && cacheData != nil {
		return s.convertFromPortsData(cacheData), nil
	}

	data, err := s.getSubscriptionFromDB(ctx, userID, groupID)
	if err != nil {
		return nil, err
	}

	_ = s.enqueueCacheWrite(cacheWriteTask{
		kind:             cacheWriteSetSubscription,
		userID:           userID,
		groupID:          groupID,
		subscriptionData: data,
	})

	return data, nil
}

func (s *BillingCacheService) convertFromPortsData(data *SubscriptionCacheData) *subscriptionCacheData {
	return &subscriptionCacheData{
		Status:       data.Status,
		ExpiresAt:    data.ExpiresAt,
		DailyUsage:   data.DailyUsage,
		WeeklyUsage:  data.WeeklyUsage,
		MonthlyUsage: data.MonthlyUsage,
		Version:      data.Version,
	}
}

func (s *BillingCacheService) convertToPortsData(data *subscriptionCacheData) *SubscriptionCacheData {
	return &SubscriptionCacheData{
		Status:       data.Status,
		ExpiresAt:    data.ExpiresAt,
		DailyUsage:   data.DailyUsage,
		WeeklyUsage:  data.WeeklyUsage,
		MonthlyUsage: data.MonthlyUsage,
		Version:      data.Version,
	}
}

// getSubscriptionFromDB
func (s *BillingCacheService) getSubscriptionFromDB(ctx context.Context, userID, groupID int64) (*subscriptionCacheData, error) {
	sub, err := s.subRepo.GetActiveByUserIDAndGroupID(ctx, userID, groupID)
	if err != nil {
		return nil, fmt.Errorf("get subscription: %w", err)
	}

	return &subscriptionCacheData{
		Status:       sub.Status,
		ExpiresAt:    sub.ExpiresAt,
		DailyUsage:   sub.DailyUsageUSD,
		WeeklyUsage:  sub.WeeklyUsageUSD,
		MonthlyUsage: sub.MonthlyUsageUSD,
		Version:      sub.UpdatedAt.Unix(),
	}, nil
}

// setSubscriptionCache
func (s *BillingCacheService) setSubscriptionCache(ctx context.Context, userID, groupID int64, data *subscriptionCacheData) {
	if s.cache == nil || data == nil {
		return
	}
	if err := s.cache.SetSubscriptionCache(ctx, userID, groupID, s.convertToPortsData(data)); err != nil {
		logger.LegacyPrintf("service.billing_cache", "Warning: set subscription cache failed for user %d group %d: %v", userID, groupID, err)
	}
}

// UpdateSubscriptionUsage
func (s *BillingCacheService) UpdateSubscriptionUsage(ctx context.Context, userID, groupID int64, costUSD float64) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.UpdateSubscriptionUsage(ctx, userID, groupID, costUSD)
}

// QueueUpdateSubscriptionUsage
func (s *BillingCacheService) QueueUpdateSubscriptionUsage(userID, groupID int64, costUSD float64) {
	if s.cache == nil {
		return
	}
	if s.enqueueCacheWrite(cacheWriteTask{
		kind:    cacheWriteUpdateSubscriptionUsage,
		userID:  userID,
		groupID: groupID,
		amount:  costUSD,
	}) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cacheWriteTimeout)
	defer cancel()
	if err := s.UpdateSubscriptionUsage(ctx, userID, groupID, costUSD); err != nil {
		logger.LegacyPrintf("service.billing_cache", "Warning: update subscription cache fallback failed for user %d group %d: %v", userID, groupID, err)
	}
}

// InvalidateSubscription
func (s *BillingCacheService) InvalidateSubscription(ctx context.Context, userID, groupID int64) error {
	if s.cache == nil {
		return nil
	}
	if err := s.cache.InvalidateSubscriptionCache(ctx, userID, groupID); err != nil {
		logger.LegacyPrintf("service.billing_cache", "Warning: invalidate subscription cache failed for user %d group %d: %v", userID, groupID, err)
		return err
	}
	return nil
}

// InvalidateAPIKeyRateLimit invalidates the Redis rate-limit usage cache for an API key.
func (s *BillingCacheService) InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error {
	if s.cache == nil {
		return nil
	}
	if err := s.cache.InvalidateAPIKeyRateLimit(ctx, keyID); err != nil {
		logger.LegacyPrintf("service.billing_cache", "Warning: invalidate api key rate limit cache failed for key %d: %v", keyID, err)
		return err
	}
	return nil
}

// ============================================
// API Key
// ============================================

// checkAPIKeyRateLimits checks rate limit windows for an API key.
// It loads usage from Redis cache (falling back to DB on cache miss),
// resets expired windows in-memory and triggers async DB reset,
// and returns an error if any window limit is exceeded.
func (s *BillingCacheService) checkAPIKeyRateLimits(ctx context.Context, apiKey *APIKey) error {
	if s.cache == nil {
		// No cache: fall back to reading from DB directly
		if s.apiKeyRateLimitLoader == nil {
			return nil
		}
		data, err := s.apiKeyRateLimitLoader.GetRateLimitData(ctx, apiKey.ID)
		if err != nil {
			return nil // Don't block requests on DB errors
		}
		return s.evaluateRateLimits(ctx, apiKey, data.Usage5h, data.Usage1d, data.Usage7d,
			data.Window5hStart, data.Window1dStart, data.Window7dStart)
	}

	cacheData, err := s.cache.GetAPIKeyRateLimit(ctx, apiKey.ID)
	if err != nil {
		// Cache miss: load from DB and populate cache
		if s.apiKeyRateLimitLoader == nil {
			return nil
		}
		dbData, dbErr := s.apiKeyRateLimitLoader.GetRateLimitData(ctx, apiKey.ID)
		if dbErr != nil {
			return nil // Don't block requests on DB errors
		}
		// Build cache entry from DB data
		cacheEntry := &APIKeyRateLimitCacheData{
			Usage5h: dbData.Usage5h,
			Usage1d: dbData.Usage1d,
			Usage7d: dbData.Usage7d,
		}
		if dbData.Window5hStart != nil {
			cacheEntry.Window5h = dbData.Window5hStart.Unix()
		}
		if dbData.Window1dStart != nil {
			cacheEntry.Window1d = dbData.Window1dStart.Unix()
		}
		if dbData.Window7dStart != nil {
			cacheEntry.Window7d = dbData.Window7dStart.Unix()
		}
		_ = s.cache.SetAPIKeyRateLimit(ctx, apiKey.ID, cacheEntry)
		cacheData = cacheEntry
	}

	var w5h, w1d, w7d *time.Time
	if cacheData.Window5h > 0 {
		t := time.Unix(cacheData.Window5h, 0)
		w5h = &t
	}
	if cacheData.Window1d > 0 {
		t := time.Unix(cacheData.Window1d, 0)
		w1d = &t
	}
	if cacheData.Window7d > 0 {
		t := time.Unix(cacheData.Window7d, 0)
		w7d = &t
	}
	return s.evaluateRateLimits(ctx, apiKey, cacheData.Usage5h, cacheData.Usage1d, cacheData.Usage7d, w5h, w1d, w7d)
}

// evaluateRateLimits checks usage against limits, triggering async resets for expired windows.
func (s *BillingCacheService) evaluateRateLimits(ctx context.Context, apiKey *APIKey, usage5h, usage1d, usage7d float64, w5h, w1d, w7d *time.Time) error {
	needsReset := false

	// Reset expired windows in-memory for check purposes
	if IsWindowExpired(w5h, RateLimitWindow5h) {
		usage5h = 0
		needsReset = true
	}
	if IsWindowExpired(w1d, RateLimitWindow1d) {
		usage1d = 0
		needsReset = true
	}
	if IsWindowExpired(w7d, RateLimitWindow7d) {
		usage7d = 0
		needsReset = true
	}

	// Trigger async DB reset if any window expired
	if needsReset {
		keyID := apiKey.ID
		go func() {
			resetCtx, cancel := context.WithTimeout(context.Background(), cacheWriteTimeout)
			defer cancel()
			if s.apiKeyRateLimitLoader != nil {
				// Use the repo directly - reset then reload cache
				if loader, ok := s.apiKeyRateLimitLoader.(interface {
					ResetRateLimitWindows(ctx context.Context, id int64) error
				}); ok {
					if err := loader.ResetRateLimitWindows(resetCtx, keyID); err != nil {
						logger.LegacyPrintf("service.billing_cache", "Warning: reset rate limit windows failed for api key %d: %v", keyID, err)
					}
				}
			}
			// Invalidate cache so next request loads fresh data
			if s.cache != nil {
				if err := s.cache.InvalidateAPIKeyRateLimit(resetCtx, keyID); err != nil {
					logger.LegacyPrintf("service.billing_cache", "Warning: invalidate rate limit cache failed for api key %d: %v", keyID, err)
				}
			}
		}()
	}

	// Check limits
	if apiKey.RateLimit5h > 0 && usage5h >= apiKey.RateLimit5h {
		return ErrAPIKeyRateLimit5hExceeded
	}
	if apiKey.RateLimit1d > 0 && usage1d >= apiKey.RateLimit1d {
		return ErrAPIKeyRateLimit1dExceeded
	}
	if apiKey.RateLimit7d > 0 && usage7d >= apiKey.RateLimit7d {
		return ErrAPIKeyRateLimit7dExceeded
	}
	return nil
}

// QueueUpdateAPIKeyRateLimitUsage asynchronously updates rate limit usage in the cache.
func (s *BillingCacheService) QueueUpdateAPIKeyRateLimitUsage(apiKeyID int64, cost float64) {
	if s.cache == nil {
		return
	}
	s.enqueueCacheWrite(cacheWriteTask{
		kind:     cacheWriteUpdateRateLimitUsage,
		apiKeyID: apiKeyID,
		amount:   cost,
	})
}

// IncrementUserPlatformQuotaUsage × platform usage
//
//
//
// < 1ms（
//
// Redis
func (s *BillingCacheService) IncrementUserPlatformQuotaUsage(userID int64, platform string, cost float64) {
	if s.cache == nil {
		return
	}
	if platform == "" || cost <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cacheWriteTimeout)
	defer cancel()
	ttl := time.Duration(s.cfg.Billing.UserPlatformQuotaCacheTTLSeconds) * time.Second
	markDirty := s.cfg.Database.UserPlatformQuotaFlusherEnabled
	if err := s.cache.IncrUserPlatformQuotaUsageCache(ctx, userID, platform, cost, ttl, markDirty); err != nil {
		logger.LegacyPrintf("service.billing_cache",
			"ALERT: incr user platform quota cache failed user=%d platform=%s cost=%f: %v",
			userID, platform, cost, err)
	}
}

// ============================================
// ============================================

// CheckBillingEligibility
// > 0
//
// platform "anthropic"），"" × platform quota
func (s *BillingCacheService) CheckBillingEligibility(ctx context.Context, user *User, apiKey *APIKey, group *Group, subscription *UserSubscription, platform string) error {
	if s.cfg.RunMode == config.RunModeSimple {
		return nil
	}
	if s.circuitBreaker != nil && !s.circuitBreaker.Allow() {
		return ErrBillingServiceUnavailable
	}

	isSubscriptionMode := group != nil && group.IsSubscriptionType() && subscription != nil

	if isSubscriptionMode {
		if err := s.checkSubscriptionEligibility(ctx, user.ID, group, subscription); err != nil {
			return err
		}
	} else {
		if err := s.checkBalanceEligibility(ctx, user.ID); err != nil {
			return err
		}
	}

	// user × platform quota
	if !isSubscriptionMode {
		if err := s.checkUserPlatformQuotaEligibility(ctx, user.ID, platform); err != nil {
			return err
		}
	}

	// Check API Key rate limits (applies to both billing modes)
	if apiKey != nil && apiKey.HasRateLimits() {
		if err := s.checkAPIKeyRateLimits(ctx, apiKey); err != nil {
			return err
		}
	}

	// RPM → Group → User），
	if err := s.checkRPM(ctx, user, group); err != nil {
		return err
	}

	return nil
}

// checkRPM
//
//  1. () rpm_override       —
//     override=0
//  2. group.rpm_limit                 —
//  3. user.rpm_limit                  —
//
// ""
// Redis
func (s *BillingCacheService) checkRPM(ctx context.Context, user *User, group *Group) error {
	if s == nil || s.userRPMCache == nil || user == nil {
		return nil
	}

	// ── ──
	if group != nil {
		//
		var override *int
		if user.UserGroupRPMOverride != nil {
			override = user.UserGroupRPMOverride
		} else if s.userGroupRateRepo != nil {
			dbOverride, err := s.userGroupRateRepo.GetRPMOverrideByUserAndGroup(ctx, user.ID, group.ID)
			if err != nil {
				logger.LegacyPrintf(
					"service.billing_cache",
					"Warning: rpm override lookup failed for user=%d group=%d: %v",
					user.ID, group.ID, err,
				)
			} else {
				override = dbOverride
			}
		}

		if override != nil {
			// override=0 →
			if *override > 0 {
				count, incErr := s.userRPMCache.IncrementUserGroupRPM(ctx, user.ID, group.ID)
				if incErr != nil {
					logger.LegacyPrintf(
						"service.billing_cache",
						"Warning: rpm increment (override) failed for user=%d group=%d: %v",
						user.ID, group.ID, incErr,
					)
					// fail-open
				} else if count > *override {
					return ErrGroupRPMExceeded
				}
			}
			// override ——
		} else if group.RPMLimit > 0 {
			//
			count, err := s.userRPMCache.IncrementUserGroupRPM(ctx, user.ID, group.ID)
			if err != nil {
				logger.LegacyPrintf(
					"service.billing_cache",
					"Warning: rpm increment (group) failed for user=%d group=%d: %v",
					user.ID, group.ID, err,
				)
				// fail-open
			} else if count > group.RPMLimit {
				return ErrGroupRPMExceeded
			}
		}
	}

	// ── ──
	if user.RPMLimit > 0 {
		count, err := s.userRPMCache.IncrementUserRPM(ctx, user.ID)
		if err != nil {
			logger.LegacyPrintf(
				"service.billing_cache",
				"Warning: rpm increment (user) failed for user=%d: %v",
				user.ID, err,
			)
			return nil // fail-open
		}
		if count > user.RPMLimit {
			return ErrUserRPMExceeded
		}
	}

	return nil
}

// checkBalanceEligibility
func (s *BillingCacheService) checkBalanceEligibility(ctx context.Context, userID int64) error {
	balance, err := s.GetUserBalance(ctx, userID)
	if err != nil {
		if s.circuitBreaker != nil {
			s.circuitBreaker.OnFailure(err)
		}
		logger.LegacyPrintf("service.billing_cache", "ALERT: billing balance check failed for user %d: %v", userID, err)
		return ErrBillingServiceUnavailable.WithCause(err)
	}
	if s.circuitBreaker != nil {
		s.circuitBreaker.OnSuccess()
	}

	if balance <= 0 {
		return ErrInsufficientBalance
	}

	return nil
}

// checkSubscriptionEligibility
func (s *BillingCacheService) checkSubscriptionEligibility(ctx context.Context, userID int64, group *Group, subscription *UserSubscription) error {
	subData, err := s.GetSubscriptionStatus(ctx, userID, group.ID)
	if err != nil {
		if s.circuitBreaker != nil {
			s.circuitBreaker.OnFailure(err)
		}
		logger.LegacyPrintf("service.billing_cache", "ALERT: billing subscription check failed for user %d group %d: %v", userID, group.ID, err)
		return ErrBillingServiceUnavailable.WithCause(err)
	}
	if s.circuitBreaker != nil {
		s.circuitBreaker.OnSuccess()
	}

	if subData.Status != SubscriptionStatusActive {
		return ErrSubscriptionInvalid
	}

	if time.Now().After(subData.ExpiresAt) {
		return ErrSubscriptionInvalid
	}

	//
	if group.HasDailyLimit() && subData.DailyUsage >= *group.DailyLimitUSD {
		return ErrDailyLimitExceeded
	}

	if group.HasWeeklyLimit() && subData.WeeklyUsage >= *group.WeeklyLimitUSD {
		return ErrWeeklyLimitExceeded
	}

	if group.HasMonthlyLimit() && subData.MonthlyUsage >= *group.MonthlyLimitUSD {
		return ErrMonthlyLimitExceeded
	}

	return nil
}

type billingCircuitBreakerState int

const (
	billingCircuitClosed billingCircuitBreakerState = iota
	billingCircuitOpen
	billingCircuitHalfOpen
)

type billingCircuitBreaker struct {
	mu                sync.Mutex
	state             billingCircuitBreakerState
	failures          int
	openedAt          time.Time
	failureThreshold  int
	resetTimeout      time.Duration
	halfOpenRequests  int
	halfOpenRemaining int
}

func newBillingCircuitBreaker(cfg config.CircuitBreakerConfig) *billingCircuitBreaker {
	if !cfg.Enabled {
		return nil
	}
	resetTimeout := time.Duration(cfg.ResetTimeoutSeconds) * time.Second
	if resetTimeout <= 0 {
		resetTimeout = 30 * time.Second
	}
	halfOpen := cfg.HalfOpenRequests
	if halfOpen <= 0 {
		halfOpen = 1
	}
	threshold := cfg.FailureThreshold
	if threshold <= 0 {
		threshold = 5
	}
	return &billingCircuitBreaker{
		state:            billingCircuitClosed,
		failureThreshold: threshold,
		resetTimeout:     resetTimeout,
		halfOpenRequests: halfOpen,
	}
}

func (b *billingCircuitBreaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case billingCircuitClosed:
		return true
	case billingCircuitOpen:
		if time.Since(b.openedAt) < b.resetTimeout {
			return false
		}
		b.state = billingCircuitHalfOpen
		b.halfOpenRemaining = b.halfOpenRequests
		logger.LegacyPrintf("service.billing_cache", "ALERT: billing circuit breaker entering half-open state")
		fallthrough
	case billingCircuitHalfOpen:
		if b.halfOpenRemaining <= 0 {
			return false
		}
		b.halfOpenRemaining--
		return true
	default:
		return false
	}
}

func (b *billingCircuitBreaker) OnFailure(err error) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case billingCircuitOpen:
		return
	case billingCircuitHalfOpen:
		b.state = billingCircuitOpen
		b.openedAt = time.Now()
		b.halfOpenRemaining = 0
		logger.LegacyPrintf("service.billing_cache", "ALERT: billing circuit breaker opened after half-open failure: %v", err)
		return
	default:
		b.failures++
		if b.failures >= b.failureThreshold {
			b.state = billingCircuitOpen
			b.openedAt = time.Now()
			b.halfOpenRemaining = 0
			logger.LegacyPrintf("service.billing_cache", "ALERT: billing circuit breaker opened after %d failures: %v", b.failures, err)
		}
	}
}

func (b *billingCircuitBreaker) OnSuccess() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	previousState := b.state
	previousFailures := b.failures

	b.state = billingCircuitClosed
	b.failures = 0
	b.halfOpenRemaining = 0

	if previousState != billingCircuitClosed {
		logger.LegacyPrintf("service.billing_cache", "ALERT: billing circuit breaker closed (was %s)", circuitStateString(previousState))
	} else if previousFailures > 0 {
		logger.LegacyPrintf("service.billing_cache", "INFO: billing circuit breaker failures reset from %d", previousFailures)
	}
}

func circuitStateString(state billingCircuitBreakerState) string {
	switch state {
	case billingCircuitClosed:
		return "closed"
	case billingCircuitOpen:
		return "open"
	case billingCircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// checkUserPlatformQuotaEligibility × platform
// = {Daily/Weekly/Monthly}QuotaExhausted =
// checkUserPlatformQuotaEligibility
//
//
//  1. ==1，
//  2. cache MISS ==0）→
//  3. Redis != nil）→ fail-open，
func (s *BillingCacheService) checkUserPlatformQuotaEligibility(
	ctx context.Context,
	userID int64,
	platform string,
) error {
	if platform == "" || s.userPlatformQuotaRepo == nil {
		return nil
	}

	// cache →
	// *
	var (
		entry    *UserPlatformQuotaCacheEntry
		ok       bool
		cacheErr error
	)
	if s.cache != nil {
		entry, ok, cacheErr = s.cache.GetUserPlatformQuotaCache(ctx, userID, platform)
	} else {
		// "cache "
		cacheErr = errBillingCacheUnavailable
	}

	// --- cache HIT with current schema →
	if cacheErr == nil && ok && entry != nil && entry.SchemaVersion == UserPlatformQuotaCacheSchemaV1 {
		now := time.Now()
		dailyUsage := entry.DailyUsageUSD
		weeklyUsage := entry.WeeklyUsageUSD
		monthlyUsage := entry.MonthlyUsageUSD
		//
		//
		//
		windowExpired := false
		newDailyStart := entry.DailyWindowStart
		newWeeklyStart := entry.WeeklyWindowStart
		newMonthlyStart := entry.MonthlyWindowStart
		if quotaWindowExpired(entry.DailyWindowStart, timezone.StartOfDay(now)) {
			dailyUsage = 0
			windowExpired = true
			dayStart := timezone.StartOfDay(now)
			newDailyStart = &dayStart
		}
		if quotaWindowExpired(entry.WeeklyWindowStart, timezone.StartOfWeek(now)) {
			weeklyUsage = 0
			windowExpired = true
			weekStart := timezone.StartOfWeek(now)
			newWeeklyStart = &weekStart
		}
		if monthlyQuotaWindowExpired(entry.MonthlyWindowStart, now) {
			monthlyUsage = 0
			windowExpired = true
			monthStart := now
			newMonthlyStart = &monthStart
		}
		//
		//
		// EXISTS=0
		//
		//
		// ()+
		// ()():
		// isSentinel 「」,
		//   1) A3 (DB ):refresh
		//   2) DB (TTL 86400s):refresh (TTL )。
		// (!=nil )
		isSentinel := entry.DailyLimitUSD == nil && entry.WeeklyLimitUSD == nil && entry.MonthlyLimitUSD == nil
		if windowExpired && s.cache != nil && !isSentinel {
			refreshed := &UserPlatformQuotaCacheEntry{
				DailyUsageUSD:      dailyUsage,
				WeeklyUsageUSD:     weeklyUsage,
				MonthlyUsageUSD:    monthlyUsage,
				SchemaVersion:      UserPlatformQuotaCacheSchemaV1,
				DailyLimitUSD:      entry.DailyLimitUSD,
				WeeklyLimitUSD:     entry.WeeklyLimitUSD,
				MonthlyLimitUSD:    entry.MonthlyLimitUSD,
				DailyWindowStart:   newDailyStart,
				WeeklyWindowStart:  newWeeklyStart,
				MonthlyWindowStart: newMonthlyStart,
			}
			ttl := time.Duration(s.cfg.Billing.UserPlatformQuotaCacheTTLSeconds) * time.Second
			setCtx, setCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			if setErr := s.cache.SetUserPlatformQuotaCache(setCtx, userID, platform, refreshed, ttl); setErr != nil {
				logger.LegacyPrintf("service.billing_cache",
					"Warning: refresh expired user platform quota cache failed user=%d platform=%s: %v",
					userID, platform, setErr)
			}
			setCancel()
		}
		if entry.DailyLimitUSD != nil && dailyUsage >= *entry.DailyLimitUSD {
			return withWindowResetsMetadata(ErrUserPlatformDailyQuotaExhausted, nextDailyReset(now))
		}
		if entry.WeeklyLimitUSD != nil && weeklyUsage >= *entry.WeeklyLimitUSD {
			return withWindowResetsMetadata(ErrUserPlatformWeeklyQuotaExhausted, nextWeeklyReset(now))
		}
		if entry.MonthlyLimitUSD != nil && monthlyUsage >= *entry.MonthlyLimitUSD {
			return withWindowResetsMetadata(ErrUserPlatformMonthlyQuotaExhausted, nextMonthlyResetFrom(entry.MonthlyWindowStart, now))
		}
		return nil
	}

	// --- cache MISS、→
	// 's ctx among all dedupe followers.
	//
	sfKey := strconv.FormatInt(userID, 10) + ":" + platform
	ch := s.quotaLoadSF.DoChan(sfKey, func() (any, error) {
		// +
		// ""
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer bgCancel()
		return s.userPlatformQuotaRepo.GetByUserPlatform(bgCtx, userID, platform)
	})
	var (
		v     any
		dbErr error
	)
	select {
	case res := <-ch:
		v, dbErr = res.Val, res.Err
	case <-ctx.Done():
		// ()。
		logger.LegacyPrintf("service.billing_cache", "Warning: user platform quota check ctx cancelled user=%d platform=%s: %v (fail-open)", userID, platform, ctx.Err())
		return nil
	}
	if dbErr != nil {
		logger.LegacyPrintf("service.billing_cache", "Warning: load user platform quota failed user=%d platform=%s: %v (fail-open)", userID, platform, dbErr)
		return nil
	}
	rec, _ := v.(*UserPlatformQuotaRecord)
	if rec == nil {
		// (cacheErr!=nil)
		// ~1201 "Redis "
		//
		if s.cache != nil && cacheErr == nil {
			now := time.Now()
			startOfDay := timezone.StartOfDay(now)
			startOfWeek := timezone.StartOfWeek(now)
			sentinel := &UserPlatformQuotaCacheEntry{
				SchemaVersion:      UserPlatformQuotaCacheSchemaV1,
				DailyWindowStart:   &startOfDay,
				WeeklyWindowStart:  &startOfWeek,
				MonthlyWindowStart: &now,
				// limits ()
			}
			sentinelTTL := time.Duration(s.cfg.Billing.UserPlatformQuotaSentinelTTLSeconds) * time.Second
			if sentinelTTL <= 0 {
				// <=0 (),
				// sentinel →
				sentinelTTL = time.Hour
			}
			setCtx, setCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			if setErr := s.cache.SetUserPlatformQuotaCache(setCtx, userID, platform, sentinel, sentinelTTL); setErr != nil {
				userPlatformQuotaSentinelSetCacheErrorTotal.Add(1)
				logger.LegacyPrintf("service.billing_cache", "Warning: set sentinel quota cache failed user=%d platform=%s: %v", userID, platform, setErr)
			}
			setCancel()
		}
		return nil
	}

	now := time.Now()
	dailyUsage := rec.DailyUsageUSD
	weeklyUsage := rec.WeeklyUsageUSD
	monthlyUsage := rec.MonthlyUsageUSD
	if quotaWindowExpired(rec.DailyWindowStart, timezone.StartOfDay(now)) {
		dailyUsage = 0
	}
	if quotaWindowExpired(rec.WeeklyWindowStart, timezone.StartOfWeek(now)) {
		weeklyUsage = 0
	}
	if monthlyQuotaWindowExpired(rec.MonthlyWindowStart, now) {
		monthlyUsage = 0
	}

	// Redis
	if cacheErr != nil {
		if rec.DailyLimitUSD != nil && dailyUsage >= *rec.DailyLimitUSD {
			return withWindowResetsMetadata(ErrUserPlatformDailyQuotaExhausted, nextDailyReset(now))
		}
		if rec.WeeklyLimitUSD != nil && weeklyUsage >= *rec.WeeklyLimitUSD {
			return withWindowResetsMetadata(ErrUserPlatformWeeklyQuotaExhausted, nextWeeklyReset(now))
		}
		if rec.MonthlyLimitUSD != nil && monthlyUsage >= *rec.MonthlyLimitUSD {
			return withWindowResetsMetadata(ErrUserPlatformMonthlyQuotaExhausted, nextMonthlyResetFrom(rec.MonthlyWindowStart, now))
		}
		return nil
	}

	// cache MISS →
	newEntry := &UserPlatformQuotaCacheEntry{
		DailyUsageUSD:      dailyUsage,
		WeeklyUsageUSD:     weeklyUsage,
		MonthlyUsageUSD:    monthlyUsage,
		SchemaVersion:      UserPlatformQuotaCacheSchemaV1,
		DailyLimitUSD:      rec.DailyLimitUSD,
		WeeklyLimitUSD:     rec.WeeklyLimitUSD,
		MonthlyLimitUSD:    rec.MonthlyLimitUSD,
		DailyWindowStart:   rec.DailyWindowStart,
		WeeklyWindowStart:  rec.WeeklyWindowStart,
		MonthlyWindowStart: rec.MonthlyWindowStart,
	}
	if s.cache != nil {
		ttl := time.Duration(s.cfg.Billing.UserPlatformQuotaCacheTTLSeconds) * time.Second
		// ()+50ms,
		//
		//
		setCtx, setCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		if setErr := s.cache.SetUserPlatformQuotaCache(setCtx, userID, platform, newEntry, ttl); setErr != nil {
			logger.LegacyPrintf("service.billing_cache", "Warning: set user platform quota cache failed user=%d platform=%s: %v", userID, platform, setErr)
		}
		setCancel()
	}

	if rec.DailyLimitUSD != nil && dailyUsage >= *rec.DailyLimitUSD {
		return withWindowResetsMetadata(ErrUserPlatformDailyQuotaExhausted, nextDailyReset(now))
	}
	if rec.WeeklyLimitUSD != nil && weeklyUsage >= *rec.WeeklyLimitUSD {
		return withWindowResetsMetadata(ErrUserPlatformWeeklyQuotaExhausted, nextWeeklyReset(now))
	}
	if rec.MonthlyLimitUSD != nil && monthlyUsage >= *rec.MonthlyLimitUSD {
		return withWindowResetsMetadata(ErrUserPlatformMonthlyQuotaExhausted, nextMonthlyResetFrom(rec.MonthlyWindowStart, now))
	}
	return nil
}

// withWindowResetsMetadata
func withWindowResetsMetadata(err error, resetAt time.Time) error {
	appErr, ok := err.(*infraerrors.ApplicationError)
	if !ok || appErr == nil {
		return err
	}
	return appErr.WithMetadata(map[string]string{
		"window_resets_at": resetAt.Format(time.RFC3339),
	})
}

// nextDailyReset
//
func nextDailyReset(now time.Time) time.Time {
	return timezone.StartOfDay(now).AddDate(0, 0, 1)
}

// nextWeeklyReset
//
func nextWeeklyReset(now time.Time) time.Time {
	return timezone.StartOfWeek(now).AddDate(0, 0, 7)
}

// nextMonthlyResetFrom + 30d）。
// start >= 30d，
// +30d：+30d；
//
func nextMonthlyResetFrom(start *time.Time, now time.Time) time.Time {
	if start == nil || now.Sub(*start) >= 30*24*time.Hour {
		return now.Add(30 * 24 * time.Hour)
	}
	return start.Add(30 * 24 * time.Hour)
}

// quotaWindowExpired
func quotaWindowExpired(start *time.Time, currWindowStart time.Time) bool {
	if start == nil {
		return true
	}
	return start.Before(currWindowStart)
}

// monthlyQuotaWindowExpired
// >= 30×24h（
// start
func monthlyQuotaWindowExpired(start *time.Time, now time.Time) bool {
	if start == nil {
		return true
	}
	return now.Sub(*start) >= 30*24*time.Hour
}

// HasUserPlatformQuotaLimit ×platform
// +
// fail-safe:(simple )
func (s *BillingCacheService) HasUserPlatformQuotaLimit(ctx context.Context, userID int64, platform string) bool {
	if s.cfg.RunMode == config.RunModeSimple {
		return false
	}
	if s.cache == nil {
		return true
	}
	entry, ok, err := s.cache.GetUserPlatformQuotaCache(ctx, userID, platform)
	if err != nil || !ok || entry == nil {
		return true
	}
	return entry.DailyLimitUSD != nil || entry.WeeklyLimitUSD != nil || entry.MonthlyLimitUSD != nil
}
