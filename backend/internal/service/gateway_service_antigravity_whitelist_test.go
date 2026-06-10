//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestGatewayService_isModelSupportedByAccount_AntigravityModelMapping(t *testing.T) {
	svc := &GatewayService{}

	//
	account := &Account{
		Platform: PlatformAntigravity,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"claude-*":   "claude-sonnet-4-5",
				"gemini-3-*": "gemini-3-flash",
			},
		},
	}

	// claude-*
	require.True(t, svc.isModelSupportedByAccount(account, "claude-sonnet-4-5"))
	require.True(t, svc.isModelSupportedByAccount(account, "claude-haiku-4-5"))
	require.True(t, svc.isModelSupportedByAccount(account, "claude-opus-4-6"))

	// gemini-3-*
	require.True(t, svc.isModelSupportedByAccount(account, "gemini-3-flash"))
	require.True(t, svc.isModelSupportedByAccount(account, "gemini-3-pro-high"))

	// gemini-2.5-*
	require.False(t, svc.isModelSupportedByAccount(account, "gemini-2.5-flash"))
	require.False(t, svc.isModelSupportedByAccount(account, "gemini-2.5-pro"))

	require.False(t, svc.isModelSupportedByAccount(account, "gpt-4"))

	require.True(t, svc.isModelSupportedByAccount(account, ""))
}

func TestGatewayService_isModelSupportedByAccount_AntigravityNoMapping(t *testing.T) {
	svc := &GatewayService{}

	//
	account := &Account{
		Platform:    PlatformAntigravity,
		Credentials: map[string]any{},
	}

	require.True(t, svc.isModelSupportedByAccount(account, "claude-sonnet-4-5"))
	require.True(t, svc.isModelSupportedByAccount(account, "gemini-3-flash"))
	require.True(t, svc.isModelSupportedByAccount(account, "gemini-2.5-pro"))
	require.True(t, svc.isModelSupportedByAccount(account, "claude-haiku-4-5"))

	require.False(t, svc.isModelSupportedByAccount(account, "claude-3-5-sonnet-20241022"))
	require.False(t, svc.isModelSupportedByAccount(account, "claude-unknown-model"))

	//
	require.False(t, svc.isModelSupportedByAccount(account, "gpt-4"))
}

// TestGatewayService_isModelSupportedByAccountWithContext_ThinkingMode
//
func TestGatewayService_isModelSupportedByAccountWithContext_ThinkingMode(t *testing.T) {
	svc := &GatewayService{}

	tests := []struct {
		name            string
		modelMapping    map[string]any
		requestedModel  string
		thinkingEnabled bool
		expected        bool
	}{
		// + thinking=true
		// mapAntigravityModel →
		{
			name: "thinking_enabled_no_base_mapping_returns_false",
			modelMapping: map[string]any{
				"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
			},
			requestedModel:  "claude-sonnet-4-5",
			thinkingEnabled: true,
			expected:        false,
		},
		// + thinking=false
		// mapAntigravityModel →
		{
			name: "thinking_disabled_no_base_mapping_returns_false",
			modelMapping: map[string]any{
				"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
			},
			requestedModel:  "claude-sonnet-4-5",
			thinkingEnabled: false,
			expected:        false,
		},
		// + thinking=true
		// = claude-sonnet-4-5-thinking，
		{
			name: "thinking_enabled_no_match_non_thinking_mapping",
			modelMapping: map[string]any{
				"claude-sonnet-4-5": "claude-sonnet-4-5",
			},
			requestedModel:  "claude-sonnet-4-5",
			thinkingEnabled: true,
			expected:        false,
		},
		// + thinking=true，
		{
			name: "both_models_thinking_enabled_matches_thinking",
			modelMapping: map[string]any{
				"claude-sonnet-4-5":          "claude-sonnet-4-5",
				"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
			},
			requestedModel:  "claude-sonnet-4-5",
			thinkingEnabled: true,
			expected:        true,
		},
		// + thinking=false，
		{
			name: "both_models_thinking_disabled_matches_non_thinking",
			modelMapping: map[string]any{
				"claude-sonnet-4-5":          "claude-sonnet-4-5",
				"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
			},
			requestedModel:  "claude-sonnet-4-5",
			thinkingEnabled: false,
			expected:        true,
		},
		// *
		{
			name: "wildcard_matches_thinking",
			modelMapping: map[string]any{
				"claude-*": "claude-sonnet-4-5",
			},
			requestedModel:  "claude-sonnet-4-5",
			thinkingEnabled: true,
			expected:        true, // claude-sonnet-4-5-thinking 匹配 claude-*
		},
		// →
		// mapAntigravityModel
		{
			name: "opus_thinking_no_base_mapping_returns_false",
			modelMapping: map[string]any{
				"claude-opus-4-6-thinking": "claude-opus-4-6-thinking",
			},
			requestedModel:  "claude-opus-4-6",
			thinkingEnabled: true,
			expected:        false,
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

			ctx := context.WithValue(context.Background(), ctxkey.ThinkingEnabled, tt.thinkingEnabled)
			result := svc.isModelSupportedByAccountWithContext(ctx, account, tt.requestedModel)

			require.Equal(t, tt.expected, result,
				"isModelSupportedByAccountWithContext(ctx[thinking=%v], account, %q) = %v, want %v",
				tt.thinkingEnabled, tt.requestedModel, result, tt.expected)
		})
	}
}

// TestGatewayService_isModelSupportedByAccount_CustomMappingNotInDefault
//
func TestGatewayService_isModelSupportedByAccount_CustomMappingNotInDefault(t *testing.T) {
	svc := &GatewayService{}

	account := &Account{
		Platform: PlatformAntigravity,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"my-custom-model":   "actual-upstream-model",
				"gpt-4o":            "some-upstream-model",
				"llama-3-70b":       "llama-3-70b-upstream",
				"claude-sonnet-4-5": "claude-sonnet-4-5",
			},
		},
	}

	//
	require.True(t, svc.isModelSupportedByAccount(account, "my-custom-model"))
	require.True(t, svc.isModelSupportedByAccount(account, "gpt-4o"))
	require.True(t, svc.isModelSupportedByAccount(account, "llama-3-70b"))
	require.True(t, svc.isModelSupportedByAccount(account, "claude-sonnet-4-5"))

	require.False(t, svc.isModelSupportedByAccount(account, "gpt-3.5-turbo"))
	require.False(t, svc.isModelSupportedByAccount(account, "unknown-model"))

	require.True(t, svc.isModelSupportedByAccount(account, ""))
}

// TestGatewayService_isModelSupportedByAccountWithContext_CustomMappingThinking
// + thinking
func TestGatewayService_isModelSupportedByAccountWithContext_CustomMappingThinking(t *testing.T) {
	svc := &GatewayService{}

	//
	account := &Account{
		Platform: PlatformAntigravity,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"claude-sonnet-4-5":          "claude-sonnet-4-5",
				"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
				"my-custom-model":            "upstream-model",
			},
		},
	}

	// thinking=true: claude-sonnet-4-5 → mapped=claude-sonnet-4-5 → +thinking → check IsModelSupported(claude-sonnet-4-5-thinking)=true
	ctx := context.WithValue(context.Background(), ctxkey.ThinkingEnabled, true)
	require.True(t, svc.isModelSupportedByAccountWithContext(ctx, account, "claude-sonnet-4-5"))

	// thinking=false: claude-sonnet-4-5 → mapped=claude-sonnet-4-5 → check IsModelSupported(claude-sonnet-4-5)=true
	ctx = context.WithValue(context.Background(), ctxkey.ThinkingEnabled, false)
	require.True(t, svc.isModelSupportedByAccountWithContext(ctx, account, "claude-sonnet-4-5"))

	//
	ctx = context.WithValue(context.Background(), ctxkey.ThinkingEnabled, true)
	require.True(t, svc.isModelSupportedByAccountWithContext(ctx, account, "my-custom-model"))
}
