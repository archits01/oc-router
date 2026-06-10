//go:build integration

package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/suite"
)

type UserRepoAPIKeyGroupFilterSuite struct {
	suite.Suite
	ctx    context.Context
	client *dbent.Client
	repo   *userRepository
}

func (s *UserRepoAPIKeyGroupFilterSuite) SetupTest() {
	s.ctx = context.Background()
	s.client = testEntClient(s.T())
	s.repo = newUserRepositoryWithSQL(s.client, integrationDB)
	// api_keys
	_, _ = integrationDB.ExecContext(s.ctx, "DELETE FROM api_keys")
	_, _ = integrationDB.ExecContext(s.ctx, "DELETE FROM user_allowed_groups")
	_, _ = integrationDB.ExecContext(s.ctx, "DELETE FROM user_subscriptions")
	_, _ = integrationDB.ExecContext(s.ctx, "DELETE FROM users")
	_, _ = integrationDB.ExecContext(s.ctx, "DELETE FROM groups")
}

func TestUserRepoAPIKeyGroupFilterSuite(t *testing.T) {
	suite.Run(t, new(UserRepoAPIKeyGroupFilterSuite))
}

func (s *UserRepoAPIKeyGroupFilterSuite) mustCreateUser(email string) *service.User {
	s.T().Helper()
	u := &service.User{
		Email:        email,
		PasswordHash: "test-password-hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		Concurrency:  5,
	}
	s.Require().NoError(s.repo.Create(s.ctx, u), "create user")
	return u
}

func (s *UserRepoAPIKeyGroupFilterSuite) mustCreateGroup(name string) *dbent.Group {
	s.T().Helper()
	g, err := s.client.Group.Create().
		SetName(name).
		SetStatus(service.StatusActive).
		Save(s.ctx)
	s.Require().NoError(err, "create group")
	return g
}

func (s *UserRepoAPIKeyGroupFilterSuite) mustCreateAPIKey(userID int64, key, name string, groupID *int64) *dbent.APIKey {
	s.T().Helper()
	create := s.client.APIKey.Create().
		SetUserID(userID).
		SetKey(key).
		SetName(name)
	if groupID != nil {
		create = create.SetGroupID(*groupID)
	}
	ak, err := create.Save(s.ctx)
	s.Require().NoError(err, "create api key")
	return ak
}

func (s *UserRepoAPIKeyGroupFilterSuite) ids(users []service.User) []int64 {
	out := make([]int64, len(users))
	for i := range users {
		out[i] = users[i].ID
	}
	return out
}

func (s *UserRepoAPIKeyGroupFilterSuite) listByAPIKeyGroup(groupID int64) []service.User {
	s.T().Helper()
	users, _, err := s.repo.ListWithFilters(
		s.ctx,
		pagination.PaginationParams{Page: 1, PageSize: 50},
		service.UserListFilters{APIKeyGroupID: groupID},
	)
	s.Require().NoError(err, "ListWithFilters")
	return users
}

//
func (s *UserRepoAPIKeyGroupFilterSuite) TestFiltersUsersByAPIKeyGroup() {
	g := s.mustCreateGroup("grp-target")
	other := s.mustCreateGroup("grp-other")
	hit := s.mustCreateUser("hit@test.com")
	miss := s.mustCreateUser("miss@test.com")
	s.mustCreateAPIKey(hit.ID, "sk-hit", "K", &g.ID)
	s.mustCreateAPIKey(miss.ID, "sk-miss", "K", &other.ID)

	s.Require().Equal([]int64{hit.ID}, s.ids(s.listByAPIKeyGroup(g.ID)))
}

//
func (s *UserRepoAPIKeyGroupFilterSuite) TestSoftDeletedAPIKeyExcluded() {
	g := s.mustCreateGroup("grp-soft")
	u := s.mustCreateUser("soft@test.com")
	ak := s.mustCreateAPIKey(u.ID, "sk-soft", "K", &g.ID)
	//
	s.Require().NoError(s.client.APIKey.DeleteOne(ak).Exec(s.ctx), "soft delete api key")

	s.Require().Empty(s.listByAPIKeyGroup(g.ID), "user with only a soft-deleted key must not match")
}

// →
func (s *UserRepoAPIKeyGroupFilterSuite) TestMultipleKeysAnyMatchDedup() {
	g := s.mustCreateGroup("grp-multi")
	other := s.mustCreateGroup("grp-multi-other")
	u := s.mustCreateUser("multi@test.com")
	s.mustCreateAPIKey(u.ID, "sk-m1", "K1", &other.ID)
	s.mustCreateAPIKey(u.ID, "sk-m2", "K2", &g.ID)
	s.mustCreateAPIKey(u.ID, "sk-m3", "K3", nil) // 无分组

	s.Require().Equal([]int64{u.ID}, s.ids(s.listByAPIKeyGroup(g.ID)))
}

// ——
func (s *UserRepoAPIKeyGroupFilterSuite) TestAPIKeyGroupAndStatusFilter() {
	g := s.mustCreateGroup("grp-combined")

	// active →
	active := s.mustCreateUser("active-hit@test.com")
	s.mustCreateAPIKey(active.ID, "sk-active", "K", &g.ID)

	// disabled → =active
	disabled := s.mustCreateUser("disabled-hit@test.com")
	s.mustCreateAPIKey(disabled.ID, "sk-disabled", "K2", &g.ID)
	_, err := s.client.User.UpdateOneID(disabled.ID).SetStatus(service.StatusDisabled).Save(s.ctx)
	s.Require().NoError(err, "disable user")

	// active → group
	other := s.mustCreateGroup("grp-combined-other")
	miss := s.mustCreateUser("active-miss@test.com")
	s.mustCreateAPIKey(miss.ID, "sk-miss", "K3", &other.ID)

	users, _, err := s.repo.ListWithFilters(
		s.ctx,
		pagination.PaginationParams{Page: 1, PageSize: 50},
		service.UserListFilters{
			APIKeyGroupID: g.ID,
			Status:        service.StatusActive,
		},
	)
	s.Require().NoError(err)
	s.Require().Equal([]int64{active.ID}, s.ids(users), "only active user with matching key group should match")
}

// =0）
func (s *UserRepoAPIKeyGroupFilterSuite) TestZeroGroupIDNoFilter() {
	g := s.mustCreateGroup("grp-zero")
	u1 := s.mustCreateUser("z1@test.com")
	u2 := s.mustCreateUser("z2@test.com")
	s.mustCreateAPIKey(u1.ID, "sk-z1", "K", &g.ID)

	users, _, err := s.repo.ListWithFilters(
		s.ctx,
		pagination.PaginationParams{Page: 1, PageSize: 50},
		service.UserListFilters{APIKeyGroupID: 0},
	)
	s.Require().NoError(err)
	s.Require().ElementsMatch([]int64{u1.ID, u2.ID}, s.ids(users))
}
