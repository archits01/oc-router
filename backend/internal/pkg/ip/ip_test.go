//go:build unit

package ip

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		//
		{"10.x private address", "10.0.0.1", true},
		{"10.x private address range end", "10.255.255.255", true},
		{"172.16.x private address", "172.16.0.1", true},
		{"172.31.x private address", "172.31.255.255", true},
		{"192.168.x private address", "192.168.1.1", true},
		{"127.0.0.1 local loopback", "127.0.0.1", true},
		{"127.x loopback range", "127.255.255.255", true},

		//
		{"8.8.8.8 public DNS", "8.8.8.8", false},
		{"1.1.1.1 public", "1.1.1.1", false},
		{"172.15.255.255 non-private", "172.15.255.255", false},
		{"172.32.0.0 non-private", "172.32.0.0", false},
		{"11.0.0.1 public", "11.0.0.1", false},

		// IPv6
		{"::1 IPv6 loopback", "::1", true},
		{"fc00:: IPv6 private", "fc00::1", true},
		{"fd00:: IPv6 private", "fd00::1", true},
		{"2001:db8::1 IPv6 public", "2001:db8::1", false},

		{"empty string", "", false},
		{"invalid string", "not-an-ip", false},
		{"incomplete IP", "192.168", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isPrivateIP(tc.ip)
			require.Equal(t, tc.expected, got, "isPrivateIP(%q)", tc.ip)
		})
	}
}

func TestGetTrustedClientIPUsesGinClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))

	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetTrustedClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "1.2.3.4")
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "9.9.9.9", w.Body.String())
}

func TestCheckIPRestrictionWithCompiledRules(t *testing.T) {
	whitelist := CompileIPRules([]string{"10.0.0.0/8", "192.168.1.2"})
	blacklist := CompileIPRules([]string{"10.1.1.1"})

	allowed, reason := CheckIPRestrictionWithCompiledRules("10.2.3.4", whitelist, blacklist)
	require.True(t, allowed)
	require.Equal(t, "", reason)

	allowed, reason = CheckIPRestrictionWithCompiledRules("10.1.1.1", whitelist, blacklist)
	require.False(t, allowed)
	require.Equal(t, "access denied", reason)
}

func TestCheckIPRestrictionWithCompiledRules_InvalidWhitelistStillDenies(t *testing.T) {
	invalidWhitelist := CompileIPRules([]string{"not-a-valid-pattern"})
	allowed, reason := CheckIPRestrictionWithCompiledRules("8.8.8.8", invalidWhitelist, nil)
	require.False(t, allowed)
	require.Equal(t, "access denied", reason)
}
