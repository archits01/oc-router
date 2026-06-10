package openai

import "testing"

func TestIsCodexCLIRequest(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want bool
	}{
		{name: "codex_cli_rs prefix", ua: "codex_cli_rs/0.1.0", want: true},
		{name: "codex_vscode prefix", ua: "codex_vscode/1.2.3", want: true},
		{name: "mixed case", ua: "Codex_CLI_Rs/0.1.0", want: true},
		{name: "composite UA contains codex", ua: "Mozilla/5.0 codex_cli_rs/0.1.0", want: true},
		{name: "whitespace wrapped", ua: "  codex_vscode/1.2.3  ", want: true},
		{name: "non-codex", ua: "curl/8.0.1", want: false},
		{name: "empty string", ua: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCodexCLIRequest(tt.ua)
			if got != tt.want {
				t.Fatalf("IsCodexCLIRequest(%q) = %v, want %v", tt.ua, got, tt.want)
			}
		})
	}
}

func TestIsCodexOfficialClientRequest(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want bool
	}{
		{name: "codex_cli_rs prefix", ua: "codex_cli_rs/0.98.0", want: true},
		{name: "codex_vscode prefix", ua: "codex_vscode/1.0.0", want: true},
		{name: "codex_app prefix", ua: "codex_app/0.1.0", want: true},
		{name: "codex_chatgpt_desktop prefix", ua: "codex_chatgpt_desktop/1.0.0", want: true},
		{name: "codex_atlas prefix", ua: "codex_atlas/1.0.0", want: true},
		{name: "codex_exec prefix", ua: "codex_exec/0.1.0", want: true},
		{name: "codex_sdk_ts prefix", ua: "codex_sdk_ts/0.1.0", want: true},
		{name: "Codex desktop UA", ua: "Codex Desktop/1.2.3", want: true},
		{name: "composite UA contains codex_app", ua: "Mozilla/5.0 codex_app/0.1.0", want: true},
		{name: "mixed case", ua: "Codex_VSCode/1.2.3", want: true},
		{name: "non-codex", ua: "curl/8.0.1", want: false},
		{name: "empty string", ua: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCodexOfficialClientRequest(tt.ua)
			if got != tt.want {
				t.Fatalf("IsCodexOfficialClientRequest(%q) = %v, want %v", tt.ua, got, tt.want)
			}
		})
	}
}

func TestIsCodexOfficialClientOriginator(t *testing.T) {
	tests := []struct {
		name       string
		originator string
		want       bool
	}{
		{name: "codex_cli_rs", originator: "codex_cli_rs", want: true},
		{name: "codex_vscode", originator: "codex_vscode", want: true},
		{name: "codex_app", originator: "codex_app", want: true},
		{name: "codex_chatgpt_desktop", originator: "codex_chatgpt_desktop", want: true},
		{name: "codex_atlas", originator: "codex_atlas", want: true},
		{name: "codex_exec", originator: "codex_exec", want: true},
		{name: "codex_sdk_ts", originator: "codex_sdk_ts", want: true},
		{name: "Codex prefix", originator: "Codex Desktop", want: true},
		{name: "whitespace wrapped", originator: "  codex_vscode  ", want: true},
		{name: "non-codex", originator: "my_client", want: false},
		{name: "empty string", originator: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCodexOfficialClientOriginator(tt.originator)
			if got != tt.want {
				t.Fatalf("IsCodexOfficialClientOriginator(%q) = %v, want %v", tt.originator, got, tt.want)
			}
		})
	}
}

func TestIsCodexOfficialClientByHeaders(t *testing.T) {
	tests := []struct {
		name       string
		ua         string
		originator string
		want       bool
	}{
		{name: "only originator matches desktop", originator: "Codex Desktop", want: true},
		{name: "only originator matches vscode", originator: "codex_vscode", want: true},
		{name: "only UA matches desktop", ua: "Codex Desktop/1.2.3", want: true},
		{name: "neither UA nor originator matches", ua: "curl/8.0.1", originator: "my_client", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCodexOfficialClientByHeaders(tt.ua, tt.originator)
			if got != tt.want {
				t.Fatalf("IsCodexOfficialClientByHeaders(%q, %q) = %v, want %v", tt.ua, tt.originator, got, tt.want)
			}
		})
	}
}
