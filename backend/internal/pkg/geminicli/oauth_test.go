package geminicli

import (
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// SessionStore
// ---------------------------------------------------------------------------

func TestSessionStore_SetAndGet(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()

	session := &OAuthSession{
		State:     "test-state",
		OAuthType: "code_assist",
		CreatedAt: time.Now(),
	}
	store.Set("sid-1", session)

	got, ok := store.Get("sid-1")
	if !ok {
		t.Fatal("expected Get to return ok=true, got false")
	}
	if got.State != "test-state" {
		t.Errorf("expected State=%q, got=%q", "test-state", got.State)
	}
}

func TestSessionStore_GetNotFound(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()

	_, ok := store.Get("non-existent ID")
	if ok {
		t.Error("expected non-existent sessionID to return ok=false")
	}
}

func TestSessionStore_GetExpired(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()

	// +1
	session := &OAuthSession{
		State:     "expired-state",
		OAuthType: "code_assist",
		CreatedAt: time.Now().Add(-(SessionTTL + 1*time.Minute)),
	}
	store.Set("expired-sid", session)

	_, ok := store.Get("expired-sid")
	if ok {
		t.Error("expected expired session to return ok=false")
	}
}

func TestSessionStore_Delete(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()

	session := &OAuthSession{
		State:     "to-delete",
		OAuthType: "code_assist",
		CreatedAt: time.Now(),
	}
	store.Set("del-sid", session)

	if _, ok := store.Get("del-sid"); !ok {
		t.Fatal("session should exist before deletion")
	}

	store.Delete("del-sid")

	if _, ok := store.Get("del-sid"); ok {
		t.Error("session should not exist after deletion")
	}
}

func TestSessionStore_Stop_Idempotent(t *testing.T) {
	store := NewSessionStore()

	//
	store.Stop()
	store.Stop()
	store.Stop()
}

func TestSessionStore_ConcurrentAccess(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			sid := "concurrent-" + string(rune('A'+idx%26))
			store.Set(sid, &OAuthSession{
				State:     sid,
				OAuthType: "code_assist",
				CreatedAt: time.Now(),
			})
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			sid := "concurrent-" + string(rune('A'+idx%26))
			store.Get(sid) // may or may not find it, key is no panic
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			sid := "concurrent-" + string(rune('A'+idx%26))
			store.Delete(sid)
		}(i)
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// GenerateRandomBytes
// ---------------------------------------------------------------------------

func TestGenerateRandomBytes(t *testing.T) {
	tests := []int{0, 1, 16, 32, 64}
	for _, n := range tests {
		b, err := GenerateRandomBytes(n)
		if err != nil {
			t.Errorf("GenerateRandomBytes(%d) error: %v", n, err)
			continue
		}
		if len(b) != n {
			t.Errorf("GenerateRandomBytes(%d) returned length=%d，expected=%d", n, len(b), n)
		}
	}
}

func TestGenerateRandomBytes_Uniqueness(t *testing.T) {
	a, _ := GenerateRandomBytes(32)
	b, _ := GenerateRandomBytes(32)
	if string(a) == string(b) {
		t.Error("two calls to GenerateRandomBytes(32) returned identical results, randomness may be compromised")
	}
}

// ---------------------------------------------------------------------------
// GenerateState
// ---------------------------------------------------------------------------

func TestGenerateState(t *testing.T) {
	state, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState() error: %v", err)
	}
	if state == "" {
		t.Error("GenerateState() returnedempty string")
	}
	// base64url '='
	if strings.Contains(state, "=") {
		t.Errorf("GenerateState() result contains '=' padding: %s", state)
	}
	// base64url '+' '/'
	if strings.ContainsAny(state, "+/") {
		t.Errorf("GenerateState() result contains non-base64url characters: %s", state)
	}
}

// ---------------------------------------------------------------------------
// GenerateSessionID
// ---------------------------------------------------------------------------

func TestGenerateSessionID(t *testing.T) {
	sid, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("GenerateSessionID() error: %v", err)
	}
	// 16 > 32
	if len(sid) != 32 {
		t.Errorf("GenerateSessionID() length=%d, expected=32", len(sid))
	}
	//
	if _, err := hex.DecodeString(sid); err != nil {
		t.Errorf("GenerateSessionID() is not a valid hex string: %s, err=%v", sid, err)
	}
}

func TestGenerateSessionID_Uniqueness(t *testing.T) {
	a, _ := GenerateSessionID()
	b, _ := GenerateSessionID()
	if a == b {
		t.Error("two calls to GenerateSessionID() returned identical results")
	}
}

// ---------------------------------------------------------------------------
// GenerateCodeVerifier
// ---------------------------------------------------------------------------

func TestGenerateCodeVerifier(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier() error: %v", err)
	}
	if verifier == "" {
		t.Error("GenerateCodeVerifier() returnedempty string")
	}
	// RFC 7636
	if len(verifier) < 43 {
		t.Errorf("GenerateCodeVerifier() length=%d，RFC 7636 requires at least 43 characters", len(verifier))
	}
	// base64url
	if strings.Contains(verifier, "=") {
		t.Errorf("GenerateCodeVerifier() contains '=' padding: %s", verifier)
	}
	if strings.ContainsAny(verifier, "+/") {
		t.Errorf("GenerateCodeVerifier() contains non-base64url characters: %s", verifier)
	}
}

// ---------------------------------------------------------------------------
// GenerateCodeChallenge
// ---------------------------------------------------------------------------

func TestGenerateCodeChallenge(t *testing.T) {
	// RFC 7636 = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	// = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	expected := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	challenge := GenerateCodeChallenge(verifier)
	if challenge != expected {
		t.Errorf("GenerateCodeChallenge(%q) = %q，expected %q", verifier, challenge, expected)
	}
}

func TestGenerateCodeChallenge_NoPadding(t *testing.T) {
	challenge := GenerateCodeChallenge("test-verifier-string")
	if strings.Contains(challenge, "=") {
		t.Errorf("GenerateCodeChallenge() result contains '=' padding: %s", challenge)
	}
}

// ---------------------------------------------------------------------------
// base64URLEncode
// ---------------------------------------------------------------------------

func TestBase64URLEncode(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"empty bytes", []byte{}},
		{"single byte", []byte{0xff}},
		{"multiple bytes", []byte{0x01, 0x02, 0x03, 0x04, 0x05}},
		{"all zeros", []byte{0x00, 0x00, 0x00}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := base64URLEncode(tt.input)
			// '=' padding
			if strings.Contains(result, "=") {
				t.Errorf("base64URLEncode(%v) contains '=' padding: %s", tt.input, result)
			}
			// '+' '/'
			if strings.ContainsAny(result, "+/") {
				t.Errorf("base64URLEncode(%v) contains non-URL-safe characters: %s", tt.input, result)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// hasRestrictedScope
// ---------------------------------------------------------------------------

func TestHasRestrictedScope(t *testing.T) {
	tests := []struct {
		scope    string
		expected bool
	}{
		//
		{"https://www.googleapis.com/auth/generative-language", true},
		{"https://www.googleapis.com/auth/generative-language.retriever", true},
		{"https://www.googleapis.com/auth/generative-language.tuning", true},
		{"https://www.googleapis.com/auth/drive", true},
		{"https://www.googleapis.com/auth/drive.readonly", true},
		{"https://www.googleapis.com/auth/drive.file", true},
		//
		{"https://www.googleapis.com/auth/cloud-platform", false},
		{"https://www.googleapis.com/auth/userinfo.email", false},
		{"https://www.googleapis.com/auth/userinfo.profile", false},
		{"", false},
		{"random-scope", false},
	}
	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			got := hasRestrictedScope(tt.scope)
			if got != tt.expected {
				t.Errorf("hasRestrictedScope(%q) = %v，expected %v", tt.scope, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// BuildAuthorizationURL
// ---------------------------------------------------------------------------

func TestBuildAuthorizationURL(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-secret")

	authURL, err := BuildAuthorizationURL(
		OAuthConfig{},
		"test-state",
		"test-challenge",
		"https://example.com/callback",
		"",
		"code_assist",
	)
	if err != nil {
		t.Fatalf("BuildAuthorizationURL() error: %v", err)
	}

	//
	checks := []string{
		"response_type=code",
		"client_id=" + GeminiCLIOAuthClientID,
		"redirect_uri=",
		"state=test-state",
		"code_challenge=test-challenge",
		"code_challenge_method=S256",
		"access_type=offline",
		"prompt=consent",
		"include_granted_scopes=true",
	}
	for _, check := range checks {
		if !strings.Contains(authURL, check) {
			t.Errorf("BuildAuthorizationURL() URL missing parameter %q\nURL: %s", check, authURL)
		}
	}

	//
	if strings.Contains(authURL, "project_id=") {
		t.Errorf("BuildAuthorizationURL() should not include project_id parameter when projectID is empty")
	}

	// URL
	if !strings.HasPrefix(authURL, AuthorizeURL+"?") {
		t.Errorf("BuildAuthorizationURL() URL should start with %s?, got: %s", AuthorizeURL, authURL)
	}
}

func TestBuildAuthorizationURL_EmptyRedirectURI(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-secret")

	_, err := BuildAuthorizationURL(
		OAuthConfig{},
		"test-state",
		"test-challenge",
		"", // empty redirectURI
		"",
		"code_assist",
	)
	if err == nil {
		t.Error("BuildAuthorizationURL() empty redirectURI should cause an error")
	}
	if !strings.Contains(err.Error(), "redirect_uri") {
		t.Errorf("error message should contain 'redirect_uri', got: %v", err)
	}
}

func TestBuildAuthorizationURL_WithProjectID(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-secret")

	authURL, err := BuildAuthorizationURL(
		OAuthConfig{},
		"test-state",
		"test-challenge",
		"https://example.com/callback",
		"my-project-123",
		"code_assist",
	)
	if err != nil {
		t.Fatalf("BuildAuthorizationURL() error: %v", err)
	}
	if !strings.Contains(authURL, "project_id=my-project-123") {
		t.Errorf("BuildAuthorizationURL() should include project_id parameter when projectID is present\nURL: %s", authURL)
	}
}

func TestBuildAuthorizationURL_UsesBuiltinSecretFallback(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "")

	authURL, err := BuildAuthorizationURL(
		OAuthConfig{},
		"test-state",
		"test-challenge",
		"https://example.com/callback",
		"",
		"code_assist",
	)
	if err != nil {
		t.Fatalf("BuildAuthorizationURL() should not cause an error: %v", err)
	}
	if !strings.Contains(authURL, "client_id="+GeminiCLIOAuthClientID) {
		t.Errorf("should use built-in Gemini CLI client_id, actual URL: %s", authURL)
	}
}

// ---------------------------------------------------------------------------
// EffectiveOAuthConfig
// ---------------------------------------------------------------------------

func TestEffectiveOAuthConfig_GoogleOne(t *testing.T) {
	//
	//
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	tests := []struct {
		name         string
		input        OAuthConfig
		oauthType    string
		wantClientID string
		wantScopes   string
		wantErr      bool
	}{
		{
			name:         "Google One uses built-in client (empty config)",
			input:        OAuthConfig{},
			oauthType:    "google_one",
			wantClientID: GeminiCLIOAuthClientID,
			wantScopes:   DefaultCodeAssistScopes,
			wantErr:      false,
		},
		{
			name: "Google One uses custom client (uses custom when custom credentials provided)",
			input: OAuthConfig{
				ClientID:     "custom-client-id",
				ClientSecret: "custom-client-secret",
			},
			oauthType:    "google_one",
			wantClientID: "custom-client-id",
			wantScopes:   DefaultCodeAssistScopes,
			wantErr:      false,
		},
		{
			name: "Google One built-in client + custom scopes (should filter restricted scopes)",
			input: OAuthConfig{
				Scopes: "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language.retriever https://www.googleapis.com/auth/drive.readonly",
			},
			oauthType:    "google_one",
			wantClientID: GeminiCLIOAuthClientID,
			wantScopes:   "https://www.googleapis.com/auth/cloud-platform",
			wantErr:      false,
		},
		{
			name: "Google One built-in client + only restricted scopes (should fallback to default)",
			input: OAuthConfig{
				Scopes: "https://www.googleapis.com/auth/generative-language.retriever https://www.googleapis.com/auth/drive.readonly",
			},
			oauthType:    "google_one",
			wantClientID: GeminiCLIOAuthClientID,
			wantScopes:   DefaultCodeAssistScopes,
			wantErr:      false,
		},
		{
			name:         "Code Assist uses built-in client",
			input:        OAuthConfig{},
			oauthType:    "code_assist",
			wantClientID: GeminiCLIOAuthClientID,
			wantScopes:   DefaultCodeAssistScopes,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EffectiveOAuthConfig(tt.input, tt.oauthType)
			if (err != nil) != tt.wantErr {
				t.Errorf("EffectiveOAuthConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if got.ClientID != tt.wantClientID {
				t.Errorf("EffectiveOAuthConfig() ClientID = %v, want %v", got.ClientID, tt.wantClientID)
			}
			if got.Scopes != tt.wantScopes {
				t.Errorf("EffectiveOAuthConfig() Scopes = %v, want %v", got.Scopes, tt.wantScopes)
			}
		})
	}
}

func TestEffectiveOAuthConfig_ScopeFiltering(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	// +
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		Scopes: "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/generative-language.retriever https://www.googleapis.com/auth/drive.readonly https://www.googleapis.com/auth/userinfo.profile",
	}, "google_one")

	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}

	//
	//
	if strings.Contains(cfg.Scopes, "generative-language") {
		t.Errorf("Scopes should not contain generative-language when using built-in client, got: %v", cfg.Scopes)
	}
	if strings.Contains(cfg.Scopes, "drive") {
		t.Errorf("Scopes should not contain drive when using built-in client, got: %v", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "cloud-platform") {
		t.Errorf("Scopes should contain cloud-platform, got: %v", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "userinfo.email") {
		t.Errorf("Scopes should contain userinfo.email, got: %v", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "userinfo.profile") {
		t.Errorf("Scopes should contain userinfo.profile, got: %v", cfg.Scopes)
	}
}

// ---------------------------------------------------------------------------
// EffectiveOAuthConfig
// ---------------------------------------------------------------------------

func TestEffectiveOAuthConfig_OnlyClientID_NoSecret(t *testing.T) {
	//
	_, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID: "some-client-id",
	}, "code_assist")
	if err == nil {
		t.Error("providing only ClientID without ClientSecret should cause an error")
	}
	if !strings.Contains(err.Error(), "client_id") || !strings.Contains(err.Error(), "client_secret") {
		t.Errorf("error message should mention client_id and client_secret, got: %v", err)
	}
}

func TestEffectiveOAuthConfig_OnlyClientSecret_NoID(t *testing.T) {
	//
	_, err := EffectiveOAuthConfig(OAuthConfig{
		ClientSecret: "some-client-secret",
	}, "code_assist")
	if err == nil {
		t.Error("providing only ClientSecret without ClientID should cause an error")
	}
	if !strings.Contains(err.Error(), "client_id") || !strings.Contains(err.Error(), "client_secret") {
		t.Errorf("error message should mention client_id and client_secret, got: %v", err)
	}
}

func TestEffectiveOAuthConfig_AIStudio_DefaultScopes_BuiltinClient(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	// ai_studio >
	cfg, err := EffectiveOAuthConfig(OAuthConfig{}, "ai_studio")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if cfg.Scopes != DefaultCodeAssistScopes {
		t.Errorf("ai_studio + built-in client should use DefaultCodeAssistScopes, got: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_AIStudio_DefaultScopes_CustomClient(t *testing.T) {
	// ai_studio >
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID:     "custom-id",
		ClientSecret: "custom-secret",
	}, "ai_studio")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if cfg.Scopes != DefaultAIStudioScopes {
		t.Errorf("ai_studio + custom client should use DefaultAIStudioScopes, got: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_AIStudio_ScopeNormalization(t *testing.T) {
	// ai_studio
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID:     "custom-id",
		ClientSecret: "custom-secret",
		Scopes:       "https://www.googleapis.com/auth/generative-language https://www.googleapis.com/auth/cloud-platform",
	}, "ai_studio")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if strings.Contains(cfg.Scopes, "auth/generative-language ") || strings.HasSuffix(cfg.Scopes, "auth/generative-language") {
		//
		parts := strings.Fields(cfg.Scopes)
		for _, p := range parts {
			if p == "https://www.googleapis.com/auth/generative-language" {
				t.Errorf("ai_studio should normalize generative-language to generative-language.retriever, actual scopes: %q", cfg.Scopes)
			}
		}
	}
	if !strings.Contains(cfg.Scopes, "generative-language.retriever") {
		t.Errorf("ai_studio after normalization should contain generative-language.retriever, got: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_CommaSeparatedScopes(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	//
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID:     "custom-id",
		ClientSecret: "custom-secret",
		Scopes:       "https://www.googleapis.com/auth/cloud-platform,https://www.googleapis.com/auth/userinfo.email",
	}, "code_assist")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if strings.Contains(cfg.Scopes, ",") {
		t.Errorf("comma-separated scopes should be normalized to space-separated, got: %q", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "cloud-platform") {
		t.Errorf("after normalization should contain cloud-platform, got: %q", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "userinfo.email") {
		t.Errorf("after normalization should contain userinfo.email, got: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_MixedCommaAndSpaceScopes(t *testing.T) {
	//
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID:     "custom-id",
		ClientSecret: "custom-secret",
		Scopes:       "https://www.googleapis.com/auth/cloud-platform, https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile",
	}, "code_assist")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	parts := strings.Fields(cfg.Scopes)
	if len(parts) != 3 {
		t.Errorf("after normalization should have 3 scopes, got: %d，scopes: %q", len(parts), cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_WhitespaceTriming(t *testing.T) {
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID:     "  custom-id  ",
		ClientSecret: "  custom-secret  ",
		Scopes:       "  https://www.googleapis.com/auth/cloud-platform  ",
	}, "code_assist")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if cfg.ClientID != "custom-id" {
		t.Errorf("ClientID should have leading/trailing whitespace trimmed, got: %q", cfg.ClientID)
	}
	if cfg.ClientSecret != "custom-secret" {
		t.Errorf("ClientSecret should have leading/trailing whitespace trimmed, got: %q", cfg.ClientSecret)
	}
	if cfg.Scopes != "https://www.googleapis.com/auth/cloud-platform" {
		t.Errorf("Scopes should have leading/trailing whitespace trimmed, got: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_NoEnvSecret(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "")

	cfg, err := EffectiveOAuthConfig(OAuthConfig{}, "code_assist")
	if err != nil {
		t.Fatalf("when env var is not set should fallback to built-in secret, actual error: %v", err)
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		t.Error("ClientSecret should not be empty")
	}
	if cfg.ClientID != GeminiCLIOAuthClientID {
		t.Errorf("ClientID should fallback to built-in client ID, got: %q", cfg.ClientID)
	}
}

func TestEffectiveOAuthConfig_AIStudio_BuiltinClient_CustomScopes(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	// ai_studio + + >
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		Scopes: "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language.retriever",
	}, "ai_studio")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	//
	if strings.Contains(cfg.Scopes, "generative-language") {
		t.Errorf("ai_studio + built-in client should filter restricted scopes, got: %q", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "cloud-platform") {
		t.Errorf("should preserve cloud-platform scope, got: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_UnknownOAuthType_DefaultScopes(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	//
	cfg, err := EffectiveOAuthConfig(OAuthConfig{}, "unknown_type")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if cfg.Scopes != DefaultCodeAssistScopes {
		t.Errorf("unknown oauthType should use DefaultCodeAssistScopes, got: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_EmptyOAuthType_DefaultScopes(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	//
	cfg, err := EffectiveOAuthConfig(OAuthConfig{}, "")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if cfg.Scopes != DefaultCodeAssistScopes {
		t.Errorf("empty oauthType should use DefaultCodeAssistScopes, got: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_CustomClient_NoScopeFiltering(t *testing.T) {
	// + google_one + >
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID:     "custom-id",
		ClientSecret: "custom-secret",
		Scopes:       "https://www.googleapis.com/auth/generative-language.retriever https://www.googleapis.com/auth/drive.readonly",
	}, "google_one")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	//
	if !strings.Contains(cfg.Scopes, "generative-language.retriever") {
		t.Errorf("custom client should not filter generative-language.retriever, got: %q", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "drive.readonly") {
		t.Errorf("custom client should not filter drive.readonly, got: %q", cfg.Scopes)
	}
}
