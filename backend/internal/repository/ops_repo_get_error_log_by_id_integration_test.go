//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestGetErrorLogByID_DeletedKeyOwner
//  1.
//  2.
func TestGetErrorLogByID_DeletedKeyOwner(t *testing.T) {
	ctx := context.Background()
	_, _ = integrationDB.ExecContext(ctx, "TRUNCATE ops_error_logs RESTART IDENTITY CASCADE")

	repo := NewOpsRepository(integrationDB).(*opsRepository)

	// ── Case 1: ──────────────────────────────
	owner := mustCreateUser(t, integrationEntClient, &service.User{
		Email: "deleted-key-owner-" + time.Now().Format("150405.000000000") + "@example.com",
	})

	var insertedID int64
	err := integrationDB.QueryRowContext(ctx, `
		INSERT INTO ops_error_logs (
			error_phase, error_type, severity, status_code, created_at,
			attempted_key_prefix, deleted_key_owner_user_id, deleted_key_name
		) VALUES (
			'auth', 'INVALID_API_KEY', 'error', 401, NOW(),
			'sk-test-abc', $1, 'my-deleted-key'
		) RETURNING id`,
		owner.ID,
	).Scan(&insertedID)
	require.NoError(t, err)
	require.Positive(t, insertedID)

	detail, err := repo.GetErrorLogByID(ctx, insertedID)
	require.NoError(t, err)
	require.NotNil(t, detail)

	require.Equal(t, "sk-test-abc", detail.AttemptedKeyPrefix)
	require.NotNil(t, detail.DeletedKeyOwnerUserID)
	require.Equal(t, owner.ID, *detail.DeletedKeyOwnerUserID)
	require.Equal(t, owner.Email, detail.DeletedKeyOwnerEmail)
	require.Equal(t, "my-deleted-key", detail.DeletedKeyName)

	// ── Case 2: ──────────────────────────────────
	var plainID int64
	err = integrationDB.QueryRowContext(ctx, `
		INSERT INTO ops_error_logs (
			error_phase, error_type, severity, status_code, created_at
		) VALUES (
			'upstream', 'upstream_error', 'error', 500, NOW()
		) RETURNING id`,
	).Scan(&plainID)
	require.NoError(t, err)
	require.Positive(t, plainID)

	plain, err := repo.GetErrorLogByID(ctx, plainID)
	require.NoError(t, err)
	require.NotNil(t, plain)

	require.Empty(t, plain.AttemptedKeyPrefix, "no prefix for plain error")
	require.Nil(t, plain.DeletedKeyOwnerUserID, "no owner for plain error")
	require.Empty(t, plain.DeletedKeyOwnerEmail, "no owner email for plain error")
	require.Empty(t, plain.DeletedKeyName, "no key name for plain error")
	require.Empty(t, plain.APIKeyPrefix, "no api key prefix for plain error")

	// ── Case 3: ()key ──────
	// (+ $41 ),
	validID, err := repo.InsertErrorLog(ctx, &service.OpsInsertErrorLogInput{
		ErrorPhase:   "request",
		ErrorType:    "api_error",
		Severity:     "error",
		StatusCode:   402,
		CreatedAt:    time.Now(),
		APIKeyPrefix: "sk-valid",
	})
	require.NoError(t, err)
	require.Positive(t, validID)

	valid, err := repo.GetErrorLogByID(ctx, validID)
	require.NoError(t, err)
	require.NotNil(t, valid)

	require.Equal(t, "sk-valid", valid.APIKeyPrefix)
	require.Empty(t, valid.AttemptedKeyPrefix, "attempted prefix and api key prefix are mutually exclusive")
	require.Nil(t, valid.DeletedKeyOwnerUserID, "valid key error has no deleted owner")
}
