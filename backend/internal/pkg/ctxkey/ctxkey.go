// Package ctxkey
package ctxkey

// Key
type Key string

const (
	// ForcePlatform
	ForcePlatform Key = "ctx_force_platform"

	// RequestID
	RequestID Key = "ctx_request_id"

	// ClientRequestID
	ClientRequestID Key = "ctx_client_request_id"

	// Model
	Model Key = "ctx_model"

	// Platform
	Platform Key = "ctx_platform"

	// AccountID
	AccountID Key = "ctx_account_id"

	// RetryCount
	RetryCount Key = "ctx_retry_count"

	// AccountSwitchCount
	AccountSwitchCount Key = "ctx_account_switch_count"

	// IsClaudeCodeClient
	IsClaudeCodeClient Key = "ctx_is_claude_code_client"

	// ThinkingEnabled
	ThinkingEnabled Key = "ctx_thinking_enabled"

	// OpenAIImageGenerationIntent
	OpenAIImageGenerationIntent Key = "ctx_openai_image_generation_intent"

	// Group
	Group Key = "ctx_group"

	// IsMaxTokensOneHaikuRequest =1 + haiku
	//
	IsMaxTokensOneHaikuRequest Key = "ctx_is_max_tokens_one_haiku"

	// SingleAccountRetry
	//
	SingleAccountRetry Key = "ctx_single_account_retry"

	// PrefetchedStickyAccountID
	// Service
	PrefetchedStickyAccountID Key = "ctx_prefetched_sticky_account_id"

	// PrefetchedStickyGroupID
	// Service
	PrefetchedStickyGroupID Key = "ctx_prefetched_sticky_group_id"

	// ClaudeCodeVersion stores the extracted Claude Code version from User-Agent (e.g. "2.1.22")
	ClaudeCodeVersion Key = "ctx_claude_code_version"
)
