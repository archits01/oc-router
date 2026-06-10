//go:build unit

//

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

// accountRepoStub
//
//
//   - exists:
//   - existsErr:
//   - deleteErr:
//   - deletedIDs:
type accountRepoStub struct {
	exists     bool    // ExistsByID 的returned值
	existsErr  error   // ExistsByID 的errorreturned值
	deleteErr  error   // Delete 的errorreturned值
	deletedIDs []int64 // 记录已delete的账号 ID 列表
}

//

func (s *accountRepoStub) Create(ctx context.Context, account *Account) error {
	panic("unexpected Create call")
}

func (s *accountRepoStub) GetByID(ctx context.Context, id int64) (*Account, error) {
	panic("unexpected GetByID call")
}

func (s *accountRepoStub) GetByIDs(ctx context.Context, ids []int64) ([]*Account, error) {
	panic("unexpected GetByIDs call")
}

// ExistsByID
//
func (s *accountRepoStub) ExistsByID(ctx context.Context, id int64) (bool, error) {
	return s.exists, s.existsErr
}

func (s *accountRepoStub) GetByCRSAccountID(ctx context.Context, crsAccountID string) (*Account, error) {
	panic("unexpected GetByCRSAccountID call")
}

func (s *accountRepoStub) FindByExtraField(ctx context.Context, key string, value any) ([]Account, error) {
	panic("unexpected FindByExtraField call")
}

func (s *accountRepoStub) ListCRSAccountIDs(ctx context.Context) (map[string]int64, error) {
	panic("unexpected ListCRSAccountIDs call")
}

func (s *accountRepoStub) Update(ctx context.Context, account *Account) error {
	panic("unexpected Update call")
}

// Delete
//
func (s *accountRepoStub) Delete(ctx context.Context, id int64) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.deleteErr
}


func (s *accountRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (s *accountRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (s *accountRepoStub) ListByGroup(ctx context.Context, groupID int64) ([]Account, error) {
	panic("unexpected ListByGroup call")
}

func (s *accountRepoStub) ListActive(ctx context.Context) ([]Account, error) {
	panic("unexpected ListActive call")
}

func (s *accountRepoStub) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	panic("unexpected ListByPlatform call")
}

func (s *accountRepoStub) UpdateLastUsed(ctx context.Context, id int64) error {
	panic("unexpected UpdateLastUsed call")
}

func (s *accountRepoStub) BatchUpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	panic("unexpected BatchUpdateLastUsed call")
}

func (s *accountRepoStub) SetError(ctx context.Context, id int64, errorMsg string) error {
	panic("unexpected SetError call")
}

func (s *accountRepoStub) ClearError(ctx context.Context, id int64) error {
	panic("unexpected ClearError call")
}

func (s *accountRepoStub) SetSchedulable(ctx context.Context, id int64, schedulable bool) error {
	panic("unexpected SetSchedulable call")
}

func (s *accountRepoStub) AutoPauseExpiredAccounts(ctx context.Context, now time.Time) (int64, error) {
	panic("unexpected AutoPauseExpiredAccounts call")
}

func (s *accountRepoStub) BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error {
	panic("unexpected BindGroups call")
}

func (s *accountRepoStub) ListSchedulable(ctx context.Context) ([]Account, error) {
	panic("unexpected ListSchedulable call")
}

func (s *accountRepoStub) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]Account, error) {
	panic("unexpected ListSchedulableByGroupID call")
}

func (s *accountRepoStub) ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error) {
	panic("unexpected ListSchedulableByPlatform call")
}

func (s *accountRepoStub) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	panic("unexpected ListSchedulableByGroupIDAndPlatform call")
}

func (s *accountRepoStub) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]Account, error) {
	panic("unexpected ListSchedulableByPlatforms call")
}

func (s *accountRepoStub) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]Account, error) {
	panic("unexpected ListSchedulableByGroupIDAndPlatforms call")
}

func (s *accountRepoStub) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]Account, error) {
	panic("unexpected ListSchedulableUngroupedByPlatform call")
}

func (s *accountRepoStub) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]Account, error) {
	panic("unexpected ListSchedulableUngroupedByPlatforms call")
}

func (s *accountRepoStub) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	panic("unexpected SetRateLimited call")
}

func (s *accountRepoStub) SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time, reason ...string) error {
	panic("unexpected SetModelRateLimit call")
}

func (s *accountRepoStub) SetOverloaded(ctx context.Context, id int64, until time.Time) error {
	panic("unexpected SetOverloaded call")
}

func (s *accountRepoStub) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	panic("unexpected SetTempUnschedulable call")
}

func (s *accountRepoStub) ClearTempUnschedulable(ctx context.Context, id int64) error {
	panic("unexpected ClearTempUnschedulable call")
}

func (s *accountRepoStub) ClearRateLimit(ctx context.Context, id int64) error {
	panic("unexpected ClearRateLimit call")
}

func (s *accountRepoStub) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	panic("unexpected ClearAntigravityQuotaScopes call")
}

func (s *accountRepoStub) ClearModelRateLimits(ctx context.Context, id int64) error {
	panic("unexpected ClearModelRateLimits call")
}

func (s *accountRepoStub) UpdateSessionWindow(ctx context.Context, id int64, start, end *time.Time, status string) error {
	panic("unexpected UpdateSessionWindow call")
}

func (s *accountRepoStub) UpdateSessionWindowEnd(ctx context.Context, id int64, end time.Time) error {
	panic("unexpected UpdateSessionWindowEnd call")
}

func (s *accountRepoStub) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	panic("unexpected UpdateExtra call")
}

func (s *accountRepoStub) BulkUpdate(ctx context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	panic("unexpected BulkUpdate call")
}

func (s *accountRepoStub) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) error {
	return nil
}

func (s *accountRepoStub) ResetQuotaUsed(ctx context.Context, id int64) error {
	return nil
}

func (s *accountRepoStub) RevertProxyFallback(ctx context.Context, accountID int64) error {
	panic("unexpected RevertProxyFallback call")
}

// TestAccountService_Delete_NotFound
//   - ExistsByID
//   -
//   - Delete
func TestAccountService_Delete_NotFound(t *testing.T) {
	repo := &accountRepoStub{exists: false}
	svc := &AccountService{accountRepo: repo}

	err := svc.Delete(context.Background(), 55)
	require.ErrorIs(t, err, ErrAccountNotFound)
	require.Empty(t, repo.deletedIDs) // validationdelete操作未被调用
}

// TestAccountService_Delete_CheckError
//   - ExistsByID
//   - "check account"
//   - Delete
func TestAccountService_Delete_CheckError(t *testing.T) {
	repo := &accountRepoStub{existsErr: errors.New("db down")}
	svc := &AccountService{accountRepo: repo}

	err := svc.Delete(context.Background(), 55)
	require.Error(t, err)
	require.ErrorContains(t, err, "check account") // validationerrorinfo包含上下文
	require.Empty(t, repo.deletedIDs)
}

// TestAccountService_Delete_DeleteError
//   - ExistsByID
//   - Delete
//   - "delete account"
//   - deletedIDs
func TestAccountService_Delete_DeleteError(t *testing.T) {
	repo := &accountRepoStub{
		exists:    true,
		deleteErr: errors.New("delete failed"),
	}
	svc := &AccountService{accountRepo: repo}

	err := svc.Delete(context.Background(), 55)
	require.Error(t, err)
	require.ErrorContains(t, err, "delete account")
	require.Equal(t, []int64{55}, repo.deletedIDs) // validationdelete操作被调用
}

// TestAccountService_Delete_Success
//   - ExistsByID
//   - Delete
//   -
//   - deletedIDs
func TestAccountService_Delete_Success(t *testing.T) {
	repo := &accountRepoStub{exists: true}
	svc := &AccountService{accountRepo: repo}

	err := svc.Delete(context.Background(), 55)
	require.NoError(t, err)
	require.Equal(t, []int64{55}, repo.deletedIDs) // validation正确的 ID 被delete
}
