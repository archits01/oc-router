//go:build unit

package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// =====================
// =====================

func TestGeminiOAuthService_GenerateAuthURL_RedirectURIStrategy(t *testing.T) {
	// NOTE: This test sets process env; it must not run in parallel.
	// The built-in Gemini CLI client secret is not embedded in this repository.
	// Tests set a dummy secret via env to simulate operator-provided configuration.
	t.Setenv(geminicli.GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	type testCase struct {
		name          string
		cfg           *config.Config
		oauthType     string
		projectID     string
		wantClientID  string
		wantRedirect  string
		wantScope     string
		wantProjectID string
		wantErrSubstr string
	}

	tests := []testCase{
		{
			name: "google_one uses built-in client when not configured and redirects to upstream",
			cfg: &config.Config{
				Gemini: config.GeminiConfig{
					OAuth: config.GeminiOAuthConfig{},
				},
			},
			oauthType:     "google_one",
			wantClientID:  geminicli.GeminiCLIOAuthClientID,
			wantRedirect:  geminicli.GeminiCLIRedirectURI,
			wantScope:     geminicli.DefaultCodeAssistScopes,
			wantProjectID: "",
		},
		{
			name: "google_one always forces built-in client even when custom client configured",
			cfg: &config.Config{
				Gemini: config.GeminiConfig{
					OAuth: config.GeminiOAuthConfig{
						ClientID:     "custom-client-id",
						ClientSecret: "custom-client-secret",
					},
				},
			},
			oauthType:     "google_one",
			wantClientID:  geminicli.GeminiCLIOAuthClientID,
			wantRedirect:  geminicli.GeminiCLIRedirectURI,
			wantScope:     geminicli.DefaultCodeAssistScopes,
			wantProjectID: "",
		},
		{
			name: "code_assist always forces built-in client even when custom client configured",
			cfg: &config.Config{
				Gemini: config.GeminiConfig{
					OAuth: config.GeminiOAuthConfig{
						ClientID:     "custom-client-id",
						ClientSecret: "custom-client-secret",
					},
				},
			},
			oauthType:     "code_assist",
			projectID:     "my-gcp-project",
			wantClientID:  geminicli.GeminiCLIOAuthClientID,
			wantRedirect:  geminicli.GeminiCLIRedirectURI,
			wantScope:     geminicli.DefaultCodeAssistScopes,
			wantProjectID: "my-gcp-project",
		},
		{
			name: "ai_studio requires custom client",
			cfg: &config.Config{
				Gemini: config.GeminiConfig{
					OAuth: config.GeminiOAuthConfig{},
				},
			},
			oauthType:     "ai_studio",
			wantErrSubstr: "AI Studio OAuth requires a custom OAuth Client",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := NewGeminiOAuthService(nil, nil, nil, nil, tt.cfg)
			got, err := svc.GenerateAuthURL(context.Background(), nil, "https://example.com/auth/callback", tt.projectID, tt.oauthType, "")
			if tt.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrSubstr)
				}
				if !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Fatalf("expected error containing %q, got: %v", tt.wantErrSubstr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("GenerateAuthURL returned error: %v", err)
			}

			parsed, err := url.Parse(got.AuthURL)
			if err != nil {
				t.Fatalf("failed to parse auth_url: %v", err)
			}
			q := parsed.Query()

			if gotState := q.Get("state"); gotState != got.State {
				t.Fatalf("state mismatch: query=%q result=%q", gotState, got.State)
			}
			if gotClientID := q.Get("client_id"); gotClientID != tt.wantClientID {
				t.Fatalf("client_id mismatch: got=%q want=%q", gotClientID, tt.wantClientID)
			}
			if gotRedirect := q.Get("redirect_uri"); gotRedirect != tt.wantRedirect {
				t.Fatalf("redirect_uri mismatch: got=%q want=%q", gotRedirect, tt.wantRedirect)
			}
			if gotScope := q.Get("scope"); gotScope != tt.wantScope {
				t.Fatalf("scope mismatch: got=%q want=%q", gotScope, tt.wantScope)
			}
			if gotProjectID := q.Get("project_id"); gotProjectID != tt.wantProjectID {
				t.Fatalf("project_id mismatch: got=%q want=%q", gotProjectID, tt.wantProjectID)
			}
		})
	}
}

// =====================
//
// =====================

func TestValidateTierID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tierID  string
		wantErr bool
	}{
		{name: "empty string合法", tierID: "", wantErr: false},
		{name: "正常 tier_id", tierID: "google_one_free", wantErr: false},
		{name: "包含斜杠", tierID: "tier/sub", wantErr: false},
		{name: "包含连字符", tierID: "gcp-standard", wantErr: false},
		{name: "纯数字", tierID: "12345", wantErr: false},
		{name: "超长字符串（65个字符）", tierID: strings.Repeat("a", 65), wantErr: true},
		{name: "刚好64个字符", tierID: strings.Repeat("b", 64), wantErr: false},
		{name: "非法字符_空格", tierID: "tier id", wantErr: true},
		{name: "非法字符_中文", tierID: "tier_中文", wantErr: true},
		{name: "非法字符_特殊符号", tierID: "tier@id", wantErr: true},
		{name: "非法字符_感叹号", tierID: "tier!id", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateTierID(tt.tierID)
			if tt.wantErr && err == nil {
				t.Fatalf("期望returnederror，但returned nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("不期望returnederror，但returned: %v", err)
			}
		})
	}
}

// =====================
//
// =====================

func TestCanonicalGeminiTierID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty string", raw: "", want: ""},
		{name: "纯空白", raw: "   ", want: ""},

		{name: "google_one_free", raw: "google_one_free", want: GeminiTierGoogleOneFree},
		{name: "google_ai_pro", raw: "google_ai_pro", want: GeminiTierGoogleAIPro},
		{name: "google_ai_ultra", raw: "google_ai_ultra", want: GeminiTierGoogleAIUltra},
		{name: "gcp_standard", raw: "gcp_standard", want: GeminiTierGCPStandard},
		{name: "gcp_enterprise", raw: "gcp_enterprise", want: GeminiTierGCPEnterprise},
		{name: "aistudio_free", raw: "aistudio_free", want: GeminiTierAIStudioFree},
		{name: "aistudio_paid", raw: "aistudio_paid", want: GeminiTierAIStudioPaid},
		{name: "google_one_unknown", raw: "google_one_unknown", want: GeminiTierGoogleOneUnknown},

		{name: "Google_One_Free 大写", raw: "Google_One_Free", want: GeminiTierGoogleOneFree},
		{name: "GCP_STANDARD 全大写", raw: "GCP_STANDARD", want: GeminiTierGCPStandard},

		// legacy
		{name: "AI_PREMIUM -> google_ai_pro", raw: "AI_PREMIUM", want: GeminiTierGoogleAIPro},
		{name: "FREE -> google_one_free", raw: "FREE", want: GeminiTierGoogleOneFree},
		{name: "GOOGLE_ONE_BASIC -> google_one_free", raw: "GOOGLE_ONE_BASIC", want: GeminiTierGoogleOneFree},
		{name: "GOOGLE_ONE_STANDARD -> google_one_free", raw: "GOOGLE_ONE_STANDARD", want: GeminiTierGoogleOneFree},
		{name: "GOOGLE_ONE_UNLIMITED -> google_ai_ultra", raw: "GOOGLE_ONE_UNLIMITED", want: GeminiTierGoogleAIUltra},
		{name: "GOOGLE_ONE_UNKNOWN -> google_one_unknown", raw: "GOOGLE_ONE_UNKNOWN", want: GeminiTierGoogleOneUnknown},

		// legacy
		{name: "STANDARD -> gcp_standard", raw: "STANDARD", want: GeminiTierGCPStandard},
		{name: "PRO -> gcp_standard", raw: "PRO", want: GeminiTierGCPStandard},
		{name: "LEGACY -> gcp_standard", raw: "LEGACY", want: GeminiTierGCPStandard},
		{name: "ENTERPRISE -> gcp_enterprise", raw: "ENTERPRISE", want: GeminiTierGCPEnterprise},
		{name: "ULTRA -> gcp_enterprise", raw: "ULTRA", want: GeminiTierGCPEnterprise},

		// kebab-case
		{name: "standard-tier -> gcp_standard", raw: "standard-tier", want: GeminiTierGCPStandard},
		{name: "pro-tier -> gcp_standard", raw: "pro-tier", want: GeminiTierGCPStandard},
		{name: "ultra-tier -> gcp_enterprise", raw: "ultra-tier", want: GeminiTierGCPEnterprise},

		{name: "unknown_value -> 空", raw: "unknown_value", want: ""},
		{name: "random-text -> 空", raw: "random-text", want: ""},

		{name: "带前后空白", raw: "  google_one_free  ", want: GeminiTierGoogleOneFree},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := canonicalGeminiTierID(tt.raw)
			if got != tt.want {
				t.Fatalf("canonicalGeminiTierID(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// =====================
//
// =====================

func TestCanonicalGeminiTierIDForOAuthType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		oauthType string
		tierID    string
		want      string
	}{
		// google_one
		{name: "google_one + google_one_free", oauthType: "google_one", tierID: "google_one_free", want: GeminiTierGoogleOneFree},
		{name: "google_one + google_ai_pro", oauthType: "google_one", tierID: "google_ai_pro", want: GeminiTierGoogleAIPro},
		{name: "google_one + google_ai_ultra", oauthType: "google_one", tierID: "google_ai_ultra", want: GeminiTierGoogleAIUltra},
		{name: "google_one + gcp_standard 被过滤", oauthType: "google_one", tierID: "gcp_standard", want: ""},
		{name: "google_one + aistudio_free 被过滤", oauthType: "google_one", tierID: "aistudio_free", want: ""},
		{name: "google_one + AI_PREMIUM 遗留映射", oauthType: "google_one", tierID: "AI_PREMIUM", want: GeminiTierGoogleAIPro},

		// code_assist
		{name: "code_assist + gcp_standard", oauthType: "code_assist", tierID: "gcp_standard", want: GeminiTierGCPStandard},
		{name: "code_assist + gcp_enterprise", oauthType: "code_assist", tierID: "gcp_enterprise", want: GeminiTierGCPEnterprise},
		{name: "code_assist + google_one_free 被过滤", oauthType: "code_assist", tierID: "google_one_free", want: ""},
		{name: "code_assist + aistudio_free 被过滤", oauthType: "code_assist", tierID: "aistudio_free", want: ""},
		{name: "code_assist + STANDARD 遗留映射", oauthType: "code_assist", tierID: "STANDARD", want: GeminiTierGCPStandard},
		{name: "code_assist + standard-tier kebab", oauthType: "code_assist", tierID: "standard-tier", want: GeminiTierGCPStandard},

		// ai_studio
		{name: "ai_studio + aistudio_free", oauthType: "ai_studio", tierID: "aistudio_free", want: GeminiTierAIStudioFree},
		{name: "ai_studio + aistudio_paid", oauthType: "ai_studio", tierID: "aistudio_paid", want: GeminiTierAIStudioPaid},
		{name: "ai_studio + gcp_standard 被过滤", oauthType: "ai_studio", tierID: "gcp_standard", want: ""},
		{name: "ai_studio + google_one_free 被过滤", oauthType: "ai_studio", tierID: "google_one_free", want: ""},

		{name: "空 tierID", oauthType: "google_one", tierID: "", want: ""},
		{name: "空 oauthType + valid tierID", oauthType: "", tierID: "gcp_standard", want: GeminiTierGCPStandard},
		{name: "未知 oauthType 接受规范化值", oauthType: "unknown_type", tierID: "gcp_standard", want: GeminiTierGCPStandard},

		// oauthType
		{name: "GOOGLE_ONE 大写", oauthType: "GOOGLE_ONE", tierID: "google_one_free", want: GeminiTierGoogleOneFree},
		{name: "oauthType 带空白", oauthType: "  code_assist  ", tierID: "gcp_standard", want: GeminiTierGCPStandard},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := canonicalGeminiTierIDForOAuthType(tt.oauthType, tt.tierID)
			if got != tt.want {
				t.Fatalf("canonicalGeminiTierIDForOAuthType(%q, %q) = %q, want %q", tt.oauthType, tt.tierID, got, tt.want)
			}
		})
	}
}

// =====================
//
// =====================

func TestExtractTierIDFromAllowedTiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		allowedTiers []geminicli.AllowedTier
		want         string
	}{
		{
			name:         "nil 列表returned LEGACY",
			allowedTiers: nil,
			want:         "LEGACY",
		},
		{
			name:         "空列表returned LEGACY",
			allowedTiers: []geminicli.AllowedTier{},
			want:         "LEGACY",
		},
		{
			name: "有 IsDefault 的 tier",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "STANDARD", IsDefault: false},
				{ID: "PRO", IsDefault: true},
				{ID: "ENTERPRISE", IsDefault: false},
			},
			want: "PRO",
		},
		{
			name: "没有 IsDefault 取第一个非空",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "STANDARD", IsDefault: false},
				{ID: "ENTERPRISE", IsDefault: false},
			},
			want: "STANDARD",
		},
		{
			name: "IsDefault 的 ID 为空，取第一个非空",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "", IsDefault: true},
				{ID: "PRO", IsDefault: false},
			},
			want: "PRO",
		},
		{
			name: "所有 ID 都为空returned LEGACY",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "", IsDefault: false},
				{ID: "   ", IsDefault: false},
			},
			want: "LEGACY",
		},
		{
			name: "ID 带空白会被 trim",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "  STANDARD  ", IsDefault: true},
			},
			want: "STANDARD",
		},
		{
			name: "单个 tier 且 IsDefault",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "ENTERPRISE", IsDefault: true},
			},
			want: "ENTERPRISE",
		},
		{
			name: "单个 tier 非 IsDefault",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "ENTERPRISE", IsDefault: false},
			},
			want: "ENTERPRISE",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractTierIDFromAllowedTiers(tt.allowedTiers)
			if got != tt.want {
				t.Fatalf("extractTierIDFromAllowedTiers() = %q, want %q", got, tt.want)
			}
		})
	}
}

// =====================
//
// =====================

func TestInferGoogleOneTier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		storageBytes int64
		want         string
	}{
		// <= 0
		{name: "0 bytes -> unknown", storageBytes: 0, want: GeminiTierGoogleOneUnknown},
		{name: "负数 -> unknown", storageBytes: -1, want: GeminiTierGoogleOneUnknown},

		// > 100TB -> ultra
		{name: "> 100TB -> ultra", storageBytes: int64(StorageTierUnlimited) + 1, want: GeminiTierGoogleAIUltra},
		{name: "200TB -> ultra", storageBytes: 200 * int64(TB), want: GeminiTierGoogleAIUltra},

		// >= 2TB -> pro (<= 100TB)
		{name: "正好 2TB -> pro", storageBytes: int64(StorageTierAIPremium), want: GeminiTierGoogleAIPro},
		{name: "5TB -> pro", storageBytes: 5 * int64(TB), want: GeminiTierGoogleAIPro},
		{name: "100TB 正好 -> pro (不是 > 100TB)", storageBytes: int64(StorageTierUnlimited), want: GeminiTierGoogleAIPro},

		// >= 15GB -> free (< 2TB)
		{name: "正好 15GB -> free", storageBytes: int64(StorageTierFree), want: GeminiTierGoogleOneFree},
		{name: "100GB -> free", storageBytes: 100 * int64(GB), want: GeminiTierGoogleOneFree},
		{name: "略低于 2TB -> free", storageBytes: int64(StorageTierAIPremium) - 1, want: GeminiTierGoogleOneFree},

		// < 15GB -> unknown
		{name: "1GB -> unknown", storageBytes: int64(GB), want: GeminiTierGoogleOneUnknown},
		{name: "略低于 15GB -> unknown", storageBytes: int64(StorageTierFree) - 1, want: GeminiTierGoogleOneUnknown},
		{name: "1 byte -> unknown", storageBytes: 1, want: GeminiTierGoogleOneUnknown},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := inferGoogleOneTier(tt.storageBytes)
			if got != tt.want {
				t.Fatalf("inferGoogleOneTier(%d) = %q, want %q", tt.storageBytes, got, tt.want)
			}
		})
	}
}

// =====================
//
// =====================

func TestIsNonRetryableGeminiOAuthError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "invalid_grant", err: fmt.Errorf("error: invalid_grant"), want: true},
		{name: "invalid_client", err: fmt.Errorf("oauth error: invalid_client"), want: true},
		{name: "unauthorized_client", err: fmt.Errorf("unauthorized_client: mismatch"), want: true},
		{name: "access_denied", err: fmt.Errorf("access_denied by user"), want: true},
		{name: "普通网络error", err: fmt.Errorf("connection timeout"), want: false},
		{name: "HTTP 500 error", err: fmt.Errorf("server error 500"), want: false},
		{name: "空errorinfo", err: fmt.Errorf(""), want: false},
		{name: "包含 invalid 但不是完整匹配", err: fmt.Errorf("invalid request"), want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isNonRetryableGeminiOAuthError(tt.err)
			if got != tt.want {
				t.Fatalf("isNonRetryableGeminiOAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// =====================
//
// =====================

func TestGeminiOAuthService_BuildAccountCredentials(t *testing.T) {
	t.Parallel()

	svc := NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{})
	defer svc.Stop()

	t.Run("完整字段", func(t *testing.T) {
		t.Parallel()
		tokenInfo := &GeminiTokenInfo{
			AccessToken:  "access-123",
			RefreshToken: "refresh-456",
			ExpiresIn:    3600,
			ExpiresAt:    1700000000,
			TokenType:    "Bearer",
			Scope:        "openid email",
			ProjectID:    "my-project",
			TierID:       "gcp_standard",
			OAuthType:    "code_assist",
			Extra: map[string]any{
				"drive_storage_limit": int64(2199023255552),
			},
		}

		creds := svc.BuildAccountCredentials(tokenInfo)

		assertCredStr(t, creds, "access_token", "access-123")
		assertCredStr(t, creds, "refresh_token", "refresh-456")
		assertCredStr(t, creds, "token_type", "Bearer")
		assertCredStr(t, creds, "scope", "openid email")
		assertCredStr(t, creds, "project_id", "my-project")
		assertCredStr(t, creds, "tier_id", "gcp_standard")
		assertCredStr(t, creds, "oauth_type", "code_assist")
		assertCredStr(t, creds, "expires_at", "1700000000")

		if _, ok := creds["drive_storage_limit"]; !ok {
			t.Fatal("extra 字段 drive_storage_limit 未包含在 creds 中")
		}
	})

	t.Run("最小字段（仅 access_token 和 expires_at）", func(t *testing.T) {
		t.Parallel()
		tokenInfo := &GeminiTokenInfo{
			AccessToken: "token-only",
			ExpiresAt:   1700000000,
		}

		creds := svc.BuildAccountCredentials(tokenInfo)

		assertCredStr(t, creds, "access_token", "token-only")
		assertCredStr(t, creds, "expires_at", "1700000000")

		for _, key := range []string{"refresh_token", "token_type", "scope", "project_id", "tier_id", "oauth_type"} {
			if _, ok := creds[key]; ok {
				t.Fatalf("不应包含空字段 %q", key)
			}
		}
	})

	t.Run("无效 tier_id 被静默跳过", func(t *testing.T) {
		t.Parallel()
		tokenInfo := &GeminiTokenInfo{
			AccessToken: "token",
			ExpiresAt:   1700000000,
			TierID:      "tier with spaces",
		}

		creds := svc.BuildAccountCredentials(tokenInfo)

		if _, ok := creds["tier_id"]; ok {
			t.Fatal("无效 tier_id 不应被存入 creds")
		}
	})

	t.Run("超长 tier_id 被静默跳过", func(t *testing.T) {
		t.Parallel()
		tokenInfo := &GeminiTokenInfo{
			AccessToken: "token",
			ExpiresAt:   1700000000,
			TierID:      strings.Repeat("x", 65),
		}

		creds := svc.BuildAccountCredentials(tokenInfo)

		if _, ok := creds["tier_id"]; ok {
			t.Fatal("超长 tier_id 不应被存入 creds")
		}
	})

	t.Run("无 extra 字段", func(t *testing.T) {
		t.Parallel()
		tokenInfo := &GeminiTokenInfo{
			AccessToken:  "token",
			ExpiresAt:    1700000000,
			RefreshToken: "rt",
		}

		creds := svc.BuildAccountCredentials(tokenInfo)

		if len(creds) != 3 { // access_token, expires_at, refresh_token
			t.Fatalf("creds 字段count mismatch: got=%d want=3, keys=%v", len(creds), credKeys(creds))
		}
	})
}

// =====================
//
// =====================

func TestGeminiOAuthService_GetOAuthConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         *config.Config
		wantEnabled bool
	}{
		{
			name: "无自定义 OAuth 客户端",
			cfg: &config.Config{
				Gemini: config.GeminiConfig{
					OAuth: config.GeminiOAuthConfig{},
				},
			},
			wantEnabled: false,
		},
		{
			name: "仅 ClientID 无 ClientSecret",
			cfg: &config.Config{
				Gemini: config.GeminiConfig{
					OAuth: config.GeminiOAuthConfig{
						ClientID: "custom-id",
					},
				},
			},
			wantEnabled: false,
		},
		{
			name: "仅 ClientSecret 无 ClientID",
			cfg: &config.Config{
				Gemini: config.GeminiConfig{
					OAuth: config.GeminiOAuthConfig{
						ClientSecret: "custom-secret",
					},
				},
			},
			wantEnabled: false,
		},
		{
			name: "使用内置 Gemini CLI ClientID（不算自定义）",
			cfg: &config.Config{
				Gemini: config.GeminiConfig{
					OAuth: config.GeminiOAuthConfig{
						ClientID:     geminicli.GeminiCLIOAuthClientID,
						ClientSecret: "some-secret",
					},
				},
			},
			wantEnabled: false,
		},
		{
			name: "自定义 OAuth 客户端（非内置 ID）",
			cfg: &config.Config{
				Gemini: config.GeminiConfig{
					OAuth: config.GeminiOAuthConfig{
						ClientID:     "my-custom-client-id",
						ClientSecret: "my-custom-client-secret",
					},
				},
			},
			wantEnabled: true,
		},
		{
			name: "带空白的自定义客户端",
			cfg: &config.Config{
				Gemini: config.GeminiConfig{
					OAuth: config.GeminiOAuthConfig{
						ClientID:     "  my-custom-client-id  ",
						ClientSecret: "  my-custom-client-secret  ",
					},
				},
			},
			wantEnabled: true,
		},
		{
			name: "纯whitespace string不算configuration",
			cfg: &config.Config{
				Gemini: config.GeminiConfig{
					OAuth: config.GeminiOAuthConfig{
						ClientID:     "   ",
						ClientSecret: "   ",
					},
				},
			},
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := NewGeminiOAuthService(nil, nil, nil, nil, tt.cfg)
			defer svc.Stop()

			result := svc.GetOAuthConfig()
			if result.AIStudioOAuthEnabled != tt.wantEnabled {
				t.Fatalf("AIStudioOAuthEnabled = %v, want %v", result.AIStudioOAuthEnabled, tt.wantEnabled)
			}
			// RequiredRedirectURIs
			if len(result.RequiredRedirectURIs) != 1 || result.RequiredRedirectURIs[0] != geminicli.AIStudioOAuthRedirectURI {
				t.Fatalf("RequiredRedirectURIs mismatch: got=%v", result.RequiredRedirectURIs)
			}
		})
	}
}

// =====================
//
// =====================

func TestGeminiOAuthService_Stop_NoPanic(t *testing.T) {
	t.Parallel()

	svc := NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{})

	//
	svc.Stop()
	//
	svc.Stop()
}

// =====================
// mock: GeminiOAuthClient
// =====================

type mockGeminiOAuthClient struct {
	exchangeCodeFunc func(ctx context.Context, oauthType, code, codeVerifier, redirectURI, proxyURL string) (*geminicli.TokenResponse, error)
	refreshTokenFunc func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error)
}

func (m *mockGeminiOAuthClient) ExchangeCode(ctx context.Context, oauthType, code, codeVerifier, redirectURI, proxyURL string) (*geminicli.TokenResponse, error) {
	if m.exchangeCodeFunc != nil {
		return m.exchangeCodeFunc(ctx, oauthType, code, codeVerifier, redirectURI, proxyURL)
	}
	panic("ExchangeCode not implemented")
}

func (m *mockGeminiOAuthClient) RefreshToken(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
	if m.refreshTokenFunc != nil {
		return m.refreshTokenFunc(ctx, oauthType, refreshToken, proxyURL)
	}
	panic("RefreshToken not implemented")
}

// =====================
// mock: GeminiCliCodeAssistClient
// =====================

type mockGeminiCodeAssistClient struct {
	loadCodeAssistFunc func(ctx context.Context, accessToken, proxyURL string, req *geminicli.LoadCodeAssistRequest) (*geminicli.LoadCodeAssistResponse, error)
	onboardUserFunc    func(ctx context.Context, accessToken, proxyURL string, req *geminicli.OnboardUserRequest) (*geminicli.OnboardUserResponse, error)
}

func (m *mockGeminiCodeAssistClient) LoadCodeAssist(ctx context.Context, accessToken, proxyURL string, req *geminicli.LoadCodeAssistRequest) (*geminicli.LoadCodeAssistResponse, error) {
	if m.loadCodeAssistFunc != nil {
		return m.loadCodeAssistFunc(ctx, accessToken, proxyURL, req)
	}
	panic("LoadCodeAssist not implemented")
}

func (m *mockGeminiCodeAssistClient) OnboardUser(ctx context.Context, accessToken, proxyURL string, req *geminicli.OnboardUserRequest) (*geminicli.OnboardUserResponse, error) {
	if m.onboardUserFunc != nil {
		return m.onboardUserFunc(ctx, accessToken, proxyURL, req)
	}
	panic("OnboardUser not implemented")
}

// =====================
// mock: ProxyRepository ()
// =====================

type mockGeminiProxyRepo struct {
	getByIDFunc func(ctx context.Context, id int64) (*Proxy, error)
}

func (m *mockGeminiProxyRepo) Create(ctx context.Context, proxy *Proxy) error { panic("not impl") }
func (m *mockGeminiProxyRepo) GetByID(ctx context.Context, id int64) (*Proxy, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, fmt.Errorf("proxy not found")
}
func (m *mockGeminiProxyRepo) ListByIDs(ctx context.Context, ids []int64) ([]Proxy, error) {
	panic("not impl")
}
func (m *mockGeminiProxyRepo) Update(ctx context.Context, proxy *Proxy) error { panic("not impl") }
func (m *mockGeminiProxyRepo) Delete(ctx context.Context, id int64) error     { panic("not impl") }
func (m *mockGeminiProxyRepo) List(ctx context.Context, params pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error) {
	panic("not impl")
}
func (m *mockGeminiProxyRepo) ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]Proxy, *pagination.PaginationResult, error) {
	panic("not impl")
}
func (m *mockGeminiProxyRepo) ListWithFiltersAndAccountCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]ProxyWithAccountCount, *pagination.PaginationResult, error) {
	panic("not impl")
}
func (m *mockGeminiProxyRepo) ListActive(ctx context.Context) ([]Proxy, error) { panic("not impl") }
func (m *mockGeminiProxyRepo) ListActiveWithAccountCount(ctx context.Context) ([]ProxyWithAccountCount, error) {
	panic("not impl")
}
func (m *mockGeminiProxyRepo) ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error) {
	panic("not impl")
}
func (m *mockGeminiProxyRepo) CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error) {
	panic("not impl")
}
func (m *mockGeminiProxyRepo) ListAccountSummariesByProxyID(ctx context.Context, proxyID int64) ([]ProxyAccountSummary, error) {
	panic("not impl")
}
func (m *mockGeminiProxyRepo) SweepExpiredProxies(ctx context.Context, now time.Time) (int64, error) {
	panic("not impl")
}
func (m *mockGeminiProxyRepo) ListAllForFallback(ctx context.Context) ([]Proxy, error) {
	panic("not impl")
}
func (m *mockGeminiProxyRepo) CountExpired(ctx context.Context) (int64, error) {
	panic("not impl")
}
func (m *mockGeminiProxyRepo) CountExpiringSoon(ctx context.Context, now time.Time) (int64, error) {
	panic("not impl")
}

// mockDriveClient implements geminicli.DriveClient for tests.
type mockDriveClient struct {
	getStorageQuotaFunc func(ctx context.Context, accessToken, proxyURL string) (*geminicli.DriveStorageInfo, error)
}

func (m *mockDriveClient) GetStorageQuota(ctx context.Context, accessToken, proxyURL string) (*geminicli.DriveStorageInfo, error) {
	if m.getStorageQuotaFunc != nil {
		return m.getStorageQuotaFunc(ctx, accessToken, proxyURL)
	}
	return nil, fmt.Errorf("drive API not available in test")
}

// =====================
//
// =====================

func TestGeminiOAuthService_RefreshToken_Success(t *testing.T) {
	t.Parallel()

	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			return &geminicli.TokenResponse{
				AccessToken:  "new-access",
				RefreshToken: "new-refresh",
				TokenType:    "Bearer",
				ExpiresIn:    3600,
				Scope:        "openid",
			}, nil
		},
	}

	svc := NewGeminiOAuthService(nil, client, nil, nil, &config.Config{})
	defer svc.Stop()

	info, err := svc.RefreshToken(context.Background(), "code_assist", "old-refresh", "")
	if err != nil {
		t.Fatalf("RefreshToken returnederror: %v", err)
	}
	if info.AccessToken != "new-access" {
		t.Fatalf("AccessToken mismatch: got=%q", info.AccessToken)
	}
	if info.RefreshToken != "new-refresh" {
		t.Fatalf("RefreshToken mismatch: got=%q", info.RefreshToken)
	}
	if info.ExpiresAt == 0 {
		t.Fatal("ExpiresAt 不应为 0")
	}
}

func TestGeminiOAuthService_RefreshToken_NonRetryableError(t *testing.T) {
	t.Parallel()

	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			return nil, fmt.Errorf("invalid_grant: token revoked")
		},
	}

	svc := NewGeminiOAuthService(nil, client, nil, nil, &config.Config{})
	defer svc.Stop()

	_, err := svc.RefreshToken(context.Background(), "code_assist", "revoked-token", "")
	if err == nil {
		t.Fatal("RefreshToken 应returnederror（不可retry的 invalid_grant）")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("error应包含 invalid_grant: got=%q", err.Error())
	}
}

func TestGeminiOAuthService_RefreshToken_RetryableError(t *testing.T) {
	t.Parallel()

	callCount := 0
	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			callCount++
			if callCount <= 2 {
				return nil, fmt.Errorf("temporary network error")
			}
			return &geminicli.TokenResponse{
				AccessToken: "recovered",
				ExpiresIn:   3600,
			}, nil
		},
	}

	svc := NewGeminiOAuthService(nil, client, nil, nil, &config.Config{})
	defer svc.Stop()

	info, err := svc.RefreshToken(context.Background(), "code_assist", "rt", "")
	if err != nil {
		t.Fatalf("RefreshToken 应在retry后success: %v", err)
	}
	if info.AccessToken != "recovered" {
		t.Fatalf("AccessToken mismatch: got=%q", info.AccessToken)
	}
	if callCount < 3 {
		t.Fatalf("应至少调用 3 次（2 次failed + 1 次success）: got=%d", callCount)
	}
}

// =====================
//
// =====================

func TestGeminiOAuthService_RefreshAccountToken_NotGeminiOAuth(t *testing.T) {
	t.Parallel()

	svc := NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{})
	defer svc.Stop()

	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
	}

	_, err := svc.RefreshAccountToken(context.Background(), account)
	if err == nil {
		t.Fatal("应returnederror（非 Gemini OAuth 账号）")
	}
	if !strings.Contains(err.Error(), "not a Gemini OAuth account") {
		t.Fatalf("errorinfomismatch: got=%q", err.Error())
	}
}

func TestGeminiOAuthService_RefreshAccountToken_NoRefreshToken(t *testing.T) {
	t.Parallel()

	svc := NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{})
	defer svc.Stop()

	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "at",
			"oauth_type":   "code_assist",
		},
	}

	_, err := svc.RefreshAccountToken(context.Background(), account)
	if err == nil {
		t.Fatal("应returnederror（无 refresh_token）")
	}
	if !strings.Contains(err.Error(), "no refresh token") {
		t.Fatalf("errorinfomismatch: got=%q", err.Error())
	}
}

func TestGeminiOAuthService_RefreshAccountToken_AIStudio(t *testing.T) {
	t.Parallel()

	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			return &geminicli.TokenResponse{
				AccessToken:  "refreshed-at",
				RefreshToken: "refreshed-rt",
				ExpiresIn:    3600,
				TokenType:    "Bearer",
			}, nil
		},
	}

	svc := NewGeminiOAuthService(&mockGeminiProxyRepo{}, client, nil, nil, &config.Config{})
	defer svc.Stop()

	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "old-at",
			"refresh_token": "old-rt",
			"oauth_type":    "ai_studio",
			"tier_id":       "aistudio_free",
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	if err != nil {
		t.Fatalf("RefreshAccountToken returnederror: %v", err)
	}
	if info.AccessToken != "refreshed-at" {
		t.Fatalf("AccessToken mismatch: got=%q", info.AccessToken)
	}
	if info.OAuthType != "ai_studio" {
		t.Fatalf("OAuthType mismatch: got=%q", info.OAuthType)
	}
}

func TestGeminiOAuthService_RefreshAccountToken_CodeAssist_WithProjectID(t *testing.T) {
	t.Parallel()

	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			return &geminicli.TokenResponse{
				AccessToken:  "refreshed",
				RefreshToken: "new-rt",
				ExpiresIn:    3600,
			}, nil
		},
	}

	svc := NewGeminiOAuthService(&mockGeminiProxyRepo{}, client, nil, nil, &config.Config{})
	defer svc.Stop()

	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "old-at",
			"refresh_token": "old-rt",
			"oauth_type":    "code_assist",
			"project_id":    "my-project",
			"tier_id":       "gcp_standard",
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	if err != nil {
		t.Fatalf("RefreshAccountToken returnederror: %v", err)
	}
	if info.ProjectID != "my-project" {
		t.Fatalf("ProjectID 应保留: got=%q", info.ProjectID)
	}
	if info.TierID != GeminiTierGCPStandard {
		t.Fatalf("TierID mismatch: got=%q want=%q", info.TierID, GeminiTierGCPStandard)
	}
	if info.OAuthType != "code_assist" {
		t.Fatalf("OAuthType mismatch: got=%q", info.OAuthType)
	}
}

func TestGeminiOAuthService_RefreshAccountToken_DefaultOAuthType(t *testing.T) {
	t.Parallel()

	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			if oauthType != "code_assist" {
				t.Errorf("默认 oauthType 应为 code_assist: got=%q", oauthType)
			}
			return &geminicli.TokenResponse{
				AccessToken: "refreshed",
				ExpiresIn:   3600,
			}, nil
		},
	}

	svc := NewGeminiOAuthService(&mockGeminiProxyRepo{}, client, nil, nil, &config.Config{})
	defer svc.Stop()

	//
	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "old-rt",
			"project_id":    "proj",
			"tier_id":       "STANDARD",
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	if err != nil {
		t.Fatalf("RefreshAccountToken returnederror: %v", err)
	}
	if info.OAuthType != "code_assist" {
		t.Fatalf("OAuthType 应默认为 code_assist: got=%q", info.OAuthType)
	}
}

func TestGeminiOAuthService_RefreshAccountToken_WithProxy(t *testing.T) {
	t.Parallel()

	proxyRepo := &mockGeminiProxyRepo{
		getByIDFunc: func(ctx context.Context, id int64) (*Proxy, error) {
			return &Proxy{
				Protocol: "http",
				Host:     "proxy.test",
				Port:     3128,
			}, nil
		},
	}

	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			if proxyURL != "http://proxy.test:3128" {
				t.Errorf("proxyURL mismatch: got=%q", proxyURL)
			}
			return &geminicli.TokenResponse{
				AccessToken: "refreshed",
				ExpiresIn:   3600,
			}, nil
		},
	}

	svc := NewGeminiOAuthService(proxyRepo, client, nil, nil, &config.Config{})
	defer svc.Stop()

	proxyID := int64(5)
	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		ProxyID:  &proxyID,
		Credentials: map[string]any{
			"refresh_token": "rt",
			"oauth_type":    "code_assist",
			"project_id":    "proj",
		},
	}

	_, err := svc.RefreshAccountToken(context.Background(), account)
	if err != nil {
		t.Fatalf("RefreshAccountToken returnederror: %v", err)
	}
}

func TestGeminiOAuthService_RefreshAccountToken_CodeAssist_NoProjectID_AutoDetect(t *testing.T) {
	t.Parallel()

	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			return &geminicli.TokenResponse{
				AccessToken: "at",
				ExpiresIn:   3600,
			}, nil
		},
	}

	codeAssist := &mockGeminiCodeAssistClient{
		loadCodeAssistFunc: func(ctx context.Context, accessToken, proxyURL string, req *geminicli.LoadCodeAssistRequest) (*geminicli.LoadCodeAssistResponse, error) {
			return &geminicli.LoadCodeAssistResponse{
				CloudAICompanionProject: "auto-project-123",
				CurrentTier:             &geminicli.TierInfo{ID: "STANDARD"},
			}, nil
		},
	}

	svc := NewGeminiOAuthService(&mockGeminiProxyRepo{}, client, codeAssist, nil, &config.Config{})
	defer svc.Stop()

	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "rt",
			"oauth_type":    "code_assist",
			//
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	if err != nil {
		t.Fatalf("RefreshAccountToken returnederror: %v", err)
	}
	if info.ProjectID != "auto-project-123" {
		t.Fatalf("ProjectID 应为自动检测值: got=%q", info.ProjectID)
	}
	if info.TierID != GeminiTierGCPStandard {
		t.Fatalf("TierID mismatch: got=%q", info.TierID)
	}
}

func TestGeminiOAuthService_RefreshAccountToken_CodeAssist_NoProjectID_FailsEmpty(t *testing.T) {
	t.Parallel()

	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			return &geminicli.TokenResponse{
				AccessToken: "at",
				ExpiresIn:   3600,
			}, nil
		},
	}

	//
	// ""> >
	// = 10
	codeAssist := &mockGeminiCodeAssistClient{
		loadCodeAssistFunc: func(ctx context.Context, accessToken, proxyURL string, req *geminicli.LoadCodeAssistRequest) (*geminicli.LoadCodeAssistResponse, error) {
			return &geminicli.LoadCodeAssistResponse{
				CurrentTier: &geminicli.TierInfo{ID: "STANDARD"},
				//
			}, nil
		},
	}

	svc := NewGeminiOAuthService(&mockGeminiProxyRepo{}, client, codeAssist, nil, &config.Config{})
	defer svc.Stop()

	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "rt",
			"oauth_type":    "code_assist",
		},
	}

	_, err := svc.RefreshAccountToken(context.Background(), account)
	if err == nil {
		t.Fatal("应returnederror（无法检测 project_id）")
	}
	if !strings.Contains(err.Error(), "project_id") {
		t.Fatalf("error message should contain project_id: got=%q", err.Error())
	}
}

func TestGeminiOAuthService_RefreshAccountToken_GoogleOne_FreshCache(t *testing.T) {
	t.Parallel()

	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			return &geminicli.TokenResponse{
				AccessToken: "at",
				ExpiresIn:   3600,
			}, nil
		},
	}

	svc := NewGeminiOAuthService(&mockGeminiProxyRepo{}, client, nil, nil, &config.Config{})
	defer svc.Stop()

	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "rt",
			"oauth_type":    "google_one",
			"project_id":    "proj",
			"tier_id":       "google_ai_pro",
		},
		Extra: map[string]any{
			"drive_tier_updated_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	if err != nil {
		t.Fatalf("RefreshAccountToken returnederror: %v", err)
	}
	//
	if info.TierID != GeminiTierGoogleAIPro {
		t.Fatalf("TierID 应使用缓存值: got=%q want=%q", info.TierID, GeminiTierGoogleAIPro)
	}
}

func TestGeminiOAuthService_RefreshAccountToken_GoogleOne_NoTierID_DefaultsFree(t *testing.T) {
	t.Parallel()

	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			return &geminicli.TokenResponse{
				AccessToken: "at",
				ExpiresIn:   3600,
			}, nil
		},
	}

	svc := NewGeminiOAuthService(&mockGeminiProxyRepo{}, client, nil, &mockDriveClient{}, &config.Config{})
	defer svc.Stop()

	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "rt",
			"oauth_type":    "google_one",
			"project_id":    "proj",
			//
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	if err != nil {
		t.Fatalf("RefreshAccountToken returnederror: %v", err)
	}
	// FetchGoogleOneTier
	// svc.FetchGoogleOneTier
	//
	if info.TierID != GeminiTierGoogleOneFree {
		t.Fatalf("TierID 应为默认 free: got=%q", info.TierID)
	}
}

func TestGeminiOAuthService_RefreshAccountToken_UnauthorizedClient_Fallback(t *testing.T) {
	t.Parallel()

	callCount := 0
	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			callCount++
			if oauthType == "code_assist" {
				return nil, fmt.Errorf("unauthorized_client: client mismatch")
			}
			// ai_studio
			return &geminicli.TokenResponse{
				AccessToken: "recovered",
				ExpiresIn:   3600,
			}, nil
		},
	}

	//
	cfg := &config.Config{
		Gemini: config.GeminiConfig{
			OAuth: config.GeminiOAuthConfig{
				ClientID:     "custom-id",
				ClientSecret: "custom-secret",
			},
		},
	}

	svc := NewGeminiOAuthService(&mockGeminiProxyRepo{}, client, nil, nil, cfg)
	defer svc.Stop()

	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "rt",
			"oauth_type":    "code_assist",
			"project_id":    "proj",
			"tier_id":       "gcp_standard",
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	if err != nil {
		t.Fatalf("RefreshAccountToken should succeed after fallback: %v", err)
	}
	if info.AccessToken != "recovered" {
		t.Fatalf("AccessToken mismatch: got=%q", info.AccessToken)
	}
}

func TestGeminiOAuthService_RefreshAccountToken_UnauthorizedClient_NoFallback(t *testing.T) {
	t.Parallel()

	client := &mockGeminiOAuthClient{
		refreshTokenFunc: func(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
			return nil, fmt.Errorf("unauthorized_client: client mismatch")
		},
	}

	//
	svc := NewGeminiOAuthService(&mockGeminiProxyRepo{}, client, nil, nil, &config.Config{})
	defer svc.Stop()

	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "rt",
			"oauth_type":    "code_assist",
			"project_id":    "proj",
		},
	}

	_, err := svc.RefreshAccountToken(context.Background(), account)
	if err == nil {
		t.Fatal("应returnederror（无 fallback）")
	}
	if !strings.Contains(err.Error(), "OAuth client mismatch") {
		t.Fatalf("error应包含 OAuth client mismatch: got=%q", err.Error())
	}
}

// =====================
//
// =====================

func TestGeminiOAuthService_ExchangeCode_SessionNotFound(t *testing.T) {
	t.Parallel()

	svc := NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{})
	defer svc.Stop()

	_, err := svc.ExchangeCode(context.Background(), &GeminiExchangeCodeInput{
		SessionID: "nonexistent",
		State:     "some-state",
		Code:      "some-code",
	})
	if err == nil {
		t.Fatal("应returnederror（session does not exist）")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("errorinfomismatch: got=%q", err.Error())
	}
}

func TestGeminiOAuthService_ExchangeCode_InvalidState(t *testing.T) {
	t.Parallel()

	svc := NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{})
	defer svc.Stop()

	//
	svc.sessionStore.Set("test-session", &geminicli.OAuthSession{
		State:        "correct-state",
		CodeVerifier: "verifier",
		OAuthType:    "ai_studio",
		CreatedAt:    time.Now(),
	})

	_, err := svc.ExchangeCode(context.Background(), &GeminiExchangeCodeInput{
		SessionID: "test-session",
		State:     "wrong-state",
		Code:      "code",
	})
	if err == nil {
		t.Fatal("应returnederror（state mismatch）")
	}
	if !strings.Contains(err.Error(), "invalid state") {
		t.Fatalf("errorinfomismatch: got=%q", err.Error())
	}
}

func TestGeminiOAuthService_ExchangeCode_EmptyState(t *testing.T) {
	t.Parallel()

	svc := NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{})
	defer svc.Stop()

	svc.sessionStore.Set("test-session", &geminicli.OAuthSession{
		State:        "correct-state",
		CodeVerifier: "verifier",
		CreatedAt:    time.Now(),
	})

	_, err := svc.ExchangeCode(context.Background(), &GeminiExchangeCodeInput{
		SessionID: "test-session",
		State:     "",
		Code:      "code",
	})
	if err == nil {
		t.Fatal("应returnederror（空 state）")
	}
}

// =====================
// =====================

func assertCredStr(t *testing.T, creds map[string]any, key, want string) {
	t.Helper()
	raw, ok := creds[key]
	if !ok {
		t.Fatalf("creds 缺少 key=%q", key)
	}
	got, ok := raw.(string)
	if !ok {
		t.Fatalf("creds[%q] 不是 string: %T", key, raw)
	}
	if got != want {
		t.Fatalf("creds[%q] = %q, want %q", key, got, want)
	}
}

func credKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
