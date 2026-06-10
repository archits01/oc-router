//go:build unit

package antigravity

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// NewAPIRequestWithURL
// ---------------------------------------------------------------------------

func TestNewAPIRequestWithURL_standard request(t *testing.T) {
	ctx := context.Background()
	baseURL := "https://example.com"
	action := "generateContent"
	token := "test-token"
	body := []byte(`{"prompt":"hello"}`)

	req, err := NewAPIRequestWithURL(ctx, baseURL, action, token, body)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}

	// ?alt=sse
	expectedURL := "https://example.com/v1internal:generateContent"
	if req.URL.String() != expectedURL {
		t.Errorf("URL mismatch: got %s, want %s", req.URL.String(), expectedURL)
	}

	if req.Method != http.MethodPost {
		t.Errorf("request method mismatch: got %s, want POST", req.Method)
	}

	//
	if ct := req.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type mismatch: got %s", ct)
	}
	if auth := req.Header.Get("Authorization"); auth != "Bearer test-token" {
		t.Errorf("Authorization mismatch: got %s", auth)
	}
	if ua := req.Header.Get("User-Agent"); ua != GetUserAgent() {
		t.Errorf("User-Agent mismatch: got %s, want %s", ua, GetUserAgent())
	}
}

func TestNewAPIRequestWithURL_streaming request(t *testing.T) {
	ctx := context.Background()
	baseURL := "https://example.com"
	action := "streamGenerateContent"
	token := "tok"
	body := []byte(`{}`)

	req, err := NewAPIRequestWithURL(ctx, baseURL, action, token, body)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}

	expectedURL := "https://example.com/v1internal:streamGenerateContent?alt=sse"
	if req.URL.String() != expectedURL {
		t.Errorf("URL mismatch: got %s, want %s", req.URL.String(), expectedURL)
	}
}

func TestNewAPIRequestWithURL_empty body(t *testing.T) {
	ctx := context.Background()
	req, err := NewAPIRequestWithURL(ctx, "https://example.com", "test", "tok", nil)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	if req.Body == nil {
		t.Error("Body should not be nil (bytes.NewReader(nil) returns an empty reader)")
	}
}

// ---------------------------------------------------------------------------
// NewAPIRequest
// ---------------------------------------------------------------------------

func TestNewAPIRequest_using default URL(t *testing.T) {
	ctx := context.Background()
	req, err := NewAPIRequest(ctx, "generateContent", "tok", []byte(`{}`))
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}

	expected := BaseURL + "/v1internal:generateContent"
	if req.URL.String() != expected {
		t.Errorf("URL mismatch: got %s, want %s", req.URL.String(), expected)
	}
}

// ---------------------------------------------------------------------------
// TierInfo.UnmarshalJSON
// ---------------------------------------------------------------------------

func TestTierInfo_UnmarshalJSON_string format(t *testing.T) {
	data := []byte(`"free-tier"`)
	var tier TierInfo
	if err := tier.UnmarshalJSON(data); err != nil {
		t.Fatalf("deserialization failed: %v", err)
	}
	if tier.ID != "free-tier" {
		t.Errorf("ID mismatch: got %s, want free-tier", tier.ID)
	}
	if tier.Name != "" {
		t.Errorf("Name should be empty: got %s", tier.Name)
	}
}

func TestTierInfo_UnmarshalJSON_object format(t *testing.T) {
	data := []byte(`{"id":"g1-pro-tier","name":"Pro","description":"Pro plan"}`)
	var tier TierInfo
	if err := tier.UnmarshalJSON(data); err != nil {
		t.Fatalf("deserialization failed: %v", err)
	}
	if tier.ID != "g1-pro-tier" {
		t.Errorf("ID mismatch: got %s, want g1-pro-tier", tier.ID)
	}
	if tier.Name != "Pro" {
		t.Errorf("Name mismatch: got %s, want Pro", tier.Name)
	}
	if tier.Description != "Pro plan" {
		t.Errorf("Description mismatch: got %s, want Pro plan", tier.Description)
	}
}

func TestTierInfo_UnmarshalJSON_null(t *testing.T) {
	data := []byte(`null`)
	var tier TierInfo
	if err := tier.UnmarshalJSON(data); err != nil {
		t.Fatalf("null deserialization failed: %v", err)
	}
	if tier.ID != "" {
		t.Errorf("ID should be empty in null case: got %s", tier.ID)
	}
}

func TestTierInfo_UnmarshalJSON_empty data(t *testing.T) {
	data := []byte(``)
	var tier TierInfo
	if err := tier.UnmarshalJSON(data); err != nil {
		t.Fatalf("empty data deserialization failed: %v", err)
	}
	if tier.ID != "" {
		t.Errorf("ID should be empty in empty data case: got %s", tier.ID)
	}
}

func TestTierInfo_UnmarshalJSON_whitespace-wrapped null(t *testing.T) {
	data := []byte(`  null  `)
	var tier TierInfo
	if err := tier.UnmarshalJSON(data); err != nil {
		t.Fatalf("whitespace null deserialization failed: %v", err)
	}
	if tier.ID != "" {
		t.Errorf("ID should be empty in whitespace null case: got %s", tier.ID)
	}
}

func TestTierInfo_UnmarshalJSON_via JSON nested structure(t *testing.T) {
	//
	jsonData := `{"currentTier":"free-tier","paidTier":{"id":"g1-ultra-tier","name":"Ultra"}}`
	var resp LoadCodeAssistResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("nested structure deserialization failed: %v", err)
	}
	if resp.CurrentTier == nil || resp.CurrentTier.ID != "free-tier" {
		t.Errorf("CurrentTier mismatch: got %+v", resp.CurrentTier)
	}
	if resp.PaidTier == nil || resp.PaidTier.ID != "g1-ultra-tier" {
		t.Errorf("PaidTier mismatch: got %+v", resp.PaidTier)
	}
}

// ---------------------------------------------------------------------------
// LoadCodeAssistResponse.GetTier
// ---------------------------------------------------------------------------

func TestGetTier_PaidTier takes priority(t *testing.T) {
	resp := &LoadCodeAssistResponse{
		CurrentTier: &TierInfo{ID: "free-tier"},
		PaidTier:    &PaidTierInfo{ID: "g1-pro-tier"},
	}
	if got := resp.GetTier(); got != "g1-pro-tier" {
		t.Errorf("should return paidTier: got %s", got)
	}
}

func TestGetTier_fallback to CurrentTier(t *testing.T) {
	resp := &LoadCodeAssistResponse{
		CurrentTier: &TierInfo{ID: "free-tier"},
	}
	if got := resp.GetTier(); got != "free-tier" {
		t.Errorf("should return currentTier: got %s", got)
	}
}

func TestGetTier_PaidTier with empty ID(t *testing.T) {
	resp := &LoadCodeAssistResponse{
		CurrentTier: &TierInfo{ID: "free-tier"},
		PaidTier:    &PaidTierInfo{ID: ""},
	}
	// paidTier.ID
	if got := resp.GetTier(); got != "free-tier" {
		t.Errorf("should fallback to currentTier when paidTier.ID is empty: got %s", got)
	}
}

func TestGetAvailableCredits(t *testing.T) {
	resp := &LoadCodeAssistResponse{
		PaidTier: &PaidTierInfo{
			ID: "g1-pro-tier",
			AvailableCredits: []AvailableCredit{
				{
					CreditType:                  "GOOGLE_ONE_AI",
					CreditAmount:                "25",
					MinimumCreditAmountForUsage: "5",
				},
			},
		},
	}

	credits := resp.GetAvailableCredits()
	if len(credits) != 1 {
		t.Fatalf("AI Credits count mismatch: got %d", len(credits))
	}
	if credits[0].GetAmount() != 25 {
		t.Errorf("CreditAmount parsed incorrectly: got %v", credits[0].GetAmount())
	}
	if credits[0].GetMinimumAmount() != 5 {
		t.Errorf("MinimumCreditAmountForUsage parsed incorrectly: got %v", credits[0].GetMinimumAmount())
	}
}

func TestGetTier_both are nil(t *testing.T) {
	resp := &LoadCodeAssistResponse{}
	if got := resp.GetTier(); got != "" {
		t.Errorf("should return empty string when both are nil: got %s", got)
	}
}

func TestTierIDToPlanType(t *testing.T) {
	tests := []struct {
		tierID string
		want   string
	}{
		{"free-tier", "Free"},
		{"g1-pro-tier", "Pro"},
		{"g1-ultra-tier", "Ultra"},
		{"FREE-TIER", "Free"},
		{"", "Free"},
		{"unknown-tier", "unknown-tier"},
	}
	for _, tt := range tests {
		t.Run(tt.tierID, func(t *testing.T) {
			if got := TierIDToPlanType(tt.tierID); got != tt.want {
				t.Errorf("TierIDToPlanType(%q) = %q, want %q", tt.tierID, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NewClient
// ---------------------------------------------------------------------------

func mustNewClient(t *testing.T, proxyURL string) *Client {
	t.Helper()
	client, err := NewClient(proxyURL)
	if err != nil {
		t.Fatalf("NewClient(%q) failed: %v", proxyURL, err)
	}
	return client
}

func TestNewClient_no proxy(t *testing.T) {
	client, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.httpClient == nil {
		t.Fatal("httpClient is nil")
	}
	if client.httpClient.Timeout != clientTimeout {
		t.Errorf("Timeout mismatch: got %v, want %v", client.httpClient.Timeout, clientTimeout)
	}
	//
	if client.httpClient.Transport != nil {
		t.Error("Transport should be nil without proxy")
	}
}

func TestNewClient_with proxy(t *testing.T) {
	client, err := NewClient("http://proxy.example.com:8080")
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.httpClient.Transport == nil {
		t.Fatal("Transport should not be nil with proxy")
	}
}

func TestNewClient_whitespace proxy(t *testing.T) {
	client, err := NewClient("   ")
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.httpClient.Transport != nil {
		t.Error("Transport should be nil for whitespace proxy")
	}
}

func TestNewClient_invalid proxy URL(t *testing.T) {
	//
	_, err := NewClient("://invalid")
	if err == nil {
		t.Fatal("invalid proxy URL should return error")
	}
	if !strings.Contains(err.Error(), "invalid proxy URL") {
		t.Errorf("error message should contain 'invalid proxy URL': got %s", err.Error())
	}
}

// ---------------------------------------------------------------------------
// IsConnectionError
// ---------------------------------------------------------------------------

func TestIsConnectionError_nil(t *testing.T) {
	if IsConnectionError(nil) {
		t.Error("nil error should not be classified as connection error")
	}
}

func TestIsConnectionError_timeout error(t *testing.T) {
	//
	err := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &timeoutError{},
	}
	if !IsConnectionError(err) {
		t.Error("timeout error should be classified as connection error")
	}
}

// timeoutError
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

func TestIsConnectionError_netOpError(t *testing.T) {
	err := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: fmt.Errorf("connection refused"),
	}
	if !IsConnectionError(err) {
		t.Error("net.OpError should be classified as connection error")
	}
}

func TestIsConnectionError_urlError(t *testing.T) {
	err := &url.Error{
		Op:  "Get",
		URL: "https://example.com",
		Err: fmt.Errorf("some error"),
	}
	if !IsConnectionError(err) {
		t.Error("url.Error should be classified as connection error")
	}
}

func TestIsConnectionError_ordinary error(t *testing.T) {
	err := fmt.Errorf("some random error")
	if IsConnectionError(err) {
		t.Error("ordinary error should not be classified as connection error")
	}
}

func TestIsConnectionError_wrapped netOpError(t *testing.T) {
	inner := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: fmt.Errorf("connection refused"),
	}
	err := fmt.Errorf("wrapping: %w", inner)
	if !IsConnectionError(err) {
		t.Error("wrapped net.OpError should be classified as connection error")
	}
}

// ---------------------------------------------------------------------------
// shouldFallbackToNextURL
// ---------------------------------------------------------------------------

func TestShouldFallbackToNextURL_connection error(t *testing.T) {
	err := &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("refused")}
	if !shouldFallbackToNextURL(err, 0) {
		t.Error("connection error should trigger URL fallback")
	}
}

func TestShouldFallbackToNextURL_status code(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{"429 Too Many Requests", http.StatusTooManyRequests, true},
		{"408 Request Timeout", http.StatusRequestTimeout, true},
		{"404 Not Found", http.StatusNotFound, true},
		{"500 Internal Server Error", http.StatusInternalServerError, true},
		{"502 Bad Gateway", http.StatusBadGateway, true},
		{"503 Service Unavailable", http.StatusServiceUnavailable, true},
		{"200 OK", http.StatusOK, false},
		{"201 Created", http.StatusCreated, false},
		{"400 Bad Request", http.StatusBadRequest, false},
		{"401 Unauthorized", http.StatusUnauthorized, false},
		{"403 Forbidden", http.StatusForbidden, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldFallbackToNextURL(nil, tt.statusCode)
			if got != tt.want {
				t.Errorf("shouldFallbackToNextURL(nil, %d) = %v, want %v", tt.statusCode, got, tt.want)
			}
		})
	}
}

func TestShouldFallbackToNextURL_no error with 200(t *testing.T) {
	if shouldFallbackToNextURL(nil, http.StatusOK) {
		t.Error("no error with 200 should not trigger URL fallback")
	}
}

// ---------------------------------------------------------------------------
// Client.ExchangeCode ()
// ---------------------------------------------------------------------------

func TestClient_ExchangeCode_success(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "test-secret"
	t.Cleanup(func() { defaultClientSecret = old })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method mismatch: got %s", r.Method)
		}
		//
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type mismatch: got %s", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("form parsing failed: %v", err)
		}
		if r.FormValue("client_id") != ClientID {
			t.Errorf("client_id mismatch: got %s", r.FormValue("client_id"))
		}
		if r.FormValue("client_secret") != "test-secret" {
			t.Errorf("client_secret mismatch: got %s", r.FormValue("client_secret"))
		}
		if r.FormValue("code") != "auth-code" {
			t.Errorf("code mismatch: got %s", r.FormValue("code"))
		}
		if r.FormValue("code_verifier") != "verifier123" {
			t.Errorf("code_verifier mismatch: got %s", r.FormValue("code_verifier"))
		}
		if r.FormValue("grant_type") != "authorization_code" {
			t.Errorf("grant_type mismatch: got %s", r.FormValue("grant_type"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "access-tok",
			ExpiresIn:    3600,
			TokenType:    "Bearer",
			RefreshToken: "refresh-tok",
		})
	}))
	defer server.Close()

	//
	//
	//
	client := &Client{httpClient: server.Client()}

	//
	//
	originalTokenURL := TokenURL
	_ = originalTokenURL
	_ = client

	//
	ctx := context.Background()
	params := url.Values{}
	params.Set("client_id", ClientID)
	params.Set("client_secret", "test-secret")
	params.Set("code", "auth-code")
	params.Set("redirect_uri", RedirectURI)
	params.Set("grant_type", "authorization_code")
	params.Set("code_verifier", "verifier123")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(params.Encode()))
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code mismatch: got %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("decoding failed: %v", err)
	}
	if tokenResp.AccessToken != "access-tok" {
		t.Errorf("AccessToken mismatch: got %s", tokenResp.AccessToken)
	}
	if tokenResp.RefreshToken != "refresh-tok" {
		t.Errorf("RefreshToken mismatch: got %s", tokenResp.RefreshToken)
	}
}

func TestClient_ExchangeCode_no ClientSecret(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = ""
	t.Cleanup(func() { defaultClientSecret = old })

	client := mustNewClient(t, "")
	_, err := client.ExchangeCode(context.Background(), "code", "verifier")
	if err == nil {
		t.Fatal("should return error when client_secret is missing")
	}
	if !strings.Contains(err.Error(), AntigravityOAuthClientSecretEnv) {
		t.Errorf("error message should contain environment variable name: got %s", err.Error())
	}
}

func TestClient_ExchangeCode_server returned error(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "test-secret"
	t.Cleanup(func() { defaultClientSecret = old })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()

	//
	resp, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status code mismatch: got %d, want 400", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Client.RefreshToken ()
// ---------------------------------------------------------------------------

func TestClient_RefreshToken_MockServer(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "test-secret"
	t.Cleanup(func() { defaultClientSecret = old })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method mismatch: got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("form parsing failed: %v", err)
		}
		if r.FormValue("grant_type") != "refresh_token" {
			t.Errorf("grant_type mismatch: got %s", r.FormValue("grant_type"))
		}
		if r.FormValue("refresh_token") != "old-refresh-tok" {
			t.Errorf("refresh_token mismatch: got %s", r.FormValue("refresh_token"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "new-access-tok",
			ExpiresIn:   3600,
			TokenType:   "Bearer",
		})
	}))
	defer server.Close()

	ctx := context.Background()
	params := url.Values{}
	params.Set("client_id", ClientID)
	params.Set("client_secret", "test-secret")
	params.Set("refresh_token", "old-refresh-tok")
	params.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(params.Encode()))
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code mismatch: got %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("decoding failed: %v", err)
	}
	if tokenResp.AccessToken != "new-access-tok" {
		t.Errorf("AccessToken mismatch: got %s", tokenResp.AccessToken)
	}
}

func TestClient_RefreshToken_no ClientSecret(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = ""
	t.Cleanup(func() { defaultClientSecret = old })

	client := mustNewClient(t, "")
	_, err := client.RefreshToken(context.Background(), "refresh-tok")
	if err == nil {
		t.Fatal("should return error when client_secret is missing")
	}
}

// ---------------------------------------------------------------------------
// Client.GetUserInfo ()
// ---------------------------------------------------------------------------

func TestClient_GetUserInfo_success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method mismatch: got %s", r.Method)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-access-token" {
			t.Errorf("Authorization mismatch: got %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(UserInfo{
			Email:      "user@example.com",
			Name:       "Test User",
			GivenName:  "Test",
			FamilyName: "User",
			Picture:    "https://example.com/photo.jpg",
		})
	}))
	defer server.Close()

	//
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-access-token")

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code mismatch: got %d", resp.StatusCode)
	}

	var userInfo UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		t.Fatalf("decoding failed: %v", err)
	}
	if userInfo.Email != "user@example.com" {
		t.Errorf("Email mismatch: got %s", userInfo.Email)
	}
	if userInfo.Name != "Test User" {
		t.Errorf("Name mismatch: got %s", userInfo.Name)
	}
}

func TestClient_GetUserInfo_server returned error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	defer server.Close()

	resp, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status code mismatch: got %d, want 401", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// TokenResponse / UserInfo JSON
// ---------------------------------------------------------------------------

func TestTokenResponse_JSON serialization(t *testing.T) {
	jsonData := `{"access_token":"at","expires_in":3600,"token_type":"Bearer","scope":"openid","refresh_token":"rt"}`
	var resp TokenResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("deserialization failed: %v", err)
	}
	if resp.AccessToken != "at" {
		t.Errorf("AccessToken mismatch: got %s", resp.AccessToken)
	}
	if resp.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn mismatch: got %d", resp.ExpiresIn)
	}
	if resp.RefreshToken != "rt" {
		t.Errorf("RefreshToken mismatch: got %s", resp.RefreshToken)
	}
}

func TestUserInfo_JSON serialization(t *testing.T) {
	jsonData := `{"email":"a@b.com","name":"Alice"}`
	var info UserInfo
	if err := json.Unmarshal([]byte(jsonData), &info); err != nil {
		t.Fatalf("deserialization failed: %v", err)
	}
	if info.Email != "a@b.com" {
		t.Errorf("Email mismatch: got %s", info.Email)
	}
	if info.Name != "Alice" {
		t.Errorf("Name mismatch: got %s", info.Name)
	}
}

// ---------------------------------------------------------------------------
// LoadCodeAssistResponse JSON
// ---------------------------------------------------------------------------

func TestLoadCodeAssistResponse_complete JSON(t *testing.T) {
	jsonData := `{
		"cloudaicompanionProject": "proj-123",
		"currentTier": "free-tier",
		"paidTier": {"id": "g1-pro-tier", "name": "Pro"},
		"ineligibleTiers": [{"tier": {"id": "g1-ultra-tier"}, "reasonCode": "INELIGIBLE_ACCOUNT"}]
	}`
	var resp LoadCodeAssistResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("deserialization failed: %v", err)
	}
	if resp.CloudAICompanionProject != "proj-123" {
		t.Errorf("CloudAICompanionProject mismatch: got %s", resp.CloudAICompanionProject)
	}
	if resp.GetTier() != "g1-pro-tier" {
		t.Errorf("GetTier mismatch: got %s", resp.GetTier())
	}
	if len(resp.IneligibleTiers) != 1 {
		t.Fatalf("IneligibleTiers count mismatch: got %d", len(resp.IneligibleTiers))
	}
	if resp.IneligibleTiers[0].ReasonCode != "INELIGIBLE_ACCOUNT" {
		t.Errorf("ReasonCode mismatch: got %s", resp.IneligibleTiers[0].ReasonCode)
	}
}

// ===========================================================================
//
// ===========================================================================

// redirectRoundTripper
type redirectRoundTripper struct {
	// >
	redirects map[string]string
	transport http.RoundTripper
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (rt *redirectRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	originalURL := req.URL.String()
	for prefix, target := range rt.redirects {
		if strings.HasPrefix(originalURL, prefix) {
			newURL := target + strings.TrimPrefix(originalURL, prefix)
			parsed, err := url.Parse(newURL)
			if err != nil {
				return nil, err
			}
			req.URL = parsed
			break
		}
	}
	if rt.transport == nil {
		return http.DefaultTransport.RoundTrip(req)
	}
	return rt.transport.RoundTrip(req)
}

// newTestClientWithRedirect
func newTestClientWithRedirect(redirects map[string]string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &redirectRoundTripper{
				redirects: redirects,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Client.ExchangeCode -
// ---------------------------------------------------------------------------

func TestClient_ExchangeCode_Success_RealCall(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "test-secret"
	t.Cleanup(func() { defaultClientSecret = old })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method mismatch: got %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type mismatch: got %s", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("form parsing failed: %v", err)
		}
		if r.FormValue("client_id") != ClientID {
			t.Errorf("client_id mismatch: got %s", r.FormValue("client_id"))
		}
		if r.FormValue("client_secret") != "test-secret" {
			t.Errorf("client_secret mismatch: got %s", r.FormValue("client_secret"))
		}
		if r.FormValue("code") != "test-auth-code" {
			t.Errorf("code mismatch: got %s", r.FormValue("code"))
		}
		if r.FormValue("code_verifier") != "test-verifier" {
			t.Errorf("code_verifier mismatch: got %s", r.FormValue("code_verifier"))
		}
		if r.FormValue("grant_type") != "authorization_code" {
			t.Errorf("grant_type mismatch: got %s", r.FormValue("grant_type"))
		}
		if r.FormValue("redirect_uri") != RedirectURI {
			t.Errorf("redirect_uri mismatch: got %s", r.FormValue("redirect_uri"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "new-access-token",
			ExpiresIn:    3600,
			TokenType:    "Bearer",
			Scope:        "openid email",
			RefreshToken: "new-refresh-token",
		})
	}))
	defer server.Close()

	client := newTestClientWithRedirect(map[string]string{
		TokenURL: server.URL,
	})

	tokenResp, err := client.ExchangeCode(context.Background(), "test-auth-code", "test-verifier")
	if err != nil {
		t.Fatalf("ExchangeCode failed: %v", err)
	}
	if tokenResp.AccessToken != "new-access-token" {
		t.Errorf("AccessToken mismatch: got %s, want new-access-token", tokenResp.AccessToken)
	}
	if tokenResp.RefreshToken != "new-refresh-token" {
		t.Errorf("RefreshToken mismatch: got %s, want new-refresh-token", tokenResp.RefreshToken)
	}
	if tokenResp.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn mismatch: got %d, want 3600", tokenResp.ExpiresIn)
	}
	if tokenResp.TokenType != "Bearer" {
		t.Errorf("TokenType mismatch: got %s, want Bearer", tokenResp.TokenType)
	}
	if tokenResp.Scope != "openid email" {
		t.Errorf("Scope mismatch: got %s, want openid email", tokenResp.Scope)
	}
}

func TestClient_ExchangeCode_ServerError_RealCall(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "test-secret"
	t.Cleanup(func() { defaultClientSecret = old })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"code expired"}`))
	}))
	defer server.Close()

	client := newTestClientWithRedirect(map[string]string{
		TokenURL: server.URL,
	})

	_, err := client.ExchangeCode(context.Background(), "expired-code", "verifier")
	if err == nil {
		t.Fatal("should return error when server returns 400")
	}
	if !strings.Contains(err.Error(), "token exchange failed") {
		t.Errorf("error message should contain 'token exchange failed': got %s", err.Error())
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error message should containstatus code 400: got %s", err.Error())
	}
}

func TestClient_ExchangeCode_InvalidJSON_RealCall(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "test-secret"
	t.Cleanup(func() { defaultClientSecret = old })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	client := newTestClientWithRedirect(map[string]string{
		TokenURL: server.URL,
	})

	_, err := client.ExchangeCode(context.Background(), "code", "verifier")
	if err == nil {
		t.Fatal("invalid JSON response should return error")
	}
	if !strings.Contains(err.Error(), "token parsefailed") {
		t.Errorf("error message should contain 'token parsefailed': got %s", err.Error())
	}
}

func TestClient_ExchangeCode_ContextCanceled_RealCall(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "test-secret"
	t.Cleanup(func() { defaultClientSecret = old })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // simulate slow response
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClientWithRedirect(map[string]string{
		TokenURL: server.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	_, err := client.ExchangeCode(ctx, "code", "verifier")
	if err == nil {
		t.Fatal("should return error when context is cancelled")
	}
}

// ---------------------------------------------------------------------------
// Client.RefreshToken -
// ---------------------------------------------------------------------------

func TestClient_RefreshToken_Success_RealCall(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "test-secret"
	t.Cleanup(func() { defaultClientSecret = old })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method mismatch: got %s, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("form parsing failed: %v", err)
		}
		if r.FormValue("grant_type") != "refresh_token" {
			t.Errorf("grant_type mismatch: got %s", r.FormValue("grant_type"))
		}
		if r.FormValue("refresh_token") != "my-refresh-token" {
			t.Errorf("refresh_token mismatch: got %s", r.FormValue("refresh_token"))
		}
		if r.FormValue("client_id") != ClientID {
			t.Errorf("client_id mismatch: got %s", r.FormValue("client_id"))
		}
		if r.FormValue("client_secret") != "test-secret" {
			t.Errorf("client_secret mismatch: got %s", r.FormValue("client_secret"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "refreshed-access-token",
			ExpiresIn:   3600,
			TokenType:   "Bearer",
		})
	}))
	defer server.Close()

	client := newTestClientWithRedirect(map[string]string{
		TokenURL: server.URL,
	})

	tokenResp, err := client.RefreshToken(context.Background(), "my-refresh-token")
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if tokenResp.AccessToken != "refreshed-access-token" {
		t.Errorf("AccessToken mismatch: got %s, want refreshed-access-token", tokenResp.AccessToken)
	}
	if tokenResp.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn mismatch: got %d, want 3600", tokenResp.ExpiresIn)
	}
}

func TestClient_RefreshToken_ServerError_RealCall(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "test-secret"
	t.Cleanup(func() { defaultClientSecret = old })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"token revoked"}`))
	}))
	defer server.Close()

	client := newTestClientWithRedirect(map[string]string{
		TokenURL: server.URL,
	})

	_, err := client.RefreshToken(context.Background(), "revoked-token")
	if err == nil {
		t.Fatal("should return error when server returns 401")
	}
	if !strings.Contains(err.Error(), "token refresh failed") {
		t.Errorf("error message should contain 'token refresh failed': got %s", err.Error())
	}
}

func TestClient_RefreshToken_InvalidJSON_RealCall(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "test-secret"
	t.Cleanup(func() { defaultClientSecret = old })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	client := newTestClientWithRedirect(map[string]string{
		TokenURL: server.URL,
	})

	_, err := client.RefreshToken(context.Background(), "refresh-tok")
	if err == nil {
		t.Fatal("invalid JSON response should return error")
	}
	if !strings.Contains(err.Error(), "token parsefailed") {
		t.Errorf("error message should contain 'token parsefailed': got %s", err.Error())
	}
}

func TestClient_RefreshToken_ContextCanceled_RealCall(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "test-secret"
	t.Cleanup(func() { defaultClientSecret = old })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClientWithRedirect(map[string]string{
		TokenURL: server.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.RefreshToken(ctx, "refresh-tok")
	if err == nil {
		t.Fatal("should return error when context is cancelled")
	}
}

// ---------------------------------------------------------------------------
// Client.GetUserInfo -
// ---------------------------------------------------------------------------

func TestClient_GetUserInfo_Success_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method mismatch: got %s, want GET", r.Method)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer user-access-token" {
			t.Errorf("Authorization mismatch: got %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(UserInfo{
			Email:      "test@example.com",
			Name:       "Test User",
			GivenName:  "Test",
			FamilyName: "User",
			Picture:    "https://example.com/avatar.jpg",
		})
	}))
	defer server.Close()

	client := newTestClientWithRedirect(map[string]string{
		UserInfoURL: server.URL,
	})

	userInfo, err := client.GetUserInfo(context.Background(), "user-access-token")
	if err != nil {
		t.Fatalf("GetUserInfo failed: %v", err)
	}
	if userInfo.Email != "test@example.com" {
		t.Errorf("Email mismatch: got %s, want test@example.com", userInfo.Email)
	}
	if userInfo.Name != "Test User" {
		t.Errorf("Name mismatch: got %s, want Test User", userInfo.Name)
	}
	if userInfo.GivenName != "Test" {
		t.Errorf("GivenName mismatch: got %s, want Test", userInfo.GivenName)
	}
	if userInfo.FamilyName != "User" {
		t.Errorf("FamilyName mismatch: got %s, want User", userInfo.FamilyName)
	}
	if userInfo.Picture != "https://example.com/avatar.jpg" {
		t.Errorf("Picture mismatch: got %s", userInfo.Picture)
	}
}

func TestClient_GetUserInfo_Unauthorized_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	defer server.Close()

	client := newTestClientWithRedirect(map[string]string{
		UserInfoURL: server.URL,
	})

	_, err := client.GetUserInfo(context.Background(), "bad-token")
	if err == nil {
		t.Fatal("should return error when server returns 401")
	}
	if !strings.Contains(err.Error(), "get userinfo failed") {
		t.Errorf("error message should contain 'get userinfo failed': got %s", err.Error())
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error message should containstatus code 401: got %s", err.Error())
	}
}

func TestClient_GetUserInfo_InvalidJSON_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{broken`))
	}))
	defer server.Close()

	client := newTestClientWithRedirect(map[string]string{
		UserInfoURL: server.URL,
	})

	_, err := client.GetUserInfo(context.Background(), "token")
	if err == nil {
		t.Fatal("invalid JSON response should return error")
	}
	if !strings.Contains(err.Error(), "userinfoparsefailed") {
		t.Errorf("error message should contain 'userinfoparsefailed': got %s", err.Error())
	}
}

func TestClient_GetUserInfo_ContextCanceled_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClientWithRedirect(map[string]string{
		UserInfoURL: server.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.GetUserInfo(ctx, "token")
	if err == nil {
		t.Fatal("should return error when context is cancelled")
	}
}

// ---------------------------------------------------------------------------
// Client.LoadCodeAssist -
// ---------------------------------------------------------------------------

// withMockBaseURLs
func withMockBaseURLs(t *testing.T, urls []string) {
	t.Helper()
	origBaseURLs := BaseURLs
	origBaseURL := BaseURL
	BaseURLs = urls
	if len(urls) > 0 {
		BaseURL = urls[0]
	}
	t.Cleanup(func() {
		BaseURLs = origBaseURLs
		BaseURL = origBaseURL
	})
}

func TestClient_LoadCodeAssist_Success_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method mismatch: got %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v1internal:loadCodeAssist") {
			t.Errorf("URL path mismatch: got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Authorization mismatch: got %s", auth)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type mismatch: got %s", ct)
		}
		if ua := r.Header.Get("User-Agent"); ua != GetUserAgent() {
			t.Errorf("User-Agent mismatch: got %s", ua)
		}

		var reqBody LoadCodeAssistRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("parse request body failed: %v", err)
		}
		if reqBody.Metadata.IDEType != "ANTIGRAVITY" {
			t.Errorf("IDEType mismatch: got %s, want ANTIGRAVITY", reqBody.Metadata.IDEType)
		}
		if strings.TrimSpace(reqBody.Metadata.IDEVersion) == "" {
			t.Errorf("IDEVersion should not be empty")
		}
		if reqBody.Metadata.IDEName != "antigravity" {
			t.Errorf("IDEName mismatch: got %s, want antigravity", reqBody.Metadata.IDEName)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"cloudaicompanionProject": "test-project-123",
			"currentTier": {"id": "free-tier", "name": "Free"},
			"paidTier": {"id": "g1-pro-tier", "name": "Pro", "description": "Pro plan"}
		}`))
	}))
	defer server.Close()

	withMockBaseURLs(t, []string{server.URL})

	client := mustNewClient(t, "")
	resp, rawResp, err := client.LoadCodeAssist(context.Background(), "test-token")
	if err != nil {
		t.Fatalf("LoadCodeAssist failed: %v", err)
	}
	if resp.CloudAICompanionProject != "test-project-123" {
		t.Errorf("CloudAICompanionProject mismatch: got %s", resp.CloudAICompanionProject)
	}
	if resp.GetTier() != "g1-pro-tier" {
		t.Errorf("GetTier mismatch: got %s, want g1-pro-tier", resp.GetTier())
	}
	if resp.CurrentTier == nil || resp.CurrentTier.ID != "free-tier" {
		t.Errorf("CurrentTier mismatch: got %+v", resp.CurrentTier)
	}
	if resp.PaidTier == nil || resp.PaidTier.ID != "g1-pro-tier" {
		t.Errorf("PaidTier mismatch: got %+v", resp.PaidTier)
	}
	//
	if rawResp == nil {
		t.Fatal("rawResp should not be nil")
	}
	if rawResp["cloudaicompanionProject"] != "test-project-123" {
		t.Errorf("rawResp cloudaicompanionProject mismatch: got %v", rawResp["cloudaicompanionProject"])
	}
}

func TestClient_LoadCodeAssist_HTTPError_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer server.Close()

	withMockBaseURLs(t, []string{server.URL})

	client := mustNewClient(t, "")
	_, _, err := client.LoadCodeAssist(context.Background(), "bad-token")
	if err == nil {
		t.Fatal("should return error when server returns 403")
	}
	if !strings.Contains(err.Error(), "loadCodeAssist failed") {
		t.Errorf("error message should contain 'loadCodeAssist failed': got %s", err.Error())
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error message should containstatus code 403: got %s", err.Error())
	}
}

func TestClient_LoadCodeAssist_InvalidJSON_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json!!!`))
	}))
	defer server.Close()

	withMockBaseURLs(t, []string{server.URL})

	client := mustNewClient(t, "")
	_, _, err := client.LoadCodeAssist(context.Background(), "token")
	if err == nil {
		t.Fatal("invalid JSON response should return error")
	}
	if !strings.Contains(err.Error(), "response parse failed") {
		t.Errorf("error message should contain 'response parse failed': got %s", err.Error())
	}
}

func TestClient_LoadCodeAssist_URLFallback_RealCall(t *testing.T) {
	//
	callCount := 0
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"cloudaicompanionProject": "fallback-project",
			"currentTier": {"id": "free-tier", "name": "Free"}
		}`))
	}))
	defer server2.Close()

	withMockBaseURLs(t, []string{server1.URL, server2.URL})

	client := mustNewClient(t, "")
	resp, _, err := client.LoadCodeAssist(context.Background(), "token")
	if err != nil {
		t.Fatalf("LoadCodeAssist should succeed after fallback: %v", err)
	}
	if resp.CloudAICompanionProject != "fallback-project" {
		t.Errorf("CloudAICompanionProject mismatch: got %s", resp.CloudAICompanionProject)
	}
	if callCount != 2 {
		t.Errorf("should have called 2 servers, actual calls: %d", callCount)
	}
}

func TestClient_LoadCodeAssist_AllURLsFail_RealCall(t *testing.T) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"unavailable"}`))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"bad_gateway"}`))
	}))
	defer server2.Close()

	withMockBaseURLs(t, []string{server1.URL, server2.URL})

	client := mustNewClient(t, "")
	_, _, err := client.LoadCodeAssist(context.Background(), "token")
	if err == nil {
		t.Fatal("should return error when all URLs fail")
	}
}

func TestClient_LoadCodeAssist_ContextCanceled_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	withMockBaseURLs(t, []string{server.URL})

	client := mustNewClient(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := client.LoadCodeAssist(ctx, "token")
	if err == nil {
		t.Fatal("should return error when context is cancelled")
	}
}

// ---------------------------------------------------------------------------
// Client.FetchAvailableModels -
// ---------------------------------------------------------------------------

func TestClient_FetchAvailableModels_Success_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method mismatch: got %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v1internal:fetchAvailableModels") {
			t.Errorf("URL path mismatch: got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Authorization mismatch: got %s", auth)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type mismatch: got %s", ct)
		}
		if ua := r.Header.Get("User-Agent"); ua != GetUserAgent() {
			t.Errorf("User-Agent mismatch: got %s", ua)
		}

		var reqBody FetchAvailableModelsRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("parse request body failed: %v", err)
		}
		if reqBody.Project != "project-abc" {
			t.Errorf("Project mismatch: got %s, want project-abc", reqBody.Project)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"models": {
				"gemini-2.0-flash": {
					"quotaInfo": {
						"remainingFraction": 0.85,
						"resetTime": "2025-01-01T00:00:00Z"
					}
				},
				"gemini-2.5-pro": {
					"quotaInfo": {
						"remainingFraction": 0.5
					}
				}
			}
		}`))
	}))
	defer server.Close()

	withMockBaseURLs(t, []string{server.URL})

	client := mustNewClient(t, "")
	resp, rawResp, err := client.FetchAvailableModels(context.Background(), "test-token", "project-abc")
	if err != nil {
		t.Fatalf("FetchAvailableModels failed: %v", err)
	}
	if resp.Models == nil {
		t.Fatal("Models should not be nil")
	}
	if len(resp.Models) != 2 {
		t.Errorf("Models count mismatch: got %d, want 2", len(resp.Models))
	}

	flashModel, ok := resp.Models["gemini-2.0-flash"]
	if !ok {
		t.Fatal("missing gemini-2.0-flash model")
	}
	if flashModel.QuotaInfo == nil {
		t.Fatal("gemini-2.0-flash QuotaInfo should not be nil")
	}
	if flashModel.QuotaInfo.RemainingFraction != 0.85 {
		t.Errorf("RemainingFraction mismatch: got %f, want 0.85", flashModel.QuotaInfo.RemainingFraction)
	}
	if flashModel.QuotaInfo.ResetTime != "2025-01-01T00:00:00Z" {
		t.Errorf("ResetTime mismatch: got %s", flashModel.QuotaInfo.ResetTime)
	}

	proModel, ok := resp.Models["gemini-2.5-pro"]
	if !ok {
		t.Fatal("missing gemini-2.5-pro model")
	}
	if proModel.QuotaInfo == nil {
		t.Fatal("gemini-2.5-pro QuotaInfo should not be nil")
	}
	if proModel.QuotaInfo.RemainingFraction != 0.5 {
		t.Errorf("RemainingFraction mismatch: got %f, want 0.5", proModel.QuotaInfo.RemainingFraction)
	}

	//
	if rawResp == nil {
		t.Fatal("rawResp should not be nil")
	}
	if rawResp["models"] == nil {
		t.Error("rawResp models should not be nil")
	}
}

func TestClient_FetchAvailableModels_HTTPError_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer server.Close()

	withMockBaseURLs(t, []string{server.URL})

	client := mustNewClient(t, "")
	_, _, err := client.FetchAvailableModels(context.Background(), "bad-token", "proj")
	if err == nil {
		t.Fatal("should return error when server returns 403")
	}
	if !strings.Contains(err.Error(), "fetchAvailableModels failed") {
		t.Errorf("error message should contain 'fetchAvailableModels failed': got %s", err.Error())
	}
}

func TestClient_FetchAvailableModels_InvalidJSON_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<<<not json>>>`))
	}))
	defer server.Close()

	withMockBaseURLs(t, []string{server.URL})

	client := mustNewClient(t, "")
	_, _, err := client.FetchAvailableModels(context.Background(), "token", "proj")
	if err == nil {
		t.Fatal("invalid JSON response should return error")
	}
	if !strings.Contains(err.Error(), "response parse failed") {
		t.Errorf("error message should contain 'response parse failed': got %s", err.Error())
	}
}

func TestClient_FetchAvailableModels_URLFallback_RealCall(t *testing.T) {
	callCount := 0
	//
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models": {"model-a": {}}}`))
	}))
	defer server2.Close()

	withMockBaseURLs(t, []string{server1.URL, server2.URL})

	client := mustNewClient(t, "")
	resp, _, err := client.FetchAvailableModels(context.Background(), "token", "proj")
	if err != nil {
		t.Fatalf("FetchAvailableModels should succeed after fallback: %v", err)
	}
	if _, ok := resp.Models["model-a"]; !ok {
		t.Error("should return model from fallback server")
	}
	if callCount != 2 {
		t.Errorf("should have called 2 servers, actual calls: %d", callCount)
	}
}

func TestClient_FetchAvailableModels_AllURLsFail_RealCall(t *testing.T) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`not found`))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`internal error`))
	}))
	defer server2.Close()

	withMockBaseURLs(t, []string{server1.URL, server2.URL})

	client := mustNewClient(t, "")
	_, _, err := client.FetchAvailableModels(context.Background(), "token", "proj")
	if err == nil {
		t.Fatal("should return error when all URLs fail")
	}
}

func TestClient_FetchAvailableModels_ContextCanceled_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	withMockBaseURLs(t, []string{server.URL})

	client := mustNewClient(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := client.FetchAvailableModels(ctx, "token", "proj")
	if err == nil {
		t.Fatal("should return error when context is cancelled")
	}
}

func TestClient_FetchAvailableModels_EmptyModels_RealCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models": {}}`))
	}))
	defer server.Close()

	withMockBaseURLs(t, []string{server.URL})

	client := mustNewClient(t, "")
	resp, rawResp, err := client.FetchAvailableModels(context.Background(), "token", "proj")
	if err != nil {
		t.Fatalf("FetchAvailableModels failed: %v", err)
	}
	if resp.Models == nil {
		t.Fatal("Models should not be nil")
	}
	if len(resp.Models) != 0 {
		t.Errorf("Models should be empty: got %d", len(resp.Models))
	}
	if rawResp == nil {
		t.Fatal("rawResp should not be nil")
	}
}

// ---------------------------------------------------------------------------
// LoadCodeAssist
// ---------------------------------------------------------------------------

func TestClient_LoadCodeAssist_408Fallback_RealCall(t *testing.T) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestTimeout)
		_, _ = w.Write([]byte(`timeout`))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cloudaicompanionProject":"p2","currentTier":"free-tier"}`))
	}))
	defer server2.Close()

	withMockBaseURLs(t, []string{server1.URL, server2.URL})

	client := mustNewClient(t, "")
	resp, _, err := client.LoadCodeAssist(context.Background(), "token")
	if err != nil {
		t.Fatalf("LoadCodeAssist should succeed after 408 fallback: %v", err)
	}
	if resp.CloudAICompanionProject != "p2" {
		t.Errorf("CloudAICompanionProject mismatch: got %s", resp.CloudAICompanionProject)
	}
}

func TestClient_FetchAvailableModels_404Fallback_RealCall(t *testing.T) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`not found`))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":{"m1":{"quotaInfo":{"remainingFraction":1.0}}}}`))
	}))
	defer server2.Close()

	withMockBaseURLs(t, []string{server1.URL, server2.URL})

	client := mustNewClient(t, "")
	resp, _, err := client.FetchAvailableModels(context.Background(), "token", "proj")
	if err != nil {
		t.Fatalf("FetchAvailableModels should succeed after 404 fallback: %v", err)
	}
	if _, ok := resp.Models["m1"]; !ok {
		t.Error("should return model m1 from fallback server")
	}
}

func TestExtractProjectIDFromOnboardResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		resp map[string]any
		want string
	}{
		{
			name: "nil response",
			resp: nil,
			want: "",
		},
		{
			name: "empty response",
			resp: map[string]any{},
			want: "",
		},
		{
			name: "project as string",
			resp: map[string]any{
				"cloudaicompanionProject": "my-project-123",
			},
			want: "my-project-123",
		},
		{
			name: "project as string with spaces",
			resp: map[string]any{
				"cloudaicompanionProject": "  my-project-123  ",
			},
			want: "my-project-123",
		},
		{
			name: "project as map with id",
			resp: map[string]any{
				"cloudaicompanionProject": map[string]any{
					"id": "proj-from-map",
				},
			},
			want: "proj-from-map",
		},
		{
			name: "project as map without id",
			resp: map[string]any{
				"cloudaicompanionProject": map[string]any{
					"name": "some-name",
				},
			},
			want: "",
		},
		{
			name: "missing cloudaicompanionProject key",
			resp: map[string]any{
				"otherField": "value",
			},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := extractProjectIDFromOnboardResponse(tc.resp)
			if got != tc.want {
				t.Fatalf("extractProjectIDFromOnboardResponse() = %q, want %q", got, tc.want)
			}
		})
	}
}
