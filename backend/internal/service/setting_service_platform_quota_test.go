//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestMergePlatformQuotaDefaults_PatchSemantics(t *testing.T) {
	five := 5.0
	base := DefaultPlatformQuotaSetting{
		DailyLimitUSD:  &five,
		WeeklyLimitUSD: &five,
	}
	ten := 10.0
	patch := DefaultPlatformQuotaSetting{DailyLimitUSD: &ten}

	mergePlatformQuotaDefaults(&base, &patch)
	if base.DailyLimitUSD == nil || *base.DailyLimitUSD != 10.0 {
		t.Errorf("daily not patched: %+v", base.DailyLimitUSD)
	}
	if base.WeeklyLimitUSD == nil || *base.WeeklyLimitUSD != 5.0 {
		t.Errorf("weekly should remain 5.0: %+v", base.WeeklyLimitUSD)
	}
}

func TestMergePlatformQuotaDefaults_ZeroIsExplicitDisable(t *testing.T) {
	five := 5.0
	base := DefaultPlatformQuotaSetting{DailyLimitUSD: &five}
	zero := 0.0
	patch := DefaultPlatformQuotaSetting{DailyLimitUSD: &zero}

	mergePlatformQuotaDefaults(&base, &patch)
	if base.DailyLimitUSD == nil || *base.DailyLimitUSD != 0 {
		t.Errorf("explicit 0 should patch base, got %+v", base.DailyLimitUSD)
	}
}

func TestMergePlatformQuotaDefaults_NilSrcIsNoop(t *testing.T) {
	five := 5.0
	base := DefaultPlatformQuotaSetting{DailyLimitUSD: &five}
	mergePlatformQuotaDefaults(&base, nil)
	if base.DailyLimitUSD == nil || *base.DailyLimitUSD != 5.0 {
		t.Errorf("nil src should be no-op: %+v", base.DailyLimitUSD)
	}
}

func floatPtrPQ(v float64) *float64 { return &v }

func newSettingServiceForPlatformQuotaTest(seed map[string]string) *SettingService {
	repo := newMockSettingRepo()
	for k, v := range seed {
		repo.data[k] = v
	}
	return NewSettingService(repo, &config.Config{})
}

func TestGetDefaultPlatformQuotas_ReturnsFourPlatforms(t *testing.T) {
	zero := 0.0
	svc := newSettingServiceForPlatformQuotaTest(map[string]string{
		// =10.5, openai monthly=0, gemini/antigravity
		SettingKeyDefaultPlatformQuotas: `{"anthropic":{"daily":10.5},"openai":{"monthly":0}}`,
	})
	got, err := svc.GetDefaultPlatformQuotas(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	//
	for _, platform := range []string{"anthropic", "openai", "gemini", "antigravity"} {
		if _, ok := got[platform]; !ok {
			t.Errorf("missing platform key: %q", platform)
		}
	}
	// anthropic daily = 10.5
	if v := got["anthropic"].DailyLimitUSD; v == nil || *v != 10.5 {
		t.Errorf("anthropic daily want 10.5, got %v", v)
	}
	// openai monthly = 0（
	if v := got["openai"].MonthlyLimitUSD; v == nil || *v != zero {
		t.Errorf("openai monthly want 0 (explicit disable), got %v", v)
	}
	// gemini → weekly = nil
	if v := got["gemini"].WeeklyLimitUSD; v != nil {
		t.Errorf("gemini weekly want nil (not configured), got %v", *v)
	}
	// antigravity → daily = nil
	if v := got["antigravity"].DailyLimitUSD; v != nil {
		t.Errorf("antigravity daily want nil (not configured), got %v", *v)
	}
}

func TestGetAuthSourcePlatformQuotas_OnlyConfiguredReturned(t *testing.T) {
	source := "email"
	// =5, monthly=100；openai weekly=0；gemini/antigravity
	svc := newSettingServiceForPlatformQuotaTest(map[string]string{
		SettingKeyAuthSourcePlatformQuotas(source): `{"anthropic":{"daily":5,"monthly":100},"openai":{"weekly":0}}`,
	})
	got := svc.GetAuthSourcePlatformQuotas(context.Background(), source)

	// anthropic →
	anthro, ok := got["anthropic"]
	if !ok {
		t.Fatal("expected anthropic to be present")
	}
	if anthro.DailyLimitUSD == nil || *anthro.DailyLimitUSD != 5.0 {
		t.Errorf("anthropic daily want 5.0, got %v", anthro.DailyLimitUSD)
	}
	if anthro.MonthlyLimitUSD == nil || *anthro.MonthlyLimitUSD != 100.0 {
		t.Errorf("anthropic monthly want 100.0, got %v", anthro.MonthlyLimitUSD)
	}
	if anthro.WeeklyLimitUSD != nil {
		t.Errorf("anthropic weekly not configured, want nil, got %v", *anthro.WeeklyLimitUSD)
	}

	// openai weekly=0 →
	oai, ok := got["openai"]
	if !ok {
		t.Fatal("expected openai to be present")
	}
	if oai.WeeklyLimitUSD == nil || *oai.WeeklyLimitUSD != 0 {
		t.Errorf("openai weekly want 0, got %v", oai.WeeklyLimitUSD)
	}

	// gemini / antigravity →
	if _, ok := got["gemini"]; ok {
		t.Error("gemini not configured, should be absent from result")
	}
	if _, ok := got["antigravity"]; ok {
		t.Error("antigravity not configured, should be absent from result")
	}
}

func TestGetAuthSourcePlatformQuotas_AllNegativeOrEmpty_NoEntry(t *testing.T) {
	source := "linuxdo"
	// →
	svc := newSettingServiceForPlatformQuotaTest(map[string]string{
		SettingKeyAuthSourcePlatformQuotas(source): `{}`,
	})
	got := svc.GetAuthSourcePlatformQuotas(context.Background(), source)
	// → override
	if _, ok := got["openai"]; ok {
		t.Error("empty JSON object should result in no openai entry")
	}
	if len(got) != 0 {
		t.Errorf("expected empty result map, got %v", got)
	}
}

// TestSystemPlatformQuotas_WriteReadRoundTrip
// ——→read
func TestSystemPlatformQuotas_WriteReadRoundTrip(t *testing.T) {
	svc := newSettingServiceForPlatformQuotaTest(nil)
	ctx := context.Background()

	ten := 10.0
	ss := &SystemSettings{
		DefaultPlatformQuotas: map[string]*DefaultPlatformQuotaSetting{
			"anthropic": {DailyLimitUSD: &ten, WeeklyLimitUSD: nil, MonthlyLimitUSD: nil},
		},
	}
	if err := svc.UpdateSettings(ctx, ss); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	got, err := svc.GetDefaultPlatformQuotas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 4-key
	for _, p := range []string{"anthropic", "openai", "gemini", "antigravity"} {
		if _, ok := got[p]; !ok {
			t.Errorf("4-key contract violated: missing platform %q", p)
		}
	}
	if v := got["anthropic"].DailyLimitUSD; v == nil || *v != ten {
		t.Fatalf("anthropic daily round-trip failed: got %v, want 10", v)
	}
	//
	if got["openai"].DailyLimitUSD != nil {
		t.Errorf("openai daily should be nil (not written), got %v", got["openai"].DailyLimitUSD)
	}
}

// TestSystemPlatformQuotas_EmptyMapClearsAll
// ={}
// "= "
func TestSystemPlatformQuotas_EmptyMapClearsAll(t *testing.T) {
	svc := newSettingServiceForPlatformQuotaTest(nil)
	ctx := context.Background()

	ten := 10.0
	if err := svc.UpdateSettings(ctx, &SystemSettings{
		DefaultPlatformQuotas: map[string]*DefaultPlatformQuotaSetting{
			"anthropic": {DailyLimitUSD: &ten},
		},
	}); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	//
	if err := svc.UpdateSettings(ctx, &SystemSettings{
		DefaultPlatformQuotas: map[string]*DefaultPlatformQuotaSetting{},
	}); err != nil {
		t.Fatalf("empty map write: %v", err)
	}

	got, err := svc.GetDefaultPlatformQuotas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 4
	for _, p := range []string{"anthropic", "openai", "gemini", "antigravity"} {
		if _, ok := got[p]; !ok {
			t.Errorf("4-key contract violated after empty write: missing %q", p)
		}
	}
	//
	for _, p := range AllowedQuotaPlatforms {
		pq := got[p]
		if pq == nil {
			continue
		}
		if pq.DailyLimitUSD != nil || pq.WeeklyLimitUSD != nil || pq.MonthlyLimitUSD != nil {
			t.Errorf("platform %q should have all-nil limits after empty-map write, got %+v", p, pq)
		}
	}
}

// TestUpdateSettingsWithAuthSourceDefaults_PlatformQuotaRoundTrip
// PUT /admin/settings × platform × window
// Round-4
func TestUpdateSettingsWithAuthSourceDefaults_PlatformQuotaRoundTrip(t *testing.T) {
	svc := newSettingServiceForPlatformQuotaTest(nil)
	systemSettings := &SystemSettings{}
	authDefaults := &AuthSourceDefaultSettings{
		Email: ProviderDefaultGrantSettings{
			PlatformQuotas: map[string]*DefaultPlatformQuotaSetting{
				"anthropic": {
					DailyLimitUSD:   floatPtrPQ(5.0),
					WeeklyLimitUSD:  nil, // 无限额
					MonthlyLimitUSD: floatPtrPQ(100.0),
				},
				"openai": {
					DailyLimitUSD: floatPtrPQ(0), // 显式禁用
				},
			},
		},
	}
	if err := svc.UpdateSettingsWithAuthSourceDefaults(context.Background(), systemSettings, authDefaults); err != nil {
		t.Fatalf("UpdateSettingsWithAuthSourceDefaults: %v", err)
	}
	got := svc.GetAuthSourcePlatformQuotas(context.Background(), "email")
	anthro := got["anthropic"]
	if anthro == nil || anthro.DailyLimitUSD == nil || *anthro.DailyLimitUSD != 5.0 {
		t.Errorf("anthropic daily round-trip failed: %+v", anthro)
	}
	if anthro != nil && anthro.WeeklyLimitUSD != nil {
		t.Errorf("anthropic weekly want nil (无限额), got %v", *anthro.WeeklyLimitUSD)
	}
	if anthro == nil || anthro.MonthlyLimitUSD == nil || *anthro.MonthlyLimitUSD != 100.0 {
		t.Errorf("anthropic monthly round-trip failed: %+v", anthro)
	}
	oai := got["openai"]
	if oai == nil || oai.DailyLimitUSD == nil || *oai.DailyLimitUSD != 0 {
		t.Errorf("openai daily=0 (禁用) round-trip failed: %+v", oai)
	}
	//
	if linux := svc.GetAuthSourcePlatformQuotas(context.Background(), "linuxdo"); len(linux) != 0 {
		t.Errorf("linuxdo should be empty, got %+v", linux)
	}
}

// TestUpdateSettingsWithAuthSourceDefaults_NilPlatformQuotaPreservesExisting #2
//
//
func TestUpdateSettingsWithAuthSourceDefaults_NilPlatformQuotaPreservesExisting(t *testing.T) {
	svc := newSettingServiceForPlatformQuotaTest(map[string]string{
		SettingKeyAuthSourcePlatformQuotas("email"): `{"anthropic":{"daily":5,"weekly":null,"monthly":null}}`,
	})
	// authDefaults ——
	authDefaults := &AuthSourceDefaultSettings{
		Email: ProviderDefaultGrantSettings{PlatformQuotas: nil},
	}
	if err := svc.UpdateSettingsWithAuthSourceDefaults(context.Background(), &SystemSettings{}, authDefaults); err != nil {
		t.Fatalf("UpdateSettingsWithAuthSourceDefaults: %v", err)
	}
	anthro := svc.GetAuthSourcePlatformQuotas(context.Background(), "email")["anthropic"]
	if anthro == nil || anthro.DailyLimitUSD == nil || *anthro.DailyLimitUSD != 5.0 {
		t.Errorf("nil PlatformQuotas 应保留既有 anthropic daily=5，got %+v", anthro)
	}
}

// TestGetAuthSourcePlatformQuotas_JSON
//
func TestGetAuthSourcePlatformQuotas_JSON(t *testing.T) {
	svc := newSettingServiceForPlatformQuotaTest(map[string]string{
		SettingKeyAuthSourcePlatformQuotas("email"): `{"openai":{"daily":null,"weekly":null,"monthly":20}}`,
	})
	got := svc.GetAuthSourcePlatformQuotas(context.Background(), "email")

	// openai monthly = 20
	oai, ok := got["openai"]
	if !ok {
		t.Fatal("expected openai to be present")
	}
	if oai.MonthlyLimitUSD == nil || *oai.MonthlyLimitUSD != 20 {
		t.Errorf("openai monthly want 20, got %v", oai.MonthlyLimitUSD)
	}
	if oai.DailyLimitUSD != nil {
		t.Errorf("openai daily want nil, got %v", *oai.DailyLimitUSD)
	}
	if oai.WeeklyLimitUSD != nil {
		t.Errorf("openai weekly want nil, got %v", *oai.WeeklyLimitUSD)
	}

	// anthropic →
	if _, ok := got["anthropic"]; ok {
		t.Error("anthropic not configured, should be absent from result")
	}
}

// TestUpdateSettingsWithAuthSourceDefaults_NegativeQuotaRejected
// auth-source platform quota
func TestUpdateSettingsWithAuthSourceDefaults_NegativeQuotaRejected(t *testing.T) {
	svc := newSettingServiceForPlatformQuotaTest(nil)
	neg := -1.0
	authDefaults := &AuthSourceDefaultSettings{
		Email: ProviderDefaultGrantSettings{
			PlatformQuotas: map[string]*DefaultPlatformQuotaSetting{
				"anthropic": {DailyLimitUSD: &neg},
			},
		},
	}
	err := svc.UpdateSettingsWithAuthSourceDefaults(context.Background(), &SystemSettings{}, authDefaults)
	require.Error(t, err, "expected error for negative quota")
	require.Equal(t, "INVALID_DEFAULT_PLATFORM_QUOTA", infraerrors.Reason(err))
}
