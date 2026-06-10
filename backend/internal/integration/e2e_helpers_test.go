//go:build e2e

package integration

import (
	"os"
	"strings"
	"testing"
)

// =============================================================================
// E2E Mock
// =============================================================================
// =true
//

// isMockMode
func isMockMode() bool {
	return strings.EqualFold(os.Getenv("E2E_MOCK"), "true")
}

// skipIfNoRealAPI
func skipIfNoRealAPI(t *testing.T) {
	t.Helper()
	if isMockMode() {
		return // do not skip in Mock mode
	}
	claudeKey := strings.TrimSpace(os.Getenv(claudeAPIKeyEnv))
	geminiKey := strings.TrimSpace(os.Getenv(geminiAPIKeyEnv))
	if claudeKey == "" && geminiKey == "" {
		t.Skip("not set API Key and Mock mode not enabled, skipping tests")
	}
}

// =============================================================================
// API Key
// =============================================================================

// safeLogKey
func safeLogKey(t *testing.T, prefix string, key string) {
	t.Helper()
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		t.Logf("%s: ***（length: %d）", prefix, len(key))
		return
	}
	t.Logf("%s: %s...（length: %d）", prefix, key[:8], len(key))
}
