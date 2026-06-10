package antigravity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	// Google OAuth
	AuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL     = "https://oauth2.googleapis.com/token"
	UserInfoURL  = "https://www.googleapis.com/oauth2/v2/userinfo"

	// Antigravity OAuth
	ClientID = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"

	// AntigravityOAuthClientSecretEnv
	AntigravityOAuthClientSecretEnv = "ANTIGRAVITY_OAUTH_CLIENT_SECRET"

	// AntigravityUserAgentVersionEnv
	AntigravityUserAgentVersionEnv = "ANTIGRAVITY_USER_AGENT_VERSION"

	// DefaultUserAgentVersion
	DefaultUserAgentVersion = "1.23.2"

	//
	RedirectURI = "http://localhost:8085/callback"

	// OAuth scopes
	Scopes = "https://www.googleapis.com/auth/cloud-platform " +
		"https://www.googleapis.com/auth/userinfo.email " +
		"https://www.googleapis.com/auth/userinfo.profile " +
		"https://www.googleapis.com/auth/cclog " +
		"https://www.googleapis.com/auth/experimentsandconfigs"

	// Session
	SessionTTL = 30 * time.Minute

	// URL
	URLAvailabilityTTL = 5 * time.Minute

	// Antigravity API
	antigravityProdBaseURL  = "https://cloudcode-pa.googleapis.com"
	antigravityDailyBaseURL = "https://daily-cloudcode-pa.sandbox.googleapis.com"
)

var userAgentVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// UserAgentVersionResolver
type UserAgentVersionResolver func(ctx context.Context) string

var (
	// defaultUserAgentVersion
	defaultUserAgentVersion  = DefaultUserAgentVersion
	userAgentVersionMu       sync.RWMutex
	userAgentVersionResolver UserAgentVersionResolver
)

// defaultClientSecret
var defaultClientSecret = "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"

func init() {
	if version := NormalizeUserAgentVersion(os.Getenv(AntigravityUserAgentVersionEnv)); version != "" {
		defaultUserAgentVersion = version
	}
	//
	if secret := os.Getenv(AntigravityOAuthClientSecretEnv); secret != "" {
		defaultClientSecret = secret
	}
}

// NormalizeUserAgentVersion
func NormalizeUserAgentVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || !userAgentVersionPattern.MatchString(version) {
		return ""
	}
	return version
}

// GetDefaultUserAgentVersion
func GetDefaultUserAgentVersion() string {
	return defaultUserAgentVersion
}

// SetUserAgentVersionResolver
func SetUserAgentVersionResolver(resolver UserAgentVersionResolver) {
	userAgentVersionMu.Lock()
	defer userAgentVersionMu.Unlock()
	userAgentVersionResolver = resolver
}

// GetUserAgentVersionForContext
func GetUserAgentVersionForContext(ctx context.Context) string {
	if ctx == nil {
		ctx = context.Background()
	}
	userAgentVersionMu.RLock()
	resolver := userAgentVersionResolver
	userAgentVersionMu.RUnlock()
	if resolver != nil {
		if version := NormalizeUserAgentVersion(resolver(ctx)); version != "" {
			return version
		}
	}
	return defaultUserAgentVersion
}

// BuildUserAgent
func BuildUserAgent(version string) string {
	if normalized := NormalizeUserAgentVersion(version); normalized != "" {
		return fmt.Sprintf("antigravity/%s windows/amd64", normalized)
	}
	return fmt.Sprintf("antigravity/%s windows/amd64", defaultUserAgentVersion)
}

// GetUserAgentForContext
func GetUserAgentForContext(ctx context.Context) string {
	return BuildUserAgent(GetUserAgentVersionForContext(ctx))
}

// GetUserAgent
func GetUserAgent() string {
	return GetUserAgentForContext(context.Background())
}

func getClientSecret() (string, error) {
	if v := strings.TrimSpace(defaultClientSecret); v != "" {
		return v, nil
	}
	return "", infraerrors.Newf(http.StatusBadRequest, "ANTIGRAVITY_OAUTH_CLIENT_SECRET_MISSING", "missing antigravity oauth client_secret; set %s", AntigravityOAuthClientSecretEnv)
}

// BaseURLs
var BaseURLs = []string{
	antigravityProdBaseURL,  // prod (primary)
	antigravityDailyBaseURL, // daily sandbox (backup)
}

// BaseURL
var BaseURL = BaseURLs[0]

// ForwardBaseURLs
func ForwardBaseURLs() []string {
	if len(BaseURLs) == 0 {
		return nil
	}
	urls := append([]string(nil), BaseURLs...)
	dailyIndex := -1
	for i, url := range urls {
		if url == antigravityDailyBaseURL {
			dailyIndex = i
			break
		}
	}
	if dailyIndex <= 0 {
		return urls
	}
	reordered := make([]string, 0, len(urls))
	reordered = append(reordered, urls[dailyIndex])
	for i, url := range urls {
		if i == dailyIndex {
			continue
		}
		reordered = append(reordered, url)
	}
	return reordered
}

// URLAvailability
type URLAvailability struct {
	mu          sync.RWMutex
	unavailable map[string]time.Time // URL -> recovery time
	ttl         time.Duration
	lastSuccess string // URL of most recent successful request, used preferentially
}

// DefaultURLAvailability
var DefaultURLAvailability = NewURLAvailability(URLAvailabilityTTL)

// NewURLAvailability
func NewURLAvailability(ttl time.Duration) *URLAvailability {
	return &URLAvailability{
		unavailable: make(map[string]time.Time),
		ttl:         ttl,
	}
}

// MarkUnavailable
func (u *URLAvailability) MarkUnavailable(url string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.unavailable[url] = time.Now().Add(u.ttl)
}

// MarkSuccess
func (u *URLAvailability) MarkSuccess(url string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.lastSuccess = url
	//
	delete(u.unavailable, url)
}

// IsAvailable
func (u *URLAvailability) IsAvailable(url string) bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	expiry, exists := u.unavailable[url]
	if !exists {
		return true
	}
	return time.Now().After(expiry)
}

// GetAvailableURLs
//
func (u *URLAvailability) GetAvailableURLs() []string {
	return u.GetAvailableURLsWithBase(BaseURLs)
}

// GetAvailableURLsWithBase
//
func (u *URLAvailability) GetAvailableURLsWithBase(baseURLs []string) []string {
	u.mu.RLock()
	defer u.mu.RUnlock()

	now := time.Now()
	result := make([]string, 0, len(baseURLs))

	//
	if u.lastSuccess != "" {
		found := false
		for _, url := range baseURLs {
			if url == u.lastSuccess {
				found = true
				break
			}
		}
		if found {
			expiry, exists := u.unavailable[u.lastSuccess]
			if !exists || now.After(expiry) {
				result = append(result, u.lastSuccess)
			}
		}
	}

	//
	for _, url := range baseURLs {
		//
		if url == u.lastSuccess {
			continue
		}
		expiry, exists := u.unavailable[url]
		if !exists || now.After(expiry) {
			result = append(result, url)
		}
	}
	return result
}

// OAuthSession
type OAuthSession struct {
	State        string    `json:"state"`
	CodeVerifier string    `json:"code_verifier"`
	ProxyURL     string    `json:"proxy_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// SessionStore OAuth session
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*OAuthSession
	stopCh   chan struct{}
}

func NewSessionStore() *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*OAuthSession),
		stopCh:   make(chan struct{}),
	}
	go store.cleanup()
	return store
}

func (s *SessionStore) Set(sessionID string, session *OAuthSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = session
}

func (s *SessionStore) Get(sessionID string) (*OAuthSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, false
	}
	if time.Since(session.CreatedAt) > SessionTTL {
		return nil, false
	}
	return session, true
}

func (s *SessionStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

func (s *SessionStore) Stop() {
	select {
	case <-s.stopCh:
		return
	default:
		close(s.stopCh)
	}
}

func (s *SessionStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			for id, session := range s.sessions {
				if time.Since(session.CreatedAt) > SessionTTL {
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func GenerateState() (string, error) {
	bytes, err := GenerateRandomBytes(32)
	if err != nil {
		return "", err
	}
	return base64URLEncode(bytes), nil
}

func GenerateSessionID() (string, error) {
	bytes, err := GenerateRandomBytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func GenerateCodeVerifier() (string, error) {
	bytes, err := GenerateRandomBytes(32)
	if err != nil {
		return "", err
	}
	return base64URLEncode(bytes), nil
}

func GenerateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64URLEncode(hash[:])
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

// BuildAuthorizationURL
func BuildAuthorizationURL(state, codeChallenge string) string {
	params := url.Values{}
	params.Set("client_id", ClientID)
	params.Set("redirect_uri", RedirectURI)
	params.Set("response_type", "code")
	params.Set("scope", Scopes)
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("include_granted_scopes", "true")

	return fmt.Sprintf("%s?%s", AuthorizeURL, params.Encode())
}
