//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- billingModelForRestriction ---

func TestBillingModelForRestriction_Requested(t *testing.T) {
	t.Parallel()
	got := billingModelForRestriction(BillingModelSourceRequested, "claude-sonnet-4-5", "claude-sonnet-4-6")
	require.Equal(t, "claude-sonnet-4-5", got)
}

func TestBillingModelForRestriction_ChannelMapped(t *testing.T) {
	t.Parallel()
	got := billingModelForRestriction(BillingModelSourceChannelMapped, "claude-sonnet-4-5", "claude-sonnet-4-6")
	require.Equal(t, "claude-sonnet-4-6", got)
}

func TestBillingModelForRestriction_Upstream(t *testing.T) {
	t.Parallel()
	got := billingModelForRestriction(BillingModelSourceUpstream, "claude-sonnet-4-5", "claude-sonnet-4-6")
	require.Equal(t, "", got, "upstream should return empty (per-account check needed)")
}

func TestBillingModelForRestriction_Empty(t *testing.T) {
	t.Parallel()
	got := billingModelForRestriction("", "claude-sonnet-4-5", "claude-sonnet-4-6")
	require.Equal(t, "claude-sonnet-4-6", got, "empty source defaults to channel_mapped")
}

// --- resolveAccountUpstreamModel ---

func TestResolveAccountUpstreamModel_Antigravity(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformAntigravity,
	}
	// Antigravity
	got := resolveAccountUpstreamModel(account, "claude-sonnet-4-6")
	require.Equal(t, "claude-sonnet-4-6", got)
}

func TestResolveAccountUpstreamModel_Antigravity_Unsupported(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformAntigravity,
	}
	got := resolveAccountUpstreamModel(account, "totally-unknown-model")
	require.Equal(t, "", got, "unsupported model should return empty")
}

func TestResolveAccountUpstreamModel_NonAntigravity(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformAnthropic,
	}
	got := resolveAccountUpstreamModel(account, "claude-sonnet-4-6")
	require.Equal(t, "claude-sonnet-4-6", got, "no mapping = passthrough")
}

// --- checkChannelPricingRestriction ---

func TestCheckChannelPricingRestriction_NilGroupID(t *testing.T) {
	t.Parallel()
	svc := &GatewayService{channelService: &ChannelService{}}
	require.False(t, svc.checkChannelPricingRestriction(context.Background(), nil, "claude-sonnet-4"))
}

func TestCheckChannelPricingRestriction_NilChannelService(t *testing.T) {
	t.Parallel()
	svc := &GatewayService{}
	gid := int64(10)
	require.False(t, svc.checkChannelPricingRestriction(context.Background(), &gid, "claude-sonnet-4"))
}

func TestCheckChannelPricingRestriction_EmptyModel(t *testing.T) {
	t.Parallel()
	svc := &GatewayService{channelService: &ChannelService{}}
	gid := int64(10)
	require.False(t, svc.checkChannelPricingRestriction(context.Background(), &gid, ""))
}

func TestCheckChannelPricingRestriction_ChannelMapped_Restricted(t *testing.T) {
	t.Parallel()
	// → claude-sonnet-4-6，
	ch := Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelPricing: []ChannelModelPricing{
			{Platform: "anthropic", Models: []string{"claude-opus-4-6"}},
		},
		ModelMapping: map[string]map[string]string{
			"anthropic": {"claude-sonnet-4-5": "claude-sonnet-4-6"},
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{10: "anthropic"}))
	svc := &GatewayService{channelService: channelSvc}

	gid := int64(10)
	require.True(t, svc.checkChannelPricingRestriction(context.Background(), &gid, "claude-sonnet-4-5"),
		"mapped model claude-sonnet-4-6 is NOT in pricing → restricted")
}

func TestCheckChannelPricingRestriction_ChannelMapped_Allowed(t *testing.T) {
	t.Parallel()
	// → claude-sonnet-4-6，
	ch := Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelPricing: []ChannelModelPricing{
			{Platform: "anthropic", Models: []string{"claude-sonnet-4-6"}},
		},
		ModelMapping: map[string]map[string]string{
			"anthropic": {"claude-sonnet-4-5": "claude-sonnet-4-6"},
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{10: "anthropic"}))
	svc := &GatewayService{channelService: channelSvc}

	gid := int64(10)
	require.False(t, svc.checkChannelPricingRestriction(context.Background(), &gid, "claude-sonnet-4-5"),
		"mapped model claude-sonnet-4-6 IS in pricing → allowed")
}

func TestCheckChannelPricingRestriction_Requested_Restricted(t *testing.T) {
	t.Parallel()
	// billing_model_source=requested，
	ch := Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceRequested,
		ModelPricing: []ChannelModelPricing{
			{Platform: "anthropic", Models: []string{"claude-sonnet-4-6"}},
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{10: "anthropic"}))
	svc := &GatewayService{channelService: channelSvc}

	gid := int64(10)
	require.True(t, svc.checkChannelPricingRestriction(context.Background(), &gid, "claude-sonnet-4-5"),
		"requested model claude-sonnet-4-5 is NOT in pricing → restricted")
}

func TestCheckChannelPricingRestriction_Requested_Allowed(t *testing.T) {
	t.Parallel()
	ch := Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceRequested,
		ModelPricing: []ChannelModelPricing{
			{Platform: "anthropic", Models: []string{"claude-sonnet-4-5"}},
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{10: "anthropic"}))
	svc := &GatewayService{channelService: channelSvc}

	gid := int64(10)
	require.False(t, svc.checkChannelPricingRestriction(context.Background(), &gid, "claude-sonnet-4-5"),
		"requested model IS in pricing → allowed")
}

func TestCheckChannelPricingRestriction_Upstream_SkipsPreCheck(t *testing.T) {
	t.Parallel()
	// upstream
	ch := Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceUpstream,
		ModelPricing: []ChannelModelPricing{
			{Platform: "anthropic", Models: []string{"claude-opus-4-6"}},
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{10: "anthropic"}))
	svc := &GatewayService{channelService: channelSvc}

	gid := int64(10)
	require.False(t, svc.checkChannelPricingRestriction(context.Background(), &gid, "unknown-model"),
		"upstream mode should skip pre-check (per-account check needed)")
}

func TestCheckChannelPricingRestriction_RestrictModelsDisabled(t *testing.T) {
	t.Parallel()
	ch := Channel{
		ID:             1,
		Status:         StatusActive,
		GroupIDs:       []int64{10},
		RestrictModels: false, // 未开启model限制
		ModelPricing: []ChannelModelPricing{
			{Platform: "anthropic", Models: []string{"claude-opus-4-6"}},
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{10: "anthropic"}))
	svc := &GatewayService{channelService: channelSvc}

	gid := int64(10)
	require.False(t, svc.checkChannelPricingRestriction(context.Background(), &gid, "any-model"),
		"RestrictModels=false → always allowed")
}

func TestCheckChannelPricingRestriction_NoChannel(t *testing.T) {
	t.Parallel()
	repo := &mockChannelRepository{
		listAllFn: func(_ context.Context) ([]Channel, error) { return nil, nil },
	}
	channelSvc := newTestChannelService(repo)
	svc := &GatewayService{channelService: channelSvc}

	gid := int64(999)
	require.False(t, svc.checkChannelPricingRestriction(context.Background(), &gid, "any-model"),
		"no channel for group → allowed")
}

// --- isUpstreamModelRestrictedByChannel ---

func TestIsUpstreamModelRestrictedByChannel_Restricted(t *testing.T) {
	t.Parallel()
	ch := Channel{
		ID:             1,
		Status:         StatusActive,
		GroupIDs:       []int64{10},
		RestrictModels: true,
		ModelPricing: []ChannelModelPricing{
			{Platform: "anthropic", Models: []string{"claude-opus-4-6"}},
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{10: "anthropic"}))
	svc := &GatewayService{channelService: channelSvc}

	account := &Account{Platform: PlatformAntigravity}
	// claude-sonnet-4-6
	//
	require.True(t, svc.isUpstreamModelRestrictedByChannel(context.Background(), 10, account, "claude-sonnet-4-6"),
		"upstream model claude-sonnet-4-6 NOT in pricing → restricted")
}

func TestIsUpstreamModelRestrictedByChannel_Allowed(t *testing.T) {
	t.Parallel()
	ch := Channel{
		ID:             1,
		Status:         StatusActive,
		GroupIDs:       []int64{10},
		RestrictModels: true,
		ModelPricing: []ChannelModelPricing{
			{Platform: "anthropic", Models: []string{"claude-sonnet-4-6"}},
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{10: "anthropic"}))
	svc := &GatewayService{channelService: channelSvc}

	account := &Account{Platform: PlatformAntigravity}
	require.False(t, svc.isUpstreamModelRestrictedByChannel(context.Background(), 10, account, "claude-sonnet-4-6"),
		"upstream model claude-sonnet-4-6 IS in pricing → allowed")
}

func TestIsUpstreamModelRestrictedByChannel_UnsupportedModel(t *testing.T) {
	t.Parallel()
	ch := Channel{
		ID:             1,
		Status:         StatusActive,
		GroupIDs:       []int64{10},
		RestrictModels: true,
		ModelPricing: []ChannelModelPricing{
			{Platform: "anthropic", Models: []string{"claude-opus-4-6"}},
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{10: "anthropic"}))
	svc := &GatewayService{channelService: channelSvc}

	account := &Account{Platform: PlatformAntigravity}
	// totally-unknown-model →
	require.False(t, svc.isUpstreamModelRestrictedByChannel(context.Background(), 10, account, "totally-unknown-model"),
		"unmappable model → upstream model empty → not restricted (account filter handles this)")
}
