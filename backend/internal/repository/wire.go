package repository

import (
	"database/sql"
	"errors"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
)

// ProvideConcurrencyCache
//
func ProvideConcurrencyCache(rdb *redis.Client, cfg *config.Config) service.ConcurrencyCache {
	waitTTLSeconds := int(cfg.Gateway.Scheduling.StickySessionWaitTimeout.Seconds())
	if cfg.Gateway.Scheduling.FallbackWaitTimeout > cfg.Gateway.Scheduling.StickySessionWaitTimeout {
		waitTTLSeconds = int(cfg.Gateway.Scheduling.FallbackWaitTimeout.Seconds())
	}
	if waitTTLSeconds <= 0 {
		waitTTLSeconds = cfg.Gateway.ConcurrencySlotTTLMinutes * 60
	}
	return NewConcurrencyCache(rdb, cfg.Gateway.ConcurrencySlotTTLMinutes, waitTTLSeconds)
}

// ProvideGitHubReleaseClient
//
func ProvideGitHubReleaseClient(cfg *config.Config) service.GitHubReleaseClient {
	return NewGitHubReleaseClient(cfg.Update.ProxyURL, cfg.Security.ProxyFallback.AllowDirectOnError)
}

// ProvidePricingRemoteClient
//
func ProvidePricingRemoteClient(cfg *config.Config) service.PricingRemoteClient {
	return NewPricingRemoteClient(cfg.Update.ProxyURL, cfg.Security.ProxyFallback.AllowDirectOnError)
}

// ProvideSessionLimitCache
//
func ProvideSessionLimitCache(rdb *redis.Client, cfg *config.Config) service.SessionLimitCache {
	defaultIdleTimeoutMinutes := 5 // 默认 5 minutes空闲timeout
	if cfg != nil && cfg.Gateway.SessionIdleTimeoutMinutes > 0 {
		defaultIdleTimeoutMinutes = cfg.Gateway.SessionIdleTimeoutMinutes
	}
	return NewSessionLimitCache(rdb, defaultIdleTimeoutMinutes)
}

// ProvideSchedulerCache
func ProvideSchedulerCache(rdb *redis.Client, cfg *config.Config) service.SchedulerCache {
	mgetChunkSize := defaultSchedulerSnapshotMGetChunkSize
	writeChunkSize := defaultSchedulerSnapshotWriteChunkSize
	if cfg != nil {
		if cfg.Gateway.Scheduling.SnapshotMGetChunkSize > 0 {
			mgetChunkSize = cfg.Gateway.Scheduling.SnapshotMGetChunkSize
		}
		if cfg.Gateway.Scheduling.SnapshotWriteChunkSize > 0 {
			writeChunkSize = cfg.Gateway.Scheduling.SnapshotWriteChunkSize
		}
	}
	return newSchedulerCacheWithChunkSizes(rdb, mgetChunkSize, writeChunkSize)
}

// ProviderSet is the Wire provider set for all repositories
var ProviderSet = wire.NewSet(
	NewUserRepository,
	NewAPIKeyRepository,
	NewGroupRepository,
	NewAccountRepository,
	NewScheduledTestPlanRepository,   // 定时test计划仓储
	NewScheduledTestResultRepository, // 定时test结果仓储
	NewProxyRepository,
	NewRedeemCodeRepository,
	NewPromoCodeRepository,
	NewAnnouncementRepository,
	NewAnnouncementReadRepository,
	NewUsageLogRepository,
	NewUsageBillingRepository,
	NewIdempotencyRepository,
	NewUsageCleanupRepository,
	NewDashboardAggregationRepository,
	NewSettingRepository,
	NewOpsRepository,
	NewUserSubscriptionRepository,
	NewUserAttributeDefinitionRepository,
	NewUserAttributeValueRepository,
	NewUserGroupRateRepository,
	NewErrorPassthroughRepository,
	NewTLSFingerprintProfileRepository,
	NewChannelRepository,
	NewChannelMonitorRepository,
	NewChannelMonitorRequestTemplateRepository,
	NewContentModerationRepository,
	NewAffiliateRepository,
	NewUserPlatformQuotaRepository,     // T14: user × platform quota
	NewUserPlatformQuotaServiceAdapter, // T14: adapter → service.UserPlatformQuotaRepository

	// Cache implementations
	NewGatewayCache,
	NewBillingCache,
	NewAPIKeyCache,
	NewTempUnschedCache,
	NewTimeoutCounterCache,
	NewOpenAI403CounterCache,
	NewInternal500CounterCache,
	ProvideConcurrencyCache,
	ProvideSessionLimitCache,
	NewRPMCache,
	NewUserRPMCache,
	NewUserMsgQueueCache,
	NewDashboardCache,
	NewEmailCache,
	NewIdentityCache,
	NewRedeemCache,
	NewUpdateCache,
	NewGeminiTokenCache,
	NewLeaderLockCache,
	ProvideSchedulerCache,
	NewSchedulerOutboxRepository,
	NewProxyLatencyCache,
	NewTotpCache,
	NewRefreshTokenCache,
	NewErrorPassthroughCache,
	NewTLSFingerprintProfileCache,
	NewContentModerationHashCache,

	// Encryptors
	NewAESEncryptor,

	// Backup infrastructure
	NewPgDumper,
	NewS3BackupStoreFactory,

	// HTTP service ports (DI Strategy A: return interface directly)
	NewTurnstileVerifier,
	ProvidePricingRemoteClient,
	ProvideGitHubReleaseClient,
	NewProxyExitInfoProber,
	NewClaudeUsageFetcher,
	NewClaudeOAuthClient,
	NewHTTPUpstream,
	NewOpenAIOAuthClient,
	NewGeminiOAuthClient,
	NewGeminiCliCodeAssistClient,
	NewGeminiDriveClient,

	ProvideEnt,
	ProvideSQLDB,
	ProvideRedis,
)

// ProvideEnt
//
//
// Wire
//
//
// *ent.Client
func ProvideEnt(cfg *config.Config) (*ent.Client, error) {
	client, _, err := InitEnt(cfg)
	return client, err
}

// ProvideSQLDB *sql.DB
//
//
//
//
//   - Ent
//   -
//
// *ent.Client
// *sql.DB
func ProvideSQLDB(client *ent.Client) (*sql.DB, error) {
	if client == nil {
		return nil, errors.New("nil ent client")
	}
	//
	drv, ok := client.Driver().(*entsql.Driver)
	if !ok {
		return nil, errors.New("ent driver does not expose *sql.DB")
	}
	//
	return drv.DB(), nil
}

// ProvideRedis
//
// Redis
//   -
//
//
// *redis.Client
func ProvideRedis(cfg *config.Config) *redis.Client {
	return InitRedis(cfg)
}
