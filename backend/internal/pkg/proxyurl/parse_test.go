package proxyurl

import (
	"strings"
	"testing"
)

func TestParse_empty stringdirect connect(t *testing.T) {
	trimmed, parsed, err := Parse("")
	if err != nil {
		t.Fatalf("empty stringshould use direct connection: %v", err)
	}
	if trimmed != "" {
		t.Errorf("trimmed should be empty: got %q", trimmed)
	}
	if parsed != nil {
		t.Errorf("parsed should be nil: got %v", parsed)
	}
}

func TestParse_whitespace stringdirect connect(t *testing.T) {
	trimmed, parsed, err := Parse("   ")
	if err != nil {
		t.Fatalf("whitespace string should use direct connection: %v", err)
	}
	if trimmed != "" {
		t.Errorf("trimmed should be empty: got %q", trimmed)
	}
	if parsed != nil {
		t.Errorf("parsed should be nil: got %v", parsed)
	}
}

func TestParse_valid HTTP proxy(t *testing.T) {
	trimmed, parsed, err := Parse("http://proxy.example.com:8080")
	if err != nil {
		t.Fatalf("valid HTTP proxy should succeed: %v", err)
	}
	if trimmed != "http://proxy.example.com:8080" {
		t.Errorf("trimmed mismatch: got %q", trimmed)
	}
	if parsed == nil {
		t.Fatal("parsed should not be nil")
	}
	if parsed.Host != "proxy.example.com:8080" {
		t.Errorf("Host mismatch: got %q", parsed.Host)
	}
}

func TestParse_valid HTTPS proxy(t *testing.T) {
	_, parsed, err := Parse("https://proxy.example.com:443")
	if err != nil {
		t.Fatalf("valid HTTPS proxy should succeed: %v", err)
	}
	if parsed.Scheme != "https" {
		t.Errorf("Scheme mismatch: got %q", parsed.Scheme)
	}
}

func TestParse_valid SOCKS5 proxy auto-upgraded to SOCKS5H(t *testing.T) {
	trimmed, parsed, err := Parse("socks5://127.0.0.1:1080")
	if err != nil {
		t.Fatalf("valid SOCKS5 proxy should succeed: %v", err)
	}
	// socks5
	if trimmed != "socks5h://127.0.0.1:1080" {
		t.Errorf("trimmed should be upgraded to socks5h: got %q", trimmed)
	}
	if parsed.Scheme != "socks5h" {
		t.Errorf("Scheme should be upgraded to socks5h: got %q", parsed.Scheme)
	}
}

func TestParse_invalid URL(t *testing.T) {
	_, _, err := Parse("://invalid")
	if err == nil {
		t.Fatal("invalid URL should return error")
	}
	if !strings.Contains(err.Error(), "invalid proxy URL") {
		t.Errorf("error message should contain 'invalid proxy URL': got %s", err.Error())
	}
}

func TestParse_missing host(t *testing.T) {
	_, _, err := Parse("http://")
	if err == nil {
		t.Fatal("missing host should return error")
	}
	if !strings.Contains(err.Error(), "missing host") {
		t.Errorf("error message should contain 'missing host': got %s", err.Error())
	}
}

func TestParse_unsupported scheme(t *testing.T) {
	_, _, err := Parse("ftp://proxy.example.com:21")
	if err == nil {
		t.Fatal("unsupported scheme should return error")
	}
	if !strings.Contains(err.Error(), "unsupported proxy scheme") {
		t.Errorf("error message should contain 'unsupported proxy scheme': got %s", err.Error())
	}
}

func TestParse_URL with password redaction(t *testing.T) {
	//
	trimmed, parsed, err := Parse("socks5://user:secret_password@proxy.local:1080")
	if err != nil {
		t.Fatalf("valid URL with password should succeed: %v", err)
	}
	if trimmed == "" || parsed == nil {
		t.Fatal("should return non-empty result")
	}
	if parsed.Scheme != "socks5h" {
		t.Errorf("Scheme should be upgraded to socks5h: got %q", parsed.Scheme)
	}
	if !strings.HasPrefix(trimmed, "socks5h://") {
		t.Errorf("trimmed should start with socks5h://: got %q", trimmed)
	}
	if parsed.User == nil {
		t.Error("should preserve UserInfo after upgrade")
	}

	//
	_, _, err = Parse("http://user:secret_password@:0/")
	if err == nil {
		t.Fatal("missing host should return error")
	}
	if strings.Contains(err.Error(), "secret_password") {
		t.Error("error message should not contain plaintext password")
	}
	if !strings.Contains(err.Error(), "missing host") {
		t.Errorf("error message should contain 'missing host': got %s", err.Error())
	}
}

func TestParse_valid URL with whitespace(t *testing.T) {
	trimmed, parsed, err := Parse("  http://proxy.example.com:8080  ")
	if err != nil {
		t.Fatalf("valid URL with whitespace should succeed: %v", err)
	}
	if trimmed != "http://proxy.example.com:8080" {
		t.Errorf("trimmed should trim whitespace: got %q", trimmed)
	}
	if parsed == nil {
		t.Fatal("parsed should not be nil")
	}
}

func TestParse_Schemecase insensitive(t *testing.T) {
	//
	trimmed, parsed, err := Parse("SOCKS5://proxy.example.com:1080")
	if err != nil {
		t.Fatalf("uppercase SOCKS5 should be accepted: %v", err)
	}
	if parsed.Scheme != "socks5h" {
		t.Errorf("uppercase SOCKS5 Scheme should be upgraded to socks5h: got %q", parsed.Scheme)
	}
	if !strings.HasPrefix(trimmed, "socks5h://") {
		t.Errorf("uppercase SOCKS5 trimmed should be upgraded to socks5h://: got %q", trimmed)
	}

	//
	_, _, err = Parse("HTTP://proxy.example.com:8080")
	if err != nil {
		t.Fatalf("uppercase HTTP should be accepted: %v", err)
	}
}

func TestParse_valid proxy with authentication(t *testing.T) {
	trimmed, parsed, err := Parse("http://user:pass@proxy.example.com:8080")
	if err != nil {
		t.Fatalf("proxy URL with authentication should succeed: %v", err)
	}
	if parsed.User == nil {
		t.Error("should preserve UserInfo")
	}
	if trimmed != "http://user:pass@proxy.example.com:8080" {
		t.Errorf("trimmed mismatch: got %q", trimmed)
	}
}

func TestParse_IPv6 address(t *testing.T) {
	trimmed, parsed, err := Parse("http://[::1]:8080")
	if err != nil {
		t.Fatalf("IPv6 proxy URL should succeed: %v", err)
	}
	if parsed.Hostname() != "::1" {
		t.Errorf("Hostname mismatch: got %q", parsed.Hostname())
	}
	if trimmed != "http://[::1]:8080" {
		t.Errorf("trimmed mismatch: got %q", trimmed)
	}
}

func TestParse_SOCKS5H remains unchanged(t *testing.T) {
	trimmed, parsed, err := Parse("socks5h://proxy.local:1080")
	if err != nil {
		t.Fatalf("valid SOCKS5H proxy should succeed: %v", err)
	}
	// socks5h
	if trimmed != "socks5h://proxy.local:1080" {
		t.Errorf("trimmed should not change: got %q", trimmed)
	}
	if parsed.Scheme != "socks5h" {
		t.Errorf("Scheme should remain socks5h: got %q", parsed.Scheme)
	}
}

func TestParse_bare address without scheme(t *testing.T) {
	//
	_, _, err := Parse("proxy.example.com:8080")
	if err == nil {
		t.Fatal("bare address without scheme should return error")
	}
}
