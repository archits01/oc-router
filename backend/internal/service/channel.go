package service

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// BillingMode
type BillingMode string

const (
	BillingModeToken      BillingMode = "token"       // 按 token 区间计费
	BillingModePerRequest BillingMode = "per_request" // 按次计费（支持上下文窗口分层）
	BillingModeImage      BillingMode = "image"       // 图片计费（当前按次，预留 token 计费）
)

// IsValid
func (m BillingMode) IsValid() bool {
	switch m {
	case BillingModeToken, BillingModePerRequest, BillingModeImage, "":
		return true
	}
	return false
}

const (
	BillingModelSourceRequested     = "requested"
	BillingModelSourceUpstream      = "upstream"
	BillingModelSourceChannelMapped = "channel_mapped"
)

// Channel
type Channel struct {
	ID                 int64
	Name               string
	Description        string
	Status             string
	BillingModelSource string         // "requested", "upstream", or "channel_mapped"
	RestrictModels     bool           // 是否限制model（仅允许定价列表中的model）
	Features           string         // 渠道特性description（JSON 数组），用于支付页面展示
	FeaturesConfig     map[string]any // 渠道功能configuration（如 web search emulation）
	CreatedAt          time.Time
	UpdatedAt          time.Time

	GroupIDs []int64
	//
	ModelPricing []ChannelModelPricing
	// → {src→dst}）
	ModelMapping map[string]map[string]string

	ApplyPricingToAccountStats bool                      // 是否应用渠道model定价到账号统计
	AccountStatsPricingRules   []AccountStatsPricingRule // 自定义账号统计定价规则（按 SortOrder 排序，先命中为准）
}

// AccountStatsPricingRule
//
type AccountStatsPricingRule struct {
	ID         int64
	ChannelID  int64
	Name       string
	GroupIDs   []int64
	AccountIDs []int64
	SortOrder  int
	Pricing    []ChannelModelPricing // 规则内的model定价（复用现有定价结构）
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ChannelModelPricing
type ChannelModelPricing struct {
	ID               int64
	ChannelID        int64
	Platform         string            // 所属平台（anthropic/openai/gemini/...）
	Models           []string          // 绑定的model列表
	BillingMode      BillingMode       // 计费模式
	InputPrice       *float64          // 每 token 输入价格（USD）— backward compatible flat 定价
	OutputPrice      *float64          // 每 token 输出价格（USD）
	CacheWritePrice  *float64          // 缓存写入价格
	CacheReadPrice   *float64          // 缓存读取价格
	ImageOutputPrice *float64          // 图片输出价格（backward compatible）
	PerRequestPrice  *float64          // 默认按次计费价格（USD）
	Intervals        []PricingInterval // 区间定价列表
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// PricingInterval
type PricingInterval struct {
	ID              int64
	PricingID       int64
	MinTokens       int      // 区间下界（含）
	MaxTokens       *int     // 区间上界（不含），nil = 无上限
	TierLabel       string   // 层级标签（按次/图片模式：1K, 2K, 4K, HD 等）
	InputPrice      *float64 // token 模式：每 token 输入价
	OutputPrice     *float64 // token 模式：每 token 输出价
	CacheWritePrice *float64 // token 模式：缓存写入价
	CacheReadPrice  *float64 // token 模式：缓存读取价
	PerRequestPrice *float64 // 按次/图片模式：每次请求价格
	SortOrder       int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// IsActive
func (c *Channel) IsActive() bool {
	return c.Status == StatusActive
}

// normalizeBillingModelSource
// *Channel
// （
func (c *Channel) normalizeBillingModelSource() {
	if c == nil {
		return
	}
	if c.BillingModelSource == "" {
		c.BillingModelSource = BillingModelSourceChannelMapped
	}
}

// GetModelPricing
func (c *Channel) GetModelPricing(model string) *ChannelModelPricing {
	modelLower := strings.ToLower(model)

	for i := range c.ModelPricing {
		for _, m := range c.ModelPricing[i].Models {
			if strings.ToLower(m) == modelLower {
				cp := c.ModelPricing[i].Clone()
				return &cp
			}
		}
	}

	return nil
}

// FindMatchingInterval
// (min, max]：min
// =0
func FindMatchingInterval(intervals []PricingInterval, totalTokens int) *PricingInterval {
	for i := range intervals {
		iv := &intervals[i]
		if totalTokens > iv.MinTokens && (iv.MaxTokens == nil || totalTokens <= *iv.MaxTokens) {
			return iv
		}
	}
	return nil
}

// GetIntervalForContext
func (p *ChannelModelPricing) GetIntervalForContext(totalTokens int) *PricingInterval {
	return FindMatchingInterval(p.Intervals, totalTokens)
}

// GetTierByLabel
func (p *ChannelModelPricing) GetTierByLabel(label string) *PricingInterval {
	labelLower := strings.ToLower(label)
	for i := range p.Intervals {
		if strings.ToLower(p.Intervals[i].TierLabel) == labelLower {
			return &p.Intervals[i]
		}
	}
	return nil
}

// Clone
func (p ChannelModelPricing) Clone() ChannelModelPricing {
	cp := p
	if p.Models != nil {
		cp.Models = make([]string, len(p.Models))
		copy(cp.Models, p.Models)
	}
	if p.Intervals != nil {
		cp.Intervals = make([]PricingInterval, len(p.Intervals))
		copy(cp.Intervals, p.Intervals)
	}
	return cp
}

// Clone
func (c *Channel) Clone() *Channel {
	if c == nil {
		return nil
	}
	cp := *c
	if c.GroupIDs != nil {
		cp.GroupIDs = make([]int64, len(c.GroupIDs))
		copy(cp.GroupIDs, c.GroupIDs)
	}
	if c.ModelPricing != nil {
		cp.ModelPricing = make([]ChannelModelPricing, len(c.ModelPricing))
		for i := range c.ModelPricing {
			cp.ModelPricing[i] = c.ModelPricing[i].Clone()
		}
	}
	if c.ModelMapping != nil {
		cp.ModelMapping = make(map[string]map[string]string, len(c.ModelMapping))
		for platform, mapping := range c.ModelMapping {
			inner := make(map[string]string, len(mapping))
			for k, v := range mapping {
				inner[k] = v
			}
			cp.ModelMapping[platform] = inner
		}
	}
	if c.FeaturesConfig != nil {
		cp.FeaturesConfig = deepCopyFeaturesConfig(c.FeaturesConfig)
	}
	if c.AccountStatsPricingRules != nil {
		cp.AccountStatsPricingRules = make([]AccountStatsPricingRule, len(c.AccountStatsPricingRules))
		for i, rule := range c.AccountStatsPricingRules {
			cp.AccountStatsPricingRules[i] = rule
			if rule.GroupIDs != nil {
				cp.AccountStatsPricingRules[i].GroupIDs = make([]int64, len(rule.GroupIDs))
				copy(cp.AccountStatsPricingRules[i].GroupIDs, rule.GroupIDs)
			}
			if rule.AccountIDs != nil {
				cp.AccountStatsPricingRules[i].AccountIDs = make([]int64, len(rule.AccountIDs))
				copy(cp.AccountStatsPricingRules[i].AccountIDs, rule.AccountIDs)
			}
			if rule.Pricing != nil {
				cp.AccountStatsPricingRules[i].Pricing = make([]ChannelModelPricing, len(rule.Pricing))
				for j := range rule.Pricing {
					cp.AccountStatsPricingRules[i].Pricing[j] = rule.Pricing[j].Clone()
				}
			}
		}
	}
	return &cp
}

// IsWebSearchEmulationEnabled
func (c *Channel) IsWebSearchEmulationEnabled(platform string) bool {
	if c == nil || c.FeaturesConfig == nil {
		return false
	}
	wse, ok := c.FeaturesConfig[featureKeyWebSearchEmulation].(map[string]any)
	if !ok {
		return false
	}
	enabled, ok := wse[platform].(bool)
	return ok && enabled
}

// IsBedrockCCCompatEnabled
//
func (c *Channel) IsBedrockCCCompatEnabled(platform string) bool {
	if c == nil || c.FeaturesConfig == nil {
		return false
	}
	//
	enabled, ok := c.FeaturesConfig[featureKeyBedrockCCCompat].(bool)
	return ok && enabled
}

// deepCopyFeaturesConfig creates a deep copy of FeaturesConfig to prevent cache pollution.
func deepCopyFeaturesConfig(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		if inner, ok := v.(map[string]any); ok {
			dst[k] = deepCopyFeaturesConfig(inner)
		} else {
			dst[k] = v
		}
	}
	return dst
}

// ValidateIntervals
//
// mode
//   - BillingModeToken（(min, max]，
//     =nil）
//   - BillingModePerRequest / BillingModeImage：
//     (1K/2K/4K )
//
//
// >= 0；MaxTokens > 0 > MinTokens；
// >= 0。
func ValidateIntervals(intervals []PricingInterval, mode BillingMode) error {
	if len(intervals) == 0 {
		return nil
	}
	sorted := make([]PricingInterval, len(intervals))
	copy(sorted, intervals)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].MinTokens < sorted[j].MinTokens
	})

	for i := range sorted {
		if err := validateSingleInterval(&sorted[i], i); err != nil {
			return err
		}
	}

	// per_request / image
	if mode == BillingModePerRequest || mode == BillingModeImage {
		return nil
	}
	return validateIntervalOverlap(sorted)
}

// validateSingleInterval
func validateSingleInterval(iv *PricingInterval, idx int) error {
	if iv.MinTokens < 0 {
		return fmt.Errorf("interval #%d: min_tokens (%d) must be >= 0", idx+1, iv.MinTokens)
	}
	if iv.MaxTokens != nil {
		if *iv.MaxTokens <= 0 {
			return fmt.Errorf("interval #%d: max_tokens (%d) must be > 0", idx+1, *iv.MaxTokens)
		}
		if *iv.MaxTokens <= iv.MinTokens {
			return fmt.Errorf("interval #%d: max_tokens (%d) must be > min_tokens (%d)",
				idx+1, *iv.MaxTokens, iv.MinTokens)
		}
	}
	return validateIntervalPrices(iv, idx)
}

// validateIntervalPrices >= 0
func validateIntervalPrices(iv *PricingInterval, idx int) error {
	prices := []struct {
		name string
		val  *float64
	}{
		{"input_price", iv.InputPrice},
		{"output_price", iv.OutputPrice},
		{"cache_write_price", iv.CacheWritePrice},
		{"cache_read_price", iv.CacheReadPrice},
		{"per_request_price", iv.PerRequestPrice},
	}
	for _, p := range prices {
		if p.val != nil && *p.val < 0 {
			return fmt.Errorf("interval #%d: %s must be >= 0", idx+1, p.name)
		}
	}
	return nil
}

// validateIntervalOverlap
func validateIntervalOverlap(sorted []PricingInterval) error {
	for i, iv := range sorted {
		if iv.MaxTokens == nil && i < len(sorted)-1 {
			return fmt.Errorf("interval #%d: unbounded interval (max_tokens=null) must be the last one",
				i+1)
		}
		if i == 0 {
			continue
		}
		prev := sorted[i-1]
		// (min, max] (prev.Min, prev.Max]，cur (cur.Min, cur.Max]
		if prev.MaxTokens == nil || *prev.MaxTokens > iv.MinTokens {
			return fmt.Errorf("interval #%d and #%d overlap: prev max=%s > cur min=%d",
				i, i+1, formatMaxTokensLabel(prev.MaxTokens), iv.MinTokens)
		}
	}
	return nil
}

func formatMaxTokensLabel(max *int) string {
	if max == nil {
		return "∞"
	}
	return fmt.Sprintf("%d", *max)
}

// ChannelUsageFields
type ChannelUsageFields struct {
	ChannelID          int64  // channel ID（0 = 无渠道）
	OriginalModel      string // user原始请求model（渠道映射前）
	ChannelMappedModel string // 渠道映射后的model名（无映射时等于 OriginalModel）
	BillingModelSource string // 计费model来源："requested" / "upstream" / "channel_mapped"
	ModelMappingChain  string // 映射链description，如 "a→b→c"
}

// SupportedModel
type SupportedModel struct {
	Name     string               // user侧model名
	Platform string               // 所属平台
	Pricing  *ChannelModelPricing // 定价详情（nil 表示未configuration定价）
}

// wildcardSuffix
const wildcardSuffix = "*"

// splitWildcardSuffix (prefix, isWildcard)。
//
//	"claude-opus-*"  → ("claude-opus-", true)
//	"claude-opus-4"  → ("claude-opus-4", false)
//	"*"              → ("", true)
//
//
func splitWildcardSuffix(pattern string) (prefix string, isWildcard bool) {
	if strings.HasSuffix(pattern, wildcardSuffix) {
		return strings.TrimSuffix(pattern, wildcardSuffix), true
	}
	return pattern, false
}

// GetModelPricingByPlatform
//
func (c *Channel) GetModelPricingByPlatform(platform, model string) *ChannelModelPricing {
	if c == nil {
		return nil
	}
	modelLower := strings.ToLower(model)
	for i := range c.ModelPricing {
		if c.ModelPricing[i].Platform != platform {
			continue
		}
		for _, m := range c.ModelPricing[i].Models {
			if strings.ToLower(m) == modelLower {
				cp := c.ModelPricing[i].Clone()
				return &cp
			}
		}
	}
	return nil
}

// platformPricingIndex
//
//
//
// byLower
//
type platformPricingIndex struct {
	byLower      map[string]*ChannelModelPricing // lowercased model name → pricing (Clone'd)
	originalCase map[string]string               // lowercased model name → original-case model name
	names        []string                        // priced model names in their ORIGINAL case, insertion-ordered, deduped case-insensitively (first wins)
}

// buildPricingIndex
//
// "claude-*"）
func buildPricingIndex(pricings []ChannelModelPricing) map[string]*platformPricingIndex {
	idx := make(map[string]*platformPricingIndex)
	for i := range pricings {
		p := pricings[i]
		pidx, ok := idx[p.Platform]
		if !ok {
			pidx = &platformPricingIndex{
				byLower:      make(map[string]*ChannelModelPricing),
				originalCase: make(map[string]string),
				names:        make([]string, 0),
			}
			idx[p.Platform] = pidx
		}
		for _, m := range p.Models {
			if _, wild := splitWildcardSuffix(m); wild {
				continue
			}
			lower := strings.ToLower(m)
			if _, exists := pidx.byLower[lower]; exists {
				continue // 首个命中胜出（case-insensitive 去重后第一个定价 / 第一个原始大小写）
			}
			cp := pricings[i].Clone()
			pidx.byLower[lower] = &cp
			pidx.originalCase[lower] = m
			pidx.names = append(pidx.names, m)
		}
	}
	return idx
}

// SupportedModels
//
// ∪ pricing
//
//   - Pass A（mapping）：
//   - → target：= src（
//     （mapping ""）。
//     target
//   - "claude-3-*"）：
//
//   - "*" →
//   - Pass B（pricing-only）：
//     ——= =
//
// ****（
// (Platform, Name) (Platform, lowercase(Name))
//
// ——
// （`ChannelService.ListAvailable`）
func (c *Channel) SupportedModels() []SupportedModel {
	if c == nil {
		return nil
	}
	if len(c.ModelMapping) == 0 && len(c.ModelPricing) == 0 {
		return nil
	}

	idx := buildPricingIndex(c.ModelPricing)

	type dedupKey struct {
		platform string
		name     string
	}
	seen := make(map[dedupKey]struct{})
	result := make([]SupportedModel, 0)

	// lookup
	lookup := func(pidx *platformPricingIndex, name string) (display string, pricing *ChannelModelPricing) {
		if pidx == nil || name == "" {
			return name, nil
		}
		lower := strings.ToLower(name)
		if p, ok := pidx.byLower[lower]; ok {
			return pidx.originalCase[lower], p
		}
		return name, nil
	}

	add := func(platform, displayName string, pricing *ChannelModelPricing) {
		key := dedupKey{platform: platform, name: strings.ToLower(displayName)}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, SupportedModel{
			Name:     displayName,
			Platform: platform,
			Pricing:  pricing,
		})
	}

	// Pass A：
	for platform, mapping := range c.ModelMapping {
		if len(mapping) == 0 {
			continue
		}
		pidx := idx[platform]
		for src, target := range mapping {
			prefix, isWild := splitWildcardSuffix(src)
			if isWild {
				if pidx == nil {
					continue
				}
				prefixLower := strings.ToLower(prefix)
				for _, candidate := range pidx.names {
					if strings.HasPrefix(strings.ToLower(candidate), prefixLower) {
						display, pricing := lookup(pidx, candidate)
						add(platform, display, pricing)
					}
				}
				continue
			}
			//
			pricingKey := target
			if pricingKey == "" {
				pricingKey = src
			}
			if _, targetWild := splitWildcardSuffix(pricingKey); targetWild {
				pricingKey = src
			}
			_, pricing := lookup(pidx, pricingKey)
			//
			displayName, _ := lookup(pidx, src)
			add(platform, displayName, pricing)
		}
	}

	// Pass B："→ "）
	for platform, pidx := range idx {
		for _, name := range pidx.names {
			display, pricing := lookup(pidx, name)
			add(platform, display, pricing)
		}
	}

	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Platform != result[j].Platform {
			return result[i].Platform < result[j].Platform
		}
		return result[i].Name < result[j].Name
	})
	return result
}
