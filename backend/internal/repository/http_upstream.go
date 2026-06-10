package repository

import (
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const (
	// directProxyKey:
	directProxyKey = "direct"
	// defaultMaxIdleConns:
	// HTTP/2
	defaultMaxIdleConns = 240
	// defaultMaxIdleConnsPerHost:
	defaultMaxIdleConnsPerHost = 120
	// defaultMaxConnsPerHost:
	defaultMaxConnsPerHost = 240
	// defaultIdleConnTimeout:
	defaultIdleConnTimeout = 90 * time.Second
	// defaultResponseHeaderTimeout:
	// LLM
	defaultResponseHeaderTimeout = 300 * time.Second
	// defaultMaxUpstreamClients:
	defaultMaxUpstreamClients = 5000
	// defaultClientIdleTTLSeconds:
	defaultClientIdleTTLSeconds = 900
	// OpenAI HTTP/2
	defaultOpenAIHTTP2FallbackErrorThreshold = 2
	defaultOpenAIHTTP2FallbackWindow         = 60 * time.Second
	defaultOpenAIHTTP2FallbackTTL            = 10 * time.Minute
)

const (
	upstreamProtocolModeDefault          = "default"
	upstreamProtocolModeOpenAIH1         = "openai_h1"
	upstreamProtocolModeOpenAIH2         = "openai_h2"
	upstreamProtocolModeOpenAIH1Fallback = "openai_h1_fallback"
)

var errUpstreamClientLimitReached = errors.New("upstream client cache limit reached")

// poolSettings
//
type poolSettings struct {
	maxIdleConns          int           // max total idle connections
	maxIdleConnsPerHost   int           // max idle connections per host
	maxConnsPerHost       int           // max connections per host (including active)
	idleConnTimeout       time.Duration // idle connection timeout
	responseHeaderTimeout time.Duration // response header wait timeout
}

type openAIHTTP2Settings struct {
	enabled                   bool
	allowProxyFallbackToHTTP1 bool
	fallbackErrorThreshold    int
	fallbackWindow            time.Duration
	fallbackTTL               time.Duration
}

// upstreamClientEntry
type upstreamClientEntry struct {
	client       *http.Client // HTTP client instance
	proxyKey     string       // proxy identifier (for detecting proxy changes)
	poolKey      string       // connection pool config identifier (for detecting config changes)
	protocolMode string       // protocol mode (default/openai_h1/openai_h2/openai_h1_fallback)
	lastUsed     int64        // last used timestamp (nanoseconds), for LRU eviction
	inFlight     int64        // current in-flight request count, not evictable when >0
}

type openAIHTTP2FallbackState struct {
	mu            sync.Mutex
	windowStart   time.Time
	errorCount    int
	fallbackUntil time.Time
}

// httpUpstreamService
//
//
// -
// -
// - +
//
// 1.
// 2.
// 6. HTTP/2
type httpUpstreamService struct {
	cfg     *config.Config                  // global config
	mu      sync.RWMutex                    // read-write lock protecting clients map
	clients map[string]*upstreamClientEntry // client cache pool, key determined by isolation strategy
	// OpenAI >H1 =
	openAIHTTP2Fallbacks sync.Map
}

// NewHTTPUpstream
//
//
//   - cfg:
//
//   - service.HTTPUpstream
func NewHTTPUpstream(cfg *config.Config) service.HTTPUpstream {
	return &httpUpstreamService{
		cfg:     cfg,
		clients: make(map[string]*upstreamClientEntry),
	}
}

// Do
//
//   - req: HTTP
//   - proxyURL:
//   - accountID:
//   - accountConcurrency:
//
//   - *http.Response: HTTP
//   - error:
//
//   -
//   - inFlight > 0
func (s *httpUpstreamService) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	if err := s.validateRequestHost(req); err != nil {
		return nil, err
	}
	profile := service.HTTPUpstreamProfileDefault
	if req != nil {
		profile = service.HTTPUpstreamProfileFromContext(req.Context())
	}

	entry, err := s.acquireClientWithProfile(proxyURL, accountID, accountConcurrency, profile)
	if err != nil {
		return nil, err
	}

	resp, err := entry.client.Do(req)
	if err != nil {
		s.recordOpenAIHTTP2Failure(profile, entry.protocolMode, entry.proxyKey, err)
		atomic.AddInt64(&entry.inFlight, -1)
		atomic.StoreInt64(&entry.lastUsed, time.Now().UnixNano())
		return nil, err
	}
	s.recordOpenAIHTTP2Success(profile, entry.protocolMode, entry.proxyKey)

	decompressResponseBody(resp)

	//
	resp.Body = wrapTrackedBody(resp.Body, func() {
		atomic.AddInt64(&entry.inFlight, -1)
		atomic.StoreInt64(&entry.lastUsed, time.Now().UnixNano())
	})

	return resp, nil
}

// DoWithTLS
//
// profile
// profile
func (s *httpUpstreamService) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	if profile == nil {
		return s.Do(req, proxyURL, accountID, accountConcurrency)
	}
	upstreamProfile := service.HTTPUpstreamProfileDefault
	if req != nil {
		upstreamProfile = service.HTTPUpstreamProfileFromContext(req.Context())
	}

	targetHost := ""
	if req != nil && req.URL != nil {
		targetHost = req.URL.Host
	}
	proxyInfo := "direct"
	if proxyURL != "" {
		proxyInfo = proxyURL
	}
	slog.Debug("tls_fingerprint_enabled", "account_id", accountID, "target", targetHost, "proxy", proxyInfo, "profile", profile.Name)

	if err := s.validateRequestHost(req); err != nil {
		return nil, err
	}

	entry, err := s.acquireClientWithTLS(proxyURL, accountID, accountConcurrency, profile, upstreamProfile)
	if err != nil {
		slog.Debug("tls_fingerprint_acquire_client_failed", "account_id", accountID, "error", err)
		return nil, err
	}

	resp, err := entry.client.Do(req)
	if err != nil {
		atomic.AddInt64(&entry.inFlight, -1)
		atomic.StoreInt64(&entry.lastUsed, time.Now().UnixNano())
		slog.Debug("tls_fingerprint_request_failed", "account_id", accountID, "error", err)
		return nil, err
	}

	decompressResponseBody(resp)

	resp.Body = wrapTrackedBody(resp.Body, func() {
		atomic.AddInt64(&entry.inFlight, -1)
		atomic.StoreInt64(&entry.lastUsed, time.Now().UnixNano())
	})

	return resp, nil
}

// acquireClientWithTLS
func (s *httpUpstreamService) acquireClientWithTLS(proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile, upstreamProfile service.HTTPUpstreamProfile) (*upstreamClientEntry, error) {
	return s.getClientEntryWithTLS(proxyURL, accountID, accountConcurrency, profile, upstreamProfile, true, true)
}

// getClientEntryWithTLS
// TLS
func (s *httpUpstreamService) getClientEntryWithTLS(proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile, upstreamProfile service.HTTPUpstreamProfile, markInFlight bool, enforceLimit bool) (*upstreamClientEntry, error) {
	isolation := s.getIsolationMode()
	proxyKey, parsedProxy, err := normalizeProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}
	settings := s.resolvePoolSettings(isolation, accountConcurrency)
	settings = s.applyProfilePoolSettings(settings, upstreamProfile)
	// TLS "tls:"
	cacheKey := "tls:" + buildCacheKey(isolation, proxyKey, accountID, upstreamProtocolModeDefault)
	poolKey := buildPoolKey(settings, upstreamProtocolModeDefault) + ":tls"

	now := time.Now()
	nowUnix := now.UnixNano()

	s.mu.RLock()
	if entry, ok := s.clients[cacheKey]; ok && s.shouldReuseEntry(entry, isolation, proxyKey, poolKey) {
		atomic.StoreInt64(&entry.lastUsed, nowUnix)
		if markInFlight {
			atomic.AddInt64(&entry.inFlight, 1)
		}
		s.mu.RUnlock()
		slog.Debug("tls_fingerprint_reusing_client", "account_id", accountID, "cache_key", cacheKey)
		return entry, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	if entry, ok := s.clients[cacheKey]; ok {
		if s.shouldReuseEntry(entry, isolation, proxyKey, poolKey) {
			atomic.StoreInt64(&entry.lastUsed, nowUnix)
			if markInFlight {
				atomic.AddInt64(&entry.inFlight, 1)
			}
			s.mu.Unlock()
			slog.Debug("tls_fingerprint_reusing_client", "account_id", accountID, "cache_key", cacheKey)
			return entry, nil
		}
		slog.Debug("tls_fingerprint_evicting_stale_client",
			"account_id", accountID,
			"cache_key", cacheKey,
			"proxy_changed", entry.proxyKey != proxyKey,
			"pool_changed", entry.poolKey != poolKey)
		s.removeClientLocked(cacheKey, entry)
	}

	if enforceLimit && s.maxUpstreamClients() > 0 {
		s.evictIdleLocked(now)
		if len(s.clients) >= s.maxUpstreamClients() {
			if !s.evictOldestIdleLocked() {
				s.mu.Unlock()
				return nil, errUpstreamClientLimitReached
			}
		}
	}

	//
	slog.Debug("tls_fingerprint_creating_new_client", "account_id", accountID, "cache_key", cacheKey, "proxy", proxyKey)
	transport, err := buildUpstreamTransportWithTLSFingerprint(settings, parsedProxy, profile)
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("build TLS fingerprint transport: %w", err)
	}

	client := &http.Client{Transport: transport}
	if s.shouldValidateResolvedIP() {
		client.CheckRedirect = s.redirectChecker
	}

	entry := &upstreamClientEntry{
		client:   client,
		proxyKey: proxyKey,
		poolKey:  poolKey,
	}
	atomic.StoreInt64(&entry.lastUsed, nowUnix)
	if markInFlight {
		atomic.StoreInt64(&entry.inFlight, 1)
	}
	s.clients[cacheKey] = entry

	s.evictIdleLocked(now)
	s.evictOverLimitLocked()
	s.mu.Unlock()
	return entry, nil
}

func (s *httpUpstreamService) shouldValidateResolvedIP() bool {
	if s.cfg == nil {
		return false
	}
	if !s.cfg.Security.URLAllowlist.Enabled {
		return false
	}
	return !s.cfg.Security.URLAllowlist.AllowPrivateHosts
}

func (s *httpUpstreamService) validateRequestHost(req *http.Request) error {
	if !s.shouldValidateResolvedIP() {
		return nil
	}
	if req == nil || req.URL == nil {
		return errors.New("request url is nil")
	}
	host := strings.TrimSpace(req.URL.Hostname())
	if host == "" {
		return errors.New("request host is empty")
	}
	if err := urlvalidator.ValidateResolvedIP(host); err != nil {
		return err
	}
	return nil
}

func (s *httpUpstreamService) redirectChecker(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return s.validateRequestHost(req)
}

// acquireClient
func (s *httpUpstreamService) acquireClient(proxyURL string, accountID int64, accountConcurrency int) (*upstreamClientEntry, error) {
	return s.acquireClientWithProfile(proxyURL, accountID, accountConcurrency, service.HTTPUpstreamProfileDefault)
}

// acquireClientWithProfile
func (s *httpUpstreamService) acquireClientWithProfile(proxyURL string, accountID int64, accountConcurrency int, profile service.HTTPUpstreamProfile) (*upstreamClientEntry, error) {
	return s.getClientEntry(proxyURL, accountID, accountConcurrency, profile, true, true)
}

// getOrCreateClient
//
//   - proxyURL:
//   - accountID:
//   - accountConcurrency:
//
//   - *upstreamClientEntry:
//
//   - proxy:
//   - account:
//   - account_proxy: +
func (s *httpUpstreamService) getOrCreateClient(proxyURL string, accountID int64, accountConcurrency int) (*upstreamClientEntry, error) {
	return s.getClientEntry(proxyURL, accountID, accountConcurrency, service.HTTPUpstreamProfileDefault, false, false)
}

// getClientEntry
// markInFlight=true
// enforceLimit=true
func (s *httpUpstreamService) getClientEntry(proxyURL string, accountID int64, accountConcurrency int, profile service.HTTPUpstreamProfile, markInFlight bool, enforceLimit bool) (*upstreamClientEntry, error) {
	isolation := s.getIsolationMode()
	//
	proxyKey, parsedProxy, err := normalizeProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}
	//
	protocolMode := s.resolveProtocolMode(profile, proxyKey, parsedProxy)
	settings := s.resolvePoolSettings(isolation, accountConcurrency)
	settings = s.applyProfilePoolSettings(settings, profile)
	cacheKey := buildCacheKey(isolation, proxyKey, accountID, protocolMode)
	poolKey := buildPoolKey(settings, protocolMode)

	now := time.Now()
	nowUnix := now.UnixNano()

	s.mu.RLock()
	if entry, ok := s.clients[cacheKey]; ok && s.shouldReuseEntry(entry, isolation, proxyKey, poolKey) {
		atomic.StoreInt64(&entry.lastUsed, nowUnix)
		if markInFlight {
			atomic.AddInt64(&entry.inFlight, 1)
		}
		s.mu.RUnlock()
		return entry, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	if entry, ok := s.clients[cacheKey]; ok {
		if s.shouldReuseEntry(entry, isolation, proxyKey, poolKey) {
			atomic.StoreInt64(&entry.lastUsed, nowUnix)
			if markInFlight {
				atomic.AddInt64(&entry.inFlight, 1)
			}
			s.mu.Unlock()
			return entry, nil
		}
		s.removeClientLocked(cacheKey, entry)
	}

	if enforceLimit && s.maxUpstreamClients() > 0 {
		s.evictIdleLocked(now)
		if len(s.clients) >= s.maxUpstreamClients() {
			if !s.evictOldestIdleLocked() {
				s.mu.Unlock()
				return nil, errUpstreamClientLimitReached
			}
		}
	}

	transport, err := buildUpstreamTransport(settings, parsedProxy, protocolMode)
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("build transport: %w", err)
	}
	client := &http.Client{Transport: transport}
	if s.shouldValidateResolvedIP() {
		client.CheckRedirect = s.redirectChecker
	}
	entry := &upstreamClientEntry{
		client:       client,
		proxyKey:     proxyKey,
		poolKey:      poolKey,
		protocolMode: protocolMode,
	}
	atomic.StoreInt64(&entry.lastUsed, nowUnix)
	if markInFlight {
		atomic.StoreInt64(&entry.inFlight, 1)
	}
	s.clients[cacheKey] = entry

	s.evictIdleLocked(now)
	s.evictOverLimitLocked()
	s.mu.Unlock()
	return entry, nil
}

// shouldReuseEntry
func (s *httpUpstreamService) shouldReuseEntry(entry *upstreamClientEntry, isolation, proxyKey, poolKey string) bool {
	if entry == nil {
		return false
	}
	if isolation == config.ConnectionPoolIsolationAccount && entry.proxyKey != proxyKey {
		return false
	}
	if entry.poolKey != poolKey {
		return false
	}
	return true
}

// removeClientLocked
//
//   - key:
//   - entry:
func (s *httpUpstreamService) removeClientLocked(key string, entry *upstreamClientEntry) {
	delete(s.clients, key)
	if entry != nil && entry.client != nil {
		entry.client.CloseIdleConnections()
	}
}

// evictIdleLocked
//
//
//   - now:
func (s *httpUpstreamService) evictIdleLocked(now time.Time) {
	ttl := s.clientIdleTTL()
	if ttl <= 0 {
		return
	}
	cutoff := now.Add(-ttl).UnixNano()
	for key, entry := range s.clients {
		if atomic.LoadInt64(&entry.inFlight) != 0 {
			continue
		}
		if atomic.LoadInt64(&entry.lastUsed) <= cutoff {
			s.removeClientLocked(key, entry)
		}
	}
}

// evictOldestIdleLocked
func (s *httpUpstreamService) evictOldestIdleLocked() bool {
	var (
		oldestKey   string
		oldestEntry *upstreamClientEntry
		oldestTime  int64
	)
	for key, entry := range s.clients {
		if atomic.LoadInt64(&entry.inFlight) != 0 {
			continue
		}
		lastUsed := atomic.LoadInt64(&entry.lastUsed)
		if oldestEntry == nil || lastUsed < oldestTime {
			oldestKey = key
			oldestEntry = entry
			oldestTime = lastUsed
		}
	}
	if oldestEntry == nil {
		return false
	}
	s.removeClientLocked(oldestKey, oldestEntry)
	return true
}

// evictOverLimitLocked
//
func (s *httpUpstreamService) evictOverLimitLocked() bool {
	maxClients := s.maxUpstreamClients()
	if maxClients <= 0 {
		return false
	}
	evicted := false
	for len(s.clients) > maxClients {
		if !s.evictOldestIdleLocked() {
			return evicted
		}
		evicted = true
	}
	return evicted
}

// getIsolationMode
//
//
//   - string:
func (s *httpUpstreamService) getIsolationMode() string {
	if s.cfg == nil {
		return config.ConnectionPoolIsolationAccountProxy
	}
	mode := strings.ToLower(strings.TrimSpace(s.cfg.Gateway.ConnectionPoolIsolation))
	if mode == "" {
		return config.ConnectionPoolIsolationAccountProxy
	}
	switch mode {
	case config.ConnectionPoolIsolationProxy, config.ConnectionPoolIsolationAccount, config.ConnectionPoolIsolationAccountProxy:
		return mode
	default:
		return config.ConnectionPoolIsolationAccountProxy
	}
}

// maxUpstreamClients
func (s *httpUpstreamService) maxUpstreamClients() int {
	if s.cfg == nil {
		return defaultMaxUpstreamClients
	}
	if s.cfg.Gateway.MaxUpstreamClients > 0 {
		return s.cfg.Gateway.MaxUpstreamClients
	}
	return defaultMaxUpstreamClients
}

// clientIdleTTL
func (s *httpUpstreamService) clientIdleTTL() time.Duration {
	if s.cfg == nil {
		return time.Duration(defaultClientIdleTTLSeconds) * time.Second
	}
	if s.cfg.Gateway.ClientIdleTTLSeconds > 0 {
		return time.Duration(s.cfg.Gateway.ClientIdleTTLSeconds) * time.Second
	}
	return time.Duration(defaultClientIdleTTLSeconds) * time.Second
}

// resolvePoolSettings
//
//   - isolation:
//   - accountConcurrency:
//
//   - poolSettings:
//
func (s *httpUpstreamService) resolvePoolSettings(isolation string, accountConcurrency int) poolSettings {
	settings := defaultPoolSettings(s.cfg)
	if (isolation == config.ConnectionPoolIsolationAccount || isolation == config.ConnectionPoolIsolationAccountProxy) && accountConcurrency > 0 {
		settings.maxIdleConns = accountConcurrency
		settings.maxIdleConnsPerHost = accountConcurrency
		settings.maxConnsPerHost = accountConcurrency
	}
	return settings
}

func (s *httpUpstreamService) applyProfilePoolSettings(settings poolSettings, profile service.HTTPUpstreamProfile) poolSettings {
	if profile != service.HTTPUpstreamProfileOpenAI {
		return settings
	}
	settings.responseHeaderTimeout = 0
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIResponseHeaderTimeout > 0 {
		settings.responseHeaderTimeout = time.Duration(s.cfg.Gateway.OpenAIResponseHeaderTimeout) * time.Second
	}
	return settings
}

// buildPoolKey
func buildPoolKey(settings poolSettings, protocolMode string) string {
	base := fmt.Sprintf(
		"idle:%d|idle_host:%d|max:%d|idle_timeout:%s|header_timeout:%s",
		settings.maxIdleConns,
		settings.maxIdleConnsPerHost,
		settings.maxConnsPerHost,
		settings.idleConnTimeout,
		settings.responseHeaderTimeout,
	)
	if protocolMode == "" || protocolMode == upstreamProtocolModeDefault {
		return base
	}
	return base + "|proto:" + protocolMode
}

// buildCacheKey
//
//   - isolation:
//   - proxyKey:
//   - accountID:
//
//   - string:
//
//   - proxy "proxy:{proxyKey}"
//   - account "account:{accountID}"
//   - account_proxy "account:{accountID}|proxy:{proxyKey}"
func buildCacheKey(isolation, proxyKey string, accountID int64, protocolMode string) string {
	var base string
	switch isolation {
	case config.ConnectionPoolIsolationAccount:
		base = fmt.Sprintf("account:%d", accountID)
	case config.ConnectionPoolIsolationAccountProxy:
		base = fmt.Sprintf("account:%d|proxy:%s", accountID, proxyKey)
	default:
		base = fmt.Sprintf("proxy:%s", proxyKey)
	}
	if protocolMode != "" && protocolMode != upstreamProtocolModeDefault {
		base += "|proto:" + protocolMode
	}
	return base
}

func (s *httpUpstreamService) resolveOpenAIHTTP2Settings() openAIHTTP2Settings {
	settings := openAIHTTP2Settings{
		enabled:                   false,
		allowProxyFallbackToHTTP1: true,
		fallbackErrorThreshold:    defaultOpenAIHTTP2FallbackErrorThreshold,
		fallbackWindow:            defaultOpenAIHTTP2FallbackWindow,
		fallbackTTL:               defaultOpenAIHTTP2FallbackTTL,
	}
	if s == nil || s.cfg == nil {
		return settings
	}
	cfg := s.cfg.Gateway.OpenAIHTTP2
	settings.enabled = cfg.Enabled
	settings.allowProxyFallbackToHTTP1 = cfg.AllowProxyFallbackToHTTP1
	if cfg.FallbackErrorThreshold > 0 {
		settings.fallbackErrorThreshold = cfg.FallbackErrorThreshold
	}
	if cfg.FallbackWindowSeconds > 0 {
		settings.fallbackWindow = time.Duration(cfg.FallbackWindowSeconds) * time.Second
	}
	if cfg.FallbackTTLSeconds > 0 {
		settings.fallbackTTL = time.Duration(cfg.FallbackTTLSeconds) * time.Second
	}
	return settings
}

func (s *httpUpstreamService) resolveProtocolMode(profile service.HTTPUpstreamProfile, proxyKey string, parsedProxy *url.URL) string {
	if profile != service.HTTPUpstreamProfileOpenAI {
		return upstreamProtocolModeDefault
	}
	settings := s.resolveOpenAIHTTP2Settings()
	if !settings.enabled {
		return upstreamProtocolModeOpenAIH1
	}
	if parsedProxy == nil {
		return upstreamProtocolModeOpenAIH2
	}
	scheme := strings.ToLower(parsedProxy.Scheme)
	if scheme != "http" && scheme != "https" {
		return upstreamProtocolModeOpenAIH2
	}
	if settings.allowProxyFallbackToHTTP1 && s.isOpenAIHTTP2FallbackActive(proxyKey) {
		return upstreamProtocolModeOpenAIH1Fallback
	}
	return upstreamProtocolModeOpenAIH2
}

func (s *httpUpstreamService) isOpenAIHTTP2FallbackActive(proxyKey string) bool {
	raw, ok := s.openAIHTTP2Fallbacks.Load(proxyKey)
	if !ok {
		return false
	}
	state, ok := raw.(*openAIHTTP2FallbackState)
	if !ok || state == nil {
		return false
	}
	return state.isFallbackActive(time.Now())
}

func (s *httpUpstreamService) getOrCreateOpenAIHTTP2FallbackState(proxyKey string) *openAIHTTP2FallbackState {
	state := &openAIHTTP2FallbackState{}
	actual, _ := s.openAIHTTP2Fallbacks.LoadOrStore(proxyKey, state)
	cached, ok := actual.(*openAIHTTP2FallbackState)
	if !ok || cached == nil {
		return state
	}
	return cached
}

func isHTTPProxyKey(proxyKey string) bool {
	return strings.HasPrefix(proxyKey, "http://") || strings.HasPrefix(proxyKey, "https://")
}

func isOpenAIHTTP2CompatibilityError(err error) bool {
	if err == nil {
		return false
	}
	if isUpstreamTimeoutError(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	if msg == "" {
		return false
	}
	markers := []string{
		"alpn",
		"no application protocol",
		"protocol error",
		"stream error",
		"goaway",
		"refused_stream",
		"frame too large",
	}
	for _, marker := range markers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func isUpstreamTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	if msg == "" {
		return false
	}
	timeoutMarkers := []string{
		"timeout awaiting response headers",
		"i/o timeout",
		"context deadline exceeded",
		"client.timeout exceeded while awaiting headers",
		"tls handshake timeout",
	}
	for _, marker := range timeoutMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func (s *httpUpstreamService) recordOpenAIHTTP2Failure(profile service.HTTPUpstreamProfile, protocolMode, proxyKey string, err error) {
	if profile != service.HTTPUpstreamProfileOpenAI || protocolMode != upstreamProtocolModeOpenAIH2 {
		return
	}
	settings := s.resolveOpenAIHTTP2Settings()
	if !settings.enabled || !settings.allowProxyFallbackToHTTP1 {
		return
	}
	if !isHTTPProxyKey(proxyKey) || !isOpenAIHTTP2CompatibilityError(err) {
		return
	}
	state := s.getOrCreateOpenAIHTTP2FallbackState(proxyKey)
	activated, until := state.recordFailure(time.Now(), settings.fallbackErrorThreshold, settings.fallbackWindow, settings.fallbackTTL)
	if activated {
		slog.Warn("openai_http2_proxy_fallback_activated",
			"proxy", proxyKey,
			"fallback_until", until.Format(time.RFC3339))
	}
}

func (s *httpUpstreamService) recordOpenAIHTTP2Success(profile service.HTTPUpstreamProfile, protocolMode, proxyKey string) {
	if profile != service.HTTPUpstreamProfileOpenAI || protocolMode != upstreamProtocolModeOpenAIH2 {
		return
	}
	if !isHTTPProxyKey(proxyKey) {
		return
	}
	raw, ok := s.openAIHTTP2Fallbacks.Load(proxyKey)
	if !ok {
		return
	}
	state, ok := raw.(*openAIHTTP2FallbackState)
	if !ok || state == nil {
		return
	}
	state.resetErrorWindow()
}

func (s *openAIHTTP2FallbackState) isFallbackActive(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fallbackUntil.IsZero() {
		return false
	}
	if now.Before(s.fallbackUntil) {
		return true
	}
	s.fallbackUntil = time.Time{}
	return false
}

func (s *openAIHTTP2FallbackState) resetErrorWindow() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.windowStart = time.Time{}
	s.errorCount = 0
}

func (s *openAIHTTP2FallbackState) recordFailure(now time.Time, threshold int, window, ttl time.Duration) (bool, time.Time) {
	if threshold <= 0 {
		threshold = defaultOpenAIHTTP2FallbackErrorThreshold
	}
	if window <= 0 {
		window = defaultOpenAIHTTP2FallbackWindow
	}
	if ttl <= 0 {
		ttl = defaultOpenAIHTTP2FallbackTTL
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.fallbackUntil.IsZero() && now.Before(s.fallbackUntil) {
		return false, s.fallbackUntil
	}
	if !s.fallbackUntil.IsZero() && !now.Before(s.fallbackUntil) {
		s.fallbackUntil = time.Time{}
	}

	if s.windowStart.IsZero() || now.Sub(s.windowStart) > window {
		s.windowStart = now
		s.errorCount = 0
	}
	s.errorCount++
	if s.errorCount < threshold {
		return false, time.Time{}
	}

	s.fallbackUntil = now.Add(ttl)
	s.windowStart = time.Time{}
	s.errorCount = 0
	return true, s.fallbackUntil
}

// normalizeProxyURL
//
//
//   - raw:
//
//   - string: "direct"）
//   - *url.URL:
//   - error:
func normalizeProxyURL(raw string) (string, *url.URL, error) {
	_, parsed, err := proxyurl.Parse(raw)
	if err != nil {
		return "", nil, err
	}
	if parsed == nil {
		return directProxyKey, nil, nil
	}
	//
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.ForceQuery = false
	if hostname := parsed.Hostname(); hostname != "" {
		port := parsed.Port()
		if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
			port = ""
		}
		hostname = strings.ToLower(hostname)
		if port != "" {
			parsed.Host = net.JoinHostPort(hostname, port)
		} else {
			parsed.Host = hostname
		}
	}
	return parsed.String(), parsed, nil
}

// defaultPoolSettings
//
//   - cfg:
//
//   - poolSettings:
func defaultPoolSettings(cfg *config.Config) poolSettings {
	maxIdleConns := defaultMaxIdleConns
	maxIdleConnsPerHost := defaultMaxIdleConnsPerHost
	maxConnsPerHost := defaultMaxConnsPerHost
	idleConnTimeout := defaultIdleConnTimeout
	responseHeaderTimeout := defaultResponseHeaderTimeout

	if cfg != nil {
		if cfg.Gateway.MaxIdleConns > 0 {
			maxIdleConns = cfg.Gateway.MaxIdleConns
		}
		if cfg.Gateway.MaxIdleConnsPerHost > 0 {
			maxIdleConnsPerHost = cfg.Gateway.MaxIdleConnsPerHost
		}
		if cfg.Gateway.MaxConnsPerHost >= 0 {
			maxConnsPerHost = cfg.Gateway.MaxConnsPerHost
		}
		if cfg.Gateway.IdleConnTimeoutSeconds > 0 {
			idleConnTimeout = time.Duration(cfg.Gateway.IdleConnTimeoutSeconds) * time.Second
		}
		if cfg.Gateway.ResponseHeaderTimeout >= 0 {
			responseHeaderTimeout = time.Duration(cfg.Gateway.ResponseHeaderTimeout) * time.Second
		}
	}

	return poolSettings{
		maxIdleConns:          maxIdleConns,
		maxIdleConnsPerHost:   maxIdleConnsPerHost,
		maxConnsPerHost:       maxConnsPerHost,
		idleConnTimeout:       idleConnTimeout,
		responseHeaderTimeout: responseHeaderTimeout,
	}
}

// buildUpstreamTransport
//
//   - settings:
//   - proxyURL:
//
//   - *http.Transport:
//   - error:
//
// Transport
//   - MaxIdleConns:
//   - MaxIdleConnsPerHost:
//   - MaxConnsPerHost:
//   - IdleConnTimeout:
//   - ResponseHeaderTimeout:
func buildUpstreamTransport(settings poolSettings, proxyURL *url.URL, protocolMode string) (*http.Transport, error) {
	transport := &http.Transport{
		MaxIdleConns:          settings.maxIdleConns,
		MaxIdleConnsPerHost:   settings.maxIdleConnsPerHost,
		MaxConnsPerHost:       settings.maxConnsPerHost,
		IdleConnTimeout:       settings.idleConnTimeout,
		ResponseHeaderTimeout: settings.responseHeaderTimeout,
	}
	switch protocolMode {
	case upstreamProtocolModeOpenAIH2:
		transport.ForceAttemptHTTP2 = true
	case upstreamProtocolModeOpenAIH1:
		transport.ForceAttemptHTTP2 = false
		transport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	case upstreamProtocolModeOpenAIH1Fallback:
		//
		transport.ForceAttemptHTTP2 = false
		transport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	}
	if err := proxyutil.ConfigureTransportProxy(transport, proxyURL); err != nil {
		return nil, err
	}
	return transport, nil
}

// buildUpstreamTransportWithTLSFingerprint
//
//
//   - settings:
//   - proxyURL:
//   - profile: TLS
//
//   - *http.Transport:
//   - error:
//
//   - nil/
//   - http/https: HTTP + utls
//   - socks5: SOCKS5 + utls
func buildUpstreamTransportWithTLSFingerprint(settings poolSettings, proxyURL *url.URL, profile *tlsfingerprint.Profile) (*http.Transport, error) {
	transport := &http.Transport{
		MaxIdleConns:          settings.maxIdleConns,
		MaxIdleConnsPerHost:   settings.maxIdleConnsPerHost,
		MaxConnsPerHost:       settings.maxConnsPerHost,
		IdleConnTimeout:       settings.idleConnTimeout,
		ResponseHeaderTimeout: settings.responseHeaderTimeout,
		//
		ForceAttemptHTTP2: false,
	}

	//
	if proxyURL == nil {
		//
		slog.Debug("tls_fingerprint_transport_direct")
		dialer := tlsfingerprint.NewDialer(profile, nil)
		transport.DialTLSContext = dialer.DialTLSContext
	} else {
		scheme := strings.ToLower(proxyURL.Scheme)
		switch scheme {
		case "socks5", "socks5h":
			// SOCKS5
			slog.Debug("tls_fingerprint_transport_socks5", "proxy", proxyURL.Host)
			socks5Dialer := tlsfingerprint.NewSOCKS5ProxyDialer(profile, proxyURL)
			transport.DialTLSContext = socks5Dialer.DialTLSContext
		case "http", "https":
			// HTTP/HTTPS
			slog.Debug("tls_fingerprint_transport_http_connect", "proxy", proxyURL.Host)
			httpDialer := tlsfingerprint.NewHTTPProxyDialer(profile, proxyURL)
			transport.DialTLSContext = httpDialer.DialTLSContext
		default:
			//
			slog.Debug("tls_fingerprint_transport_unknown_scheme_fallback", "scheme", scheme)
			if err := proxyutil.ConfigureTransportProxy(transport, proxyURL); err != nil {
				return nil, err
			}
		}
	}

	return transport, nil
}

// trackedBody
//
type trackedBody struct {
	io.ReadCloser // original response body
	once          sync.Once
	onClose       func() // callback function on close
}

// Close
//
func (b *trackedBody) Close() error {
	err := b.ReadCloser.Close()
	if b.onClose != nil {
		b.once.Do(b.onClose)
	}
	return err
}

// wrapTrackedBody
//
//
//   - body:
//   - onClose:
//
//   - io.ReadCloser:
func wrapTrackedBody(body io.ReadCloser, onClose func()) io.ReadCloser {
	if body == nil {
		return body
	}
	return &trackedBody{ReadCloser: body, onClose: onClose}
}

// decompressResponseBody
//
//
func decompressResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	ce := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	if ce == "" {
		return
	}

	var reader io.Reader
	switch ce {
	case "gzip":
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return // decompression failed, keep as-is
		}
		reader = gr
	case "br":
		reader = brotli.NewReader(resp.Body)
	case "deflate":
		reader = flate.NewReader(resp.Body)
	default:
		return
	}

	originalBody := resp.Body
	resp.Body = &decompressedBody{reader: reader, closer: originalBody}
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length") // length uncertain after decompression
	resp.ContentLength = -1
}

// decompressedBody
type decompressedBody struct {
	reader io.Reader
	closer io.Closer
}

func (d *decompressedBody) Read(p []byte) (int, error) {
	return d.reader.Read(p)
}

func (d *decompressedBody) Close() error {
	//
	if rc, ok := d.reader.(io.Closer); ok {
		_ = rc.Close()
	}
	return d.closer.Close()
}
