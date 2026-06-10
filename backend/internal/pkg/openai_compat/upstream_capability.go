// Package openai_compat
//
//
// （DeepSeek、Kimi、GLM、Qwen
// →Responses
// /v1/responses，
//
// internal/service/openai_apikey_responses_probe.go
//
//   - ——
//     pensieve/short-term/knowledge/upstream-capability-detection-design-tradeoffs）
//   - ""），
//     pensieve/short-term/maxims/preserve-existing-runtime-behavior-when-replacing-logic-in-stateful-systems）
package openai_compat

// AccountResponsesSupport
//
// =openai + type=apikey
type AccountResponsesSupport int

const (
	// ResponsesSupportUnknown
	// ""
	ResponsesSupportUnknown AccountResponsesSupport = iota

	// ResponsesSupportYes
	ResponsesSupportYes

	// ResponsesSupportNo
	// /v1/chat/completions
	ResponsesSupportNo
)

// ResponsesSupportMode
type ResponsesSupportMode string

const (
	// ResponsesSupportModeAuto
	ResponsesSupportModeAuto ResponsesSupportMode = "auto"

	// ResponsesSupportModeForceResponses
	ResponsesSupportModeForceResponses ResponsesSupportMode = "force_responses"

	// ResponsesSupportModeForceChatCompletions
	ResponsesSupportModeForceChatCompletions ResponsesSupportMode = "force_chat_completions"
)

// ExtraKeyResponsesMode
// ==
// force_chat_completions=
const ExtraKeyResponsesMode = "openai_responses_mode"

// ExtraKeyResponsesSupported
// ===
const ExtraKeyResponsesSupported = "openai_responses_supported"

// NormalizeResponsesSupportMode
//
func NormalizeResponsesSupportMode(mode string) ResponsesSupportMode {
	switch ResponsesSupportMode(mode) {
	case ResponsesSupportModeForceResponses:
		return ResponsesSupportModeForceResponses
	case ResponsesSupportModeForceChatCompletions:
		return ResponsesSupportModeForceChatCompletions
	default:
		return ResponsesSupportModeAuto
	}
}

// ResolveResponsesSupport
//
// ——
// "=="
func ResolveResponsesSupport(extra map[string]any) AccountResponsesSupport {
	if extra == nil {
		return ResponsesSupportUnknown
	}
	if mode, ok := extra[ExtraKeyResponsesMode].(string); ok {
		switch NormalizeResponsesSupportMode(mode) {
		case ResponsesSupportModeForceResponses:
			return ResponsesSupportYes
		case ResponsesSupportModeForceChatCompletions:
			return ResponsesSupportNo
		}
	}
	v, ok := extra[ExtraKeyResponsesSupported]
	if !ok {
		return ResponsesSupportUnknown
	}
	supported, ok := v.(bool)
	if !ok {
		return ResponsesSupportUnknown
	}
	if supported {
		return ResponsesSupportYes
	}
	return ResponsesSupportNo
}

// ShouldUseResponsesAPI
// "CC→Responses + "
//
//
//  1.
//  2. ——""
//
//
// （
func ShouldUseResponsesAPI(extra map[string]any) bool {
	return ResolveResponsesSupport(extra) != ResponsesSupportNo
}
