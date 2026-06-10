package service

import (
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"time"
)

// ChannelMonitorRequestTemplate
// +
type ChannelMonitorRequestTemplate struct {
	ID               int64
	Name             string
	Provider         string
	APIMode          string
	Description      string
	ExtraHeaders     map[string]string
	BodyOverrideMode string
	BodyOverride     map[string]any
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ChannelMonitorRequestTemplateListParams
type ChannelMonitorRequestTemplateListParams struct {
	Provider string // 空 = 全部；非空则按 provider 过滤
	APIMode  string // 空 = 全部；非空则按 api_mode 过滤
}

// ChannelMonitorRequestTemplateCreateParams
type ChannelMonitorRequestTemplateCreateParams struct {
	Name             string
	Provider         string
	APIMode          string
	Description      string
	ExtraHeaders     map[string]string
	BodyOverrideMode string
	BodyOverride     map[string]any
}

// ChannelMonitorRequestTemplateUpdateParams =
//
type ChannelMonitorRequestTemplateUpdateParams struct {
	Name             *string
	APIMode          *string
	Description      *string
	ExtraHeaders     *map[string]string
	BodyOverrideMode *string
	BodyOverride     *map[string]any
}

// *
var (
	ErrChannelMonitorTemplateNotFound = infraerrors.NotFound(
		"CHANNEL_MONITOR_TEMPLATE_NOT_FOUND", "channel monitor request template not found",
	)
	ErrChannelMonitorTemplateInvalidProvider = infraerrors.BadRequest(
		"CHANNEL_MONITOR_TEMPLATE_INVALID_PROVIDER", "template provider must be one of openai/anthropic/gemini",
	)
	ErrChannelMonitorTemplateInvalidAPIMode = infraerrors.BadRequest(
		"CHANNEL_MONITOR_TEMPLATE_INVALID_API_MODE", "template api_mode must be chat_completions or responses; responses is only supported for openai",
	)
	ErrChannelMonitorTemplateMissingName = infraerrors.BadRequest(
		"CHANNEL_MONITOR_TEMPLATE_MISSING_NAME", "template name is required",
	)
	ErrChannelMonitorTemplateInvalidBodyMode = infraerrors.BadRequest(
		"CHANNEL_MONITOR_TEMPLATE_INVALID_BODY_MODE", "body_override_mode must be one of off/merge/replace",
	)
	ErrChannelMonitorTemplateBodyRequired = infraerrors.BadRequest(
		"CHANNEL_MONITOR_TEMPLATE_BODY_REQUIRED", "body_override is required when body_override_mode is merge or replace",
	)
	ErrChannelMonitorTemplateHeaderForbidden = infraerrors.BadRequest(
		"CHANNEL_MONITOR_TEMPLATE_HEADER_FORBIDDEN", "header name is forbidden (hop-by-hop or computed by HTTP client)",
	)
	ErrChannelMonitorTemplateHeaderInvalidName = infraerrors.BadRequest(
		"CHANNEL_MONITOR_TEMPLATE_HEADER_INVALID_NAME", "header name contains invalid characters",
	)
	ErrChannelMonitorTemplateProviderMismatch = infraerrors.BadRequest(
		"CHANNEL_MONITOR_TEMPLATE_PROVIDER_MISMATCH", "monitor provider does not match template provider",
	)
	ErrChannelMonitorTemplateAPIModeMismatch = infraerrors.BadRequest(
		"CHANNEL_MONITOR_TEMPLATE_API_MODE_MISMATCH", "monitor api_mode does not match template api_mode",
	)
	ErrChannelMonitorTemplateApplyEmpty = infraerrors.BadRequest(
		"CHANNEL_MONITOR_TEMPLATE_APPLY_EMPTY", "monitor_ids must be a non-empty array",
	)
)
