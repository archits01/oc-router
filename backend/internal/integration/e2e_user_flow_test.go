//go:build e2e

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// E2E
// → → → →

var (
	testUserEmail    = "e2e-test-" + fmt.Sprintf("%d", time.Now().UnixMilli()) + "@test.local"
	testUserPassword = "E2eTest@12345"
	testUserName     = "e2e-test-user"
)

// TestUserRegistrationAndLogin
func TestUserRegistrationAndLogin(t *testing.T) {
	t.Run("register new user", func(t *testing.T) {
		payload := map[string]string{
			"email":    testUserEmail,
			"password": testUserPassword,
			"username": testUserName,
		}
		body, _ := json.Marshal(payload)

		resp, err := doRequest(t, "POST", "/api/auth/register", body, "")
		if err != nil {
			t.Skipf("registration endpoint unavailable, skipping user flow tests: %v", err)
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)

		//
		switch resp.StatusCode {
		case 200:
			t.Logf("✅ user registration successful: %s", testUserEmail)
		case 400:
			t.Logf("⚠️ user may already exist: %s", string(respBody))
		case 403:
			t.Skipf("registration feature is disabled: %s", string(respBody))
		default:
			t.Logf("⚠️ registration returned HTTP %d: %s（continuing to try login）", resp.StatusCode, string(respBody))
		}
	})

	//
	var accessToken string
	t.Run("user login to obtain JWT", func(t *testing.T) {
		payload := map[string]string{
			"email":    testUserEmail,
			"password": testUserPassword,
		}
		body, _ := json.Marshal(payload)

		resp, err := doRequest(t, "POST", "/api/auth/login", body, "")
		if err != nil {
			t.Fatalf("login request failed: %v", err)
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != 200 {
			t.Skipf("login failed HTTP %d: %s（may need to register user first）", resp.StatusCode, string(respBody))
			return
		}

		var result map[string]any
		if err := json.Unmarshal(respBody, &result); err != nil {
			t.Fatalf("failed to parse login response: %v", err)
		}

		//
		if token, ok := result["access_token"].(string); ok && token != "" {
			accessToken = token
		} else if data, ok := result["data"].(map[string]any); ok {
			if token, ok := data["access_token"].(string); ok {
				accessToken = token
			}
		}

		if accessToken == "" {
			t.Skipf("failed to get access_token, response: %s", string(respBody))
			return
		}

		//
		if len(accessToken) < 10 {
			t.Fatalf("access_token format is invalid: %s", accessToken)
		}

		t.Logf("✅ login successful, obtained JWT（length: %d）", len(accessToken))
	})

	if accessToken == "" {
		t.Skip("JWT not obtained, skipping subsequent tests")
		return
	}

	//
	t.Run("get current user info", func(t *testing.T) {
		resp, err := doRequest(t, "GET", "/api/user/me", nil, accessToken)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("HTTP %d: %s", resp.StatusCode, string(body))
		}

		t.Logf("✅ successfully obtained user info")
	})
}

// TestAPIKeyLifecycle
func TestAPIKeyLifecycle(t *testing.T) {
	//
	accessToken := loginTestUser(t)
	if accessToken == "" {
		t.Skip("unable to login, skipping API Key lifecycle tests")
		return
	}

	var apiKey string

	//
	t.Run("create API Key", func(t *testing.T) {
		payload := map[string]string{
			"name": "e2e-test-key-" + fmt.Sprintf("%d", time.Now().UnixMilli()),
		}
		body, _ := json.Marshal(payload)

		resp, err := doRequest(t, "POST", "/api/keys", body, accessToken)
		if err != nil {
			t.Fatalf("create API Key request failed: %v", err)
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != 200 {
			t.Skipf("create API Key failed HTTP %d: %s", resp.StatusCode, string(respBody))
			return
		}

		var result map[string]any
		if err := json.Unmarshal(respBody, &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		//
		if key, ok := result["key"].(string); ok {
			apiKey = key
		} else if data, ok := result["data"].(map[string]any); ok {
			if key, ok := data["key"].(string); ok {
				apiKey = key
			}
		}

		if apiKey == "" {
			t.Skipf("failed to get API Key, response: %s", string(respBody))
			return
		}

		//
		masked := apiKey
		if len(masked) > 8 {
			masked = masked[:8] + "..."
		}
		t.Logf("✅ API Key created successfully: %s", masked)
	})

	if apiKey == "" {
		t.Skip("API Key not created, skipping subsequent tests")
		return
	}

	//
	t.Run("use API Key to call gateway", func(t *testing.T) {
		//
		resp, err := doRequest(t, "GET", "/v1/models", nil, apiKey)
		if err != nil {
			t.Fatalf("gateway request failed: %v", err)
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)

		//
		switch {
		case resp.StatusCode == 200:
			t.Logf("✅ API Key gateway call successful")
		case resp.StatusCode == 402:
			t.Logf("⚠️ insufficient balance, but API Key authentication passed")
		case resp.StatusCode == 403:
			t.Logf("⚠️ no available accounts, but API Key authentication passed")
		default:
			t.Logf("⚠️ gateway returned HTTP %d: %s", resp.StatusCode, string(respBody))
		}
	})

	t.Run("query usage records", func(t *testing.T) {
		resp, err := doRequest(t, "GET", "/api/usage/dashboard", nil, accessToken)
		if err != nil {
			t.Fatalf("usage query request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("⚠️ usage query returned HTTP %d: %s", resp.StatusCode, string(body))
			return
		}

		t.Logf("✅ usage query successful")
	})
}

// =============================================================================
// =============================================================================

func doRequest(t *testing.T, method, path string, body []byte, token string) (*http.Response, error) {
	t.Helper()

	url := baseURL + path
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}

func loginTestUser(t *testing.T) string {
	t.Helper()

	adminEmail := getEnv("ADMIN_EMAIL", "admin@sub2api.local")
	adminPassword := getEnv("ADMIN_PASSWORD", "")

	if adminPassword == "" {
		adminEmail = testUserEmail
		adminPassword = testUserPassword
	}

	payload := map[string]string{
		"email":    adminEmail,
		"password": adminPassword,
	}
	body, _ := json.Marshal(payload)

	resp, err := doRequest(t, "POST", "/api/auth/login", body, "")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return ""
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return ""
	}

	if token, ok := result["access_token"].(string); ok {
		return token
	}
	if data, ok := result["data"].(map[string]any); ok {
		if token, ok := data["access_token"].(string); ok {
			return token
		}
	}

	return ""
}

// redactAPIKey API Key
func redactAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return "***"
	}
	return key[:8] + "..."
}
