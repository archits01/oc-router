package service

import (
	"strings"
	"testing"
)

// TestOpenAIGatewayService_ToolCorrection
func TestOpenAIGatewayService_ToolCorrection(t *testing.T) {
	//
	service := &OpenAIGatewayService{
		toolCorrector: NewCodexToolCorrector(),
	}

	tests := []struct {
		name     string
		input    []byte
		expected string
		changed  bool
	}{
		{
			name: "correct apply_patch in response body",
			input: []byte(`{
				"choices": [{
					"message": {
						"tool_calls": [{
							"function": {"name": "apply_patch"}
						}]
					}
				}]
			}`),
			expected: "edit",
			changed:  true,
		},
		{
			name: "correct update_plan in response body",
			input: []byte(`{
				"tool_calls": [{
					"function": {"name": "update_plan"}
				}]
			}`),
			expected: "todowrite",
			changed:  true,
		},
		{
			name: "no change for correct tool name",
			input: []byte(`{
				"tool_calls": [{
					"function": {"name": "edit"}
				}]
			}`),
			expected: "edit",
			changed:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.correctToolCallsInResponseBody(tt.input)
			resultStr := string(result)

			if !strings.Contains(resultStr, tt.expected) {
				t.Errorf("expected result to contain %q, got %q", tt.expected, resultStr)
			}

			if tt.changed && string(result) == string(tt.input) {
				t.Error("expected result to be different from input, but they are the same")
			}

			if !tt.changed && string(result) != string(tt.input) {
				t.Error("expected result to be same as input, but they are different")
			}
		})
	}
}

// TestOpenAIGatewayService_ToolCorrectorInitialization
func TestOpenAIGatewayService_ToolCorrectorInitialization(t *testing.T) {
	service := &OpenAIGatewayService{
		toolCorrector: NewCodexToolCorrector(),
	}

	if service.toolCorrector == nil {
		t.Fatal("toolCorrector should not be nil")
	}

	data := `{"tool_calls":[{"function":{"name":"apply_patch"}}]}`
	corrected, changed := service.toolCorrector.CorrectToolCallsInSSEData(data)

	if !changed {
		t.Error("expected tool call to be corrected")
	}

	if !strings.Contains(corrected, "edit") {
		t.Errorf("expected corrected data to contain 'edit', got %q", corrected)
	}
}

// TestToolCorrectionStats
func TestToolCorrectionStats(t *testing.T) {
	service := &OpenAIGatewayService{
		toolCorrector: NewCodexToolCorrector(),
	}

	testData := []string{
		`{"tool_calls":[{"function":{"name":"apply_patch"}}]}`,
		`{"tool_calls":[{"function":{"name":"update_plan"}}]}`,
		`{"tool_calls":[{"function":{"name":"apply_patch"}}]}`,
	}

	for _, data := range testData {
		service.toolCorrector.CorrectToolCallsInSSEData(data)
	}

	stats := service.toolCorrector.GetStats()

	if stats.TotalCorrected != 3 {
		t.Errorf("expected 3 corrections, got %d", stats.TotalCorrected)
	}

	if stats.CorrectionsByTool["apply_patch->edit"] != 2 {
		t.Errorf("expected 2 apply_patch->edit corrections, got %d", stats.CorrectionsByTool["apply_patch->edit"])
	}

	if stats.CorrectionsByTool["update_plan->todowrite"] != 1 {
		t.Errorf("expected 1 update_plan->todowrite correction, got %d", stats.CorrectionsByTool["update_plan->todowrite"])
	}
}
