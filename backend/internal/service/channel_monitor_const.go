package service

import (
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ChannelMonitor
//
const (
	// monitorRequestTimeout
	monitorRequestTimeout = 45 * time.Second
	// monitorPingTimeout HEAD
	monitorPingTimeout = 8 * time.Second
	// monitorDegradedThreshold
	monitorDegradedThreshold = 6 * time.Second
	// monitorHistoryRetentionDays
	// 60s * 30 ≈ 43200 <= 2M
	//
	monitorHistoryRetentionDays = 30
	// monitorRollupRetentionDays
	//
	monitorRollupRetentionDays = 30
	// monitorMaintenanceMaxDaysPerRun
	// +
	monitorMaintenanceMaxDaysPerRun = 35
	// monitorWorkerConcurrency
	monitorWorkerConcurrency = 5
	// monitorStartupLoadTimeout Start
	monitorStartupLoadTimeout = 10 * time.Second
	// monitorMinIntervalSeconds / monitorMaxIntervalSeconds
	monitorMinIntervalSeconds = 15
	monitorMaxIntervalSeconds = 3600
	// monitorMessageMaxBytes message
	monitorMessageMaxBytes = 500
	// monitorResponseMaxBytes
	monitorResponseMaxBytes = 64 * 1024
	// monitorErrorBodySnippetMaxBytes
	// `{"error":{"message":"..."}}`），
	// "upstream HTTP <status>: " (500)
	monitorErrorBodySnippetMaxBytes = 300
	// monitorChallengeMin / monitorChallengeMax challenge
	monitorChallengeMin = 1
	monitorChallengeMax = 50

	// providerOpenAIPath OpenAI Chat Completions
	providerOpenAIPath = "/v1/chat/completions"
	// providerOpenAIResponsesPath OpenAI Responses API
	providerOpenAIResponsesPath = "/v1/responses"
	// providerAnthropicPath Anthropic Messages
	providerAnthropicPath = "/v1/messages"
	// providerGeminiPathTemplate Gemini generateContent
	providerGeminiPathTemplate = "/v1beta/models/%s:generateContent"

	// MonitorProviderOpenAI / Anthropic / Gemini provider
	MonitorProviderOpenAI    = "openai"
	MonitorProviderAnthropic = "anthropic"
	MonitorProviderGemini    = "gemini"

	// MonitorStatusOperational
	MonitorStatusOperational = "operational"
	MonitorStatusDegraded    = "degraded"
	MonitorStatusFailed      = "failed"
	MonitorStatusError       = "error"

	// monitorAvailability7Days / 15 / 30
	monitorAvailability7Days  = 7
	monitorAvailability15Days = 15
	monitorAvailability30Days = 30

	// MonitorHistoryDefaultLimit
	MonitorHistoryDefaultLimit = 100
	// MonitorHistoryMaxLimit
	MonitorHistoryMaxLimit = 1000

	// monitorTimelineMaxPoints
	monitorTimelineMaxPoints = 60

	// monitorEndpointResolveTimeout validateEndpoint
	monitorEndpointResolveTimeout = 5 * time.Second

	// ---- checker / runner

	// monitorAnthropicAPIVersion Anthropic Messages API
	monitorAnthropicAPIVersion = "2023-06-01"
	// monitorChallengeMaxTokens
	monitorChallengeMaxTokens = 50

	// monitorRunOneBuffer runOne
	monitorRunOneBuffer = 10 * time.Second

	// monitorIdleConnTimeout HTTP transport
	monitorIdleConnTimeout = 30 * time.Second
	// monitorTLSHandshakeTimeout HTTP transport TLS
	monitorTLSHandshakeTimeout = 10 * time.Second
	// monitorResponseHeaderTimeout HTTP transport
	monitorResponseHeaderTimeout = 30 * time.Second
	// monitorPingDiscardMaxBytes ping
	monitorPingDiscardMaxBytes = 1024

	// monitorDialTimeout
	monitorDialTimeout = 10 * time.Second
	// monitorDialKeepAlive
	monitorDialKeepAlive = 30 * time.Second
)

var (
	ErrChannelMonitorNotFound = infraerrors.NotFound(
		"CHANNEL_MONITOR_NOT_FOUND", "channel monitor not found",
	)
	ErrChannelMonitorInvalidProvider = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_PROVIDER", "provider must be one of openai/anthropic/gemini",
	)
	ErrChannelMonitorInvalidAPIMode = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_API_MODE", "api_mode must be chat_completions or responses; responses is only supported for openai",
	)
	ErrChannelMonitorInvalidRequestBody = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_REQUEST_BODY", "openai replace-mode body_override must include non-empty messages for chat_completions or non-empty instructions and input for responses",
	)
	ErrChannelMonitorInvalidInterval = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_INTERVAL", "interval_seconds must be in [15, 3600]",
	)
	ErrChannelMonitorInvalidEndpoint = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_ENDPOINT", "endpoint must be a valid https URL",
	)
	ErrChannelMonitorEndpointScheme = infraerrors.BadRequest(
		"CHANNEL_MONITOR_ENDPOINT_SCHEME", "endpoint must use https scheme",
	)
	ErrChannelMonitorEndpointPath = infraerrors.BadRequest(
		"CHANNEL_MONITOR_ENDPOINT_PATH", "endpoint must be base origin only (no path/query/fragment)",
	)
	ErrChannelMonitorEndpointPrivate = infraerrors.BadRequest(
		"CHANNEL_MONITOR_ENDPOINT_PRIVATE", "endpoint must be a public host",
	)
	ErrChannelMonitorEndpointUnreachable = infraerrors.BadRequest(
		"CHANNEL_MONITOR_ENDPOINT_UNREACHABLE", "endpoint hostname could not be resolved",
	)
	ErrChannelMonitorMissingAPIKey = infraerrors.BadRequest(
		"CHANNEL_MONITOR_MISSING_API_KEY", "api_key is required when creating a monitor",
	)
	ErrChannelMonitorMissingPrimaryModel = infraerrors.BadRequest(
		"CHANNEL_MONITOR_MISSING_PRIMARY_MODEL", "primary_model is required",
	)
	ErrChannelMonitorAPIKeyDecryptFailed = infraerrors.InternalServer(
		"CHANNEL_MONITOR_KEY_DECRYPT_FAILED", "api key decryption failed; please re-edit the monitor with a fresh key",
	)
)
