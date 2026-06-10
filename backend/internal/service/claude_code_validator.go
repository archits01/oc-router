package service

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// ClaudeCodeValidator
//
type ClaudeCodeValidator struct{}

var (
	// User-Agent ()
	claudeCodeUAPattern = regexp.MustCompile(`(?i)^claude-cli/\d+\.\d+\.\d+`)

	claudeCodeUAVersionPattern = regexp.MustCompile(`(?i)^claude-cli/(\d+\.\d+\.\d+)`)

	// System prompt
	systemPromptThreshold = 0.5
)

// Claude Code
//
var claudeCodeSystemPrompts = []string{
	// claudeOtherSystemPrompt1 - Primary
	"You are Claude Code, Anthropic's official CLI for Claude.",

	// claudeOtherSystemPrompt3 - Agent SDK
	"You are a Claude agent, built on Anthropic's Claude Agent SDK.",

	// claudeOtherSystemPrompt4 - Compact Agent SDK
	"You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.",

	// exploreAgentSystemPrompt
	"You are a file search specialist for Claude Code, Anthropic's official CLI for Claude.",

	// claudeOtherSystemPromptCompact - Compact ()
	"You are a helpful AI assistant tasked with summarizing conversations.",

	// claudeOtherSystemPrompt2 - Secondary ()
	"You are an interactive CLI tool that helps users",
}

const (
	// claudeCodeBillingHeaderPrefix
	//
	//
	//
	claudeCodeBillingHeaderPrefix = "x-anthropic-billing-header"
	// claudeCodeCLIEntrypointMarker
	claudeCodeCLIEntrypointMarker = "cc_entrypoint=cli"
)

// NewClaudeCodeValidator
func NewClaudeCodeValidator() *ClaudeCodeValidator {
	return &ClaudeCodeValidator{}
}

// Validate
//
//
//	Step 1: User-Agent () -
//	Step 2:
//	Step 3: =1 + haiku
//	Step 4:
//	        - System prompt
//	        - X-App header
//	        - anthropic-beta header
//	        - anthropic-version header
//	        - metadata.user_id
func (v *ClaudeCodeValidator) Validate(r *http.Request, body map[string]any) bool {
	// Step 1: User-Agent
	ua := r.Header.Get("User-Agent")
	if !claudeCodeUAPattern.MatchString(ua) {
		return false
	}

	// Step 2:
	path := r.URL.Path
	if !strings.Contains(path, "messages") {
		return true
	}

	// count_tokens
	if isMessagesCountTokensPath(path) {
		return true
	}

	// Step 3: =1 + haiku
	//
	if isMaxTokensOneHaiku, ok := IsMaxTokensOneHaikuRequestFromContext(r.Context()); ok && isMaxTokensOneHaiku {
		return true // 绕过 system prompt 检查，UA 已在 Step 1 validation
	}

	// Step 4: messages

	// 4.1
	if !v.hasClaudeCodeSystemPrompt(body) {
		return false
	}

	// 4.2
	xApp := r.Header.Get("X-App")
	if xApp == "" {
		return false
	}

	anthropicBeta := r.Header.Get("anthropic-beta")
	if anthropicBeta == "" {
		return false
	}

	anthropicVersion := r.Header.Get("anthropic-version")
	if anthropicVersion == "" {
		return false
	}

	// 4.3
	if body == nil {
		return false
	}

	metadata, ok := body["metadata"].(map[string]any)
	if !ok {
		return false
	}

	userID, ok := metadata["user_id"].(string)
	if !ok || userID == "" {
		return false
	}

	if ParseMetadataUserID(userID) == nil {
		return false
	}

	return true
}

func isMessagesCountTokensPath(path string) bool {
	return strings.HasSuffix(path, "/messages/count_tokens")
}

// hasClaudeCodeSystemPrompt
//
func (v *ClaudeCodeValidator) hasClaudeCodeSystemPrompt(body map[string]any) bool {
	if body == nil {
		return false
	}

	//
	if _, ok := body["model"].(string); !ok {
		return false
	}

	//
	systemEntries, ok := body["system"].([]any)
	if !ok {
		return false
	}

	//
	for _, entry := range systemEntries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}

		text, ok := entryMap["text"].(string)
		if !ok || text == "" {
			continue
		}

		//
		//
		if strings.HasPrefix(text, claudeCodeBillingHeaderPrefix) &&
			strings.Contains(text, claudeCodeCLIEntrypointMarker) {
			return true
		}

		bestScore := v.bestSimilarityScore(text)
		if bestScore >= systemPromptThreshold {
			return true
		}
	}

	return false
}

// bestSimilarityScore
func (v *ClaudeCodeValidator) bestSimilarityScore(text string) float64 {
	normalizedText := normalizePrompt(text)
	bestScore := 0.0

	for _, template := range claudeCodeSystemPrompts {
		normalizedTemplate := normalizePrompt(template)
		score := diceCoefficient(normalizedText, normalizedTemplate)
		if score > bestScore {
			bestScore = score
		}
	}

	return bestScore
}

// normalizePrompt
func normalizePrompt(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// diceCoefficient –Dice coefficient）
//
// * |intersection| / (|bigrams(a)| + |bigrams(b)|)
func diceCoefficient(a, b string) float64 {
	if a == b {
		return 1.0
	}

	if len(a) < 2 || len(b) < 2 {
		return 0.0
	}

	//
	bigramsA := getBigrams(a)
	bigramsB := getBigrams(b)

	if len(bigramsA) == 0 || len(bigramsB) == 0 {
		return 0.0
	}

	intersection := 0
	for bigram, countA := range bigramsA {
		if countB, exists := bigramsB[bigram]; exists {
			if countA < countB {
				intersection += countA
			} else {
				intersection += countB
			}
		}
	}

	//
	totalA := 0
	for _, count := range bigramsA {
		totalA += count
	}
	totalB := 0
	for _, count := range bigramsB {
		totalB += count
	}

	return float64(2*intersection) / float64(totalA+totalB)
}

// getBigrams
func getBigrams(s string) map[string]int {
	bigrams := make(map[string]int)
	runes := []rune(strings.ToLower(s))

	for i := 0; i < len(runes)-1; i++ {
		bigram := string(runes[i : i+2])
		bigrams[bigram]++
	}

	return bigrams
}

// ValidateUserAgent
func (v *ClaudeCodeValidator) ValidateUserAgent(ua string) bool {
	return claudeCodeUAPattern.MatchString(ua)
}

// IncludesClaudeCodeSystemPrompt
//
func (v *ClaudeCodeValidator) IncludesClaudeCodeSystemPrompt(body map[string]any) bool {
	return v.hasClaudeCodeSystemPrompt(body)
}

// IsClaudeCodeClient
func IsClaudeCodeClient(ctx context.Context) bool {
	if v, ok := ctx.Value(ctxkey.IsClaudeCodeClient).(bool); ok {
		return v
	}
	return false
}

// SetClaudeCodeClient
func SetClaudeCodeClient(ctx context.Context, isClaudeCode bool) context.Context {
	return context.WithValue(ctx, ctxkey.IsClaudeCodeClient, isClaudeCode)
}

// ExtractVersion
// "2.1.22"
func (v *ClaudeCodeValidator) ExtractVersion(ua string) string {
	return ExtractCLIVersion(ua)
}

// SetClaudeCodeVersion
func SetClaudeCodeVersion(ctx context.Context, version string) context.Context {
	return context.WithValue(ctx, ctxkey.ClaudeCodeVersion, version)
}

// GetClaudeCodeVersion
func GetClaudeCodeVersion(ctx context.Context) string {
	if v, ok := ctx.Value(ctxkey.ClaudeCodeVersion).(string); ok {
		return v
	}
	return ""
}

// CompareVersions
// (a < b), 0 (a == b), 1 (a > b)
func CompareVersions(a, b string) int {
	aParts := parseSemver(a)
	bParts := parseSemver(b)
	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}
	return 0
}

// parseSemver [major, minor, patch]
func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	result := [3]int{0, 0, 0}
	for i := 0; i < len(parts) && i < 3; i++ {
		if parsed, err := strconv.Atoi(parts[i]); err == nil {
			result[i] = parsed
		}
	}
	return result
}
