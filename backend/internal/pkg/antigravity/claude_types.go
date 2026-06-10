package antigravity

import "encoding/json"

// Claude

// ClaudeRequest Claude Messages API
type ClaudeRequest struct {
	Model       string          `json:"model"`
	Messages    []ClaudeMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	System      json.RawMessage `json:"system,omitempty"` // string or []SystemBlock
	Stream      bool            `json:"stream,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	TopK        *int            `json:"top_k,omitempty"`
	Tools       []ClaudeTool    `json:"tools,omitempty"`
	Thinking    *ThinkingConfig `json:"thinking,omitempty"`
	Metadata    *ClaudeMetadata `json:"metadata,omitempty"`
}

// ClaudeMessage Claude
type ClaudeMessage struct {
	Role    string          `json:"role"` // user, assistant
	Content json.RawMessage `json:"content"`
}

// ThinkingConfig Thinking
type ThinkingConfig struct {
	Type         string `json:"type"`                    // "enabled" / "adaptive" / "disabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"` // thinking budget
}

// ClaudeMetadata
type ClaudeMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

// ClaudeTool Claude
// 1. { "name": "...", "description": "...", "input_schema": {...} }
// 2. Custom (MCP): { "type": "custom", "name": "...", "custom": { "description": "...", "input_schema": {...} } }
type ClaudeTool struct {
	Type        string          `json:"type,omitempty"` // "custom" or empty (standard format)
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`  // used in standard format
	InputSchema map[string]any  `json:"input_schema,omitempty"` // used in standard format
	Custom      *CustomToolSpec `json:"custom,omitempty"`       // used in custom format
}

// CustomToolSpec MCP custom
type CustomToolSpec struct {
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

// ClaudeCustomToolSpec
type ClaudeCustomToolSpec = CustomToolSpec

// SystemBlock system prompt
type SystemBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ContentBlock Claude
type ContentBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	// tool_use
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
	// tool_result
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	// image
	Source *ImageSource `json:"source,omitempty"`
}

// ImageSource Claude
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", "image/jpeg", etc.
	Data      string `json:"data"`
}

// ClaudeResponse Claude Messages API
type ClaudeResponse struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"` // "message"
	Role         string              `json:"role"` // "assistant"
	Model        string              `json:"model"`
	Content      []ClaudeContentItem `json:"content"`
	StopReason   string              `json:"stop_reason,omitempty"`   // end_turn, tool_use, max_tokens
	StopSequence *string             `json:"stop_sequence,omitempty"` // null or specific value
	Usage        ClaudeUsage         `json:"usage"`
}

// ClaudeContentItem Claude
type ClaudeContentItem struct {
	Type string `json:"type"` // text, thinking, tool_use

	// text
	Text string `json:"text,omitempty"`

	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	// tool_use
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

// ClaudeUsage Claude
type ClaudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	ImageOutputTokens        int `json:"image_output_tokens,omitempty"`
}

// ClaudeError Claude
type ClaudeError struct {
	Type  string      `json:"type"` // "error"
	Error ErrorDetail `json:"error"`
}

// ErrorDetail
type ErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// modelDef Antigravity
type modelDef struct {
	ID          string
	DisplayName string
	CreatedAt   string // used only in Claude API format
}

// Antigravity
var claudeModels = []modelDef{
	{ID: "claude-fable-5", DisplayName: "Claude Fable 5", CreatedAt: "2026-06-09T00:00:00Z"},
	{ID: "claude-opus-4-5-thinking", DisplayName: "Claude Opus 4.5 Thinking", CreatedAt: "2025-11-01T00:00:00Z"},
	{ID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", CreatedAt: "2025-09-29T00:00:00Z"},
	{ID: "claude-sonnet-4-5-thinking", DisplayName: "Claude Sonnet 4.5 Thinking", CreatedAt: "2025-09-29T00:00:00Z"},
	{ID: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", CreatedAt: "2026-02-05T00:00:00Z"},
	{ID: "claude-opus-4-6-thinking", DisplayName: "Claude Opus 4.6 Thinking", CreatedAt: "2026-02-05T00:00:00Z"},
	{ID: "claude-opus-4-7", DisplayName: "Claude Opus 4.7", CreatedAt: "2026-04-17T00:00:00Z"},
	{ID: "claude-opus-4-8", DisplayName: "Claude Opus 4.8", CreatedAt: "2026-05-29T00:00:00Z"},
	{ID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", CreatedAt: "2026-02-17T00:00:00Z"},
}

// Antigravity
var geminiModels = []modelDef{
	{ID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", CreatedAt: "2025-01-01T00:00:00Z"},
	{ID: "gemini-2.5-flash-image", DisplayName: "Gemini 2.5 Flash Image", CreatedAt: "2025-01-01T00:00:00Z"},
	{ID: "gemini-2.5-flash-image-preview", DisplayName: "Gemini 2.5 Flash Image Preview", CreatedAt: "2025-01-01T00:00:00Z"},
	{ID: "gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash Lite", CreatedAt: "2025-01-01T00:00:00Z"},
	{ID: "gemini-2.5-flash-thinking", DisplayName: "Gemini 2.5 Flash Thinking", CreatedAt: "2025-01-01T00:00:00Z"},
	{ID: "gemini-3-flash", DisplayName: "Gemini 3 Flash", CreatedAt: "2025-06-01T00:00:00Z"},
	{ID: "gemini-3-pro-low", DisplayName: "Gemini 3 Pro Low", CreatedAt: "2025-06-01T00:00:00Z"},
	{ID: "gemini-3-pro-high", DisplayName: "Gemini 3 Pro High", CreatedAt: "2025-06-01T00:00:00Z"},
	{ID: "gemini-3.1-pro-low", DisplayName: "Gemini 3.1 Pro Low", CreatedAt: "2026-02-19T00:00:00Z"},
	{ID: "gemini-3.1-pro-high", DisplayName: "Gemini 3.1 Pro High", CreatedAt: "2026-02-19T00:00:00Z"},
	{ID: "gemini-3.1-flash-image", DisplayName: "Gemini 3.1 Flash Image", CreatedAt: "2026-02-19T00:00:00Z"},
	{ID: "gemini-3.1-flash-image-preview", DisplayName: "Gemini 3.1 Flash Image Preview", CreatedAt: "2026-02-19T00:00:00Z"},
	{ID: "gemini-3-pro-preview", DisplayName: "Gemini 3 Pro Preview", CreatedAt: "2025-06-01T00:00:00Z"},
	{ID: "gemini-3-pro-image", DisplayName: "Gemini 3 Pro Image", CreatedAt: "2025-06-01T00:00:00Z"},
}

// ========== Claude API (/v1/models) ==========

// ClaudeModel Claude API
type ClaudeModel struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

// DefaultModels + Gemini）
func DefaultModels() []ClaudeModel {
	all := append(claudeModels, geminiModels...)
	result := make([]ClaudeModel, len(all))
	for i, m := range all {
		result[i] = ClaudeModel{ID: m.ID, Type: "model", DisplayName: m.DisplayName, CreatedAt: m.CreatedAt}
	}
	return result
}

// ========== Gemini v1beta (/v1beta/models) ==========

// GeminiModel Gemini v1beta
type GeminiModel struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName,omitempty"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods,omitempty"`
}

// GeminiModelsListResponse Gemini v1beta
type GeminiModelsListResponse struct {
	Models []GeminiModel `json:"models"`
}

var defaultGeminiMethods = []string{"generateContent", "streamGenerateContent"}

// DefaultGeminiModels
func DefaultGeminiModels() []GeminiModel {
	result := make([]GeminiModel, len(geminiModels))
	for i, m := range geminiModels {
		result[i] = GeminiModel{Name: "models/" + m.ID, DisplayName: m.DisplayName, SupportedGenerationMethods: defaultGeminiMethods}
	}
	return result
}

// FallbackGeminiModelsList
func FallbackGeminiModelsList() GeminiModelsListResponse {
	return GeminiModelsListResponse{Models: DefaultGeminiModels()}
}

// FallbackGeminiModel
func FallbackGeminiModel(model string) GeminiModel {
	if model == "" {
		return GeminiModel{Name: "models/unknown", SupportedGenerationMethods: defaultGeminiMethods}
	}
	name := model
	if len(model) < 7 || model[:7] != "models/" {
		name = "models/" + model
	}
	return GeminiModel{Name: name, SupportedGenerationMethods: defaultGeminiMethods}
}
