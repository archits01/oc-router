//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRepo_ListWithFilters_IncludeDeleted(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := NewUserRepository(client, integrationDB)

	active := mustCreateUser(t, client, &service.User{Email: "shared-keyword-active@test.com"})
	deleted := mustCreateUser(t, client, &service.User{Email: "shared-keyword-deleted@test.com"})
	require.NoError(t, client.User.DeleteOneID(deleted.ID).Exec(ctx))

	params := pagination.PaginationParams{Page: 1, PageSize: 50, SortBy: "email", SortOrder: "asc"}

	usersDefault, resDefault, err := repo.ListWithFilters(ctx, params,
		service.UserListFilters{Search: "shared-keyword-"})
	require.NoError(t, err)
	require.Len(t, usersDefault, 1)
	require.Equal(t, active.ID, usersDefault[0].ID)
	require.EqualValues(t, 1, resDefault.Total)

	// IncludeDeleted=true：
	usersAll, resAll, err := repo.ListWithFilters(ctx, params,
		service.UserListFilters{Search: "shared-keyword-", IncludeDeleted: true})
	require.NoError(t, err)
	require.Len(t, usersAll, 2)
	require.EqualValues(t, 2, resAll.Total, "Count must match result set row count")

	var delUser *service.User
	for i := range usersAll {
		if usersAll[i].ID == deleted.ID {
			delUser = &usersAll[i]
		}
	}
	require.NotNil(t, delUser)
	require.NotNil(t, delUser.DeletedAt)
}

func TestUserRepo_GetByIDIncludeDeleted(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := NewUserRepository(client, integrationDB)

	u := mustCreateUser(t, client, &service.User{Email: "getbyid-deleted@test.com"})
	require.NoError(t, client.User.DeleteOneID(u.ID).Exec(ctx))

	//
	_, err := repo.GetByID(ctx, u.ID)
	require.ErrorIs(t, err, service.ErrUserNotFound)

	// GetByIDIncludeDeleted：
	got, err := repo.GetByIDIncludeDeleted(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, "getbyid-deleted@test.com", got.Email)
	require.NotNil(t, got.DeletedAt)
}
