package openai

import "testing"

// ="Claude Code"。
const (
	testClaudeCodeOriginator = "Claude Code"
	testClaudeCodeUserAgent  = "Claude Code/0.5.0 (Macos 15.5; arm64) iTerm2.app (Claude Code; 1.0.4)"
)

func TestIsAllowedClientMatch(t *testing.T) {
	entry := AllowedClientEntry{Originator: "Claude Code", UAContains: []string{"Claude Code/"}}

	tests := []struct {
		name       string
		ua         string
		originator string
		want       bool
	}{
		{name: "real signature match", ua: testClaudeCodeUserAgent, originator: testClaudeCodeOriginator, want: true},
		{name: "case insensitive", ua: "claude code/0.5.0 (macos)", originator: "claude code", want: true},
		{name: "originator whitespace trimmed", ua: testClaudeCodeUserAgent, originator: "  Claude Code  ", want: true},
		{name: "originator non-exact (with suffix) no match", ua: testClaudeCodeUserAgent, originator: "Claude Code Extra", want: false},
		{name: "originator empty no match", ua: testClaudeCodeUserAgent, originator: "", want: false},
		{name: "originator is official codex no match", ua: testClaudeCodeUserAgent, originator: "codex_cli_rs", want: false},
		{name: "UA missing Claude Code/ marker no match", ua: "curl/8.0", originator: testClaudeCodeOriginator, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAllowedClientMatch(tt.ua, tt.originator, entry); got != tt.want {
				t.Fatalf("IsAllowedClientMatch(%q, %q) = %v, want %v", tt.ua, tt.originator, got, tt.want)
			}
		})
	}
}

func TestIsAllowedClientMatch_EmptyOriginatorEntryNeverMatches(t *testing.T) {
	// registry
	entry := AllowedClientEntry{Originator: "", UAContains: []string{"Claude Code/"}}
	if IsAllowedClientMatch(testClaudeCodeUserAgent, "", entry) {
		t.Fatal("entry with empty Originator should not match any request")
	}
}

func TestIsAllowedClientMatch_EmptyUAContainsNeverMatches(t *testing.T) {
	//
	entry := AllowedClientEntry{Originator: "Claude Code", UAContains: nil}
	if IsAllowedClientMatch(testClaudeCodeUserAgent, testClaudeCodeOriginator, entry) {
		t.Fatal("preset without declared UA feature should not match, preventing degradation to single-factor originator matching")
	}
}

func TestIsAllowedClientMatch_WhitespaceUAMarkerNeverMatches(t *testing.T) {
	//
	//
	entry := AllowedClientEntry{Originator: "Claude Code", UAContains: []string{"   "}}
	if IsAllowedClientMatch(testClaudeCodeUserAgent, testClaudeCodeOriginator, entry) {
		t.Fatal("UAContains with all-whitespace marker should not match, preventing degradation to single-factor originator matching")
	}
}

func TestIsAllowedClientMatch_MixedEmptyUAMarkerNeverMatches(t *testing.T) {
	//
	entry := AllowedClientEntry{Originator: "Claude Code", UAContains: []string{"", "Claude Code/"}}
	if IsAllowedClientMatch(testClaudeCodeUserAgent, testClaudeCodeOriginator, entry) {
		t.Fatal("UAContains with mixed whitespace marker should not match")
	}
}

func TestMatchAllowedClients(t *testing.T) {
	tests := []struct {
		name       string
		ua         string
		originator string
		clientIDs  []string
		want       bool
	}{
		{name: "claude_code preset matches real signature", ua: testClaudeCodeUserAgent, originator: testClaudeCodeOriginator, clientIDs: []string{AllowedClientClaudeCode}, want: true},
		{name: "claude_code preset + forged originator no match", ua: testClaudeCodeUserAgent, originator: "my_client", clientIDs: []string{AllowedClientClaudeCode}, want: false},
		{name: "empty list not allowed", ua: testClaudeCodeUserAgent, originator: testClaudeCodeOriginator, clientIDs: nil, want: false},
		{name: "unknown preset ID not allowed", ua: testClaudeCodeUserAgent, originator: testClaudeCodeOriginator, clientIDs: []string{"unknown_client"}, want: false},
		{name: "ID case/whitespace tolerance", ua: testClaudeCodeUserAgent, originator: testClaudeCodeOriginator, clientIDs: []string{"  Claude_Code "}, want: true},
		{name: "any preset match allows through", ua: testClaudeCodeUserAgent, originator: testClaudeCodeOriginator, clientIDs: []string{"unknown_client", AllowedClientClaudeCode}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchAllowedClients(tt.ua, tt.originator, tt.clientIDs); got != tt.want {
				t.Fatalf("MatchAllowedClients(%q, %q, %v) = %v, want %v", tt.ua, tt.originator, tt.clientIDs, got, tt.want)
			}
		})
	}
}
