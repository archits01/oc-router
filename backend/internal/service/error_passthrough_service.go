package service

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// ErrorPassthroughRepository
type ErrorPassthroughRepository interface {
	// List
	List(ctx context.Context) ([]*model.ErrorPassthroughRule, error)
	// GetByID
	GetByID(ctx context.Context, id int64) (*model.ErrorPassthroughRule, error)
	// Create
	Create(ctx context.Context, rule *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error)
	// Update
	Update(ctx context.Context, rule *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error)
	// Delete
	Delete(ctx context.Context, id int64) error
}

// ErrorPassthroughCache
type ErrorPassthroughCache interface {
	// Get
	Get(ctx context.Context) ([]*model.ErrorPassthroughRule, bool)
	// Set
	Set(ctx context.Context, rules []*model.ErrorPassthroughRule) error
	// Invalidate
	Invalidate(ctx context.Context) error
	// NotifyUpdate
	NotifyUpdate(ctx context.Context) error
	// SubscribeUpdates
	SubscribeUpdates(ctx context.Context, handler func())
}

// ErrorPassthroughService
type ErrorPassthroughService struct {
	repo  ErrorPassthroughRepository
	cache ErrorPassthroughCache

	localCache   []*cachedPassthroughRule
	localCacheMu sync.RWMutex
}

// cachedPassthroughRule
type cachedPassthroughRule struct {
	*model.ErrorPassthroughRule
	lowerKeywords  []string         // 预计算的小写关键词
	lowerPlatforms []string         // 预计算的小写平台
	errorCodeSet   map[int]struct{} // 预计算的 error code set
}

const maxBodyMatchLen = 8 << 10 // 8KB，errorinfo不会在 8KB 之后才出现

// NewErrorPassthroughService
func NewErrorPassthroughService(
	repo ErrorPassthroughRepository,
	cache ErrorPassthroughCache,
) *ErrorPassthroughService {
	svc := &ErrorPassthroughService{
		repo:  repo,
		cache: cache,
	}

	ctx := context.Background()
	if err := svc.reloadRulesFromDB(ctx); err != nil {
		logger.LegacyPrintf("service.error_passthrough", "[ErrorPassthroughService] Failed to load rules from DB on startup: %v", err)
		if fallbackErr := svc.refreshLocalCache(ctx); fallbackErr != nil {
			logger.LegacyPrintf("service.error_passthrough", "[ErrorPassthroughService] Failed to load rules from cache fallback on startup: %v", fallbackErr)
		}
	}

	if cache != nil {
		cache.SubscribeUpdates(ctx, func() {
			if err := svc.refreshLocalCache(context.Background()); err != nil {
				logger.LegacyPrintf("service.error_passthrough", "[ErrorPassthroughService] Failed to refresh cache on notification: %v", err)
			}
		})
	}

	return svc
}

// List
func (s *ErrorPassthroughService) List(ctx context.Context) ([]*model.ErrorPassthroughRule, error) {
	return s.repo.List(ctx)
}

// GetByID
func (s *ErrorPassthroughService) GetByID(ctx context.Context, id int64) (*model.ErrorPassthroughRule, error) {
	return s.repo.GetByID(ctx, id)
}

// Create
func (s *ErrorPassthroughService) Create(ctx context.Context, rule *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error) {
	if err := rule.Validate(); err != nil {
		return nil, err
	}

	created, err := s.repo.Create(ctx, rule)
	if err != nil {
		return nil, err
	}

	refreshCtx, cancel := s.newCacheRefreshContext()
	defer cancel()
	s.invalidateAndNotify(refreshCtx)

	return created, nil
}

// Update
func (s *ErrorPassthroughService) Update(ctx context.Context, rule *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error) {
	if err := rule.Validate(); err != nil {
		return nil, err
	}

	updated, err := s.repo.Update(ctx, rule)
	if err != nil {
		return nil, err
	}

	refreshCtx, cancel := s.newCacheRefreshContext()
	defer cancel()
	s.invalidateAndNotify(refreshCtx)

	return updated, nil
}

// Delete
func (s *ErrorPassthroughService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}

	refreshCtx, cancel := s.newCacheRefreshContext()
	defer cancel()
	s.invalidateAndNotify(refreshCtx)

	return nil
}

// MatchRule
//
func (s *ErrorPassthroughService) MatchRule(platform string, statusCode int, body []byte) *model.ErrorPassthroughRule {
	rules := s.getCachedRules()
	if len(rules) == 0 {
		return nil
	}

	lowerPlatform := strings.ToLower(platform)
	var bodyLower string // 延迟initialization，只在需要关键词匹配时计算
	var bodyLowerDone bool

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if !s.platformMatchesCached(rule, lowerPlatform) {
			continue
		}
		if s.ruleMatchesOptimized(rule, statusCode, body, &bodyLower, &bodyLowerDone) {
			return rule.ErrorPassthroughRule
		}
	}

	return nil
}

// getCachedRules
func (s *ErrorPassthroughService) getCachedRules() []*cachedPassthroughRule {
	s.localCacheMu.RLock()
	rules := s.localCache
	s.localCacheMu.RUnlock()

	if rules != nil {
		return rules
	}

	ctx := context.Background()
	if err := s.refreshLocalCache(ctx); err != nil {
		logger.LegacyPrintf("service.error_passthrough", "[ErrorPassthroughService] Failed to refresh cache: %v", err)
		return nil
	}

	s.localCacheMu.RLock()
	defer s.localCacheMu.RUnlock()
	return s.localCache
}

// refreshLocalCache
func (s *ErrorPassthroughService) refreshLocalCache(ctx context.Context) error {
	//
	if s.cache != nil {
		if rules, ok := s.cache.Get(ctx); ok {
			s.setLocalCache(rules)
			return nil
		}
	}

	return s.reloadRulesFromDB(ctx)
}

//
//
func (s *ErrorPassthroughService) reloadRulesFromDB(ctx context.Context) error {
	rules, err := s.repo.List(ctx)
	if err != nil {
		return err
	}

	//
	if s.cache != nil {
		if err := s.cache.Set(ctx, rules); err != nil {
			logger.LegacyPrintf("service.error_passthrough", "[ErrorPassthroughService] Failed to set cache: %v", err)
		}
	}

	//
	s.setLocalCache(rules)

	return nil
}

// setLocalCache
func (s *ErrorPassthroughService) setLocalCache(rules []*model.ErrorPassthroughRule) {
	cached := make([]*cachedPassthroughRule, len(rules))
	for i, r := range rules {
		cr := &cachedPassthroughRule{ErrorPassthroughRule: r}
		if len(r.Keywords) > 0 {
			cr.lowerKeywords = make([]string, len(r.Keywords))
			for j, kw := range r.Keywords {
				cr.lowerKeywords[j] = strings.ToLower(kw)
			}
		}
		if len(r.Platforms) > 0 {
			cr.lowerPlatforms = make([]string, len(r.Platforms))
			for j, p := range r.Platforms {
				cr.lowerPlatforms[j] = strings.ToLower(p)
			}
		}
		if len(r.ErrorCodes) > 0 {
			cr.errorCodeSet = make(map[int]struct{}, len(r.ErrorCodes))
			for _, code := range r.ErrorCodes {
				cr.errorCodeSet[code] = struct{}{}
			}
		}
		cached[i] = cr
	}

	sort.Slice(cached, func(i, j int) bool {
		return cached[i].Priority < cached[j].Priority
	})

	s.localCacheMu.Lock()
	s.localCache = cached
	s.localCacheMu.Unlock()
}

// clearLocalCache
func (s *ErrorPassthroughService) clearLocalCache() {
	s.localCacheMu.Lock()
	s.localCache = nil
	s.localCacheMu.Unlock()
}

// newCacheRefreshContext
func (s *ErrorPassthroughService) newCacheRefreshContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 3*time.Second)
}

// invalidateAndNotify
func (s *ErrorPassthroughService) invalidateAndNotify(ctx context.Context) {
	if s.cache != nil {
		if err := s.cache.Invalidate(ctx); err != nil {
			logger.LegacyPrintf("service.error_passthrough", "[ErrorPassthroughService] Failed to invalidate cache: %v", err)
		}
	}

	if err := s.reloadRulesFromDB(ctx); err != nil {
		logger.LegacyPrintf("service.error_passthrough", "[ErrorPassthroughService] Failed to refresh local cache: %v", err)
		s.clearLocalCache()
	}

	if s.cache != nil {
		if err := s.cache.NotifyUpdate(ctx); err != nil {
			logger.LegacyPrintf("service.error_passthrough", "[ErrorPassthroughService] Failed to notify cache update: %v", err)
		}
	}
}

// ensureBodyLower
func ensureBodyLower(body []byte, bodyLower *string, done *bool) string {
	if *done {
		return *bodyLower
	}
	b := body
	if len(b) > maxBodyMatchLen {
		b = b[:maxBodyMatchLen]
	}
	*bodyLower = strings.ToLower(string(b))
	*done = true
	return *bodyLower
}

// platformMatchesCached
func (s *ErrorPassthroughService) platformMatchesCached(rule *cachedPassthroughRule, lowerPlatform string) bool {
	if len(rule.lowerPlatforms) == 0 {
		return true
	}
	for _, p := range rule.lowerPlatforms {
		if p == lowerPlatform {
			return true
		}
	}
	return false
}

// ruleMatchesOptimized
func (s *ErrorPassthroughService) ruleMatchesOptimized(rule *cachedPassthroughRule, statusCode int, body []byte, bodyLower *string, bodyLowerDone *bool) bool {
	hasErrorCodes := len(rule.errorCodeSet) > 0
	hasKeywords := len(rule.lowerKeywords) > 0

	if !hasErrorCodes && !hasKeywords {
		return false
	}

	codeMatch := !hasErrorCodes || s.containsIntSet(rule.errorCodeSet, statusCode)

	if rule.MatchMode == model.MatchModeAll {
		// "all"
		if hasErrorCodes && !codeMatch {
			return false
		}
		if hasKeywords {
			return s.containsAnyKeywordCached(ensureBodyLower(body, bodyLower, bodyLowerDone), rule.lowerKeywords)
		}
		return codeMatch
	}

	// "any"
	if hasErrorCodes && hasKeywords {
		if codeMatch {
			return true
		}
		return s.containsAnyKeywordCached(ensureBodyLower(body, bodyLower, bodyLowerDone), rule.lowerKeywords)
	}
	if hasKeywords {
		return s.containsAnyKeywordCached(ensureBodyLower(body, bodyLower, bodyLowerDone), rule.lowerKeywords)
	}
	return codeMatch
}

// containsIntSet
func (s *ErrorPassthroughService) containsIntSet(set map[int]struct{}, val int) bool {
	_, ok := set[val]
	return ok
}

// containsAnyKeywordCached
func (s *ErrorPassthroughService) containsAnyKeywordCached(bodyLower string, lowerKeywords []string) bool {
	for _, kw := range lowerKeywords {
		if strings.Contains(bodyLower, kw) {
			return true
		}
	}
	return false
}
