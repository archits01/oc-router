package service

import (
	"net/http"
	"strings"
)

// headerWireCasing
// Go → X-App），
//
//
// (claude-cli/2.1.81)
var headerWireCasing = map[string]string{
	// Title case
	"accept":     "Accept",
	"user-agent": "User-Agent",

	// X-Stainless-*
	"x-stainless-retry-count":     "X-Stainless-Retry-Count",
	"x-stainless-timeout":         "X-Stainless-Timeout",
	"x-stainless-lang":            "X-Stainless-Lang",
	"x-stainless-package-version": "X-Stainless-Package-Version",
	"x-stainless-os":              "X-Stainless-OS",
	"x-stainless-arch":            "X-Stainless-Arch",
	"x-stainless-runtime":         "X-Stainless-Runtime",
	"x-stainless-runtime-version": "X-Stainless-Runtime-Version",
	"x-stainless-helper-method":   "x-stainless-helper-method",

	// Anthropic SDK
	"anthropic-dangerous-direct-browser-access": "anthropic-dangerous-direct-browser-access",
	"anthropic-version":                         "anthropic-version",
	"anthropic-beta":                            "anthropic-beta",
	"x-app":                                     "x-app",
	"content-type":                              "content-type",
	"accept-language":                           "accept-language",
	"sec-fetch-mode":                            "sec-fetch-mode",
	"accept-encoding":                           "accept-encoding",
	"authorization":                             "authorization",

	// Claude Code 2.1.87+
	"x-claude-code-session-id": "X-Claude-Code-Session-Id",
	"x-client-request-id":      "x-client-request-id",
	"content-length":           "content-length",
}

// headerWireOrder
//
var headerWireOrder = []string{
	"Accept",
	"X-Stainless-Retry-Count",
	"X-Stainless-Timeout",
	"X-Stainless-Lang",
	"X-Stainless-Package-Version",
	"X-Stainless-OS",
	"X-Stainless-Arch",
	"X-Stainless-Runtime",
	"X-Stainless-Runtime-Version",
	"anthropic-dangerous-direct-browser-access",
	"anthropic-version",
	"authorization",
	"x-app",
	"User-Agent",
	"X-Claude-Code-Session-Id",
	"content-type",
	"anthropic-beta",
	"x-client-request-id",
	"accept-language",
	"sec-fetch-mode",
	"accept-encoding",
	"content-length",
	"x-stainless-helper-method",
}

// headerWireOrderSet
var headerWireOrderSet map[string]struct{}

func init() {
	headerWireOrderSet = make(map[string]struct{}, len(headerWireOrder))
	for _, k := range headerWireOrder {
		headerWireOrderSet[strings.ToLower(k)] = struct{}{}
	}
}

// resolveWireCasing
//
func resolveWireCasing(key string) string {
	if wk, ok := headerWireCasing[strings.ToLower(key)]; ok {
		return wk
	}
	return key
}

// setHeaderRaw sets a header bypassing Go's canonical-case normalization.
// The key is stored exactly as provided, preserving original casing.
//
// It first removes any existing value under the canonical key, the wire casing key,
// and the exact raw key, preventing duplicates from any source.
func setHeaderRaw(h http.Header, key, value string) {
	h.Del(key) // remove canonical form (e.g. "Anthropic-Beta")
	if wk := resolveWireCasing(key); wk != key {
		delete(h, wk) // remove wire casing form if different
	}
	delete(h, key) // remove exact raw key if it differs from canonical
	h[key] = []string{value}
}

// addHeaderRaw appends a header value bypassing Go's canonical-case normalization.
func addHeaderRaw(h http.Header, key, value string) {
	h[key] = append(h[key], value)
}

// deleteHeaderAllForms removes a header in all common key forms (raw, wire casing,
// canonical) so subsequent setHeaderRaw will not coexist with a passthrough value
// written under a different casing.
func deleteHeaderAllForms(h http.Header, key string) {
	if h == nil || key == "" {
		return
	}
	h.Del(key) // canonical
	delete(h, key)
	if wk := resolveWireCasing(key); wk != key {
		delete(h, wk)
	}
}

// getHeaderRaw reads a header value, trying multiple key forms to handle the mismatch
// between Go canonical keys, wire casing keys, and raw keys:
//  1. exact key as provided
//  2. wire casing form (from headerWireCasing)
//  3. Go canonical form (via http.Header.Get)
func getHeaderRaw(h http.Header, key string) string {
	// 1. exact key
	if vals := h[key]; len(vals) > 0 {
		return vals[0]
	}
	// 2. wire casing (e.g. looking up "Anthropic-Dangerous-Direct-Browser-Access" finds "anthropic-dangerous-direct-browser-access")
	if wk := resolveWireCasing(key); wk != key {
		if vals := h[wk]; len(vals) > 0 {
			return vals[0]
		}
	}
	// 3. canonical fallback
	return h.Get(key)
}

// sortHeadersByWireOrder
//
func sortHeadersByWireOrder(h http.Header) []string {
	// > actual map key
	present := make(map[string]string, len(h))
	for k := range h {
		present[strings.ToLower(k)] = k
	}

	result := make([]string, 0, len(h))
	seen := make(map[string]struct{}, len(h))

	//
	for _, wk := range headerWireOrder {
		lk := strings.ToLower(wk)
		if actual, ok := present[lk]; ok {
			if _, dup := seen[lk]; !dup {
				result = append(result, actual)
				seen[lk] = struct{}{}
			}
		}
	}

	//
	for k := range h {
		lk := strings.ToLower(k)
		if _, ok := seen[lk]; !ok {
			result = append(result, k)
			seen[lk] = struct{}{}
		}
	}

	return result
}
