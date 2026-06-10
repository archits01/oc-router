package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
)

// RateLimitService
type RateLimitService struct {
	accountRepo           AccountRepository
	usageRepo             UsageLogRepository
	cfg                   *config.Config
	geminiQuotaService    *GeminiQuotaService
	tempUnschedCache      TempUnschedCache
	timeoutCounterCache   TimeoutCounterCache
	openAI403CounterCache OpenAI403CounterCache
	settingService        *SettingService
	tokenCacheInvalidator TokenCacheInvalidator
	runtimeBlocker        AccountRuntimeBlocker
	usageCacheMu          sync.RWMutex
	usageCache            map[int64]*geminiUsageCacheEntry
}

type AccountRuntimeBlocker interface {
	BlockAccountScheduling(account *Account, until time.Time, reason string)
	ClearAccountSchedulingBlock(accountID int64)
}

// SuccessfulTestRecoveryResult
type SuccessfulTestRecoveryResult struct {
	ClearedError     bool
	ClearedRateLimit bool
}

// AccountRecoveryOptions
type AccountRecoveryOptions struct {
	InvalidateToken bool
}

type geminiUsageCacheEntry struct {
	windowStart time.Time
	cachedAt    time.Time
	totals      GeminiUsageTotals
}

type geminiUsageTotalsBatchProvider interface {
	GetGeminiUsageTotalsBatch(ctx context.Context, accountIDs []int64, startTime, endTime time.Time) (map[int64]GeminiUsageTotals, error)
}

const geminiPrecheckCacheTTL = time.Minute

const (
	defaultRateLimit429CooldownSeconds = 5
	maxRateLimit429CooldownSeconds     = 7200
)

const (
	openAIImageRateLimitDefaultCooldown = time.Minute
	openAIImageRateLimitReason          = "openai_image_rate_limited"
)

var openAIImageTryAgainPattern = regexp.MustCompile(`(?i)try again in\s+([0-9]+(?:\.[0-9]+)?)\s*(ms|s|sec|secs|second|seconds|m|min|mins|minute|minutes)`)

const (
	openAI403CooldownMinutesDefault = 10
	openAI403DisableThreshold       = 3
	openAI403CounterWindowMinutes   = 180
)

// NewRateLimitService
func NewRateLimitService(accountRepo AccountRepository, usageRepo UsageLogRepository, cfg *config.Config, geminiQuotaService *GeminiQuotaService, tempUnschedCache TempUnschedCache) *RateLimitService {
	return &RateLimitService{
		accountRepo:        accountRepo,
		usageRepo:          usageRepo,
		cfg:                cfg,
		geminiQuotaService: geminiQuotaService,
		tempUnschedCache:   tempUnschedCache,
		usageCache:         make(map[int64]*geminiUsageCacheEntry),
	}
}

// SetTimeoutCounterCache
func (s *RateLimitService) SetTimeoutCounterCache(cache TimeoutCounterCache) {
	s.timeoutCounterCache = cache
}

// SetOpenAI403CounterCache
func (s *RateLimitService) SetOpenAI403CounterCache(cache OpenAI403CounterCache) {
	s.openAI403CounterCache = cache
}

// SetSettingService
func (s *RateLimitService) SetSettingService(settingService *SettingService) {
	s.settingService = settingService
}

// SetTokenCacheInvalidator
func (s *RateLimitService) SetTokenCacheInvalidator(invalidator TokenCacheInvalidator) {
	s.tokenCacheInvalidator = invalidator
}

func (s *RateLimitService) SetAccountRuntimeBlocker(blocker AccountRuntimeBlocker) {
	s.runtimeBlocker = blocker
}

func (s *RateLimitService) notifyAccountSchedulingBlocked(account *Account, until time.Time, reason string) {
	if s == nil || s.runtimeBlocker == nil || account == nil {
		return
	}
	s.runtimeBlocker.BlockAccountScheduling(account, until, reason)
}

func (s *RateLimitService) notifyAccountSchedulingBlockCleared(accountID int64) {
	if s == nil || s.runtimeBlocker == nil || accountID <= 0 {
		return
	}
	s.runtimeBlocker.ClearAccountSchedulingBlock(accountID)
}

// ErrorPolicyResult
type ErrorPolicyResult int

const (
	ErrorPolicyNone            ErrorPolicyResult = iota // 未命中任何策略，继续默认逻辑
	ErrorPolicySkipped                                  // 自定义error码开启但未命中，跳过处理
	ErrorPolicyMatched                                  // 自定义error码命中，应停止调度
	ErrorPolicyTempUnscheduled                          // 临时不可调度规则命中
)

// CheckErrorPolicy
func (s *RateLimitService) CheckErrorPolicy(ctx context.Context, account *Account, statusCode int, responseBody []byte) ErrorPolicyResult {
	if account.IsCustomErrorCodesEnabled() {
		if account.ShouldHandleErrorCode(statusCode) {
			return ErrorPolicyMatched
		}
		slog.Info("account_error_code_skipped", "account_id", account.ID, "status_code", statusCode)
		return ErrorPolicySkipped
	}
	if account.IsPoolMode() {
		return ErrorPolicySkipped
	}
	if s.tryTempUnschedulable(ctx, account, statusCode, responseBody) {
		return ErrorPolicyTempUnscheduled
	}
	return ErrorPolicyNone
}

// HandleUpstreamError
func (s *RateLimitService) HandleUpstreamError(ctx context.Context, account *Account, statusCode int, headers http.Header, responseBody []byte, requestedModel ...string) (shouldDisable bool) {
	customErrorCodesEnabled := account.IsCustomErrorCodesEnabled()

	if account.IsPoolMode() && !customErrorCodesEnabled {
		slog.Info("pool_mode_error_skipped", "account_id", account.ID, "status_code", statusCode)
		return false
	}

	// apikey
	if !account.ShouldHandleErrorCode(statusCode) {
		slog.Info("account_error_code_skipped", "account_id", account.ID, "status_code", statusCode)
		return false
	}

	if len(requestedModel) > 0 && s.HandleUpstreamModelNotFound(ctx, account, requestedModel[0], statusCode, responseBody) {
		return true
	}

	//
	if statusCode != 401 {
		if s.tryTempUnschedulable(ctx, account, statusCode, responseBody) {
			return true
		}
	}

	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(responseBody))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	if upstreamMsg != "" {
		upstreamMsg = truncateForLog([]byte(upstreamMsg), 512)
	}

	switch statusCode {
	case 400:
		// "organization has been disabled" →
		if strings.Contains(strings.ToLower(upstreamMsg), "organization has been disabled") {
			msg := "Organization disabled (400): " + upstreamMsg
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
		} else if account.Platform == PlatformAnthropic && strings.Contains(strings.ToLower(upstreamMsg), "credit balance") {
			// Anthropic API key
			msg := "Credit balance exhausted (400): " + upstreamMsg
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
		} else if strings.Contains(strings.ToLower(upstreamMsg), "identity verification is required") {
			// KYC →
			msg := "Identity verification required (400): " + upstreamMsg
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
		}
		//
	case 401:
		// OpenAI: token_invalidated / token_revoked
		openai401Code := extractUpstreamErrorCode(responseBody)
		if account.Platform == PlatformOpenAI && (openai401Code == "token_invalidated" || openai401Code == "token_revoked") {
			msg := "Token revoked (401): account authentication permanently revoked"
			if upstreamMsg != "" {
				msg = "Token revoked (401): " + upstreamMsg
			}
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
			break
		}
		// OpenAI: {"detail":"Unauthorized"}
		if account.Platform == PlatformOpenAI && gjson.GetBytes(responseBody, "detail").String() == "Unauthorized" {
			msg := "Unauthorized (401): account authentication failed permanently"
			if upstreamMsg != "" {
				msg = "Unauthorized (401): " + upstreamMsg
			}
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
			break
		}
		// OAuth
		// Antigravity
		if account.Type == AccountTypeOAuth && account.Platform != PlatformAntigravity {
			if s.tokenCacheInvalidator != nil {
				if err := s.tokenCacheInvalidator.InvalidateToken(ctx, account); err != nil {
					slog.Warn("oauth_401_invalidate_cache_failed", "account_id", account.ID, "error", err)
				}
			}
			//
			//
			if strings.TrimSpace(account.GetCredential("refresh_token")) == "" {
				msg := "Authentication failed (401): refresh_token missing, cannot recover"
				if upstreamMsg != "" {
					msg = "OAuth 401 (no refresh_token): " + upstreamMsg
				}
				s.handleAuthError(ctx, account, msg)
				shouldDisable = true
				break
			}
			// 2. =active
			//
			//
			// persistAccountCredentials → accountRepository.UpdateCredentials → SetCredentials），
			//
			//
			// tryRecoverFromRefreshRace == usedRT
			// + SetTempUnschedulable
			//
			msg := "Authentication failed (401): invalid or expired credentials"
			if upstreamMsg != "" {
				msg = "OAuth 401: " + upstreamMsg
			}
			cooldownMinutes := s.cfg.RateLimit.OAuth401CooldownMinutes
			if cooldownMinutes <= 0 {
				cooldownMinutes = 10
			}
			until := time.Now().Add(time.Duration(cooldownMinutes) * time.Minute)
			s.notifyAccountSchedulingBlocked(account, until, "oauth_401")
			if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, msg); err != nil {
				slog.Warn("oauth_401_set_temp_unschedulable_failed", "account_id", account.ID, "error", err)
			}
			shouldDisable = true
		} else {
			//
			msg := "Authentication failed (401): invalid or expired credentials"
			if upstreamMsg != "" {
				msg = "Authentication failed (401): " + upstreamMsg
			}
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
		}
	case 402:
		// OpenAI: deactivated_workspace
		if account.Platform == PlatformOpenAI && gjson.GetBytes(responseBody, "detail.code").String() == "deactivated_workspace" {
			msg := "Workspace deactivated (402): workspace has been deactivated"
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
			break
		}
		msg := "Payment required (402): insufficient balance or billing issue"
		if upstreamMsg != "" {
			msg = "Payment required (402): " + upstreamMsg
		}
		s.handleAuthError(ctx, account, msg)
		shouldDisable = true
	case 403:
		logger.LegacyPrintf(
			"service.ratelimit",
			"[HandleUpstreamErrorRaw] account_id=%d platform=%s type=%s status=403 request_id=%s cf_ray=%s upstream_msg=%s raw_body=%s",
			account.ID,
			account.Platform,
			account.Type,
			strings.TrimSpace(headers.Get("x-request-id")),
			strings.TrimSpace(headers.Get("cf-ray")),
			upstreamMsg,
			truncateForLog(responseBody, 1024),
		)
		shouldDisable = s.handle403(ctx, account, upstreamMsg, responseBody)
	case 429:
		s.handle429(ctx, account, headers, responseBody)
		shouldDisable = false
	case 529:
		s.handle529(ctx, account)
		shouldDisable = false
	default:
		if customErrorCodesEnabled {
			msg := "Custom error code triggered"
			if upstreamMsg != "" {
				msg = upstreamMsg
			}
			s.handleCustomErrorCode(ctx, account, statusCode, msg)
			shouldDisable = true
		} else if statusCode >= 500 {
			//
			slog.Warn("account_upstream_error", "account_id", account.ID, "status_code", statusCode)
			shouldDisable = false
		}
	}

	return shouldDisable
}

// PreCheckUsage proactively checks local quota before dispatching a request.
// Returns false when the account should be skipped.
func (s *RateLimitService) PreCheckUsage(ctx context.Context, account *Account, requestedModel string) (bool, error) {
	if account == nil || account.Platform != PlatformGemini {
		return true, nil
	}
	if s.usageRepo == nil || s.geminiQuotaService == nil {
		return true, nil
	}

	quota, ok := s.geminiQuotaService.QuotaForAccount(ctx, account)
	if !ok {
		return true, nil
	}

	now := time.Now()
	modelClass := geminiModelClassFromName(requestedModel)

	// 1) Daily quota precheck (RPD; resets at PST midnight)
	{
		var limit int64
		if quota.SharedRPD > 0 {
			limit = quota.SharedRPD
		} else {
			switch modelClass {
			case geminiModelFlash:
				limit = quota.FlashRPD
			default:
				limit = quota.ProRPD
			}
		}

		if limit > 0 {
			start := geminiDailyWindowStart(now)
			totals, ok := s.getGeminiUsageTotals(account.ID, start, now)
			if !ok {
				stats, err := s.usageRepo.GetModelStatsWithFilters(ctx, start, now, 0, 0, account.ID, 0, nil, nil, nil)
				if err != nil {
					return true, err
				}
				totals = geminiAggregateUsage(stats)
				s.setGeminiUsageTotals(account.ID, start, now, totals)
			}

			var used int64
			if quota.SharedRPD > 0 {
				used = totals.ProRequests + totals.FlashRequests
			} else {
				switch modelClass {
				case geminiModelFlash:
					used = totals.FlashRequests
				default:
					used = totals.ProRequests
				}
			}

			if used >= limit {
				resetAt := geminiDailyResetTime(now)
				// NOTE:
				// - This is a local precheck to reduce upstream 429s.
				// - Do NOT mark the account as rate-limited here; rate_limit_reset_at should reflect real upstream 429s.
				slog.Info("gemini_precheck_daily_quota_reached", "account_id", account.ID, "used", used, "limit", limit, "reset_at", resetAt)
				return false, nil
			}
		}
	}

	// 2) Minute quota precheck (RPM; fixed window current minute)
	{
		var limit int64
		if quota.SharedRPM > 0 {
			limit = quota.SharedRPM
		} else {
			switch modelClass {
			case geminiModelFlash:
				limit = quota.FlashRPM
			default:
				limit = quota.ProRPM
			}
		}

		if limit > 0 {
			start := now.Truncate(time.Minute)
			stats, err := s.usageRepo.GetModelStatsWithFilters(ctx, start, now, 0, 0, account.ID, 0, nil, nil, nil)
			if err != nil {
				return true, err
			}
			totals := geminiAggregateUsage(stats)

			var used int64
			if quota.SharedRPM > 0 {
				used = totals.ProRequests + totals.FlashRequests
			} else {
				switch modelClass {
				case geminiModelFlash:
					used = totals.FlashRequests
				default:
					used = totals.ProRequests
				}
			}

			if used >= limit {
				resetAt := start.Add(time.Minute)
				// Do not persist "rate limited" status from local precheck. See note above.
				slog.Info("gemini_precheck_minute_quota_reached", "account_id", account.ID, "used", used, "limit", limit, "reset_at", resetAt)
				return false, nil
			}
		}
	}

	return true, nil
}

// PreCheckUsageBatch performs quota precheck for multiple accounts in one request.
// Returned map value=false means the account should be skipped.
func (s *RateLimitService) PreCheckUsageBatch(ctx context.Context, accounts []*Account, requestedModel string) (map[int64]bool, error) {
	result := make(map[int64]bool, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		result[account.ID] = true
	}

	if len(accounts) == 0 || requestedModel == "" {
		return result, nil
	}
	if s.usageRepo == nil || s.geminiQuotaService == nil {
		return result, nil
	}

	modelClass := geminiModelClassFromName(requestedModel)
	now := time.Now()
	dailyStart := geminiDailyWindowStart(now)
	minuteStart := now.Truncate(time.Minute)

	type quotaAccount struct {
		account *Account
		quota   GeminiQuota
	}
	quotaAccounts := make([]quotaAccount, 0, len(accounts))
	for _, account := range accounts {
		if account == nil || account.Platform != PlatformGemini {
			continue
		}
		quota, ok := s.geminiQuotaService.QuotaForAccount(ctx, account)
		if !ok {
			continue
		}
		quotaAccounts = append(quotaAccounts, quotaAccount{
			account: account,
			quota:   quota,
		})
	}
	if len(quotaAccounts) == 0 {
		return result, nil
	}

	// 1) Daily precheck (cached + batch DB fallback)
	dailyTotalsByID := make(map[int64]GeminiUsageTotals, len(quotaAccounts))
	dailyMissIDs := make([]int64, 0, len(quotaAccounts))
	for _, item := range quotaAccounts {
		limit := geminiDailyLimit(item.quota, modelClass)
		if limit <= 0 {
			continue
		}
		accountID := item.account.ID
		if totals, ok := s.getGeminiUsageTotals(accountID, dailyStart, now); ok {
			dailyTotalsByID[accountID] = totals
			continue
		}
		dailyMissIDs = append(dailyMissIDs, accountID)
	}
	if len(dailyMissIDs) > 0 {
		totalsBatch, err := s.getGeminiUsageTotalsBatch(ctx, dailyMissIDs, dailyStart, now)
		if err != nil {
			return result, err
		}
		for _, accountID := range dailyMissIDs {
			totals := totalsBatch[accountID]
			dailyTotalsByID[accountID] = totals
			s.setGeminiUsageTotals(accountID, dailyStart, now, totals)
		}
	}
	for _, item := range quotaAccounts {
		limit := geminiDailyLimit(item.quota, modelClass)
		if limit <= 0 {
			continue
		}
		accountID := item.account.ID
		used := geminiUsedRequests(item.quota, modelClass, dailyTotalsByID[accountID], true)
		if used >= limit {
			resetAt := geminiDailyResetTime(now)
			slog.Info("gemini_precheck_daily_quota_reached_batch", "account_id", accountID, "used", used, "limit", limit, "reset_at", resetAt)
			result[accountID] = false
		}
	}

	// 2) Minute precheck (batch DB)
	minuteIDs := make([]int64, 0, len(quotaAccounts))
	for _, item := range quotaAccounts {
		accountID := item.account.ID
		if !result[accountID] {
			continue
		}
		if geminiMinuteLimit(item.quota, modelClass) <= 0 {
			continue
		}
		minuteIDs = append(minuteIDs, accountID)
	}
	if len(minuteIDs) == 0 {
		return result, nil
	}

	minuteTotalsByID, err := s.getGeminiUsageTotalsBatch(ctx, minuteIDs, minuteStart, now)
	if err != nil {
		return result, err
	}
	for _, item := range quotaAccounts {
		accountID := item.account.ID
		if !result[accountID] {
			continue
		}

		limit := geminiMinuteLimit(item.quota, modelClass)
		if limit <= 0 {
			continue
		}

		used := geminiUsedRequests(item.quota, modelClass, minuteTotalsByID[accountID], false)
		if used >= limit {
			resetAt := minuteStart.Add(time.Minute)
			slog.Info("gemini_precheck_minute_quota_reached_batch", "account_id", accountID, "used", used, "limit", limit, "reset_at", resetAt)
			result[accountID] = false
		}
	}

	return result, nil
}

func (s *RateLimitService) getGeminiUsageTotalsBatch(ctx context.Context, accountIDs []int64, start, end time.Time) (map[int64]GeminiUsageTotals, error) {
	result := make(map[int64]GeminiUsageTotals, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}

	ids := make([]int64, 0, len(accountIDs))
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID <= 0 {
			continue
		}
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		ids = append(ids, accountID)
	}
	if len(ids) == 0 {
		return result, nil
	}

	if batchReader, ok := s.usageRepo.(geminiUsageTotalsBatchProvider); ok {
		stats, err := batchReader.GetGeminiUsageTotalsBatch(ctx, ids, start, end)
		if err != nil {
			return nil, err
		}
		for _, accountID := range ids {
			result[accountID] = stats[accountID]
		}
		return result, nil
	}

	for _, accountID := range ids {
		stats, err := s.usageRepo.GetModelStatsWithFilters(ctx, start, end, 0, 0, accountID, 0, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		result[accountID] = geminiAggregateUsage(stats)
	}
	return result, nil
}

func geminiDailyLimit(quota GeminiQuota, modelClass geminiModelClass) int64 {
	if quota.SharedRPD > 0 {
		return quota.SharedRPD
	}
	switch modelClass {
	case geminiModelFlash:
		return quota.FlashRPD
	default:
		return quota.ProRPD
	}
}

func geminiMinuteLimit(quota GeminiQuota, modelClass geminiModelClass) int64 {
	if quota.SharedRPM > 0 {
		return quota.SharedRPM
	}
	switch modelClass {
	case geminiModelFlash:
		return quota.FlashRPM
	default:
		return quota.ProRPM
	}
}

func geminiUsedRequests(quota GeminiQuota, modelClass geminiModelClass, totals GeminiUsageTotals, daily bool) int64 {
	if daily {
		if quota.SharedRPD > 0 {
			return totals.ProRequests + totals.FlashRequests
		}
	} else {
		if quota.SharedRPM > 0 {
			return totals.ProRequests + totals.FlashRequests
		}
	}
	switch modelClass {
	case geminiModelFlash:
		return totals.FlashRequests
	default:
		return totals.ProRequests
	}
}

func (s *RateLimitService) getGeminiUsageTotals(accountID int64, windowStart, now time.Time) (GeminiUsageTotals, bool) {
	s.usageCacheMu.RLock()
	defer s.usageCacheMu.RUnlock()

	if s.usageCache == nil {
		return GeminiUsageTotals{}, false
	}

	entry, ok := s.usageCache[accountID]
	if !ok || entry == nil {
		return GeminiUsageTotals{}, false
	}
	if !entry.windowStart.Equal(windowStart) {
		return GeminiUsageTotals{}, false
	}
	if now.Sub(entry.cachedAt) >= geminiPrecheckCacheTTL {
		return GeminiUsageTotals{}, false
	}
	return entry.totals, true
}

func (s *RateLimitService) setGeminiUsageTotals(accountID int64, windowStart, now time.Time, totals GeminiUsageTotals) {
	s.usageCacheMu.Lock()
	defer s.usageCacheMu.Unlock()
	if s.usageCache == nil {
		s.usageCache = make(map[int64]*geminiUsageCacheEntry)
	}
	s.usageCache[accountID] = &geminiUsageCacheEntry{
		windowStart: windowStart,
		cachedAt:    now,
		totals:      totals,
	}
}

// GeminiCooldown returns the fallback cooldown duration for Gemini 429s based on tier.
func (s *RateLimitService) GeminiCooldown(ctx context.Context, account *Account) time.Duration {
	if account == nil {
		return 5 * time.Minute
	}
	if s.geminiQuotaService == nil {
		return 5 * time.Minute
	}
	return s.geminiQuotaService.CooldownForAccount(ctx, account)
}

// handleAuthError (401/403)，
func (s *RateLimitService) handleAuthError(ctx context.Context, account *Account, errorMsg string) {
	s.notifyAccountSchedulingBlocked(account, time.Time{}, "auth_error")
	if err := s.accountRepo.SetError(ctx, account.ID, errorMsg); err != nil {
		slog.Warn("account_set_error_failed", "account_id", account.ID, "error", err)
		return
	}
	slog.Warn("account_disabled_auth_error", "account_id", account.ID, "error", errorMsg)
}

func buildForbiddenErrorMessage(prefix string, upstreamMsg string, responseBody []byte, fallback string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix != "" && !strings.HasSuffix(prefix, " ") {
		prefix += " "
	}

	if msg := strings.TrimSpace(upstreamMsg); msg != "" {
		return prefix + msg
	}

	rawBody := bytes.TrimSpace(responseBody)
	if len(rawBody) > 0 {
		if json.Valid(rawBody) {
			var compact bytes.Buffer
			if err := json.Compact(&compact, rawBody); err == nil {
				return prefix + truncateForLog(compact.Bytes(), 512)
			}
		}
		return prefix + truncateForLog(rawBody, 512)
	}

	return prefix + fallback
}

// handle403
// Antigravity
//
func (s *RateLimitService) handle403(ctx context.Context, account *Account, upstreamMsg string, responseBody []byte) (shouldDisable bool) {
	if account.Platform == PlatformAntigravity {
		return s.handleAntigravity403(ctx, account, upstreamMsg, responseBody)
	}
	if account.Platform == PlatformOpenAI {
		return s.handleOpenAI403(ctx, account, upstreamMsg, responseBody)
	}
	//
	msg := buildForbiddenErrorMessage(
		"Access forbidden (403):",
		upstreamMsg,
		responseBody,
		"account may be suspended or lack permissions",
	)
	s.handleAuthError(ctx, account, msg)
	return true
}

func (s *RateLimitService) handleOpenAI403(ctx context.Context, account *Account, upstreamMsg string, responseBody []byte) (shouldDisable bool) {
	msg := buildForbiddenErrorMessage(
		"Access forbidden (403):",
		upstreamMsg,
		responseBody,
		"account may be suspended or lack permissions",
	)

	if s.openAI403CounterCache == nil {
		s.handleAuthError(ctx, account, msg)
		return true
	}

	count, err := s.openAI403CounterCache.IncrementOpenAI403Count(ctx, account.ID, openAI403CounterWindowMinutes)
	if err != nil {
		slog.Warn("openai_403_increment_failed", "account_id", account.ID, "error", err)
		s.handleAuthError(ctx, account, msg)
		return true
	}

	if count >= openAI403DisableThreshold {
		msg = fmt.Sprintf("%s | consecutive_403=%d/%d", msg, count, openAI403DisableThreshold)
		s.handleAuthError(ctx, account, msg)
		return true
	}

	until := time.Now().Add(time.Duration(openAI403CooldownMinutesDefault) * time.Minute)
	reason := fmt.Sprintf("OpenAI 403 temporary cooldown (%d/%d): %s", count, openAI403DisableThreshold, msg)
	s.notifyAccountSchedulingBlocked(account, until, "openai_403_temp")
	if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, reason); err != nil {
		slog.Warn("openai_403_set_temp_unschedulable_failed", "account_id", account.ID, "error", err)
		s.handleAuthError(ctx, account, msg)
		return true
	}

	slog.Warn(
		"openai_403_temp_unschedulable",
		"account_id", account.ID,
		"until", until,
		"count", count,
		"threshold", openAI403DisableThreshold,
	)
	return true
}

// handleAntigravity403
// validation（→
// violation（→
// generic（→
func (s *RateLimitService) handleAntigravity403(ctx context.Context, account *Account, upstreamMsg string, responseBody []byte) (shouldDisable bool) {
	fbType := classifyForbiddenType(string(responseBody))

	switch fbType {
	case forbiddenTypeValidation:
		// VALIDATION_REQUIRED:
		msg := buildForbiddenErrorMessage(
			"Validation required (403):",
			upstreamMsg,
			responseBody,
			"account needs Google verification",
		)
		if validationURL := extractValidationURL(string(responseBody)); validationURL != "" {
			msg += " | validation_url: " + validationURL
		}
		s.handleAuthError(ctx, account, msg)
		return true

	case forbiddenTypeViolation:
		msg := buildForbiddenErrorMessage(
			"Account violation (403):",
			upstreamMsg,
			responseBody,
			"terms of service violation",
		)
		s.handleAuthError(ctx, account, msg)
		return true

	default:
		//
		msg := buildForbiddenErrorMessage(
			"Access forbidden (403):",
			upstreamMsg,
			responseBody,
			"account may be suspended or lack permissions",
		)
		s.handleAuthError(ctx, account, msg)
		return true
	}
}

// handleCustomErrorCode
func (s *RateLimitService) handleCustomErrorCode(ctx context.Context, account *Account, statusCode int, errorMsg string) {
	msg := "Custom error code " + strconv.Itoa(statusCode) + ": " + errorMsg
	s.notifyAccountSchedulingBlocked(account, time.Time{}, "custom_error_code")
	if err := s.accountRepo.SetError(ctx, account.ID, msg); err != nil {
		slog.Warn("account_set_error_failed", "account_id", account.ID, "status_code", statusCode, "error", err)
		return
	}
	slog.Warn("account_disabled_custom_error", "account_id", account.ID, "status_code", statusCode, "error", errorMsg)
}

// handle429
func (s *RateLimitService) handle429(ctx context.Context, account *Account, headers http.Header, responseBody []byte) {
	// 1. OpenAI *
	if account.Platform == PlatformOpenAI {
		persistOpenAI429PlanType(ctx, s.accountRepo, account, responseBody)
		s.persistOpenAICodexSnapshot(ctx, account, headers)
		if resetAt := s.calculateOpenAI429ResetTime(headers); resetAt != nil {
			s.notifyAccountSchedulingBlocked(account, *resetAt, "429")
			if err := s.accountRepo.SetRateLimited(ctx, account.ID, *resetAt); err != nil {
				slog.Warn("rate_limit_set_failed", "account_id", account.ID, "error", err)
				return
			}
			slog.Info("openai_account_rate_limited", "account_id", account.ID, "reset_at", *resetAt)
			return
		}
	}

	// 2. Anthropic
	if result := calculateAnthropic429ResetTime(headers); result != nil {
		s.notifyAccountSchedulingBlocked(account, result.resetAt, "429")
		if err := s.accountRepo.SetRateLimited(ctx, account.ID, result.resetAt); err != nil {
			slog.Warn("rate_limit_set_failed", "account_id", account.ID, "error", err)
			return
		}

		//
		windowEnd := result.resetAt
		if result.fiveHourReset != nil {
			windowEnd = *result.fiveHourReset
		}
		windowStart := windowEnd.Add(-5 * time.Hour)
		if err := s.accountRepo.UpdateSessionWindow(ctx, account.ID, &windowStart, &windowEnd, "rejected"); err != nil {
			slog.Warn("rate_limit_update_session_window_failed", "account_id", account.ID, "error", err)
		}

		slog.Info("anthropic_account_rate_limited", "account_id", account.ID, "reset_at", result.resetAt, "reset_in", time.Until(result.resetAt).Truncate(time.Second))
		return
	}

	// 3.
	resetTimestamp := headers.Get("anthropic-ratelimit-unified-reset")

	// 4.
	if resetTimestamp == "" {
		switch account.Platform {
		case PlatformOpenAI:
			//
			if resetAt := parseOpenAIRateLimitResetTime(responseBody); resetAt != nil {
				resetTime := time.Unix(*resetAt, 0)
				s.notifyAccountSchedulingBlocked(account, resetTime, "429")
				if err := s.accountRepo.SetRateLimited(ctx, account.ID, resetTime); err != nil {
					slog.Warn("rate_limit_set_failed", "account_id", account.ID, "error", err)
					return
				}
				slog.Info("account_rate_limited", "account_id", account.ID, "platform", account.Platform, "reset_at", resetTime, "reset_in", time.Until(resetTime).Truncate(time.Second))
				return
			}
		case PlatformGemini, PlatformAntigravity:
			//
			if resetAt := ParseGeminiRateLimitResetTime(responseBody); resetAt != nil {
				resetTime := time.Unix(*resetAt, 0)
				s.notifyAccountSchedulingBlocked(account, resetTime, "429")
				if err := s.accountRepo.SetRateLimited(ctx, account.ID, resetTime); err != nil {
					slog.Warn("rate_limit_set_failed", "account_id", account.ID, "error", err)
					return
				}
				slog.Info("account_rate_limited", "account_id", account.ID, "platform", account.Platform, "reset_at", resetTime, "reset_in", time.Until(resetTime).Truncate(time.Second))
				return
			}
		}

		// Anthropic
		if account.Platform == PlatformAnthropic {
			slog.Warn("rate_limit_429_no_reset_time_skipped",
				"account_id", account.ID,
				"platform", account.Platform,
				"reason", "no rate limit reset time in headers, likely not a real rate limit")
			return
		}

		s.apply429FallbackRateLimit(ctx, account, "no_reset_time")
		return
	}

	//
	ts, err := strconv.ParseInt(resetTimestamp, 10, 64)
	if err != nil {
		slog.Warn("rate_limit_reset_parse_failed", "reset_timestamp", resetTimestamp, "error", err)
		s.apply429FallbackRateLimit(ctx, account, "reset_parse_failed")
		return
	}

	resetAt := time.Unix(ts, 0)

	s.notifyAccountSchedulingBlocked(account, resetAt, "429")
	if err := s.accountRepo.SetRateLimited(ctx, account.ID, resetAt); err != nil {
		slog.Warn("rate_limit_set_failed", "account_id", account.ID, "error", err)
		return
	}

	windowEnd := resetAt
	windowStart := resetAt.Add(-5 * time.Hour)
	if err := s.accountRepo.UpdateSessionWindow(ctx, account.ID, &windowStart, &windowEnd, "rejected"); err != nil {
		slog.Warn("rate_limit_update_session_window_failed", "account_id", account.ID, "error", err)
	}

	slog.Info("account_rate_limited", "account_id", account.ID, "reset_at", resetAt)
}

func (s *RateLimitService) apply429FallbackRateLimit(ctx context.Context, account *Account, reason string) {
	cooldown, enabled := s.get429FallbackCooldown(ctx, account)
	if !enabled {
		slog.Info("rate_limit_429_fallback_ignored", "account_id", account.ID, "platform", account.Platform, "reason", reason)
		return
	}

	resetAt := time.Now().Add(cooldown)
	slog.Warn("rate_limit_429_fallback_used", "account_id", account.ID, "platform", account.Platform, "reason", reason, "using_default", cooldown.String())
	s.notifyAccountSchedulingBlocked(account, resetAt, "429_fallback")
	if err := s.accountRepo.SetRateLimited(ctx, account.ID, resetAt); err != nil {
		slog.Warn("rate_limit_set_failed", "account_id", account.ID, "error", err)
	}
}

func (s *RateLimitService) get429FallbackCooldown(ctx context.Context, account *Account) (time.Duration, bool) {
	if s.settingService != nil {
		settings, err := s.settingService.GetRateLimit429CooldownSettings(ctx)
		if err == nil && settings != nil {
			if !settings.Enabled {
				return 0, false
			}
			seconds := clampRateLimit429CooldownSeconds(settings.CooldownSeconds)
			return time.Duration(seconds) * time.Second, true
		}
		slog.Warn("rate_limit_429_settings_read_failed", "account_id", account.ID, "error", err)
	}

	seconds := defaultRateLimit429CooldownSeconds
	seconds = clampRateLimit429CooldownSeconds(seconds)
	return time.Duration(seconds) * time.Second, true
}

func clampRateLimit429CooldownSeconds(seconds int) int {
	if seconds < 1 {
		return 1
	}
	if seconds > maxRateLimit429CooldownSeconds {
		return maxRateLimit429CooldownSeconds
	}
	return seconds
}

// calculateOpenAI429ResetTime
//
func calculateOpenAI429ResetTime(headers http.Header) *time.Time {
	snapshot := ParseCodexRateLimitHeaders(headers)
	if snapshot == nil {
		return nil
	}

	normalized := snapshot.Normalize()
	if normalized == nil {
		return nil
	}

	now := time.Now()

	// >= 100）
	is7dExhausted := normalized.Used7dPercent != nil && *normalized.Used7dPercent >= 100
	is5hExhausted := normalized.Used5hPercent != nil && *normalized.Used5hPercent >= 100

	if is7dExhausted && normalized.Reset7dSeconds != nil {
		resetAt := now.Add(time.Duration(*normalized.Reset7dSeconds) * time.Second)
		slog.Info("openai_429_7d_limit_exhausted", "reset_after_seconds", *normalized.Reset7dSeconds, "reset_at", resetAt)
		return &resetAt
	}
	if is5hExhausted && normalized.Reset5hSeconds != nil {
		resetAt := now.Add(time.Duration(*normalized.Reset5hSeconds) * time.Second)
		slog.Info("openai_429_5h_limit_exhausted", "reset_after_seconds", *normalized.Reset5hSeconds, "reset_at", resetAt)
		return &resetAt
	}

	// %
	var maxResetSecs int
	if normalized.Reset7dSeconds != nil && *normalized.Reset7dSeconds > maxResetSecs {
		maxResetSecs = *normalized.Reset7dSeconds
	}
	if normalized.Reset5hSeconds != nil && *normalized.Reset5hSeconds > maxResetSecs {
		maxResetSecs = *normalized.Reset5hSeconds
	}
	if maxResetSecs > 0 {
		resetAt := now.Add(time.Duration(maxResetSecs) * time.Second)
		slog.Info("openai_429_using_max_reset", "max_reset_seconds", maxResetSecs, "reset_at", resetAt)
		return &resetAt
	}

	return nil
}

func (s *RateLimitService) calculateOpenAI429ResetTime(headers http.Header) *time.Time {
	return calculateOpenAI429ResetTime(headers)
}

// anthropic429Result holds the parsed Anthropic 429 rate-limit information.
type anthropic429Result struct {
	resetAt       time.Time  // The correct reset time to use for SetRateLimited
	fiveHourReset *time.Time // 5h window reset timestamp (for session window calculation), nil if not available
}

// calculateAnthropic429ResetTime parses Anthropic's per-window rate-limit headers
// to determine which window (5h or 7d) actually triggered the 429.
//
// Headers used:
//   - anthropic-ratelimit-unified-5h-utilization / anthropic-ratelimit-unified-5h-surpassed-threshold
//   - anthropic-ratelimit-unified-5h-reset
//   - anthropic-ratelimit-unified-7d-utilization / anthropic-ratelimit-unified-7d-surpassed-threshold
//   - anthropic-ratelimit-unified-7d-reset
//
// Returns nil when the per-window headers are absent (caller should fall back to
// the aggregated anthropic-ratelimit-unified-reset header).
func calculateAnthropic429ResetTime(headers http.Header) *anthropic429Result {
	reset5hStr := headers.Get("anthropic-ratelimit-unified-5h-reset")
	reset7dStr := headers.Get("anthropic-ratelimit-unified-7d-reset")

	if reset5hStr == "" && reset7dStr == "" {
		return nil
	}

	var reset5h, reset7d *time.Time
	if ts, err := strconv.ParseInt(reset5hStr, 10, 64); err == nil {
		t := time.Unix(ts, 0)
		reset5h = &t
	}
	if ts, err := strconv.ParseInt(reset7dStr, 10, 64); err == nil {
		t := time.Unix(ts, 0)
		reset7d = &t
	}

	is5hExceeded := isAnthropicWindowExceeded(headers, "5h")
	is7dExceeded := isAnthropicWindowExceeded(headers, "7d")

	slog.Info("anthropic_429_window_analysis",
		"is_5h_exceeded", is5hExceeded,
		"is_7d_exceeded", is7dExceeded,
		"reset_5h", reset5hStr,
		"reset_7d", reset7dStr,
	)

	// Select the correct reset time based on which window(s) are exceeded.
	var chosen *time.Time
	switch {
	case is5hExceeded && is7dExceeded:
		// Both exceeded → prefer 7d (longer cooldown), fall back to 5h
		chosen = reset7d
		if chosen == nil {
			chosen = reset5h
		}
	case is5hExceeded:
		chosen = reset5h
	case is7dExceeded:
		chosen = reset7d
	default:
		// Neither flag clearly exceeded — pick the sooner reset as best guess
		chosen = pickSooner(reset5h, reset7d)
	}

	if chosen == nil {
		return nil
	}
	return &anthropic429Result{resetAt: *chosen, fiveHourReset: reset5h}
}

// isAnthropicWindowExceeded checks whether a given Anthropic rate-limit window
// (e.g. "5h" or "7d") has been exceeded, using utilization and surpassed-threshold headers.
func isAnthropicWindowExceeded(headers http.Header, window string) bool {
	prefix := "anthropic-ratelimit-unified-" + window + "-"

	// Check surpassed-threshold first (most explicit signal)
	if st := headers.Get(prefix + "surpassed-threshold"); strings.EqualFold(st, "true") {
		return true
	}

	// Fall back to utilization >= 1.0
	if utilStr := headers.Get(prefix + "utilization"); utilStr != "" {
		if util, err := strconv.ParseFloat(utilStr, 64); err == nil && util >= 1.0-1e-9 {
			// Use a small epsilon to handle floating point: treat 0.9999999... as >= 1.0
			return true
		}
	}

	return false
}

// pickSooner returns whichever of the two time pointers is earlier.
// If only one is non-nil, it is returned. If both are nil, returns nil.
func pickSooner(a, b *time.Time) *time.Time {
	switch {
	case a != nil && b != nil:
		if a.Before(*b) {
			return a
		}
		return b
	case a != nil:
		return a
	default:
		return b
	}
}

func (s *RateLimitService) persistOpenAICodexSnapshot(ctx context.Context, account *Account, headers http.Header) {
	if s == nil || s.accountRepo == nil || account == nil || headers == nil {
		return
	}
	snapshot := ParseCodexRateLimitHeaders(headers)
	if snapshot == nil {
		return
	}
	updates := buildCodexUsageExtraUpdates(snapshot, time.Now())
	if len(updates) == 0 {
		return
	}
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		slog.Warn("openai_codex_snapshot_persist_failed", "account_id", account.ID, "error", err)
	}
}

// parseOpenAIRateLimitResetTime
// OpenAI
//
//	{
//	  "error": {
//	    "message": "The usage limit has been reached",
//	    "type": "usage_limit_reached",
//	    "resets_at": 1769404154,
//	    "resets_in_seconds": 133107
//	  }
//	}
func parseOpenAIRateLimitResetTime(body []byte) *int64 {
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}

	errObj, ok := parsed["error"].(map[string]any)
	if !ok {
		return nil
	}

	//
	errType, _ := errObj["type"].(string)
	if errType != "usage_limit_reached" && errType != "rate_limit_exceeded" {
		return nil
	}

	//
	if resetsAt, ok := errObj["resets_at"].(float64); ok {
		ts := int64(resetsAt)
		return &ts
	}
	if resetsAt, ok := errObj["resets_at"].(string); ok {
		if ts, err := strconv.ParseInt(resetsAt, 10, 64); err == nil {
			return &ts
		}
	}

	//
	if resetsInSeconds, ok := errObj["resets_in_seconds"].(float64); ok {
		ts := time.Now().Unix() + int64(resetsInSeconds)
		return &ts
	}
	if resetsInSeconds, ok := errObj["resets_in_seconds"].(string); ok {
		if sec, err := strconv.ParseInt(resetsInSeconds, 10, 64); err == nil {
			ts := time.Now().Unix() + sec
			return &ts
		}
	}

	return nil
}

func parseOpenAIRateLimitPlanType(body []byte) string {
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}

	errObj, ok := parsed["error"].(map[string]any)
	if !ok {
		return ""
	}

	errType, _ := errObj["type"].(string)
	if errType != "usage_limit_reached" && errType != "rate_limit_exceeded" {
		return ""
	}

	planType, _ := errObj["plan_type"].(string)
	return strings.ToLower(strings.TrimSpace(planType))
}

func persistOpenAI429PlanType(ctx context.Context, repo AccountRepository, account *Account, body []byte) {
	if repo == nil || account == nil || account.Platform != PlatformOpenAI {
		return
	}

	planType := parseOpenAIRateLimitPlanType(body)
	if planType == "" {
		return
	}

	current := strings.TrimSpace(account.GetCredential("plan_type"))
	if strings.EqualFold(current, planType) {
		return
	}

	if _, err := repo.BulkUpdate(ctx, []int64{account.ID}, AccountBulkUpdate{
		Credentials: map[string]any{"plan_type": planType},
	}); err != nil {
		slog.Warn("openai_429_plan_type_sync_failed", "account_id", account.ID, "plan_type", planType, "error", err)
		return
	}

	if account.Credentials == nil {
		account.Credentials = make(map[string]any, 1)
	}
	account.Credentials["plan_type"] = planType
	slog.Info("openai_429_plan_type_synced", "account_id", account.ID, "previous_plan_type", current, "plan_type", planType)
}

// handle529
func (s *RateLimitService) handle529(ctx context.Context, account *Account) {
	var settings *OverloadCooldownSettings
	if s.settingService != nil {
		var err error
		settings, err = s.settingService.GetOverloadCooldownSettings(ctx)
		if err != nil {
			slog.Warn("overload_settings_read_failed", "account_id", account.ID, "error", err)
			settings = nil
		}
	}
	if settings == nil {
		cooldown := s.cfg.RateLimit.OverloadCooldownMinutes
		if cooldown <= 0 {
			cooldown = 10
		}
		settings = &OverloadCooldownSettings{Enabled: true, CooldownMinutes: cooldown}
	}

	if !settings.Enabled {
		slog.Info("account_529_ignored", "account_id", account.ID, "reason", "overload_cooldown_disabled")
		return
	}

	cooldownMinutes := settings.CooldownMinutes
	if cooldownMinutes <= 0 {
		cooldownMinutes = 10
	}

	until := time.Now().Add(time.Duration(cooldownMinutes) * time.Minute)
	s.notifyAccountSchedulingBlocked(account, until, "529")
	if err := s.accountRepo.SetOverloaded(ctx, account.ID, until); err != nil {
		slog.Warn("overload_set_failed", "account_id", account.ID, "error", err)
		return
	}

	slog.Info("account_overloaded", "account_id", account.ID, "until", until)
}

// UpdateSessionWindow
func (s *RateLimitService) UpdateSessionWindow(ctx context.Context, account *Account, headers http.Header) {
	status := headers.Get("anthropic-ratelimit-unified-5h-status")
	if status == "" {
		return
	}

	//
	var windowStart, windowEnd *time.Time
	needInitWindow := account.SessionWindowEnd == nil || time.Now().After(*account.SessionWindowEnd)

	if resetStr := headers.Get("anthropic-ratelimit-unified-5h-reset"); resetStr != "" {
		if ts, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
			//
			if ts > 1e11 {
				slog.Warn("account_session_window_header_millis_detected", "account_id", account.ID, "raw_reset", resetStr)
				ts = ts / 1000
			}
			end := time.Unix(ts, 0)
			//
			minAllowed := time.Now().Add(-5 * time.Hour)
			maxAllowed := time.Now().Add(7 * 24 * time.Hour)
			if end.Before(minAllowed) || end.After(maxAllowed) {
				slog.Warn("account_session_window_header_out_of_range", "account_id", account.ID, "raw_reset", resetStr, "parsed_end", end)
			} else if needInitWindow || account.SessionWindowEnd == nil || !end.Equal(*account.SessionWindowEnd) {
				start := end.Add(-5 * time.Hour)
				windowStart = &start
				windowEnd = &end
				slog.Info("account_session_window_from_header", "account_id", account.ID, "window_start", start, "window_end", end, "status", status)
			}
		} else {
			slog.Warn("account_session_window_header_parse_failed", "account_id", account.ID, "raw_reset", resetStr, "error", err)
		}
	}

	if windowEnd == nil && needInitWindow && (status == "allowed" || status == "allowed_warning") {
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
		end := start.Add(5 * time.Hour)
		windowStart = &start
		windowEnd = &end
		slog.Info("account_session_window_initialized", "account_id", account.ID, "window_start", start, "window_end", end, "status", status)
	}

	//
	if windowEnd != nil && needInitWindow {
		_ = s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
			"session_window_utilization":   nil,
			"passive_usage_7d_utilization": nil,
			"passive_usage_7d_reset":       nil,
			"passive_usage_sampled_at":     nil,
		})
	}

	if err := s.accountRepo.UpdateSessionWindow(ctx, account.ID, windowStart, windowEnd, status); err != nil {
		slog.Warn("session_window_update_failed", "account_id", account.ID, "error", err)
	}

	// + 7d utilization，
	extraUpdates := make(map[string]any, 4)
	// 5h utilization（0-1
	if utilStr := headers.Get("anthropic-ratelimit-unified-5h-utilization"); utilStr != "" {
		if util, err := strconv.ParseFloat(utilStr, 64); err == nil {
			extraUpdates["session_window_utilization"] = util
		}
	}
	// 7d utilization（0-1
	if utilStr := headers.Get("anthropic-ratelimit-unified-7d-utilization"); utilStr != "" {
		if util, err := strconv.ParseFloat(utilStr, 64); err == nil {
			extraUpdates["passive_usage_7d_utilization"] = util
		}
	}
	// 7d reset timestamp
	if resetStr := headers.Get("anthropic-ratelimit-unified-7d-reset"); resetStr != "" {
		if ts, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
			if ts > 1e11 {
				ts = ts / 1000
			}
			extraUpdates["passive_usage_7d_reset"] = ts
		}
	}
	if len(extraUpdates) > 0 {
		extraUpdates["passive_usage_sampled_at"] = time.Now().UTC().Format(time.RFC3339)
		if err := s.accountRepo.UpdateExtra(ctx, account.ID, extraUpdates); err != nil {
			slog.Warn("passive_usage_update_failed", "account_id", account.ID, "error", err)
		}
	}

	//
	if status == "allowed" && account.IsRateLimited() {
		if err := s.ClearRateLimit(ctx, account.ID); err != nil {
			slog.Warn("rate_limit_clear_failed", "account_id", account.ID, "error", err)
		}
	}
}

// ClearRateLimit
func (s *RateLimitService) ClearRateLimit(ctx context.Context, accountID int64) error {
	if err := s.accountRepo.ClearRateLimit(ctx, accountID); err != nil {
		return err
	}
	if err := s.accountRepo.ClearAntigravityQuotaScopes(ctx, accountID); err != nil {
		return err
	}
	if err := s.accountRepo.ClearModelRateLimits(ctx, accountID); err != nil {
		return err
	}
	if err := s.accountRepo.ClearTempUnschedulable(ctx, accountID); err != nil {
		return err
	}
	if s.tempUnschedCache != nil {
		if err := s.tempUnschedCache.DeleteTempUnsched(ctx, accountID); err != nil {
			slog.Warn("temp_unsched_cache_delete_failed", "account_id", accountID, "error", err)
		}
	}
	s.ResetOpenAI403Counter(ctx, accountID)
	s.notifyAccountSchedulingBlockCleared(accountID)
	return nil
}

func (s *RateLimitService) ResetOpenAI403Counter(ctx context.Context, accountID int64) {
	if s == nil || s.openAI403CounterCache == nil || accountID <= 0 {
		return
	}
	if err := s.openAI403CounterCache.ResetOpenAI403Count(ctx, accountID); err != nil {
		slog.Warn("openai_403_reset_failed", "account_id", accountID, "error", err)
	}
}

// RecoverAccountState
func (s *RateLimitService) RecoverAccountState(ctx context.Context, accountID int64, options AccountRecoveryOptions) (*SuccessfulTestRecoveryResult, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}

	result := &SuccessfulTestRecoveryResult{}
	if account.Status == StatusError {
		if err := s.accountRepo.ClearError(ctx, accountID); err != nil {
			return nil, err
		}
		result.ClearedError = true
		if options.InvalidateToken && s.tokenCacheInvalidator != nil && account.IsOAuth() {
			if invalidateErr := s.tokenCacheInvalidator.InvalidateToken(ctx, account); invalidateErr != nil {
				slog.Warn("recover_account_state_invalidate_token_failed", "account_id", accountID, "error", invalidateErr)
			}
		}
	}

	if hasRecoverableRuntimeState(account) {
		if err := s.ClearRateLimit(ctx, accountID); err != nil {
			return nil, err
		}
		result.ClearedRateLimit = true
	}
	if result.ClearedError || result.ClearedRateLimit {
		s.ResetOpenAI403Counter(ctx, accountID)
		if result.ClearedError && !result.ClearedRateLimit {
			s.notifyAccountSchedulingBlockCleared(accountID)
		}
	}

	return result, nil
}

// RecoverAccountAfterSuccessfulTest
//
func (s *RateLimitService) RecoverAccountAfterSuccessfulTest(ctx context.Context, accountID int64) (*SuccessfulTestRecoveryResult, error) {
	return s.RecoverAccountState(ctx, accountID, AccountRecoveryOptions{})
}

func (s *RateLimitService) ClearTempUnschedulable(ctx context.Context, accountID int64) error {
	if err := s.accountRepo.ClearTempUnschedulable(ctx, accountID); err != nil {
		return err
	}
	if s.tempUnschedCache != nil {
		if err := s.tempUnschedCache.DeleteTempUnsched(ctx, accountID); err != nil {
			slog.Warn("temp_unsched_cache_delete_failed", "account_id", accountID, "error", err)
		}
	}
	if err := s.accountRepo.ClearModelRateLimits(ctx, accountID); err != nil {
		slog.Warn("clear_model_rate_limits_on_temp_unsched_reset_failed", "account_id", accountID, "error", err)
	}
	s.notifyAccountSchedulingBlockCleared(accountID)
	return nil
}

func hasRecoverableRuntimeState(account *Account) bool {
	if account == nil {
		return false
	}
	if account.RateLimitedAt != nil || account.RateLimitResetAt != nil || account.OverloadUntil != nil || account.TempUnschedulableUntil != nil {
		return true
	}
	if len(account.Extra) == 0 {
		return false
	}
	return hasNonEmptyMapValue(account.Extra, "model_rate_limits") ||
		hasNonEmptyMapValue(account.Extra, "antigravity_quota_scopes")
}

func hasNonEmptyMapValue(extra map[string]any, key string) bool {
	raw, ok := extra[key]
	if !ok || raw == nil {
		return false
	}
	switch typed := raw.(type) {
	case map[string]any:
		return len(typed) > 0
	case map[string]string:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	default:
		return true
	}
}

func (s *RateLimitService) GetTempUnschedStatus(ctx context.Context, accountID int64) (*TempUnschedState, error) {
	now := time.Now().Unix()
	if s.tempUnschedCache != nil {
		state, err := s.tempUnschedCache.GetTempUnsched(ctx, accountID)
		if err != nil {
			return nil, err
		}
		if state != nil && state.UntilUnix > now {
			return state, nil
		}
	}

	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.TempUnschedulableUntil == nil {
		return nil, nil
	}
	if account.TempUnschedulableUntil.Unix() <= now {
		return nil, nil
	}

	state := &TempUnschedState{
		UntilUnix: account.TempUnschedulableUntil.Unix(),
	}

	if account.TempUnschedulableReason != "" {
		var parsed TempUnschedState
		if err := json.Unmarshal([]byte(account.TempUnschedulableReason), &parsed); err == nil {
			if parsed.UntilUnix == 0 {
				parsed.UntilUnix = state.UntilUnix
			}
			state = &parsed
		} else {
			state.ErrorMessage = account.TempUnschedulableReason
		}
	}

	if s.tempUnschedCache != nil {
		if err := s.tempUnschedCache.SetTempUnsched(ctx, accountID, state); err != nil {
			slog.Warn("temp_unsched_cache_set_failed", "account_id", accountID, "error", err)
		}
	}

	return state, nil
}

func (s *RateLimitService) HandleTempUnschedulable(ctx context.Context, account *Account, statusCode int, responseBody []byte) bool {
	if account == nil {
		return false
	}
	if !account.ShouldHandleErrorCode(statusCode) {
		return false
	}
	return s.tryTempUnschedulable(ctx, account, statusCode, responseBody)
}

func (s *RateLimitService) HandleOpenAIImageRateLimit(ctx context.Context, account *Account, statusCode int, headers http.Header, responseBody []byte) bool {
	if s == nil || account == nil || s.accountRepo == nil {
		return false
	}
	if account.Platform != PlatformOpenAI {
		return false
	}
	if !account.ShouldHandleErrorCode(statusCode) {
		slog.Info("openai_image_rate_limit_skipped_by_error_code_policy", "account_id", account.ID, "status_code", statusCode)
		return false
	}
	if !isOpenAIImageRateLimitError(statusCode, responseBody) {
		return false
	}

	resetAt := openAIImageRateLimitResetAt(headers, responseBody)
	if err := s.accountRepo.SetModelRateLimit(ctx, account.ID, openAIImageGenerationRateLimitKey, resetAt, openAIImageRateLimitReason); err != nil {
		slog.Warn("openai_image_rate_limit_set_model_rate_limit_failed", "account_id", account.ID, "scope", openAIImageGenerationRateLimitKey, "error", err)
		return true
	}
	slog.Info("openai_image_rate_limited", "account_id", account.ID, "scope", openAIImageGenerationRateLimitKey, "reset_at", resetAt, "reset_in", time.Until(resetAt).Truncate(time.Second))
	return true
}

func isOpenAIImageRateLimitError(statusCode int, body []byte) bool {
	if statusCode != http.StatusTooManyRequests || len(body) == 0 {
		return false
	}
	lower := strings.ToLower(string(body))
	for _, marker := range []string{
		"for limit gpt-image",
		"input-images per min",
		"gpt-image-2-codex",
		"gpt-image",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func openAIImageRateLimitResetAt(headers http.Header, body []byte) time.Time {
	now := time.Now()
	if resetAt := parseRetryAfterResetTime(headers, now); resetAt != nil && resetAt.After(now) {
		return *resetAt
	}
	if resetAt := calculateOpenAI429ResetTime(headers); resetAt != nil && resetAt.After(now) {
		return *resetAt
	}
	if resetUnix := parseOpenAIRateLimitResetTime(body); resetUnix != nil {
		if resetAt := time.Unix(*resetUnix, 0); resetAt.After(now) {
			return resetAt
		}
	}
	if cooldown := parseOpenAIImageTryAgainCooldown(body); cooldown > 0 {
		return now.Add(cooldown)
	}
	return now.Add(openAIImageRateLimitDefaultCooldown)
}

func parseRetryAfterResetTime(headers http.Header, now time.Time) *time.Time {
	if headers == nil {
		return nil
	}
	raw := strings.TrimSpace(headers.Get("Retry-After"))
	if raw == "" {
		return nil
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		resetAt := now.Add(time.Duration(seconds * float64(time.Second)))
		return &resetAt
	}
	if parsed, err := http.ParseTime(raw); err == nil {
		return &parsed
	}
	return nil
}

func parseOpenAIImageTryAgainCooldown(body []byte) time.Duration {
	if len(body) == 0 {
		return 0
	}
	match := openAIImageTryAgainPattern.FindSubmatch(body)
	if len(match) != 3 {
		return 0
	}
	value, err := strconv.ParseFloat(string(match[1]), 64)
	if err != nil || value <= 0 {
		return 0
	}
	switch strings.ToLower(string(match[2])) {
	case "ms":
		return time.Duration(value * float64(time.Millisecond))
	case "s", "sec", "secs", "second", "seconds":
		return time.Duration(value * float64(time.Second))
	case "m", "min", "mins", "minute", "minutes":
		return time.Duration(value * float64(time.Minute))
	default:
		return 0
	}
}

const upstreamModelNotFoundCooldown = 30 * time.Minute
const upstreamModelNotFoundReason = "upstream_404_model_not_found"
const tempUnschedBodyMaxBytes = 64 << 10
const tempUnschedMessageMaxBytes = 2048

func (s *RateLimitService) HandleUpstreamModelNotFound(ctx context.Context, account *Account, requestedModel string, statusCode int, responseBody []byte) bool {
	if s == nil || account == nil || s.accountRepo == nil {
		return false
	}
	if !account.ShouldHandleErrorCode(statusCode) {
		return false
	}
	if !isUpstreamModelNotFoundError(statusCode, responseBody) {
		return false
	}
	modelKey := modelRateLimitKeyForUpstreamModelNotFound(ctx, account, requestedModel)
	if modelKey == "" {
		return false
	}
	resetAt := time.Now().Add(upstreamModelNotFoundCooldown)
	if err := s.accountRepo.SetModelRateLimit(ctx, account.ID, modelKey, resetAt, upstreamModelNotFoundReason); err != nil {
		slog.Warn("upstream_model_not_found_set_model_rate_limit_failed", "account_id", account.ID, "model", modelKey, "error", err)
		return true
	}
	slog.Info("upstream_model_not_found_model_rate_limited", "account_id", account.ID, "model", modelKey, "reset_at", resetAt)
	return true
}

func modelRateLimitKeyForUpstreamModelNotFound(ctx context.Context, account *Account, requestedModel string) string {
	modelKey := strings.TrimSpace(requestedModel)
	if account == nil || modelKey == "" {
		return modelKey
	}
	if account.Platform == PlatformAntigravity {
		if resolved := strings.TrimSpace(resolveFinalAntigravityModelKey(ctx, account, modelKey)); resolved != "" {
			return resolved
		}
		return modelKey
	}
	if mapped := strings.TrimSpace(account.GetMappedModel(modelKey)); mapped != "" {
		return mapped
	}
	return modelKey
}

func (s *RateLimitService) tryTempUnschedulable(ctx context.Context, account *Account, statusCode int, responseBody []byte) bool {
	if account == nil {
		return false
	}
	if !account.IsTempUnschedulableEnabled() {
		return false
	}
	// 401
	//
	// Antigravity
	if statusCode == http.StatusUnauthorized && account.Platform != PlatformAntigravity {
		reason := account.TempUnschedulableReason
		//
		if reason == "" {
			if dbAcc, err := s.accountRepo.GetByID(ctx, account.ID); err == nil && dbAcc != nil {
				reason = dbAcc.TempUnschedulableReason
			}
		}
		if wasTempUnschedByStatusCode(reason, statusCode) {
			slog.Info("401_escalated_to_error", "account_id", account.ID,
				"reason", "previous temp-unschedulable was also 401")
			return false
		}
	}
	rules := account.GetTempUnschedulableRules()
	if len(rules) == 0 {
		return false
	}
	if statusCode <= 0 || len(responseBody) == 0 {
		return false
	}

	body := responseBody
	if len(body) > tempUnschedBodyMaxBytes {
		body = body[:tempUnschedBodyMaxBytes]
	}
	bodyLower := strings.ToLower(string(body))

	for idx, rule := range rules {
		if rule.ErrorCode != statusCode || len(rule.Keywords) == 0 {
			continue
		}
		matchedKeyword := matchTempUnschedKeyword(bodyLower, rule.Keywords)
		if matchedKeyword == "" {
			continue
		}

		if s.triggerTempUnschedulable(ctx, account, rule, idx, statusCode, matchedKeyword, responseBody) {
			return true
		}
	}

	return false
}

func wasTempUnschedByStatusCode(reason string, statusCode int) bool {
	if statusCode <= 0 {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return false
	}

	var state TempUnschedState
	if err := json.Unmarshal([]byte(reason), &state); err != nil {
		return false
	}
	return state.StatusCode == statusCode
}

func matchTempUnschedKeyword(bodyLower string, keywords []string) string {
	if bodyLower == "" {
		return ""
	}
	for _, keyword := range keywords {
		k := strings.TrimSpace(keyword)
		if k == "" {
			continue
		}
		if strings.Contains(bodyLower, strings.ToLower(k)) {
			return k
		}
	}
	return ""
}

func (s *RateLimitService) triggerTempUnschedulable(ctx context.Context, account *Account, rule TempUnschedulableRule, ruleIndex int, statusCode int, matchedKeyword string, responseBody []byte) bool {
	if account == nil {
		return false
	}
	if rule.DurationMinutes <= 0 {
		return false
	}

	now := time.Now()
	until := now.Add(time.Duration(rule.DurationMinutes) * time.Minute)

	state := &TempUnschedState{
		UntilUnix:       until.Unix(),
		TriggeredAtUnix: now.Unix(),
		StatusCode:      statusCode,
		MatchedKeyword:  matchedKeyword,
		RuleIndex:       ruleIndex,
		ErrorMessage:    truncateTempUnschedMessage(responseBody, tempUnschedMessageMaxBytes),
	}

	reason := ""
	if raw, err := json.Marshal(state); err == nil {
		reason = string(raw)
	}
	if reason == "" {
		reason = strings.TrimSpace(state.ErrorMessage)
	}

	s.notifyAccountSchedulingBlocked(account, until, "temp_unschedulable")
	if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, reason); err != nil {
		slog.Warn("temp_unsched_set_failed", "account_id", account.ID, "error", err)
		return false
	}

	if s.tempUnschedCache != nil {
		if err := s.tempUnschedCache.SetTempUnsched(ctx, account.ID, state); err != nil {
			slog.Warn("temp_unsched_cache_set_failed", "account_id", account.ID, "error", err)
		}
	}

	slog.Info("account_temp_unschedulable", "account_id", account.ID, "until", until, "rule_index", ruleIndex, "status_code", statusCode)
	return true
}

func truncateTempUnschedMessage(body []byte, maxBytes int) string {
	if maxBytes <= 0 || len(body) == 0 {
		return ""
	}
	if len(body) > maxBytes {
		body = body[:maxBytes]
	}
	return strings.TrimSpace(string(body))
}

// HandleStreamTimeout
func (s *RateLimitService) HandleStreamTimeout(ctx context.Context, account *Account, model string) bool {
	if account == nil {
		return false
	}

	if s.settingService == nil {
		slog.Warn("stream_timeout_setting_service_missing", "account_id", account.ID)
		return false
	}

	settings, err := s.settingService.GetStreamTimeoutSettings(ctx)
	if err != nil {
		slog.Warn("stream_timeout_get_settings_failed", "account_id", account.ID, "error", err)
		return false
	}

	if !settings.Enabled {
		return false
	}

	if settings.Action == StreamTimeoutActionNone {
		return false
	}

	var count int64 = 1
	if s.timeoutCounterCache != nil {
		count, err = s.timeoutCounterCache.IncrementTimeoutCount(ctx, account.ID, settings.ThresholdWindowMinutes)
		if err != nil {
			slog.Warn("stream_timeout_increment_count_failed", "account_id", account.ID, "error", err)
			// =1
			count = 1
		}
	}

	slog.Info("stream_timeout_count", "account_id", account.ID, "count", count, "threshold", settings.ThresholdCount, "window_minutes", settings.ThresholdWindowMinutes, "model", model)

	if count < int64(settings.ThresholdCount) {
		return false
	}

	switch settings.Action {
	case StreamTimeoutActionTempUnsched:
		return s.triggerStreamTimeoutTempUnsched(ctx, account, settings, model)
	case StreamTimeoutActionError:
		return s.triggerStreamTimeoutError(ctx, account, model)
	default:
		return false
	}
}

// triggerStreamTimeoutTempUnsched
func (s *RateLimitService) triggerStreamTimeoutTempUnsched(ctx context.Context, account *Account, settings *StreamTimeoutSettings, model string) bool {
	now := time.Now()
	until := now.Add(time.Duration(settings.TempUnschedMinutes) * time.Minute)

	state := &TempUnschedState{
		UntilUnix:       until.Unix(),
		TriggeredAtUnix: now.Unix(),
		StatusCode:      0, // timeout没有status code
		MatchedKeyword:  "stream_timeout",
		RuleIndex:       -1, // 表示系统级规则
		ErrorMessage:    "Stream data interval timeout for model: " + model,
	}

	reason := ""
	if raw, err := json.Marshal(state); err == nil {
		reason = string(raw)
	}
	if reason == "" {
		reason = state.ErrorMessage
	}

	s.notifyAccountSchedulingBlocked(account, until, "stream_timeout_temp_unschedulable")
	if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, reason); err != nil {
		slog.Warn("stream_timeout_set_temp_unsched_failed", "account_id", account.ID, "error", err)
		return false
	}

	if s.tempUnschedCache != nil {
		if err := s.tempUnschedCache.SetTempUnsched(ctx, account.ID, state); err != nil {
			slog.Warn("stream_timeout_set_temp_unsched_cache_failed", "account_id", account.ID, "error", err)
		}
	}

	if s.timeoutCounterCache != nil {
		if err := s.timeoutCounterCache.ResetTimeoutCount(ctx, account.ID); err != nil {
			slog.Warn("stream_timeout_reset_count_failed", "account_id", account.ID, "error", err)
		}
	}

	slog.Info("stream_timeout_temp_unschedulable", "account_id", account.ID, "until", until, "model", model)
	return true
}

// triggerStreamTimeoutError
func (s *RateLimitService) triggerStreamTimeoutError(ctx context.Context, account *Account, model string) bool {
	errorMsg := "Stream data interval timeout (repeated failures) for model: " + model

	s.notifyAccountSchedulingBlocked(account, time.Time{}, "stream_timeout_error")
	if err := s.accountRepo.SetError(ctx, account.ID, errorMsg); err != nil {
		slog.Warn("stream_timeout_set_error_failed", "account_id", account.ID, "error", err)
		return false
	}

	if s.timeoutCounterCache != nil {
		if err := s.timeoutCounterCache.ResetTimeoutCount(ctx, account.ID); err != nil {
			slog.Warn("stream_timeout_reset_count_failed", "account_id", account.ID, "error", err)
		}
	}

	slog.Warn("stream_timeout_account_error", "account_id", account.ID, "model", model)
	return true
}
