// Package httpclient
//
//
// 1. proxy_probe_service.go:
// 2. pricing_service.go:
// 3. turnstile_service.go:
// 4. github_release_service.go:
// 5. claude_usage_service.go:
//
// 1.
// 2.
// 3.
// 4.
package httpclient

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

// Transport
const (
	defaultMaxIdleConns        = 100              // max idle connections
	defaultMaxIdleConnsPerHost = 10               // max idle connections per host
	defaultIdleConnTimeout     = 90 * time.Second // idle connection timeout (should be less than upstream LB timeout)
	defaultDialTimeout         = 5 * time.Second  // TCP connection timeout (including proxy handshake), fast fail when proxy is unreachable
	defaultTLSHandshakeTimeout = 5 * time.Second  // TLS handshake timeout
	validatedHostTTL           = 30 * time.Second // DNS Rebinding validation cache TTL
)

// Options
type Options struct {
	ProxyURL              string        // proxy URL (supports http/https/socks5/socks5h)
	Timeout               time.Duration // total request timeout
	ResponseHeaderTimeout time.Duration // response header wait timeout
	InsecureSkipVerify    bool          // skip TLS certificate verification (disabled, must not be set to true)
	ValidateResolvedIP    bool          // validate resolved IP (prevent DNS Rebinding)
	AllowPrivateHosts     bool          // allow private address resolution (used with ValidateResolvedIP)

	MaxIdleConns        int // max total idle connections (default 100)
	MaxIdleConnsPerHost int // max idle connections per host (default 10)
	MaxConnsPerHost     int // max connections per host (default 0, unlimited)
}

// sharedClients
var sharedClients sync.Map

var validateResolvedIP = urlvalidator.ValidateResolvedIP

// GetClient
//
func GetClient(opts Options) (*http.Client, error) {
	key := buildClientKey(opts)
	if cached, ok := sharedClients.Load(key); ok {
		if client, ok := cached.(*http.Client); ok {
			return client, nil
		}
	}

	client, err := buildClient(opts)
	if err != nil {
		return nil, err
	}

	actual, _ := sharedClients.LoadOrStore(key, client)
	if c, ok := actual.(*http.Client); ok {
		return c, nil
	}
	return client, nil
}

func buildClient(opts Options) (*http.Client, error) {
	transport, err := buildTransport(opts)
	if err != nil {
		return nil, err
	}

	var rt http.RoundTripper = transport
	if opts.ValidateResolvedIP && !opts.AllowPrivateHosts {
		rt = newValidatedTransport(transport)
	}
	return &http.Client{
		Transport: rt,
		Timeout:   opts.Timeout,
	}, nil
}

func buildTransport(opts Options) (*http.Transport, error) {
	maxIdleConns := opts.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = defaultMaxIdleConns
	}
	maxIdleConnsPerHost := opts.MaxIdleConnsPerHost
	if maxIdleConnsPerHost <= 0 {
		maxIdleConnsPerHost = defaultMaxIdleConnsPerHost
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: defaultDialTimeout,
		}).DialContext,
		TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		MaxConnsPerHost:       opts.MaxConnsPerHost, // 0 means unlimited
		IdleConnTimeout:       defaultIdleConnTimeout,
		ResponseHeaderTimeout: opts.ResponseHeaderTimeout,
	}

	if opts.InsecureSkipVerify {
		return nil, fmt.Errorf("insecure_skip_verify is not allowed; install a trusted certificate instead")
	}

	_, parsed, err := proxyurl.Parse(opts.ProxyURL)
	if err != nil {
		return nil, err
	}
	if parsed == nil {
		return transport, nil
	}

	if err := proxyutil.ConfigureTransportProxy(transport, parsed); err != nil {
		return nil, err
	}

	return transport, nil
}

func buildClientKey(opts Options) string {
	return fmt.Sprintf("%s|%s|%s|%t|%t|%t|%d|%d|%d",
		strings.TrimSpace(opts.ProxyURL),
		opts.Timeout.String(),
		opts.ResponseHeaderTimeout.String(),
		opts.InsecureSkipVerify,
		opts.ValidateResolvedIP,
		opts.AllowPrivateHosts,
		opts.MaxIdleConns,
		opts.MaxIdleConnsPerHost,
		opts.MaxConnsPerHost,
	)
}

type validatedTransport struct {
	base           http.RoundTripper
	validatedHosts sync.Map // map[string]time.Time, value is expiration time
	now            func() time.Time
}

func newValidatedTransport(base http.RoundTripper) *validatedTransport {
	return &validatedTransport{
		base: base,
		now:  time.Now,
	}
}

func (t *validatedTransport) isValidatedHost(host string, now time.Time) bool {
	if t == nil {
		return false
	}
	raw, ok := t.validatedHosts.Load(host)
	if !ok {
		return false
	}
	expireAt, ok := raw.(time.Time)
	if !ok {
		t.validatedHosts.Delete(host)
		return false
	}
	if now.Before(expireAt) {
		return true
	}
	t.validatedHosts.Delete(host)
	return false
}

func (t *validatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req != nil && req.URL != nil {
		host := strings.ToLower(strings.TrimSpace(req.URL.Hostname()))
		if host != "" {
			now := time.Now()
			if t != nil && t.now != nil {
				now = t.now()
			}
			if !t.isValidatedHost(host, now) {
				if err := validateResolvedIP(host); err != nil {
					return nil, err
				}
				t.validatedHosts.Store(host, now.Add(validatedHostTTL))
			}
		}
	}
	if t == nil || t.base == nil {
		return nil, fmt.Errorf("validated transport base is nil")
	}
	return t.base.RoundTrip(req)
}
