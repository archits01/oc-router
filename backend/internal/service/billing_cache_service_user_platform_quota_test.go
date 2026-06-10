//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// fakeIncrCache
type fakeIncrCache struct {
	BillingCache
	calls []incrCall
}

type incrCall struct {
	userID    int64
	platform  string
	cost      float64
	ttl       time.Duration
	markDirty bool
}

func (f *fakeIncrCache) IncrUserPlatformQuotaUsageCache(ctx context.Context, userID int64, platform string, cost float64, ttl time.Duration, markDirty bool) error {
	f.calls = append(f.calls, incrCall{userID, platform, cost, ttl, markDirty})
	return nil
}

// IncrementUserPlatformQuotaUsage
//
func TestIncrementUserPlatformQuotaUsage_SyncCallsCache(t *testing.T) {
	fake := &fakeIncrCache{}
	cfg := &config.Config{}
	cfg.Billing.UserPlatformQuotaCacheTTLSeconds = 120

	s := &BillingCacheService{
		cache: fake,
		cfg:   cfg,
	}

	s.IncrementUserPlatformQuotaUsage(101, "anthropic", 0.25)
	s.IncrementUserPlatformQuotaUsage(101, "openai", 0.50)

	if len(fake.calls) != 2 {
		t.Fatalf("expected 2 incr calls, got %d", len(fake.calls))
	}
	if fake.calls[0] != (incrCall{userID: 101, platform: "anthropic", cost: 0.25, ttl: 120 * time.Second, markDirty: false}) {
		t.Errorf("call[0] = %+v", fake.calls[0])
	}
	if fake.calls[1] != (incrCall{userID: 101, platform: "openai", cost: 0.50, ttl: 120 * time.Second, markDirty: false}) {
		t.Errorf("call[1] = %+v", fake.calls[1])
	}
}

// ── T6 tests: checkUserPlatformQuotaEligibility ──────────────────────────────

// fakeQuotaRepo
type fakeQuotaRepo struct {
	rec *UserPlatformQuotaRecord
}

func (f *fakeQuotaRepo) GetByUserPlatform(_ context.Context, _ int64, _ string) (*UserPlatformQuotaRecord, error) {
	return f.rec, nil
}

func (f *fakeQuotaRepo) BulkInsertInitial(_ context.Context, _ []UserPlatformQuotaRecord) error {
	return nil
}

func (f *fakeQuotaRepo) IncrementUsageWithReset(_ context.Context, _ int64, _ string, _ float64, _ time.Time) error {
	return nil
}

func (f *fakeQuotaRepo) ListByUser(_ context.Context, _ int64) ([]UserPlatformQuotaRecord, error) {
	return nil, nil
}

func (f *fakeQuotaRepo) UpsertForUser(_ context.Context, _ int64, _ []UserPlatformQuotaRecord) error {
	return nil
}

func (f *fakeQuotaRepo) ResetExpiredWindow(_ context.Context, _ int64, _ string, _ string, _ time.Time) error {
	return nil
}

func (f *fakeQuotaRepo) BatchSnapshotUsage(_ context.Context, _ []UserPlatformQuotaSnapshot, _ time.Time) error {
	return nil
}

// fakeFullCache + Set + Incr + Delete + Pop/Readd/BatchGet（
// mu
type fakeFullCache struct {
	BillingCache
	mu          sync.Mutex
	entry       *UserPlatformQuotaCacheEntry
	deleteCalls int
	setCalls    int           // SetUserPlatformQuotaCache 调用次数
	lastSetTTL  time.Duration // 最近一次 Set 的 ttl
	getErr      error         // 非 nil 时 Get 先returned (nil,false,getErr)
	setErr      error         // 非 nil 时 Set returned该 err(setCalls 仍+1)
	// dirty
	dirty map[UserPlatformQuotaKey]struct{}
}

// getDeleteCalls
func (f *fakeFullCache) getDeleteCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deleteCalls
}

// getEntry
func (f *fakeFullCache) getEntry() *UserPlatformQuotaCacheEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.entry
}

// getSetCalls
func (f *fakeFullCache) getSetCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.setCalls
}

// getLastSetTTL
func (f *fakeFullCache) getLastSetTTL() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastSetTTL
}

func (f *fakeFullCache) GetUserPlatformQuotaCache(_ context.Context, _ int64, _ string) (*UserPlatformQuotaCacheEntry, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, false, f.getErr
	}
	if f.entry == nil {
		return nil, false, nil
	}
	return f.entry, true, nil
}

func (f *fakeFullCache) SetUserPlatformQuotaCache(_ context.Context, _ int64, _ string, e *UserPlatformQuotaCacheEntry, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	if f.setErr != nil {
		return f.setErr
	}
	f.entry = e
	f.lastSetTTL = ttl
	return nil
}

func (f *fakeFullCache) DeleteUserPlatformQuotaCache(_ context.Context, _ int64, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls++
	f.entry = nil
	return nil
}

func (f *fakeFullCache) PopDirtyUserPlatformQuotaKeys(_ context.Context, n int) ([]UserPlatformQuotaKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.dirty) == 0 {
		return nil, nil
	}
	keys := make([]UserPlatformQuotaKey, 0, n)
	for k := range f.dirty {
		if len(keys) >= n {
			break
		}
		keys = append(keys, k)
		delete(f.dirty, k)
	}
	return keys, nil
}

func (f *fakeFullCache) ReaddDirtyUserPlatformQuotaKeys(_ context.Context, keys []UserPlatformQuotaKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dirty == nil {
		f.dirty = make(map[UserPlatformQuotaKey]struct{})
	}
	for _, k := range keys {
		f.dirty[k] = struct{}{}
	}
	return nil
}

// BatchGetUserPlatformQuotaCache → nil），
//
func (f *fakeFullCache) BatchGetUserPlatformQuotaCache(_ context.Context, keys []UserPlatformQuotaKey) ([]*UserPlatformQuotaCacheEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	results := make([]*UserPlatformQuotaCacheEntry, len(keys))
	for i := range keys {
		results[i] = f.entry
	}
	return results, nil
}

func newServiceForPreflight(t *testing.T, repo UserPlatformQuotaRepository, cache BillingCache) *BillingCacheService {
	t.Helper()
	cfg := &config.Config{}
	cfg.Billing.UserPlatformQuotaCacheTTLSeconds = 60
	return &BillingCacheService{
		cache:                 cache,
		cfg:                   cfg,
		userPlatformQuotaRepo: repo,
	}
}

// currentDayStart
func currentDayStart() *time.Time {
	s := timezone.StartOfDay(time.Now())
	return &s
}

func TestCheckUserPlatformQuotaEligibility_AllowsWhenUnderLimit(t *testing.T) {
	daily := 10.0
	repo := &fakeQuotaRepo{rec: &UserPlatformQuotaRecord{
		UserID: 1, Platform: "anthropic", DailyLimitUSD: &daily,
	}}
	cache := &fakeFullCache{entry: &UserPlatformQuotaCacheEntry{
		DailyUsageUSD:    4.5,
		DailyLimitUSD:    &daily,
		DailyWindowStart: currentDayStart(),
		SchemaVersion:    UserPlatformQuotaCacheSchemaV1,
	}}
	s := newServiceForPreflight(t, repo, cache)
	if err := s.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCheckUserPlatformQuotaEligibility_DailyExhausted(t *testing.T) {
	daily := 5.0
	repo := &fakeQuotaRepo{rec: &UserPlatformQuotaRecord{
		UserID: 1, Platform: "anthropic", DailyLimitUSD: &daily,
	}}
	cache := &fakeFullCache{entry: &UserPlatformQuotaCacheEntry{
		DailyUsageUSD:    5.0,
		DailyLimitUSD:    &daily,
		DailyWindowStart: currentDayStart(),
		SchemaVersion:    UserPlatformQuotaCacheSchemaV1,
	}}
	s := newServiceForPreflight(t, repo, cache)
	err := s.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic")
	if !errors.Is(err, ErrUserPlatformDailyQuotaExhausted) {
		t.Errorf("expected ErrUserPlatformDailyQuotaExhausted, got %v", err)
	}
}

func TestCheckUserPlatformQuotaEligibility_NilLimitMeansUnlimited(t *testing.T) {
	repo := &fakeQuotaRepo{rec: &UserPlatformQuotaRecord{
		UserID: 1, Platform: "anthropic",
	}}
	cache := &fakeFullCache{entry: &UserPlatformQuotaCacheEntry{
		DailyUsageUSD:    999,
		DailyWindowStart: currentDayStart(),
		SchemaVersion:    UserPlatformQuotaCacheSchemaV1,
		// DailyLimitUSD nil →
	}}
	s := newServiceForPreflight(t, repo, cache)
	if err := s.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic"); err != nil {
		t.Errorf("nil limits should be unlimited, got %v", err)
	}
}

func TestCheckUserPlatformQuotaEligibility_ZeroLimitImmediateBlock(t *testing.T) {
	zero := 0.0
	repo := &fakeQuotaRepo{rec: &UserPlatformQuotaRecord{
		UserID: 1, Platform: "anthropic", DailyLimitUSD: &zero,
	}}
	cache := &fakeFullCache{entry: &UserPlatformQuotaCacheEntry{
		DailyUsageUSD:    0,
		DailyLimitUSD:    &zero,
		DailyWindowStart: currentDayStart(),
		SchemaVersion:    UserPlatformQuotaCacheSchemaV1,
	}}
	s := newServiceForPreflight(t, repo, cache)
	err := s.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic")
	if !errors.Is(err, ErrUserPlatformDailyQuotaExhausted) {
		t.Errorf("expected daily exhausted for limit=0, got %v", err)
	}
}

func TestCheckUserPlatformQuotaEligibility_NoRecordMeansUnlimited(t *testing.T) {
	repo := &fakeQuotaRepo{rec: nil}
	cache := &fakeFullCache{}
	s := newServiceForPreflight(t, repo, cache)
	if err := s.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic"); err != nil {
		t.Errorf("no record = unlimited, got %v", err)
	}
}

// TestCheckUserPlatformQuotaEligibility_OldSchemaCacheMissTriggersDB =0）
//
// DB record
func TestCheckUserPlatformQuotaEligibility_OldSchemaCacheMissTriggersDB(t *testing.T) {
	daily := 5.0
	dayStart := currentDayStart()
	repo := &fakeQuotaRepo{rec: &UserPlatformQuotaRecord{
		UserID: 1, Platform: "anthropic", DailyLimitUSD: &daily, DailyUsageUSD: 6.0,
		DailyWindowStart: dayStart,
	}}
	// SchemaVersion=0（
	cache := &fakeFullCache{entry: &UserPlatformQuotaCacheEntry{DailyUsageUSD: 1.0}}
	s := newServiceForPreflight(t, repo, cache)
	err := s.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic")
	if !errors.Is(err, ErrUserPlatformDailyQuotaExhausted) {
		t.Errorf("旧版 entry 应走 DB 路径并报 daily exhausted, got %v", err)
	}
}

// TestCheckUserPlatformQuotaEligibility_WindowExpiredInCache
func TestCheckUserPlatformQuotaEligibility_WindowExpiredInCache(t *testing.T) {
	daily := 5.0
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) // 远古窗口起始，肯定expired
	repo := &fakeQuotaRepo{rec: &UserPlatformQuotaRecord{
		UserID: 1, Platform: "anthropic", DailyLimitUSD: &daily,
	}}
	cache := &fakeFullCache{entry: &UserPlatformQuotaCacheEntry{
		DailyUsageUSD:    10.0, // 超限，但窗口expired
		DailyLimitUSD:    &daily,
		DailyWindowStart: &past,
		SchemaVersion:    UserPlatformQuotaCacheSchemaV1,
	}}
	s := newServiceForPreflight(t, repo, cache)
	err := s.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic")
	if err != nil {
		t.Errorf("过期窗口应归zero放行, got %v", err)
	}
}

// TestCheckUserPlatformQuotaEligibility_WindowExpiredRefreshesCache
// V1 HIT ():
//  1. →
//  2. cache entry + window_start
//     limit
func TestCheckUserPlatformQuotaEligibility_WindowExpiredRefreshesCache(t *testing.T) {
	daily := 5.0
	//
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeQuotaRepo{rec: &UserPlatformQuotaRecord{
		UserID: 1, Platform: "anthropic", DailyLimitUSD: &daily,
	}}
	cache := &fakeFullCache{entry: &UserPlatformQuotaCacheEntry{
		DailyUsageUSD:    10.0, // 超限,但窗口expired → 应被本地清zero后放行
		DailyLimitUSD:    &daily,
		DailyWindowStart: &past,
		SchemaVersion:    UserPlatformQuotaCacheSchemaV1,
	}}
	s := newServiceForPreflight(t, repo, cache)

	// (=0 < limit=5)
	err := s.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic")
	if err != nil {
		t.Errorf("过期窗口应归zero放行, got %v", err)
	}

	//
	refreshed := cache.getEntry()
	if refreshed == nil {
		t.Fatal("窗口过期后 cache entry should not be nil(应被 SetCache 覆盖,而非 Delete)")
	}
	if refreshed.DailyUsageUSD != 0 {
		t.Errorf("刷新后 DailyUsageUSD = %v, want 0", refreshed.DailyUsageUSD)
	}
	if refreshed.DailyLimitUSD == nil || *refreshed.DailyLimitUSD != daily {
		t.Errorf("刷新后 DailyLimitUSD = %v, want %v(保留)", refreshed.DailyLimitUSD, daily)
	}
	if refreshed.SchemaVersion != UserPlatformQuotaCacheSchemaV1 {
		t.Errorf("刷新后 SchemaVersion = %d, want V1", refreshed.SchemaVersion)
	}
	if refreshed.DailyWindowStart == nil || refreshed.DailyWindowStart.Equal(past) {
		t.Errorf("刷新后 DailyWindowStart = %v, 应update到当前窗口而非保留 past=%v", refreshed.DailyWindowStart, past)
	}
}

// ── T5 tests: QueueUpdateUserPlatformQuotaUsage ───────────────────────────────

// ── C-NEW-1: monthlyQuotaWindowExpired 30 ─────────────────────────

func TestMonthlyQuotaWindowExpired_NilStart(t *testing.T) {
	if !monthlyQuotaWindowExpired(nil, time.Now().UTC()) {
		t.Error("nil start should be considered expired")
	}
}

func TestMonthlyQuotaWindowExpired_Expired(t *testing.T) {
	start := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if !monthlyQuotaWindowExpired(&start, time.Now().UTC()) {
		t.Error("start exactly 30 days ago should be expired")
	}
}

func TestMonthlyQuotaWindowExpired_Active(t *testing.T) {
	start := time.Now().UTC().Add(-29 * 24 * time.Hour)
	if monthlyQuotaWindowExpired(&start, time.Now().UTC()) {
		t.Error("start 29 days ago should NOT be expired")
	}
}

// TestMonthlyQuotaWindowExpired_CrossMonthBoundary
func TestMonthlyQuotaWindowExpired_CrossMonthBoundary(t *testing.T) {
	//
	start := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if monthlyQuotaWindowExpired(&start, now) {
		t.Error("11 days into window should NOT be expired (30-day rolling, not calendar month)")
	}
}

// TestNextMonthlyResetFrom
func TestNextMonthlyResetFrom_WithStart(t *testing.T) {
	start := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	want := start.Add(30 * 24 * time.Hour)
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	got := nextMonthlyResetFrom(&start, now)
	if !got.Equal(want) {
		t.Errorf("nextMonthlyResetFrom = %v, want %v", got, want)
	}
}

func TestNextMonthlyResetFrom_NilStart(t *testing.T) {
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	got := nextMonthlyResetFrom(nil, now)
	want := now.Add(30 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Errorf("nextMonthlyResetFrom(nil) = %v, want now+30d=%v", got, want)
	}
}

func TestNextMonthlyResetFrom_NilStart_NotEqualToNow(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	got := nextMonthlyResetFrom(nil, now)
	want := now.Add(30 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Errorf("nextMonthlyResetFrom(nil) = %v, want %v (now+30d)", got, want)
	}
	if got.Equal(now) {
		t.Error("nextMonthlyResetFrom(nil) must not return now (should be now+30d)")
	}
}

// TestNextMonthlyResetFrom_ExpiredStart >= 30d）
// +30d，+30d（
// fallback
func TestNextMonthlyResetFrom_ExpiredStart(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC) // 距 start 61 天，expired
	got := nextMonthlyResetFrom(&start, now)
	want := now.Add(30 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Errorf("nextMonthlyResetFrom(expired) = %v, want now+30d=%v", got, want)
	}
	if !got.After(now) {
		t.Error("expired window 的下次重置必须在 now 之后，不能是过去时间")
	}
}

func TestIncrementUserPlatformQuotaUsage_GuardsAgainstEmpty(t *testing.T) {
	fake := &fakeIncrCache{}
	cfg := &config.Config{}
	cfg.Billing.UserPlatformQuotaCacheTTLSeconds = 60
	s := &BillingCacheService{
		cache: fake,
		cfg:   cfg,
	}

	s.IncrementUserPlatformQuotaUsage(1, "", 0.5)        // empty platform → noop
	s.IncrementUserPlatformQuotaUsage(1, "openai", 0)    // zero cost → noop
	s.IncrementUserPlatformQuotaUsage(1, "openai", -0.1) // negative → noop

	if len(fake.calls) != 0 {
		t.Errorf("expected 0 calls (all guarded), got %d", len(fake.calls))
	}
}

// ── C-NEW-2: ×platform quota ──────────────────────────
//
// 1. standard =0 →
// 2. — !isSubscriptionMode
//
//

// fakeZeroQuotaCache =0（quota
type fakeZeroQuotaCache struct {
	BillingCache
	called bool
}

func (f *fakeZeroQuotaCache) GetUserPlatformQuotaCache(_ context.Context, _ int64, _ string) (*UserPlatformQuotaCacheEntry, bool, error) {
	f.called = true
	daily := 0.0
	entry := &UserPlatformQuotaCacheEntry{
		DailyUsageUSD:    0,
		DailyLimitUSD:    &daily,
		DailyWindowStart: func() *time.Time { t := time.Now().UTC(); return &t }(),
		SchemaVersion:    UserPlatformQuotaCacheSchemaV1,
	}
	return entry, true, nil
}

func (f *fakeZeroQuotaCache) DeleteUserPlatformQuotaCache(_ context.Context, _ int64, _ string) error {
	return nil
}

// SetUserPlatformQuotaCache
// "→ SetCache "
func (f *fakeZeroQuotaCache) SetUserPlatformQuotaCache(_ context.Context, _ int64, _ string, _ *UserPlatformQuotaCacheEntry, _ time.Duration) error {
	return nil
}

// GetSubscriptionCache
//
func (f *fakeZeroQuotaCache) GetSubscriptionCache(_ context.Context, _ int64, _ int64) (*SubscriptionCacheData, error) {
	return &SubscriptionCacheData{
		Status:       SubscriptionStatusActive,
		ExpiresAt:    time.Now().Add(30 * 24 * time.Hour),
		DailyUsage:   0,
		WeeklyUsage:  0,
		MonthlyUsage: 0,
	}, nil
}

func (f *fakeZeroQuotaCache) GetUserBalanceCache(_ context.Context, _ int64) (float64, bool, error) {
	return 100.0, true, nil
}

// TestCheckUserPlatformQuotaEligibility_StandardMode_BlocksWhenLimitZero
// standard =0
func TestCheckUserPlatformQuotaEligibility_StandardMode_BlocksWhenLimitZero(t *testing.T) {
	fake := &fakeZeroQuotaCache{}
	cfg := &config.Config{}
	cfg.Billing.UserPlatformQuotaCacheTTLSeconds = 60
	s := &BillingCacheService{
		cache:                 fake,
		cfg:                   cfg,
		userPlatformQuotaRepo: &fakeQuotaRepo{},
	}
	err := s.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic")
	if !errors.Is(err, ErrUserPlatformDailyQuotaExhausted) {
		t.Errorf("standard mode with limit=0 should return ErrUserPlatformDailyQuotaExhausted, got: %v", err)
	}
	if !fake.called {
		t.Error("GetUserPlatformQuotaCache should have been called in standard mode")
	}
}

// TestCheckBillingEligibility_SubscriptionMode_BypassesPlatformQuota
// ×platform quota
func TestCheckBillingEligibility_SubscriptionMode_BypassesPlatformQuota(t *testing.T) {
	fake := &fakeZeroQuotaCache{} // GetUserPlatformQuotaCache returned limit=0，若被调用则拦截
	cfg := &config.Config{}
	cfg.Billing.UserPlatformQuotaCacheTTLSeconds = 60
	s := &BillingCacheService{
		cache:                 fake,
		cfg:                   cfg,
		userPlatformQuotaRepo: &fakeQuotaRepo{},
	}

	subGroup := &Group{
		ID:               10,
		SubscriptionType: "subscription",
		Status:           "active",
		// → checkSubscriptionEligibility
	}
	sub := &UserSubscription{Status: "active"}
	user := &User{ID: 42}

	err := s.CheckBillingEligibility(context.Background(), user, nil, subGroup, sub, "anthropic")
	// ×platform quota
	if errors.Is(err, ErrUserPlatformDailyQuotaExhausted) ||
		errors.Is(err, ErrUserPlatformWeeklyQuotaExhausted) ||
		errors.Is(err, ErrUserPlatformMonthlyQuotaExhausted) {
		t.Errorf("subscription mode should bypass user×platform quota, got: %v", err)
	}
	// GetUserPlatformQuotaCache
	if fake.called {
		t.Error("GetUserPlatformQuotaCache must NOT be called in subscription mode (C-NEW-2)")
	}
}

// TestCheckBillingEligibility_NonSubscriptionGroup_AppliesQuota
// =nil）
func TestCheckBillingEligibility_NonSubscriptionGroup_AppliesQuota(t *testing.T) {
	called := &fakeZeroQuotaCache{}
	cfg := &config.Config{}
	cfg.Billing.UserPlatformQuotaCacheTTLSeconds = 60
	s := &BillingCacheService{
		cache:                 called,
		cfg:                   cfg,
		userPlatformQuotaRepo: &fakeQuotaRepo{},
	}
	err := s.checkUserPlatformQuotaEligibility(context.Background(), 99, "openai")
	if !errors.Is(err, ErrUserPlatformDailyQuotaExhausted) {
		t.Errorf("non-subscription mode quota check should block, got: %v", err)
	}
	if !called.called {
		t.Error("GetUserPlatformQuotaCache should be consulted in non-subscription mode")
	}
}

// ── B-3: monthlyQuotaWindowExpired 30 ────────────────────────
//
//  1. → expired
//  2. 30*24h - 1ns → not expired
//  3. → 2024-03-29T00:00:01Z）→ expired
//  4. → 2025-01-14T00:00:01Z）→ expired
//
// repo
func TestMonthlyQuotaWindowExpired_BoundaryTable(t *testing.T) {
	const thirtyDays = 30 * 24 * time.Hour

	cases := []struct {
		name    string
		start   time.Time
		now     time.Time
		expired bool
	}{
		{
			name:    "exactly 30 days → expired",
			start:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			now:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(thirtyDays),
			expired: true,
		},
		{
			name:    "30d minus 1ns → not expired",
			start:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			now:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(thirtyDays - 1),
			expired: false,
		},
		{
			name:    "cross month-end (Feb→Mar, 29d+1s) → expired",
			start:   time.Date(2024, 2, 28, 0, 0, 0, 0, time.UTC),
			now:     time.Date(2024, 3, 29, 0, 0, 1, 0, time.UTC),
			expired: true,
		},
		{
			name:    "cross year boundary (Dec→Jan, 30d+1s) → expired",
			start:   time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC),
			now:     time.Date(2025, 1, 14, 0, 0, 1, 0, time.UTC),
			expired: true,
		},
	}

	for _, tc := range cases {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			got := monthlyQuotaWindowExpired(&tc.start, tc.now)
			if got != tc.expired {
				t.Errorf("monthlyQuotaWindowExpired(start=%v, now=%v) = %v, want %v",
					tc.start, tc.now, got, tc.expired)
			}
		})
	}
}

// TestCheckUserPlatformQuotaEligibility_NoRow_WritesSentinel
// cache MISS + DB (),
// TTL = UserPlatformQuotaSentinelTTLSeconds,(fail-open)。
func TestCheckUserPlatformQuotaEligibility_NoRow_WritesSentinel(t *testing.T) {
	repo := &fakeQuotaRepo{rec: nil} // DB 无行
	cache := &fakeFullCache{}        // entry=nil → Get returned MISS
	svc := newServiceForPreflight(t, repo, cache)
	svc.cfg.Billing.UserPlatformQuotaSentinelTTLSeconds = 3600

	if err := svc.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic"); err != nil {
		t.Fatalf("expected nil (fail-open), got %v", err)
	}
	if cache.getSetCalls() != 1 {
		t.Fatalf("expected 1 SetUserPlatformQuotaCache call for sentinel, got %d", cache.getSetCalls())
	}
	sentinel := cache.getEntry()
	if sentinel == nil {
		t.Fatal("expected sentinel entry backfilled")
	}
	if sentinel.DailyLimitUSD != nil || sentinel.WeeklyLimitUSD != nil || sentinel.MonthlyLimitUSD != nil {
		t.Errorf("sentinel must have all-nil limits")
	}
	if sentinel.DailyWindowStart == nil || sentinel.WeeklyWindowStart == nil || sentinel.MonthlyWindowStart == nil {
		t.Errorf("sentinel must have non-nil window_start to avoid refresh churn")
	}
	if sentinel.SchemaVersion != UserPlatformQuotaCacheSchemaV1 {
		t.Errorf("sentinel schema = %d, want V1", sentinel.SchemaVersion)
	}
	if cache.getLastSetTTL() != 3600*time.Second {
		t.Errorf("sentinel ttl = %v, want 3600s", cache.getLastSetTTL())
	}
}

// TestCheckUserPlatformQuotaEligibility_RedisGetError_NoSentinelBackfill
// Redis GET (cacheErr!=nil)+ DB ("Redis " ),
func TestCheckUserPlatformQuotaEligibility_RedisGetError_NoSentinelBackfill(t *testing.T) {
	repo := &fakeQuotaRepo{rec: nil}
	cache := &fakeFullCache{getErr: errors.New("redis get down")}
	svc := newServiceForPreflight(t, repo, cache)
	svc.cfg.Billing.UserPlatformQuotaSentinelTTLSeconds = 3600

	if err := svc.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic"); err != nil {
		t.Fatalf("redis 故障应 fail-open, got %v", err)
	}
	if cache.getSetCalls() != 0 {
		t.Errorf("redis-get-error 时不应回填 sentinel, got %d set calls", cache.getSetCalls())
	}
}

// TestCheckUserPlatformQuotaEligibility_NoRow_SentinelSetFailsFailOpen
// sentinel SET ()
func TestCheckUserPlatformQuotaEligibility_NoRow_SentinelSetFailsFailOpen(t *testing.T) {
	before := userPlatformQuotaSentinelSetCacheErrorTotal.Load()
	repo := &fakeQuotaRepo{rec: nil}
	cache := &fakeFullCache{setErr: errors.New("redis set timeout")}
	svc := newServiceForPreflight(t, repo, cache)
	svc.cfg.Billing.UserPlatformQuotaSentinelTTLSeconds = 3600

	if err := svc.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic"); err != nil {
		t.Fatalf("sentinel set failed应 fail-open, got %v", err)
	}
	if cache.getSetCalls() != 1 {
		t.Errorf("应尝试 set sentinel 恰好一次, got %d", cache.getSetCalls())
	}
	if got := userPlatformQuotaSentinelSetCacheErrorTotal.Load() - before; got != 1 {
		t.Errorf("set failed应使 metric +1, got delta %d", got)
	}
}

// TestCheckUserPlatformQuotaEligibility_SentinelCrossDay_NoRefresh
// ()(daily/weekly )
// ()。
func TestCheckUserPlatformQuotaEligibility_SentinelCrossDay_NoRefresh(t *testing.T) {
	yesterday := timezone.StartOfDay(time.Now().AddDate(0, 0, -1))
	lastWeek := timezone.StartOfWeek(time.Now().AddDate(0, 0, -7))
	monthAgoOK := time.Now().AddDate(0, 0, -5) // <30d, monthly 不过期
	sentinel := &UserPlatformQuotaCacheEntry{
		SchemaVersion:      UserPlatformQuotaCacheSchemaV1,
		DailyWindowStart:   &yesterday, // 跨日 → daily windowExpired = true
		WeeklyWindowStart:  &lastWeek,  // 跨周 → weekly windowExpired = true
		MonthlyWindowStart: &monthAgoOK,
		// limits → sentinel
	}
	cache := &fakeFullCache{entry: sentinel} // entry 非 nil → Get HIT
	svc := newServiceForPreflight(t, &fakeQuotaRepo{}, cache)

	if err := svc.checkUserPlatformQuotaEligibility(context.Background(), 1, "anthropic"); err != nil {
		t.Fatalf("sentinel = no limit, expected nil, got %v", err)
	}
	if cache.getSetCalls() != 0 {
		t.Errorf("sentinel cross-window must NOT trigger refresh SetCache, got %d calls", cache.getSetCalls())
	}
}

// ── TestHasUserPlatformQuotaLimit ────────────────────────────────────────────

func TestHasUserPlatformQuotaLimit(t *testing.T) {
	daily := 5.0

	tests := []struct {
		name    string
		setup   func() *BillingCacheService
		want    bool
	}{
		{
			name: "has_limit",
			setup: func() *BillingCacheService {
				entry := &UserPlatformQuotaCacheEntry{DailyLimitUSD: &daily}
				svc := newServiceForPreflight(t, &fakeQuotaRepo{}, &fakeFullCache{entry: entry})
				return svc
			},
			want: true,
		},
		{
			name: "sentinel_no_limit",
			setup: func() *BillingCacheService {
				entry := &UserPlatformQuotaCacheEntry{} // 三个 limit 字段全 nil
				svc := newServiceForPreflight(t, &fakeQuotaRepo{}, &fakeFullCache{entry: entry})
				return svc
			},
			want: false,
		},
		{
			name: "cache_miss",
			setup: func() *BillingCacheService {
				// entry==nil → GetUserPlatformQuotaCache (nil,false,nil)
				svc := newServiceForPreflight(t, &fakeQuotaRepo{}, &fakeFullCache{})
				return svc
			},
			want: true, // fail-safe
		},
		{
			name: "redis_err",
			setup: func() *BillingCacheService {
				svc := newServiceForPreflight(t, &fakeQuotaRepo{}, &fakeFullCache{getErr: errors.New("redis down")})
				return svc
			},
			want: true, // fail-safe
		},
		{
			name: "simple_mode",
			setup: func() *BillingCacheService {
				entry := &UserPlatformQuotaCacheEntry{DailyLimitUSD: &daily}
				svc := newServiceForPreflight(t, &fakeQuotaRepo{}, &fakeFullCache{entry: entry})
				svc.cfg.RunMode = config.RunModeSimple
				return svc
			},
			want: false, // simple 模式始终跳过
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := tt.setup()
			got := svc.HasUserPlatformQuotaLimit(context.Background(), 1, "anthropic")
			if got != tt.want {
				t.Errorf("HasUserPlatformQuotaLimit() = %v, want %v", got, tt.want)
			}
		})
	}
}
