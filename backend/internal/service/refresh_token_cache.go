package service

import (
	"context"
	"errors"
	"time"
)

// ErrRefreshTokenNotFound is returned when a refresh token is not found in cache.
// This is used to abstract away the underlying cache implementation (e.g., redis.Nil).
var ErrRefreshTokenNotFound = errors.New("refresh token not found")

// RefreshTokenData
type RefreshTokenData struct {
	UserID       int64     `json:"user_id"`
	TokenVersion int64     `json:"token_version"` // 用于检测密码更改后的Token失效
	FamilyID     string    `json:"family_id"`     // Token家族ID，用于防重放攻击
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// RefreshTokenCache
//
//
// Key
//   - refresh_token:{token_hash}     -> RefreshTokenData (JSON)
//   - user_refresh_tokens:{user_id}  -> Set<token_hash>
//   - token_family:{family_id}       -> Set<token_hash>
type RefreshTokenCache interface {
	// StoreRefreshToken
	// tokenHash: Token
	// data: Token
	// ttl: Token
	StoreRefreshToken(ctx context.Context, tokenHash string, data *RefreshTokenData, ttl time.Duration) error

	// GetRefreshToken
	// (data, nil)
	// (nil, ErrRefreshTokenNotFound)
	// (nil, err)
	GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshTokenData, error)

	// DeleteRefreshToken
	//
	DeleteRefreshToken(ctx context.Context, tokenHash string) error

	// DeleteUserRefreshTokens
	DeleteUserRefreshTokens(ctx context.Context, userID int64) error

	// DeleteTokenFamily
	//
	DeleteTokenFamily(ctx context.Context, familyID string) error

	// AddToUserTokenSet
	//
	AddToUserTokenSet(ctx context.Context, userID int64, tokenHash string, ttl time.Duration) error

	// AddToFamilyTokenSet
	//
	AddToFamilyTokenSet(ctx context.Context, familyID string, tokenHash string, ttl time.Duration) error

	// GetUserTokenHashes
	//
	GetUserTokenHashes(ctx context.Context, userID int64) ([]string, error)

	// GetFamilyTokenHashes
	//
	GetFamilyTokenHashes(ctx context.Context, familyID string) ([]string, error)

	// IsTokenInFamily
	//
	IsTokenInFamily(ctx context.Context, familyID string, tokenHash string) (bool, error)
}
