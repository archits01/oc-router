package antigravity

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBuildParts_ThinkingBlockWithoutSignature
func TestBuildParts_ThinkingBlockWithoutSignature(t *testing.T) {
	tests := []struct {
		name              string
		content           string
		allowDummyThought bool
		expectedParts     int
		description       string
	}{
		{
			name: "Claude model - downgrade thinking to text without signature",
			content: `[
				{"type": "text", "text": "Hello"},
				{"type": "thinking", "thinking": "Let me think...", "signature": ""},
				{"type": "text", "text": "World"}
			]`,
			allowDummyThought: false,
			expectedParts:     3, // thinking content degraded to plain text part
			description:       "Claude model without signature should degrade thinking to text and disable thinking mode at the upper level",
		},
		{
			name: "Claude model - preserve thinking block with signature",
			content: `[
				{"type": "text", "text": "Hello"},
				{"type": "thinking", "thinking": "Let me think...", "signature": "sig_real_123"},
				{"type": "text", "text": "World"}
			]`,
			allowDummyThought: false,
			expectedParts:     3,
			description:       "Claude model should pass through thinking blocks with signature (for Vertex signing path)",
		},
		{
			name: "Gemini model - use dummy signature",
			content: `[
				{"type": "text", "text": "Hello"},
				{"type": "thinking", "thinking": "Let me think...", "signature": ""},
				{"type": "text", "text": "World"}
			]`,
			allowDummyThought: true,
			expectedParts:     3, // all three blocks preserved, thinking uses dummy signature
			description:       "Gemini model should use dummy signature for thinking blocks without signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolIDToName := make(map[string]string)
			parts, _, err := buildParts(json.RawMessage(tt.content), toolIDToName, tt.allowDummyThought)

			if err != nil {
				t.Fatalf("buildParts() error = %v", err)
			}

			if len(parts) != tt.expectedParts {
				t.Errorf("%s: got %d parts, want %d parts", tt.description, len(parts), tt.expectedParts)
			}

			switch tt.name {
			case "Claude model - preserve thinking block with signature":
				if len(parts) != 3 {
					t.Fatalf("expected 3 parts, got %d", len(parts))
				}
				if !parts[1].Thought || parts[1].ThoughtSignature != "sig_real_123" {
					t.Fatalf("expected thought part with signature sig_real_123, got thought=%v signature=%q",
						parts[1].Thought, parts[1].ThoughtSignature)
				}
			case "Claude model - downgrade thinking to text without signature":
				if len(parts) != 3 {
					t.Fatalf("expected 3 parts, got %d", len(parts))
				}
				if parts[1].Thought {
					t.Fatalf("expected downgraded text part, got thought=%v signature=%q",
						parts[1].Thought, parts[1].ThoughtSignature)
				}
				if parts[1].Text != "Let me think..." {
					t.Fatalf("expected downgraded text %q, got %q", "Let me think...", parts[1].Text)
				}
			case "Gemini model - use dummy signature":
				if len(parts) != 3 {
					t.Fatalf("expected 3 parts, got %d", len(parts))
				}
				if !parts[1].Thought || parts[1].ThoughtSignature != DummyThoughtSignature {
					t.Fatalf("expected dummy thought signature, got thought=%v signature=%q",
						parts[1].Thought, parts[1].ThoughtSignature)
				}
			}
		})
	}
}

func TestBuildParts_ToolUseSignatureHandling(t *testing.T) {
	content := `[
		{"type": "tool_use", "id": "t1", "name": "Bash", "input": {"command": "ls"}, "signature": "sig_tool_abc"}
	]`

	t.Run("Gemini preserves provided tool_use signature", func(t *testing.T) {
		toolIDToName := make(map[string]string)
		parts, _, err := buildParts(json.RawMessage(content), toolIDToName, true)
		if err != nil {
			t.Fatalf("buildParts() error = %v", err)
		}
		if len(parts) != 1 || parts[0].FunctionCall == nil {
			t.Fatalf("expected 1 functionCall part, got %+v", parts)
		}
		if parts[0].ThoughtSignature != "sig_tool_abc" {
			t.Fatalf("expected preserved tool signature %q, got %q", "sig_tool_abc", parts[0].ThoughtSignature)
		}
	})

	t.Run("Gemini falls back to dummy tool_use signature when missing", func(t *testing.T) {
		contentNoSig := `[
			{"type": "tool_use", "id": "t1", "name": "Bash", "input": {"command": "ls"}}
		]`
		toolIDToName := make(map[string]string)
		parts, _, err := buildParts(json.RawMessage(contentNoSig), toolIDToName, true)
		if err != nil {
			t.Fatalf("buildParts() error = %v", err)
		}
		if len(parts) != 1 || parts[0].FunctionCall == nil {
			t.Fatalf("expected 1 functionCall part, got %+v", parts)
		}
		if parts[0].ThoughtSignature != DummyThoughtSignature {
			t.Fatalf("expected dummy tool signature %q, got %q", DummyThoughtSignature, parts[0].ThoughtSignature)
		}
	})

	t.Run("Claude model - preserve valid signature for tool_use", func(t *testing.T) {
		toolIDToName := make(map[string]string)
		parts, _, err := buildParts(json.RawMessage(content), toolIDToName, false)
		if err != nil {
			t.Fatalf("buildParts() error = %v", err)
		}
		if len(parts) != 1 || parts[0].FunctionCall == nil {
			t.Fatalf("expected 1 functionCall part, got %+v", parts)
		}
		// Claude
		if parts[0].ThoughtSignature != "sig_tool_abc" {
			t.Fatalf("expected preserved tool signature %q, got %q", "sig_tool_abc", parts[0].ThoughtSignature)
		}
	})
}

// TestBuildTools_CustomTypeTools
func TestBuildTools_CustomTypeTools(t *testing.T) {
	tests := []struct {
		name        string
		tools       []ClaudeTool
		expectedLen int
		description string
	}{
		{
			name: "Standard tool format",
			tools: []ClaudeTool{
				{
					Name:        "get_weather",
					Description: "Get weather information",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"location": map[string]any{"type": "string"},
						},
					},
				},
			},
			expectedLen: 1,
			description: "standard tool format should convert correctly",
		},
		{
			name: "Custom type tool (MCP format)",
			tools: []ClaudeTool{
				{
					Type: "custom",
					Name: "mcp_tool",
					Custom: &ClaudeCustomToolSpec{
						Description: "MCP tool description",
						InputSchema: map[string]any{
							"type": "object",
							"properties": map[string]any{
								"param": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
			expectedLen: 1,
			description: "Custom type tools should read description and input_schema from Custom field",
		},
		{
			name: "Mixed standard and custom tools",
			tools: []ClaudeTool{
				{
					Name:        "standard_tool",
					Description: "Standard tool",
					InputSchema: map[string]any{"type": "object"},
				},
				{
					Type: "custom",
					Name: "custom_tool",
					Custom: &ClaudeCustomToolSpec{
						Description: "Custom tool",
						InputSchema: map[string]any{"type": "object"},
					},
				},
			},
			expectedLen: 1, // returns one GeminiToolDeclaration containing 2 function declarations
			description: "mixed standard and custom tools should all convert correctly",
		},
		{
			name: "Invalid custom tool - nil Custom field",
			tools: []ClaudeTool{
				{
					Type: "custom",
					Name: "invalid_custom",
					// Custom
				},
			},
			expectedLen: 0, // should be skipped
			description: "custom tool with nil Custom field should be skipped",
		},
		{
			name: "Invalid custom tool - nil InputSchema",
			tools: []ClaudeTool{
				{
					Type: "custom",
					Name: "invalid_custom",
					Custom: &ClaudeCustomToolSpec{
						Description: "Invalid",
						// InputSchema
					},
				},
			},
			expectedLen: 0, // should be skipped
			description: "custom tool with nil InputSchema should be skipped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildTools(tt.tools)

			if len(result) != tt.expectedLen {
				t.Errorf("%s: got %d tool declarations, want %d", tt.description, len(result), tt.expectedLen)
			}

			//
			if len(result) > 0 && result[0].FunctionDeclarations != nil {
				if len(result[0].FunctionDeclarations) != len(tt.tools) {
					t.Errorf("%s: got %d function declarations, want %d",
						tt.description, len(result[0].FunctionDeclarations), len(tt.tools))
				}
			}
		})
	}
}

func TestBuildTools_PreservesWebSearchAlongsideFunctions(t *testing.T) {
	tools := []ClaudeTool{
		{
			Name:        "get_weather",
			Description: "Get weather information",
			InputSchema: map[string]any{"type": "object"},
		},
		{
			Type: "web_search_20250305",
			Name: "web_search",
		},
	}

	result := buildTools(tools)
	require.Len(t, result, 2)
	require.Len(t, result[0].FunctionDeclarations, 1)
	require.Equal(t, "get_weather", result[0].FunctionDeclarations[0].Name)
	require.NotNil(t, result[1].GoogleSearch)
	require.NotNil(t, result[1].GoogleSearch.EnhancedContent)
	require.NotNil(t, result[1].GoogleSearch.EnhancedContent.ImageSearch)
	require.Equal(t, 5, result[1].GoogleSearch.EnhancedContent.ImageSearch.MaxResultCount)
}

func TestBuildGenerationConfig_ThinkingDynamicBudget(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		thinking    *ThinkingConfig
		wantBudget  int
		wantPresent bool
	}{
		{
			name:        "enabled without budget defaults to dynamic (-1)",
			model:       "claude-opus-4-6-thinking",
			thinking:    &ThinkingConfig{Type: "enabled"},
			wantBudget:  -1,
			wantPresent: true,
		},
		{
			name:        "enabled with budget uses the provided value",
			model:       "claude-opus-4-6-thinking",
			thinking:    &ThinkingConfig{Type: "enabled", BudgetTokens: 1024},
			wantBudget:  1024,
			wantPresent: true,
		},
		{
			name:        "enabled with -1 budget uses dynamic (-1)",
			model:       "claude-opus-4-6-thinking",
			thinking:    &ThinkingConfig{Type: "enabled", BudgetTokens: -1},
			wantBudget:  -1,
			wantPresent: true,
		},
		{
			name:        "adaptive on opus4.6 maps to high budget (24576)",
			model:       "claude-opus-4-6-thinking",
			thinking:    &ThinkingConfig{Type: "adaptive", BudgetTokens: 20000},
			wantBudget:  ClaudeAdaptiveHighThinkingBudgetTokens,
			wantPresent: true,
		},
		{
			name:        "adaptive on non-opus model keeps default dynamic (-1)",
			model:       "claude-sonnet-4-5-thinking",
			thinking:    &ThinkingConfig{Type: "adaptive"},
			wantBudget:  -1,
			wantPresent: true,
		},
		{
			name:        "disabled does not emit thinkingConfig",
			model:       "claude-opus-4-6-thinking",
			thinking:    &ThinkingConfig{Type: "disabled", BudgetTokens: 1024},
			wantBudget:  0,
			wantPresent: false,
		},
		{
			name:        "nil thinking does not emit thinkingConfig",
			model:       "claude-opus-4-6-thinking",
			thinking:    nil,
			wantBudget:  0,
			wantPresent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ClaudeRequest{
				Model:    tt.model,
				Thinking: tt.thinking,
			}
			cfg := buildGenerationConfig(req)
			if cfg == nil {
				t.Fatalf("expected non-nil generationConfig")
			}

			if tt.wantPresent {
				if cfg.ThinkingConfig == nil {
					t.Fatalf("expected thinkingConfig to be present")
				}
				if !cfg.ThinkingConfig.IncludeThoughts {
					t.Fatalf("expected includeThoughts=true")
				}
				if cfg.ThinkingConfig.ThinkingBudget != tt.wantBudget {
					t.Fatalf("expected thinkingBudget=%d, got %d", tt.wantBudget, cfg.ThinkingConfig.ThinkingBudget)
				}
				return
			}

			if cfg.ThinkingConfig != nil {
				t.Fatalf("expected thinkingConfig to be nil, got %+v", cfg.ThinkingConfig)
			}
		})
	}
}

func TestTransformClaudeToGeminiWithOptions_PreservesBillingHeaderSystemBlock(t *testing.T) {
	tests := []struct {
		name   string
		system json.RawMessage
	}{
		{
			name:   "system array",
			system: json.RawMessage(`[{"type":"text","text":"x-anthropic-billing-header keep"}]`),
		},
		{
			name:   "system string",
			system: json.RawMessage(`"x-anthropic-billing-header keep"`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claudeReq := &ClaudeRequest{
				Model:  "claude-3-5-sonnet-latest",
				System: tt.system,
				Messages: []ClaudeMessage{
					{
						Role:    "user",
						Content: json.RawMessage(`[{"type":"text","text":"hello"}]`),
					},
				},
			}

			body, err := TransformClaudeToGeminiWithOptions(claudeReq, "project-1", "gemini-2.5-flash", DefaultTransformOptions())
			require.NoError(t, err)

			var req V1InternalRequest
			require.NoError(t, json.Unmarshal(body, &req))
			require.NotNil(t, req.Request.SystemInstruction)

			found := false
			for _, part := range req.Request.SystemInstruction.Parts {
				if strings.Contains(part.Text, "x-anthropic-billing-header keep") {
					found = true
					break
				}
			}

			require.True(t, found, "converted systemInstruction should preserve x-anthropic-billing-header content")
		})
	}
}

func TestTransformClaudeToGeminiWithOptions_PreservesWebSearchAlongsideFunctions(t *testing.T) {
	claudeReq := &ClaudeRequest{
		Model: "claude-3-5-sonnet-latest",
		Messages: []ClaudeMessage{
			{
				Role:    "user",
				Content: json.RawMessage(`[{"type":"text","text":"hello"}]`),
			},
		},
		Tools: []ClaudeTool{
			{
				Name:        "get_weather",
				Description: "Get weather information",
				InputSchema: map[string]any{"type": "object"},
			},
			{
				Type: "web_search_20250305",
				Name: "web_search",
			},
		},
	}

	body, err := TransformClaudeToGeminiWithOptions(claudeReq, "project-1", "gemini-2.5-flash", DefaultTransformOptions())
	require.NoError(t, err)

	var req V1InternalRequest
	require.NoError(t, json.Unmarshal(body, &req))
	require.Len(t, req.Request.Tools, 2)
	require.Len(t, req.Request.Tools[0].FunctionDeclarations, 1)
	require.Equal(t, "get_weather", req.Request.Tools[0].FunctionDeclarations[0].Name)
	require.NotNil(t, req.Request.Tools[1].GoogleSearch)
}
