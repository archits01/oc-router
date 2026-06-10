package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/userplatformquota"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/lib/pq"
)

// UserPlatformQuotaRecord
//
type UserPlatformQuotaRecord struct {
	UserID             int64
	Platform           string
	DailyLimitUSD      *float64
	WeeklyLimitUSD     *float64
	MonthlyLimitUSD    *float64
	DailyUsageUSD      float64
	WeeklyUsageUSD     float64
	MonthlyUsageUSD    float64
	DailyWindowStart   *time.Time
	WeeklyWindowStart  *time.Time
	MonthlyWindowStart *time.Time
}

// ErrUserPlatformQuotaNotFound ""
var ErrUserPlatformQuotaNotFound = fmt.Errorf("user platform quota record not found")

// ErrUserPlatformQuotaFKViolation
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

// UserPlatformQuotaRepository
type UserPlatformQuotaRepository interface {
	// BulkInsertInitial
	BulkInsertInitial(ctx context.Context, records []UserPlatformQuotaRecord) error
	// GetByUserPlatform (nil, nil)。
	GetByUserPlatform(ctx context.Context, userID int64, platform string) (*UserPlatformQuotaRecord, error)
	// ListByUser
	ListByUser(ctx context.Context, userID int64) ([]UserPlatformQuotaRecord, error)
	// IncrementUsageWithReset
	IncrementUsageWithReset(ctx context.Context, userID int64, platform string, cost float64, now time.Time) error
	// ResetExpiredWindow
	ResetExpiredWindow(ctx context.Context, userID int64, platform string, window string, newStart time.Time) error
	// UpsertForUser
	UpsertForUser(ctx context.Context, userID int64, records []UserPlatformQuotaRecord) error
	// BatchSnapshotUsage ()。
	// usage/window_start (Redis ),
	// (user,platform)
	BatchSnapshotUsage(ctx context.Context, snapshots []UserPlatformQuotaSnapshot, now time.Time) error
}

type userPlatformQuotaRepository struct {
	client *dbent.Client
}

// NewUserPlatformQuotaRepository
func NewUserPlatformQuotaRepository(client *dbent.Client) UserPlatformQuotaRepository {
	return &userPlatformQuotaRepository{client: client}
}

// BulkInsertInitial
//
// FK
//
// *_limit_usd IS NULL THEN EXCLUDED.*_limit_usd ELSE existing ...
//   -
//
//   - ****
//     ——
//   -
//   -
func (r *userPlatformQuotaRepository) BulkInsertInitial(ctx context.Context, records []UserPlatformQuotaRecord) error {
	if len(records) == 0 {
		return nil
	}

	client := clientFromContext(ctx, r.client)

	var sb strings.Builder
	_, _ = sb.WriteString("INSERT INTO user_platform_quotas (user_id, platform, daily_limit_usd, weekly_limit_usd, monthly_limit_usd, daily_usage_usd, weekly_usage_usd, monthly_usage_usd, created_at, updated_at) VALUES ")
	args := make([]any, 0, len(records)*6)
	// ()
	// = time.Now()
	now := time.Now()
	for i, rec := range records {
		base := i * 6
		if i > 0 {
			_, _ = sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d,0,0,0,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+6)
		args = append(args,
			rec.UserID, rec.Platform,
			rec.DailyLimitUSD, rec.WeeklyLimitUSD, rec.MonthlyLimitUSD,
			now,
		)
	}
	//
	//
	// - →
	// -
	_, _ = sb.WriteString(` ON CONFLICT (user_id, platform) WHERE deleted_at IS NULL
		DO UPDATE SET
			daily_limit_usd   = COALESCE(user_platform_quotas.daily_limit_usd, EXCLUDED.daily_limit_usd),
			weekly_limit_usd  = COALESCE(user_platform_quotas.weekly_limit_usd, EXCLUDED.weekly_limit_usd),
			monthly_limit_usd = COALESCE(user_platform_quotas.monthly_limit_usd, EXCLUDED.monthly_limit_usd),
			updated_at        = EXCLUDED.updated_at`)

	_, err := client.ExecContext(ctx, sb.String(), args...)
	return err
}

// GetByUserPlatform (nil, nil)。
func (r *userPlatformQuotaRepository) GetByUserPlatform(ctx context.Context, userID int64, platform string) (*UserPlatformQuotaRecord, error) {
	client := clientFromContext(ctx, r.client)
	entity, err := client.UserPlatformQuota.Query().
		Where(
			userplatformquota.UserIDEQ(userID),
			userplatformquota.PlatformEQ(platform),
			userplatformquota.DeletedAtIsNil(),
		).
		Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return entQuotaToRecord(entity), nil
}

// ListByUser
func (r *userPlatformQuotaRepository) ListByUser(ctx context.Context, userID int64) ([]UserPlatformQuotaRecord, error) {
	client := clientFromContext(ctx, r.client)
	rows, err := client.UserPlatformQuota.Query().
		Where(
			userplatformquota.UserIDEQ(userID),
			userplatformquota.DeletedAtIsNil(),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UserPlatformQuotaRecord, 0, len(rows))
	for _, e := range rows {
		out = append(out, *entQuotaToRecord(e))
	}
	return out, nil
}

// IncrementUsageWithReset (user, platform) *_usage_usd。
//   - (prev_window_start vs current_window_start)
//     = =
//   - **limit **
//     ——
//     +
//
//
func (r *userPlatformQuotaRepository) IncrementUsageWithReset(ctx context.Context, userID int64, platform string, cost float64, now time.Time) error {
	return r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		existing, err := txClient.UserPlatformQuota.Query().
			Where(
				userplatformquota.UserIDEQ(userID),
				userplatformquota.PlatformEQ(platform),
				userplatformquota.DeletedAtIsNil(),
			).
			ForUpdate().
			Only(txCtx)
		if dbent.IsNotFound(err) {
			// fail-open *
			//
			// SELECT FOR UPDATE
			//
			//
			const insertSQL = `INSERT INTO user_platform_quotas
				(user_id, platform, daily_usage_usd, weekly_usage_usd, monthly_usage_usd,
				 daily_window_start, weekly_window_start, monthly_window_start, created_at, updated_at)
				VALUES ($1, $2, $3, $3, $3, $4, $5, $6, $7, $7)
				ON CONFLICT (user_id, platform) WHERE deleted_at IS NULL DO UPDATE SET
					daily_usage_usd   = user_platform_quotas.daily_usage_usd   + EXCLUDED.daily_usage_usd,
					weekly_usage_usd  = user_platform_quotas.weekly_usage_usd  + EXCLUDED.weekly_usage_usd,
					monthly_usage_usd = user_platform_quotas.monthly_usage_usd + EXCLUDED.monthly_usage_usd,
					updated_at        = EXCLUDED.updated_at`
			// $6 = now：30
			_, e := txClient.ExecContext(txCtx, insertSQL,
				userID, platform, cost,
				timezone.StartOfDay(now), timezone.StartOfWeek(now), now, now)
			return e
		}
		if err != nil {
			return err
		}

		newDaily := maybeReset(existing.DailyUsageUsd, existing.DailyWindowStart, timezone.StartOfDay(now), cost)
		newWeekly := maybeReset(existing.WeeklyUsageUsd, existing.WeeklyWindowStart, timezone.StartOfWeek(now), cost)
		// 30
		newMonthly, newMonthlyStart := monthlyMaybeReset(existing.MonthlyUsageUsd, existing.MonthlyWindowStart, cost, now)

		_, e := existing.Update().
			SetDailyUsageUsd(newDaily).
			SetWeeklyUsageUsd(newWeekly).
			SetMonthlyUsageUsd(newMonthly).
			SetDailyWindowStart(timezone.StartOfDay(now)).
			SetWeeklyWindowStart(timezone.StartOfWeek(now)).
			SetMonthlyWindowStart(newMonthlyStart). // 30 天滚动：仅过期时update起始
			Save(txCtx)
		return e
	})
}

// ResetExpiredWindow
//
// ⚠️ "check-then-reset" helper）：
//
//	"Expired" ****。
//	*_usage_usd *_window_start。
//
//
//	""
//
//	""
//
//
func (r *userPlatformQuotaRepository) ResetExpiredWindow(ctx context.Context, userID int64, platform string, window string, newStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	upd := client.UserPlatformQuota.Update().
		Where(
			userplatformquota.UserIDEQ(userID),
			userplatformquota.PlatformEQ(platform),
			userplatformquota.DeletedAtIsNil(),
		)
	switch window {
	case "daily":
		upd = upd.SetDailyUsageUsd(0).SetDailyWindowStart(newStart)
	case "weekly":
		upd = upd.SetWeeklyUsageUsd(0).SetWeeklyWindowStart(newStart)
	case "monthly":
		upd = upd.SetMonthlyUsageUsd(0).SetMonthlyWindowStart(newStart)
	default:
		return fmt.Errorf("unknown window %q", window)
	}
	n, err := upd.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrUserPlatformQuotaNotFound
	}
	return nil
}

// withTx
func (r *userPlatformQuotaRepository) withTx(ctx context.Context, fn func(txCtx context.Context, txClient *dbent.Client) error) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return fn(ctx, tx.Client())
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin user_platform_quota transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := fn(txCtx, tx.Client()); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit user_platform_quota transaction: %w", err)
	}
	return nil
}

// entQuotaToRecord
//
func entQuotaToRecord(e *dbent.UserPlatformQuota) *UserPlatformQuotaRecord {
	return &UserPlatformQuotaRecord{
		UserID:             e.UserID,
		Platform:           e.Platform,
		DailyLimitUSD:      e.DailyLimitUsd,
		WeeklyLimitUSD:     e.WeeklyLimitUsd,
		MonthlyLimitUSD:    e.MonthlyLimitUsd,
		DailyUsageUSD:      e.DailyUsageUsd,
		WeeklyUsageUSD:     e.WeeklyUsageUsd,
		MonthlyUsageUSD:    e.MonthlyUsageUsd,
		DailyWindowStart:   e.DailyWindowStart,
		WeeklyWindowStart:  e.WeeklyWindowStart,
		MonthlyWindowStart: e.MonthlyWindowStart,
	}
}

// maybeReset
// -
// - + cost（
func maybeReset(prevUsage float64, prevStart *time.Time, currStart time.Time, cost float64) float64 {
	if prevStart == nil || !prevStart.Equal(currStart) {
		return cost
	}
	return prevUsage + cost
}

// monthlyMaybeReset
// >= 30×24h（
// (newUsage, newWindowStart)。
func monthlyMaybeReset(prevUsage float64, prevStart *time.Time, cost float64, now time.Time) (float64, time.Time) {
	if prevStart == nil || now.Sub(*prevStart) >= 30*24*time.Hour {
		return cost, now
	}
	return prevUsage + cost, *prevStart
}

// UpsertForUser
//  1.
//  2. = NULL
//     UPDATE
//
// *_limit_usd + deleted_at + updated_at，*_usage_usd / *_window_start。
func (r *userPlatformQuotaRepository) UpsertForUser(ctx context.Context, userID int64, records []UserPlatformQuotaRecord) error {
	return r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		platforms := make([]string, 0, len(records))
		for _, rec := range records {
			platforms = append(platforms, rec.Platform)
		}
		now := time.Now()
		if err := softDeleteMissingPlatforms(txCtx, txClient, userID, platforms, now); err != nil {
			return err
		}
		for _, rec := range records {
			affected, err := updateLimitsRow(txCtx, txClient, userID, rec, now)
			if err != nil {
				return err
			}
			if affected == 0 {
				if err := insertLimitsRow(txCtx, txClient, userID, rec, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// softDeleteMissingPlatforms
// keepPlatforms →
// now ()，
// () ()
func softDeleteMissingPlatforms(ctx context.Context, client *dbent.Client, userID int64, keepPlatforms []string, now time.Time) error {
	var (
		query string
		args  []any
	)
	if len(keepPlatforms) == 0 {
		query = `UPDATE user_platform_quotas SET deleted_at = $2, updated_at = $2
		         WHERE user_id = $1 AND deleted_at IS NULL`
		args = []any{userID, now}
	} else {
		placeholders := make([]string, len(keepPlatforms))
		args = make([]any, 0, len(keepPlatforms)+2)
		args = append(args, userID, now)
		for i, p := range keepPlatforms {
			placeholders[i] = fmt.Sprintf("$%d", i+3)
			args = append(args, p)
		}
		query = fmt.Sprintf(`UPDATE user_platform_quotas SET deleted_at = $2, updated_at = $2
		         WHERE user_id = $1 AND deleted_at IS NULL AND platform NOT IN (%s)`,
			strings.Join(placeholders, ","))
	}
	_, err := client.ExecContext(ctx, query, args...)
	return err
}

// updateLimitsRow
//
//
// affected=0
func updateLimitsRow(ctx context.Context, client *dbent.Client, userID int64, rec UserPlatformQuotaRecord, now time.Time) (int64, error) {
	const query = `UPDATE user_platform_quotas
		SET daily_limit_usd = $1, weekly_limit_usd = $2, monthly_limit_usd = $3,
		    deleted_at = NULL, updated_at = $4
		WHERE user_id = $5 AND platform = $6 AND deleted_at IS NULL`
	res, err := client.ExecContext(ctx, query,
		rec.DailyLimitUSD, rec.WeeklyLimitUSD, rec.MonthlyLimitUSD, now,
		userID, rec.Platform)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// insertLimitsRow
//
//
// affected=0
func insertLimitsRow(ctx context.Context, client *dbent.Client, userID int64, rec UserPlatformQuotaRecord, now time.Time) error {
	const query = `INSERT INTO user_platform_quotas
		(user_id, platform, daily_limit_usd, weekly_limit_usd, monthly_limit_usd,
		 daily_usage_usd, weekly_usage_usd, monthly_usage_usd, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 0, 0, 0, $6, $6)
		ON CONFLICT (user_id, platform) WHERE deleted_at IS NULL DO NOTHING`
	res, err := client.ExecContext(ctx, query,
		userID, rec.Platform,
		rec.DailyLimitUSD, rec.WeeklyLimitUSD, rec.MonthlyLimitUSD,
		now)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		//
		_, err = updateLimitsRow(ctx, client, userID, rec, now)
		return err
	}
	return nil
}

// batchRows × 6000 ≈ 54000
const batchRows = 6000

// BatchSnapshotUsage
// $1=now ×usage, 3×window_start）。
// FK
//
// 【】——
// (flusher)≤ batchRows
// (=1000 < 6000,)。
// (ResetExpiredWindow/UpsertForUser)
// ""
func (r *userPlatformQuotaRepository) BatchSnapshotUsage(ctx context.Context, snapshots []UserPlatformQuotaSnapshot, now time.Time) error {
	if len(snapshots) == 0 {
		return nil
	}

	client := clientFromContext(ctx, r.client)

	for start := 0; start < len(snapshots); start += batchRows {
		end := start + batchRows
		if end > len(snapshots) {
			end = len(snapshots)
		}
		batch := snapshots[start:end]

		var sb strings.Builder
		_, _ = sb.WriteString(
			"INSERT INTO user_platform_quotas" +
				" (user_id, platform, daily_usage_usd, weekly_usage_usd, monthly_usage_usd," +
				" daily_window_start, weekly_window_start, monthly_window_start, created_at, updated_at)" +
				" VALUES ")

		// $1 = now（$2
		args := []any{now}
		for i, s := range batch {
			if i > 0 {
				_, _ = sb.WriteString(",")
			}
			b := len(args) // 当前 per-row 第一个参数的 0-based 索引，实际占位符 = b+1
			fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$1,$1)",
				b+1, b+2, b+3, b+4, b+5, b+6, b+7, b+8)
			args = append(args,
				s.UserID, s.Platform,
				s.DailyUsageUSD, s.WeeklyUsageUSD, s.MonthlyUsageUSD,
				s.DailyWindowStart, s.WeeklyWindowStart, s.MonthlyWindowStart,
			)
		}

		_, _ = sb.WriteString(
			" ON CONFLICT (user_id, platform) WHERE deleted_at IS NULL DO UPDATE SET" +
				"  daily_usage_usd      = EXCLUDED.daily_usage_usd," +
				"  weekly_usage_usd     = EXCLUDED.weekly_usage_usd," +
				"  monthly_usage_usd    = EXCLUDED.monthly_usage_usd," +
				"  daily_window_start   = EXCLUDED.daily_window_start," +
				"  weekly_window_start  = EXCLUDED.weekly_window_start," +
				"  monthly_window_start = EXCLUDED.monthly_window_start," +
				"  updated_at           = EXCLUDED.updated_at")

		if _, err := client.ExecContext(ctx, sb.String(), args...); err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == "23503" {
				return ErrUserPlatformQuotaFKViolation
			}
			return err
		}
	}
	return nil
}
