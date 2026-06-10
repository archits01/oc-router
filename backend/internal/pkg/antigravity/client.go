// Package antigravity provides a client for the Antigravity API.
package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
)

// ForbiddenError
type ForbiddenError struct {
	StatusCode int
	Body       string
}

func (e *ForbiddenError) Error() string {
	return fmt.Sprintf("fetchAvailableModels failed (HTTP %d): %s", e.StatusCode, e.Body)
}

// NewAPIRequestWithURL
func NewAPIRequestWithURL(ctx context.Context, baseURL, action, accessToken string, body []byte) (*http.Request, error) {
	// ?alt=sse
	apiURL := fmt.Sprintf("%s/v1internal:%s", baseURL, action)
	isStream := action == "streamGenerateContent"
	if isStream {
		apiURL += "?alt=sse"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	//
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", GetUserAgentForContext(ctx))

	return req, nil
}

// NewAPIRequest
//
func NewAPIRequest(ctx context.Context, action, accessToken string, body []byte) (*http.Request, error) {
	return NewAPIRequestWithURL(ctx, BaseURL, action, accessToken, body)
}

// TokenResponse Google OAuth token
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// UserInfo Google
type UserInfo struct {
	Email      string `json:"email"`
	Name       string `json:"name,omitempty"`
	GivenName  string `json:"given_name,omitempty"`
	FamilyName string `json:"family_name,omitempty"`
	Picture    string `json:"picture,omitempty"`
}

// LoadCodeAssistRequest loadCodeAssist
type LoadCodeAssistRequest struct {
	Metadata struct {
		IDEType    string `json:"ideType"`
		IDEVersion string `json:"ideVersion"`
		IDEName    string `json:"ideName"`
	} `json:"metadata"`
}

// TierInfo
type TierInfo struct {
	ID          string `json:"id"`          // free-tier, g1-pro-tier, g1-ultra-tier
	Name        string `json:"name"`        // display name
	Description string `json:"description"` // description
}

// UnmarshalJSON supports both legacy string tiers and object tiers.
func (t *TierInfo) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var id string
		if err := json.Unmarshal(data, &id); err != nil {
			return err
		}
		t.ID = id
		return nil
	}
	type alias TierInfo
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*t = TierInfo(decoded)
	return nil
}

// IneligibleTier
type IneligibleTier struct {
	Tier *TierInfo `json:"tier,omitempty"`
	// ReasonCode
	ReasonCode    string `json:"reasonCode,omitempty"`
	ReasonMessage string `json:"reasonMessage,omitempty"`
}

// LoadCodeAssistResponse loadCodeAssist
type LoadCodeAssistResponse struct {
	CloudAICompanionProject string            `json:"cloudaicompanionProject"`
	CurrentTier             *TierInfo         `json:"currentTier,omitempty"`
	PaidTier                *PaidTierInfo     `json:"paidTier,omitempty"`
	IneligibleTiers         []*IneligibleTier `json:"ineligibleTiers,omitempty"`
}

// PaidTierInfo
type PaidTierInfo struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	AvailableCredits []AvailableCredit `json:"availableCredits,omitempty"`
}

// UnmarshalJSON
func (p *PaidTierInfo) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var id string
		if err := json.Unmarshal(data, &id); err != nil {
			return err
		}
		p.ID = id
		return nil
	}
	type alias PaidTierInfo
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = PaidTierInfo(raw)
	return nil
}

// AvailableCredit
type AvailableCredit struct {
	CreditType                  string `json:"creditType,omitempty"`
	CreditAmount                string `json:"creditAmount,omitempty"`
	MinimumCreditAmountForUsage string `json:"minimumCreditAmountForUsage,omitempty"`
}

// GetAmount
func (c *AvailableCredit) GetAmount() float64 {
	if c.CreditAmount == "" {
		return 0
	}
	var value float64
	_, _ = fmt.Sscanf(c.CreditAmount, "%f", &value)
	return value
}

// GetMinimumAmount
func (c *AvailableCredit) GetMinimumAmount() float64 {
	if c.MinimumCreditAmountForUsage == "" {
		return 0
	}
	var value float64
	_, _ = fmt.Sscanf(c.MinimumCreditAmountForUsage, "%f", &value)
	return value
}

// OnboardUserRequest onboardUser
type OnboardUserRequest struct {
	TierID   string `json:"tierId"`
	Metadata struct {
		IDEType    string `json:"ideType"`
		Platform   string `json:"platform,omitempty"`
		PluginType string `json:"pluginType,omitempty"`
	} `json:"metadata"`
}

// OnboardUserResponse onboardUser
type OnboardUserResponse struct {
	Name     string         `json:"name,omitempty"`
	Done     bool           `json:"done"`
	Response map[string]any `json:"response,omitempty"`
}

// GetTier
//
func (r *LoadCodeAssistResponse) GetTier() string {
	if r.PaidTier != nil && r.PaidTier.ID != "" {
		return r.PaidTier.ID
	}
	if r.CurrentTier != nil {
		return r.CurrentTier.ID
	}
	return ""
}

// GetAvailableCredits
func (r *LoadCodeAssistResponse) GetAvailableCredits() []AvailableCredit {
	if r.PaidTier == nil {
		return nil
	}
	return r.PaidTier.AvailableCredits
}

// TierIDToPlanType
func TierIDToPlanType(tierID string) string {
	switch strings.ToLower(strings.TrimSpace(tierID)) {
	case "free-tier":
		return "Free"
	case "g1-pro-tier":
		return "Pro"
	case "g1-ultra-tier":
		return "Ultra"
	default:
		if tierID == "" {
			return "Free"
		}
		return tierID
	}
}

// Client Antigravity API
type Client struct {
	httpClient *http.Client
}

const (
	// proxyDialTimeout
	proxyDialTimeout = 5 * time.Second
	// proxyTLSHandshakeTimeout
	proxyTLSHandshakeTimeout = 5 * time.Second
	// clientTimeout
	clientTimeout = 10 * time.Second
	// fetchAvailableModelsBodyLimit limits model-list responses to avoid unbounded memory use.
	fetchAvailableModelsBodyLimit int64 = 8 << 20
)

func NewClient(proxyURL string) (*Client, error) {
	client := &http.Client{
		Timeout: clientTimeout,
	}

	_, parsed, err := proxyurl.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	if parsed != nil {
		transport := &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: proxyDialTimeout,
			}).DialContext,
			TLSHandshakeTimeout: proxyTLSHandshakeTimeout,
		}
		if err := proxyutil.ConfigureTransportProxy(transport, parsed); err != nil {
			return nil, fmt.Errorf("configure proxy: %w", err)
		}
		client.Transport = transport
	}

	return &Client{
		httpClient: client,
	}, nil
}

// IsConnectionError
func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	//
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	//
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

// shouldFallbackToNextURL
//
func shouldFallbackToNextURL(err error, statusCode int) bool {
	if IsConnectionError(err) {
		return true
	}
	return statusCode == http.StatusTooManyRequests ||
		statusCode == http.StatusRequestTimeout ||
		statusCode == http.StatusNotFound ||
		statusCode >= 500
}

// ExchangeCode
func (c *Client) ExchangeCode(ctx context.Context, code, codeVerifier string) (*TokenResponse, error) {
	clientSecret, err := getClientSecret()
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("client_id", ClientID)
	params.Set("client_secret", clientSecret)
	params.Set("code", code)
	params.Set("redirect_uri", RedirectURI)
	params.Set("grant_type", "authorization_code")
	params.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return nil, fmt.Errorf("token parsefailed: %w", err)
	}

	return &tokenResp, nil
}

// RefreshToken
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	clientSecret, err := getClientSecret()
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("client_id", ClientID)
	params.Set("client_secret", clientSecret)
	params.Set("refresh_token", refreshToken)
	params.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return nil, fmt.Errorf("token parsefailed: %w", err)
	}

	return &tokenResp, nil
}

// GetUserInfo
func (c *Client) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, UserInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinforequest failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get userinfo failed (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var userInfo UserInfo
	if err := json.Unmarshal(bodyBytes, &userInfo); err != nil {
		return nil, fmt.Errorf("userinfoparsefailed: %w", err)
	}

	return &userInfo, nil
}

// LoadCodeAssist
// → daily → prod
func (c *Client) LoadCodeAssist(ctx context.Context, accessToken string) (*LoadCodeAssistResponse, map[string]any, error) {
	reqBody := LoadCodeAssistRequest{}
	reqBody.Metadata.IDEType = "ANTIGRAVITY"
	reqBody.Metadata.IDEVersion = GetUserAgentVersionForContext(ctx)
	reqBody.Metadata.IDEName = "antigravity"

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	// > daily
	availableURLs := BaseURLs

	var lastErr error
	for urlIdx, baseURL := range availableURLs {
		apiURL := baseURL + "/v1internal:loadCodeAssist"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(bodyBytes)))
		if err != nil {
			lastErr = fmt.Errorf("create request failed: %w", err)
			continue
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", GetUserAgentForContext(ctx))

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("loadCodeAssist request failed: %w", err)
			if shouldFallbackToNextURL(err, 0) && urlIdx < len(availableURLs)-1 {
				log.Printf("[antigravity] loadCodeAssist URL fallback: %s -> %s", baseURL, availableURLs[urlIdx+1])
				continue
			}
			return nil, nil, lastErr
		}

		respBodyBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close() // close immediately to avoid resource leaks from defer inside loops
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response: %w", err)
		}

		//
		if shouldFallbackToNextURL(nil, resp.StatusCode) && urlIdx < len(availableURLs)-1 {
			log.Printf("[antigravity] loadCodeAssist URL fallback (HTTP %d): %s -> %s", resp.StatusCode, baseURL, availableURLs[urlIdx+1])
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("loadCodeAssist failed (HTTP %d): %s", resp.StatusCode, string(respBodyBytes))
		}

		var loadResp LoadCodeAssistResponse
		if err := json.Unmarshal(respBodyBytes, &loadResp); err != nil {
			return nil, nil, fmt.Errorf("response parse failed: %w", err)
		}

		//
		var rawResp map[string]any
		_ = json.Unmarshal(respBodyBytes, &rawResp)

		//
		DefaultURLAvailability.MarkSuccess(baseURL)
		return &loadResp, rawResp, nil
	}

	return nil, nil, lastErr
}

// OnboardUser
// 1)
// 2)
func (c *Client) OnboardUser(ctx context.Context, accessToken, tierID string) (string, error) {
	tierID = strings.TrimSpace(tierID)
	if tierID == "" {
		return "", fmt.Errorf("tier_id is empty")
	}

	reqBody := OnboardUserRequest{TierID: tierID}
	reqBody.Metadata.IDEType = "ANTIGRAVITY"
	reqBody.Metadata.Platform = "PLATFORM_UNSPECIFIED"
	reqBody.Metadata.PluginType = "GEMINI"

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to serialize request: %w", err)
	}

	availableURLs := BaseURLs
	var lastErr error

	for urlIdx, baseURL := range availableURLs {
		apiURL := baseURL + "/v1internal:onboardUser"

		for attempt := 1; attempt <= 5; attempt++ {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
			if err != nil {
				lastErr = fmt.Errorf("create request failed: %w", err)
				break
			}
			req.Header.Set("Authorization", "Bearer "+accessToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", GetUserAgentForContext(ctx))

			resp, err := c.httpClient.Do(req)
			if err != nil {
				lastErr = fmt.Errorf("onboardUser request failed: %w", err)
				if shouldFallbackToNextURL(err, 0) && urlIdx < len(availableURLs)-1 {
					log.Printf("[antigravity] onboardUser URL fallback: %s -> %s", baseURL, availableURLs[urlIdx+1])
					break
				}
				return "", lastErr
			}

			respBodyBytes, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				return "", fmt.Errorf("failed to read response: %w", err)
			}

			if shouldFallbackToNextURL(nil, resp.StatusCode) && urlIdx < len(availableURLs)-1 {
				log.Printf("[antigravity] onboardUser URL fallback (HTTP %d): %s -> %s", resp.StatusCode, baseURL, availableURLs[urlIdx+1])
				break
			}

			if resp.StatusCode != http.StatusOK {
				lastErr = fmt.Errorf("onboardUser failed (HTTP %d): %s", resp.StatusCode, string(respBodyBytes))
				return "", lastErr
			}

			var onboardResp OnboardUserResponse
			if err := json.Unmarshal(respBodyBytes, &onboardResp); err != nil {
				lastErr = fmt.Errorf("onboardUser response parse failed: %w", err)
				return "", lastErr
			}

			if onboardResp.Done {
				if projectID := extractProjectIDFromOnboardResponse(onboardResp.Response); projectID != "" {
					DefaultURLAvailability.MarkSuccess(baseURL)
					return projectID, nil
				}
				lastErr = fmt.Errorf("onboardUser completed but did not return project_id")
				return "", lastErr
			}

			// done=false
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("onboardUser did not return project_id")
}

func extractProjectIDFromOnboardResponse(resp map[string]any) string {
	if len(resp) == 0 {
		return ""
	}

	if v, ok := resp["cloudaicompanionProject"]; ok {
		switch project := v.(type) {
		case string:
			return strings.TrimSpace(project)
		case map[string]any:
			if id, ok := project["id"].(string); ok {
				return strings.TrimSpace(id)
			}
		}
	}

	return ""
}

// ModelQuotaInfo
type ModelQuotaInfo struct {
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
}

// ModelInfo
type ModelInfo struct {
	QuotaInfo          *ModelQuotaInfo `json:"quotaInfo,omitempty"`
	DisplayName        string          `json:"displayName,omitempty"`
	SupportsImages     *bool           `json:"supportsImages,omitempty"`
	SupportsThinking   *bool           `json:"supportsThinking,omitempty"`
	ThinkingBudget     *int            `json:"thinkingBudget,omitempty"`
	Recommended        *bool           `json:"recommended,omitempty"`
	MaxTokens          *int            `json:"maxTokens,omitempty"`
	MaxOutputTokens    *int            `json:"maxOutputTokens,omitempty"`
	SupportedMimeTypes map[string]bool `json:"supportedMimeTypes,omitempty"`
}

// DeprecatedModelInfo
type DeprecatedModelInfo struct {
	NewModelID string `json:"newModelId"`
}

// FetchAvailableModelsRequest fetchAvailableModels
type FetchAvailableModelsRequest struct {
	Project string `json:"project"`
}

// FetchAvailableModelsResponse fetchAvailableModels
type FetchAvailableModelsResponse struct {
	Models             map[string]ModelInfo           `json:"models"`
	DeprecatedModelIDs map[string]DeprecatedModelInfo `json:"deprecatedModelIds,omitempty"`
}

// FetchAvailableModels
// → daily → prod
func (c *Client) FetchAvailableModels(ctx context.Context, accessToken, projectID string) (*FetchAvailableModelsResponse, map[string]any, error) {
	if c == nil || c.httpClient == nil {
		return nil, nil, errors.New("antigravity client is not configured")
	}

	reqBody := FetchAvailableModelsRequest{Project: projectID}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	// > daily
	availableURLs := BaseURLs

	fetchClient := c.fetchAvailableModelsHTTPClient()
	var lastErr error
	for urlIdx, baseURL := range availableURLs {
		apiURL := baseURL + "/v1internal:fetchAvailableModels"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(bodyBytes)))
		if err != nil {
			lastErr = fmt.Errorf("create request failed: %w", err)
			continue
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", GetUserAgentForContext(ctx))

		resp, err := fetchClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("fetchAvailableModels request failed: %w", err)
			if shouldFallbackToNextURL(err, 0) && urlIdx < len(availableURLs)-1 {
				log.Printf("[antigravity] fetchAvailableModels URL fallback: %s -> %s", baseURL, availableURLs[urlIdx+1])
				continue
			}
			return nil, nil, lastErr
		}

		respBodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, fetchAvailableModelsBodyLimit+1))
		_ = resp.Body.Close() // close immediately to avoid resource leaks from defer inside loops
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response: %w", err)
		}
		if int64(len(respBodyBytes)) > fetchAvailableModelsBodyLimit {
			return nil, nil, fmt.Errorf("response exceeds %d bytes", fetchAvailableModelsBodyLimit)
		}

		//
		if shouldFallbackToNextURL(nil, resp.StatusCode) && urlIdx < len(availableURLs)-1 {
			log.Printf("[antigravity] fetchAvailableModels URL fallback (HTTP %d): %s -> %s", resp.StatusCode, baseURL, availableURLs[urlIdx+1])
			continue
		}

		if resp.StatusCode == http.StatusForbidden {
			return nil, nil, &ForbiddenError{
				StatusCode: resp.StatusCode,
				Body:       string(respBodyBytes),
			}
		}

		if resp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("fetchAvailableModels failed (HTTP %d): %s", resp.StatusCode, string(respBodyBytes))
		}

		var modelsResp FetchAvailableModelsResponse
		if err := json.Unmarshal(respBodyBytes, &modelsResp); err != nil {
			return nil, nil, fmt.Errorf("response parse failed: %w", err)
		}

		//
		var rawResp map[string]any
		_ = json.Unmarshal(respBodyBytes, &rawResp)

		//
		DefaultURLAvailability.MarkSuccess(baseURL)
		return &modelsResp, rawResp, nil
	}

	return nil, nil, lastErr
}

func (c *Client) fetchAvailableModelsHTTPClient() *http.Client {
	fetchClient := *c.httpClient
	fetchClient.CheckRedirect = checkFetchAvailableModelsRedirect
	return &fetchClient
}

func checkFetchAvailableModelsRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if req == nil || req.URL == nil {
		return errors.New("redirect url is nil")
	}
	if !isAllowedFetchAvailableModelsRedirectHost(req.URL.Hostname()) {
		return fmt.Errorf("redirect to unsupported host: %s", req.URL.Hostname())
	}
	return nil
}

func isAllowedFetchAvailableModelsRedirectHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, baseURL := range BaseURLs {
		parsed, err := url.Parse(baseURL)
		if err != nil {
			continue
		}
		if strings.EqualFold(host, parsed.Hostname()) {
			return true
		}
	}
	return false
}

// ── Privacy API ──────────────────────────────────────────────────────

// privacyBaseURL
const privacyBaseURL = antigravityDailyBaseURL

// SetUserSettingsRequest setUserSettings
type SetUserSettingsRequest struct {
	UserSettings map[string]any `json:"user_settings"`
}

// FetchUserInfoRequest fetchUserInfo
type FetchUserInfoRequest struct {
	Project string `json:"project"`
}

// FetchUserInfoResponse fetchUserInfo
type FetchUserInfoResponse struct {
	UserSettings map[string]any `json:"userSettings,omitempty"`
	RegionCode   string         `json:"regionCode,omitempty"`
}

// IsPrivate
func (r *FetchUserInfoResponse) IsPrivate() bool {
	if r == nil || r.UserSettings == nil {
		return true
	}
	_, hasTelemetry := r.UserSettings["telemetryEnabled"]
	return !hasTelemetry
}

// SetUserSettingsResponse setUserSettings
type SetUserSettingsResponse struct {
	UserSettings map[string]any `json:"userSettings,omitempty"`
}

// IsSuccess {"userSettings":{}}
func (r *SetUserSettingsResponse) IsSuccess() bool {
	if r == nil {
		return false
	}
	// userSettings
	if len(r.UserSettings) == 0 {
		return true
	}
	//
	_, hasTelemetry := r.UserSettings["telemetryEnabled"]
	return !hasTelemetry
}

// SetUserSettings
func (c *Client) SetUserSettings(ctx context.Context, accessToken string) (*SetUserSettingsResponse, error) {
	//
	payload := SetUserSettingsRequest{UserSettings: map[string]any{}}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	apiURL := privacyBaseURL + "/v1internal:setUserSettings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", GetUserAgentForContext(ctx))
	req.Header.Set("X-Goog-Api-Client", "gl-node/22.21.1")
	req.Host = "daily-cloudcode-pa.googleapis.com"

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("setUserSettings request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("setUserSettings failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result SetUserSettingsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("response parse failed: %w", err)
	}

	return &result, nil
}

// FetchUserInfo
func (c *Client) FetchUserInfo(ctx context.Context, accessToken, projectID string) (*FetchUserInfoResponse, error) {
	reqBody := FetchUserInfoRequest{Project: projectID}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	apiURL := privacyBaseURL + "/v1internal:fetchUserInfo"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", GetUserAgentForContext(ctx))
	req.Header.Set("X-Goog-Api-Client", "gl-node/22.21.1")
	req.Host = "daily-cloudcode-pa.googleapis.com"

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetchUserInfo request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetchUserInfo failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result FetchUserInfoResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("response parse failed: %w", err)
	}

	return &result, nil
}
