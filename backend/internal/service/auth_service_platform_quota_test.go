//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// fakeInsertRecorder
type fakeInsertRecorder struct {
	records []UserPlatformQuotaRecord
	err     error
}

func (f *fakeInsertRecorder) GetByUserPlatform(_ context.Context, _ int64, _ string) (*UserPlatformQuotaRecord, error) {
	return nil, nil
}

func (f *fakeInsertRecorder) BulkInsertInitial(_ context.Context, recs []UserPlatformQuotaRecord) error {
	if f.err != nil {
		return f.err
	}
	f.records = append(f.records, recs...)
	return nil
}

func (f *fakeInsertRecorder) IncrementUsageWithReset(_ context.Context, _ int64, _ string, _ float64, _ time.Time) error {
	return nil
}

func (f *fakeInsertRecorder) ListByUser(_ context.Context, _ int64) ([]UserPlatformQuotaRecord, error) {
	return nil, nil
}

func (f *fakeInsertRecorder) UpsertForUser(_ context.Context, _ int64, _ []UserPlatformQuotaRecord) error {
	return nil
}

func (f *fakeInsertRecorder) ResetExpiredWindow(_ context.Context, _ int64, _ string, _ string, _ time.Time) error {
	return nil
}

func (f *fakeInsertRecorder) BatchSnapshotUsage(_ context.Context, _ []UserPlatformQuotaSnapshot, _ time.Time) error {
	return nil
}

func TestSnapshotPlatformQuotaDefaults_PassesToRepoBulkInsert(t *testing.T) {
	fakeRepo := &fakeInsertRecorder{}
	s := &AuthService{userPlatformQuotaRepo: fakeRepo}

	five := 5.0
	plan := &signupGrantPlan{
		PlatformQuotas: map[string]*DefaultPlatformQuotaSetting{
			"anthropic":   {DailyLimitUSD: &five},
			"openai":      {},
			"gemini":      {},
			"antigravity": {},
		},
	}
	if err := s.snapshotPlatformQuotaDefaults(context.Background(), 999, plan); err != nil {
		t.Fatal(err)
	}
	if len(fakeRepo.records) != 4 {
		t.Errorf("expected 4 records, got %d", len(fakeRepo.records))
	}
	found := false
	for _, r := range fakeRepo.records {
		if r.UserID == 999 && r.Platform == "anthropic" && r.DailyLimitUSD != nil && *r.DailyLimitUSD == 5 {
			found = true
		}
	}
	if !found {
		t.Error("anthropic daily = 5 not snapshotted")
	}
}

func TestSnapshotPlatformQuotaDefaults_NilPlanIsNoop(t *testing.T) {
	fakeRepo := &fakeInsertRecorder{}
	s := &AuthService{userPlatformQuotaRepo: fakeRepo}
	if err := s.snapshotPlatformQuotaDefaults(context.Background(), 1, nil); err != nil {
		t.Errorf("nil plan should be noop, got %v", err)
	}
	if len(fakeRepo.records) != 0 {
		t.Errorf("expected no records, got %d", len(fakeRepo.records))
	}
}

func TestSnapshotPlatformQuotaDefaults_RepoErrorFailsOpen(t *testing.T) {
	fakeRepo := &fakeInsertRecorder{err: fmt.Errorf("db down")}
	s := &AuthService{userPlatformQuotaRepo: fakeRepo}
	five := 5.0
	plan := &signupGrantPlan{
		PlatformQuotas: map[string]*DefaultPlatformQuotaSetting{
			"anthropic": {DailyLimitUSD: &five},
		},
	}
	if err := s.snapshotPlatformQuotaDefaults(context.Background(), 1, plan); err != nil {
		t.Errorf("fail-open: expected nil even on repo error, got %v", err)
	}
}

func TestSnapshotPlatformQuotaDefaults_NilRepoIsNoop(t *testing.T) {
	s := &AuthService{userPlatformQuotaRepo: nil}
	five := 5.0
	plan := &signupGrantPlan{
		PlatformQuotas: map[string]*DefaultPlatformQuotaSetting{"a": {DailyLimitUSD: &five}},
	}
	if err := s.snapshotPlatformQuotaDefaults(context.Background(), 1, plan); err != nil {
		t.Errorf("nil repo should be noop, got %v", err)
	}
}

// resolveSignupGrantPlan
// settingRepoStub
func TestResolveSignupGrantPlan_GlobalQuotaLoadedBeforeAuthSource(t *testing.T) {
	//
	settings := map[string]string{
		SettingKeyRegistrationEnabled: "true",
		SettingKeyDefaultPlatformQuotas: `{
			"anthropic":   {"daily": 10, "weekly": 50, "monthly": 200},
			"openai":      {"daily": 5,  "weekly": 25, "monthly": 100},
			"gemini":      {"daily": 5,  "weekly": 25, "monthly": 100},
			"antigravity": {"daily": 5,  "weekly": 25, "monthly": 100}
		}`,
	}
	svc := newAuthService(nil, settings, nil, nil)
	plan := svc.resolveSignupGrantPlan(context.Background(), "email")
	if plan.PlatformQuotas == nil {
		t.Fatal("expected PlatformQuotas to be non-nil after loading global quota KVs")
	}
	q := plan.PlatformQuotas["anthropic"]
	if q == nil {
		t.Fatal("expected anthropic quota to be set")
	}
	if q.DailyLimitUSD == nil || *q.DailyLimitUSD != 10 {
		t.Errorf("expected anthropic daily=10, got %v", q.DailyLimitUSD)
	}
}

// TestResolveSignupGrantPlan_DisabledAuthSourceStillCarriesGlobalQuota
// !enabled
func TestResolveSignupGrantPlan_DisabledAuthSourceStillCarriesGlobalQuota(t *testing.T) {
	settings := map[string]string{
		SettingKeyRegistrationEnabled: "true",
		// auth source => !enabled
		SettingKeyDefaultPlatformQuotas: `{"anthropic": {"daily": 10, "weekly": 50, "monthly": 200}}`,
	}
	svc := newAuthService(nil, settings, nil, nil)
	plan := svc.resolveSignupGrantPlan(context.Background(), "email")
	// !enabled
	if plan.PlatformQuotas == nil {
		t.Fatal("P1 violated: PlatformQuotas is nil even with global quota KVs set")
	}
	// P1
	if _, ok := plan.PlatformQuotas["anthropic"]; !ok {
		t.Error("P1 violated: disabled auth source path dropped global platform quota")
	}
}
