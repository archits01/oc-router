package service

import "time"

// APIKeyAuthSnapshot API Key
type APIKeyAuthSnapshot struct {
	Version     int                      `json:"version"`
	APIKeyID    int64                    `json:"api_key_id"`
	UserID      int64                    `json:"user_id"`
	GroupID     *int64                   `json:"group_id,omitempty"`
	Name        string                   `json:"name"`
	Status      string                   `json:"status"`
	IPWhitelist []string                 `json:"ip_whitelist,omitempty"`
	IPBlacklist []string                 `json:"ip_blacklist,omitempty"`
	User        APIKeyAuthUserSnapshot   `json:"user"`
	Group       *APIKeyAuthGroupSnapshot `json:"group,omitempty"`

	// Quota fields for API Key independent quota feature
	Quota     float64 `json:"quota"`      // Quota limit in USD (0 = unlimited)
	QuotaUsed float64 `json:"quota_used"` // Used quota amount

	// Expiration field for API Key expiration feature
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // Expiration time (nil = never expires)

	// Rate limit configuration (only limits, not usage - usage read from Redis at check time)
	RateLimit5h float64 `json:"rate_limit_5h"`
	RateLimit1d float64 `json:"rate_limit_1d"`
	RateLimit7d float64 `json:"rate_limit_7d"`
}

// APIKeyAuthUserSnapshot
type APIKeyAuthUserSnapshot struct {
	ID            int64   `json:"id"`
	Status        string  `json:"status"`
	Role          string  `json:"role"`
	Balance       float64 `json:"balance"`
	Concurrency   int     `json:"concurrency"`
	AllowedGroups []int64 `json:"allowed_groups,omitempty"`

	// Balance notification fields (required for CheckBalanceAfterDeduction)
	Email                      string             `json:"email"`
	Username                   string             `json:"username"`
	BalanceNotifyEnabled       bool               `json:"balance_notify_enabled"`
	BalanceNotifyThresholdType string             `json:"balance_notify_threshold_type"`
	BalanceNotifyThreshold     *float64           `json:"balance_notify_threshold,omitempty"`
	BalanceNotifyExtraEmails   []NotifyEmailEntry `json:"balance_notify_extra_emails,omitempty"`
	TotalRecharged             float64            `json:"total_recharged"`

	// RPMLimit =
	RPMLimit int `json:"rpm_limit"`

	// UserGroupRPMOverride (user, group)
	// nil = = >0 =
	UserGroupRPMOverride *int `json:"user_group_rpm_override,omitempty"`
}

// APIKeyAuthGroupSnapshot
type APIKeyAuthGroupSnapshot struct {
	ID                              int64    `json:"id"`
	Name                            string   `json:"name"`
	Platform                        string   `json:"platform"`
	IsExclusive                     bool     `json:"is_exclusive"`
	Status                          string   `json:"status"`
	SubscriptionType                string   `json:"subscription_type"`
	RateMultiplier                  float64  `json:"rate_multiplier"`
	DailyLimitUSD                   *float64 `json:"daily_limit_usd,omitempty"`
	WeeklyLimitUSD                  *float64 `json:"weekly_limit_usd,omitempty"`
	MonthlyLimitUSD                 *float64 `json:"monthly_limit_usd,omitempty"`
	AllowImageGeneration            bool     `json:"allow_image_generation"`
	ImageRateIndependent            bool     `json:"image_rate_independent"`
	ImageRateMultiplier             float64  `json:"image_rate_multiplier"`
	ImagePrice1K                    *float64 `json:"image_price_1k,omitempty"`
	ImagePrice2K                    *float64 `json:"image_price_2k,omitempty"`
	ImagePrice4K                    *float64 `json:"image_price_4k,omitempty"`
	ClaudeCodeOnly                  bool     `json:"claude_code_only"`
	FallbackGroupID                 *int64   `json:"fallback_group_id,omitempty"`
	FallbackGroupIDOnInvalidRequest *int64   `json:"fallback_group_id_on_invalid_request,omitempty"`

	// Model routing is used by gateway account selection, so it must be part of auth cache snapshot.
	// Only anthropic groups use these fields; others may leave them empty.
	ModelRouting        map[string][]int64 `json:"model_routing,omitempty"`
	ModelRoutingEnabled bool               `json:"model_routing_enabled"`
	MCPXMLInject        bool               `json:"mcp_xml_inject"`

	//
	SupportedModelScopes []string `json:"supported_model_scopes,omitempty"`

	// OpenAI Messages
	AllowMessagesDispatch       bool                              `json:"allow_messages_dispatch"`
	DefaultMappedModel          string                            `json:"default_mapped_model,omitempty"`
	MessagesDispatchModelConfig OpenAIMessagesDispatchModelConfig `json:"messages_dispatch_model_config,omitempty"`
	ModelsListConfig            GroupModelsListConfig             `json:"models_list_config,omitempty"`

	// RPMLimit =
	RPMLimit int `json:"rpm_limit"`
}

// APIKeyAuthCacheEntry
type APIKeyAuthCacheEntry struct {
	NotFound bool                `json:"not_found"`
	Snapshot *APIKeyAuthSnapshot `json:"snapshot,omitempty"`
}
