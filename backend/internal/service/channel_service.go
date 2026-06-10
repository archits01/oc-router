package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"golang.org/x/sync/singleflight"
)

var (
	ErrChannelNotFound       = infraerrors.NotFound("CHANNEL_NOT_FOUND", "channel not found")
	ErrChannelExists         = infraerrors.Conflict("CHANNEL_EXISTS", "channel name already exists")
	ErrGroupAlreadyInChannel = infraerrors.Conflict(
		"GROUP_ALREADY_IN_CHANNEL",
		"one or more groups already belong to another channel",
	)
)

// ChannelRepository
type ChannelRepository interface {
	Create(ctx context.Context, channel *Channel) error
	GetByID(ctx context.Context, id int64) (*Channel, error)
	Update(ctx context.Context, channel *Channel) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, params pagination.PaginationParams, status, search string) ([]Channel, *pagination.PaginationResult, error)
	ListAll(ctx context.Context) ([]Channel, error)
	ExistsByName(ctx context.Context, name string) (bool, error)
	ExistsByNameExcluding(ctx context.Context, name string, excludeID int64) (bool, error)

	GetGroupIDs(ctx context.Context, channelID int64) ([]int64, error)
	SetGroupIDs(ctx context.Context, channelID int64, groupIDs []int64) error
	GetChannelIDByGroupID(ctx context.Context, groupID int64) (int64, error)
	GetGroupsInOtherChannels(ctx context.Context, channelID int64, groupIDs []int64) ([]int64, error)

	GetGroupPlatforms(ctx context.Context, groupIDs []int64) (map[int64]string, error)

	ListModelPricing(ctx context.Context, channelID int64) ([]ChannelModelPricing, error)
	CreateModelPricing(ctx context.Context, pricing *ChannelModelPricing) error
	UpdateModelPricing(ctx context.Context, pricing *ChannelModelPricing) error
	DeleteModelPricing(ctx context.Context, id int64) error
	ReplaceModelPricing(ctx context.Context, channelID int64, pricingList []ChannelModelPricing) error
}

// channelModelKey
type channelModelKey struct {
	groupID  int64
	platform string // platform identifier
	model    string // lowercase
}

// channelGroupPlatformKey
type channelGroupPlatformKey struct {
	groupID  int64
	platform string
}

// wildcardPricingEntry
type wildcardPricingEntry struct {
	prefix  string
	pricing *ChannelModelPricing
}

// wildcardMappingEntry
type wildcardMappingEntry struct {
	prefix string
	target string
}

// channelCache (1)
type channelCache struct {
	pricingByGroupModel     map[channelModelKey]*ChannelModelPricing            // (groupID, platform, model) -> pricing
	wildcardByGroupPlatform map[channelGroupPlatformKey][]*wildcardPricingEntry // (groupID, platform) -> wildcard pricing (in config order, first match wins)
	mappingByGroupModel     map[channelModelKey]string                          // (groupID, platform, model) -> mapping target
	wildcardMappingByGP     map[channelGroupPlatformKey][]*wildcardMappingEntry // (groupID, platform) -> wildcard mapping (in config order, first match wins)
	channelByGroupID        map[int64]*Channel                                  // groupID -> channel
	groupPlatform           map[int64]string                                    // groupID → platform

	//
	byID     map[int64]*Channel
	loadedAt time.Time
}

// ChannelMappingResult
type ChannelMappingResult struct {
	MappedModel        string // model name after mapping (equals original model name when no mapping)
	ChannelID          int64  // channel ID (0 = no channel association)
	Mapped             bool   // whether mapping occurred
	BillingModelSource string // billing model source ("requested" / "upstream" / "channel_mapped")
}

// BuildModelMappingChain
// reqModel:
// upstreamModel:
func (r ChannelMappingResult) BuildModelMappingChain(reqModel, upstreamModel string) string {
	if !r.Mapped {
		if upstreamModel != "" && upstreamModel != reqModel {
			return reqModel + "→" + upstreamModel
		}
		return ""
	}
	if upstreamModel != "" && upstreamModel != r.MappedModel {
		return reqModel + "→" + r.MappedModel + "→" + upstreamModel
	}
	return reqModel + "→" + r.MappedModel
}

// ToUsageFields
func (r ChannelMappingResult) ToUsageFields(reqModel, upstreamModel string) ChannelUsageFields {
	channelMappedModel := reqModel
	if r.Mapped {
		channelMappedModel = r.MappedModel
	}
	return ChannelUsageFields{
		ChannelID:          r.ChannelID,
		OriginalModel:      reqModel,
		ChannelMappedModel: channelMappedModel,
		BillingModelSource: r.BillingModelSource,
		ModelMappingChain:  r.BuildModelMappingChain(reqModel, upstreamModel),
	}
}

const (
	channelCacheTTL       = 10 * time.Minute
	channelErrorTTL       = 5 * time.Second // short cache TTL on DB error
	channelCacheDBTimeout = 10 * time.Second
)

// ChannelService
type ChannelService struct {
	repo                 ChannelRepository
	groupRepo            GroupRepository
	authCacheInvalidator APIKeyAuthCacheInvalidator
	pricingService       *PricingService // used for fallback to global pricing in "available channels" display; can be nil (test scenarios)

	cache   atomic.Value // *channelCache
	cacheSF singleflight.Group
}

// NewChannelService
// pricingService
//
func NewChannelService(repo ChannelRepository, groupRepo GroupRepository, authCacheInvalidator APIKeyAuthCacheInvalidator, pricingService *PricingService) *ChannelService {
	s := &ChannelService{
		repo:                 repo,
		groupRepo:            groupRepo,
		authCacheInvalidator: authCacheInvalidator,
		pricingService:       pricingService,
	}
	return s
}

// loadCache
func (s *ChannelService) loadCache(ctx context.Context) (*channelCache, error) {
	if cached, ok := s.cache.Load().(*channelCache); ok && cached != nil {
		if time.Since(cached.loadedAt) < channelCacheTTL {
			return cached, nil
		}
	}

	result, err, _ := s.cacheSF.Do("channel_cache", func() (any, error) {
		if cached, ok := s.cache.Load().(*channelCache); ok && cached != nil {
			if time.Since(cached.loadedAt) < channelCacheTTL {
				return cached, nil
			}
		}
		return s.buildCache(ctx)
	})
	if err != nil {
		return nil, err
	}
	cache, ok := result.(*channelCache)
	if !ok {
		return nil, fmt.Errorf("unexpected cache type")
	}
	return cache, nil
}

// newEmptyChannelCache
func newEmptyChannelCache() *channelCache {
	return &channelCache{
		pricingByGroupModel:     make(map[channelModelKey]*ChannelModelPricing),
		wildcardByGroupPlatform: make(map[channelGroupPlatformKey][]*wildcardPricingEntry),
		mappingByGroupModel:     make(map[channelModelKey]string),
		wildcardMappingByGP:     make(map[channelGroupPlatformKey][]*wildcardMappingEntry),
		channelByGroupID:        make(map[int64]*Channel),
		groupPlatform:           make(map[int64]string),
		byID:                    make(map[int64]*Channel),
	}
}

// expandPricingToCache +
//
// ()
func expandPricingToCache(cache *channelCache, ch *Channel, gid int64, platform string) {
	for j := range ch.ModelPricing {
		pricing := &ch.ModelPricing[j]
		if !isPlatformPricingMatch(platform, pricing.Platform) {
			continue // skip pricing for other platforms
		}
		//
		pricingPlatform := pricing.Platform
		gpKey := channelGroupPlatformKey{groupID: gid, platform: pricingPlatform}
		for _, model := range pricing.Models {
			if strings.HasSuffix(model, "*") {
				prefix := strings.ToLower(strings.TrimSuffix(model, "*"))
				cache.wildcardByGroupPlatform[gpKey] = append(cache.wildcardByGroupPlatform[gpKey], &wildcardPricingEntry{
					prefix:  prefix,
					pricing: pricing,
				})
			} else {
				key := channelModelKey{groupID: gid, platform: pricingPlatform, model: strings.ToLower(model)}
				cache.pricingByGroupModel[key] = pricing
			}
		}
	}
}

// expandMappingToCache +
//
func expandMappingToCache(cache *channelCache, ch *Channel, gid int64, platform string) {
	for _, mappingPlatform := range matchingPlatforms(platform) {
		platformMapping, ok := ch.ModelMapping[mappingPlatform]
		if !ok {
			continue
		}
		//
		gpKey := channelGroupPlatformKey{groupID: gid, platform: mappingPlatform}
		for src, dst := range platformMapping {
			if strings.HasSuffix(src, "*") {
				prefix := strings.ToLower(strings.TrimSuffix(src, "*"))
				cache.wildcardMappingByGP[gpKey] = append(cache.wildcardMappingByGP[gpKey], &wildcardMappingEntry{
					prefix: prefix,
					target: dst,
				})
			} else {
				key := channelModelKey{groupID: gid, platform: mappingPlatform, model: strings.ToLower(src)}
				cache.mappingByGroupModel[key] = dst
			}
		}
	}
}

// storeErrorCache
// = channelErrorTTL。
func (s *ChannelService) storeErrorCache() {
	errorCache := newEmptyChannelCache()
	errorCache.loadedAt = time.Now().Add(-(channelCacheTTL - channelErrorTTL))
	s.cache.Store(errorCache)
}

// buildCache
//
func (s *ChannelService) buildCache(ctx context.Context) (*channelCache, error) {
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), channelCacheDBTimeout)
	defer cancel()

	channels, groupPlatforms, err := s.fetchChannelData(dbCtx)
	if err != nil {
		return nil, err
	}

	cache := populateChannelCache(channels, groupPlatforms)
	s.cache.Store(cache)
	return cache, nil
}

// fetchChannelData
func (s *ChannelService) fetchChannelData(ctx context.Context) ([]Channel, map[int64]string, error) {
	channels, err := s.repo.ListAll(ctx)
	if err != nil {
		slog.Warn("failed to build channel cache", "error", err)
		s.storeErrorCache()
		return nil, nil, fmt.Errorf("list all channels: %w", err)
	}

	var allGroupIDs []int64
	for i := range channels {
		allGroupIDs = append(allGroupIDs, channels[i].GroupIDs...)
	}

	groupPlatforms := make(map[int64]string)
	if len(allGroupIDs) > 0 {
		groupPlatforms, err = s.repo.GetGroupPlatforms(ctx, allGroupIDs)
		if err != nil {
			slog.Warn("failed to load group platforms for channel cache", "error", err)
			s.storeErrorCache()
			return nil, nil, fmt.Errorf("get group platforms: %w", err)
		}
	}
	return channels, groupPlatforms, nil
}

// populateChannelCache
//
// （gateway routing / billing /
// ""
func populateChannelCache(channels []Channel, groupPlatforms map[int64]string) *channelCache {
	cache := newEmptyChannelCache()
	cache.groupPlatform = groupPlatforms
	cache.byID = make(map[int64]*Channel, len(channels))
	cache.loadedAt = time.Now()

	for i := range channels {
		channels[i].normalizeBillingModelSource()
		ch := &channels[i]
		cache.byID[ch.ID] = ch
		for _, gid := range ch.GroupIDs {
			cache.channelByGroupID[gid] = ch
			platform := groupPlatforms[gid]
			expandPricingToCache(cache, ch, gid, platform)
			expandMappingToCache(cache, ch, gid, platform)
		}
	}

	return cache
}

// invalidateCache

// isPlatformPricingMatch
//
func isPlatformPricingMatch(groupPlatform, pricingPlatform string) bool {
	return groupPlatform == pricingPlatform
}

// matchingPlatforms
func matchingPlatforms(groupPlatform string) []string {
	return []string{groupPlatform}
}
func (s *ChannelService) invalidateCache() {
	s.cache.Store((*channelCache)(nil))
	s.cacheSF.Forget("channel_cache")

	//
	if _, err := s.buildCache(context.Background()); err != nil {
		slog.Warn("failed to rebuild channel cache after invalidation", "error", err)
	}
}

// matchWildcard
func (c *channelCache) matchWildcard(groupID int64, platform, modelLower string) *ChannelModelPricing {
	gpKey := channelGroupPlatformKey{groupID: groupID, platform: platform}
	wildcards := c.wildcardByGroupPlatform[gpKey]
	for _, wc := range wildcards {
		if strings.HasPrefix(modelLower, wc.prefix) {
			return wc.pricing
		}
	}
	return nil
}

// matchWildcardMapping
func (c *channelCache) matchWildcardMapping(groupID int64, platform, modelLower string) string {
	gpKey := channelGroupPlatformKey{groupID: groupID, platform: platform}
	wildcards := c.wildcardMappingByGP[gpKey]
	for _, wc := range wildcards {
		if strings.HasPrefix(modelLower, wc.prefix) {
			return wc.target
		}
	}
	return ""
}

// lookupPricingAcrossPlatforms
func lookupPricingAcrossPlatforms(cache *channelCache, groupID int64, groupPlatform, modelLower string) *ChannelModelPricing {
	for _, p := range matchingPlatforms(groupPlatform) {
		key := channelModelKey{groupID: groupID, platform: p, model: modelLower}
		if pricing, ok := cache.pricingByGroupModel[key]; ok {
			return pricing
		}
	}
	for _, p := range matchingPlatforms(groupPlatform) {
		if pricing := cache.matchWildcard(groupID, p, modelLower); pricing != nil {
			return pricing
		}
	}
	return nil
}

// lookupMappingAcrossPlatforms
//
func lookupMappingAcrossPlatforms(cache *channelCache, groupID int64, groupPlatform, modelLower string) string {
	for _, p := range matchingPlatforms(groupPlatform) {
		key := channelModelKey{groupID: groupID, platform: p, model: modelLower}
		if mapped, ok := cache.mappingByGroupModel[key]; ok {
			return mapped
		}
	}
	for _, p := range matchingPlatforms(groupPlatform) {
		if mapped := cache.matchWildcardMapping(groupID, p, modelLower); mapped != "" {
			return mapped
		}
	}
	return ""
}

// GetChannelForGroup (1)）
func (s *ChannelService) GetChannelForGroup(ctx context.Context, groupID int64) (*Channel, error) {
	cache, err := s.loadCache(ctx)
	if err != nil {
		return nil, err
	}

	ch, ok := cache.channelByGroupID[groupID]
	if !ok || !ch.IsActive() {
		return nil, nil
	}

	return ch.Clone(), nil
}

// GetGroupPlatform
func (s *ChannelService) GetGroupPlatform(ctx context.Context, groupID int64) string {
	cache, err := s.loadCache(ctx)
	if err != nil {
		return ""
	}
	return cache.groupPlatform[groupID]
}

// channelLookup
type channelLookup struct {
	cache    *channelCache
	channel  *Channel
	platform string
}

// lookupGroupChannel
// ==nil !=nil
func (s *ChannelService) lookupGroupChannel(ctx context.Context, groupID int64) (*channelLookup, error) {
	cache, err := s.loadCache(ctx)
	if err != nil {
		return nil, err
	}
	ch, ok := cache.channelByGroupID[groupID]
	if !ok || !ch.IsActive() {
		return nil, nil
	}
	return &channelLookup{
		cache:    cache,
		channel:  ch,
		platform: cache.groupPlatform[groupID],
	}, nil
}

// GetChannelModelPricing +(1)）。
func (s *ChannelService) GetChannelModelPricing(ctx context.Context, groupID int64, model string) *ChannelModelPricing {
	lk, err := s.lookupGroupChannel(ctx, groupID)
	if err != nil {
		slog.Warn("failed to load channel cache", "group_id", groupID, "error", err)
		return nil
	}
	if lk == nil {
		return nil
	}

	modelLower := strings.ToLower(model)
	pricing := lookupPricingAcrossPlatforms(lk.cache, groupID, lk.platform, modelLower)
	if pricing == nil {
		return nil
	}

	cp := pricing.Clone()
	return &cp
}

// ResolveChannelMapping (1)）
func (s *ChannelService) ResolveChannelMapping(ctx context.Context, groupID int64, model string) ChannelMappingResult {
	lk, err := s.lookupGroupChannel(ctx, groupID)
	if err != nil {
		slog.Warn("failed to load channel cache for mapping", "group_id", groupID, "error", err)
	}
	if lk == nil {
		return ChannelMappingResult{MappedModel: model}
	}
	return resolveMapping(lk, groupID, model)
}

// IsModelRestricted
//
//
func (s *ChannelService) IsModelRestricted(ctx context.Context, groupID int64, model string) bool {
	lk, err := s.lookupGroupChannel(ctx, groupID)
	if err != nil {
		slog.Warn("failed to load channel cache for model restriction check", "group_id", groupID, "error", err)
	}
	if lk == nil {
		return false
	}
	return checkRestricted(lk, groupID, model)
}

// ResolveChannelMappingAndRestrict
//
// restricted
func (s *ChannelService) ResolveChannelMappingAndRestrict(ctx context.Context, groupID *int64, model string) (ChannelMappingResult, bool) {
	if groupID == nil {
		return ChannelMappingResult{MappedModel: model}, false
	}
	lk, _ := s.lookupGroupChannel(ctx, *groupID)
	if lk == nil {
		return ChannelMappingResult{MappedModel: model}, false
	}
	return resolveMapping(lk, *groupID, model), false
}

// resolveMapping
// antigravity
func resolveMapping(lk *channelLookup, groupID int64, model string) ChannelMappingResult {
	// lk.channel
	result := ChannelMappingResult{
		MappedModel:        model,
		ChannelID:          lk.channel.ID,
		BillingModelSource: lk.channel.BillingModelSource,
	}

	modelLower := strings.ToLower(model)
	if mapped := lookupMappingAcrossPlatforms(lk.cache, groupID, lk.platform, modelLower); mapped != "" {
		result.MappedModel = mapped
		result.Mapped = true
	}

	return result
}

// checkRestricted
func checkRestricted(lk *channelLookup, groupID int64, model string) bool {
	if !lk.channel.RestrictModels {
		return false
	}
	modelLower := strings.ToLower(model)
	if lookupPricingAcrossPlatforms(lk.cache, groupID, lk.platform, modelLower) != nil {
		return false
	}
	return true
}

// ReplaceModelInBody
func ReplaceModelInBody(body []byte, newModel string) []byte {
	if len(body) == 0 {
		return body
	}
	if current := gjson.GetBytes(body, "model"); current.Exists() && current.String() == newModel {
		return body
	}
	newBody, err := sjson.SetBytes(body, "model", newModel)
	if err != nil {
		return body
	}
	return newBody
}

// RemovePreviousResponseIDFromBody
func RemovePreviousResponseIDFromBody(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	if !gjson.GetBytes(body, "previous_response_id").Exists() {
		return body
	}
	newBody, err := sjson.DeleteBytes(body, "previous_response_id")
	if err != nil {
		return body
	}
	return newBody
}

// validateChannelConfig + +
// Create
func validateChannelConfig(pricing []ChannelModelPricing, mapping map[string]map[string]string) error {
	if err := validatePricingEntries(pricing); err != nil {
		return err
	}
	return validateNoConflictingMappings(mapping)
}

// validatePricingEntries + +
//
func validatePricingEntries(pricing []ChannelModelPricing) error {
	if err := validateNoConflictingModels(pricing); err != nil {
		return err
	}
	if err := validatePricingIntervals(pricing); err != nil {
		return err
	}
	return validatePricingBillingMode(pricing)
}

// validatePricingBillingMode
func validatePricingBillingMode(pricing []ChannelModelPricing) error {
	for _, p := range pricing {
		if err := checkBillingModeRequirements(p); err != nil {
			return err
		}
		if err := checkPricesNotNegative(p); err != nil {
			return err
		}
		if err := checkIntervalsHavePrices(p); err != nil {
			return err
		}
	}
	return nil
}

func checkBillingModeRequirements(p ChannelModelPricing) error {
	if p.BillingMode == BillingModePerRequest || p.BillingMode == BillingModeImage {
		if p.PerRequestPrice == nil && len(p.Intervals) == 0 {
			return infraerrors.BadRequest(
				"BILLING_MODE_MISSING_PRICE",
				"per-request price or intervals required for per_request/image billing mode",
			)
		}
	}
	return nil
}

func checkPricesNotNegative(p ChannelModelPricing) error {
	checks := []struct {
		field string
		val   *float64
	}{
		{"input_price", p.InputPrice},
		{"output_price", p.OutputPrice},
		{"cache_write_price", p.CacheWritePrice},
		{"cache_read_price", p.CacheReadPrice},
		{"image_output_price", p.ImageOutputPrice},
		{"per_request_price", p.PerRequestPrice},
	}
	for _, c := range checks {
		if c.val != nil && *c.val < 0 {
			return infraerrors.BadRequest("NEGATIVE_PRICE", fmt.Sprintf("%s must be >= 0", c.field))
		}
	}
	return nil
}

func checkIntervalsHavePrices(p ChannelModelPricing) error {
	for _, iv := range p.Intervals {
		if iv.InputPrice == nil && iv.OutputPrice == nil &&
			iv.CacheWritePrice == nil && iv.CacheReadPrice == nil &&
			iv.PerRequestPrice == nil {
			return infraerrors.BadRequest(
				"INTERVAL_MISSING_PRICE",
				fmt.Sprintf("interval [%d, %s] has no price fields set for model %v",
					iv.MinTokens, formatMaxTokens(iv.MaxTokens), p.Models),
			)
		}
	}
	return nil
}

func formatMaxTokens(max *int) string {
	if max == nil {
		return "∞"
	}
	return fmt.Sprintf("%d", *max)
}

// --- CRUD ---

// Create
func (s *ChannelService) Create(ctx context.Context, input *CreateChannelInput) (*Channel, error) {
	exists, err := s.repo.ExistsByName(ctx, input.Name)
	if err != nil {
		return nil, fmt.Errorf("check channel exists: %w", err)
	}
	if exists {
		return nil, ErrChannelExists
	}

	if err := s.checkGroupConflicts(ctx, 0, input.GroupIDs); err != nil {
		return nil, err
	}

	channel := &Channel{
		Name:                       input.Name,
		Description:                input.Description,
		Status:                     StatusActive,
		BillingModelSource:         input.BillingModelSource,
		RestrictModels:             input.RestrictModels,
		GroupIDs:                   input.GroupIDs,
		ModelPricing:               input.ModelPricing,
		ModelMapping:               input.ModelMapping,
		Features:                   input.Features,
		FeaturesConfig:             input.FeaturesConfig,
		ApplyPricingToAccountStats: input.ApplyPricingToAccountStats,
		AccountStatsPricingRules:   input.AccountStatsPricingRules,
	}
	channel.normalizeBillingModelSource()

	if err := validateChannelConfig(channel.ModelPricing, channel.ModelMapping); err != nil {
		return nil, err
	}
	for i, rule := range channel.AccountStatsPricingRules {
		if err := validatePricingEntries(rule.Pricing); err != nil {
			return nil, fmt.Errorf("account stats pricing rule #%d: %w", i+1, err)
		}
	}

	if err := s.repo.Create(ctx, channel); err != nil {
		return nil, fmt.Errorf("create channel: %w", err)
	}

	s.invalidateCache()
	created, err := s.repo.GetByID(ctx, channel.ID)
	if err != nil {
		return nil, err
	}
	created.normalizeBillingModelSource()
	return created, nil
}

// GetByID
//
func (s *ChannelService) GetByID(ctx context.Context, id int64) (*Channel, error) {
	ch, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	ch.normalizeBillingModelSource()
	return ch, nil
}

// Update
func (s *ChannelService) Update(ctx context.Context, id int64, input *UpdateChannelInput) (*Channel, error) {
	channel, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}

	if err := s.applyUpdateInput(ctx, channel, input); err != nil {
		return nil, err
	}

	if err := validateChannelConfig(channel.ModelPricing, channel.ModelMapping); err != nil {
		return nil, err
	}
	for i, rule := range channel.AccountStatsPricingRules {
		if err := validatePricingEntries(rule.Pricing); err != nil {
			return nil, fmt.Errorf("account stats pricing rule #%d: %w", i+1, err)
		}
	}

	oldGroupIDs := s.getOldGroupIDs(ctx, id)

	if err := s.repo.Update(ctx, channel); err != nil {
		return nil, fmt.Errorf("update channel: %w", err)
	}

	s.invalidateCache()
	s.invalidateAuthCacheForGroups(ctx, oldGroupIDs, channel.GroupIDs)

	updated, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	updated.normalizeBillingModelSource()
	return updated, nil
}

// applyUpdateInput
func (s *ChannelService) applyUpdateInput(ctx context.Context, channel *Channel, input *UpdateChannelInput) error {
	if input.Name != "" && input.Name != channel.Name {
		exists, err := s.repo.ExistsByNameExcluding(ctx, input.Name, channel.ID)
		if err != nil {
			return fmt.Errorf("check channel exists: %w", err)
		}
		if exists {
			return ErrChannelExists
		}
		channel.Name = input.Name
	}
	if input.Description != nil {
		channel.Description = *input.Description
	}
	if input.Status != "" {
		channel.Status = input.Status
	}
	if input.RestrictModels != nil {
		channel.RestrictModels = *input.RestrictModels
	}
	if input.Features != nil {
		channel.Features = *input.Features
	}
	if input.GroupIDs != nil {
		if err := s.checkGroupConflicts(ctx, channel.ID, *input.GroupIDs); err != nil {
			return err
		}
		channel.GroupIDs = *input.GroupIDs
	}
	if input.ModelPricing != nil {
		channel.ModelPricing = *input.ModelPricing
	}
	if input.ModelMapping != nil {
		channel.ModelMapping = input.ModelMapping
	}
	if input.BillingModelSource != "" {
		channel.BillingModelSource = input.BillingModelSource
	}
	if input.FeaturesConfig != nil {
		channel.FeaturesConfig = input.FeaturesConfig
	}
	if input.ApplyPricingToAccountStats != nil {
		channel.ApplyPricingToAccountStats = *input.ApplyPricingToAccountStats
	}
	if input.AccountStatsPricingRules != nil {
		channel.AccountStatsPricingRules = *input.AccountStatsPricingRules
	}
	return nil
}

// checkGroupConflicts
// channelID
func (s *ChannelService) checkGroupConflicts(ctx context.Context, channelID int64, groupIDs []int64) error {
	if len(groupIDs) == 0 {
		return nil
	}
	conflicting, err := s.repo.GetGroupsInOtherChannels(ctx, channelID, groupIDs)
	if err != nil {
		return fmt.Errorf("check group conflicts: %w", err)
	}
	if len(conflicting) > 0 {
		return ErrGroupAlreadyInChannel
	}
	return nil
}

// getOldGroupIDs
func (s *ChannelService) getOldGroupIDs(ctx context.Context, channelID int64) []int64 {
	if s.authCacheInvalidator == nil {
		return nil
	}
	oldGroupIDs, err := s.repo.GetGroupIDs(ctx, channelID)
	if err != nil {
		slog.Warn("failed to get old group IDs for cache invalidation", "channel_id", channelID, "error", err)
	}
	return oldGroupIDs
}

// invalidateAuthCacheForGroups
func (s *ChannelService) invalidateAuthCacheForGroups(ctx context.Context, groupIDSets ...[]int64) {
	if s.authCacheInvalidator == nil {
		return
	}
	seen := make(map[int64]struct{})
	for _, ids := range groupIDSets {
		for _, gid := range ids {
			if _, ok := seen[gid]; ok {
				continue
			}
			seen[gid] = struct{}{}
			s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, gid)
		}
	}
}

// Delete
func (s *ChannelService) Delete(ctx context.Context, id int64) error {
	groupIDs, err := s.repo.GetGroupIDs(ctx, id)
	if err != nil {
		slog.Warn("failed to get group IDs before delete", "channel_id", id, "error", err)
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}

	s.invalidateCache()
	s.invalidateAuthCacheForGroups(ctx, groupIDs)

	return nil
}

// List
func (s *ChannelService) List(ctx context.Context, params pagination.PaginationParams, status, search string) ([]Channel, *pagination.PaginationResult, error) {
	channels, res, err := s.repo.List(ctx, params, status, search)
	if err != nil {
		return nil, nil, err
	}
	for i := range channels {
		channels[i].normalizeBillingModelSource()
	}
	return channels, res, nil
}

// modelEntry
type modelEntry struct {
	pattern  string // original pattern (e.g. "claude-*" or "claude-opus-4")
	prefix   string // lowercase prefix (wildcard with * removed, exact name kept as-is)
	wildcard bool
}

// conflictsBetween
func conflictsBetween(a, b modelEntry) bool {
	switch {
	case !a.wildcard && !b.wildcard:
		return a.prefix == b.prefix
	case a.wildcard && !b.wildcard:
		return strings.HasPrefix(b.prefix, a.prefix)
	case !a.wildcard && b.wildcard:
		return strings.HasPrefix(a.prefix, b.prefix)
	default:
		return strings.HasPrefix(a.prefix, b.prefix) ||
			strings.HasPrefix(b.prefix, a.prefix)
	}
}

// toModelEntry
func toModelEntry(pattern string) modelEntry {
	prefix, isWild := splitWildcardSuffix(strings.ToLower(pattern))
	return modelEntry{pattern: pattern, prefix: prefix, wildcard: isWild}
}

// validateNoConflictingModels
func validateNoConflictingModels(pricingList []ChannelModelPricing) error {
	byPlatform := make(map[string][]modelEntry)
	for _, p := range pricingList {
		for _, model := range p.Models {
			byPlatform[p.Platform] = append(byPlatform[p.Platform], toModelEntry(model))
		}
	}
	for platform, entries := range byPlatform {
		if err := detectConflicts(entries, platform, "MODEL_PATTERN_CONFLICT", "model patterns"); err != nil {
			return err
		}
	}
	return nil
}

// validateNoConflictingMappings
func validateNoConflictingMappings(mapping map[string]map[string]string) error {
	for platform, platformMapping := range mapping {
		entries := make([]modelEntry, 0, len(platformMapping))
		for src := range platformMapping {
			entries = append(entries, toModelEntry(src))
		}
		if err := detectConflicts(entries, platform, "MAPPING_PATTERN_CONFLICT", "mapping source patterns"); err != nil {
			return err
		}
	}
	return nil
}

func validatePricingIntervals(pricingList []ChannelModelPricing) error {
	for _, pricing := range pricingList {
		if err := ValidateIntervals(pricing.Intervals, pricing.BillingMode); err != nil {
			return infraerrors.BadRequest(
				"INVALID_PRICING_INTERVALS",
				fmt.Sprintf("invalid pricing intervals for platform '%s' models %v: %v",
					pricing.Platform, pricing.Models, err),
			)
		}
	}
	return nil
}

// detectConflicts
func detectConflicts(entries []modelEntry, platform, errCode, label string) error {
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if conflictsBetween(entries[i], entries[j]) {
				return infraerrors.BadRequest(errCode,
					fmt.Sprintf("%s '%s' and '%s' conflict in platform '%s': overlapping match range",
						label, entries[i].pattern, entries[j].pattern, platform))
			}
		}
	}
	return nil
}

// --- Input types ---

// CreateChannelInput
type CreateChannelInput struct {
	Name                       string
	Description                string
	GroupIDs                   []int64
	ModelPricing               []ChannelModelPricing
	ModelMapping               map[string]map[string]string // platform → {src→dst}
	BillingModelSource         string
	RestrictModels             bool
	Features                   string
	FeaturesConfig             map[string]any
	ApplyPricingToAccountStats bool
	AccountStatsPricingRules   []AccountStatsPricingRule
}

// UpdateChannelInput
type UpdateChannelInput struct {
	Name                       string
	Description                *string
	Status                     string
	GroupIDs                   *[]int64
	ModelPricing               *[]ChannelModelPricing
	ModelMapping               map[string]map[string]string // platform → {src→dst}
	BillingModelSource         string
	RestrictModels             *bool
	Features                   *string
	FeaturesConfig             map[string]any
	ApplyPricingToAccountStats *bool
	AccountStatsPricingRules   *[]AccountStatsPricingRule
}
