// Package model
package model

import "time"

// ErrorPassthroughRule
type ErrorPassthroughRule struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`             // rule name
	Enabled         bool      `json:"enabled"`          // whether enabled
	Priority        int       `json:"priority"`         // priority (lower number = higher priority)
	ErrorCodes      []int     `json:"error_codes"`      // list of error codes to match (OR relation)
	Keywords        []string  `json:"keywords"`         // list of keywords to match (OR relation)
	MatchMode       string    `json:"match_mode"`       // "any" (any condition) or "all" (all conditions)
	Platforms       []string  `json:"platforms"`        // applicable platform list
	PassthroughCode bool      `json:"passthrough_code"` // whether to pass through the original status code
	ResponseCode    *int      `json:"response_code"`    // custom status code (used when passthrough_code=false)
	PassthroughBody bool      `json:"passthrough_body"` // whether to pass through the original error info
	CustomMessage   *string   `json:"custom_message"`   // custom error message (used when passthrough_body=false)
	SkipMonitoring  bool      `json:"skip_monitoring"`  // whether to skip ops monitoring records
	Description     *string   `json:"description"`      // rule description
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// MatchModeAny
const MatchModeAny = "any"

// MatchModeAll
const MatchModeAll = "all"

const (
	PlatformAnthropic   = "anthropic"
	PlatformOpenAI      = "openai"
	PlatformGemini      = "gemini"
	PlatformAntigravity = "antigravity"
)

// AllPlatforms
func AllPlatforms() []string {
	return []string{PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity}
}

// Validate
func (r *ErrorPassthroughRule) Validate() error {
	if r.Name == "" {
		return &ValidationError{Field: "name", Message: "name is required"}
	}
	if r.MatchMode != MatchModeAny && r.MatchMode != MatchModeAll {
		return &ValidationError{Field: "match_mode", Message: "match_mode must be 'any' or 'all'"}
	}
	if len(r.ErrorCodes) == 0 && len(r.Keywords) == 0 {
		return &ValidationError{Field: "conditions", Message: "at least one error_code or keyword is required"}
	}
	if !r.PassthroughCode && (r.ResponseCode == nil || *r.ResponseCode <= 0) {
		return &ValidationError{Field: "response_code", Message: "response_code is required when passthrough_code is false"}
	}
	if !r.PassthroughBody && (r.CustomMessage == nil || *r.CustomMessage == "") {
		return &ValidationError{Field: "custom_message", Message: "custom_message is required when passthrough_body is false"}
	}
	return nil
}

// ValidationError
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
