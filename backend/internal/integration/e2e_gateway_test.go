//go:build e2e

package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	baseURL = getEnv("BASE_URL", "http://localhost:8080")
	// ENDPOINT_PREFIX:
	// - "" ():
	// - "/antigravity":
	endpointPrefix = getEnv("ENDPOINT_PREFIX", "")
	testInterval   = 1 * time.Second // test interval to prevent rate limiting
)

const (
	//
	//   export CLAUDE_API_KEY="sk-..."
	//   export GEMINI_API_KEY="sk-..."
	claudeAPIKeyEnv = "CLAUDE_API_KEY"
	geminiAPIKeyEnv = "GEMINI_API_KEY"
)

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// Claude
var claudeModels = []string{
	// Opus
	"claude-opus-4-5-thinking", // directly supported
	"claude-opus-4",            // maps to claude-opus-4-5-thinking
	"claude-opus-4-5-20251101", // maps to claude-opus-4-5-thinking
	// Sonnet
	"claude-sonnet-4-5",          // directly supported
	"claude-sonnet-4-5-thinking", // directly supported
	"claude-sonnet-4-5-20250929", // maps to claude-sonnet-4-5-thinking
	"claude-3-5-sonnet-20241022", // maps to claude-sonnet-4-5
	// Haiku
	"claude-haiku-4",
	"claude-haiku-4-5",
	"claude-haiku-4-5-20251001",
	"claude-3-haiku-20240307",
}

// Gemini
var geminiModels = []string{
	"gemini-2.5-flash",
	"gemini-2.5-flash-lite",
	"gemini-3-flash",
	"gemini-3-pro-low",
	"gemini-3-pro-high",
}

func TestMain(m *testing.M) {
	mode := "mixed mode"
	if endpointPrefix != "" {
		mode = "Antigravity mode"
	}
	claudeKeySet := strings.TrimSpace(os.Getenv(claudeAPIKeyEnv)) != ""
	geminiKeySet := strings.TrimSpace(os.Getenv(geminiAPIKeyEnv)) != ""
	fmt.Printf("\n🚀 E2E Gateway Tests - %s (prefix=%q, %s, %s=%v, %s=%v)\n\n",
		baseURL,
		endpointPrefix,
		mode,
		claudeAPIKeyEnv,
		claudeKeySet,
		geminiAPIKeyEnv,
		geminiKeySet,
	)
	os.Exit(m.Run())
}

func requireClaudeAPIKey(t *testing.T) string {
	t.Helper()
	key := strings.TrimSpace(os.Getenv(claudeAPIKeyEnv))
	if key == "" {
		t.Skipf("not set %s，skipping Claude-related E2E tests", claudeAPIKeyEnv)
	}
	return key
}

func requireGeminiAPIKey(t *testing.T) string {
	t.Helper()
	key := strings.TrimSpace(os.Getenv(geminiAPIKeyEnv))
	if key == "" {
		t.Skipf("not set %s，skipping Gemini-related E2E tests", geminiAPIKeyEnv)
	}
	return key
}

// TestClaudeModelsList
func TestClaudeModelsList(t *testing.T) {
	claudeKey := requireClaudeAPIKey(t)
	url := baseURL + endpointPrefix + "/v1/models"

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+claudeKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["object"] != "list" {
		t.Errorf("expected object=list, got %v", result["object"])
	}

	data, ok := result["data"].([]any)
	if !ok {
		t.Fatal("response missing data array")
	}
	t.Logf("✅ returned %d models", len(data))
}

// TestGeminiModelsList
func TestGeminiModelsList(t *testing.T) {
	geminiKey := requireGeminiAPIKey(t)
	url := baseURL + endpointPrefix + "/v1beta/models"

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+geminiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	models, ok := result["models"].([]any)
	if !ok {
		t.Fatal("response missing models array")
	}
	t.Logf("✅ returned %d models", len(models))
}

// TestClaudeMessages
func TestClaudeMessages(t *testing.T) {
	claudeKey := requireClaudeAPIKey(t)
	for i, model := range claudeModels {
		if i > 0 {
			time.Sleep(testInterval)
		}
		t.Run(model+"_non-streaming", func(t *testing.T) {
			testClaudeMessage(t, claudeKey, model, false)
		})
		time.Sleep(testInterval)
		t.Run(model+"_streaming", func(t *testing.T) {
			testClaudeMessage(t, claudeKey, model, true)
		})
	}
}

func testClaudeMessage(t *testing.T, claudeKey string, model string, stream bool) {
	url := baseURL + endpointPrefix + "/v1/messages"

	payload := map[string]any{
		"model":      model,
		"max_tokens": 50,
		"stream":     stream,
		"messages": []map[string]string{
			{"role": "user", "content": "Say 'hello' in one word."},
		},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+claudeKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if stream {
		//
		scanner := bufio.NewScanner(resp.Body)
		eventCount := 0
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				eventCount++
				if eventCount >= 3 {
					break
				}
			}
		}
		if eventCount == 0 {
			t.Fatal("no SSE events received")
		}
		t.Logf("✅ received %d+ SSE events", eventCount)
	} else {
		//
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if result["type"] != "message" {
			t.Errorf("expected type=message, got %v", result["type"])
		}
		t.Logf("✅ received message response id=%v", result["id"])
	}
}

// TestGeminiGenerateContent
func TestGeminiGenerateContent(t *testing.T) {
	geminiKey := requireGeminiAPIKey(t)
	for i, model := range geminiModels {
		if i > 0 {
			time.Sleep(testInterval)
		}
		t.Run(model+"_non-streaming", func(t *testing.T) {
			testGeminiGenerate(t, geminiKey, model, false)
		})
		time.Sleep(testInterval)
		t.Run(model+"_streaming", func(t *testing.T) {
			testGeminiGenerate(t, geminiKey, model, true)
		})
	}
}

func testGeminiGenerate(t *testing.T, geminiKey string, model string, stream bool) {
	action := "generateContent"
	if stream {
		action = "streamGenerateContent"
	}
	url := fmt.Sprintf("%s%s/v1beta/models/%s:%s", baseURL, endpointPrefix, model, action)
	if stream {
		url += "?alt=sse"
	}

	payload := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]string{
					{"text": "Say 'hello' in one word."},
				},
			},
		},
		"generationConfig": map[string]int{
			"maxOutputTokens": 50,
		},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+geminiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if stream {
		//
		scanner := bufio.NewScanner(resp.Body)
		eventCount := 0
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				eventCount++
				if eventCount >= 3 {
					break
				}
			}
		}
		if eventCount == 0 {
			t.Fatal("no SSE events received")
		}
		t.Logf("✅ received %d+ SSE events", eventCount)
	} else {
		//
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if _, ok := result["candidates"]; !ok {
			t.Error("response missing candidates field")
		}
		t.Log("✅ received candidates response")
	}
}

// TestClaudeMessagesWithComplexTools
//
func TestClaudeMessagesWithComplexTools(t *testing.T) {
	claudeKey := requireClaudeAPIKey(t)
	models := []string{
		"claude-opus-4-5-20251101",  // Claude model
		"claude-haiku-4-5-20251001", // maps to Gemini
	}

	for i, model := range models {
		if i > 0 {
			time.Sleep(testInterval)
		}
		t.Run(model+"_complex tools", func(t *testing.T) {
			testClaudeMessageWithTools(t, claudeKey, model)
		})
	}
}

func testClaudeMessageWithTools(t *testing.T, claudeKey string, model string) {
	url := baseURL + endpointPrefix + "/v1/messages"

	//
	//
	tools := []map[string]any{
		{
			"name":        "read_file",
			"description": "Read file contents",
			"input_schema": map[string]any{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type":    "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "File path",
						"minLength":   1,
						"maxLength":   4096,
						"pattern":     "^[^\\x00]+$",
					},
					"encoding": map[string]any{
						"type":    []string{"string", "null"},
						"default": "utf-8",
						"enum":    []string{"utf-8", "ascii", "latin-1"},
					},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "write_file",
			"description": "Write content to file",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":      "string",
						"minLength": 1,
					},
					"content": map[string]any{
						"type":      "string",
						"maxLength": 1048576,
					},
				},
				"required":             []string{"path", "content"},
				"additionalProperties": false,
				"strict":               true,
			},
		},
		{
			"name":        "list_files",
			"description": "List files in directory",
			"input_schema": map[string]any{
				"$id":  "https://example.com/list-files.schema.json",
				"type": "object",
				"properties": map[string]any{
					"directory": map[string]any{
						"type": "string",
					},
					"patterns": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":      "string",
							"minLength": 1,
						},
						"minItems":    1,
						"maxItems":    100,
						"uniqueItems": true,
					},
					"recursive": map[string]any{
						"type":    "boolean",
						"default": false,
					},
				},
				"required":             []string{"directory"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "search_code",
			"description": "Search code in files",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":      "string",
						"minLength": 1,
						"format":    "regex",
					},
					"max_results": map[string]any{
						"type":             "integer",
						"minimum":          1,
						"maximum":          1000,
						"exclusiveMinimum": 0,
						"default":          100,
					},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
				"examples": []map[string]any{
					{"query": "function.*test", "max_results": 50},
				},
			},
		},
		//
		{
			"name":        "invalid_required_tool",
			"description": "Tool with invalid required field",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type": "string",
					},
				},
				// "nonexistent_field"
				"required": []string{"name", "nonexistent_field"},
			},
		},
		//
		{
			"name":        "no_properties_tool",
			"description": "Tool without properties",
			"input_schema": map[string]any{
				"type":     "object",
				"required": []string{"should_be_removed"},
			},
		},
		//
		{
			"name":        "no_type_tool",
			"description": "Tool without type",
			"input_schema": map[string]any{
				"properties": map[string]any{
					"value": map[string]any{
						"type": "string",
					},
				},
			},
		},
	}

	payload := map[string]any{
		"model":      model,
		"max_tokens": 100,
		"stream":     false,
		"messages": []map[string]string{
			{"role": "user", "content": "List files in the current directory"},
		},
		"tools": tools,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+claudeKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// 400
	if resp.StatusCode == 400 {
		t.Fatalf("schema sanitization failed, received 400 error: %s", string(respBody))
	}

	// 503
	if resp.StatusCode == 503 {
		t.Skipf("account temporarily unavailable (503): %s", string(respBody))
	}

	// 429
	if resp.StatusCode == 429 {
		t.Skipf("request rate limited (429): %s", string(respBody))
	}

	if resp.StatusCode != 200 {
		t.Fatalf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["type"] != "message" {
		t.Errorf("expected type=message, got %v", result["type"])
	}
	t.Logf("✅ complex tool schema test passed, id=%v", result["id"])
}

// TestClaudeMessagesWithThinkingAndTools
//
//
func TestClaudeMessagesWithThinkingAndTools(t *testing.T) {
	claudeKey := requireClaudeAPIKey(t)
	models := []string{
		"claude-haiku-4-5-20251001", // gemini-3-flash
	}
	for i, model := range models {
		if i > 0 {
			time.Sleep(testInterval)
		}
		t.Run(model+"_thinking mode tool invocation", func(t *testing.T) {
			testClaudeThinkingWithToolHistory(t, claudeKey, model)
		})
	}
}

func testClaudeThinkingWithToolHistory(t *testing.T, claudeKey string, model string) {
	url := baseURL + endpointPrefix + "/v1/messages"

	// → assistant → →
	//
	payload := map[string]any{
		"model":      model,
		"max_tokens": 200,
		"stream":     false,
		//
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": 1024,
		},
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "List files in the current directory",
			},
			// assistant
			map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{
						"type": "text",
						"text": "I'll list the files for you.",
					},
					{
						"type":  "tool_use",
						"id":    "toolu_01XGmNv",
						"name":  "Bash",
						"input": map[string]any{"command": "ls -la"},
						//
					},
				},
			},
			map[string]any{
				"role": "user",
				"content": []map[string]any{
					{
						"type":        "tool_result",
						"tool_use_id": "toolu_01XGmNv",
						"content":     "file1.txt\nfile2.txt\ndir1/",
					},
				},
			},
		},
		"tools": []map[string]any{
			{
				"name":        "Bash",
				"description": "Execute bash commands",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{
							"type": "string",
						},
					},
					"required": []string{"command"},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+claudeKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// 400
	if resp.StatusCode == 400 {
		t.Fatalf("thought_signature handling failed, received 400 error: %s", string(respBody))
	}

	// 503
	if resp.StatusCode == 503 {
		t.Skipf("account temporarily unavailable (503): %s", string(respBody))
	}

	// 429
	if resp.StatusCode == 429 {
		t.Skipf("request rate limited (429): %s", string(respBody))
	}

	if resp.StatusCode != 200 {
		t.Fatalf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["type"] != "message" {
		t.Errorf("expected type=message, got %v", result["type"])
	}
	t.Logf("✅ thinking mode tool invocation test passed, id=%v", result["id"])
}

// TestClaudeMessagesWithGeminiModel
//
// ="/antigravity"）
func TestClaudeMessagesWithGeminiModel(t *testing.T) {
	if endpointPrefix != "/antigravity" {
		t.Skip("only runs in Antigravity mode")
	}
	claudeKey := requireClaudeAPIKey(t)

	//
	geminiViaClaude := []string{
		"gemini-3-flash",       // directly supported
		"gemini-3-pro-low",     // directly supported
		"gemini-3-pro-high",    // directly supported
		"gemini-3-pro",         // prefix maps -> gemini-3-pro-high
		"gemini-3-pro-preview", // prefix maps -> gemini-3-pro-high
	}

	for i, model := range geminiViaClaude {
		if i > 0 {
			time.Sleep(testInterval)
		}
		t.Run(model+"_via Claude endpoint", func(t *testing.T) {
			testClaudeMessage(t, claudeKey, model, false)
		})
		time.Sleep(testInterval)
		t.Run(model+"_via Claude endpoint streaming", func(t *testing.T) {
			testClaudeMessage(t, claudeKey, model, true)
		})
	}
}

// TestClaudeMessagesWithNoSignature
//
func TestClaudeMessagesWithNoSignature(t *testing.T) {
	claudeKey := requireClaudeAPIKey(t)
	models := []string{
		"claude-haiku-4-5-20251001", // gemini-3-flash - supports no signature
	}
	for i, model := range models {
		if i > 0 {
			time.Sleep(testInterval)
		}
		t.Run(model+"_no signature", func(t *testing.T) {
			testClaudeWithNoSignature(t, claudeKey, model)
		})
	}
}

func testClaudeWithNoSignature(t *testing.T, claudeKey string, model string) {
	url := baseURL + endpointPrefix + "/v1/messages"

	//
	payload := map[string]any{
		"model":      model,
		"max_tokens": 200,
		"stream":     false,
		//
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": 1024,
		},
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "What is 2+2?",
			},
			// assistant
			map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{
						"type":     "thinking",
						"thinking": "Let me calculate 2+2...",
						//
					},
					{
						"type": "text",
						"text": "2+2 equals 4.",
					},
				},
			},
			map[string]any{
				"role":    "user",
				"content": "What is 3+3?",
			},
		},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+claudeKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 400 {
		t.Fatalf("no-signature thinking handling failed, received 400 error: %s", string(respBody))
	}

	if resp.StatusCode == 503 {
		t.Skipf("account temporarily unavailable (503): %s", string(respBody))
	}

	if resp.StatusCode == 429 {
		t.Skipf("request rate limited (429): %s", string(respBody))
	}

	if resp.StatusCode != 200 {
		t.Fatalf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["type"] != "message" {
		t.Errorf("expected type=message, got %v", result["type"])
	}
	t.Logf("✅ no-signature thinking handling test passed, id=%v", result["id"])
}

// TestGeminiEndpointWithClaudeModel
// ="/antigravity"）
func TestGeminiEndpointWithClaudeModel(t *testing.T) {
	if endpointPrefix != "/antigravity" {
		t.Skip("only runs in Antigravity mode")
	}
	geminiKey := requireGeminiAPIKey(t)

	//
	claudeViaGemini := []string{
		"claude-sonnet-4-5",
		"claude-opus-4-5-thinking",
	}

	for i, model := range claudeViaGemini {
		if i > 0 {
			time.Sleep(testInterval)
		}
		t.Run(model+"_via Gemini endpoint", func(t *testing.T) {
			testGeminiGenerate(t, geminiKey, model, false)
		})
		time.Sleep(testInterval)
		t.Run(model+"_via Gemini endpoint streaming", func(t *testing.T) {
			testGeminiGenerate(t, geminiKey, model, true)
		})
	}
}
