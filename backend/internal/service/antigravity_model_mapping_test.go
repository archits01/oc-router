//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAntigravityGatewayService_GetMappedModel(t *testing.T) {
	svc := &AntigravityGatewayService{}

	tests := []struct {
		name           string
		requestedModel string
		accountMapping map[string]string
		expected       string
	}{
		{
			name:           "账户映射优先",
			requestedModel: "claude-3-5-sonnet-20241022",
			accountMapping: map[string]string{"claude-3-5-sonnet-20241022": "custom-model"},
			expected:       "custom-model",
		},
		{
			name:           "账户映射 - 可覆盖默认映射的model",
			requestedModel: "claude-sonnet-4-5",
			accountMapping: map[string]string{"claude-sonnet-4-5": "my-custom-sonnet"},
			expected:       "my-custom-sonnet",
		},
		{
			name:           "账户映射 - 可覆盖未知model",
			requestedModel: "claude-opus-4",
			accountMapping: map[string]string{"claude-opus-4": "my-opus"},
			expected:       "my-opus",
		},

		// 2.
		{
			name:           "默认映射 - claude-opus-4-6 → claude-opus-4-6-thinking",
			requestedModel: "claude-opus-4-6",
			accountMapping: nil,
			expected:       "claude-opus-4-6-thinking",
		},
		{
			name:           "默认映射 - claude-opus-4-5-20251101 → claude-opus-4-6-thinking",
			requestedModel: "claude-opus-4-5-20251101",
			accountMapping: nil,
			expected:       "claude-opus-4-6-thinking",
		},
		{
			name:           "默认映射 - claude-opus-4-5-thinking → claude-opus-4-6-thinking",
			requestedModel: "claude-opus-4-5-thinking",
			accountMapping: nil,
			expected:       "claude-opus-4-6-thinking",
		},
		{
			name:           "默认映射 - claude-haiku-4-5 → claude-sonnet-4-6",
			requestedModel: "claude-haiku-4-5",
			accountMapping: nil,
			expected:       "claude-sonnet-4-6",
		},
		{
			name:           "默认映射 - claude-haiku-4-5-20251001 → claude-sonnet-4-6",
			requestedModel: "claude-haiku-4-5-20251001",
			accountMapping: nil,
			expected:       "claude-sonnet-4-6",
		},
		{
			name:           "默认映射 - claude-sonnet-4-5-20250929 → claude-sonnet-4-5",
			requestedModel: "claude-sonnet-4-5-20250929",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5",
		},

		{
			name:           "默认映射透传 - claude-fable-5",
			requestedModel: "claude-fable-5",
			accountMapping: nil,
			expected:       "claude-fable-5",
		},
		{
			name:           "默认映射透传 - claude-sonnet-4-6",
			requestedModel: "claude-sonnet-4-6",
			accountMapping: nil,
			expected:       "claude-sonnet-4-6",
		},
		{
			name:           "默认映射透传 - claude-sonnet-4-5",
			requestedModel: "claude-sonnet-4-5",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "默认映射透传 - claude-opus-4-8",
			requestedModel: "claude-opus-4-8",
			accountMapping: nil,
			expected:       "claude-opus-4-8",
		},
		{
			name:           "默认映射透传 - claude-opus-4-7",
			requestedModel: "claude-opus-4-7",
			accountMapping: nil,
			expected:       "claude-opus-4-7",
		},
		{
			name:           "默认映射透传 - claude-opus-4-6-thinking",
			requestedModel: "claude-opus-4-6-thinking",
			accountMapping: nil,
			expected:       "claude-opus-4-6-thinking",
		},
		{
			name:           "默认映射透传 - claude-sonnet-4-5-thinking",
			requestedModel: "claude-sonnet-4-5-thinking",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5-thinking",
		},
		{
			name:           "默认映射透传 - gemini-2.5-flash",
			requestedModel: "gemini-2.5-flash",
			accountMapping: nil,
			expected:       "gemini-2.5-flash",
		},
		{
			name:           "默认映射透传 - gemini-2.5-pro",
			requestedModel: "gemini-2.5-pro",
			accountMapping: nil,
			expected:       "gemini-2.5-pro",
		},
		{
			name:           "默认映射透传 - gemini-3-flash",
			requestedModel: "gemini-3-flash",
			accountMapping: nil,
			expected:       "gemini-3-flash",
		},

		{
			name:           "未知model - claude-unknown returned空",
			requestedModel: "claude-unknown",
			accountMapping: nil,
			expected:       "",
		},
		{
			name:           "未知model - claude-3-5-sonnet-20241022 returned空（未在默认映射）",
			requestedModel: "claude-3-5-sonnet-20241022",
			accountMapping: nil,
			expected:       "",
		},
		{
			name:           "未知model - claude-3-opus-20240229 returned空",
			requestedModel: "claude-3-opus-20240229",
			accountMapping: nil,
			expected:       "",
		},
		{
			name:           "未知model - claude-opus-4 returned空",
			requestedModel: "claude-opus-4",
			accountMapping: nil,
			expected:       "",
		},
		{
			name:           "未知model - gemini-future-model returned空",
			requestedModel: "gemini-future-model",
			accountMapping: nil,
			expected:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform: PlatformAntigravity,
			}
			if tt.accountMapping != nil {
				// GetModelMapping [string]any
				mappingAny := make(map[string]any)
				for k, v := range tt.accountMapping {
					mappingAny[k] = v
				}
				account.Credentials = map[string]any{
					"model_mapping": mappingAny,
				}
			}

			got := svc.getMappedModel(account, tt.requestedModel)
			require.Equal(t, tt.expected, got, "model: %s", tt.requestedModel)
		})
	}
}

func TestAntigravityGatewayService_GetMappedModel_EdgeCases(t *testing.T) {
	svc := &AntigravityGatewayService{}

	tests := []struct {
		name           string
		requestedModel string
		expected       string
	}{
		//
		{"empty string", "", ""},
		{"非claude/gemini前缀 - gpt", "gpt-4", ""},
		{"非claude/gemini前缀 - llama", "llama-3", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: PlatformAntigravity}
			got := svc.getMappedModel(account, tt.requestedModel)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestAntigravityGatewayService_IsModelSupported(t *testing.T) {
	svc := &AntigravityGatewayService{}

	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		{"directly supported - claude-fable-5", "claude-fable-5", true},
		{"directly supported - claude-sonnet-4-5", "claude-sonnet-4-5", true},
		{"directly supported - gemini-3-flash", "gemini-3-flash", true},

		{"可映射 - claude-opus-4-8", "claude-opus-4-8", true},
		{"可映射 - claude-opus-4-6", "claude-opus-4-6", true},

		//
		{"Gemini前缀", "gemini-unknown", true},
		{"Claude前缀", "claude-unknown", true},

		{"不支持 - gpt-4", "gpt-4", false},
		{"不支持 - empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.IsModelSupported(tt.model)
			require.Equal(t, tt.expected, got)
		})
	}
}

// TestMapAntigravityModel_WildcardTargetEqualsRequest
// {"claude-*": "claude-sonnet-4-5"}，"claude-sonnet-4-5"
func TestMapAntigravityModel_WildcardTargetEqualsRequest(t *testing.T) {
	tests := []struct {
		name           string
		modelMapping   map[string]any
		requestedModel string
		expected       string
	}{
		{
			name:           "wildcard target equals request model",
			modelMapping:   map[string]any{"claude-*": "claude-sonnet-4-5"},
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "wildcard target differs from request model",
			modelMapping:   map[string]any{"claude-*": "claude-sonnet-4-5"},
			requestedModel: "claude-opus-4-6",
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "wildcard no match",
			modelMapping:   map[string]any{"claude-*": "claude-sonnet-4-5"},
			requestedModel: "gpt-4o",
			expected:       "",
		},
		{
			name:           "explicit passthrough same name",
			modelMapping:   map[string]any{"claude-sonnet-4-5": "claude-sonnet-4-5"},
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "multiple wildcards target equals one request",
			modelMapping:   map[string]any{"claude-*": "claude-sonnet-4-5", "gemini-*": "gemini-2.5-flash"},
			requestedModel: "gemini-2.5-flash",
			expected:       "gemini-2.5-flash",
		},
		{
			name:           "customtools alias falls back to normalized preview mapping",
			modelMapping:   map[string]any{"gemini-3.1-pro-preview": "gemini-3.1-pro-high"},
			requestedModel: "gemini-3.1-pro-preview-customtools",
			expected:       "gemini-3.1-pro-high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform: PlatformAntigravity,
				Credentials: map[string]any{
					"model_mapping": tt.modelMapping,
				},
			}
			got := mapAntigravityModel(account, tt.requestedModel)
			require.Equal(t, tt.expected, got, "mapAntigravityModel(%q) = %q, want %q", tt.requestedModel, got, tt.expected)
		})
	}
}
