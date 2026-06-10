package service

import (
	"context"
	"log/slog"
	"strconv"
)

type TokenCacheInvalidator interface {
	InvalidateToken(ctx context.Context, account *Account) error
}

type CompositeTokenCacheInvalidator struct {
	cache GeminiTokenCache // 统一使用一个缓存接口，通过缓存键前缀区分平台
}

func NewCompositeTokenCacheInvalidator(cache GeminiTokenCache) *CompositeTokenCacheInvalidator {
	return &CompositeTokenCacheInvalidator{
		cache: cache,
	}
}

func (c *CompositeTokenCacheInvalidator) InvalidateToken(ctx context.Context, account *Account) error {
	if c == nil || c.cache == nil || account == nil {
		return nil
	}
	if account.Type != AccountTypeOAuth {
		return nil
	}

	var keysToDelete []string
	accountIDKey := "account:" + strconv.FormatInt(account.ID, 10)

	switch account.Platform {
	case PlatformGemini:
		// Gemini
		//
		//
		keysToDelete = append(keysToDelete, GeminiTokenCacheKey(account))
		keysToDelete = append(keysToDelete, "gemini:"+accountIDKey)
	case PlatformAntigravity:
		// Antigravity
		keysToDelete = append(keysToDelete, AntigravityTokenCacheKey(account))
		keysToDelete = append(keysToDelete, "ag:"+accountIDKey)
	case PlatformOpenAI:
		keysToDelete = append(keysToDelete, OpenAITokenCacheKey(account))
	case PlatformAnthropic:
		keysToDelete = append(keysToDelete, ClaudeTokenCacheKey(account))
	default:
		return nil
	}

	seen := make(map[string]bool)
	for _, key := range keysToDelete {
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := c.cache.DeleteAccessToken(ctx, key); err != nil {
			slog.Warn("token_cache_delete_failed", "key", key, "account_id", account.ID, "error", err)
		}
	}

	return nil
}

// CheckTokenVersion
//
//
//   - latestAccount:
//   - isStale: true
func CheckTokenVersion(ctx context.Context, account *Account, repo AccountRepository) (latestAccount *Account, isStale bool) {
	if account == nil || repo == nil {
		return nil, false
	}

	currentVersion := account.GetCredentialAsInt64("_token_version")

	latestAccount, err := repo.GetByID(ctx, account.ID)
	if err != nil || latestAccount == nil {
		//
		return nil, false
	}

	latestVersion := latestAccount.GetCredentialAsInt64("_token_version")

	//
	//
	if currentVersion == 0 && latestVersion > 0 {
		slog.Debug("token_version_stale_no_current_version",
			"account_id", account.ID,
			"latest_version", latestVersion)
		return latestAccount, true
	}

	if currentVersion == 0 && latestVersion == 0 {
		return latestAccount, false
	}

	//
	if latestVersion > currentVersion {
		slog.Debug("token_version_stale",
			"account_id", account.ID,
			"current_version", currentVersion,
			"latest_version", latestVersion)
		return latestAccount, true
	}

	return latestAccount, false
}
