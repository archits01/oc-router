//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// mustCreateUserForQuota
func mustCreateUserForQuota(t *testing.T, client *dbent.Client) int64 {
	t.Helper()
	u := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("quota-test-%d@example.com", time.Now().UnixNano()),
	})
	return u.ID
}

func TestUserPlatformQuotaRepository_BulkInsertInitial_Idempotent(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateUserForQuota(t, client)

	repo := NewUserPlatformQuotaRepository(client)

	daily := 5.0
	records := []UserPlatformQuotaRecord{
		{UserID: userID, Platform: "anthropic", DailyLimitUSD: &daily},
		{UserID: userID, Platform: "openai"},
	}

	require.NoError(t, repo.BulkInsertInitial(txCtx, records), "first insert")
	//
	require.NoError(t, repo.BulkInsertInitial(txCtx, records), "second insert (idempotent)")

	list, err := repo.ListByUser(txCtx, userID)
	require.NoError(t, err, "list")
	require.Len(t, list, 2, "expected 2 records after idempotent insert")

	//
	var anthropicRec *UserPlatformQuotaRecord
	for i := range list {
		if list[i].Platform == "anthropic" {
			anthropicRec = &list[i]
		}
	}
	require.NotNil(t, anthropicRec, "anthropic record should exist")
	require.NotNil(t, anthropicRec.DailyLimitUSD, "daily limit should be set")
	require.InDelta(t, 5.0, *anthropicRec.DailyLimitUSD, 1e-9, "daily limit should be 5.0")
}

func TestUserPlatformQuotaRepository_BulkInsertInitial_Empty(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	repo := NewUserPlatformQuotaRepository(client)
	require.NoError(t, repo.BulkInsertInitial(txCtx, nil))
	require.NoError(t, repo.BulkInsertInitial(txCtx, []UserPlatformQuotaRecord{}))
}

func TestUserPlatformQuotaRepository_GetByUserPlatform(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateUserForQuota(t, client)

	repo := NewUserPlatformQuotaRepository(client)

	//
	rec, err := repo.GetByUserPlatform(txCtx, userID, "anthropic")
	require.NoError(t, err, "get before insert should not error")
	require.Nil(t, rec, "get before insert should return nil")

	daily := 10.0
	require.NoError(t, repo.BulkInsertInitial(txCtx, []UserPlatformQuotaRecord{
		{UserID: userID, Platform: "anthropic", DailyLimitUSD: &daily},
	}))

	rec, err = repo.GetByUserPlatform(txCtx, userID, "anthropic")
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, userID, rec.UserID)
	require.Equal(t, "anthropic", rec.Platform)
	require.NotNil(t, rec.DailyLimitUSD)
	require.InDelta(t, 10.0, *rec.DailyLimitUSD, 1e-9)
}

func TestUserPlatformQuotaRepository_IncrementUsageWithReset_SameWindow(t *testing.T) {
	ctx := context.Background()

	// IncrementUsageWithReset
	client := testEntClient(t)

	userID := mustCreateUserForQuota(t, client)

	repo := NewUserPlatformQuotaRepository(client)
	now := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC) // Friday

	require.NoError(t, repo.IncrementUsageWithReset(ctx, userID, "anthropic", 1.5, now))

	rec, err := repo.GetByUserPlatform(ctx, userID, "anthropic")
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.InDelta(t, 1.5, rec.DailyUsageUSD, 1e-9, "initial daily usage")
	require.InDelta(t, 1.5, rec.WeeklyUsageUSD, 1e-9, "initial weekly usage")
	require.InDelta(t, 1.5, rec.MonthlyUsageUSD, 1e-9, "initial monthly usage")

	require.NoError(t, repo.IncrementUsageWithReset(ctx, userID, "anthropic", 0.5, now))

	rec, err = repo.GetByUserPlatform(ctx, userID, "anthropic")
	require.NoError(t, err)
	require.InDelta(t, 2.0, rec.DailyUsageUSD, 1e-9, "accumulated daily usage")
	require.InDelta(t, 2.0, rec.WeeklyUsageUSD, 1e-9, "accumulated weekly usage")
	require.InDelta(t, 2.0, rec.MonthlyUsageUSD, 1e-9, "accumulated monthly usage")
}

func TestUserPlatformQuotaRepository_IncrementUsageWithReset_DailyReset(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	userID := mustCreateUserForQuota(t, client)

	repo := NewUserPlatformQuotaRepository(client)

	day1 := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC) // Friday (same week, same month)
	day2 := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC) // Saturday (same week, same month)

	require.NoError(t, repo.IncrementUsageWithReset(ctx, userID, "anthropic", 3.0, day1))
	require.NoError(t, repo.IncrementUsageWithReset(ctx, userID, "anthropic", 1.0, day2))

	rec, err := repo.GetByUserPlatform(ctx, userID, "anthropic")
	require.NoError(t, err)
	require.InDelta(t, 1.0, rec.DailyUsageUSD, 1e-9, "daily should reset to 1.0")
	require.InDelta(t, 4.0, rec.WeeklyUsageUSD, 1e-9, "weekly should accumulate to 4.0 (same week)")
	require.InDelta(t, 4.0, rec.MonthlyUsageUSD, 1e-9, "monthly should accumulate to 4.0 (same month)")
}

func TestUserPlatformQuotaRepository_IncrementUsageWithReset_WeeklyReset(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	userID := mustCreateUserForQuota(t, client)

	repo := NewUserPlatformQuotaRepository(client)

	// 5
	fri := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	nextMon := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC) // next Monday

	require.NoError(t, repo.IncrementUsageWithReset(ctx, userID, "openai", 5.0, fri))
	require.NoError(t, repo.IncrementUsageWithReset(ctx, userID, "openai", 2.0, nextMon))

	rec, err := repo.GetByUserPlatform(ctx, userID, "openai")
	require.NoError(t, err)
	require.InDelta(t, 2.0, rec.DailyUsageUSD, 1e-9, "daily resets to new cost")
	require.InDelta(t, 2.0, rec.WeeklyUsageUSD, 1e-9, "weekly resets (new week)")
	require.InDelta(t, 7.0, rec.MonthlyUsageUSD, 1e-9, "monthly accumulates (same month)")
}

func TestUserPlatformQuotaRepository_ResetExpiredWindow(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateUserForQuota(t, client)

	repo := NewUserPlatformQuotaRepository(client)

	//
	_, err := client.UserPlatformQuota.Create().
		SetUserID(userID).
		SetPlatform("gemini").
		SetDailyUsageUsd(10.0).
		SetWeeklyUsageUsd(20.0).
		SetMonthlyUsageUsd(50.0).
		SetDailyWindowStart(time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)).
		SetWeeklyWindowStart(time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)).
		SetMonthlyWindowStart(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)).
		Save(txCtx)
	require.NoError(t, err)

	newStart := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.ResetExpiredWindow(txCtx, userID, "gemini", "daily", newStart))

	rec, err := repo.GetByUserPlatform(txCtx, userID, "gemini")
	require.NoError(t, err)
	require.InDelta(t, 0.0, rec.DailyUsageUSD, 1e-9, "daily usage reset to 0")
	require.NotNil(t, rec.DailyWindowStart)
	require.True(t, rec.DailyWindowStart.Equal(newStart), "daily window start updated")
	require.InDelta(t, 20.0, rec.WeeklyUsageUSD, 1e-9, "weekly usage unchanged")
	require.InDelta(t, 50.0, rec.MonthlyUsageUSD, 1e-9, "monthly usage unchanged")
}

func TestUserPlatformQuotaRepository_ResetExpiredWindow_UnknownWindow(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	repo := NewUserPlatformQuotaRepository(client)
	err := repo.ResetExpiredWindow(ctx, 999, "anthropic", "yearly", time.Now())
	require.Error(t, err, "unknown window should return error")
}

func TestUserPlatformQuotaRepository_BulkInsertInitial_MultiRow(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateUserForQuota(t, client)
	repo := NewUserPlatformQuotaRepository(client)

	d1, d2, d3 := 5.0, 10.0, 15.0
	records := []UserPlatformQuotaRecord{
		{UserID: userID, Platform: "anthropic", DailyLimitUSD: &d1},
		{UserID: userID, Platform: "openai", DailyLimitUSD: &d2},
		{UserID: userID, Platform: "gemini", DailyLimitUSD: &d3},
	}
	require.NoError(t, repo.BulkInsertInitial(txCtx, records), "multi-row insert failed")

	list, err := repo.ListByUser(txCtx, userID)
	require.NoError(t, err)
	require.Len(t, list, 3, "expected 3 rows, got %d", len(list))

	//
	byPlatform := map[string]*UserPlatformQuotaRecord{}
	for i := range list {
		byPlatform[list[i].Platform] = &list[i]
	}
	require.NotNil(t, byPlatform["anthropic"], "anthropic record should exist")
	require.NotNil(t, byPlatform["anthropic"].DailyLimitUSD, "anthropic daily limit should be set")
	require.InDelta(t, 5.0, *byPlatform["anthropic"].DailyLimitUSD, 1e-9, "anthropic daily_limit = want 5.0")

	require.NotNil(t, byPlatform["openai"], "openai record should exist")
	require.NotNil(t, byPlatform["openai"].DailyLimitUSD, "openai daily limit should be set")
	require.InDelta(t, 10.0, *byPlatform["openai"].DailyLimitUSD, 1e-9, "openai daily_limit = want 10.0")

	require.NotNil(t, byPlatform["gemini"], "gemini record should exist")
	require.NotNil(t, byPlatform["gemini"].DailyLimitUSD, "gemini daily limit should be set")
	require.InDelta(t, 15.0, *byPlatform["gemini"].DailyLimitUSD, 1e-9, "gemini daily_limit = want 15.0")
}

func TestUserPlatformQuotaRepository_ResetExpiredWindow_NotFoundReturnsSentinel(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUserPlatformQuotaRepository(client)

	err := repo.ResetExpiredWindow(ctx, 99999, "anthropic", "daily", time.Now())
	require.True(t, errors.Is(err, ErrUserPlatformQuotaNotFound),
		"expected ErrUserPlatformQuotaNotFound, got %v", err)
}

// TestBatchSnapshotUsage_InsertOverwriteMultiKey
//  1.
//  2.
func TestBatchSnapshotUsage_InsertOverwriteMultiKey(t *testing.T) {
	ctx := context.Background()
	// BatchSnapshotUsage
	client := testEntClient(t)

	userID1 := mustCreateUserForQuota(t, client)
	userID2 := mustCreateUserForQuota(t, client)

	repo := NewUserPlatformQuotaRepository(client)

	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	dailyStart := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	weeklyStart := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC) // current Monday
	monthlyStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	// ── ──────────────────────────────────────────────────────
	firstBatch := []UserPlatformQuotaSnapshot{
		{
			UserID:             userID1,
			Platform:           "anthropic",
			DailyUsageUSD:      1.0,
			WeeklyUsageUSD:     3.0,
			MonthlyUsageUSD:    5.0,
			DailyWindowStart:   dailyStart,
			WeeklyWindowStart:  weeklyStart,
			MonthlyWindowStart: monthlyStart,
		},
		{
			UserID:             userID2,
			Platform:           "openai",
			DailyUsageUSD:      2.0,
			WeeklyUsageUSD:     4.0,
			MonthlyUsageUSD:    6.0,
			DailyWindowStart:   dailyStart,
			WeeklyWindowStart:  weeklyStart,
			MonthlyWindowStart: monthlyStart,
		},
	}
	require.NoError(t, repo.BatchSnapshotUsage(ctx, firstBatch, now), "first batch upsert")

	//
	rec1, err := repo.GetByUserPlatform(ctx, userID1, "anthropic")
	require.NoError(t, err)
	require.NotNil(t, rec1, "user1/anthropic should exist after first batch")
	require.InDelta(t, 1.0, rec1.DailyUsageUSD, 1e-9, "user1 daily after first batch")
	require.InDelta(t, 3.0, rec1.WeeklyUsageUSD, 1e-9, "user1 weekly after first batch")
	require.InDelta(t, 5.0, rec1.MonthlyUsageUSD, 1e-9, "user1 monthly after first batch")

	rec2, err := repo.GetByUserPlatform(ctx, userID2, "openai")
	require.NoError(t, err)
	require.NotNil(t, rec2, "user2/openai should exist after first batch")
	require.InDelta(t, 2.0, rec2.DailyUsageUSD, 1e-9, "user2 daily after first batch")

	// ── ──────────────────
	now2 := now.Add(5 * time.Minute)
	secondBatch := []UserPlatformQuotaSnapshot{
		{
			UserID:             userID1,
			Platform:           "anthropic",
			DailyUsageUSD:      9.9,  // new value, not 1.0+9.9=10.9
			WeeklyUsageUSD:     19.9, // new value, not 3.0+19.9=22.9
			MonthlyUsageUSD:    29.9, // new value
			DailyWindowStart:   dailyStart,
			WeeklyWindowStart:  weeklyStart,
			MonthlyWindowStart: monthlyStart,
		},
		{
			UserID:             userID2,
			Platform:           "openai",
			DailyUsageUSD:      8.8,
			WeeklyUsageUSD:     18.8,
			MonthlyUsageUSD:    28.8,
			DailyWindowStart:   dailyStart,
			WeeklyWindowStart:  weeklyStart,
			MonthlyWindowStart: monthlyStart,
		},
	}
	require.NoError(t, repo.BatchSnapshotUsage(ctx, secondBatch, now2), "second batch upsert")

	//
	rec1After, err := repo.GetByUserPlatform(ctx, userID1, "anthropic")
	require.NoError(t, err)
	require.NotNil(t, rec1After)
	require.InDelta(t, 9.9, rec1After.DailyUsageUSD, 1e-9, "user1 daily must be overwritten to 9.9 (not accumulated)")
	require.InDelta(t, 19.9, rec1After.WeeklyUsageUSD, 1e-9, "user1 weekly must be overwritten to 19.9")
	require.InDelta(t, 29.9, rec1After.MonthlyUsageUSD, 1e-9, "user1 monthly must be overwritten to 29.9")

	rec2After, err := repo.GetByUserPlatform(ctx, userID2, "openai")
	require.NoError(t, err)
	require.NotNil(t, rec2After)
	require.InDelta(t, 8.8, rec2After.DailyUsageUSD, 1e-9, "user2 daily must be overwritten to 8.8 (not accumulated)")
	require.InDelta(t, 18.8, rec2After.WeeklyUsageUSD, 1e-9, "user2 weekly must be overwritten to 18.8")
	require.InDelta(t, 28.8, rec2After.MonthlyUsageUSD, 1e-9, "user2 monthly must be overwritten to 28.8")
}
