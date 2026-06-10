package service

import (
	"context"
	"errors"
	"time"
)

// ErrUserPlatformQuotaNotFound service
// adapter
// handler
var ErrUserPlatformQuotaNotFound = errors.New("user platform quota not found")

// ErrUserPlatformQuotaFKViolation service
// user_id
var ErrUserPlatformQuotaFKViolation = errors.New("user platform quota snapshot FK violation")

// UserPlatformQuotaSnapshot
//
type UserPlatformQuotaSnapshot struct {
	UserID             int64
	Platform           string
	DailyUsageUSD      float64
	WeeklyUsageUSD     float64
	MonthlyUsageUSD    float64
	DailyWindowStart   time.Time
	WeeklyWindowStart  time.Time
	MonthlyWindowStart time.Time
}

// UserPlatformQuotaRecord service
type UserPlatformQuotaRecord struct {
	UserID          int64
	Platform        string
	DailyLimitUSD   *float64
	WeeklyLimitUSD  *float64
	MonthlyLimitUSD *float64
	DailyUsageUSD   float64
	WeeklyUsageUSD  float64
	MonthlyUsageUSD float64
	//
	DailyWindowStart   *time.Time
	WeeklyWindowStart  *time.Time
	MonthlyWindowStart *time.Time
}

// UserPlatformQuotaRepository × platform quota
// repository
type UserPlatformQuotaRepository interface {
	// GetByUserPlatform (nil, nil)。
	GetByUserPlatform(ctx context.Context, userID int64, platform string) (*UserPlatformQuotaRecord, error)
	// BulkInsertInitial
	BulkInsertInitial(ctx context.Context, records []UserPlatformQuotaRecord) error
	// IncrementUsageWithReset
	IncrementUsageWithReset(ctx context.Context, userID int64, platform string, cost float64, now time.Time) error
	// ListByUser
	ListByUser(ctx context.Context, userID int64) ([]UserPlatformQuotaRecord, error)
	// UpsertForUser
	//   1.
	//   2.
	//      *_limit_usd + deleted_at + updated_at，*_usage_usd / *_window_start。
	// records
	UpsertForUser(ctx context.Context, userID int64, records []UserPlatformQuotaRecord) error
	// ResetExpiredWindow "daily"|"weekly"|"monthly"）
	//
	ResetExpiredWindow(ctx context.Context, userID int64, platform string, window string, newStart time.Time) error
	// BatchSnapshotUsage
	BatchSnapshotUsage(ctx context.Context, snapshots []UserPlatformQuotaSnapshot, now time.Time) error
}
