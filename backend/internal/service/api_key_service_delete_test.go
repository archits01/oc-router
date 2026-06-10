//go:build unit

// API Key
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

// apiKeyRepoStub
//
//
//   - apiKey/getByIDErr:
//   - deleteErr:
//   - deletedIDs:
type apiKeyRepoStub struct {
	apiKey             *APIKey // GetKeyAndOwnerID 的returned值
	getByIDErr         error   // GetKeyAndOwnerID 的errorreturned值
	deleteErr          error   // Delete 的errorreturned值
	deletedIDs         []int64 // 记录已delete的 API Key ID 列表
	allowListByUserID  bool
	listByUserIDKeys   []APIKey
	listByUserIDErr    error
	listByUserIDCalls  []int64
	listByUserIDParams []pagination.PaginationParams
	updateLastUsed     func(ctx context.Context, id int64, usedAt time.Time) error
	touchedIDs         []int64
	touchedUsedAts     []time.Time
}

//

func (s *apiKeyRepoStub) Create(ctx context.Context, key *APIKey) error {
	panic("unexpected Create call")
}

func (s *apiKeyRepoStub) GetByID(ctx context.Context, id int64) (*APIKey, error) {
	if s.getByIDErr != nil {
		return nil, s.getByIDErr
	}
	if s.apiKey != nil {
		clone := *s.apiKey
		return &clone, nil
	}
	panic("unexpected GetByID call")
}

func (s *apiKeyRepoStub) GetKeyAndOwnerID(ctx context.Context, id int64) (string, int64, error) {
	if s.getByIDErr != nil {
		return "", 0, s.getByIDErr
	}
	if s.apiKey != nil {
		return s.apiKey.Key, s.apiKey.UserID, nil
	}
	return "", 0, ErrAPIKeyNotFound
}

func (s *apiKeyRepoStub) GetByKey(ctx context.Context, key string) (*APIKey, error) {
	panic("unexpected GetByKey call")
}

func (s *apiKeyRepoStub) GetByKeyForAuth(ctx context.Context, key string) (*APIKey, error) {
	panic("unexpected GetByKeyForAuth call")
}

func (s *apiKeyRepoStub) Update(ctx context.Context, key *APIKey) error {
	panic("unexpected Update call")
}

// Delete
//
func (s *apiKeyRepoStub) Delete(ctx context.Context, id int64) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.deleteErr
}

// DeleteWithAudit
func (s *apiKeyRepoStub) DeleteWithAudit(ctx context.Context, id int64) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.deleteErr
}


func (s *apiKeyRepoStub) ListByUserID(ctx context.Context, userID int64, params pagination.PaginationParams, filters APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	if !s.allowListByUserID {
		panic("unexpected ListByUserID call")
	}
	s.listByUserIDCalls = append(s.listByUserIDCalls, userID)
	s.listByUserIDParams = append(s.listByUserIDParams, params)
	if s.listByUserIDErr != nil {
		return nil, nil, s.listByUserIDErr
	}
	keys := append([]APIKey(nil), s.listByUserIDKeys...)
	return keys, &pagination.PaginationResult{
		Total:    int64(len(keys)),
		Page:     params.Page,
		PageSize: params.PageSize,
		Pages:    1,
	}, nil
}

func (s *apiKeyRepoStub) VerifyOwnership(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error) {
	panic("unexpected VerifyOwnership call")
}

func (s *apiKeyRepoStub) CountByUserID(ctx context.Context, userID int64) (int64, error) {
	panic("unexpected CountByUserID call")
}

func (s *apiKeyRepoStub) ExistsByKey(ctx context.Context, key string) (bool, error) {
	panic("unexpected ExistsByKey call")
}

func (s *apiKeyRepoStub) ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}

func (s *apiKeyRepoStub) SearchAPIKeys(ctx context.Context, userID int64, keyword string, limit int) ([]APIKey, error) {
	panic("unexpected SearchAPIKeys call")
}

func (s *apiKeyRepoStub) ClearGroupIDByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected ClearGroupIDByGroupID call")
}
func (s *apiKeyRepoStub) UpdateGroupIDByUserAndGroup(ctx context.Context, userID, oldGroupID, newGroupID int64) (int64, error) {
	panic("unexpected UpdateGroupIDByUserAndGroup call")
}

func (s *apiKeyRepoStub) CountByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected CountByGroupID call")
}

func (s *apiKeyRepoStub) ListKeysByUserID(ctx context.Context, userID int64) ([]string, error) {
	panic("unexpected ListKeysByUserID call")
}

func (s *apiKeyRepoStub) ListKeysByGroupID(ctx context.Context, groupID int64) ([]string, error) {
	panic("unexpected ListKeysByGroupID call")
}

func (s *apiKeyRepoStub) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) (float64, error) {
	panic("unexpected IncrementQuotaUsed call")
}

func (s *apiKeyRepoStub) UpdateLastUsed(ctx context.Context, id int64, usedAt time.Time) error {
	s.touchedIDs = append(s.touchedIDs, id)
	s.touchedUsedAts = append(s.touchedUsedAts, usedAt)
	if s.updateLastUsed != nil {
		return s.updateLastUsed(ctx, id, usedAt)
	}
	return nil
}

func (s *apiKeyRepoStub) IncrementRateLimitUsage(ctx context.Context, id int64, cost float64) error {
	panic("unexpected IncrementRateLimitUsage call")
}

func (s *apiKeyRepoStub) ResetRateLimitWindows(ctx context.Context, id int64) error {
	panic("unexpected ResetRateLimitWindows call")
}

func (s *apiKeyRepoStub) GetRateLimitData(ctx context.Context, id int64) (*APIKeyRateLimitData, error) {
	panic("unexpected GetRateLimitData call")
}

// apiKeyCacheStub
//
//   - invalidated:
type apiKeyCacheStub struct {
	invalidated    []int64  // 记录调用 DeleteCreateAttemptCount 时传入的user ID
	deleteAuthKeys []string // 记录调用 DeleteAuthCache 时传入的缓存 key
}

// GetCreateAttemptCount
func (s *apiKeyCacheStub) GetCreateAttemptCount(ctx context.Context, userID int64) (int, error) {
	return 0, nil
}

// IncrementCreateAttemptCount
func (s *apiKeyCacheStub) IncrementCreateAttemptCount(ctx context.Context, userID int64) error {
	return nil
}

// DeleteCreateAttemptCount
//
func (s *apiKeyCacheStub) DeleteCreateAttemptCount(ctx context.Context, userID int64) error {
	s.invalidated = append(s.invalidated, userID)
	return nil
}

// IncrementDailyUsage
func (s *apiKeyCacheStub) IncrementDailyUsage(ctx context.Context, apiKey string) error {
	return nil
}

// SetDailyUsageExpiry
func (s *apiKeyCacheStub) SetDailyUsageExpiry(ctx context.Context, apiKey string, ttl time.Duration) error {
	return nil
}

func (s *apiKeyCacheStub) GetAuthCache(ctx context.Context, key string) (*APIKeyAuthCacheEntry, error) {
	return nil, nil
}

func (s *apiKeyCacheStub) SetAuthCache(ctx context.Context, key string, entry *APIKeyAuthCacheEntry, ttl time.Duration) error {
	return nil
}

func (s *apiKeyCacheStub) DeleteAuthCache(ctx context.Context, key string) error {
	s.deleteAuthKeys = append(s.deleteAuthKeys, key)
	return nil
}

func (s *apiKeyCacheStub) PublishAuthCacheInvalidation(ctx context.Context, cacheKey string) error {
	return nil
}

func (s *apiKeyCacheStub) SubscribeAuthCacheInvalidation(ctx context.Context, handler func(cacheKey string)) error {
	return nil
}

// TestApiKeyService_Delete_OwnerMismatch
//   - GetKeyAndOwnerID
//   -
//   -
//   - Delete
func TestApiKeyService_Delete_OwnerMismatch(t *testing.T) {
	repo := &apiKeyRepoStub{
		apiKey: &APIKey{ID: 10, UserID: 1, Key: "k"},
	}
	cache := &apiKeyCacheStub{}
	svc := &APIKeyService{apiKeyRepo: repo, cache: cache}

	err := svc.Delete(context.Background(), 10, 2) // API Key ID=10, 调用者 userID=2
	require.ErrorIs(t, err, ErrInsufficientPerms)
	require.Empty(t, repo.deletedIDs)   // validationdelete操作未被调用
	require.Empty(t, cache.invalidated) // validation缓存未被清除
	require.Empty(t, cache.deleteAuthKeys)
}

// TestApiKeyService_Delete_Success
//   - GetKeyAndOwnerID
//   -
//   - Delete
//   -
//   -
func TestApiKeyService_Delete_Success(t *testing.T) {
	repo := &apiKeyRepoStub{
		apiKey: &APIKey{ID: 42, UserID: 7, Key: "k"},
	}
	cache := &apiKeyCacheStub{}
	svc := &APIKeyService{apiKeyRepo: repo, cache: cache}
	svc.lastUsedTouchL1.Store(int64(42), time.Now())

	err := svc.Delete(context.Background(), 42, 7) // API Key ID=42, 调用者 userID=7
	require.NoError(t, err)
	require.Equal(t, []int64{42}, repo.deletedIDs)  // validation正确的 API Key 被delete
	require.Equal(t, []int64{7}, cache.invalidated) // validation所有者的缓存被清除
	require.Equal(t, []string{svc.authCacheKey("k")}, cache.deleteAuthKeys)
	_, exists := svc.lastUsedTouchL1.Load(int64(42))
	require.False(t, exists, "delete should clear touch debounce cache")
}

// TestApiKeyService_Delete_NotFound
//   - GetKeyAndOwnerID
//   -
//   - Delete
func TestApiKeyService_Delete_NotFound(t *testing.T) {
	repo := &apiKeyRepoStub{getByIDErr: ErrAPIKeyNotFound}
	cache := &apiKeyCacheStub{}
	svc := &APIKeyService{apiKeyRepo: repo, cache: cache}

	err := svc.Delete(context.Background(), 99, 1)
	require.ErrorIs(t, err, ErrAPIKeyNotFound)
	require.Empty(t, repo.deletedIDs)
	require.Empty(t, cache.invalidated)
	require.Empty(t, cache.deleteAuthKeys)
}

// TestApiKeyService_Delete_DeleteFails
//   - GetKeyAndOwnerID
//   - DeleteWithAudit
//   - "delete api key"
func TestApiKeyService_Delete_DeleteFails(t *testing.T) {
	repo := &apiKeyRepoStub{
		apiKey:    &APIKey{ID: 42, UserID: 3, Key: "k"},
		deleteErr: errors.New("delete failed"),
	}
	cache := &apiKeyCacheStub{}
	svc := &APIKeyService{apiKeyRepo: repo, cache: cache}

	err := svc.Delete(context.Background(), 3, 3) // API Key ID=3, 调用者 userID=3
	require.Error(t, err)
	require.ErrorContains(t, err, "delete api key")
	require.Equal(t, []int64{3}, repo.deletedIDs) // validation DeleteWithAudit 被调用
	require.Empty(t, cache.invalidated)           // validationdeletefailed时缓存未被清除（新顺序：先删后清）
	require.Empty(t, cache.deleteAuthKeys)        // validationdeletefailed时 auth 缓存未被清除
}
