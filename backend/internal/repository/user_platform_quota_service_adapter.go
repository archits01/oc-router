package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// userPlatformQuotaServiceAdapter
// *service.UserPlatformQuotaRecord）。
type userPlatformQuotaServiceAdapter struct {
	inner *userPlatformQuotaRepository
}

// NewUserPlatformQuotaServiceAdapter
//
func NewUserPlatformQuotaServiceAdapter(repo UserPlatformQuotaRepository) service.UserPlatformQuotaRepository {
	impl, ok := repo.(*userPlatformQuotaRepository)
	if !ok {
		//
		return &genericUserPlatformQuotaAdapter{inner: repo}
	}
	return &userPlatformQuotaServiceAdapter{inner: impl}
}

func (a *userPlatformQuotaServiceAdapter) GetByUserPlatform(ctx context.Context, userID int64, platform string) (*service.UserPlatformQuotaRecord, error) {
	rec, err := a.inner.GetByUserPlatform(ctx, userID, platform)
	if err != nil || rec == nil {
		return nil, err
	}
	return toServiceRecord(rec), nil
}

// IncrementUsageWithReset (user, platform)
func (a *userPlatformQuotaServiceAdapter) IncrementUsageWithReset(ctx context.Context, userID int64, platform string, cost float64, now time.Time) error {
	return a.inner.IncrementUsageWithReset(ctx, userID, platform, cost, now)
}

// ListByUser
func (a *userPlatformQuotaServiceAdapter) ListByUser(ctx context.Context, userID int64) ([]service.UserPlatformQuotaRecord, error) {
	rows, err := a.inner.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]service.UserPlatformQuotaRecord, len(rows))
	for i, r := range rows {
		out[i] = service.UserPlatformQuotaRecord{
			UserID:             r.UserID,
			Platform:           r.Platform,
			DailyLimitUSD:      r.DailyLimitUSD,
			WeeklyLimitUSD:     r.WeeklyLimitUSD,
			MonthlyLimitUSD:    r.MonthlyLimitUSD,
			DailyUsageUSD:      r.DailyUsageUSD,
			WeeklyUsageUSD:     r.WeeklyUsageUSD,
			MonthlyUsageUSD:    r.MonthlyUsageUSD,
			DailyWindowStart:   r.DailyWindowStart,
			WeeklyWindowStart:  r.WeeklyWindowStart,
			MonthlyWindowStart: r.MonthlyWindowStart,
		}
	}
	return out, nil
}

// BulkInsertInitial
func (a *userPlatformQuotaServiceAdapter) BulkInsertInitial(ctx context.Context, records []service.UserPlatformQuotaRecord) error {
	repoRecords := make([]UserPlatformQuotaRecord, len(records))
	for i, r := range records {
		repoRecords[i] = UserPlatformQuotaRecord{
			UserID:          r.UserID,
			Platform:        r.Platform,
			DailyLimitUSD:   r.DailyLimitUSD,
			WeeklyLimitUSD:  r.WeeklyLimitUSD,
			MonthlyLimitUSD: r.MonthlyLimitUSD,
		}
	}
	return a.inner.BulkInsertInitial(ctx, repoRecords)
}

// UpsertForUser
func (a *userPlatformQuotaServiceAdapter) UpsertForUser(ctx context.Context, userID int64, records []service.UserPlatformQuotaRecord) error {
	repoRecords := toRepoRecords(records)
	return a.inner.UpsertForUser(ctx, userID, repoRecords)
}

// ResetExpiredWindow
func (a *userPlatformQuotaServiceAdapter) ResetExpiredWindow(ctx context.Context, userID int64, platform string, window string, newStart time.Time) error {
	err := a.inner.ResetExpiredWindow(ctx, userID, platform, window, newStart)
	if errors.Is(err, ErrUserPlatformQuotaNotFound) {
		return fmt.Errorf("%w: %w", service.ErrUserPlatformQuotaNotFound, err)
	}
	return err
}

// BatchSnapshotUsage []service.UserPlatformQuotaSnapshot → []UserPlatformQuotaSnapshot，
//
func (a *userPlatformQuotaServiceAdapter) BatchSnapshotUsage(ctx context.Context, snapshots []service.UserPlatformQuotaSnapshot, now time.Time) error {
	repoSnaps := make([]UserPlatformQuotaSnapshot, len(snapshots))
	for i, s := range snapshots {
		repoSnaps[i] = UserPlatformQuotaSnapshot{
			UserID:             s.UserID,
			Platform:           s.Platform,
			DailyUsageUSD:      s.DailyUsageUSD,
			WeeklyUsageUSD:     s.WeeklyUsageUSD,
			MonthlyUsageUSD:    s.MonthlyUsageUSD,
			DailyWindowStart:   s.DailyWindowStart,
			WeeklyWindowStart:  s.WeeklyWindowStart,
			MonthlyWindowStart: s.MonthlyWindowStart,
		}
	}
	err := a.inner.BatchSnapshotUsage(ctx, repoSnaps, now)
	if errors.Is(err, ErrUserPlatformQuotaFKViolation) {
		return fmt.Errorf("%w: %v", service.ErrUserPlatformQuotaFKViolation, err)
	}
	return err
}

// genericUserPlatformQuotaAdapter
type genericUserPlatformQuotaAdapter struct {
	inner UserPlatformQuotaRepository
}

func (a *genericUserPlatformQuotaAdapter) GetByUserPlatform(ctx context.Context, userID int64, platform string) (*service.UserPlatformQuotaRecord, error) {
	rec, err := a.inner.GetByUserPlatform(ctx, userID, platform)
	if err != nil || rec == nil {
		return nil, err
	}
	return toServiceRecord(rec), nil
}

// IncrementUsageWithReset
func (a *genericUserPlatformQuotaAdapter) IncrementUsageWithReset(ctx context.Context, userID int64, platform string, cost float64, now time.Time) error {
	return a.inner.IncrementUsageWithReset(ctx, userID, platform, cost, now)
}

// ListByUser
func (a *genericUserPlatformQuotaAdapter) ListByUser(ctx context.Context, userID int64) ([]service.UserPlatformQuotaRecord, error) {
	rows, err := a.inner.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]service.UserPlatformQuotaRecord, len(rows))
	for i, r := range rows {
		out[i] = service.UserPlatformQuotaRecord{
			UserID:             r.UserID,
			Platform:           r.Platform,
			DailyLimitUSD:      r.DailyLimitUSD,
			WeeklyLimitUSD:     r.WeeklyLimitUSD,
			MonthlyLimitUSD:    r.MonthlyLimitUSD,
			DailyUsageUSD:      r.DailyUsageUSD,
			WeeklyUsageUSD:     r.WeeklyUsageUSD,
			MonthlyUsageUSD:    r.MonthlyUsageUSD,
			DailyWindowStart:   r.DailyWindowStart,
			WeeklyWindowStart:  r.WeeklyWindowStart,
			MonthlyWindowStart: r.MonthlyWindowStart,
		}
	}
	return out, nil
}

// BulkInsertInitial
func (a *genericUserPlatformQuotaAdapter) BulkInsertInitial(ctx context.Context, records []service.UserPlatformQuotaRecord) error {
	repoRecords := make([]UserPlatformQuotaRecord, len(records))
	for i, r := range records {
		repoRecords[i] = UserPlatformQuotaRecord{
			UserID:          r.UserID,
			Platform:        r.Platform,
			DailyLimitUSD:   r.DailyLimitUSD,
			WeeklyLimitUSD:  r.WeeklyLimitUSD,
			MonthlyLimitUSD: r.MonthlyLimitUSD,
		}
	}
	return a.inner.BulkInsertInitial(ctx, repoRecords)
}

// UpsertForUser
func (a *genericUserPlatformQuotaAdapter) UpsertForUser(ctx context.Context, userID int64, records []service.UserPlatformQuotaRecord) error {
	repoRecords := toRepoRecords(records)
	return a.inner.UpsertForUser(ctx, userID, repoRecords)
}

// ResetExpiredWindow
func (a *genericUserPlatformQuotaAdapter) ResetExpiredWindow(ctx context.Context, userID int64, platform string, window string, newStart time.Time) error {
	err := a.inner.ResetExpiredWindow(ctx, userID, platform, window, newStart)
	if errors.Is(err, ErrUserPlatformQuotaNotFound) {
		return fmt.Errorf("%w: %w", service.ErrUserPlatformQuotaNotFound, err)
	}
	return err
}

// BatchSnapshotUsage []service.UserPlatformQuotaSnapshot → []UserPlatformQuotaSnapshot（
//
func (a *genericUserPlatformQuotaAdapter) BatchSnapshotUsage(ctx context.Context, snapshots []service.UserPlatformQuotaSnapshot, now time.Time) error {
	repoSnaps := make([]UserPlatformQuotaSnapshot, len(snapshots))
	for i, s := range snapshots {
		repoSnaps[i] = UserPlatformQuotaSnapshot{
			UserID:             s.UserID,
			Platform:           s.Platform,
			DailyUsageUSD:      s.DailyUsageUSD,
			WeeklyUsageUSD:     s.WeeklyUsageUSD,
			MonthlyUsageUSD:    s.MonthlyUsageUSD,
			DailyWindowStart:   s.DailyWindowStart,
			WeeklyWindowStart:  s.WeeklyWindowStart,
			MonthlyWindowStart: s.MonthlyWindowStart,
		}
	}
	err := a.inner.BatchSnapshotUsage(ctx, repoSnaps, now)
	if errors.Is(err, ErrUserPlatformQuotaFKViolation) {
		return fmt.Errorf("%w: %v", service.ErrUserPlatformQuotaFKViolation, err)
	}
	return err
}

// toServiceRecord
func toServiceRecord(rec *UserPlatformQuotaRecord) *service.UserPlatformQuotaRecord {
	return &service.UserPlatformQuotaRecord{
		UserID:             rec.UserID,
		Platform:           rec.Platform,
		DailyLimitUSD:      rec.DailyLimitUSD,
		WeeklyLimitUSD:     rec.WeeklyLimitUSD,
		MonthlyLimitUSD:    rec.MonthlyLimitUSD,
		DailyUsageUSD:      rec.DailyUsageUSD,
		WeeklyUsageUSD:     rec.WeeklyUsageUSD,
		MonthlyUsageUSD:    rec.MonthlyUsageUSD,
		DailyWindowStart:   rec.DailyWindowStart,
		WeeklyWindowStart:  rec.WeeklyWindowStart,
		MonthlyWindowStart: rec.MonthlyWindowStart,
	}
}

// toRepoRecords
func toRepoRecords(records []service.UserPlatformQuotaRecord) []UserPlatformQuotaRecord {
	out := make([]UserPlatformQuotaRecord, len(records))
	for i, r := range records {
		out[i] = UserPlatformQuotaRecord{
			UserID:             r.UserID,
			Platform:           r.Platform,
			DailyLimitUSD:      r.DailyLimitUSD,
			WeeklyLimitUSD:     r.WeeklyLimitUSD,
			MonthlyLimitUSD:    r.MonthlyLimitUSD,
			DailyUsageUSD:      r.DailyUsageUSD,
			WeeklyUsageUSD:     r.WeeklyUsageUSD,
			MonthlyUsageUSD:    r.MonthlyUsageUSD,
			DailyWindowStart:   r.DailyWindowStart,
			WeeklyWindowStart:  r.WeeklyWindowStart,
			MonthlyWindowStart: r.MonthlyWindowStart,
		}
	}
	return out
}
