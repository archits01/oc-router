package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	// antigravityRefreshWindow Antigravity token
	// Google OAuth token
	antigravityRefreshWindow = 15 * time.Minute
)

// AntigravityTokenRefresher
type AntigravityTokenRefresher struct {
	antigravityOAuthService *AntigravityOAuthService
}

func NewAntigravityTokenRefresher(antigravityOAuthService *AntigravityOAuthService) *AntigravityTokenRefresher {
	return &AntigravityTokenRefresher{
		antigravityOAuthService: antigravityOAuthService,
	}
}

// CacheKey
func (r *AntigravityTokenRefresher) CacheKey(account *Account) string {
	return AntigravityTokenCacheKey(account)
}

// CanRefresh
func (r *AntigravityTokenRefresher) CanRefresh(account *Account) bool {
	return account.Platform == PlatformAntigravity && account.Type == AccountTypeOAuth
}

// NeedsRefresh
// Antigravity
func (r *AntigravityTokenRefresher) NeedsRefresh(account *Account, _ time.Duration) bool {
	if !r.CanRefresh(account) {
		return false
	}
	expiresAt := account.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		return false
	}
	timeUntilExpiry := time.Until(*expiresAt)
	needsRefresh := timeUntilExpiry < antigravityRefreshWindow
	if needsRefresh {
		fmt.Printf("[AntigravityTokenRefresher] Account %d needs refresh: expires_at=%s, time_until_expiry=%v, window=%v\n",
			account.ID, expiresAt.Format("2006-01-02 15:04:05"), timeUntilExpiry, antigravityRefreshWindow)
	}
	return needsRefresh
}

// Refresh
func (r *AntigravityTokenRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	tokenInfo, err := r.antigravityOAuthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return nil, err
	}

	newCredentials := r.antigravityOAuthService.BuildAccountCredentials(tokenInfo)
	//
	newCredentials = MergeCredentials(account.Credentials, newCredentials)

	//
	//
	if newProjectID, _ := newCredentials["project_id"].(string); newProjectID == "" {
		if oldProjectID := strings.TrimSpace(account.GetCredential("project_id")); oldProjectID != "" {
			newCredentials["project_id"] = oldProjectID
		}
	}

	//
	// LoadCodeAssist
	// Token
	if tokenInfo.ProjectIDMissing {
		if tokenInfo.ProjectID != "" {
			//
			log.Printf("[AntigravityTokenRefresher] Account %d: LoadCodeAssist 临时failed，保留旧 project_id", account.ID)
		} else {
			//
			log.Printf("[AntigravityTokenRefresher] Account %d: LoadCodeAssist failed，project_id 缺失，但 token 已update，将在下次刷新时retry", account.ID)
		}
	}

	return newCredentials, nil
}
