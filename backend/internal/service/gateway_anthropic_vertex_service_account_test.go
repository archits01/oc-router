package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayService_BuildAnthropicVertexServiceAccountRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("Authorization", "Bearer inbound-token")
	c.Request.Header.Set("X-Api-Key", "inbound-api-key")
	c.Request.Header.Set("Anthropic-Version", "2023-06-01")
	c.Request.Header.Set("Anthropic-Beta", "interleaved-thinking-2025-05-14")

	account := &Account{
		ID:       301,
		Platform: PlatformAnthropic,
		Type:     AccountTypeServiceAccount,
		Credentials: map[string]any{
			"project_id": "vertex-proj",
			"location":   "us-east5",
		},
	}
	body := []byte(`{"model":"claude-sonnet-4-5","stream":false,"max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`)

	svc := &GatewayService{}
	req, _, err := svc.buildUpstreamRequest(
		context.Background(),
		c,
		account,
		body,
		"vertex-token",
		"service_account",
		"claude-sonnet-4-5@20250929",
		false,
		false,
	)
	require.NoError(t, err)
	require.Equal(t, "https://us-east5-aiplatform.googleapis.com/v1/projects/vertex-proj/locations/us-east5/publishers/anthropic/models/claude-sonnet-4-5@20250929:rawPredict", req.URL.String())
	require.Equal(t, "Bearer vertex-token", getHeaderRaw(req.Header, "authorization"))
	require.Empty(t, getHeaderRaw(req.Header, "x-api-key"))
	require.Empty(t, getHeaderRaw(req.Header, "anthropic-version"))
	require.Equal(t, "interleaved-thinking-2025-05-14", getHeaderRaw(req.Header, "anthropic-beta"))

	got := readRequestBodyForTest(t, req)
	require.Equal(t, "", gjson.GetBytes(got, "model").String())
	require.Equal(t, vertexAnthropicVersion, gjson.GetBytes(got, "anthropic_version").String())
	require.Equal(t, "hello", gjson.GetBytes(got, "messages.0.content").String())
}

func readRequestBodyForTest(t *testing.T, req *http.Request) []byte {
	t.Helper()
	require.NotNil(t, req.Body)
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	return body
}

// Vertex
// body↔beta header
// → Vertex builder
//
func TestGatewayService_BuildAnthropicVertexServiceAccount_StripsContextManagementWhenBetaMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	//
	c.Request.Header.Set("Anthropic-Beta", "interleaved-thinking-2025-05-14")

	account := &Account{
		ID: 302, Platform: PlatformAnthropic, Type: AccountTypeServiceAccount,
		Credentials: map[string]any{"project_id": "vertex-proj", "location": "us-east5"},
	}
	// body
	body := []byte(`{"model":"claude-haiku-4-5","context_management":{"edits":[{"type":"clear_thinking_20251015","keep":"all"}]},"messages":[{"role":"user","content":"hi"}]}`)

	svc := &GatewayService{}
	req, _, err := svc.buildUpstreamRequest(
		context.Background(), c, account, body,
		"vertex-token", "service_account", "claude-haiku-4-5@20251001", false, false,
	)
	require.NoError(t, err)

	got := readRequestBodyForTest(t, req)
	require.False(t, gjson.GetBytes(got, "context_management").Exists(),
		"when client header lacks context-management beta under Vertex path, must strip body field with same name")
	// header
	outBeta := getHeaderRaw(req.Header, "anthropic-beta")
	require.False(t, anthropicBetaTokensContains(outBeta, "context-management-2025-06-27"),
		"symmetric with body: outgoing anthropic-beta header also does not contain context-management beta")
}

// Vertex
func TestGatewayService_BuildAnthropicVertexServiceAccount_PreservesContextManagementWhenBetaPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("Anthropic-Beta", "interleaved-thinking-2025-05-14,context-management-2025-06-27")

	account := &Account{
		ID: 303, Platform: PlatformAnthropic, Type: AccountTypeServiceAccount,
		Credentials: map[string]any{"project_id": "vertex-proj", "location": "us-east5"},
	}
	body := []byte(`{"model":"claude-sonnet-4-6","context_management":{"edits":[{"type":"clear_thinking_20251015"}]},"messages":[]}`)

	svc := &GatewayService{}
	req, _, err := svc.buildUpstreamRequest(
		context.Background(), c, account, body,
		"vertex-token", "service_account", "claude-sonnet-4-6@20260218", false, false,
	)
	require.NoError(t, err)

	got := readRequestBodyForTest(t, req)
	require.True(t, gjson.GetBytes(got, "context_management").Exists(),
		"when Vertex + client header contains context-management beta, field must be preserved")
	outBeta := getHeaderRaw(req.Header, "anthropic-beta")
	require.True(t, anthropicBetaTokensContains(outBeta, "context-management-2025-06-27"),
		"symmetric with body: outgoing anthropic-beta header also contains context-management beta")
}
