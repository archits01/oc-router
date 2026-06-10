//go:build unit

package antigravity

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// getClientSecret
// ---------------------------------------------------------------------------

func TestGetClientSecret_EnvVarSet(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = ""
	t.Cleanup(func() { defaultClientSecret = old })
	t.Setenv(AntigravityOAuthClientSecretEnv, "my-secret-value")

	//
	defaultClientSecret = os.Getenv(AntigravityOAuthClientSecretEnv)

	secret, err := getClientSecret()
	if err != nil {
		t.Fatalf("failed to get client_secret: %v", err)
	}
	if secret != "my-secret-value" {
		t.Errorf("client_secret mismatch: got %s, want my-secret-value", secret)
	}
}

func TestGetClientSecret_EnvVarEmpty(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = ""
	t.Cleanup(func() { defaultClientSecret = old })

	_, err := getClientSecret()
	if err == nil {
		t.Fatal("should return error when defaultClientSecret is empty")
	}
	if !strings.Contains(err.Error(), AntigravityOAuthClientSecretEnv) {
		t.Errorf("error message should contain environment variable name: got %s", err.Error())
	}
}

func TestGetClientSecret_EnvVarNotSet(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = ""
	t.Cleanup(func() { defaultClientSecret = old })

	_, err := getClientSecret()
	if err == nil {
		t.Fatal("should return error when defaultClientSecret is empty")
	}
}

func TestGetClientSecret_EnvVarContainsSpaces(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "   "
	t.Cleanup(func() { defaultClientSecret = old })

	_, err := getClientSecret()
	if err == nil {
		t.Fatal("should return error when defaultClientSecret contains only spaces")
	}
}

func TestGetClientSecret_EnvVarHasLeadingTrailingSpaces(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "  valid-secret  "
	t.Cleanup(func() { defaultClientSecret = old })

	secret, err := getClientSecret()
	if err != nil {
		t.Fatalf("failed to get client_secret: %v", err)
	}
	if secret != "valid-secret" {
		t.Errorf("should trim leading/trailing spaces: got %q, want %q", secret, "valid-secret")
	}
}

// ---------------------------------------------------------------------------
// ForwardBaseURLs
// ---------------------------------------------------------------------------

func TestForwardBaseURLs_DailyFirst(t *testing.T) {
	urls := ForwardBaseURLs()
	if len(urls) == 0 {
		t.Fatal("ForwardBaseURLs returned empty list")
	}

	// daily URL
	if urls[0] != antigravityDailyBaseURL {
		t.Errorf("first URL should be daily: got %s, want %s", urls[0], antigravityDailyBaseURL)
	}

	//
	if len(urls) != len(BaseURLs) {
		t.Errorf("URL count mismatch: got %d, want %d", len(urls), len(BaseURLs))
	}

	//
	found := false
	for _, u := range urls {
		if u == antigravityProdBaseURL {
			found = true
			break
		}
	}
	if !found {
		t.Error("ForwardBaseURLs is missing prod URL")
	}
}

func TestForwardBaseURLs_DoesNotModifyOriginalSlice(t *testing.T) {
	originalFirst := BaseURLs[0]
	_ = ForwardBaseURLs()
	//
	if BaseURLs[0] != originalFirst {
		t.Errorf("ForwardBaseURLs should not modify original BaseURLs: got %s, want %s", BaseURLs[0], originalFirst)
	}
}

// ---------------------------------------------------------------------------
// URLAvailability
// ---------------------------------------------------------------------------

func TestNewURLAvailability(t *testing.T) {
	ua := NewURLAvailability(5 * time.Minute)
	if ua == nil {
		t.Fatal("NewURLAvailability returned nil")
	}
	if ua.ttl != 5*time.Minute {
		t.Errorf("TTL mismatch: got %v, want 5m", ua.ttl)
	}
	if ua.unavailable == nil {
		t.Error("unavailable map should not be nil")
	}
}

func TestURLAvailability_MarkUnavailable(t *testing.T) {
	ua := NewURLAvailability(5 * time.Minute)
	testURL := "https://example.com"

	ua.MarkUnavailable(testURL)

	if ua.IsAvailable(testURL) {
		t.Error("IsAvailable should return false after marking as unavailable")
	}
}

func TestURLAvailability_MarkSuccess(t *testing.T) {
	ua := NewURLAvailability(5 * time.Minute)
	testURL := "https://example.com"

	ua.MarkUnavailable(testURL)
	if ua.IsAvailable(testURL) {
		t.Error("should be unavailable after marking as unavailable")
	}

	ua.MarkSuccess(testURL)
	if !ua.IsAvailable(testURL) {
		t.Error("should be available again after MarkSuccess")
	}

	//
	ua.mu.RLock()
	if ua.lastSuccess != testURL {
		t.Errorf("lastSuccess mismatch: got %s, want %s", ua.lastSuccess, testURL)
	}
	ua.mu.RUnlock()
}

func TestURLAvailability_IsAvailable_TTLExpired(t *testing.T) {
	//
	ua := NewURLAvailability(1 * time.Millisecond)
	testURL := "https://example.com"

	ua.MarkUnavailable(testURL)
	//
	time.Sleep(5 * time.Millisecond)

	if !ua.IsAvailable(testURL) {
		t.Error("URL should be available again after TTL expires")
	}
}

func TestURLAvailability_IsAvailable_UnmarkedURL(t *testing.T) {
	ua := NewURLAvailability(5 * time.Minute)
	if !ua.IsAvailable("https://never-marked.com") {
		t.Error("unmarked URL should be available by default")
	}
}

func TestURLAvailability_GetAvailableURLs(t *testing.T) {
	ua := NewURLAvailability(10 * time.Minute)

	//
	urls := ua.GetAvailableURLs()
	if len(urls) != len(BaseURLs) {
		t.Errorf("available URL count mismatch: got %d, want %d", len(urls), len(BaseURLs))
	}
}

func TestURLAvailability_GetAvailableURLs_OneMarkedUnavailable(t *testing.T) {
	ua := NewURLAvailability(10 * time.Minute)

	if len(BaseURLs) < 2 {
		t.Skip("BaseURLs has fewer than 2 entries, skipping this test")
	}

	ua.MarkUnavailable(BaseURLs[0])
	urls := ua.GetAvailableURLs()

	//
	for _, u := range urls {
		if u == BaseURLs[0] {
			t.Errorf("URL marked as unavailable should not appear in available list: %s", BaseURLs[0])
		}
	}
}

func TestURLAvailability_GetAvailableURLsWithBase(t *testing.T) {
	ua := NewURLAvailability(10 * time.Minute)
	customURLs := []string{"https://a.com", "https://b.com", "https://c.com"}

	urls := ua.GetAvailableURLsWithBase(customURLs)
	if len(urls) != 3 {
		t.Errorf("available URL count mismatch: got %d, want 3", len(urls))
	}
}

func TestURLAvailability_GetAvailableURLsWithBase_LastSuccessFirst(t *testing.T) {
	ua := NewURLAvailability(10 * time.Minute)
	customURLs := []string{"https://a.com", "https://b.com", "https://c.com"}

	ua.MarkSuccess("https://c.com")

	urls := ua.GetAvailableURLsWithBase(customURLs)
	if len(urls) != 3 {
		t.Fatalf("available URL count mismatch: got %d, want 3", len(urls))
	}
	// c.com
	if urls[0] != "https://c.com" {
		t.Errorf("lastSuccess should be first: got %s, want https://c.com", urls[0])
	}
	if urls[1] != "https://a.com" {
		t.Errorf("second should be a.com: got %s", urls[1])
	}
	if urls[2] != "https://b.com" {
		t.Errorf("third should be b.com: got %s", urls[2])
	}
}

func TestURLAvailability_GetAvailableURLsWithBase_LastSuccessUnavailable(t *testing.T) {
	ua := NewURLAvailability(10 * time.Minute)
	customURLs := []string{"https://a.com", "https://b.com"}

	ua.MarkSuccess("https://b.com")
	ua.MarkUnavailable("https://b.com")

	urls := ua.GetAvailableURLsWithBase(customURLs)
	// b.com
	if len(urls) != 1 {
		t.Fatalf("available URL count mismatch: got %d, want 1", len(urls))
	}
	if urls[0] != "https://a.com" {
		t.Errorf("only a.com should be available: got %s", urls[0])
	}
}

func TestURLAvailability_GetAvailableURLsWithBase_LastSuccessNotInList(t *testing.T) {
	ua := NewURLAvailability(10 * time.Minute)
	customURLs := []string{"https://a.com", "https://b.com"}

	ua.MarkSuccess("https://not-in-list.com")

	urls := ua.GetAvailableURLsWithBase(customURLs)
	// lastSuccess
	if len(urls) != 2 {
		t.Fatalf("available URL count mismatch: got %d, want 2", len(urls))
	}
}

// ---------------------------------------------------------------------------
// SessionStore
// ---------------------------------------------------------------------------

func TestNewSessionStore(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()

	if store == nil {
		t.Fatal("NewSessionStore returned nil")
	}
	if store.sessions == nil {
		t.Error("sessions map should not be nil")
	}
}

func TestSessionStore_SetAndGet(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()

	session := &OAuthSession{
		State:        "test-state",
		CodeVerifier: "test-verifier",
		ProxyURL:     "http://proxy.example.com",
		CreatedAt:    time.Now(),
	}

	store.Set("session-1", session)

	got, ok := store.Get("session-1")
	if !ok {
		t.Fatal("Get should return true")
	}
	if got.State != "test-state" {
		t.Errorf("State mismatch: got %s", got.State)
	}
	if got.CodeVerifier != "test-verifier" {
		t.Errorf("CodeVerifier mismatch: got %s", got.CodeVerifier)
	}
	if got.ProxyURL != "http://proxy.example.com" {
		t.Errorf("ProxyURL mismatch: got %s", got.ProxyURL)
	}
}

func TestSessionStore_Get_does not exist(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()

	_, ok := store.Get("nonexistent")
	if ok {
		t.Error("non-existent session should return false")
	}
}

func TestSessionStore_Get_Expired(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()

	session := &OAuthSession{
		State:     "expired-state",
		CreatedAt: time.Now().Add(-SessionTTL - time.Minute), // expired
	}

	store.Set("expired-session", session)

	_, ok := store.Get("expired-session")
	if ok {
		t.Error("expired session should return false")
	}
}

func TestSessionStore_Delete(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()

	session := &OAuthSession{
		State:     "to-delete",
		CreatedAt: time.Now(),
	}

	store.Set("del-session", session)
	store.Delete("del-session")

	_, ok := store.Get("del-session")
	if ok {
		t.Error("Get should return false after delete")
	}
}

func TestSessionStore_Delete_does not exist(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()

	//
	store.Delete("nonexistent")
}

func TestSessionStore_Stop(t *testing.T) {
	store := NewSessionStore()
	store.Stop()

	//
	store.Stop()
}

func TestSessionStore_MultipleSessions(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()

	for i := 0; i < 10; i++ {
		session := &OAuthSession{
			State:     "state-" + string(rune('0'+i)),
			CreatedAt: time.Now(),
		}
		store.Set("session-"+string(rune('0'+i)), session)
	}

	for i := 0; i < 10; i++ {
		_, ok := store.Get("session-" + string(rune('0'+i)))
		if !ok {
			t.Errorf("session-%d should exist", i)
		}
	}
}

// ---------------------------------------------------------------------------
// GenerateRandomBytes
// ---------------------------------------------------------------------------

func TestGenerateRandomBytes_CorrectLength(t *testing.T) {
	sizes := []int{0, 1, 16, 32, 64, 128}
	for _, size := range sizes {
		b, err := GenerateRandomBytes(size)
		if err != nil {
			t.Fatalf("GenerateRandomBytes(%d) failed: %v", size, err)
		}
		if len(b) != size {
			t.Errorf("length mismatch: got %d, want %d", len(b), size)
		}
	}
}

func TestGenerateRandomBytes_DifferentCallsProduceDifferentResults(t *testing.T) {
	b1, err := GenerateRandomBytes(32)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	b2, err := GenerateRandomBytes(32)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if string(b1) == string(b2) {
		t.Error("two generated random bytes are identical, which is extremely unlikely and may indicate a problem")
	}
}

// ---------------------------------------------------------------------------
// GenerateState
// ---------------------------------------------------------------------------

func TestGenerateState_ReturnValueFormat(t *testing.T) {
	state, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState failed: %v", err)
	}
	if state == "" {
		t.Error("GenerateState returned empty string")
	}
	// base64url +, /, =
	if strings.ContainsAny(state, "+/=") {
		t.Errorf("GenerateState return value contains non-base64url characters: %s", state)
	}
	// 32 =
	if len(state) != 43 {
		t.Errorf("GenerateState return value length mismatch: got %d, want 43", len(state))
	}
}

func TestGenerateState_Uniqueness(t *testing.T) {
	s1, _ := GenerateState()
	s2, _ := GenerateState()
	if s1 == s2 {
		t.Error("two GenerateState results are identical")
	}
}

// ---------------------------------------------------------------------------
// GenerateSessionID
// ---------------------------------------------------------------------------

func TestGenerateSessionID_ReturnValueFormat(t *testing.T) {
	id, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("GenerateSessionID failed: %v", err)
	}
	if id == "" {
		t.Error("GenerateSessionID returned empty string")
	}
	// 16
	if len(id) != 32 {
		t.Errorf("GenerateSessionID return value length mismatch: got %d, want 32", len(id))
	}
	//
	if _, err := hex.DecodeString(id); err != nil {
		t.Errorf("GenerateSessionID return value is not a valid hex string: %s, err: %v", id, err)
	}
}

func TestGenerateSessionID_Uniqueness(t *testing.T) {
	id1, _ := GenerateSessionID()
	id2, _ := GenerateSessionID()
	if id1 == id2 {
		t.Error("two GenerateSessionID results are identical")
	}
}

// ---------------------------------------------------------------------------
// GenerateCodeVerifier
// ---------------------------------------------------------------------------

func TestGenerateCodeVerifier_ReturnValueFormat(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier failed: %v", err)
	}
	if verifier == "" {
		t.Error("GenerateCodeVerifier returned empty string")
	}
	// base64url +, /, =
	if strings.ContainsAny(verifier, "+/=") {
		t.Errorf("GenerateCodeVerifier return value contains non-base64url characters: %s", verifier)
	}
	// 32
	if len(verifier) != 43 {
		t.Errorf("GenerateCodeVerifier return value length mismatch: got %d, want 43", len(verifier))
	}
}

func TestGenerateCodeVerifier_Uniqueness(t *testing.T) {
	v1, _ := GenerateCodeVerifier()
	v2, _ := GenerateCodeVerifier()
	if v1 == v2 {
		t.Error("two GenerateCodeVerifier results are identical")
	}
}

// ---------------------------------------------------------------------------
// GenerateCodeChallenge
// ---------------------------------------------------------------------------

func TestGenerateCodeChallenge_SHA256_Base64URL(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	challenge := GenerateCodeChallenge(verifier)

	hash := sha256.Sum256([]byte(verifier))
	expected := strings.TrimRight(base64.URLEncoding.EncodeToString(hash[:]), "=")

	if challenge != expected {
		t.Errorf("CodeChallenge mismatch: got %s, want %s", challenge, expected)
	}
}

func TestGenerateCodeChallenge_NoPaddingCharacters(t *testing.T) {
	challenge := GenerateCodeChallenge("test-verifier")
	if strings.Contains(challenge, "=") {
		t.Errorf("CodeChallenge should not contain = padding characters: %s", challenge)
	}
}

func TestGenerateCodeChallenge_NoNonURLSafeCharacters(t *testing.T) {
	challenge := GenerateCodeChallenge("another-verifier")
	if strings.ContainsAny(challenge, "+/") {
		t.Errorf("CodeChallenge should not contain + or / characters: %s", challenge)
	}
}

func TestGenerateCodeChallenge_SameInputSameOutput(t *testing.T) {
	c1 := GenerateCodeChallenge("same-verifier")
	c2 := GenerateCodeChallenge("same-verifier")
	if c1 != c2 {
		t.Errorf("same input should produce same output: got %s and %s", c1, c2)
	}
}

func TestGenerateCodeChallenge_DifferentInputDifferentOutput(t *testing.T) {
	c1 := GenerateCodeChallenge("verifier-1")
	c2 := GenerateCodeChallenge("verifier-2")
	if c1 == c2 {
		t.Error("different input should produce different output")
	}
}

// ---------------------------------------------------------------------------
// BuildAuthorizationURL
// ---------------------------------------------------------------------------

func TestBuildAuthorizationURL_ParameterValidation(t *testing.T) {
	state := "test-state-123"
	codeChallenge := "test-challenge-abc"

	authURL := BuildAuthorizationURL(state, codeChallenge)

	//
	if !strings.HasPrefix(authURL, AuthorizeURL+"?") {
		t.Errorf("URL should start with %s?: got %s", AuthorizeURL, authURL)
	}

	//
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse URL failed: %v", err)
	}

	params := parsed.Query()

	expectedParams := map[string]string{
		"client_id":              ClientID,
		"redirect_uri":           RedirectURI,
		"response_type":          "code",
		"scope":                  Scopes,
		"state":                  state,
		"code_challenge":         codeChallenge,
		"code_challenge_method":  "S256",
		"access_type":            "offline",
		"prompt":                 "consent",
		"include_granted_scopes": "true",
	}

	for key, want := range expectedParams {
		got := params.Get(key)
		if got != want {
			t.Errorf("parameter %s mismatch: got %q, want %q", key, got, want)
		}
	}
}

func TestBuildAuthorizationURL_ParameterCount(t *testing.T) {
	authURL := BuildAuthorizationURL("s", "c")
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse URL failed: %v", err)
	}

	params := parsed.Query()
	expectedCount := 10
	if len(params) != expectedCount {
		t.Errorf("parameter count mismatch: got %d, want %d", len(params), expectedCount)
	}
}

func TestBuildAuthorizationURL_SpecialCharacterEncoding(t *testing.T) {
	state := "state+with/special=chars"
	codeChallenge := "challenge+value"

	authURL := BuildAuthorizationURL(state, codeChallenge)

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse URL failed: %v", err)
	}

	if got := parsed.Query().Get("state"); got != state {
		t.Errorf("state parameter encode/decode mismatch: got %q, want %q", got, state)
	}
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

func TestConstants_CorrectValues(t *testing.T) {
	if AuthorizeURL != "https://accounts.google.com/o/oauth2/v2/auth" {
		t.Errorf("AuthorizeURL mismatch: got %s", AuthorizeURL)
	}
	if TokenURL != "https://oauth2.googleapis.com/token" {
		t.Errorf("TokenURL mismatch: got %s", TokenURL)
	}
	if UserInfoURL != "https://www.googleapis.com/oauth2/v2/userinfo" {
		t.Errorf("UserInfoURL mismatch: got %s", UserInfoURL)
	}
	if ClientID != "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com" {
		t.Errorf("ClientID mismatch: got %s", ClientID)
	}
	secret, err := getClientSecret()
	if err != nil {
		t.Fatalf("getClientSecret should return default value, but got error: %v", err)
	}
	if secret != "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" {
		t.Errorf("default client_secret mismatch: got %s", secret)
	}
	if RedirectURI != "http://localhost:8085/callback" {
		t.Errorf("RedirectURI mismatch: got %s", RedirectURI)
	}
	if GetUserAgent() != "antigravity/1.23.2 windows/amd64" {
		t.Errorf("UserAgent mismatch: got %s", GetUserAgent())
	}
	if SessionTTL != 30*time.Minute {
		t.Errorf("SessionTTL mismatch: got %v", SessionTTL)
	}
	if URLAvailabilityTTL != 5*time.Minute {
		t.Errorf("URLAvailabilityTTL mismatch: got %v", URLAvailabilityTTL)
	}
}

func TestScopes_ContainsRequiredScopes(t *testing.T) {
	expectedScopes := []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
		"https://www.googleapis.com/auth/cclog",
		"https://www.googleapis.com/auth/experimentsandconfigs",
	}

	for _, scope := range expectedScopes {
		if !strings.Contains(Scopes, scope) {
			t.Errorf("Scopes is missing %s", scope)
		}
	}
}
