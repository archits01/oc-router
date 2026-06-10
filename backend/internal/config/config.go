// Package config provides configuration loading, defaults, and validation.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/viper"
)

const (
	RunModeStandard = "standard"
	RunModeSimple   = "simple"
)

const (
	UsageRecordOverflowPolicyDrop   = "drop"
	UsageRecordOverflowPolicySample = "sample"
	UsageRecordOverflowPolicySync   = "sync"
)

// DefaultCSPPolicy is the default Content-Security-Policy with nonce support
// __CSP_NONCE__ will be replaced with actual nonce at request time by the SecurityHeaders middleware
const DefaultCSPPolicy = "default-src 'self'; script-src 'self' __CSP_NONCE__ https://challenges.cloudflare.com https://static.cloudflareinsights.com https://*.stripe.com https://static.airwallex.com https://checkout.airwallex.com https://static-demo.airwallex.com https://checkout-demo.airwallex.com; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://static.airwallex.com https://checkout.airwallex.com https://static-demo.airwallex.com https://checkout-demo.airwallex.com; img-src 'self' data: https:; font-src 'self' data: https://fonts.gstatic.com; connect-src 'self' https:; frame-src https://challenges.cloudflare.com https://*.stripe.com https://checkout.airwallex.com https://checkout-demo.airwallex.com; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// UMQ（
const (
	// UMQModeSerialize: + RPM
	UMQModeSerialize = "serialize"
	// UMQModeThrottle:
	UMQModeThrottle = "throttle"
)

//
const (
	// ConnectionPoolIsolationProxy:
	ConnectionPoolIsolationProxy = "proxy"
	// ConnectionPoolIsolationAccount:
	ConnectionPoolIsolationAccount = "account"
	// ConnectionPoolIsolationAccountProxy: +
	ConnectionPoolIsolationAccountProxy = "account_proxy"
)

// DefaultUpstreamResponseReadMaxBytes
// 128 MB %，
//
const DefaultUpstreamResponseReadMaxBytes int64 = 128 * 1024 * 1024

type Config struct {
	Server                  ServerConfig                  `mapstructure:"server"`
	Log                     LogConfig                     `mapstructure:"log"`
	CORS                    CORSConfig                    `mapstructure:"cors"`
	Security                SecurityConfig                `mapstructure:"security"`
	Billing                 BillingConfig                 `mapstructure:"billing"`
	Turnstile               TurnstileConfig               `mapstructure:"turnstile"`
	Database                DatabaseConfig                `mapstructure:"database"`
	Redis                   RedisConfig                   `mapstructure:"redis"`
	Ops                     OpsConfig                     `mapstructure:"ops"`
	JWT                     JWTConfig                     `mapstructure:"jwt"`
	Totp                    TotpConfig                    `mapstructure:"totp"`
	LinuxDo                 LinuxDoConnectConfig          `mapstructure:"linuxdo_connect"`
	WeChat                  WeChatConnectConfig           `mapstructure:"wechat_connect"`
	OIDC                    OIDCConnectConfig             `mapstructure:"oidc_connect"`
	DingTalk                DingTalkConnectConfig         `mapstructure:"dingtalk_connect"`
	GitHubOAuth             EmailOAuthProviderConfig      `mapstructure:"github_oauth"`
	GoogleOAuth             EmailOAuthProviderConfig      `mapstructure:"google_oauth"`
	Default                 DefaultConfig                 `mapstructure:"default"`
	RateLimit               RateLimitConfig               `mapstructure:"rate_limit"`
	Pricing                 PricingConfig                 `mapstructure:"pricing"`
	Gateway                 GatewayConfig                 `mapstructure:"gateway"`
	APIKeyAuth              APIKeyAuthCacheConfig         `mapstructure:"api_key_auth_cache"`
	SubscriptionCache       SubscriptionCacheConfig       `mapstructure:"subscription_cache"`
	SubscriptionMaintenance SubscriptionMaintenanceConfig `mapstructure:"subscription_maintenance"`
	Dashboard               DashboardCacheConfig          `mapstructure:"dashboard_cache"`
	DashboardAgg            DashboardAggregationConfig    `mapstructure:"dashboard_aggregation"`
	UsageCleanup            UsageCleanupConfig            `mapstructure:"usage_cleanup"`
	Concurrency             ConcurrencyConfig             `mapstructure:"concurrency"`
	TokenRefresh            TokenRefreshConfig            `mapstructure:"token_refresh"`
	RunMode                 string                        `mapstructure:"run_mode" yaml:"run_mode"`
	Timezone                string                        `mapstructure:"timezone"` // e.g. "Asia/Shanghai", "UTC"
	Gemini                  GeminiConfig                  `mapstructure:"gemini"`
	Update                  UpdateConfig                  `mapstructure:"update"`
	Idempotency             IdempotencyConfig             `mapstructure:"idempotency"`
}

type LogConfig struct {
	Level           string            `mapstructure:"level"`
	Format          string            `mapstructure:"format"`
	ServiceName     string            `mapstructure:"service_name"`
	Environment     string            `mapstructure:"env"`
	Caller          bool              `mapstructure:"caller"`
	StacktraceLevel string            `mapstructure:"stacktrace_level"`
	Output          LogOutputConfig   `mapstructure:"output"`
	Rotation        LogRotationConfig `mapstructure:"rotation"`
	Sampling        LogSamplingConfig `mapstructure:"sampling"`
}

type LogOutputConfig struct {
	ToStdout bool   `mapstructure:"to_stdout"`
	ToFile   bool   `mapstructure:"to_file"`
	FilePath string `mapstructure:"file_path"`
}

type LogRotationConfig struct {
	MaxSizeMB  int  `mapstructure:"max_size_mb"`
	MaxBackups int  `mapstructure:"max_backups"`
	MaxAgeDays int  `mapstructure:"max_age_days"`
	Compress   bool `mapstructure:"compress"`
	LocalTime  bool `mapstructure:"local_time"`
}

type LogSamplingConfig struct {
	Enabled    bool `mapstructure:"enabled"`
	Initial    int  `mapstructure:"initial"`
	Thereafter int  `mapstructure:"thereafter"`
}

type GeminiConfig struct {
	OAuth GeminiOAuthConfig `mapstructure:"oauth"`
	Quota GeminiQuotaConfig `mapstructure:"quota"`
}

type GeminiOAuthConfig struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	Scopes       string `mapstructure:"scopes"`
}

type GeminiQuotaConfig struct {
	Tiers  map[string]GeminiTierQuotaConfig `mapstructure:"tiers"`
	Policy string                           `mapstructure:"policy"`
}

type GeminiTierQuotaConfig struct {
	ProRPD          *int64 `mapstructure:"pro_rpd" json:"pro_rpd"`
	FlashRPD        *int64 `mapstructure:"flash_rpd" json:"flash_rpd"`
	CooldownMinutes *int   `mapstructure:"cooldown_minutes" json:"cooldown_minutes"`
}

type UpdateConfig struct {
	// ProxyURL
	//
	// "http://127.0.0.1:7890", "socks5://127.0.0.1:1080"
	ProxyURL string `mapstructure:"proxy_url"`
}

type IdempotencyConfig struct {
	// ObserveOnly
	ObserveOnly bool `mapstructure:"observe_only"`
	// DefaultTTLSeconds
	DefaultTTLSeconds int `mapstructure:"default_ttl_seconds"`
	// SystemOperationTTLSeconds
	SystemOperationTTLSeconds int `mapstructure:"system_operation_ttl_seconds"`
	// ProcessingTimeoutSeconds processing
	ProcessingTimeoutSeconds int `mapstructure:"processing_timeout_seconds"`
	// FailedRetryBackoffSeconds
	FailedRetryBackoffSeconds int `mapstructure:"failed_retry_backoff_seconds"`
	// MaxStoredResponseLen
	MaxStoredResponseLen int `mapstructure:"max_stored_response_len"`
	// CleanupIntervalSeconds
	CleanupIntervalSeconds int `mapstructure:"cleanup_interval_seconds"`
	// CleanupBatchSize
	CleanupBatchSize int `mapstructure:"cleanup_batch_size"`
}

type LinuxDoConnectConfig struct {
	Enabled             bool   `mapstructure:"enabled"`
	ClientID            string `mapstructure:"client_id"`
	ClientSecret        string `mapstructure:"client_secret"`
	AuthorizeURL        string `mapstructure:"authorize_url"`
	TokenURL            string `mapstructure:"token_url"`
	UserInfoURL         string `mapstructure:"userinfo_url"`
	Scopes              string `mapstructure:"scopes"`
	RedirectURL         string `mapstructure:"redirect_url"`          // backend callback URL (must be registered with the provider)
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url"` // frontend route for receiving token (default: /auth/linuxdo/callback)
	TokenAuthMethod     string `mapstructure:"token_auth_method"`     // client_secret_post / client_secret_basic / none
	UsePKCE             bool   `mapstructure:"use_pkce"`

	//
	UserInfoEmailPath    string `mapstructure:"userinfo_email_path"`
	UserInfoIDPath       string `mapstructure:"userinfo_id_path"`
	UserInfoUsernamePath string `mapstructure:"userinfo_username_path"`
}

type WeChatConnectConfig struct {
	Enabled             bool   `mapstructure:"enabled"`
	AppID               string `mapstructure:"app_id"`
	AppSecret           string `mapstructure:"app_secret"`
	OpenAppID           string `mapstructure:"open_app_id"`
	OpenAppSecret       string `mapstructure:"open_app_secret"`
	MPAppID             string `mapstructure:"mp_app_id"`
	MPAppSecret         string `mapstructure:"mp_app_secret"`
	MobileAppID         string `mapstructure:"mobile_app_id"`
	MobileAppSecret     string `mapstructure:"mobile_app_secret"`
	OpenEnabled         bool   `mapstructure:"open_enabled"`
	MPEnabled           bool   `mapstructure:"mp_enabled"`
	MobileEnabled       bool   `mapstructure:"mobile_enabled"`
	Mode                string `mapstructure:"mode"`
	Scopes              string `mapstructure:"scopes"`
	RedirectURL         string `mapstructure:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url"`
}

type OIDCConnectConfig struct {
	Enabled                 bool   `mapstructure:"enabled"`
	ProviderName            string `mapstructure:"provider_name"` // display name: "Keycloak" etc.
	ClientID                string `mapstructure:"client_id"`
	ClientSecret            string `mapstructure:"client_secret"`
	IssuerURL               string `mapstructure:"issuer_url"`
	DiscoveryURL            string `mapstructure:"discovery_url"`
	AuthorizeURL            string `mapstructure:"authorize_url"`
	TokenURL                string `mapstructure:"token_url"`
	UserInfoURL             string `mapstructure:"userinfo_url"`
	JWKSURL                 string `mapstructure:"jwks_url"`
	Scopes                  string `mapstructure:"scopes"`                // default "openid email profile"
	RedirectURL             string `mapstructure:"redirect_url"`          // backend callback URL (must be registered with the provider)
	FrontendRedirectURL     string `mapstructure:"frontend_redirect_url"` // frontend route for receiving token (default: /auth/oidc/callback)
	TokenAuthMethod         string `mapstructure:"token_auth_method"`     // client_secret_post / client_secret_basic / none
	UsePKCE                 bool   `mapstructure:"use_pkce"`
	ValidateIDToken         bool   `mapstructure:"validate_id_token"`
	UsePKCEExplicit         bool   `mapstructure:"-" yaml:"-"`
	ValidateIDTokenExplicit bool   `mapstructure:"-" yaml:"-"`
	AllowedSigningAlgs      string `mapstructure:"allowed_signing_algs"`   // default "RS256,ES256,PS256"
	ClockSkewSeconds        int    `mapstructure:"clock_skew_seconds"`     // default 120
	RequireEmailVerified    bool   `mapstructure:"require_email_verified"` // default false

	//
	UserInfoEmailPath    string `mapstructure:"userinfo_email_path"`
	UserInfoIDPath       string `mapstructure:"userinfo_id_path"`
	UserInfoUsernamePath string `mapstructure:"userinfo_username_path"`
}

type DingTalkConnectConfig struct {
	Enabled             bool   `mapstructure:"enabled"`
	ClientID            string `mapstructure:"client_id"`
	ClientSecret        string `mapstructure:"client_secret"`
	AuthorizeURL        string `mapstructure:"authorize_url"`
	TokenURL            string `mapstructure:"token_url"`
	UserInfoURL         string `mapstructure:"userinfo_url"`
	Scopes              string `mapstructure:"scopes"`
	RedirectURL         string `mapstructure:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url"`

	DingTalkAppKind string `mapstructure:"dingtalk_app_kind"` // only "internal_app" (V4 fail-closed)
	AppType         string `mapstructure:"app_type"`          // "public" (default) | "internal"

	// Corp | internal_only）
	CorpRestrictionPolicy   string `mapstructure:"corp_restriction_policy"`
	InternalCorpID          string `mapstructure:"internal_corp_id"`
	BypassRegistration      bool   `mapstructure:"bypass_registration"`
	SyncCorpEmail           bool   `mapstructure:"sync_corp_email"`
	SyncDisplayName         bool   `mapstructure:"sync_display_name"`
	SyncDept                bool   `mapstructure:"sync_dept"`
	SyncCorpEmailAttrKey    string `mapstructure:"sync_corp_email_attr_key"`
	SyncDisplayNameAttrKey  string `mapstructure:"sync_display_name_attr_key"`
	SyncDeptAttrKey         string `mapstructure:"sync_dept_attr_key"`
	SyncCorpEmailAttrName   string `mapstructure:"sync_corp_email_attr_name"`
	SyncDisplayNameAttrName string `mapstructure:"sync_display_name_attr_name"`
	SyncDeptAttrName        string `mapstructure:"sync_dept_attr_name"`

	// + Username
	RequireEmail            bool   `mapstructure:"require_email"`
	UsernameOverwritePolicy string `mapstructure:"username_overwrite_policy"`

	// Attribute（
	UsernameAttributeKey         string   `mapstructure:"username_attribute_key"`
	EnableAttributeMatching      bool     `mapstructure:"enable_attribute_matching"`
	EnableAttributeSync          bool     `mapstructure:"enable_attribute_sync"`
	AttributeSyncFields          []string `mapstructure:"attribute_sync_fields"`
	AttributeSyncOverwritePolicy string   `mapstructure:"attribute_sync_overwrite_policy"`
}

type EmailOAuthProviderConfig struct {
	Enabled             bool   `mapstructure:"enabled"`
	ClientID            string `mapstructure:"client_id"`
	ClientSecret        string `mapstructure:"client_secret"`
	AuthorizeURL        string `mapstructure:"authorize_url"`
	TokenURL            string `mapstructure:"token_url"`
	UserInfoURL         string `mapstructure:"userinfo_url"`
	EmailsURL           string `mapstructure:"emails_url"`
	Scopes              string `mapstructure:"scopes"`
	RedirectURL         string `mapstructure:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url"`
}

const (
	defaultWeChatConnectMode             = "open"
	defaultWeChatConnectScopes           = "snsapi_login"
	defaultWeChatConnectFrontendRedirect = "/auth/wechat/callback"
)

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeWeChatConnectMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "mp":
		return "mp"
	case "mobile":
		return "mobile"
	default:
		return defaultWeChatConnectMode
	}
}

func normalizeWeChatConnectStoredMode(openEnabled, mpEnabled, mobileEnabled bool, mode string) string {
	mode = normalizeWeChatConnectMode(mode)
	switch mode {
	case "open":
		if openEnabled {
			return "open"
		}
	case "mp":
		if mpEnabled {
			return "mp"
		}
	case "mobile":
		if mobileEnabled {
			return "mobile"
		}
	}
	switch {
	case openEnabled:
		return "open"
	case mpEnabled:
		return "mp"
	case mobileEnabled:
		return "mobile"
	default:
		return mode
	}
}

func defaultWeChatConnectScopesForMode(mode string) string {
	switch normalizeWeChatConnectMode(mode) {
	case "mp":
		return "snsapi_userinfo"
	case "mobile":
		return ""
	default:
		return defaultWeChatConnectScopes
	}
}

func normalizeWeChatConnectScopes(raw, mode string) string {
	switch normalizeWeChatConnectMode(mode) {
	case "mp":
		switch strings.TrimSpace(raw) {
		case "snsapi_base":
			return "snsapi_base"
		case "snsapi_userinfo":
			return "snsapi_userinfo"
		default:
			return defaultWeChatConnectScopesForMode(mode)
		}
	case "mobile":
		return ""
	default:
		return defaultWeChatConnectScopes
	}
}

func shouldApplyLegacyWeChatEnv(configKey, envKey string) bool {
	if viper.InConfig(configKey) {
		return false
	}
	_, hasNewEnv := os.LookupEnv(envKey)
	return !hasNewEnv
}

func hasExplicitConfigOrEnv(configKey, envKey string) bool {
	if viper.InConfig(configKey) {
		return true
	}
	_, ok := os.LookupEnv(envKey)
	return ok
}

func applyLegacyWeChatConnectEnvCompatibility(cfg *WeChatConnectConfig) {
	if cfg == nil {
		return
	}

	legacyOpenAppID := ""
	if shouldApplyLegacyWeChatEnv("wechat_connect.open_app_id", "WECHAT_CONNECT_OPEN_APP_ID") &&
		shouldApplyLegacyWeChatEnv("wechat_connect.app_id", "WECHAT_CONNECT_APP_ID") {
		legacyOpenAppID = strings.TrimSpace(os.Getenv("WECHAT_OAUTH_OPEN_APP_ID"))
		if legacyOpenAppID != "" {
			cfg.OpenAppID = legacyOpenAppID
		}
	}

	legacyOpenAppSecret := ""
	if shouldApplyLegacyWeChatEnv("wechat_connect.open_app_secret", "WECHAT_CONNECT_OPEN_APP_SECRET") &&
		shouldApplyLegacyWeChatEnv("wechat_connect.app_secret", "WECHAT_CONNECT_APP_SECRET") {
		legacyOpenAppSecret = strings.TrimSpace(os.Getenv("WECHAT_OAUTH_OPEN_APP_SECRET"))
		if legacyOpenAppSecret != "" {
			cfg.OpenAppSecret = legacyOpenAppSecret
		}
	}

	legacyMPAppID := ""
	if shouldApplyLegacyWeChatEnv("wechat_connect.mp_app_id", "WECHAT_CONNECT_MP_APP_ID") &&
		shouldApplyLegacyWeChatEnv("wechat_connect.app_id", "WECHAT_CONNECT_APP_ID") {
		legacyMPAppID = strings.TrimSpace(os.Getenv("WECHAT_OAUTH_MP_APP_ID"))
		if legacyMPAppID != "" {
			cfg.MPAppID = legacyMPAppID
		}
	}

	legacyMPAppSecret := ""
	if shouldApplyLegacyWeChatEnv("wechat_connect.mp_app_secret", "WECHAT_CONNECT_MP_APP_SECRET") &&
		shouldApplyLegacyWeChatEnv("wechat_connect.app_secret", "WECHAT_CONNECT_APP_SECRET") {
		legacyMPAppSecret = strings.TrimSpace(os.Getenv("WECHAT_OAUTH_MP_APP_SECRET"))
		if legacyMPAppSecret != "" {
			cfg.MPAppSecret = legacyMPAppSecret
		}
	}

	if shouldApplyLegacyWeChatEnv("wechat_connect.frontend_redirect_url", "WECHAT_CONNECT_FRONTEND_REDIRECT_URL") {
		if legacyFrontend := strings.TrimSpace(os.Getenv("WECHAT_OAUTH_FRONTEND_REDIRECT_URL")); legacyFrontend != "" {
			cfg.FrontendRedirectURL = legacyFrontend
		}
	}

	hasLegacyOpen := legacyOpenAppID != "" && legacyOpenAppSecret != ""
	hasLegacyMP := legacyMPAppID != "" && legacyMPAppSecret != ""

	if shouldApplyLegacyWeChatEnv("wechat_connect.enabled", "WECHAT_CONNECT_ENABLED") && (hasLegacyOpen || hasLegacyMP) {
		cfg.Enabled = true
	}
	if shouldApplyLegacyWeChatEnv("wechat_connect.open_enabled", "WECHAT_CONNECT_OPEN_ENABLED") && hasLegacyOpen {
		cfg.OpenEnabled = true
	}
	if shouldApplyLegacyWeChatEnv("wechat_connect.mp_enabled", "WECHAT_CONNECT_MP_ENABLED") && hasLegacyMP {
		cfg.MPEnabled = true
	}
	if shouldApplyLegacyWeChatEnv("wechat_connect.mode", "WECHAT_CONNECT_MODE") {
		switch {
		case hasLegacyMP && !hasLegacyOpen:
			cfg.Mode = "mp"
		case hasLegacyOpen:
			cfg.Mode = "open"
		}
	}
	if shouldApplyLegacyWeChatEnv("wechat_connect.scopes", "WECHAT_CONNECT_SCOPES") {
		switch {
		case hasLegacyMP && !hasLegacyOpen:
			cfg.Scopes = defaultWeChatConnectScopesForMode("mp")
		case hasLegacyOpen:
			cfg.Scopes = defaultWeChatConnectScopesForMode("open")
		}
	}
}

func normalizeWeChatConnectConfig(cfg *WeChatConnectConfig) {
	if cfg == nil {
		return
	}

	cfg.AppID = strings.TrimSpace(cfg.AppID)
	cfg.AppSecret = strings.TrimSpace(cfg.AppSecret)
	cfg.OpenAppID = strings.TrimSpace(cfg.OpenAppID)
	cfg.OpenAppSecret = strings.TrimSpace(cfg.OpenAppSecret)
	cfg.MPAppID = strings.TrimSpace(cfg.MPAppID)
	cfg.MPAppSecret = strings.TrimSpace(cfg.MPAppSecret)
	cfg.MobileAppID = strings.TrimSpace(cfg.MobileAppID)
	cfg.MobileAppSecret = strings.TrimSpace(cfg.MobileAppSecret)
	cfg.Mode = normalizeWeChatConnectMode(cfg.Mode)
	cfg.RedirectURL = strings.TrimSpace(cfg.RedirectURL)
	cfg.FrontendRedirectURL = strings.TrimSpace(cfg.FrontendRedirectURL)

	cfg.AppID = firstNonEmptyString(cfg.AppID, cfg.OpenAppID, cfg.MPAppID, cfg.MobileAppID)
	cfg.AppSecret = firstNonEmptyString(cfg.AppSecret, cfg.OpenAppSecret, cfg.MPAppSecret, cfg.MobileAppSecret)
	cfg.OpenAppID = firstNonEmptyString(cfg.OpenAppID, cfg.AppID)
	cfg.OpenAppSecret = firstNonEmptyString(cfg.OpenAppSecret, cfg.AppSecret)
	cfg.MPAppID = firstNonEmptyString(cfg.MPAppID, cfg.AppID)
	cfg.MPAppSecret = firstNonEmptyString(cfg.MPAppSecret, cfg.AppSecret)
	cfg.MobileAppID = firstNonEmptyString(cfg.MobileAppID, cfg.AppID)
	cfg.MobileAppSecret = firstNonEmptyString(cfg.MobileAppSecret, cfg.AppSecret)

	if !cfg.OpenEnabled && !cfg.MPEnabled && !cfg.MobileEnabled && cfg.Enabled {
		switch cfg.Mode {
		case "mp":
			cfg.MPEnabled = true
		case "mobile":
			cfg.MobileEnabled = true
		default:
			cfg.OpenEnabled = true
		}
	}
	cfg.Mode = normalizeWeChatConnectStoredMode(cfg.OpenEnabled, cfg.MPEnabled, cfg.MobileEnabled, cfg.Mode)
	cfg.Scopes = normalizeWeChatConnectScopes(cfg.Scopes, cfg.Mode)
	if cfg.FrontendRedirectURL == "" {
		cfg.FrontendRedirectURL = defaultWeChatConnectFrontendRedirect
	}
}

// TokenRefreshConfig OAuth token
type TokenRefreshConfig struct {
	Enabled bool `mapstructure:"enabled"`
	CheckIntervalMinutes int `mapstructure:"check_interval_minutes"`
	//
	RefreshBeforeExpiryHours float64 `mapstructure:"refresh_before_expiry_hours"`
	MaxRetries int `mapstructure:"max_retries"`
	RetryBackoffSeconds int `mapstructure:"retry_backoff_seconds"`
}

type PricingConfig struct {
	//
	RemoteURL string `mapstructure:"remote_url"`
	//
	HashURL string `mapstructure:"hash_url"`
	DataDir string `mapstructure:"data_dir"`
	FallbackFile string `mapstructure:"fallback_file"`
	UpdateIntervalHours int `mapstructure:"update_interval_hours"`
	HashCheckIntervalMinutes int `mapstructure:"hash_check_interval_minutes"`
}

type ServerConfig struct {
	Host               string    `mapstructure:"host"`
	Port               int       `mapstructure:"port"`
	Mode               string    `mapstructure:"mode"`                  // debug/release
	FrontendURL        string    `mapstructure:"frontend_url"`          // frontend base URL for generating external links in emails
	ReadHeaderTimeout  int       `mapstructure:"read_header_timeout"`   // read header timeout (seconds)
	IdleTimeout        int       `mapstructure:"idle_timeout"`          // idle connection timeout (seconds)
	TrustedProxies     []string  `mapstructure:"trusted_proxies"`       // trusted proxy list (CIDR/IP)
	MaxRequestBodySize int64     `mapstructure:"max_request_body_size"` // global max request body size
	H2C                H2CConfig `mapstructure:"h2c"`                   // HTTP/2 Cleartext config
}

// H2CConfig HTTP/2 Cleartext
type H2CConfig struct {
	Enabled                      bool   `mapstructure:"enabled"`                          // whether to enable H2C
	MaxConcurrentStreams         uint32 `mapstructure:"max_concurrent_streams"`           // max concurrent streams
	IdleTimeout                  int    `mapstructure:"idle_timeout"`                     // idle timeout (seconds)
	MaxReadFrameSize             int    `mapstructure:"max_read_frame_size"`              // max frame size (bytes)
	MaxUploadBufferPerConnection int    `mapstructure:"max_upload_buffer_per_connection"` // upload buffer per connection (bytes)
	MaxUploadBufferPerStream     int    `mapstructure:"max_upload_buffer_per_stream"`     // upload buffer per stream (bytes)
}

type CORSConfig struct {
	AllowedOrigins   []string `mapstructure:"allowed_origins"`
	AllowCredentials bool     `mapstructure:"allow_credentials"`
}

type SecurityConfig struct {
	URLAllowlist                     URLAllowlistConfig   `mapstructure:"url_allowlist"`
	ResponseHeaders                  ResponseHeaderConfig `mapstructure:"response_headers"`
	CSP                              CSPConfig            `mapstructure:"csp"`
	ProxyFallback                    ProxyFallbackConfig  `mapstructure:"proxy_fallback"`
	ProxyProbe                       ProxyProbeConfig     `mapstructure:"proxy_probe"`
	TrustForwardedIPForAPIKeyACL     bool                 `mapstructure:"trust_forwarded_ip_for_api_key_acl"`
	trustForwardedIPForAPIKeyACLLive *atomic.Bool         `mapstructure:"-"`
}

func (c *Config) TrustForwardedIPForAPIKeyACL() bool {
	if c == nil {
		return false
	}
	live := c.Security.trustForwardedIPForAPIKeyACLLive
	if live == nil {
		return c.Security.TrustForwardedIPForAPIKeyACL
	}
	return live.Load()
}

func (c *Config) SetTrustForwardedIPForAPIKeyACL(enabled bool) {
	if c == nil {
		return
	}
	c.Security.TrustForwardedIPForAPIKeyACL = enabled
	if c.Security.trustForwardedIPForAPIKeyACLLive == nil {
		c.Security.trustForwardedIPForAPIKeyACLLive = &atomic.Bool{}
	}
	c.Security.trustForwardedIPForAPIKeyACLLive.Store(enabled)
}

type URLAllowlistConfig struct {
	Enabled           bool     `mapstructure:"enabled"`
	UpstreamHosts     []string `mapstructure:"upstream_hosts"`
	PricingHosts      []string `mapstructure:"pricing_hosts"`
	CRSHosts          []string `mapstructure:"crs_hosts"`
	AllowPrivateHosts bool     `mapstructure:"allow_private_hosts"`
	//
	AllowInsecureHTTP bool `mapstructure:"allow_insecure_http"`
}

type ResponseHeaderConfig struct {
	Enabled           bool     `mapstructure:"enabled"`
	AdditionalAllowed []string `mapstructure:"additional_allowed"`
	ForceRemove       []string `mapstructure:"force_remove"`
}

type CSPConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Policy  string `mapstructure:"policy"`
}

type ProxyFallbackConfig struct {
	// AllowDirectOnError
	//   - GitHub Release
	//
	//
	AllowDirectOnError bool `mapstructure:"allow_direct_on_error"`
}

type ProxyProbeConfig struct {
	InsecureSkipVerify bool `mapstructure:"insecure_skip_verify"` // disabled: TLS certificate verification skip not allowed
}

type BillingConfig struct {
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
	// UserPlatformQuotaCacheTTLSeconds × =1
	//   - billing_cache_service.cacheWriteWorker
	//   - billing_cache_service.checkUserPlatformQuotaEligibility
	//
	UserPlatformQuotaCacheTTLSeconds int `mapstructure:"user_platform_quota_cache_ttl_seconds"`
	// UserPlatformQuotaSentinelTTLSeconds sentinel()entry
	// =1h。
	UserPlatformQuotaSentinelTTLSeconds int `mapstructure:"user_platform_quota_sentinel_ttl_seconds"`
}

type CircuitBreakerConfig struct {
	Enabled             bool `mapstructure:"enabled"`
	FailureThreshold    int  `mapstructure:"failure_threshold"`
	ResetTimeoutSeconds int  `mapstructure:"reset_timeout_seconds"`
	HalfOpenRequests    int  `mapstructure:"half_open_requests"`
}

type ConcurrencyConfig struct {
	// PingInterval:
	PingInterval int `mapstructure:"ping_interval"`
}

type ImageConcurrencyConfig struct {
	// Enabled:
	Enabled bool `mapstructure:"enabled"`
	// MaxConcurrentRequests:
	MaxConcurrentRequests int `mapstructure:"max_concurrent_requests"`
	// OverflowMode:
	OverflowMode string `mapstructure:"overflow_mode"`
	// WaitTimeoutSeconds: overflow_mode=wait
	WaitTimeoutSeconds int `mapstructure:"wait_timeout_seconds"`
	// MaxWaitingRequests: overflow_mode=wait
	MaxWaitingRequests int `mapstructure:"max_waiting_requests"`
}

const (
	ImageConcurrencyOverflowModeReject = "reject"
	ImageConcurrencyOverflowModeWait   = "wait"
)

// GatewayConfig API
type GatewayConfig struct {
	ResponseHeaderTimeout int `mapstructure:"response_header_timeout"`
	// OpenAIResponseHeaderTimeout: OpenAI/Codex
	// OpenAI/Codex
	OpenAIResponseHeaderTimeout int `mapstructure:"openai_response_header_timeout"`
	MaxBodySize int64 `mapstructure:"max_body_size"`
	UpstreamResponseReadMaxBytes int64 `mapstructure:"upstream_response_read_max_bytes"`
	ProxyProbeResponseReadMaxBytes int64 `mapstructure:"proxy_probe_response_read_max_bytes"`
	// Gemini
	GeminiDebugResponseHeaders bool `mapstructure:"gemini_debug_response_headers"`
	// ConnectionPoolIsolation:
	ConnectionPoolIsolation string `mapstructure:"connection_pool_isolation"`
	// ForceCodexCLI: `/v1/responses`
	//
	ForceCodexCLI bool `mapstructure:"force_codex_cli"`
	// CodexImageGenerationBridgeEnabled: `/v1/responses`
	//
	CodexImageGenerationBridgeEnabled bool `mapstructure:"codex_image_generation_bridge_enabled"`
	// ForcedCodexInstructionsTemplateFile:
	// {{ .ExistingInstructions }}。
	ForcedCodexInstructionsTemplateFile string `mapstructure:"forced_codex_instructions_template_file"`
	// ForcedCodexInstructionsTemplate:
	ForcedCodexInstructionsTemplate string `mapstructure:"-"`
	// OpenAIPassthroughAllowTimeoutHeaders: OpenAI
	//
	OpenAIPassthroughAllowTimeoutHeaders bool `mapstructure:"openai_passthrough_allow_timeout_headers"`
	// OpenAIWS: OpenAI Responses WebSocket
	OpenAIWS GatewayOpenAIWSConfig `mapstructure:"openai_ws"`
	// OpenAIScheduler: OpenAI
	OpenAIScheduler GatewayOpenAISchedulerConfig `mapstructure:"openai_scheduler"`
	// OpenAIHTTP2: OpenAI HTTP
	OpenAIHTTP2 GatewayOpenAIHTTP2Config `mapstructure:"openai_http2"`
	// ImageConcurrency:
	ImageConcurrency ImageConcurrencyConfig `mapstructure:"image_concurrency"`

	// HTTP
	// MaxIdleConns:
	MaxIdleConns int `mapstructure:"max_idle_conns"`
	// MaxIdleConnsPerHost:
	MaxIdleConnsPerHost int `mapstructure:"max_idle_conns_per_host"`
	// MaxConnsPerHost: +
	MaxConnsPerHost int `mapstructure:"max_conns_per_host"`
	// IdleConnTimeoutSeconds:
	IdleConnTimeoutSeconds int `mapstructure:"idle_conn_timeout_seconds"`
	// MaxUpstreamClients:
	//
	// * 1.2（
	MaxUpstreamClients int `mapstructure:"max_upstream_clients"`
	// ClientIdleTTLSeconds:
	//
	ClientIdleTTLSeconds int `mapstructure:"client_idle_ttl_seconds"`
	// ConcurrencySlotTTLMinutes:
	//
	ConcurrencySlotTTLMinutes int `mapstructure:"concurrency_slot_ttl_minutes"`
	// SessionIdleTimeoutMinutes:
	//
	SessionIdleTimeoutMinutes int `mapstructure:"session_idle_timeout_minutes"`

	// StreamDataIntervalTimeout:
	StreamDataIntervalTimeout int `mapstructure:"stream_data_interval_timeout"`
	// StreamKeepaliveInterval:
	StreamKeepaliveInterval int `mapstructure:"stream_keepalive_interval"`
	// ImageStreamDataIntervalTimeout:
	ImageStreamDataIntervalTimeout int `mapstructure:"image_stream_data_interval_timeout"`
	// ImageStreamKeepaliveInterval:
	ImageStreamKeepaliveInterval int `mapstructure:"image_stream_keepalive_interval"`
	// MaxLineSize:
	MaxLineSize int `mapstructure:"max_line_size"`

	LogUpstreamErrorBody bool `mapstructure:"log_upstream_error_body"`
	LogUpstreamErrorBodyMaxBytes int `mapstructure:"log_upstream_error_body_max_bytes"`

	// API-key
	InjectBetaForAPIKey bool `mapstructure:"inject_beta_for_apikey"`

	//
	FailoverOn400 bool `mapstructure:"failover_on_400"`

	MaxAccountSwitches int `mapstructure:"max_account_switches"`
	// Gemini
	MaxAccountSwitchesGemini int `mapstructure:"max_account_switches_gemini"`

	// Antigravity 429 fallback
	AntigravityFallbackCooldownMinutes int `mapstructure:"antigravity_fallback_cooldown_minutes"`

	// Scheduling:
	Scheduling GatewaySchedulingConfig `mapstructure:"scheduling"`

	// TLSFingerprint: TLS
	TLSFingerprint TLSFingerprintConfig `mapstructure:"tls_fingerprint"`

	// UsageRecord: +
	UsageRecord GatewayUsageRecordConfig `mapstructure:"usage_record"`

	// UserGroupRateCacheTTLSeconds:
	UserGroupRateCacheTTLSeconds int `mapstructure:"user_group_rate_cache_ttl_seconds"`
	// ModelsListCacheTTLSeconds: /v1/models
	ModelsListCacheTTLSeconds int `mapstructure:"models_list_cache_ttl_seconds"`

	// UserMessageQueue:
	// "user" + RPM
	UserMessageQueue UserMessageQueueConfig `mapstructure:"user_message_queue"`
}

// GatewayOpenAIHTTP2Config OpenAI HTTP
//
type GatewayOpenAIHTTP2Config struct {
	// Enabled:
	Enabled bool `mapstructure:"enabled"`
	// AllowProxyFallbackToHTTP1: HTTP/HTTPS
	AllowProxyFallbackToHTTP1 bool `mapstructure:"allow_proxy_fallback_to_http1"`
	// FallbackErrorThreshold:
	FallbackErrorThreshold int `mapstructure:"fallback_error_threshold"`
	// FallbackWindowSeconds:
	FallbackWindowSeconds int `mapstructure:"fallback_window_seconds"`
	// FallbackTTLSeconds:
	FallbackTTLSeconds int `mapstructure:"fallback_ttl_seconds"`
}

// UserMessageQueueConfig
//
type UserMessageQueueConfig struct {
	// Mode:
	// "serialize" = + RPM
	// "throttle" =
	// "" =
	Mode string `mapstructure:"mode"`
	// Enabled: "serialize"）
	Enabled bool `mapstructure:"enabled"`
	// LockTTLMs:
	LockTTLMs int `mapstructure:"lock_ttl_ms"`
	// WaitTimeoutMs:
	WaitTimeoutMs int `mapstructure:"wait_timeout_ms"`
	// MinDelayMs: RPM
	MinDelayMs int `mapstructure:"min_delay_ms"`
	// MaxDelayMs: RPM
	MaxDelayMs int `mapstructure:"max_delay_ms"`
	// CleanupIntervalSeconds:
	CleanupIntervalSeconds int `mapstructure:"cleanup_interval_seconds"`
}

// WaitTimeout
func (c *UserMessageQueueConfig) WaitTimeout() time.Duration {
	if c.WaitTimeoutMs <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.WaitTimeoutMs) * time.Millisecond
}

// GetEffectiveMode
// ()
func (c *UserMessageQueueConfig) GetEffectiveMode() string {
	if c.Mode == UMQModeSerialize || c.Mode == UMQModeThrottle {
		return c.Mode
	}
	if c.Enabled {
		return UMQModeSerialize // backward compatible
	}
	return ""
}

// GatewayOpenAIWSConfig OpenAI Responses WebSocket
//
type GatewayOpenAIWSConfig struct {
	// ModeRouterV2Enabled:
	ModeRouterV2Enabled bool `mapstructure:"mode_router_v2_enabled"`
	// IngressModeDefault: ingress
	IngressModeDefault string `mapstructure:"ingress_mode_default"`
	// Enabled:
	Enabled bool `mapstructure:"enabled"`
	// OAuthEnabled:
	OAuthEnabled bool `mapstructure:"oauth_enabled"`
	// APIKeyEnabled:
	APIKeyEnabled bool `mapstructure:"apikey_enabled"`
	// ForceHTTP:
	ForceHTTP bool `mapstructure:"force_http"`
	// AllowStoreRecovery: =true（
	AllowStoreRecovery bool `mapstructure:"allow_store_recovery"`
	// IngressPreviousResponseRecoveryEnabled: ingress
	IngressPreviousResponseRecoveryEnabled bool `mapstructure:"ingress_previous_response_recovery_enabled"`
	// StoreDisabledConnMode: store=false
	// - strict:
	// - adaptive:
	// - off:
	StoreDisabledConnMode string `mapstructure:"store_disabled_conn_mode"`
	// StoreDisabledForceNewConn: store=false
	//
	StoreDisabledForceNewConn bool `mapstructure:"store_disabled_force_new_conn"`
	// PrewarmGenerateEnabled: =false
	PrewarmGenerateEnabled bool `mapstructure:"prewarm_generate_enabled"`
	// ClientReadLimitBytes:
	ClientReadLimitBytes int64 `mapstructure:"client_read_limit_bytes"`
	// HTTPBridgeEnabled:
	HTTPBridgeEnabled bool `mapstructure:"http_bridge_enabled"`
	// HTTPBridgeThresholdBytes:
	HTTPBridgeThresholdBytes int64 `mapstructure:"http_bridge_threshold_bytes"`

	// Feature
	ResponsesWebsockets   bool `mapstructure:"responses_websockets"`
	ResponsesWebsocketsV2 bool `mapstructure:"responses_websockets_v2"`

	MaxConnsPerAccount int `mapstructure:"max_conns_per_account"`
	MinIdlePerAccount  int `mapstructure:"min_idle_per_account"`
	MaxIdlePerAccount  int `mapstructure:"max_idle_per_account"`
	// DynamicMaxConnsByAccountConcurrencyEnabled:
	DynamicMaxConnsByAccountConcurrencyEnabled bool `mapstructure:"dynamic_max_conns_by_account_concurrency_enabled"`
	// OAuthMaxConnsFactor: OAuth =ceil(concurrency*factor)）
	OAuthMaxConnsFactor float64 `mapstructure:"oauth_max_conns_factor"`
	// APIKeyMaxConnsFactor: API Key =ceil(concurrency*factor)）
	APIKeyMaxConnsFactor  float64 `mapstructure:"apikey_max_conns_factor"`
	DialTimeoutSeconds    int     `mapstructure:"dial_timeout_seconds"`
	ReadTimeoutSeconds    int     `mapstructure:"read_timeout_seconds"`
	WriteTimeoutSeconds   int     `mapstructure:"write_timeout_seconds"`
	PoolTargetUtilization float64 `mapstructure:"pool_target_utilization"`
	QueueLimitPerConn     int     `mapstructure:"queue_limit_per_conn"`
	// EventFlushBatchSize: WS
	EventFlushBatchSize int `mapstructure:"event_flush_batch_size"`
	// EventFlushIntervalMS: WS
	EventFlushIntervalMS int `mapstructure:"event_flush_interval_ms"`
	// PrewarmCooldownMS:
	PrewarmCooldownMS int `mapstructure:"prewarm_cooldown_ms"`
	// FallbackCooldownSeconds: WS
	FallbackCooldownSeconds int `mapstructure:"fallback_cooldown_seconds"`
	// RetryBackoffInitialMS: WS <=0
	RetryBackoffInitialMS int `mapstructure:"retry_backoff_initial_ms"`
	// RetryBackoffMaxMS: WS
	RetryBackoffMaxMS int `mapstructure:"retry_backoff_max_ms"`
	// RetryJitterRatio: WS
	RetryJitterRatio float64 `mapstructure:"retry_jitter_ratio"`
	// RetryTotalBudgetMS: WS
	RetryTotalBudgetMS int `mapstructure:"retry_total_budget_ms"`
	// PayloadLogSampleRate: payload_schema
	PayloadLogSampleRate float64 `mapstructure:"payload_log_sample_rate"`

	LBTopK int `mapstructure:"lb_top_k"`
	// StickySessionTTLSeconds: session_hash -> account_id
	StickySessionTTLSeconds int `mapstructure:"sticky_session_ttl_seconds"`
	// SessionHashReadOldFallback: “”
	SessionHashReadOldFallback bool `mapstructure:"session_hash_read_old_fallback"`
	// SessionHashDualWriteOld:
	SessionHashDualWriteOld bool `mapstructure:"session_hash_dual_write_old"`
	// MetadataBridgeEnabled: RequestMetadata *
	MetadataBridgeEnabled bool `mapstructure:"metadata_bridge_enabled"`
	// StickyResponseIDTTLSeconds: response_id -> account_id
	StickyResponseIDTTLSeconds int `mapstructure:"sticky_response_id_ttl_seconds"`
	// StickyPreviousResponseTTLSeconds:
	StickyPreviousResponseTTLSeconds int `mapstructure:"sticky_previous_response_ttl_seconds"`

	SchedulerScoreWeights GatewayOpenAIWSSchedulerScoreWeights `mapstructure:"scheduler_score_weights"`
}

// GatewayOpenAIWSSchedulerScoreWeights
type GatewayOpenAIWSSchedulerScoreWeights struct {
	Priority  float64 `mapstructure:"priority"`
	Load      float64 `mapstructure:"load"`
	Queue     float64 `mapstructure:"queue"`
	ErrorRate float64 `mapstructure:"error_rate"`
	TTFT      float64 `mapstructure:"ttft"`
}

// GatewayOpenAISchedulerConfig OpenAI
type GatewayOpenAISchedulerConfig struct {
	// StickyEscapeEnabled:
	StickyEscapeEnabled bool `mapstructure:"sticky_escape_enabled"`
	// StickyEscapeTTFTMs: TTFT EWMA
	StickyEscapeTTFTMs int `mapstructure:"sticky_escape_ttft_ms"`
	// StickyEscapeErrorRate:
	StickyEscapeErrorRate float64 `mapstructure:"sticky_escape_error_rate"`
}

// GatewayUsageRecordConfig
type GatewayUsageRecordConfig struct {
	// WorkerCount: worker
	WorkerCount int `mapstructure:"worker_count"`
	// QueueSize:
	QueueSize int `mapstructure:"queue_size"`
	// TaskTimeoutSeconds:
	TaskTimeoutSeconds int `mapstructure:"task_timeout_seconds"`
	// OverflowPolicy:
	OverflowPolicy string `mapstructure:"overflow_policy"`
	// OverflowSamplePercent: sample
	OverflowSamplePercent int `mapstructure:"overflow_sample_percent"`

	// AutoScaleEnabled:
	AutoScaleEnabled bool `mapstructure:"auto_scale_enabled"`
	// AutoScaleMinWorkers:
	AutoScaleMinWorkers int `mapstructure:"auto_scale_min_workers"`
	// AutoScaleMaxWorkers:
	AutoScaleMaxWorkers int `mapstructure:"auto_scale_max_workers"`
	// AutoScaleUpQueuePercent:
	AutoScaleUpQueuePercent int `mapstructure:"auto_scale_up_queue_percent"`
	// AutoScaleDownQueuePercent:
	AutoScaleDownQueuePercent int `mapstructure:"auto_scale_down_queue_percent"`
	// AutoScaleUpStep:
	AutoScaleUpStep int `mapstructure:"auto_scale_up_step"`
	// AutoScaleDownStep:
	AutoScaleDownStep int `mapstructure:"auto_scale_down_step"`
	// AutoScaleCheckIntervalSeconds:
	AutoScaleCheckIntervalSeconds int `mapstructure:"auto_scale_check_interval_seconds"`
	// AutoScaleCooldownSeconds:
	AutoScaleCooldownSeconds int `mapstructure:"auto_scale_cooldown_seconds"`
}

// TLSFingerprintConfig TLS
// (Node.js)
type TLSFingerprintConfig struct {
	// Enabled:
	Enabled bool `mapstructure:"enabled"`
	// Profiles:
	// key "claude_cli_v2", "chrome_120"
	Profiles map[string]TLSProfileConfig `mapstructure:"profiles"`
}

// TLSProfileConfig
//
// (tests/tls-fingerprint-web)
type TLSProfileConfig struct {
	// Name:
	Name string `mapstructure:"name"`
	// EnableGREASE:
	EnableGREASE bool `mapstructure:"enable_grease"`
	// CipherSuites: TLS
	CipherSuites []uint16 `mapstructure:"cipher_suites"`
	// Curves:
	Curves []uint16 `mapstructure:"curves"`
	// PointFormats:
	PointFormats []uint16 `mapstructure:"point_formats"`
	// SignatureAlgorithms:
	SignatureAlgorithms []uint16 `mapstructure:"signature_algorithms"`
	// ALPNProtocols: ALPN["h2", "http/1.1"]）
	ALPNProtocols []string `mapstructure:"alpn_protocols"`
	// SupportedVersions: [0x0304, 0x0303]
	SupportedVersions []uint16 `mapstructure:"supported_versions"`
	// KeyShareGroups: Key Share[29]
	KeyShareGroups []uint16 `mapstructure:"key_share_groups"`
	// PSKModes: PSK[1]
	PSKModes []uint16 `mapstructure:"psk_modes"`
	// Extensions: TLS
	// [0,11,10,35,16,22,23,13,43,45,51]
	// GREASE()
	Extensions []uint16 `mapstructure:"extensions"`
}

// GatewaySchedulingConfig accounts scheduling configuration.
type GatewaySchedulingConfig struct {
	StickySessionMaxWaiting  int           `mapstructure:"sticky_session_max_waiting"`
	StickySessionWaitTimeout time.Duration `mapstructure:"sticky_session_wait_timeout"`

	FallbackWaitTimeout time.Duration `mapstructure:"fallback_wait_timeout"`
	FallbackMaxWaiting  int           `mapstructure:"fallback_max_waiting"`

	// "last_used"() "random"()
	FallbackSelectionMode string `mapstructure:"fallback_selection_mode"`

	LoadBatchEnabled    bool `mapstructure:"load_batch_enabled"`
	LoadBatchCacheTTLMS int  `mapstructure:"load_batch_cache_ttl_ms"`
	//
	SnapshotMGetChunkSize int `mapstructure:"snapshot_mget_chunk_size"`
	SnapshotWriteChunkSize int `mapstructure:"snapshot_write_chunk_size"`

	SlotCleanupInterval time.Duration `mapstructure:"slot_cleanup_interval"`

	DbFallbackEnabled bool `mapstructure:"db_fallback_enabled"`
	DbFallbackTimeoutSeconds int `mapstructure:"db_fallback_timeout_seconds"`
	//
	DbFallbackMaxQPS int `mapstructure:"db_fallback_max_qps"`

	// Outbox
	// Outbox
	OutboxPollIntervalSeconds int `mapstructure:"outbox_poll_interval_seconds"`
	// Outbox
	OutboxLagWarnSeconds int `mapstructure:"outbox_lag_warn_seconds"`
	// Outbox
	OutboxLagRebuildSeconds int `mapstructure:"outbox_lag_rebuild_seconds"`
	// Outbox
	OutboxLagRebuildFailures int `mapstructure:"outbox_lag_rebuild_failures"`
	// Outbox
	OutboxBacklogRebuildRows int `mapstructure:"outbox_backlog_rebuild_rows"`

	FullRebuildIntervalSeconds int `mapstructure:"full_rebuild_interval_seconds"`
}

func (s *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// DatabaseConfig
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	SSLMode  string `mapstructure:"sslmode"`
	// MaxOpenConns:
	MaxOpenConns int `mapstructure:"max_open_conns"`
	// MaxIdleConns:
	MaxIdleConns int `mapstructure:"max_idle_conns"`
	// ConnMaxLifetimeMinutes:
	ConnMaxLifetimeMinutes int `mapstructure:"conn_max_lifetime_minutes"`
	// ConnMaxIdleTimeMinutes:
	ConnMaxIdleTimeMinutes int `mapstructure:"conn_max_idle_time_minutes"`
	// UserPlatformQuotaFlusherEnabled: ×platform
	UserPlatformQuotaFlusherEnabled bool `mapstructure:"user_platform_quota_flusher_enabled"`
	// UserPlatformQuotaFlushIntervalMs: flusher
	UserPlatformQuotaFlushIntervalMs int `mapstructure:"user_platform_quota_flush_interval_ms"`
	// UserPlatformQuotaFlushBatchSize: flusher
	// ≤ 6000（
	UserPlatformQuotaFlushBatchSize int `mapstructure:"user_platform_quota_flush_batch_size"`
}

func (d *DatabaseConfig) DSN() string {
	//
	if d.Password == "" {
		return fmt.Sprintf(
			"host=%s port=%d user=%s dbname=%s sslmode=%s",
			d.Host, d.Port, d.User, d.DBName, d.SSLMode,
		)
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode,
	)
}

// DSNWithTimezone returns DSN with timezone setting
func (d *DatabaseConfig) DSNWithTimezone(tz string) string {
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	//
	if d.Password == "" {
		return fmt.Sprintf(
			"host=%s port=%d user=%s dbname=%s sslmode=%s TimeZone=%s",
			d.Host, d.Port, d.User, d.DBName, d.SSLMode, tz,
		)
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode, tz,
	)
}

// RedisConfig Redis
type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	// DialTimeoutSeconds:
	DialTimeoutSeconds int `mapstructure:"dial_timeout_seconds"`
	// ReadTimeoutSeconds:
	ReadTimeoutSeconds int `mapstructure:"read_timeout_seconds"`
	// WriteTimeoutSeconds:
	WriteTimeoutSeconds int `mapstructure:"write_timeout_seconds"`
	// PoolSize:
	PoolSize int `mapstructure:"pool_size"`
	// MinIdleConns:
	MinIdleConns int `mapstructure:"min_idle_conns"`
	// EnableTLS:
	EnableTLS bool `mapstructure:"enable_tls"`
}

func (r *RedisConfig) Address() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

type OpsConfig struct {
	// Enabled controls whether ops features should run.
	//
	// NOTE: vNext still has a DB-backed feature flag (ops_monitoring_enabled) for runtime on/off.
	// This config flag is the "hard switch" for deployments that want to disable ops completely.
	Enabled bool `mapstructure:"enabled"`

	// UsePreaggregatedTables prefers ops_metrics_hourly/daily for long-window dashboard queries.
	UsePreaggregatedTables bool `mapstructure:"use_preaggregated_tables"`

	// Cleanup controls periodic deletion of old ops data to prevent unbounded growth.
	Cleanup OpsCleanupConfig `mapstructure:"cleanup"`

	// MetricsCollectorCache controls Redis caching for expensive per-window collector queries.
	MetricsCollectorCache OpsMetricsCollectorCacheConfig `mapstructure:"metrics_collector_cache"`

	// Pre-aggregation configuration.
	Aggregation OpsAggregationConfig `mapstructure:"aggregation"`
}

type OpsCleanupConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Schedule string `mapstructure:"schedule"`

	// Retention days (0 disables that cleanup target).
	//
	// vNext requirement: default 30 days across ops datasets.
	ErrorLogRetentionDays      int `mapstructure:"error_log_retention_days"`
	MinuteMetricsRetentionDays int `mapstructure:"minute_metrics_retention_days"`
	HourlyMetricsRetentionDays int `mapstructure:"hourly_metrics_retention_days"`
}

type OpsAggregationConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type OpsMetricsCollectorCacheConfig struct {
	Enabled bool          `mapstructure:"enabled"`
	TTL     time.Duration `mapstructure:"ttl"`
}

type JWTConfig struct {
	Secret     string `mapstructure:"secret"`
	ExpireHour int    `mapstructure:"expire_hour"`
	// AccessTokenExpireMinutes: Access Token
	// - >0:
	// - =0:
	AccessTokenExpireMinutes int `mapstructure:"access_token_expire_minutes"`
	// RefreshTokenExpireDays: Refresh Token
	RefreshTokenExpireDays int `mapstructure:"refresh_token_expire_days"`
	// RefreshWindowMinutes:
	RefreshWindowMinutes int `mapstructure:"refresh_window_minutes"`
}

// TotpConfig TOTP
type TotpConfig struct {
	// EncryptionKey
	EncryptionKey string `mapstructure:"encryption_key"`
	// EncryptionKeyConfigured
	//
	EncryptionKeyConfigured bool `mapstructure:"-"`
}

type TurnstileConfig struct {
	Required bool `mapstructure:"required"`
}

type DefaultConfig struct {
	AdminEmail      string  `mapstructure:"admin_email"`
	AdminPassword   string  `mapstructure:"admin_password"`
	UserConcurrency int     `mapstructure:"user_concurrency"`
	UserBalance     float64 `mapstructure:"user_balance"`
	APIKeyPrefix    string  `mapstructure:"api_key_prefix"`
	RateMultiplier  float64 `mapstructure:"rate_multiplier"`
}

type RateLimitConfig struct {
	OverloadCooldownMinutes int `mapstructure:"overload_cooldown_minutes"`  // 529 overload cooldown time (minutes)
	OAuth401CooldownMinutes int `mapstructure:"oauth_401_cooldown_minutes"` // OAuth 401 temporary unschedulable cooldown (minutes)
}

// APIKeyAuthCacheConfig API Key
type APIKeyAuthCacheConfig struct {
	L1Size             int  `mapstructure:"l1_size"`
	L1TTLSeconds       int  `mapstructure:"l1_ttl_seconds"`
	L2TTLSeconds       int  `mapstructure:"l2_ttl_seconds"`
	NegativeTTLSeconds int  `mapstructure:"negative_ttl_seconds"`
	JitterPercent      int  `mapstructure:"jitter_percent"`
	Singleflight       bool `mapstructure:"singleflight"`
}

// SubscriptionCacheConfig
type SubscriptionCacheConfig struct {
	L1Size        int `mapstructure:"l1_size"`
	L1TTLSeconds  int `mapstructure:"l1_ttl_seconds"`
	JitterPercent int `mapstructure:"jitter_percent"`
}

// SubscriptionMaintenanceConfig
// “”
type SubscriptionMaintenanceConfig struct {
	WorkerCount int `mapstructure:"worker_count"`
	QueueSize   int `mapstructure:"queue_size"`
}

// DashboardCacheConfig
type DashboardCacheConfig struct {
	// Enabled:
	Enabled bool `mapstructure:"enabled"`
	// KeyPrefix: Redis key
	KeyPrefix string `mapstructure:"key_prefix"`
	// StatsFreshTTLSeconds: “”
	StatsFreshTTLSeconds int `mapstructure:"stats_fresh_ttl_seconds"`
	// StatsTTLSeconds: Redis
	StatsTTLSeconds int `mapstructure:"stats_ttl_seconds"`
	// StatsRefreshTimeoutSeconds:
	StatsRefreshTimeoutSeconds int `mapstructure:"stats_refresh_timeout_seconds"`
}

// DashboardAggregationConfig
type DashboardAggregationConfig struct {
	// Enabled:
	Enabled bool `mapstructure:"enabled"`
	// IntervalSeconds:
	IntervalSeconds int `mapstructure:"interval_seconds"`
	// LookbackSeconds:
	LookbackSeconds int `mapstructure:"lookback_seconds"`
	// BackfillEnabled:
	BackfillEnabled bool `mapstructure:"backfill_enabled"`
	// BackfillMaxDays:
	BackfillMaxDays int `mapstructure:"backfill_max_days"`
	// Retention:
	Retention DashboardAggregationRetentionConfig `mapstructure:"retention"`
	// RecomputeDays:
	RecomputeDays int `mapstructure:"recompute_days"`
}

// DashboardAggregationRetentionConfig
type DashboardAggregationRetentionConfig struct {
	UsageLogsDays         int `mapstructure:"usage_logs_days"`
	UsageBillingDedupDays int `mapstructure:"usage_billing_dedup_days"`
	HourlyDays            int `mapstructure:"hourly_days"`
	DailyDays             int `mapstructure:"daily_days"`
}

// UsageCleanupConfig
type UsageCleanupConfig struct {
	// Enabled:
	Enabled bool `mapstructure:"enabled"`
	// MaxRangeDays:
	MaxRangeDays int `mapstructure:"max_range_days"`
	// BatchSize:
	BatchSize int `mapstructure:"batch_size"`
	// WorkerIntervalSeconds:
	WorkerIntervalSeconds int `mapstructure:"worker_interval_seconds"`
	// TaskTimeoutSeconds:
	TaskTimeoutSeconds int `mapstructure:"task_timeout_seconds"`
}

func NormalizeRunMode(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case RunModeStandard, RunModeSimple:
		return normalized
	default:
		return RunModeStandard
	}
}

// Load
func Load() (*Config, error) {
	return load(false)
}

// LoadForBootstrap
//
//
func LoadForBootstrap() (*Config, error) {
	return load(true)
}

func load(allowMissingJWTSecret bool) (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	// Add config paths in priority order
	// 1. DATA_DIR environment variable (highest priority)
	if dataDir := os.Getenv("DATA_DIR"); dataDir != "" {
		viper.AddConfigPath(dataDir)
	}
	// 2. Docker data directory
	viper.AddConfigPath("/app/data")
	// 3. Current directory
	viper.AddConfigPath(".")
	// 4. Config subdirectory
	viper.AddConfigPath("./config")
	// 5. System config directory
	viper.AddConfigPath("/etc/sub2api")

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	setDefaults()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config error: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config error: %w", err)
	}
	if cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs == 0 {
		cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 15000
	}
	if cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate == 0 {
		cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate = 0.5
	}
	if !cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled && !viper.IsSet("gateway.openai_scheduler.sticky_escape_enabled") {
		cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
	}

	cfg.RunMode = NormalizeRunMode(cfg.RunMode)
	cfg.Server.Mode = strings.ToLower(strings.TrimSpace(cfg.Server.Mode))
	if cfg.Server.Mode == "" {
		cfg.Server.Mode = "debug"
	}
	cfg.Server.FrontendURL = strings.TrimSpace(cfg.Server.FrontendURL)
	cfg.JWT.Secret = strings.TrimSpace(cfg.JWT.Secret)
	cfg.LinuxDo.ClientID = strings.TrimSpace(cfg.LinuxDo.ClientID)
	cfg.LinuxDo.ClientSecret = strings.TrimSpace(cfg.LinuxDo.ClientSecret)
	cfg.LinuxDo.AuthorizeURL = strings.TrimSpace(cfg.LinuxDo.AuthorizeURL)
	cfg.LinuxDo.TokenURL = strings.TrimSpace(cfg.LinuxDo.TokenURL)
	cfg.LinuxDo.UserInfoURL = strings.TrimSpace(cfg.LinuxDo.UserInfoURL)
	cfg.LinuxDo.Scopes = strings.TrimSpace(cfg.LinuxDo.Scopes)
	cfg.LinuxDo.RedirectURL = strings.TrimSpace(cfg.LinuxDo.RedirectURL)
	cfg.LinuxDo.FrontendRedirectURL = strings.TrimSpace(cfg.LinuxDo.FrontendRedirectURL)
	cfg.LinuxDo.TokenAuthMethod = strings.ToLower(strings.TrimSpace(cfg.LinuxDo.TokenAuthMethod))
	cfg.LinuxDo.UserInfoEmailPath = strings.TrimSpace(cfg.LinuxDo.UserInfoEmailPath)
	cfg.LinuxDo.UserInfoIDPath = strings.TrimSpace(cfg.LinuxDo.UserInfoIDPath)
	cfg.LinuxDo.UserInfoUsernamePath = strings.TrimSpace(cfg.LinuxDo.UserInfoUsernamePath)
	applyLegacyWeChatConnectEnvCompatibility(&cfg.WeChat)
	normalizeWeChatConnectConfig(&cfg.WeChat)
	cfg.OIDC.ProviderName = strings.TrimSpace(cfg.OIDC.ProviderName)
	cfg.OIDC.ClientID = strings.TrimSpace(cfg.OIDC.ClientID)
	cfg.OIDC.ClientSecret = strings.TrimSpace(cfg.OIDC.ClientSecret)
	cfg.OIDC.IssuerURL = strings.TrimSpace(cfg.OIDC.IssuerURL)
	cfg.OIDC.DiscoveryURL = strings.TrimSpace(cfg.OIDC.DiscoveryURL)
	cfg.OIDC.AuthorizeURL = strings.TrimSpace(cfg.OIDC.AuthorizeURL)
	cfg.OIDC.TokenURL = strings.TrimSpace(cfg.OIDC.TokenURL)
	cfg.OIDC.UserInfoURL = strings.TrimSpace(cfg.OIDC.UserInfoURL)
	cfg.OIDC.JWKSURL = strings.TrimSpace(cfg.OIDC.JWKSURL)
	cfg.OIDC.Scopes = strings.TrimSpace(cfg.OIDC.Scopes)
	cfg.OIDC.RedirectURL = strings.TrimSpace(cfg.OIDC.RedirectURL)
	cfg.OIDC.FrontendRedirectURL = strings.TrimSpace(cfg.OIDC.FrontendRedirectURL)
	cfg.OIDC.TokenAuthMethod = strings.ToLower(strings.TrimSpace(cfg.OIDC.TokenAuthMethod))
	cfg.OIDC.AllowedSigningAlgs = strings.TrimSpace(cfg.OIDC.AllowedSigningAlgs)
	cfg.OIDC.UserInfoEmailPath = strings.TrimSpace(cfg.OIDC.UserInfoEmailPath)
	cfg.OIDC.UserInfoIDPath = strings.TrimSpace(cfg.OIDC.UserInfoIDPath)
	cfg.OIDC.UserInfoUsernamePath = strings.TrimSpace(cfg.OIDC.UserInfoUsernamePath)
	cfg.OIDC.UsePKCEExplicit = hasExplicitConfigOrEnv("oidc_connect.use_pkce", "OIDC_CONNECT_USE_PKCE")
	cfg.OIDC.ValidateIDTokenExplicit = hasExplicitConfigOrEnv("oidc_connect.validate_id_token", "OIDC_CONNECT_VALIDATE_ID_TOKEN")
	cfg.Dashboard.KeyPrefix = strings.TrimSpace(cfg.Dashboard.KeyPrefix)
	cfg.CORS.AllowedOrigins = normalizeStringSlice(cfg.CORS.AllowedOrigins)
	cfg.Security.ResponseHeaders.AdditionalAllowed = normalizeStringSlice(cfg.Security.ResponseHeaders.AdditionalAllowed)
	cfg.Security.ResponseHeaders.ForceRemove = normalizeStringSlice(cfg.Security.ResponseHeaders.ForceRemove)
	cfg.Security.CSP.Policy = strings.TrimSpace(cfg.Security.CSP.Policy)
	cfg.SetTrustForwardedIPForAPIKeyACL(cfg.Security.TrustForwardedIPForAPIKeyACL)
	cfg.Log.Level = strings.ToLower(strings.TrimSpace(cfg.Log.Level))
	cfg.Log.Format = strings.ToLower(strings.TrimSpace(cfg.Log.Format))
	cfg.Log.ServiceName = strings.TrimSpace(cfg.Log.ServiceName)
	cfg.Log.Environment = strings.TrimSpace(cfg.Log.Environment)
	cfg.Log.StacktraceLevel = strings.ToLower(strings.TrimSpace(cfg.Log.StacktraceLevel))
	cfg.Log.Output.FilePath = strings.TrimSpace(cfg.Log.Output.FilePath)
	cfg.Gateway.ForcedCodexInstructionsTemplateFile = strings.TrimSpace(cfg.Gateway.ForcedCodexInstructionsTemplateFile)
	if cfg.Gateway.ForcedCodexInstructionsTemplateFile != "" {
		content, err := os.ReadFile(cfg.Gateway.ForcedCodexInstructionsTemplateFile)
		if err != nil {
			return nil, fmt.Errorf("read forced codex instructions template %q: %w", cfg.Gateway.ForcedCodexInstructionsTemplateFile, err)
		}
		cfg.Gateway.ForcedCodexInstructionsTemplate = string(content)
	}

	//
	// <=0）
	if cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds <= 0 && cfg.Gateway.OpenAIWS.StickyPreviousResponseTTLSeconds > 0 {
		cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = cfg.Gateway.OpenAIWS.StickyPreviousResponseTTLSeconds
	}

	// Normalize UMQ mode:
	if m := cfg.Gateway.UserMessageQueue.Mode; m != "" && m != UMQModeSerialize && m != UMQModeThrottle {
		slog.Warn("invalid user_message_queue mode, disabling",
			"mode", m,
			"valid_modes", []string{UMQModeSerialize, UMQModeThrottle})
		cfg.Gateway.UserMessageQueue.Mode = ""
	}

	// Auto-generate TOTP encryption key if not set (32 bytes = 64 hex chars for AES-256)
	cfg.Totp.EncryptionKey = strings.TrimSpace(cfg.Totp.EncryptionKey)
	if cfg.Totp.EncryptionKey == "" {
		key, err := generateJWTSecret(32) // Reuse the same random generation function
		if err != nil {
			return nil, fmt.Errorf("generate totp encryption key error: %w", err)
		}
		cfg.Totp.EncryptionKey = key
		cfg.Totp.EncryptionKeyConfigured = false
		slog.Warn("TOTP encryption key auto-generated. Consider setting a fixed key for production.")
	} else {
		cfg.Totp.EncryptionKeyConfigured = true
	}

	originalJWTSecret := cfg.JWT.Secret
	if allowMissingJWTSecret && originalJWTSecret == "" {
		//
		cfg.JWT.Secret = strings.Repeat("0", 32)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config error: %w", err)
	}

	if allowMissingJWTSecret && originalJWTSecret == "" {
		cfg.JWT.Secret = ""
	}

	if !cfg.Security.URLAllowlist.Enabled {
		slog.Warn("security.url_allowlist.enabled=false; allowlist/SSRF checks disabled (minimal format validation only).")
	}
	if !cfg.Security.ResponseHeaders.Enabled {
		slog.Warn("security.response_headers.enabled=false; configurable header filtering disabled (default allowlist only).")
	}

	if cfg.JWT.Secret != "" && isWeakJWTSecret(cfg.JWT.Secret) {
		slog.Warn("JWT secret appears weak; use a 32+ character random secret in production.")
	}
	if len(cfg.Security.ResponseHeaders.AdditionalAllowed) > 0 || len(cfg.Security.ResponseHeaders.ForceRemove) > 0 {
		slog.Info("response header policy configured",
			"additional_allowed", cfg.Security.ResponseHeaders.AdditionalAllowed,
			"force_remove", cfg.Security.ResponseHeaders.ForceRemove,
		)
	}

	return &cfg, nil
}

func setDefaults() {
	viper.SetDefault("run_mode", RunModeStandard)

	// Server
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.mode", "release")
	viper.SetDefault("server.frontend_url", "")
	viper.SetDefault("server.read_header_timeout", 30) // 30 seconds read header timeout
	viper.SetDefault("server.idle_timeout", 120)       // 120 seconds idle timeout
	viper.SetDefault("server.trusted_proxies", []string{})
	viper.SetDefault("server.max_request_body_size", int64(256*1024*1024))
	// H2C
	viper.SetDefault("server.h2c.enabled", false)
	viper.SetDefault("server.h2c.max_concurrent_streams", uint32(50))      // 50 concurrent streams
	viper.SetDefault("server.h2c.idle_timeout", 75)                        // 75 seconds
	viper.SetDefault("server.h2c.max_read_frame_size", 1<<20)              // 1MB（sufficient)
	viper.SetDefault("server.h2c.max_upload_buffer_per_connection", 2<<20) // 2MB
	viper.SetDefault("server.h2c.max_upload_buffer_per_stream", 512<<10)   // 512KB

	// Log
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "console")
	viper.SetDefault("log.service_name", "sub2api")
	viper.SetDefault("log.env", "production")
	viper.SetDefault("log.caller", true)
	viper.SetDefault("log.stacktrace_level", "error")
	viper.SetDefault("log.output.to_stdout", true)
	viper.SetDefault("log.output.to_file", true)
	viper.SetDefault("log.output.file_path", "")
	viper.SetDefault("log.rotation.max_size_mb", 100)
	viper.SetDefault("log.rotation.max_backups", 10)
	viper.SetDefault("log.rotation.max_age_days", 7)
	viper.SetDefault("log.rotation.compress", true)
	viper.SetDefault("log.rotation.local_time", true)
	viper.SetDefault("log.sampling.enabled", false)
	viper.SetDefault("log.sampling.initial", 100)
	viper.SetDefault("log.sampling.thereafter", 100)

	// CORS
	viper.SetDefault("cors.allowed_origins", []string{})
	viper.SetDefault("cors.allow_credentials", true)

	// Security
	viper.SetDefault("security.url_allowlist.enabled", false)
	viper.SetDefault("security.url_allowlist.upstream_hosts", []string{
		"api.openai.com",
		"api.anthropic.com",
		"api.kimi.com",
		"open.bigmodel.cn",
		"api.minimaxi.com",
		"generativelanguage.googleapis.com",
		"cloudcode-pa.googleapis.com",
		"*.openai.azure.com",
	})
	viper.SetDefault("security.url_allowlist.pricing_hosts", []string{
		"raw.githubusercontent.com",
	})
	viper.SetDefault("security.url_allowlist.crs_hosts", []string{})
	viper.SetDefault("security.url_allowlist.allow_private_hosts", true)
	viper.SetDefault("security.url_allowlist.allow_insecure_http", true)
	viper.SetDefault("security.response_headers.enabled", true)
	viper.SetDefault("security.response_headers.additional_allowed", []string{})
	viper.SetDefault("security.response_headers.force_remove", []string{})
	viper.SetDefault("security.csp.enabled", true)
	viper.SetDefault("security.csp.policy", DefaultCSPPolicy)
	viper.SetDefault("security.proxy_probe.insecure_skip_verify", false)
	viper.SetDefault("security.trust_forwarded_ip_for_api_key_acl", false)

	// Security - disable direct fallback on proxy error
	viper.SetDefault("security.proxy_fallback.allow_direct_on_error", false)

	// Billing
	viper.SetDefault("billing.circuit_breaker.enabled", true)
	viper.SetDefault("billing.circuit_breaker.failure_threshold", 5)
	viper.SetDefault("billing.circuit_breaker.reset_timeout_seconds", 30)
	viper.SetDefault("billing.circuit_breaker.half_open_requests", 3)
	viper.SetDefault("billing.user_platform_quota_cache_ttl_seconds", 86400)
	viper.SetDefault("billing.user_platform_quota_sentinel_ttl_seconds", 3600)

	// Turnstile
	viper.SetDefault("turnstile.required", false)

	// LinuxDo Connect OAuth
	viper.SetDefault("linuxdo_connect.enabled", false)
	viper.SetDefault("linuxdo_connect.client_id", "")
	viper.SetDefault("linuxdo_connect.client_secret", "")
	viper.SetDefault("linuxdo_connect.authorize_url", "https://connect.linux.do/oauth2/authorize")
	viper.SetDefault("linuxdo_connect.token_url", "https://connect.linux.do/oauth2/token")
	viper.SetDefault("linuxdo_connect.userinfo_url", "https://connect.linux.do/api/user")
	viper.SetDefault("linuxdo_connect.scopes", "user")
	viper.SetDefault("linuxdo_connect.redirect_url", "")
	viper.SetDefault("linuxdo_connect.frontend_redirect_url", "/auth/linuxdo/callback")
	viper.SetDefault("linuxdo_connect.token_auth_method", "client_secret_post")
	viper.SetDefault("linuxdo_connect.use_pkce", false)
	viper.SetDefault("linuxdo_connect.userinfo_email_path", "")
	viper.SetDefault("linuxdo_connect.userinfo_id_path", "")
	viper.SetDefault("linuxdo_connect.userinfo_username_path", "")

	// WeChat Connect OAuth
	viper.SetDefault("wechat_connect.enabled", false)
	viper.SetDefault("wechat_connect.app_id", "")
	viper.SetDefault("wechat_connect.app_secret", "")
	viper.SetDefault("wechat_connect.open_app_id", "")
	viper.SetDefault("wechat_connect.open_app_secret", "")
	viper.SetDefault("wechat_connect.mp_app_id", "")
	viper.SetDefault("wechat_connect.mp_app_secret", "")
	viper.SetDefault("wechat_connect.mobile_app_id", "")
	viper.SetDefault("wechat_connect.mobile_app_secret", "")
	viper.SetDefault("wechat_connect.open_enabled", false)
	viper.SetDefault("wechat_connect.mp_enabled", false)
	viper.SetDefault("wechat_connect.mobile_enabled", false)
	viper.SetDefault("wechat_connect.mode", defaultWeChatConnectMode)
	viper.SetDefault("wechat_connect.scopes", defaultWeChatConnectScopes)
	viper.SetDefault("wechat_connect.redirect_url", "")
	viper.SetDefault("wechat_connect.frontend_redirect_url", defaultWeChatConnectFrontendRedirect)

	// Generic OIDC OAuth
	viper.SetDefault("oidc_connect.enabled", false)
	viper.SetDefault("oidc_connect.provider_name", "OIDC")
	viper.SetDefault("oidc_connect.client_id", "")
	viper.SetDefault("oidc_connect.client_secret", "")
	viper.SetDefault("oidc_connect.issuer_url", "")
	viper.SetDefault("oidc_connect.discovery_url", "")
	viper.SetDefault("oidc_connect.authorize_url", "")
	viper.SetDefault("oidc_connect.token_url", "")
	viper.SetDefault("oidc_connect.userinfo_url", "")
	viper.SetDefault("oidc_connect.jwks_url", "")
	viper.SetDefault("oidc_connect.scopes", "openid email profile")
	viper.SetDefault("oidc_connect.redirect_url", "")
	viper.SetDefault("oidc_connect.frontend_redirect_url", "/auth/oidc/callback")
	viper.SetDefault("oidc_connect.token_auth_method", "client_secret_post")
	viper.SetDefault("oidc_connect.use_pkce", true)
	viper.SetDefault("oidc_connect.validate_id_token", true)
	viper.SetDefault("oidc_connect.allowed_signing_algs", "RS256,ES256,PS256")
	viper.SetDefault("oidc_connect.clock_skew_seconds", 120)
	viper.SetDefault("oidc_connect.require_email_verified", false)
	viper.SetDefault("oidc_connect.userinfo_email_path", "")
	viper.SetDefault("oidc_connect.userinfo_id_path", "")
	viper.SetDefault("oidc_connect.userinfo_username_path", "")

	// DingTalk Connect OAuth
	viper.SetDefault("dingtalk_connect.enabled", false)
	viper.SetDefault("dingtalk_connect.authorize_url", "https://login.dingtalk.com/oauth2/auth")
	viper.SetDefault("dingtalk_connect.token_url", "https://api.dingtalk.com/v1.0/oauth2/userAccessToken")
	viper.SetDefault("dingtalk_connect.userinfo_url", "https://api.dingtalk.com/v1.0/contact/users/me")
	viper.SetDefault("dingtalk_connect.scopes", "openid")
	viper.SetDefault("dingtalk_connect.frontend_redirect_url", "/auth/dingtalk/callback")
	viper.SetDefault("dingtalk_connect.dingtalk_app_kind", "internal_app")
	viper.SetDefault("dingtalk_connect.app_type", "public")
	viper.SetDefault("dingtalk_connect.corp_restriction_policy", "none")
	viper.SetDefault("dingtalk_connect.require_email", true)
	viper.SetDefault("dingtalk_connect.username_overwrite_policy", "if_empty")

	// Database
	viper.SetDefault("database.host", "localhost")
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("database.user", "postgres")
	viper.SetDefault("database.password", "postgres")
	viper.SetDefault("database.dbname", "sub2api")
	viper.SetDefault("database.sslmode", "prefer")
	viper.SetDefault("database.max_open_conns", 256)
	viper.SetDefault("database.max_idle_conns", 128)
	viper.SetDefault("database.conn_max_lifetime_minutes", 30)
	viper.SetDefault("database.conn_max_idle_time_minutes", 5)
	viper.SetDefault("database.user_platform_quota_flusher_enabled", false)
	viper.SetDefault("database.user_platform_quota_flush_interval_ms", 2000)
	viper.SetDefault("database.user_platform_quota_flush_batch_size", 1000)

	// Redis
	viper.SetDefault("redis.host", "localhost")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("redis.password", "")
	viper.SetDefault("redis.db", 0)
	viper.SetDefault("redis.dial_timeout_seconds", 5)
	viper.SetDefault("redis.read_timeout_seconds", 3)
	viper.SetDefault("redis.write_timeout_seconds", 3)
	viper.SetDefault("redis.pool_size", 1024)
	viper.SetDefault("redis.min_idle_conns", 128)
	viper.SetDefault("redis.enable_tls", false)

	// Ops (vNext)
	viper.SetDefault("ops.enabled", true)
	viper.SetDefault("ops.use_preaggregated_tables", true)
	viper.SetDefault("ops.cleanup.enabled", true)
	viper.SetDefault("ops.cleanup.schedule", "0 2 * * *")
	// Retention days: vNext defaults to 30 days across ops datasets.
	viper.SetDefault("ops.cleanup.error_log_retention_days", 30)
	viper.SetDefault("ops.cleanup.minute_metrics_retention_days", 30)
	viper.SetDefault("ops.cleanup.hourly_metrics_retention_days", 30)
	viper.SetDefault("ops.aggregation.enabled", true)
	viper.SetDefault("ops.metrics_collector_cache.enabled", true)
	// TTL should be slightly larger than collection interval (1m) to maximize cross-replica cache hits.
	viper.SetDefault("ops.metrics_collector_cache.ttl", 65*time.Second)

	// JWT
	viper.SetDefault("jwt.secret", "")
	viper.SetDefault("jwt.expire_hour", 24)
	viper.SetDefault("jwt.access_token_expire_minutes", 0) // 0 means fallback to expire_hour
	viper.SetDefault("jwt.refresh_token_expire_days", 30)  // 30-day refresh token validity
	viper.SetDefault("jwt.refresh_window_minutes", 2)      // allow refresh 2 minutes before expiration

	// TOTP
	viper.SetDefault("totp.encryption_key", "")

	// Default
	// Admin credentials are created via the setup flow (web wizard / CLI / AUTO_SETUP).
	// Do not ship fixed defaults here to avoid insecure "known credentials" in production.
	viper.SetDefault("default.admin_email", "")
	viper.SetDefault("default.admin_password", "")
	viper.SetDefault("default.user_concurrency", 5)
	viper.SetDefault("default.user_balance", 0)
	viper.SetDefault("default.api_key_prefix", "sk-")
	viper.SetDefault("default.rate_multiplier", 1.0)

	// RateLimit
	viper.SetDefault("rate_limit.overload_cooldown_minutes", 10)
	viper.SetDefault("rate_limit.oauth_401_cooldown_minutes", 10)

	// Pricing -
	viper.SetDefault("pricing.remote_url", "https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.json")
	viper.SetDefault("pricing.hash_url", "https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.sha256")
	viper.SetDefault("pricing.data_dir", "./data")
	viper.SetDefault("pricing.fallback_file", "./resources/model-pricing/model_prices_and_context_window.json")
	viper.SetDefault("pricing.update_interval_hours", 24)
	viper.SetDefault("pricing.hash_check_interval_minutes", 10)

	// Timezone (default to Asia/Shanghai for Chinese users)
	viper.SetDefault("timezone", "Asia/Shanghai")

	// API Key auth cache
	viper.SetDefault("api_key_auth_cache.l1_size", 65535)
	viper.SetDefault("api_key_auth_cache.l1_ttl_seconds", 15)
	viper.SetDefault("api_key_auth_cache.l2_ttl_seconds", 300)
	viper.SetDefault("api_key_auth_cache.negative_ttl_seconds", 30)
	viper.SetDefault("api_key_auth_cache.jitter_percent", 10)
	viper.SetDefault("api_key_auth_cache.singleflight", true)

	// Subscription auth L1 cache
	viper.SetDefault("subscription_cache.l1_size", 16384)
	viper.SetDefault("subscription_cache.l1_ttl_seconds", 10)
	viper.SetDefault("subscription_cache.jitter_percent", 10)

	// Dashboard cache
	viper.SetDefault("dashboard_cache.enabled", true)
	viper.SetDefault("dashboard_cache.key_prefix", "sub2api:")
	viper.SetDefault("dashboard_cache.stats_fresh_ttl_seconds", 15)
	viper.SetDefault("dashboard_cache.stats_ttl_seconds", 30)
	viper.SetDefault("dashboard_cache.stats_refresh_timeout_seconds", 30)

	// Dashboard aggregation
	viper.SetDefault("dashboard_aggregation.enabled", true)
	viper.SetDefault("dashboard_aggregation.interval_seconds", 60)
	viper.SetDefault("dashboard_aggregation.lookback_seconds", 120)
	viper.SetDefault("dashboard_aggregation.backfill_enabled", false)
	viper.SetDefault("dashboard_aggregation.backfill_max_days", 31)
	viper.SetDefault("dashboard_aggregation.retention.usage_logs_days", 90)
	viper.SetDefault("dashboard_aggregation.retention.usage_billing_dedup_days", 365)
	viper.SetDefault("dashboard_aggregation.retention.hourly_days", 180)
	viper.SetDefault("dashboard_aggregation.retention.daily_days", 730)
	viper.SetDefault("dashboard_aggregation.recompute_days", 2)

	// Usage cleanup task
	viper.SetDefault("usage_cleanup.enabled", true)
	viper.SetDefault("usage_cleanup.max_range_days", 31)
	viper.SetDefault("usage_cleanup.batch_size", 5000)
	viper.SetDefault("usage_cleanup.worker_interval_seconds", 10)
	viper.SetDefault("usage_cleanup.task_timeout_seconds", 1800)

	// Idempotency
	viper.SetDefault("idempotency.observe_only", true)
	viper.SetDefault("idempotency.default_ttl_seconds", 86400)
	viper.SetDefault("idempotency.system_operation_ttl_seconds", 3600)
	viper.SetDefault("idempotency.processing_timeout_seconds", 30)
	viper.SetDefault("idempotency.failed_retry_backoff_seconds", 5)
	viper.SetDefault("idempotency.max_stored_response_len", 64*1024)
	viper.SetDefault("idempotency.cleanup_interval_seconds", 60)
	viper.SetDefault("idempotency.cleanup_batch_size", 500)

	// Gateway
	viper.SetDefault("gateway.response_header_timeout", 600) // 600 seconds (10 min) upstream response header timeout; may queue longer under heavy LLM load
	viper.SetDefault("gateway.openai_response_header_timeout", 0)
	viper.SetDefault("gateway.log_upstream_error_body", true)
	viper.SetDefault("gateway.log_upstream_error_body_max_bytes", 2048)
	viper.SetDefault("gateway.inject_beta_for_apikey", false)
	viper.SetDefault("gateway.failover_on_400", false)
	viper.SetDefault("gateway.max_account_switches", 10)
	viper.SetDefault("gateway.max_account_switches_gemini", 3)
	viper.SetDefault("gateway.force_codex_cli", false)
	viper.SetDefault("gateway.codex_image_generation_bridge_enabled", false)
	viper.SetDefault("gateway.openai_passthrough_allow_timeout_headers", false)
	// OpenAI Responses WebSocket（
	viper.SetDefault("gateway.openai_ws.enabled", true)
	viper.SetDefault("gateway.openai_ws.mode_router_v2_enabled", false)
	viper.SetDefault("gateway.openai_ws.ingress_mode_default", "ctx_pool")
	viper.SetDefault("gateway.openai_ws.oauth_enabled", true)
	viper.SetDefault("gateway.openai_ws.apikey_enabled", true)
	viper.SetDefault("gateway.openai_ws.force_http", false)
	viper.SetDefault("gateway.openai_ws.allow_store_recovery", false)
	viper.SetDefault("gateway.openai_ws.ingress_previous_response_recovery_enabled", true)
	viper.SetDefault("gateway.openai_ws.store_disabled_conn_mode", "strict")
	viper.SetDefault("gateway.openai_ws.store_disabled_force_new_conn", true)
	viper.SetDefault("gateway.openai_ws.prewarm_generate_enabled", false)
	viper.SetDefault("gateway.openai_ws.client_read_limit_bytes", 64*1024*1024)
	viper.SetDefault("gateway.openai_ws.http_bridge_enabled", true)
	viper.SetDefault("gateway.openai_ws.http_bridge_threshold_bytes", 15*1024*1024)
	viper.SetDefault("gateway.openai_ws.responses_websockets", false)
	viper.SetDefault("gateway.openai_ws.responses_websockets_v2", true)
	viper.SetDefault("gateway.openai_ws.max_conns_per_account", 128)
	viper.SetDefault("gateway.openai_ws.min_idle_per_account", 4)
	viper.SetDefault("gateway.openai_ws.max_idle_per_account", 12)
	viper.SetDefault("gateway.openai_ws.dynamic_max_conns_by_account_concurrency_enabled", true)
	viper.SetDefault("gateway.openai_ws.oauth_max_conns_factor", 1.0)
	viper.SetDefault("gateway.openai_ws.apikey_max_conns_factor", 1.0)
	viper.SetDefault("gateway.openai_ws.dial_timeout_seconds", 10)
	viper.SetDefault("gateway.openai_ws.read_timeout_seconds", 900)
	viper.SetDefault("gateway.openai_ws.write_timeout_seconds", 120)
	viper.SetDefault("gateway.openai_ws.pool_target_utilization", 0.7)
	viper.SetDefault("gateway.openai_ws.queue_limit_per_conn", 64)
	viper.SetDefault("gateway.openai_ws.event_flush_batch_size", 1)
	viper.SetDefault("gateway.openai_ws.event_flush_interval_ms", 10)
	viper.SetDefault("gateway.openai_ws.prewarm_cooldown_ms", 300)
	viper.SetDefault("gateway.openai_ws.fallback_cooldown_seconds", 30)
	viper.SetDefault("gateway.openai_ws.retry_backoff_initial_ms", 120)
	viper.SetDefault("gateway.openai_ws.retry_backoff_max_ms", 2000)
	viper.SetDefault("gateway.openai_ws.retry_jitter_ratio", 0.2)
	viper.SetDefault("gateway.openai_ws.retry_total_budget_ms", 5000)
	viper.SetDefault("gateway.openai_ws.payload_log_sample_rate", 0.2)
	viper.SetDefault("gateway.openai_ws.lb_top_k", 7)
	viper.SetDefault("gateway.openai_ws.sticky_session_ttl_seconds", 3600)
	viper.SetDefault("gateway.openai_ws.session_hash_read_old_fallback", true)
	viper.SetDefault("gateway.openai_ws.session_hash_dual_write_old", true)
	viper.SetDefault("gateway.openai_ws.metadata_bridge_enabled", true)
	viper.SetDefault("gateway.openai_ws.sticky_response_id_ttl_seconds", 3600)
	viper.SetDefault("gateway.openai_ws.sticky_previous_response_ttl_seconds", 3600)
	viper.SetDefault("gateway.openai_ws.scheduler_score_weights.priority", 1.0)
	viper.SetDefault("gateway.openai_ws.scheduler_score_weights.load", 1.0)
	viper.SetDefault("gateway.openai_ws.scheduler_score_weights.queue", 0.7)
	viper.SetDefault("gateway.openai_ws.scheduler_score_weights.error_rate", 0.8)
	viper.SetDefault("gateway.openai_ws.scheduler_score_weights.ttft", 0.5)
	// OpenAI HTTP upstream protocol strategy
	viper.SetDefault("gateway.openai_http2.enabled", true)
	viper.SetDefault("gateway.openai_http2.allow_proxy_fallback_to_http1", true)
	viper.SetDefault("gateway.openai_http2.fallback_error_threshold", 2)
	viper.SetDefault("gateway.openai_http2.fallback_window_seconds", 60)
	viper.SetDefault("gateway.openai_http2.fallback_ttl_seconds", 600)
	viper.SetDefault("gateway.image_concurrency.enabled", false)
	viper.SetDefault("gateway.image_concurrency.max_concurrent_requests", 0)
	viper.SetDefault("gateway.image_concurrency.overflow_mode", ImageConcurrencyOverflowModeReject)
	viper.SetDefault("gateway.image_concurrency.wait_timeout_seconds", 30)
	viper.SetDefault("gateway.image_concurrency.max_waiting_requests", 100)
	viper.SetDefault("gateway.antigravity_fallback_cooldown_minutes", 1)
	viper.SetDefault("gateway.antigravity_extra_retries", 10)
	viper.SetDefault("gateway.max_body_size", int64(256*1024*1024))
	viper.SetDefault("gateway.upstream_response_read_max_bytes", DefaultUpstreamResponseReadMaxBytes)
	viper.SetDefault("gateway.proxy_probe_response_read_max_bytes", int64(1024*1024))
	viper.SetDefault("gateway.gemini_debug_response_headers", false)
	viper.SetDefault("gateway.connection_pool_isolation", ConnectionPoolIsolationAccountProxy)
	// HTTP +
	viper.SetDefault("gateway.max_idle_conns", 2560)          // max total idle connections (can be increased for high-concurrency scenarios)
	viper.SetDefault("gateway.max_idle_conns_per_host", 120)  // max idle connections per host (default for HTTP/2 scenarios)
	viper.SetDefault("gateway.max_conns_per_host", 1024)      // max connections per host (including active; can be increased to 2400+ for streaming/HTTP1.1 scenarios)
	viper.SetDefault("gateway.idle_conn_timeout_seconds", 90) // idle connection timeout (seconds)
	viper.SetDefault("gateway.max_upstream_clients", 5000)
	viper.SetDefault("gateway.client_idle_ttl_seconds", 900)
	viper.SetDefault("gateway.concurrency_slot_ttl_minutes", 30) // concurrency slot expiry time (supports long-running requests)
	viper.SetDefault("gateway.stream_data_interval_timeout", 180)
	viper.SetDefault("gateway.stream_keepalive_interval", 10)
	viper.SetDefault("gateway.image_stream_data_interval_timeout", 900)
	viper.SetDefault("gateway.image_stream_keepalive_interval", 10)
	viper.SetDefault("gateway.max_line_size", 500*1024*1024)
	viper.SetDefault("gateway.scheduling.sticky_session_max_waiting", 3)
	viper.SetDefault("gateway.scheduling.sticky_session_wait_timeout", 120*time.Second)
	viper.SetDefault("gateway.scheduling.fallback_wait_timeout", 30*time.Second)
	viper.SetDefault("gateway.scheduling.fallback_max_waiting", 100)
	viper.SetDefault("gateway.scheduling.fallback_selection_mode", "last_used")
	viper.SetDefault("gateway.scheduling.load_batch_enabled", true)
	viper.SetDefault("gateway.scheduling.load_batch_cache_ttl_ms", 200)
	viper.SetDefault("gateway.scheduling.snapshot_mget_chunk_size", 128)
	viper.SetDefault("gateway.scheduling.snapshot_write_chunk_size", 256)
	viper.SetDefault("gateway.scheduling.slot_cleanup_interval", 30*time.Second)
	viper.SetDefault("gateway.scheduling.db_fallback_enabled", true)
	viper.SetDefault("gateway.scheduling.db_fallback_timeout_seconds", 0)
	viper.SetDefault("gateway.scheduling.db_fallback_max_qps", 0)
	viper.SetDefault("gateway.scheduling.outbox_poll_interval_seconds", 1)
	viper.SetDefault("gateway.scheduling.outbox_lag_warn_seconds", 5)
	viper.SetDefault("gateway.scheduling.outbox_lag_rebuild_seconds", 10)
	viper.SetDefault("gateway.scheduling.outbox_lag_rebuild_failures", 3)
	viper.SetDefault("gateway.scheduling.outbox_backlog_rebuild_rows", 10000)
	viper.SetDefault("gateway.scheduling.full_rebuild_interval_seconds", 300)
	viper.SetDefault("gateway.usage_record.worker_count", 128)
	viper.SetDefault("gateway.usage_record.queue_size", 16384)
	viper.SetDefault("gateway.usage_record.task_timeout_seconds", 5)
	viper.SetDefault("gateway.usage_record.overflow_policy", UsageRecordOverflowPolicySample)
	viper.SetDefault("gateway.usage_record.overflow_sample_percent", 10)
	viper.SetDefault("gateway.usage_record.auto_scale_enabled", true)
	viper.SetDefault("gateway.usage_record.auto_scale_min_workers", 128)
	viper.SetDefault("gateway.usage_record.auto_scale_max_workers", 512)
	viper.SetDefault("gateway.usage_record.auto_scale_up_queue_percent", 70)
	viper.SetDefault("gateway.usage_record.auto_scale_down_queue_percent", 15)
	viper.SetDefault("gateway.usage_record.auto_scale_up_step", 32)
	viper.SetDefault("gateway.usage_record.auto_scale_down_step", 16)
	viper.SetDefault("gateway.usage_record.auto_scale_check_interval_seconds", 3)
	viper.SetDefault("gateway.usage_record.auto_scale_cooldown_seconds", 10)
	viper.SetDefault("gateway.user_group_rate_cache_ttl_seconds", 30)
	viper.SetDefault("gateway.models_list_cache_ttl_seconds", 15)
	// TLS
	viper.SetDefault("gateway.user_message_queue.enabled", false)
	viper.SetDefault("gateway.user_message_queue.lock_ttl_ms", 120000)
	viper.SetDefault("gateway.user_message_queue.wait_timeout_ms", 30000)
	viper.SetDefault("gateway.user_message_queue.min_delay_ms", 200)
	viper.SetDefault("gateway.user_message_queue.max_delay_ms", 2000)
	viper.SetDefault("gateway.user_message_queue.cleanup_interval_seconds", 60)

	viper.SetDefault("gateway.tls_fingerprint.enabled", true)
	viper.SetDefault("concurrency.ping_interval", 10)

	// TokenRefresh
	viper.SetDefault("token_refresh.enabled", true)
	viper.SetDefault("token_refresh.check_interval_minutes", 5)        // check every 5 minutes
	viper.SetDefault("token_refresh.refresh_before_expiry_hours", 0.5) // refresh 30 minutes before expiry (adapted for Google 1-hour tokens)
	viper.SetDefault("token_refresh.max_retries", 3)                   // max 3 retries
	viper.SetDefault("token_refresh.retry_backoff_seconds", 2)         // retry backoff base 2 seconds

	// Gemini OAuth - configure via environment variables or config file
	// GEMINI_OAUTH_CLIENT_ID and GEMINI_OAUTH_CLIENT_SECRET
	// Default: uses Gemini CLI public credentials (set via environment)
	viper.SetDefault("gemini.oauth.client_id", "")
	viper.SetDefault("gemini.oauth.client_secret", "")
	viper.SetDefault("gemini.oauth.scopes", "")
	viper.SetDefault("gemini.quota.policy", "")

	// Subscription Maintenance (bounded queue + worker pool)
	viper.SetDefault("subscription_maintenance.worker_count", 2)
	viper.SetDefault("subscription_maintenance.queue_size", 1024)

}

func (c *Config) Validate() error {
	jwtSecret := strings.TrimSpace(c.JWT.Secret)
	if jwtSecret == "" {
		return fmt.Errorf("jwt.secret is required")
	}
	// NOTE:
	// “”“”。
	if len([]byte(jwtSecret)) < 32 {
		return fmt.Errorf("jwt.secret must be at least 32 bytes")
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	case "":
		return fmt.Errorf("log.level is required")
	default:
		return fmt.Errorf("log.level must be one of: debug/info/warn/error")
	}
	switch c.Log.Format {
	case "json", "console":
	case "":
		return fmt.Errorf("log.format is required")
	default:
		return fmt.Errorf("log.format must be one of: json/console")
	}
	switch c.Log.StacktraceLevel {
	case "none", "error", "fatal":
	case "":
		return fmt.Errorf("log.stacktrace_level is required")
	default:
		return fmt.Errorf("log.stacktrace_level must be one of: none/error/fatal")
	}
	if !c.Log.Output.ToStdout && !c.Log.Output.ToFile {
		return fmt.Errorf("log.output.to_stdout and log.output.to_file cannot both be false")
	}
	if c.Log.Rotation.MaxSizeMB <= 0 {
		return fmt.Errorf("log.rotation.max_size_mb must be positive")
	}
	if c.Log.Rotation.MaxBackups < 0 {
		return fmt.Errorf("log.rotation.max_backups must be non-negative")
	}
	if c.Log.Rotation.MaxAgeDays < 0 {
		return fmt.Errorf("log.rotation.max_age_days must be non-negative")
	}
	if c.Log.Sampling.Enabled {
		if c.Log.Sampling.Initial <= 0 {
			return fmt.Errorf("log.sampling.initial must be positive when sampling is enabled")
		}
		if c.Log.Sampling.Thereafter <= 0 {
			return fmt.Errorf("log.sampling.thereafter must be positive when sampling is enabled")
		}
	} else {
		if c.Log.Sampling.Initial < 0 {
			return fmt.Errorf("log.sampling.initial must be non-negative")
		}
		if c.Log.Sampling.Thereafter < 0 {
			return fmt.Errorf("log.sampling.thereafter must be non-negative")
		}
	}

	if c.SubscriptionMaintenance.WorkerCount < 0 {
		return fmt.Errorf("subscription_maintenance.worker_count must be non-negative")
	}
	if c.SubscriptionMaintenance.QueueSize < 0 {
		return fmt.Errorf("subscription_maintenance.queue_size must be non-negative")
	}

	// Gemini OAuth
	//
	geminiClientID := strings.TrimSpace(c.Gemini.OAuth.ClientID)
	geminiClientSecret := strings.TrimSpace(c.Gemini.OAuth.ClientSecret)
	if (geminiClientID == "") != (geminiClientSecret == "") {
		return fmt.Errorf("gemini.oauth.client_id and gemini.oauth.client_secret must be both set or both empty")
	}

	if strings.TrimSpace(c.Server.FrontendURL) != "" {
		if err := ValidateAbsoluteHTTPURL(c.Server.FrontendURL); err != nil {
			return fmt.Errorf("server.frontend_url invalid: %w", err)
		}
		u, err := url.Parse(strings.TrimSpace(c.Server.FrontendURL))
		if err != nil {
			return fmt.Errorf("server.frontend_url invalid: %w", err)
		}
		if u.RawQuery != "" || u.ForceQuery {
			return fmt.Errorf("server.frontend_url invalid: must not include query")
		}
		if u.User != nil {
			return fmt.Errorf("server.frontend_url invalid: must not include userinfo")
		}
		warnIfInsecureURL("server.frontend_url", c.Server.FrontendURL)
	}
	if c.JWT.ExpireHour <= 0 {
		return fmt.Errorf("jwt.expire_hour must be positive")
	}
	if c.JWT.ExpireHour > 168 {
		return fmt.Errorf("jwt.expire_hour must be <= 168 (7 days)")
	}
	if c.JWT.ExpireHour > 24 {
		slog.Warn("jwt.expire_hour is high; consider shorter expiration for security", "expire_hour", c.JWT.ExpireHour)
	}
	// JWT Refresh Token
	if c.JWT.AccessTokenExpireMinutes < 0 {
		return fmt.Errorf("jwt.access_token_expire_minutes must be non-negative")
	}
	if c.JWT.AccessTokenExpireMinutes > 720 {
		slog.Warn("jwt.access_token_expire_minutes is high; consider shorter expiration for security", "access_token_expire_minutes", c.JWT.AccessTokenExpireMinutes)
	}
	if c.JWT.RefreshTokenExpireDays <= 0 {
		return fmt.Errorf("jwt.refresh_token_expire_days must be positive")
	}
	if c.JWT.RefreshTokenExpireDays > 90 {
		slog.Warn("jwt.refresh_token_expire_days is high; consider shorter expiration for security", "refresh_token_expire_days", c.JWT.RefreshTokenExpireDays)
	}
	if c.JWT.RefreshWindowMinutes < 0 {
		return fmt.Errorf("jwt.refresh_window_minutes must be non-negative")
	}
	if c.Security.CSP.Enabled && strings.TrimSpace(c.Security.CSP.Policy) == "" {
		return fmt.Errorf("security.csp.policy is required when CSP is enabled")
	}
	if c.LinuxDo.Enabled {
		if strings.TrimSpace(c.LinuxDo.ClientID) == "" {
			return fmt.Errorf("linuxdo_connect.client_id is required when linuxdo_connect.enabled=true")
		}
		if strings.TrimSpace(c.LinuxDo.AuthorizeURL) == "" {
			return fmt.Errorf("linuxdo_connect.authorize_url is required when linuxdo_connect.enabled=true")
		}
		if strings.TrimSpace(c.LinuxDo.TokenURL) == "" {
			return fmt.Errorf("linuxdo_connect.token_url is required when linuxdo_connect.enabled=true")
		}
		if strings.TrimSpace(c.LinuxDo.UserInfoURL) == "" {
			return fmt.Errorf("linuxdo_connect.userinfo_url is required when linuxdo_connect.enabled=true")
		}
		if strings.TrimSpace(c.LinuxDo.RedirectURL) == "" {
			return fmt.Errorf("linuxdo_connect.redirect_url is required when linuxdo_connect.enabled=true")
		}
		method := strings.ToLower(strings.TrimSpace(c.LinuxDo.TokenAuthMethod))
		switch method {
		case "", "client_secret_post", "client_secret_basic", "none":
		default:
			return fmt.Errorf("linuxdo_connect.token_auth_method must be one of: client_secret_post/client_secret_basic/none")
		}
		if (method == "" || method == "client_secret_post" || method == "client_secret_basic") &&
			strings.TrimSpace(c.LinuxDo.ClientSecret) == "" {
			return fmt.Errorf("linuxdo_connect.client_secret is required when linuxdo_connect.enabled=true and token_auth_method is client_secret_post/client_secret_basic")
		}
		if strings.TrimSpace(c.LinuxDo.FrontendRedirectURL) == "" {
			return fmt.Errorf("linuxdo_connect.frontend_redirect_url is required when linuxdo_connect.enabled=true")
		}

		if err := ValidateAbsoluteHTTPURL(c.LinuxDo.AuthorizeURL); err != nil {
			return fmt.Errorf("linuxdo_connect.authorize_url invalid: %w", err)
		}
		if err := ValidateAbsoluteHTTPURL(c.LinuxDo.TokenURL); err != nil {
			return fmt.Errorf("linuxdo_connect.token_url invalid: %w", err)
		}
		if err := ValidateAbsoluteHTTPURL(c.LinuxDo.UserInfoURL); err != nil {
			return fmt.Errorf("linuxdo_connect.userinfo_url invalid: %w", err)
		}
		if err := ValidateAbsoluteHTTPURL(c.LinuxDo.RedirectURL); err != nil {
			return fmt.Errorf("linuxdo_connect.redirect_url invalid: %w", err)
		}
		if err := ValidateFrontendRedirectURL(c.LinuxDo.FrontendRedirectURL); err != nil {
			return fmt.Errorf("linuxdo_connect.frontend_redirect_url invalid: %w", err)
		}

		warnIfInsecureURL("linuxdo_connect.authorize_url", c.LinuxDo.AuthorizeURL)
		warnIfInsecureURL("linuxdo_connect.token_url", c.LinuxDo.TokenURL)
		warnIfInsecureURL("linuxdo_connect.userinfo_url", c.LinuxDo.UserInfoURL)
		warnIfInsecureURL("linuxdo_connect.redirect_url", c.LinuxDo.RedirectURL)
		warnIfInsecureURL("linuxdo_connect.frontend_redirect_url", c.LinuxDo.FrontendRedirectURL)
	}
	if c.WeChat.Enabled {
		weChat := c.WeChat
		normalizeWeChatConnectConfig(&weChat)

		if weChat.OpenEnabled {
			if strings.TrimSpace(weChat.OpenAppID) == "" {
				return fmt.Errorf("wechat_connect.open_app_id is required when wechat_connect.open_enabled=true")
			}
			if strings.TrimSpace(weChat.OpenAppSecret) == "" {
				return fmt.Errorf("wechat_connect.open_app_secret is required when wechat_connect.open_enabled=true")
			}
		}
		if weChat.MPEnabled {
			if strings.TrimSpace(weChat.MPAppID) == "" {
				return fmt.Errorf("wechat_connect.mp_app_id is required when wechat_connect.mp_enabled=true")
			}
			if strings.TrimSpace(weChat.MPAppSecret) == "" {
				return fmt.Errorf("wechat_connect.mp_app_secret is required when wechat_connect.mp_enabled=true")
			}
		}
		if weChat.MobileEnabled {
			if strings.TrimSpace(weChat.MobileAppID) == "" {
				return fmt.Errorf("wechat_connect.mobile_app_id is required when wechat_connect.mobile_enabled=true")
			}
			if strings.TrimSpace(weChat.MobileAppSecret) == "" {
				return fmt.Errorf("wechat_connect.mobile_app_secret is required when wechat_connect.mobile_enabled=true")
			}
		}
		if v := strings.TrimSpace(weChat.RedirectURL); v != "" {
			if err := ValidateAbsoluteHTTPURL(v); err != nil {
				return fmt.Errorf("wechat_connect.redirect_url invalid: %w", err)
			}
			warnIfInsecureURL("wechat_connect.redirect_url", v)
		}
		if err := ValidateFrontendRedirectURL(weChat.FrontendRedirectURL); err != nil {
			return fmt.Errorf("wechat_connect.frontend_redirect_url invalid: %w", err)
		}
		warnIfInsecureURL("wechat_connect.frontend_redirect_url", weChat.FrontendRedirectURL)
	}
	if c.OIDC.Enabled {
		if strings.TrimSpace(c.OIDC.ClientID) == "" {
			return fmt.Errorf("oidc_connect.client_id is required when oidc_connect.enabled=true")
		}
		if strings.TrimSpace(c.OIDC.IssuerURL) == "" {
			return fmt.Errorf("oidc_connect.issuer_url is required when oidc_connect.enabled=true")
		}
		if strings.TrimSpace(c.OIDC.RedirectURL) == "" {
			return fmt.Errorf("oidc_connect.redirect_url is required when oidc_connect.enabled=true")
		}
		if strings.TrimSpace(c.OIDC.FrontendRedirectURL) == "" {
			return fmt.Errorf("oidc_connect.frontend_redirect_url is required when oidc_connect.enabled=true")
		}
		if !scopeContainsOpenID(c.OIDC.Scopes) {
			return fmt.Errorf("oidc_connect.scopes must contain openid")
		}

		method := strings.ToLower(strings.TrimSpace(c.OIDC.TokenAuthMethod))
		switch method {
		case "", "client_secret_post", "client_secret_basic", "none":
		default:
			return fmt.Errorf("oidc_connect.token_auth_method must be one of: client_secret_post/client_secret_basic/none")
		}
		if (method == "" || method == "client_secret_post" || method == "client_secret_basic") &&
			strings.TrimSpace(c.OIDC.ClientSecret) == "" {
			return fmt.Errorf("oidc_connect.client_secret is required when oidc_connect.enabled=true and token_auth_method is client_secret_post/client_secret_basic")
		}
		if c.OIDC.ClockSkewSeconds < 0 || c.OIDC.ClockSkewSeconds > 600 {
			return fmt.Errorf("oidc_connect.clock_skew_seconds must be between 0 and 600")
		}
		if c.OIDC.ValidateIDToken && strings.TrimSpace(c.OIDC.AllowedSigningAlgs) == "" {
			return fmt.Errorf("oidc_connect.allowed_signing_algs is required when oidc_connect.validate_id_token=true")
		}

		if err := ValidateAbsoluteHTTPURL(c.OIDC.IssuerURL); err != nil {
			return fmt.Errorf("oidc_connect.issuer_url invalid: %w", err)
		}
		if v := strings.TrimSpace(c.OIDC.DiscoveryURL); v != "" {
			if err := ValidateAbsoluteHTTPURL(v); err != nil {
				return fmt.Errorf("oidc_connect.discovery_url invalid: %w", err)
			}
		}
		if v := strings.TrimSpace(c.OIDC.AuthorizeURL); v != "" {
			if err := ValidateAbsoluteHTTPURL(v); err != nil {
				return fmt.Errorf("oidc_connect.authorize_url invalid: %w", err)
			}
		}
		if v := strings.TrimSpace(c.OIDC.TokenURL); v != "" {
			if err := ValidateAbsoluteHTTPURL(v); err != nil {
				return fmt.Errorf("oidc_connect.token_url invalid: %w", err)
			}
		}
		if v := strings.TrimSpace(c.OIDC.UserInfoURL); v != "" {
			if err := ValidateAbsoluteHTTPURL(v); err != nil {
				return fmt.Errorf("oidc_connect.userinfo_url invalid: %w", err)
			}
		}
		if v := strings.TrimSpace(c.OIDC.JWKSURL); v != "" {
			if err := ValidateAbsoluteHTTPURL(v); err != nil {
				return fmt.Errorf("oidc_connect.jwks_url invalid: %w", err)
			}
		}
		if err := ValidateAbsoluteHTTPURL(c.OIDC.RedirectURL); err != nil {
			return fmt.Errorf("oidc_connect.redirect_url invalid: %w", err)
		}
		if err := ValidateFrontendRedirectURL(c.OIDC.FrontendRedirectURL); err != nil {
			return fmt.Errorf("oidc_connect.frontend_redirect_url invalid: %w", err)
		}

		warnIfInsecureURL("oidc_connect.issuer_url", c.OIDC.IssuerURL)
		warnIfInsecureURL("oidc_connect.discovery_url", c.OIDC.DiscoveryURL)
		warnIfInsecureURL("oidc_connect.authorize_url", c.OIDC.AuthorizeURL)
		warnIfInsecureURL("oidc_connect.token_url", c.OIDC.TokenURL)
		warnIfInsecureURL("oidc_connect.userinfo_url", c.OIDC.UserInfoURL)
		warnIfInsecureURL("oidc_connect.jwks_url", c.OIDC.JWKSURL)
		warnIfInsecureURL("oidc_connect.redirect_url", c.OIDC.RedirectURL)
		warnIfInsecureURL("oidc_connect.frontend_redirect_url", c.OIDC.FrontendRedirectURL)
	}
	if c.Billing.CircuitBreaker.Enabled {
		if c.Billing.CircuitBreaker.FailureThreshold <= 0 {
			return fmt.Errorf("billing.circuit_breaker.failure_threshold must be positive")
		}
		if c.Billing.CircuitBreaker.ResetTimeoutSeconds <= 0 {
			return fmt.Errorf("billing.circuit_breaker.reset_timeout_seconds must be positive")
		}
		if c.Billing.CircuitBreaker.HalfOpenRequests <= 0 {
			return fmt.Errorf("billing.circuit_breaker.half_open_requests must be positive")
		}
	}
	if c.Database.MaxOpenConns <= 0 {
		return fmt.Errorf("database.max_open_conns must be positive")
	}
	if c.Database.MaxIdleConns < 0 {
		return fmt.Errorf("database.max_idle_conns must be non-negative")
	}
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		return fmt.Errorf("database.max_idle_conns cannot exceed database.max_open_conns")
	}
	if c.Database.ConnMaxLifetimeMinutes < 0 {
		return fmt.Errorf("database.conn_max_lifetime_minutes must be non-negative")
	}
	if c.Database.ConnMaxIdleTimeMinutes < 0 {
		return fmt.Errorf("database.conn_max_idle_time_minutes must be non-negative")
	}
	if c.Redis.DialTimeoutSeconds <= 0 {
		return fmt.Errorf("redis.dial_timeout_seconds must be positive")
	}
	if c.Redis.ReadTimeoutSeconds <= 0 {
		return fmt.Errorf("redis.read_timeout_seconds must be positive")
	}
	if c.Redis.WriteTimeoutSeconds <= 0 {
		return fmt.Errorf("redis.write_timeout_seconds must be positive")
	}
	if c.Redis.PoolSize <= 0 {
		return fmt.Errorf("redis.pool_size must be positive")
	}
	if c.Redis.MinIdleConns < 0 {
		return fmt.Errorf("redis.min_idle_conns must be non-negative")
	}
	if c.Redis.MinIdleConns > c.Redis.PoolSize {
		return fmt.Errorf("redis.min_idle_conns cannot exceed redis.pool_size")
	}
	if c.Dashboard.Enabled {
		if c.Dashboard.StatsFreshTTLSeconds <= 0 {
			return fmt.Errorf("dashboard_cache.stats_fresh_ttl_seconds must be positive")
		}
		if c.Dashboard.StatsTTLSeconds <= 0 {
			return fmt.Errorf("dashboard_cache.stats_ttl_seconds must be positive")
		}
		if c.Dashboard.StatsRefreshTimeoutSeconds <= 0 {
			return fmt.Errorf("dashboard_cache.stats_refresh_timeout_seconds must be positive")
		}
		if c.Dashboard.StatsFreshTTLSeconds > c.Dashboard.StatsTTLSeconds {
			return fmt.Errorf("dashboard_cache.stats_fresh_ttl_seconds must be <= dashboard_cache.stats_ttl_seconds")
		}
	} else {
		if c.Dashboard.StatsFreshTTLSeconds < 0 {
			return fmt.Errorf("dashboard_cache.stats_fresh_ttl_seconds must be non-negative")
		}
		if c.Dashboard.StatsTTLSeconds < 0 {
			return fmt.Errorf("dashboard_cache.stats_ttl_seconds must be non-negative")
		}
		if c.Dashboard.StatsRefreshTimeoutSeconds < 0 {
			return fmt.Errorf("dashboard_cache.stats_refresh_timeout_seconds must be non-negative")
		}
	}
	if c.DashboardAgg.Enabled {
		if c.DashboardAgg.IntervalSeconds <= 0 {
			return fmt.Errorf("dashboard_aggregation.interval_seconds must be positive")
		}
		if c.DashboardAgg.LookbackSeconds < 0 {
			return fmt.Errorf("dashboard_aggregation.lookback_seconds must be non-negative")
		}
		if c.DashboardAgg.BackfillMaxDays < 0 {
			return fmt.Errorf("dashboard_aggregation.backfill_max_days must be non-negative")
		}
		if c.DashboardAgg.BackfillEnabled && c.DashboardAgg.BackfillMaxDays == 0 {
			return fmt.Errorf("dashboard_aggregation.backfill_max_days must be positive")
		}
		if c.DashboardAgg.Retention.UsageLogsDays <= 0 {
			return fmt.Errorf("dashboard_aggregation.retention.usage_logs_days must be positive")
		}
		if c.DashboardAgg.Retention.UsageBillingDedupDays <= 0 {
			return fmt.Errorf("dashboard_aggregation.retention.usage_billing_dedup_days must be positive")
		}
		if c.DashboardAgg.Retention.UsageBillingDedupDays < c.DashboardAgg.Retention.UsageLogsDays {
			return fmt.Errorf("dashboard_aggregation.retention.usage_billing_dedup_days must be greater than or equal to usage_logs_days")
		}
		if c.DashboardAgg.Retention.HourlyDays <= 0 {
			return fmt.Errorf("dashboard_aggregation.retention.hourly_days must be positive")
		}
		if c.DashboardAgg.Retention.DailyDays <= 0 {
			return fmt.Errorf("dashboard_aggregation.retention.daily_days must be positive")
		}
		if c.DashboardAgg.RecomputeDays < 0 {
			return fmt.Errorf("dashboard_aggregation.recompute_days must be non-negative")
		}
	} else {
		if c.DashboardAgg.IntervalSeconds < 0 {
			return fmt.Errorf("dashboard_aggregation.interval_seconds must be non-negative")
		}
		if c.DashboardAgg.LookbackSeconds < 0 {
			return fmt.Errorf("dashboard_aggregation.lookback_seconds must be non-negative")
		}
		if c.DashboardAgg.BackfillMaxDays < 0 {
			return fmt.Errorf("dashboard_aggregation.backfill_max_days must be non-negative")
		}
		if c.DashboardAgg.Retention.UsageLogsDays < 0 {
			return fmt.Errorf("dashboard_aggregation.retention.usage_logs_days must be non-negative")
		}
		if c.DashboardAgg.Retention.UsageBillingDedupDays < 0 {
			return fmt.Errorf("dashboard_aggregation.retention.usage_billing_dedup_days must be non-negative")
		}
		if c.DashboardAgg.Retention.UsageBillingDedupDays > 0 &&
			c.DashboardAgg.Retention.UsageLogsDays > 0 &&
			c.DashboardAgg.Retention.UsageBillingDedupDays < c.DashboardAgg.Retention.UsageLogsDays {
			return fmt.Errorf("dashboard_aggregation.retention.usage_billing_dedup_days must be greater than or equal to usage_logs_days")
		}
		if c.DashboardAgg.Retention.HourlyDays < 0 {
			return fmt.Errorf("dashboard_aggregation.retention.hourly_days must be non-negative")
		}
		if c.DashboardAgg.Retention.DailyDays < 0 {
			return fmt.Errorf("dashboard_aggregation.retention.daily_days must be non-negative")
		}
		if c.DashboardAgg.RecomputeDays < 0 {
			return fmt.Errorf("dashboard_aggregation.recompute_days must be non-negative")
		}
	}
	if c.UsageCleanup.Enabled {
		if c.UsageCleanup.MaxRangeDays <= 0 {
			return fmt.Errorf("usage_cleanup.max_range_days must be positive")
		}
		if c.UsageCleanup.BatchSize <= 0 {
			return fmt.Errorf("usage_cleanup.batch_size must be positive")
		}
		if c.UsageCleanup.WorkerIntervalSeconds <= 0 {
			return fmt.Errorf("usage_cleanup.worker_interval_seconds must be positive")
		}
		if c.UsageCleanup.TaskTimeoutSeconds <= 0 {
			return fmt.Errorf("usage_cleanup.task_timeout_seconds must be positive")
		}
	} else {
		if c.UsageCleanup.MaxRangeDays < 0 {
			return fmt.Errorf("usage_cleanup.max_range_days must be non-negative")
		}
		if c.UsageCleanup.BatchSize < 0 {
			return fmt.Errorf("usage_cleanup.batch_size must be non-negative")
		}
		if c.UsageCleanup.WorkerIntervalSeconds < 0 {
			return fmt.Errorf("usage_cleanup.worker_interval_seconds must be non-negative")
		}
		if c.UsageCleanup.TaskTimeoutSeconds < 0 {
			return fmt.Errorf("usage_cleanup.task_timeout_seconds must be non-negative")
		}
	}
	if c.Idempotency.DefaultTTLSeconds <= 0 {
		return fmt.Errorf("idempotency.default_ttl_seconds must be positive")
	}
	if c.Idempotency.SystemOperationTTLSeconds <= 0 {
		return fmt.Errorf("idempotency.system_operation_ttl_seconds must be positive")
	}
	if c.Idempotency.ProcessingTimeoutSeconds <= 0 {
		return fmt.Errorf("idempotency.processing_timeout_seconds must be positive")
	}
	if c.Idempotency.FailedRetryBackoffSeconds <= 0 {
		return fmt.Errorf("idempotency.failed_retry_backoff_seconds must be positive")
	}
	if c.Idempotency.MaxStoredResponseLen <= 0 {
		return fmt.Errorf("idempotency.max_stored_response_len must be positive")
	}
	if c.Idempotency.CleanupIntervalSeconds <= 0 {
		return fmt.Errorf("idempotency.cleanup_interval_seconds must be positive")
	}
	if c.Idempotency.CleanupBatchSize <= 0 {
		return fmt.Errorf("idempotency.cleanup_batch_size must be positive")
	}
	if c.Gateway.MaxBodySize <= 0 {
		return fmt.Errorf("gateway.max_body_size must be positive")
	}
	if c.Gateway.UpstreamResponseReadMaxBytes <= 0 {
		return fmt.Errorf("gateway.upstream_response_read_max_bytes must be positive")
	}
	if c.Gateway.ProxyProbeResponseReadMaxBytes <= 0 {
		return fmt.Errorf("gateway.proxy_probe_response_read_max_bytes must be positive")
	}
	if c.Gateway.ResponseHeaderTimeout < 0 {
		return fmt.Errorf("gateway.response_header_timeout must be non-negative")
	}
	if c.Gateway.OpenAIResponseHeaderTimeout < 0 {
		return fmt.Errorf("gateway.openai_response_header_timeout must be non-negative")
	}
	if strings.TrimSpace(c.Gateway.ConnectionPoolIsolation) != "" {
		switch c.Gateway.ConnectionPoolIsolation {
		case ConnectionPoolIsolationProxy, ConnectionPoolIsolationAccount, ConnectionPoolIsolationAccountProxy:
		default:
			return fmt.Errorf("gateway.connection_pool_isolation must be one of: %s/%s/%s",
				ConnectionPoolIsolationProxy, ConnectionPoolIsolationAccount, ConnectionPoolIsolationAccountProxy)
		}
	}
	if c.Gateway.ImageConcurrency.MaxConcurrentRequests < 0 {
		return fmt.Errorf("gateway.image_concurrency.max_concurrent_requests must be non-negative")
	}
	switch strings.TrimSpace(c.Gateway.ImageConcurrency.OverflowMode) {
	case "", ImageConcurrencyOverflowModeReject, ImageConcurrencyOverflowModeWait:
	default:
		return fmt.Errorf("gateway.image_concurrency.overflow_mode must be one of: %s/%s",
			ImageConcurrencyOverflowModeReject, ImageConcurrencyOverflowModeWait)
	}
	if c.Gateway.ImageConcurrency.WaitTimeoutSeconds < 0 {
		return fmt.Errorf("gateway.image_concurrency.wait_timeout_seconds must be non-negative")
	}
	if c.Gateway.ImageConcurrency.MaxWaitingRequests < 0 {
		return fmt.Errorf("gateway.image_concurrency.max_waiting_requests must be non-negative")
	}
	if c.Gateway.MaxIdleConns <= 0 {
		return fmt.Errorf("gateway.max_idle_conns must be positive")
	}
	if c.Gateway.MaxIdleConnsPerHost <= 0 {
		return fmt.Errorf("gateway.max_idle_conns_per_host must be positive")
	}
	if c.Gateway.MaxConnsPerHost < 0 {
		return fmt.Errorf("gateway.max_conns_per_host must be non-negative")
	}
	if c.Gateway.IdleConnTimeoutSeconds <= 0 {
		return fmt.Errorf("gateway.idle_conn_timeout_seconds must be positive")
	}
	if c.Gateway.IdleConnTimeoutSeconds > 180 {
		slog.Warn("gateway.idle_conn_timeout_seconds is high; consider 60-120 seconds for better connection reuse", "idle_conn_timeout_seconds", c.Gateway.IdleConnTimeoutSeconds)
	}
	if c.Gateway.MaxUpstreamClients <= 0 {
		return fmt.Errorf("gateway.max_upstream_clients must be positive")
	}
	if c.Gateway.ClientIdleTTLSeconds <= 0 {
		return fmt.Errorf("gateway.client_idle_ttl_seconds must be positive")
	}
	if c.Gateway.ConcurrencySlotTTLMinutes <= 0 {
		return fmt.Errorf("gateway.concurrency_slot_ttl_minutes must be positive")
	}
	if c.Gateway.StreamDataIntervalTimeout < 0 {
		return fmt.Errorf("gateway.stream_data_interval_timeout must be non-negative")
	}
	if c.Gateway.StreamDataIntervalTimeout != 0 &&
		(c.Gateway.StreamDataIntervalTimeout < 30 || c.Gateway.StreamDataIntervalTimeout > 300) {
		return fmt.Errorf("gateway.stream_data_interval_timeout must be 0 or between 30-300 seconds")
	}
	if c.Gateway.StreamKeepaliveInterval < 0 {
		return fmt.Errorf("gateway.stream_keepalive_interval must be non-negative")
	}
	if c.Gateway.StreamKeepaliveInterval != 0 &&
		(c.Gateway.StreamKeepaliveInterval < 5 || c.Gateway.StreamKeepaliveInterval > 30) {
		return fmt.Errorf("gateway.stream_keepalive_interval must be 0 or between 5-30 seconds")
	}
	if c.Gateway.ImageStreamDataIntervalTimeout < 0 {
		return fmt.Errorf("gateway.image_stream_data_interval_timeout must be non-negative")
	}
	if c.Gateway.ImageStreamDataIntervalTimeout != 0 &&
		(c.Gateway.ImageStreamDataIntervalTimeout < 60 || c.Gateway.ImageStreamDataIntervalTimeout > 1800) {
		return fmt.Errorf("gateway.image_stream_data_interval_timeout must be 0 or between 60-1800 seconds")
	}
	if c.Gateway.ImageStreamKeepaliveInterval < 0 {
		return fmt.Errorf("gateway.image_stream_keepalive_interval must be non-negative")
	}
	if c.Gateway.ImageStreamKeepaliveInterval != 0 &&
		(c.Gateway.ImageStreamKeepaliveInterval < 5 || c.Gateway.ImageStreamKeepaliveInterval > 60) {
		return fmt.Errorf("gateway.image_stream_keepalive_interval must be 0 or between 5-60 seconds")
	}
	//
	if c.Gateway.OpenAIWS.StickyResponseIDTTLSeconds <= 0 && c.Gateway.OpenAIWS.StickyPreviousResponseTTLSeconds > 0 {
		c.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = c.Gateway.OpenAIWS.StickyPreviousResponseTTLSeconds
	}
	if c.Gateway.OpenAIWS.MaxConnsPerAccount <= 0 {
		return fmt.Errorf("gateway.openai_ws.max_conns_per_account must be positive")
	}
	if c.Gateway.OpenAIWS.MinIdlePerAccount < 0 {
		return fmt.Errorf("gateway.openai_ws.min_idle_per_account must be non-negative")
	}
	if c.Gateway.OpenAIWS.MaxIdlePerAccount < 0 {
		return fmt.Errorf("gateway.openai_ws.max_idle_per_account must be non-negative")
	}
	if c.Gateway.OpenAIWS.MinIdlePerAccount > c.Gateway.OpenAIWS.MaxIdlePerAccount {
		return fmt.Errorf("gateway.openai_ws.min_idle_per_account must be <= max_idle_per_account")
	}
	if c.Gateway.OpenAIWS.MaxIdlePerAccount > c.Gateway.OpenAIWS.MaxConnsPerAccount {
		return fmt.Errorf("gateway.openai_ws.max_idle_per_account must be <= max_conns_per_account")
	}
	if c.Gateway.OpenAIWS.OAuthMaxConnsFactor <= 0 {
		return fmt.Errorf("gateway.openai_ws.oauth_max_conns_factor must be positive")
	}
	if c.Gateway.OpenAIWS.APIKeyMaxConnsFactor <= 0 {
		return fmt.Errorf("gateway.openai_ws.apikey_max_conns_factor must be positive")
	}
	if c.Gateway.OpenAIWS.DialTimeoutSeconds <= 0 {
		return fmt.Errorf("gateway.openai_ws.dial_timeout_seconds must be positive")
	}
	if c.Gateway.OpenAIWS.ReadTimeoutSeconds <= 0 {
		return fmt.Errorf("gateway.openai_ws.read_timeout_seconds must be positive")
	}
	if c.Gateway.OpenAIWS.WriteTimeoutSeconds <= 0 {
		return fmt.Errorf("gateway.openai_ws.write_timeout_seconds must be positive")
	}
	if c.Gateway.OpenAIWS.PoolTargetUtilization <= 0 || c.Gateway.OpenAIWS.PoolTargetUtilization > 1 {
		return fmt.Errorf("gateway.openai_ws.pool_target_utilization must be within (0,1]")
	}
	if c.Gateway.OpenAIWS.QueueLimitPerConn <= 0 {
		return fmt.Errorf("gateway.openai_ws.queue_limit_per_conn must be positive")
	}
	if c.Gateway.OpenAIWS.EventFlushBatchSize <= 0 {
		return fmt.Errorf("gateway.openai_ws.event_flush_batch_size must be positive")
	}
	if c.Gateway.OpenAIWS.EventFlushIntervalMS < 0 {
		return fmt.Errorf("gateway.openai_ws.event_flush_interval_ms must be non-negative")
	}
	if c.Gateway.OpenAIWS.PrewarmCooldownMS < 0 {
		return fmt.Errorf("gateway.openai_ws.prewarm_cooldown_ms must be non-negative")
	}
	if c.Gateway.OpenAIWS.ClientReadLimitBytes <= 0 {
		return fmt.Errorf("gateway.openai_ws.client_read_limit_bytes must be positive")
	}
	if c.Gateway.OpenAIWS.HTTPBridgeThresholdBytes < 0 {
		return fmt.Errorf("gateway.openai_ws.http_bridge_threshold_bytes must be non-negative")
	}
	if c.Gateway.OpenAIWS.HTTPBridgeEnabled && c.Gateway.OpenAIWS.HTTPBridgeThresholdBytes == 0 {
		return fmt.Errorf("gateway.openai_ws.http_bridge_threshold_bytes must be positive when http_bridge_enabled is true")
	}
	if c.Gateway.OpenAIWS.FallbackCooldownSeconds < 0 {
		return fmt.Errorf("gateway.openai_ws.fallback_cooldown_seconds must be non-negative")
	}
	if c.Gateway.OpenAIWS.RetryBackoffInitialMS < 0 {
		return fmt.Errorf("gateway.openai_ws.retry_backoff_initial_ms must be non-negative")
	}
	if c.Gateway.OpenAIWS.RetryBackoffMaxMS < 0 {
		return fmt.Errorf("gateway.openai_ws.retry_backoff_max_ms must be non-negative")
	}
	if c.Gateway.OpenAIWS.RetryBackoffInitialMS > 0 && c.Gateway.OpenAIWS.RetryBackoffMaxMS > 0 &&
		c.Gateway.OpenAIWS.RetryBackoffMaxMS < c.Gateway.OpenAIWS.RetryBackoffInitialMS {
		return fmt.Errorf("gateway.openai_ws.retry_backoff_max_ms must be >= retry_backoff_initial_ms")
	}
	if c.Gateway.OpenAIWS.RetryJitterRatio < 0 || c.Gateway.OpenAIWS.RetryJitterRatio > 1 {
		return fmt.Errorf("gateway.openai_ws.retry_jitter_ratio must be within [0,1]")
	}
	if c.Gateway.OpenAIWS.RetryTotalBudgetMS < 0 {
		return fmt.Errorf("gateway.openai_ws.retry_total_budget_ms must be non-negative")
	}
	if mode := strings.ToLower(strings.TrimSpace(c.Gateway.OpenAIWS.IngressModeDefault)); mode != "" {
		switch mode {
		case "off", "ctx_pool", "passthrough":
		case "shared", "dedicated":
			slog.Warn("gateway.openai_ws.ingress_mode_default is deprecated, treating as ctx_pool; please update to off|ctx_pool|passthrough", "value", mode)
		default:
			return fmt.Errorf("gateway.openai_ws.ingress_mode_default must be one of off|ctx_pool|passthrough")
		}
	}
	if mode := strings.ToLower(strings.TrimSpace(c.Gateway.OpenAIWS.StoreDisabledConnMode)); mode != "" {
		switch mode {
		case "strict", "adaptive", "off":
		default:
			return fmt.Errorf("gateway.openai_ws.store_disabled_conn_mode must be one of strict|adaptive|off")
		}
	}
	if c.Gateway.OpenAIWS.PayloadLogSampleRate < 0 || c.Gateway.OpenAIWS.PayloadLogSampleRate > 1 {
		return fmt.Errorf("gateway.openai_ws.payload_log_sample_rate must be within [0,1]")
	}
	if c.Gateway.OpenAIWS.LBTopK <= 0 {
		return fmt.Errorf("gateway.openai_ws.lb_top_k must be positive")
	}
	if c.Gateway.OpenAIWS.StickySessionTTLSeconds <= 0 {
		return fmt.Errorf("gateway.openai_ws.sticky_session_ttl_seconds must be positive")
	}
	if c.Gateway.OpenAIWS.StickyResponseIDTTLSeconds <= 0 {
		return fmt.Errorf("gateway.openai_ws.sticky_response_id_ttl_seconds must be positive")
	}
	if c.Gateway.OpenAIWS.StickyPreviousResponseTTLSeconds < 0 {
		return fmt.Errorf("gateway.openai_ws.sticky_previous_response_ttl_seconds must be non-negative")
	}
	if c.Gateway.OpenAIHTTP2.FallbackErrorThreshold < 0 {
		return fmt.Errorf("gateway.openai_http2.fallback_error_threshold must be non-negative")
	}
	if c.Gateway.OpenAIHTTP2.FallbackWindowSeconds < 0 {
		return fmt.Errorf("gateway.openai_http2.fallback_window_seconds must be non-negative")
	}
	if c.Gateway.OpenAIHTTP2.FallbackTTLSeconds < 0 {
		return fmt.Errorf("gateway.openai_http2.fallback_ttl_seconds must be non-negative")
	}
	if c.Gateway.OpenAIWS.SchedulerScoreWeights.Priority < 0 ||
		c.Gateway.OpenAIWS.SchedulerScoreWeights.Load < 0 ||
		c.Gateway.OpenAIWS.SchedulerScoreWeights.Queue < 0 ||
		c.Gateway.OpenAIWS.SchedulerScoreWeights.ErrorRate < 0 ||
		c.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT < 0 {
		return fmt.Errorf("gateway.openai_ws.scheduler_score_weights.* must be non-negative")
	}
	weightSum := c.Gateway.OpenAIWS.SchedulerScoreWeights.Priority +
		c.Gateway.OpenAIWS.SchedulerScoreWeights.Load +
		c.Gateway.OpenAIWS.SchedulerScoreWeights.Queue +
		c.Gateway.OpenAIWS.SchedulerScoreWeights.ErrorRate +
		c.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT
	if weightSum <= 0 {
		return fmt.Errorf("gateway.openai_ws.scheduler_score_weights must not all be zero")
	}
	if c.Gateway.OpenAIScheduler.StickyEscapeTTFTMs <= 0 {
		return fmt.Errorf("gateway.openai_scheduler.sticky_escape_ttft_ms must be positive")
	}
	if c.Gateway.OpenAIScheduler.StickyEscapeErrorRate < 0 || c.Gateway.OpenAIScheduler.StickyEscapeErrorRate > 1 {
		return fmt.Errorf("gateway.openai_scheduler.sticky_escape_error_rate must be between 0 and 1")
	}
	if c.Gateway.MaxLineSize < 0 {
		return fmt.Errorf("gateway.max_line_size must be non-negative")
	}
	if c.Gateway.MaxLineSize != 0 && c.Gateway.MaxLineSize < 1024*1024 {
		return fmt.Errorf("gateway.max_line_size must be at least 1MB")
	}
	if c.Gateway.UsageRecord.WorkerCount <= 0 {
		return fmt.Errorf("gateway.usage_record.worker_count must be positive")
	}
	if c.Gateway.UsageRecord.QueueSize <= 0 {
		return fmt.Errorf("gateway.usage_record.queue_size must be positive")
	}
	if c.Gateway.UsageRecord.TaskTimeoutSeconds <= 0 {
		return fmt.Errorf("gateway.usage_record.task_timeout_seconds must be positive")
	}
	switch strings.ToLower(strings.TrimSpace(c.Gateway.UsageRecord.OverflowPolicy)) {
	case UsageRecordOverflowPolicyDrop, UsageRecordOverflowPolicySample, UsageRecordOverflowPolicySync:
	default:
		return fmt.Errorf("gateway.usage_record.overflow_policy must be one of: %s/%s/%s",
			UsageRecordOverflowPolicyDrop, UsageRecordOverflowPolicySample, UsageRecordOverflowPolicySync)
	}
	if c.Gateway.UsageRecord.OverflowSamplePercent < 0 || c.Gateway.UsageRecord.OverflowSamplePercent > 100 {
		return fmt.Errorf("gateway.usage_record.overflow_sample_percent must be between 0-100")
	}
	if strings.EqualFold(strings.TrimSpace(c.Gateway.UsageRecord.OverflowPolicy), UsageRecordOverflowPolicySample) &&
		c.Gateway.UsageRecord.OverflowSamplePercent <= 0 {
		return fmt.Errorf("gateway.usage_record.overflow_sample_percent must be positive when overflow_policy=sample")
	}
	if c.Gateway.UsageRecord.AutoScaleEnabled {
		if c.Gateway.UsageRecord.AutoScaleMinWorkers <= 0 {
			return fmt.Errorf("gateway.usage_record.auto_scale_min_workers must be positive")
		}
		if c.Gateway.UsageRecord.AutoScaleMaxWorkers <= 0 {
			return fmt.Errorf("gateway.usage_record.auto_scale_max_workers must be positive")
		}
		if c.Gateway.UsageRecord.AutoScaleMaxWorkers < c.Gateway.UsageRecord.AutoScaleMinWorkers {
			return fmt.Errorf("gateway.usage_record.auto_scale_max_workers must be >= auto_scale_min_workers")
		}
		if c.Gateway.UsageRecord.WorkerCount < c.Gateway.UsageRecord.AutoScaleMinWorkers ||
			c.Gateway.UsageRecord.WorkerCount > c.Gateway.UsageRecord.AutoScaleMaxWorkers {
			return fmt.Errorf("gateway.usage_record.worker_count must be between auto_scale_min_workers and auto_scale_max_workers")
		}
		if c.Gateway.UsageRecord.AutoScaleUpQueuePercent <= 0 || c.Gateway.UsageRecord.AutoScaleUpQueuePercent > 100 {
			return fmt.Errorf("gateway.usage_record.auto_scale_up_queue_percent must be between 1-100")
		}
		if c.Gateway.UsageRecord.AutoScaleDownQueuePercent < 0 || c.Gateway.UsageRecord.AutoScaleDownQueuePercent >= 100 {
			return fmt.Errorf("gateway.usage_record.auto_scale_down_queue_percent must be between 0-99")
		}
		if c.Gateway.UsageRecord.AutoScaleDownQueuePercent >= c.Gateway.UsageRecord.AutoScaleUpQueuePercent {
			return fmt.Errorf("gateway.usage_record.auto_scale_down_queue_percent must be less than auto_scale_up_queue_percent")
		}
		if c.Gateway.UsageRecord.AutoScaleUpStep <= 0 {
			return fmt.Errorf("gateway.usage_record.auto_scale_up_step must be positive")
		}
		if c.Gateway.UsageRecord.AutoScaleDownStep <= 0 {
			return fmt.Errorf("gateway.usage_record.auto_scale_down_step must be positive")
		}
		if c.Gateway.UsageRecord.AutoScaleCheckIntervalSeconds <= 0 {
			return fmt.Errorf("gateway.usage_record.auto_scale_check_interval_seconds must be positive")
		}
		if c.Gateway.UsageRecord.AutoScaleCooldownSeconds < 0 {
			return fmt.Errorf("gateway.usage_record.auto_scale_cooldown_seconds must be non-negative")
		}
	}
	if c.Gateway.UserGroupRateCacheTTLSeconds <= 0 {
		return fmt.Errorf("gateway.user_group_rate_cache_ttl_seconds must be positive")
	}
	if c.Gateway.ModelsListCacheTTLSeconds < 10 || c.Gateway.ModelsListCacheTTLSeconds > 30 {
		return fmt.Errorf("gateway.models_list_cache_ttl_seconds must be between 10-30")
	}
	if c.Gateway.Scheduling.StickySessionMaxWaiting <= 0 {
		return fmt.Errorf("gateway.scheduling.sticky_session_max_waiting must be positive")
	}
	if c.Gateway.Scheduling.StickySessionWaitTimeout <= 0 {
		return fmt.Errorf("gateway.scheduling.sticky_session_wait_timeout must be positive")
	}
	if c.Gateway.Scheduling.FallbackWaitTimeout <= 0 {
		return fmt.Errorf("gateway.scheduling.fallback_wait_timeout must be positive")
	}
	if c.Gateway.Scheduling.FallbackMaxWaiting <= 0 {
		return fmt.Errorf("gateway.scheduling.fallback_max_waiting must be positive")
	}
	if c.Gateway.Scheduling.LoadBatchCacheTTLMS < 0 {
		return fmt.Errorf("gateway.scheduling.load_batch_cache_ttl_ms must be non-negative")
	}
	if c.Gateway.Scheduling.SnapshotMGetChunkSize <= 0 {
		return fmt.Errorf("gateway.scheduling.snapshot_mget_chunk_size must be positive")
	}
	if c.Gateway.Scheduling.SnapshotWriteChunkSize <= 0 {
		return fmt.Errorf("gateway.scheduling.snapshot_write_chunk_size must be positive")
	}
	if c.Gateway.Scheduling.SlotCleanupInterval < 0 {
		return fmt.Errorf("gateway.scheduling.slot_cleanup_interval must be non-negative")
	}
	if c.Gateway.Scheduling.DbFallbackTimeoutSeconds < 0 {
		return fmt.Errorf("gateway.scheduling.db_fallback_timeout_seconds must be non-negative")
	}
	if c.Gateway.Scheduling.DbFallbackMaxQPS < 0 {
		return fmt.Errorf("gateway.scheduling.db_fallback_max_qps must be non-negative")
	}
	if c.Gateway.Scheduling.OutboxPollIntervalSeconds <= 0 {
		return fmt.Errorf("gateway.scheduling.outbox_poll_interval_seconds must be positive")
	}
	if c.Gateway.Scheduling.OutboxLagWarnSeconds < 0 {
		return fmt.Errorf("gateway.scheduling.outbox_lag_warn_seconds must be non-negative")
	}
	if c.Gateway.Scheduling.OutboxLagRebuildSeconds < 0 {
		return fmt.Errorf("gateway.scheduling.outbox_lag_rebuild_seconds must be non-negative")
	}
	if c.Gateway.Scheduling.OutboxLagRebuildFailures <= 0 {
		return fmt.Errorf("gateway.scheduling.outbox_lag_rebuild_failures must be positive")
	}
	if c.Gateway.Scheduling.OutboxBacklogRebuildRows < 0 {
		return fmt.Errorf("gateway.scheduling.outbox_backlog_rebuild_rows must be non-negative")
	}
	if c.Gateway.Scheduling.FullRebuildIntervalSeconds < 0 {
		return fmt.Errorf("gateway.scheduling.full_rebuild_interval_seconds must be non-negative")
	}
	if c.Gateway.Scheduling.OutboxLagWarnSeconds > 0 &&
		c.Gateway.Scheduling.OutboxLagRebuildSeconds > 0 &&
		c.Gateway.Scheduling.OutboxLagRebuildSeconds < c.Gateway.Scheduling.OutboxLagWarnSeconds {
		return fmt.Errorf("gateway.scheduling.outbox_lag_rebuild_seconds must be >= outbox_lag_warn_seconds")
	}
	if c.Ops.MetricsCollectorCache.TTL < 0 {
		return fmt.Errorf("ops.metrics_collector_cache.ttl must be non-negative")
	}
	if c.Ops.Cleanup.ErrorLogRetentionDays < 0 {
		return fmt.Errorf("ops.cleanup.error_log_retention_days must be non-negative")
	}
	if c.Ops.Cleanup.MinuteMetricsRetentionDays < 0 {
		return fmt.Errorf("ops.cleanup.minute_metrics_retention_days must be non-negative")
	}
	if c.Ops.Cleanup.HourlyMetricsRetentionDays < 0 {
		return fmt.Errorf("ops.cleanup.hourly_metrics_retention_days must be non-negative")
	}
	if c.Ops.Cleanup.Enabled && strings.TrimSpace(c.Ops.Cleanup.Schedule) == "" {
		return fmt.Errorf("ops.cleanup.schedule is required when ops.cleanup.enabled=true")
	}
	if c.Concurrency.PingInterval < 5 || c.Concurrency.PingInterval > 30 {
		return fmt.Errorf("concurrency.ping_interval must be between 5-30 seconds")
	}
	if err := ValidateDingTalkConfig(c.DingTalk); err != nil {
		return fmt.Errorf("dingtalk_connect: %w", err)
	}
	return nil
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return values
	}
	normalized := make([]string, 0, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func isWeakJWTSecret(secret string) bool {
	lower := strings.ToLower(strings.TrimSpace(secret))
	if lower == "" {
		return true
	}
	weak := map[string]struct{}{
		"change-me-in-production": {},
		"changeme":                {},
		"secret":                  {},
		"password":                {},
		"123456":                  {},
		"12345678":                {},
		"admin":                   {},
		"jwt-secret":              {},
	}
	_, exists := weak[lower]
	return exists
}

func generateJWTSecret(byteLength int) (string, error) {
	if byteLength <= 0 {
		byteLength = 32
	}
	buf := make([]byte, byteLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// GetServerAddress returns the server address (host:port) from config file or environment variable.
// This is a lightweight function that can be used before full config validation,
// such as during setup wizard startup.
// Priority: config.yaml > environment variables > defaults
func GetServerAddress() string {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/sub2api")

	// Support SERVER_HOST and SERVER_PORT environment variables
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)

	// Try to read config file (ignore errors if not found)
	_ = v.ReadInConfig()

	host := v.GetString("server.host")
	port := v.GetInt("server.port")
	return fmt.Sprintf("%s:%d", host, port)
}

// ValidateAbsoluteHTTPURL (S) URL
func ValidateAbsoluteHTTPURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if !u.IsAbs() {
		return fmt.Errorf("must be absolute")
	}
	if !isHTTPScheme(u.Scheme) {
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("missing host")
	}
	if u.Fragment != "" {
		return fmt.Errorf("must not include fragment")
	}
	return nil
}

// ValidateFrontendRedirectURL
func ValidateFrontendRedirectURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty url")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return fmt.Errorf("contains invalid characters")
	}
	if strings.HasPrefix(raw, "/") {
		if strings.HasPrefix(raw, "//") {
			return fmt.Errorf("must not start with //")
		}
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if !u.IsAbs() {
		return fmt.Errorf("must be absolute http(s) url or relative path")
	}
	if !isHTTPScheme(u.Scheme) {
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("missing host")
	}
	if u.Fragment != "" {
		return fmt.Errorf("must not include fragment")
	}
	return nil
}

func scopeContainsOpenID(scopes string) bool {
	for _, scope := range strings.Fields(strings.ToLower(strings.TrimSpace(scopes))) {
		if scope == "openid" {
			return true
		}
	}
	return false
}

// isHTTPScheme
func isHTTPScheme(scheme string) bool {
	return strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")
}

func warnIfInsecureURL(field, raw string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return
	}
	if strings.EqualFold(u.Scheme, "http") {
		slog.Warn("url uses http scheme; use https in production to avoid token leakage", "field", field)
	}
}
