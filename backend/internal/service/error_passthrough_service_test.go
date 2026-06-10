//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockErrorPassthroughRepo
type mockErrorPassthroughRepo struct {
	rules     []*model.ErrorPassthroughRule
	listErr   error
	getErr    error
	createErr error
	updateErr error
	deleteErr error
}

type mockErrorPassthroughCache struct {
	rules            []*model.ErrorPassthroughRule
	hasData          bool
	getCalled        int
	setCalled        int
	invalidateCalled int
	notifyCalled     int
}

func newMockErrorPassthroughCache(rules []*model.ErrorPassthroughRule, hasData bool) *mockErrorPassthroughCache {
	return &mockErrorPassthroughCache{
		rules:   cloneRules(rules),
		hasData: hasData,
	}
}

func (m *mockErrorPassthroughCache) Get(ctx context.Context) ([]*model.ErrorPassthroughRule, bool) {
	m.getCalled++
	if !m.hasData {
		return nil, false
	}
	return cloneRules(m.rules), true
}

func (m *mockErrorPassthroughCache) Set(ctx context.Context, rules []*model.ErrorPassthroughRule) error {
	m.setCalled++
	m.rules = cloneRules(rules)
	m.hasData = true
	return nil
}

func (m *mockErrorPassthroughCache) Invalidate(ctx context.Context) error {
	m.invalidateCalled++
	m.rules = nil
	m.hasData = false
	return nil
}

func (m *mockErrorPassthroughCache) NotifyUpdate(ctx context.Context) error {
	m.notifyCalled++
	return nil
}

func (m *mockErrorPassthroughCache) SubscribeUpdates(ctx context.Context, handler func()) {
}

func cloneRules(rules []*model.ErrorPassthroughRule) []*model.ErrorPassthroughRule {
	if rules == nil {
		return nil
	}
	out := make([]*model.ErrorPassthroughRule, len(rules))
	copy(out, rules)
	return out
}

func (m *mockErrorPassthroughRepo) List(ctx context.Context) ([]*model.ErrorPassthroughRule, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.rules, nil
}

func (m *mockErrorPassthroughRepo) GetByID(ctx context.Context, id int64) (*model.ErrorPassthroughRule, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for _, r := range m.rules {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, nil
}

func (m *mockErrorPassthroughRepo) Create(ctx context.Context, rule *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	rule.ID = int64(len(m.rules) + 1)
	m.rules = append(m.rules, rule)
	return rule, nil
}

func (m *mockErrorPassthroughRepo) Update(ctx context.Context, rule *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	for i, r := range m.rules {
		if r.ID == rule.ID {
			m.rules[i] = rule
			return rule, nil
		}
	}
	return rule, nil
}

func (m *mockErrorPassthroughRepo) Delete(ctx context.Context, id int64) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i, r := range m.rules {
		if r.ID == id {
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			return nil
		}
	}
	return nil
}

// newTestService
func newTestService(rules []*model.ErrorPassthroughRule) *ErrorPassthroughService {
	repo := &mockErrorPassthroughRepo{rules: rules}
	svc := &ErrorPassthroughService{
		repo:  repo,
		cache: nil, // do not use cache
	}
	//
	svc.setLocalCache(rules)
	return svc
}

// newCachedRuleForTest
func newCachedRuleForTest(rule *model.ErrorPassthroughRule) *cachedPassthroughRule {
	cr := &cachedPassthroughRule{ErrorPassthroughRule: rule}
	if len(rule.Keywords) > 0 {
		cr.lowerKeywords = make([]string, len(rule.Keywords))
		for j, kw := range rule.Keywords {
			cr.lowerKeywords[j] = strings.ToLower(kw)
		}
	}
	if len(rule.Platforms) > 0 {
		cr.lowerPlatforms = make([]string, len(rule.Platforms))
		for j, p := range rule.Platforms {
			cr.lowerPlatforms[j] = strings.ToLower(p)
		}
	}
	if len(rule.ErrorCodes) > 0 {
		cr.errorCodeSet = make(map[int]struct{}, len(rule.ErrorCodes))
		for _, code := range rule.ErrorCodes {
			cr.errorCodeSet[code] = struct{}{}
		}
	}
	return cr
}

// =============================================================================
//
// =============================================================================

func TestRuleMatches_NoConditions(t *testing.T) {
	svc := newTestService(nil)
	rule := newCachedRuleForTest(&model.ErrorPassthroughRule{
		Enabled:    true,
		ErrorCodes: []int{},
		Keywords:   []string{},
		MatchMode:  model.MatchModeAny,
	})

	var bodyLower string
	var bodyLowerDone bool
	assert.False(t, svc.ruleMatchesOptimized(rule, 422, []byte("some error message"), &bodyLower, &bodyLowerDone),
		"should not match when no conditions are configured")
}

func TestRuleMatches_OnlyErrorCodes_AnyMode(t *testing.T) {
	svc := newTestService(nil)
	rule := newCachedRuleForTest(&model.ErrorPassthroughRule{
		Enabled:    true,
		ErrorCodes: []int{422, 400},
		Keywords:   []string{},
		MatchMode:  model.MatchModeAny,
	})

	tests := []struct {
		name       string
		statusCode int
		body       string
		expected   bool
	}{
		{"status code match 422", 422, "any message", true},
		{"status code match 400", 400, "any message", true},
		{"status code mismatch 500", 500, "any message", false},
		{"status code mismatch 429", 429, "any message", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyLower string
			var bodyLowerDone bool
			result := svc.ruleMatchesOptimized(rule, tt.statusCode, []byte(tt.body), &bodyLower, &bodyLowerDone)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRuleMatches_OnlyKeywords_AnyMode(t *testing.T) {
	svc := newTestService(nil)
	rule := newCachedRuleForTest(&model.ErrorPassthroughRule{
		Enabled:    true,
		ErrorCodes: []int{},
		Keywords:   []string{"context limit", "model not supported"},
		MatchMode:  model.MatchModeAny,
	})

	tests := []struct {
		name       string
		statusCode int
		body       string
		expected   bool
	}{
		{"keyword match context limit", 500, "error: context limit reached", true},
		{"keyword match model not supported", 400, "the model not supported here", true},
		{"keyword mismatch", 422, "some other error", false},
		{"keyword case - auto conversion", 500, "Context Limit exceeded", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyLower string
			var bodyLowerDone bool
			result := svc.ruleMatchesOptimized(rule, tt.statusCode, []byte(tt.body), &bodyLower, &bodyLowerDone)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRuleMatches_BothConditions_AnyMode(t *testing.T) {
	// any
	svc := newTestService(nil)
	rule := newCachedRuleForTest(&model.ErrorPassthroughRule{
		Enabled:    true,
		ErrorCodes: []int{422, 400},
		Keywords:   []string{"context limit"},
		MatchMode:  model.MatchModeAny,
	})

	tests := []struct {
		name       string
		statusCode int
		body       string
		expected   bool
		reason     string
	}{
		{
			name:       "both status code and keyword match",
			statusCode: 422,
			body:       "context limit reached",
			expected:   true,
			reason:     "both match",
		},
		{
			name:       "only status code matches",
			statusCode: 422,
			body:       "some other error",
			expected:   true,
			reason:     "code matches, keyword doesn't - OR mode should match",
		},
		{
			name:       "only keyword matches",
			statusCode: 500,
			body:       "context limit exceeded",
			expected:   true,
			reason:     "keyword matches, code doesn't - OR mode should match",
		},
		{
			name:       "neither matches",
			statusCode: 500,
			body:       "some other error",
			expected:   false,
			reason:     "neither matches",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyLower string
			var bodyLowerDone bool
			result := svc.ruleMatchesOptimized(rule, tt.statusCode, []byte(tt.body), &bodyLower, &bodyLowerDone)
			assert.Equal(t, tt.expected, result, tt.reason)
		})
	}
}

func TestRuleMatches_BothConditions_AllMode(t *testing.T) {
	// all
	svc := newTestService(nil)
	rule := newCachedRuleForTest(&model.ErrorPassthroughRule{
		Enabled:    true,
		ErrorCodes: []int{422, 400},
		Keywords:   []string{"context limit"},
		MatchMode:  model.MatchModeAll,
	})

	tests := []struct {
		name       string
		statusCode int
		body       string
		expected   bool
		reason     string
	}{
		{
			name:       "both status code and keyword match",
			statusCode: 422,
			body:       "context limit reached",
			expected:   true,
			reason:     "both match - AND mode should match",
		},
		{
			name:       "only status code matches",
			statusCode: 422,
			body:       "some other error",
			expected:   false,
			reason:     "code matches but keyword doesn't - AND mode should NOT match",
		},
		{
			name:       "only keyword matches",
			statusCode: 500,
			body:       "context limit exceeded",
			expected:   false,
			reason:     "keyword matches but code doesn't - AND mode should NOT match",
		},
		{
			name:       "neither matches",
			statusCode: 500,
			body:       "some other error",
			expected:   false,
			reason:     "neither matches",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyLower string
			var bodyLowerDone bool
			result := svc.ruleMatchesOptimized(rule, tt.statusCode, []byte(tt.body), &bodyLower, &bodyLowerDone)
			assert.Equal(t, tt.expected, result, tt.reason)
		})
	}
}

// =============================================================================
//
// =============================================================================

func TestPlatformMatches(t *testing.T) {
	svc := newTestService(nil)

	tests := []struct {
		name            string
		rulePlatforms   []string
		requestPlatform string
		expected        bool
	}{
		{
			name:            "empty platform list matches all",
			rulePlatforms:   []string{},
			requestPlatform: "anthropic",
			expected:        true,
		},
		{
			name:            "nil platform list matches all",
			rulePlatforms:   nil,
			requestPlatform: "openai",
			expected:        true,
		},
		{
			name:            "exact match anthropic",
			rulePlatforms:   []string{"anthropic", "openai"},
			requestPlatform: "anthropic",
			expected:        true,
		},
		{
			name:            "exact match openai",
			rulePlatforms:   []string{"anthropic", "openai"},
			requestPlatform: "openai",
			expected:        true,
		},
		{
			name:            "mismatch gemini",
			rulePlatforms:   []string{"anthropic", "openai"},
			requestPlatform: "gemini",
			expected:        false,
		},
		{
			name:            "case insensitive",
			rulePlatforms:   []string{"Anthropic", "OpenAI"},
			requestPlatform: "anthropic",
			expected:        true,
		},
		{
			name:            "match antigravity",
			rulePlatforms:   []string{"antigravity"},
			requestPlatform: "antigravity",
			expected:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := newCachedRuleForTest(&model.ErrorPassthroughRule{
				Platforms: tt.rulePlatforms,
			})
			result := svc.platformMatchesCached(rule, strings.ToLower(tt.requestPlatform))
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
//
// =============================================================================

func TestMatchRule_Priority(t *testing.T) {
	rules := []*model.ErrorPassthroughRule{
		{
			ID:         1,
			Name:       "Low Priority",
			Enabled:    true,
			Priority:   10,
			ErrorCodes: []int{422},
			MatchMode:  model.MatchModeAny,
		},
		{
			ID:         2,
			Name:       "High Priority",
			Enabled:    true,
			Priority:   1,
			ErrorCodes: []int{422},
			MatchMode:  model.MatchModeAny,
		},
	}

	svc := newTestService(rules)
	matched := svc.MatchRule("anthropic", 422, []byte("error"))

	require.NotNil(t, matched)
	assert.Equal(t, int64(2), matched.ID, "should match higher priority (lower number) rule")
	assert.Equal(t, "High Priority", matched.Name)
}

func TestMatchRule_DisabledRule(t *testing.T) {
	rules := []*model.ErrorPassthroughRule{
		{
			ID:         1,
			Name:       "Disabled Rule",
			Enabled:    false,
			Priority:   1,
			ErrorCodes: []int{422},
			MatchMode:  model.MatchModeAny,
		},
		{
			ID:         2,
			Name:       "Enabled Rule",
			Enabled:    true,
			Priority:   10,
			ErrorCodes: []int{422},
			MatchMode:  model.MatchModeAny,
		},
	}

	svc := newTestService(rules)
	matched := svc.MatchRule("anthropic", 422, []byte("error"))

	require.NotNil(t, matched)
	assert.Equal(t, int64(2), matched.ID, "should skip disabled rule")
}

func TestMatchRule_PlatformFilter(t *testing.T) {
	rules := []*model.ErrorPassthroughRule{
		{
			ID:         1,
			Name:       "Anthropic Only",
			Enabled:    true,
			Priority:   1,
			ErrorCodes: []int{422},
			Platforms:  []string{"anthropic"},
			MatchMode:  model.MatchModeAny,
		},
		{
			ID:         2,
			Name:       "OpenAI Only",
			Enabled:    true,
			Priority:   2,
			ErrorCodes: []int{422},
			Platforms:  []string{"openai"},
			MatchMode:  model.MatchModeAny,
		},
		{
			ID:         3,
			Name:       "All Platforms",
			Enabled:    true,
			Priority:   3,
			ErrorCodes: []int{422},
			Platforms:  []string{},
			MatchMode:  model.MatchModeAny,
		},
	}

	svc := newTestService(rules)

	t.Run("Anthropic request matches Anthropic rule", func(t *testing.T) {
		matched := svc.MatchRule("anthropic", 422, []byte("error"))
		require.NotNil(t, matched)
		assert.Equal(t, int64(1), matched.ID)
	})

	t.Run("OpenAI request matches OpenAI rule", func(t *testing.T) {
		matched := svc.MatchRule("openai", 422, []byte("error"))
		require.NotNil(t, matched)
		assert.Equal(t, int64(2), matched.ID)
	})

	t.Run("Gemini request matches all-platform rule", func(t *testing.T) {
		matched := svc.MatchRule("gemini", 422, []byte("error"))
		require.NotNil(t, matched)
		assert.Equal(t, int64(3), matched.ID)
	})

	t.Run("Antigravity request matches all-platform rule", func(t *testing.T) {
		matched := svc.MatchRule("antigravity", 422, []byte("error"))
		require.NotNil(t, matched)
		assert.Equal(t, int64(3), matched.ID)
	})
}

func TestMatchRule_NoMatch(t *testing.T) {
	rules := []*model.ErrorPassthroughRule{
		{
			ID:         1,
			Name:       "Rule for 422",
			Enabled:    true,
			Priority:   1,
			ErrorCodes: []int{422},
			MatchMode:  model.MatchModeAny,
		},
	}

	svc := newTestService(rules)
	matched := svc.MatchRule("anthropic", 500, []byte("error"))

	assert.Nil(t, matched, "should return nil when no rules match")
}

func TestMatchRule_EmptyRules(t *testing.T) {
	svc := newTestService([]*model.ErrorPassthroughRule{})
	matched := svc.MatchRule("anthropic", 422, []byte("error"))

	assert.Nil(t, matched, "should return nil when no rules exist")
}

func TestMatchRule_CaseInsensitiveKeyword(t *testing.T) {
	rules := []*model.ErrorPassthroughRule{
		{
			ID:        1,
			Name:      "Context Limit",
			Enabled:   true,
			Priority:  1,
			Keywords:  []string{"Context Limit"},
			MatchMode: model.MatchModeAny,
		},
	}

	svc := newTestService(rules)

	tests := []struct {
		name     string
		body     string
		expected bool
	}{
		{"exact match", "Context Limit reached", true},
		{"lowercase match", "context limit reached", true},
		{"uppercase match", "CONTEXT LIMIT REACHED", true},
		{"mixed case", "ConTeXt LiMiT error", true},
		{"mismatch", "some other error", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := svc.MatchRule("anthropic", 500, []byte(tt.body))
			if tt.expected {
				assert.NotNil(t, matched)
			} else {
				assert.Nil(t, matched)
			}
		})
	}
}

// =============================================================================
// =============================================================================

func TestMatchRule_RealWorldScenario_ContextLimitPassthrough(t *testing.T) {
	// + "context limit has been reached"，
	rules := []*model.ErrorPassthroughRule{
		{
			ID:              1,
			Name:            "Context Limit Passthrough",
			Enabled:         true,
			Priority:        1,
			ErrorCodes:      []int{422},
			Keywords:        []string{"context limit"},
			MatchMode:       model.MatchModeAll, // must satisfy all conditions
			Platforms:       []string{"anthropic", "antigravity"},
			PassthroughCode: true,
			PassthroughBody: true,
		},
	}

	svc := newTestService(rules)

	//
	t.Run("Anthropic 422 with context limit", func(t *testing.T) {
		body := []byte(`{"type":"error","error":{"type":"invalid_request","message":"The context limit has been reached"}}`)
		matched := svc.MatchRule("anthropic", 422, body)
		require.NotNil(t, matched)
		assert.True(t, matched.PassthroughCode)
		assert.True(t, matched.PassthroughBody)
	})

	//
	t.Run("Antigravity 422 with context limit", func(t *testing.T) {
		body := []byte(`{"error":"context limit exceeded"}`)
		matched := svc.MatchRule("antigravity", 422, body)
		require.NotNil(t, matched)
	})

	//
	t.Run("OpenAI should not match", func(t *testing.T) {
		body := []byte(`{"error":"context limit exceeded"}`)
		matched := svc.MatchRule("openai", 422, body)
		assert.Nil(t, matched, "OpenAI is not in the rule platform list")
	})

	t.Run("Wrong status code", func(t *testing.T) {
		body := []byte(`{"error":"context limit exceeded"}`)
		matched := svc.MatchRule("anthropic", 400, body)
		assert.Nil(t, matched, "status code mismatch")
	})

	t.Run("Wrong keyword", func(t *testing.T) {
		body := []byte(`{"error":"rate limit exceeded"}`)
		matched := svc.MatchRule("anthropic", 422, body)
		assert.Nil(t, matched, "keyword mismatch")
	})
}

func TestMatchRule_RealWorldScenario_CustomErrorMessage(t *testing.T) {
	customMsg := "Service temporarily unavailable, please try again later"
	responseCode := 503
	rules := []*model.ErrorPassthroughRule{
		{
			ID:              1,
			Name:            "Hide Internal Errors",
			Enabled:         true,
			Priority:        1,
			ErrorCodes:      []int{500, 502, 503},
			MatchMode:       model.MatchModeAny,
			PassthroughCode: false,
			ResponseCode:    &responseCode,
			PassthroughBody: false,
			CustomMessage:   &customMsg,
		},
	}

	svc := newTestService(rules)

	matched := svc.MatchRule("anthropic", 500, []byte("internal server error"))
	require.NotNil(t, matched)
	assert.False(t, matched.PassthroughCode)
	assert.Equal(t, 503, *matched.ResponseCode)
	assert.False(t, matched.PassthroughBody)
	assert.Equal(t, customMsg, *matched.CustomMessage)
}

// =============================================================================
//
// =============================================================================

func TestErrorPassthroughRule_Validate(t *testing.T) {
	tests := []struct {
		name        string
		rule        *model.ErrorPassthroughRule
		expectError bool
		errorField  string
	}{
		{
			name: "valid rule - passthrough mode (with error codes)",
			rule: &model.ErrorPassthroughRule{
				Name:            "Valid Rule",
				MatchMode:       model.MatchModeAny,
				ErrorCodes:      []int{422},
				PassthroughCode: true,
				PassthroughBody: true,
			},
			expectError: false,
		},
		{
			name: "valid rule - passthrough mode (with keywords)",
			rule: &model.ErrorPassthroughRule{
				Name:            "Valid Rule",
				MatchMode:       model.MatchModeAny,
				Keywords:        []string{"context limit"},
				PassthroughCode: true,
				PassthroughBody: true,
			},
			expectError: false,
		},
		{
			name: "valid rule - custom response",
			rule: &model.ErrorPassthroughRule{
				Name:            "Valid Rule",
				MatchMode:       model.MatchModeAll,
				ErrorCodes:      []int{500},
				Keywords:        []string{"internal error"},
				PassthroughCode: false,
				ResponseCode:    testIntPtr(503),
				PassthroughBody: false,
				CustomMessage:   testStrPtr("Custom error"),
			},
			expectError: false,
		},
		{
			name: "missing name",
			rule: &model.ErrorPassthroughRule{
				Name:            "",
				MatchMode:       model.MatchModeAny,
				ErrorCodes:      []int{422},
				PassthroughCode: true,
				PassthroughBody: true,
			},
			expectError: true,
			errorField:  "name",
		},
		{
			name: "invalid match mode",
			rule: &model.ErrorPassthroughRule{
				Name:            "Invalid Mode",
				MatchMode:       "invalid",
				ErrorCodes:      []int{422},
				PassthroughCode: true,
				PassthroughBody: true,
			},
			expectError: true,
			errorField:  "match_mode",
		},
		{
			name: "missing match conditions (both error codes and keywords empty)",
			rule: &model.ErrorPassthroughRule{
				Name:            "No Conditions",
				MatchMode:       model.MatchModeAny,
				ErrorCodes:      []int{},
				Keywords:        []string{},
				PassthroughCode: true,
				PassthroughBody: true,
			},
			expectError: true,
			errorField:  "conditions",
		},
		{
			name: "missing match conditions (nil slices)",
			rule: &model.ErrorPassthroughRule{
				Name:            "Nil Conditions",
				MatchMode:       model.MatchModeAny,
				ErrorCodes:      nil,
				Keywords:        nil,
				PassthroughCode: true,
				PassthroughBody: true,
			},
			expectError: true,
			errorField:  "conditions",
		},
		{
			name: "custom status code but no value provided",
			rule: &model.ErrorPassthroughRule{
				Name:            "Missing Code",
				MatchMode:       model.MatchModeAny,
				ErrorCodes:      []int{422},
				PassthroughCode: false,
				ResponseCode:    nil,
				PassthroughBody: true,
			},
			expectError: true,
			errorField:  "response_code",
		},
		{
			name: "custom message but no value provided",
			rule: &model.ErrorPassthroughRule{
				Name:            "Missing Message",
				MatchMode:       model.MatchModeAny,
				ErrorCodes:      []int{422},
				PassthroughCode: true,
				PassthroughBody: false,
				CustomMessage:   nil,
			},
			expectError: true,
			errorField:  "custom_message",
		},
		{
			name: "custom message is empty string",
			rule: &model.ErrorPassthroughRule{
				Name:            "Empty Message",
				MatchMode:       model.MatchModeAny,
				ErrorCodes:      []int{422},
				PassthroughCode: true,
				PassthroughBody: false,
				CustomMessage:   testStrPtr(""),
			},
			expectError: true,
			errorField:  "custom_message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if tt.expectError {
				require.Error(t, err)
				validationErr, ok := err.(*model.ValidationError)
				require.True(t, ok, "should return ValidationError")
				assert.Equal(t, tt.errorField, validationErr.Field)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
//
// =============================================================================

func TestCreate_ForceRefreshCacheAfterWrite(t *testing.T) {
	ctx := context.Background()

	staleRule := newPassthroughRuleForWritePathTest(99, "service temporarily unavailable after multiple", "old cached message")
	repo := &mockErrorPassthroughRepo{rules: []*model.ErrorPassthroughRule{}}
	cache := newMockErrorPassthroughCache([]*model.ErrorPassthroughRule{staleRule}, true)

	svc := &ErrorPassthroughService{repo: repo, cache: cache}
	svc.setLocalCache([]*model.ErrorPassthroughRule{staleRule})

	newRule := newPassthroughRuleForWritePathTest(0, "service temporarily unavailable after multiple", "upstream request failed")
	created, err := svc.Create(ctx, newRule)
	require.NoError(t, err)
	require.NotNil(t, created)

	body := []byte(`{"message":"Service temporarily unavailable after multiple retries, please try again later"}`)
	matched := svc.MatchRule("anthropic", 503, body)
	require.NotNil(t, matched)
	assert.Equal(t, created.ID, matched.ID)
	if assert.NotNil(t, matched.CustomMessage) {
		assert.Equal(t, "upstream request failed", *matched.CustomMessage)
	}

	assert.Equal(t, 0, cache.getCalled, "write path refresh should not depend on cache.Get")
	assert.Equal(t, 1, cache.invalidateCalled)
	assert.Equal(t, 1, cache.setCalled)
	assert.Equal(t, 1, cache.notifyCalled)
}

func TestUpdate_ForceRefreshCacheAfterWrite(t *testing.T) {
	ctx := context.Background()

	originalRule := newPassthroughRuleForWritePathTest(1, "old keyword", "old message")
	repo := &mockErrorPassthroughRepo{rules: []*model.ErrorPassthroughRule{originalRule}}
	cache := newMockErrorPassthroughCache([]*model.ErrorPassthroughRule{originalRule}, true)

	svc := &ErrorPassthroughService{repo: repo, cache: cache}
	svc.setLocalCache([]*model.ErrorPassthroughRule{originalRule})

	updatedRule := newPassthroughRuleForWritePathTest(1, "new keyword", "new message")
	_, err := svc.Update(ctx, updatedRule)
	require.NoError(t, err)

	oldBody := []byte(`{"message":"old keyword"}`)
	oldMatched := svc.MatchRule("anthropic", 503, oldBody)
	assert.Nil(t, oldMatched, "old keyword should not match after update")

	newBody := []byte(`{"message":"new keyword"}`)
	newMatched := svc.MatchRule("anthropic", 503, newBody)
	require.NotNil(t, newMatched)
	if assert.NotNil(t, newMatched.CustomMessage) {
		assert.Equal(t, "new message", *newMatched.CustomMessage)
	}

	assert.Equal(t, 0, cache.getCalled, "write path refresh should not depend on cache.Get")
	assert.Equal(t, 1, cache.invalidateCalled)
	assert.Equal(t, 1, cache.setCalled)
	assert.Equal(t, 1, cache.notifyCalled)
}

func TestDelete_ForceRefreshCacheAfterWrite(t *testing.T) {
	ctx := context.Background()

	rule := newPassthroughRuleForWritePathTest(1, "to be deleted", "pre-deletion message")
	repo := &mockErrorPassthroughRepo{rules: []*model.ErrorPassthroughRule{rule}}
	cache := newMockErrorPassthroughCache([]*model.ErrorPassthroughRule{rule}, true)

	svc := &ErrorPassthroughService{repo: repo, cache: cache}
	svc.setLocalCache([]*model.ErrorPassthroughRule{rule})

	err := svc.Delete(ctx, 1)
	require.NoError(t, err)

	body := []byte(`{"message":"to be deleted"}`)
	matched := svc.MatchRule("anthropic", 503, body)
	assert.Nil(t, matched, "rule should not match after deletion")

	assert.Equal(t, 0, cache.getCalled, "write path refresh should not depend on cache.Get")
	assert.Equal(t, 1, cache.invalidateCalled)
	assert.Equal(t, 1, cache.setCalled)
	assert.Equal(t, 1, cache.notifyCalled)
}

func TestNewService_StartupReloadFromDBToHealStaleCache(t *testing.T) {
	staleRule := newPassthroughRuleForWritePathTest(99, "stale keyword", "old cached message")
	latestRule := newPassthroughRuleForWritePathTest(1, "fresh keyword", "latest message")

	repo := &mockErrorPassthroughRepo{rules: []*model.ErrorPassthroughRule{latestRule}}
	cache := newMockErrorPassthroughCache([]*model.ErrorPassthroughRule{staleRule}, true)

	svc := NewErrorPassthroughService(repo, cache)

	matchedFresh := svc.MatchRule("anthropic", 503, []byte(`{"message":"fresh keyword"}`))
	require.NotNil(t, matchedFresh)
	assert.Equal(t, int64(1), matchedFresh.ID)

	matchedStale := svc.MatchRule("anthropic", 503, []byte(`{"message":"stale keyword"}`))
	assert.Nil(t, matchedStale, "after start should override stale cache with latest DB rules")

	assert.Equal(t, 0, cache.getCalled, "start forced DB refresh should not depend on cache.Get")
	assert.Equal(t, 1, cache.setCalled, "after start should write back to cache, overriding stale cache")
}

func TestUpdate_RefreshFailureShouldNotKeepStaleEnabledRule(t *testing.T) {
	ctx := context.Background()

	staleRule := newPassthroughRuleForWritePathTest(1, "service temporarily unavailable after multiple", "old cached message")
	repo := &mockErrorPassthroughRepo{
		rules:   []*model.ErrorPassthroughRule{staleRule},
		listErr: errors.New("db list failed"),
	}
	cache := newMockErrorPassthroughCache([]*model.ErrorPassthroughRule{staleRule}, true)

	svc := &ErrorPassthroughService{repo: repo, cache: cache}
	svc.setLocalCache([]*model.ErrorPassthroughRule{staleRule})

	disabledRule := *staleRule
	disabledRule.Enabled = false
	_, err := svc.Update(ctx, &disabledRule)
	require.NoError(t, err)

	body := []byte(`{"message":"Service temporarily unavailable after multiple retries, please try again later"}`)
	matched := svc.MatchRule("anthropic", 503, body)
	assert.Nil(t, matched, "should not continue matching old enabled rules on refresh failure")

	svc.localCacheMu.RLock()
	assert.Nil(t, svc.localCache, "local cache should be cleared after refresh failure to avoid false matches")
	svc.localCacheMu.RUnlock()
}

func newPassthroughRuleForWritePathTest(id int64, keyword, customMsg string) *model.ErrorPassthroughRule {
	responseCode := 503
	rule := &model.ErrorPassthroughRule{
		ID:              id,
		Name:            "write-path-cache-refresh",
		Enabled:         true,
		Priority:        1,
		ErrorCodes:      []int{503},
		Keywords:        []string{keyword},
		MatchMode:       model.MatchModeAll,
		PassthroughCode: false,
		ResponseCode:    &responseCode,
		PassthroughBody: false,
		CustomMessage:   &customMsg,
	}
	return rule
}

// Helper functions
func testIntPtr(i int) *int       { return &i }
func testStrPtr(s string) *string { return &s }
