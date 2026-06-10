package service

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const defaultBedrockRegion = "us-east-1"

// featureKeyBedrockCCCompat is the key used in Channel.FeaturesConfig for Bedrock CC compatibility.
const featureKeyBedrockCCCompat = "bedrock_cc_compat"

var bedrockCrossRegionPrefixes = []string{"us.", "eu.", "apac.", "jp.", "au.", "us-gov.", "global."}

// BedrockCrossRegionPrefix
//
func BedrockCrossRegionPrefix(region string) string {
	switch {
	case strings.HasPrefix(region, "us-gov"):
		return "us-gov" // GovCloud 使用独立的 us-gov 前缀
	case strings.HasPrefix(region, "us-"):
		return "us"
	case strings.HasPrefix(region, "eu-"):
		return "eu"
	case region == "ap-northeast-1":
		return "jp" // 日本区域使用独立的 jp 前缀（AWS 官方定义）
	case region == "ap-southeast-2":
		return "au" // 澳大利亚区域使用独立的 au 前缀（AWS 官方定义）
	case strings.HasPrefix(region, "ap-"):
		return "apac" // 其余亚太区域使用通用 apac 前缀
	case strings.HasPrefix(region, "ca-"):
		return "us" // 加拿大区域使用 us 前缀的跨区域推理
	case strings.HasPrefix(region, "sa-"):
		return "us" // 南美区域使用 us 前缀的跨区域推理
	default:
		return "us"
	}
}

// AdjustBedrockModelRegionPrefix
// =eu-west-1 "us.anthropic.claude-opus-4-6-v1" → "eu.anthropic.claude-opus-4-6-v1"
// ="global"
func AdjustBedrockModelRegionPrefix(modelID, region string) string {
	var targetPrefix string
	if region == "global" {
		targetPrefix = "global"
	} else {
		targetPrefix = BedrockCrossRegionPrefix(region)
	}

	for _, p := range bedrockCrossRegionPrefixes {
		if strings.HasPrefix(modelID, p) {
			if p == targetPrefix+"." {
				return modelID // 前缀已匹配，无需替换
			}
			return targetPrefix + "." + modelID[len(p):]
		}
	}

	// "anthropic.claude-..."），
	return modelID
}

func bedrockRuntimeRegion(account *Account) string {
	if account == nil {
		return defaultBedrockRegion
	}
	if region := account.GetCredential("aws_region"); region != "" {
		return region
	}
	return defaultBedrockRegion
}

func shouldForceBedrockGlobal(account *Account) bool {
	return account != nil && account.GetCredential("aws_force_global") == "true"
}

func isRegionalBedrockModelID(modelID string) bool {
	for _, prefix := range bedrockCrossRegionPrefixes {
		if strings.HasPrefix(modelID, prefix) {
			return true
		}
	}
	return false
}

func isLikelyBedrockModelID(modelID string) bool {
	lower := strings.ToLower(strings.TrimSpace(modelID))
	if lower == "" {
		return false
	}
	if strings.HasPrefix(lower, "arn:") {
		return true
	}
	for _, prefix := range []string{
		"anthropic.",
		"amazon.",
		"meta.",
		"mistral.",
		"cohere.",
		"ai21.",
		"deepseek.",
		"stability.",
		"writer.",
		"nova.",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return isRegionalBedrockModelID(lower)
}

func normalizeBedrockModelID(modelID string) (normalized string, shouldAdjustRegion bool, ok bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return "", false, false
	}
	if mapped, exists := domain.DefaultBedrockModelMapping[modelID]; exists {
		return mapped, true, true
	}
	if isRegionalBedrockModelID(modelID) {
		return modelID, true, true
	}
	if isLikelyBedrockModelID(modelID) {
		return modelID, false, true
	}
	return "", false, false
}

// ResolveBedrockModelID resolves a requested Claude model into a Bedrock model ID.
// It applies account model_mapping first, then default Bedrock aliases, and finally
// adjusts Anthropic cross-region prefixes to match the account region.
func ResolveBedrockModelID(account *Account, requestedModel string) (string, bool) {
	if account == nil {
		return "", false
	}

	mappedModel := account.GetMappedModel(requestedModel)
	modelID, shouldAdjustRegion, ok := normalizeBedrockModelID(mappedModel)
	if !ok {
		return "", false
	}
	if shouldAdjustRegion {
		targetRegion := bedrockRuntimeRegion(account)
		if shouldForceBedrockGlobal(account) {
			targetRegion = "global"
		}
		modelID = AdjustBedrockModelRegionPrefix(modelID, targetRegion)
	}
	return modelID, true
}

// BuildBedrockURL
// stream=true
// modelID (safe="")
func BuildBedrockURL(region, modelID string, stream bool) string {
	if region == "" {
		region = defaultBedrockRegion
	}
	encodedModelID := url.PathEscape(modelID)
	// url.PathEscape ":"），
	// %3A
	encodedModelID = strings.ReplaceAll(encodedModelID, ":", "%3A")
	if stream {
		return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/invoke-with-response-stream", region, encodedModelID)
	}
	return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/invoke", region, encodedModelID)
}

// PrepareBedrockRequestBody
//  1.
//  2.
//  3.
//  4. {defer_loading: true}）
//  5.
//  6.
//  7.
//  8.
func PrepareBedrockRequestBody(body []byte, modelID string, betaHeader string) ([]byte, error) {
	betaTokens := ResolveBedrockBetaTokens(betaHeader, body, modelID)
	return PrepareBedrockRequestBodyWithTokens(body, modelID, betaTokens, false)
}

// PrepareBedrockRequestBodyWithTokens prepares a Bedrock request using pre-resolved beta tokens.
// ccCompat
func PrepareBedrockRequestBodyWithTokens(body []byte, modelID string, betaTokens []string, ccCompat bool) ([]byte, error) {
	var err error

	betaTokens = filterBedrockBetaTokens(betaTokens)
	body = sanitizeBedrockFieldsForBetaTokens(body, betaTokens)

	//
	body, err = sjson.SetBytes(body, "anthropic_version", "bedrock-2023-05-31")
	if err != nil {
		return nil, fmt.Errorf("inject anthropic_version: %w", err)
	}

	//
	// 1.
	// 2.
	//    () + _get_tool_search_beta_header_for_bedrock()
	if len(betaTokens) > 0 {
		body, err = sjson.SetBytes(body, "anthropic_beta", betaTokens)
		if err != nil {
			return nil, fmt.Errorf("inject anthropic_beta: %w", err)
		}
		logger.LegacyPrintf("service.gateway", "[Bedrock] Injected beta tokens: %v (model=%s ccCompat=%v)", betaTokens, modelID, ccCompat)
	} else {
		body, _ = sjson.DeleteBytes(body, "anthropic_beta")
	}

	//
	body, _ = sjson.DeleteBytes(body, "provider")
	body, _ = sjson.DeleteBytes(body, "metadata")

	//
	body, err = sjson.DeleteBytes(body, "model")
	if err != nil {
		return nil, fmt.Errorf("remove model field: %w", err)
	}

	//
	body, err = sjson.DeleteBytes(body, "stream")
	if err != nil {
		return nil, fmt.Errorf("remove stream field: %w", err)
	}

	//
	// ()
	body = convertOutputFormatToInlineSchema(body)

	//
	body, err = sjson.DeleteBytes(body, "output_config")
	if err != nil {
		return nil, fmt.Errorf("remove output_config field: %w", err)
	}

	//
	// Claude Code (v2.1.69+) {defer_loading: true}，
	// Anthropic API "Extra inputs are not permitted"
	body = removeCustomFieldFromTools(body)

	//
	body = sanitizeBedrockCacheControl(body, modelID)

	// CC
	if ccCompat {
		body = sanitizeBedrockThinking(body, modelID)
		body = sanitizeBedrockToolUseIDs(body)
	}

	return body, nil
}

// ResolveBedrockBetaTokens computes the final Bedrock beta token list before policy filtering.
func ResolveBedrockBetaTokens(betaHeader string, body []byte, modelID string) []string {
	betaTokens := parseAnthropicBetaHeader(betaHeader)
	betaTokens = autoInjectBedrockBetaTokens(betaTokens, body, modelID)
	return filterBedrockBetaTokens(betaTokens)
}

// convertOutputFormatToInlineSchema
// Bedrock Invoke
// ()
func convertOutputFormatToInlineSchema(body []byte) []byte {
	outputFormat := gjson.GetBytes(body, "output_format")
	if !outputFormat.Exists() || !outputFormat.IsObject() {
		return body
	}

	//
	body, _ = sjson.DeleteBytes(body, "output_format")

	schema := outputFormat.Get("schema")
	if !schema.Exists() {
		return body
	}

	//
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}
	msgArr := messages.Array()
	lastUserIdx := -1
	for i := len(msgArr) - 1; i >= 0; i-- {
		if msgArr[i].Get("role").String() == "user" {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx < 0 {
		return body
	}

	//
	schemaJSON, err := json.Marshal(json.RawMessage(schema.Raw))
	if err != nil {
		return body
	}

	content := msgArr[lastUserIdx].Get("content")
	basePath := fmt.Sprintf("messages.%d.content", lastUserIdx)

	if content.IsArray() {
		//
		idx := len(content.Array())
		body, _ = sjson.SetBytes(body, fmt.Sprintf("%s.%d.type", basePath, idx), "text")
		body, _ = sjson.SetBytes(body, fmt.Sprintf("%s.%d.text", basePath, idx), string(schemaJSON))
	} else if content.Type == gjson.String {
		// content
		originalText := content.String()
		body, _ = sjson.SetBytes(body, basePath, []map[string]string{
			{"type": "text", "text": originalText},
			{"type": "text", "text": string(schemaJSON)},
		})
	}

	return body
}

// removeCustomFieldFromTools
func removeCustomFieldFromTools(body []byte) []byte {
	tools := gjson.GetBytes(body, "tools")
	if !tools.Exists() || !tools.IsArray() {
		return body
	}
	var err error
	for i := range tools.Array() {
		body, err = sjson.DeleteBytes(body, fmt.Sprintf("tools.%d.custom", i))
		if err != nil {
			continue
		}
	}
	return body
}

// claudeVersionRe
// {tier}-{major}-{minor} {tier}-{major}.{minor}
var claudeVersionRe = regexp.MustCompile(`claude-(?:haiku|sonnet|opus)-(\d+)[-.](\d+)`)

// isBedrockClaude45OrNewer
// Claude 4.5+ "5m" "1h"）
func isBedrockClaude45OrNewer(modelID string) bool {
	lower := strings.ToLower(modelID)
	if isBedrockFable5(lower) {
		return true
	}
	matches := claudeVersionRe.FindStringSubmatch(lower)
	if matches == nil {
		return false
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	return major > 4 || (major == 4 && minor >= 5)
}

// sanitizeBedrockCacheControl
// Bedrock
//   - scope：Bedrock "global"
//   - ttl：+ "5m" "1h"，
func sanitizeBedrockCacheControl(body []byte, modelID string) []byte {
	isClaude45 := isBedrockClaude45OrNewer(modelID)

	//
	systemArr := gjson.GetBytes(body, "system")
	if systemArr.Exists() && systemArr.IsArray() {
		for i, item := range systemArr.Array() {
			if !item.IsObject() {
				continue
			}
			cc := item.Get("cache_control")
			if !cc.Exists() || !cc.IsObject() {
				continue
			}
			body = deleteCacheControlUnsupportedFields(body, fmt.Sprintf("system.%d.cache_control", i), cc, isClaude45)
		}
	}

	//
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}
	for mi, msg := range messages.Array() {
		if !msg.IsObject() {
			continue
		}
		content := msg.Get("content")
		if !content.Exists() || !content.IsArray() {
			continue
		}
		for ci, block := range content.Array() {
			if !block.IsObject() {
				continue
			}
			cc := block.Get("cache_control")
			if !cc.Exists() || !cc.IsObject() {
				continue
			}
			body = deleteCacheControlUnsupportedFields(body, fmt.Sprintf("messages.%d.content.%d.cache_control", mi, ci), cc, isClaude45)
		}
	}

	return body
}

// deleteCacheControlUnsupportedFields
func deleteCacheControlUnsupportedFields(body []byte, basePath string, cc gjson.Result, isClaude45 bool) []byte {
	// Bedrock "global"）
	if cc.Get("scope").Exists() {
		body, _ = sjson.DeleteBytes(body, basePath+".scope")
	}

	// ttl：+ "5m" "1h"，
	ttl := cc.Get("ttl")
	if ttl.Exists() {
		shouldRemove := true
		if isClaude45 {
			v := ttl.String()
			if v == "5m" || v == "1h" {
				shouldRemove = false
			}
		}
		if shouldRemove {
			body, _ = sjson.DeleteBytes(body, basePath+".ttl")
		}
	}

	return body
}

// parseAnthropicBetaHeader
func parseAnthropicBetaHeader(header string) []string {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	if strings.HasPrefix(header, "[") && strings.HasSuffix(header, "]") {
		var parsed []any
		if err := json.Unmarshal([]byte(header), &parsed); err == nil {
			tokens := make([]string, 0, len(parsed))
			for _, item := range parsed {
				token := strings.TrimSpace(fmt.Sprint(item))
				if token != "" {
					tokens = append(tokens, token)
				}
			}
			return tokens
		}
	}
	var tokens []string
	for _, part := range strings.Split(header, ",") {
		t := strings.TrimSpace(part)
		if t != "" {
			tokens = append(tokens, t)
		}
	}
	return tokens
}

// bedrockSupportedBetaTokens
// + litellm anthropic_beta_headers_config.json
//
var bedrockSupportedBetaTokens = map[string]bool{
	"computer-use-2025-01-24":                true,
	"computer-use-2025-11-24":                true,
	"context-1m-2025-08-07":                  true,
	"context-management-2025-06-27":          true, // compaction + clear_thinking，AWS 文档已支持
	"compact-2026-01-12":                     true, // 官方支持，仅 InvokeModel API（Opus 4.6+）
	"fine-grained-tool-streaming-2025-05-14": true, // AWS Tool Use 文档已支持
	// "interleaved-thinking-2025-05-14": false, //
	"tool-search-tool-2025-10-19": true,
	"tool-examples-2025-10-29":    true,
}

const bedrockContextManagementBetaToken = "context-management-2025-06-27"

// bedrockBetaTokenTransforms
// Anthropic
var bedrockBetaTokenTransforms = map[string]string{
	"advanced-tool-use-2025-11-20": "tool-search-tool-2025-10-19",
}

// autoInjectBedrockBetaTokens
// ()
// AmazonAnthropicClaudeMessagesConfig._get_tool_search_beta_header_for_bedrock()
//
//
//
func autoInjectBedrockBetaTokens(tokens []string, body []byte, modelID string) []string {
	seen := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		seen[t] = true
	}

	inject := func(token string) {
		if !seen[token] {
			tokens = append(tokens, token)
			seen[token] = true
		}
	}

	//
	//

	//
	// tools ="computer_20xxxxxx" →
	tools := gjson.GetBytes(body, "tools")
	if tools.Exists() && tools.IsArray() {
		toolSearchUsed := false
		programmaticToolCallingUsed := false
		inputExamplesUsed := false
		for _, tool := range tools.Array() {
			toolType := tool.Get("type").String()
			if strings.HasPrefix(toolType, "computer_20") {
				inject("computer-use-2025-11-24")
			}
			if isBedrockToolSearchType(toolType) {
				toolSearchUsed = true
			}
			if hasCodeExecutionAllowedCallers(tool) {
				programmaticToolCallingUsed = true
			}
			if hasInputExamples(tool) {
				inputExamplesUsed = true
			}
		}
		if programmaticToolCallingUsed || inputExamplesUsed {
			// programmatic tool calling
			//
			inject("advanced-tool-use-2025-11-20")
		}
		if toolSearchUsed && bedrockModelSupportsToolSearch(modelID) {
			//
			// → tool-search-tool
			if !programmaticToolCallingUsed && !inputExamplesUsed {
				inject("tool-search-tool-2025-10-19")
			} else {
				inject("advanced-tool-use-2025-11-20")
			}
		}
	}

	return tokens
}

func isBedrockToolSearchType(toolType string) bool {
	return toolType == "tool_search_tool_regex_20251119" || toolType == "tool_search_tool_bm25_20251119"
}

func hasCodeExecutionAllowedCallers(tool gjson.Result) bool {
	allowedCallers := tool.Get("allowed_callers")
	if containsStringInJSONArray(allowedCallers, "code_execution_20250825") {
		return true
	}
	return containsStringInJSONArray(tool.Get("function.allowed_callers"), "code_execution_20250825")
}

func hasInputExamples(tool gjson.Result) bool {
	if arr := tool.Get("input_examples"); arr.Exists() && arr.IsArray() && len(arr.Array()) > 0 {
		return true
	}
	arr := tool.Get("function.input_examples")
	return arr.Exists() && arr.IsArray() && len(arr.Array()) > 0
}

func containsStringInJSONArray(result gjson.Result, target string) bool {
	if !result.Exists() || !result.IsArray() {
		return false
	}
	for _, item := range result.Array() {
		if item.String() == target {
			return true
		}
	}
	return false
}

// bedrockModelSupportsToolSearch
// +
func bedrockModelSupportsToolSearch(modelID string) bool {
	lower := strings.ToLower(modelID)
	matches := claudeVersionRe.FindStringSubmatch(lower)
	if matches == nil {
		return false
	}
	// Haiku
	if strings.Contains(lower, "haiku") {
		return false
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	return major > 4 || (major == 4 && minor >= 5)
}

// filterBedrockBetaTokens
// 1. → tool-search-tool）
// 2.
// 3.
func filterBedrockBetaTokens(tokens []string) []string {
	seen := make(map[string]bool, len(tokens))
	var result []string

	for _, t := range tokens {
		if replacement, ok := bedrockBetaTokenTransforms[t]; ok {
			t = replacement
		}
		//
		if bedrockSupportedBetaTokens[t] && !seen[t] {
			result = append(result, t)
			seen[t] = true
		}
	}

	//
	if seen["tool-search-tool-2025-10-19"] && !seen["tool-examples-2025-10-29"] {
		result = append(result, "tool-examples-2025-10-29")
	}

	return result
}

func sanitizeBedrockFieldsForBetaTokens(body []byte, betaTokens []string) []byte {
	if !containsBedrockBetaToken(betaTokens, bedrockContextManagementBetaToken) && gjson.GetBytes(body, "context_management").Exists() {
		body, _ = sjson.DeleteBytes(body, "context_management")
	}
	return body
}

func containsBedrockBetaToken(tokens []string, target string) bool {
	for _, token := range tokens {
		if token == target {
			return true
		}
	}
	return false
}

// bedrockToolUseIDRe
var bedrockToolUseIDRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// isBedrockOpus47OrNewer
// Opus 4.7 "adaptive"，"enabled"
func isBedrockOpus47OrNewer(modelID string) bool {
	lower := strings.ToLower(modelID)
	if !strings.Contains(lower, "opus") {
		return false
	}
	matches := claudeVersionRe.FindStringSubmatch(lower)
	if matches == nil {
		return false
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	return major > 4 || (major == 4 && minor >= 7)
}

func isBedrockFable5(modelID string) bool {
	return strings.Contains(strings.ToLower(modelID), "claude-fable-5")
}

const defaultThinkingBudgetTokens = 10000

// sanitizeBedrockThinking
//   - Fable 5:
//   - Opus 4.7+: "adaptive"，"enabled" "adaptive"
//   - "enabled"
func sanitizeBedrockThinking(body []byte, modelID string) []byte {
	thinking := gjson.GetBytes(body, "thinking")
	if !thinking.Exists() || !thinking.IsObject() {
		return body
	}

	thinkingType := thinking.Get("type").String()
	if thinkingType == "" {
		return body
	}

	if isBedrockFable5(modelID) {
		if thinkingType == "enabled" {
			body, _ = sjson.SetBytes(body, "thinking.type", "adaptive")
		}
		if thinkingType == "enabled" || thinkingType == "adaptive" {
			body, _ = sjson.DeleteBytes(body, "thinking.budget_tokens")
		}
		return body
	}

	if isBedrockOpus47OrNewer(modelID) {
		if thinkingType == "enabled" {
			body, _ = sjson.SetBytes(body, "thinking.type", "adaptive")
			body, _ = sjson.DeleteBytes(body, "thinking.budget_tokens")
		}
		return body
	}

	if thinkingType == "enabled" && !thinking.Get("budget_tokens").Exists() {
		body, _ = sjson.SetBytes(body, "thinking.budget_tokens", defaultThinkingBudgetTokens)
	}

	return body
}

// sanitizeBedrockToolUseIDs
// '^[a-zA-Z0-9_-]+$'。
func sanitizeBedrockToolUseIDs(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}
	for mi, msg := range messages.Array() {
		content := msg.Get("content")
		if !content.Exists() || !content.IsArray() {
			continue
		}
		for ci, block := range content.Array() {
			switch block.Get("type").String() {
			case "tool_use":
				body = sanitizeIDField(body, block.Get("id").String(), fmt.Sprintf("messages.%d.content.%d.id", mi, ci))
			case "tool_result":
				body = sanitizeIDField(body, block.Get("tool_use_id").String(), fmt.Sprintf("messages.%d.content.%d.tool_use_id", mi, ci))
			}
		}
	}
	return body
}

func sanitizeIDField(body []byte, id, path string) []byte {
	if id == "" {
		return body
	}
	sanitized := bedrockToolUseIDRe.ReplaceAllString(id, "_")
	if sanitized != id {
		body, _ = sjson.SetBytes(body, path, sanitized)
	}
	return body
}

const defaultCCMaxTokens = 81920

// sanitizeBedrockCCFields
//   -
//   -
//   - +
//   -
//   -
func sanitizeBedrockCCFields(body []byte) []byte {
	if gjson.GetBytes(body, "service_tier").Exists() {
		body, _ = sjson.DeleteBytes(body, "service_tier")
	}
	if gjson.GetBytes(body, "interface_geo").Exists() {
		body, _ = sjson.DeleteBytes(body, "interface_geo")
	}
	if gjson.GetBytes(body, "context_management").Exists() {
		body, _ = sjson.DeleteBytes(body, "context_management")
	}
	if !gjson.GetBytes(body, "max_tokens").Exists() {
		body, _ = sjson.SetBytes(body, "max_tokens", defaultCCMaxTokens)
	}
	if !gjson.GetBytes(body, "anthropic_version").Exists() {
		body, _ = sjson.SetBytes(body, "anthropic_version", "bedrock-2023-05-31")
	}
	return body
}

// sanitizeBedrockCCBetaTokens
// CC
func sanitizeBedrockCCBetaTokens(body []byte, modelID string) []byte {
	betaField := gjson.GetBytes(body, "anthropic_beta")
	if !betaField.Exists() {
		return body
	}

	var tokens []string
	if betaField.IsArray() {
		for _, t := range betaField.Array() {
			if t.Type == gjson.String {
				tokens = append(tokens, t.String())
			}
		}
	}

	originalTokens := append([]string(nil), tokens...) // 保存原始 tokens 用于日志

	// + +
	//
	tokens = autoInjectBedrockBetaTokens(tokens, body, modelID)
	tokens = filterBedrockBetaTokens(tokens)

	if len(tokens) == 0 {
		//
		body, _ = sjson.DeleteBytes(body, "anthropic_beta")
		logger.LegacyPrintf("service.gateway", "[Bedrock CC Compat] Removed all beta tokens: original=%v", originalTokens)
	} else {
		//
		body, _ = sjson.SetBytes(body, "anthropic_beta", tokens)
		if len(originalTokens) > 0 {
			logger.LegacyPrintf("service.gateway", "[Bedrock CC Compat] Filtered beta tokens: original=%v final=%v", originalTokens, tokens)
		} else {
			logger.LegacyPrintf("service.gateway", "[Bedrock CC Compat] Auto-injected beta tokens: %v", tokens)
		}
	}

	return body
}
