package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
)

// fingerprintSalt
//
//
//
//
const fingerprintSalt = "59cf53e54c78"

// computeClaudeCodeFingerprint
//
//  1. =user
//  2. '0'
//  3. SHA256(SALT + chars + cc_version)
//
//
// =X.Y.Z.{fp}
func computeClaudeCodeFingerprint(body []byte, version string) string {
	firstText := extractFirstUserText(body)
	indices := []int{4, 7, 20}
	chars := make([]byte, 0, 3)
	for _, i := range indices {
		if i < len(firstText) {
			chars = append(chars, firstText[i])
		} else {
			chars = append(chars, '0')
		}
	}
	sum := sha256.Sum256([]byte(fingerprintSalt + string(chars) + version))
	return hex.EncodeToString(sum[:])[:3]
}

// extractFirstUserText
// []block
func extractFirstUserText(body []byte) string {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return ""
	}
	first := ""
	messages.ForEach(func(_, msg gjson.Result) bool {
		if msg.Get("role").String() != "user" {
			return true
		}
		content := msg.Get("content")
		if content.Type == gjson.String {
			first = content.String()
			return false
		}
		if content.IsArray() {
			content.ForEach(func(_, block gjson.Result) bool {
				if block.Get("type").String() == "text" {
					first = block.Get("text").String()
					return false
				}
				return true
			})
			return false
		}
		return false
	})
	return first
}

// buildBillingAttributionBlockJSON
//
//
//
//	{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.161.{fp}; cc_entrypoint=cli; cch=00000;"}
//
// cch=00000
//
//
//
// Claude Code prompt block
func buildBillingAttributionBlockJSON(body []byte, cliVersion string) ([]byte, error) {
	if cliVersion == "" {
		return nil, fmt.Errorf("cliVersion required")
	}
	fp := computeClaudeCodeFingerprint(body, cliVersion)
	text := fmt.Sprintf(
		"x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=cli; cch=00000;",
		cliVersion, fp,
	)
	return json.Marshal(map[string]string{
		"type": "text",
		"text": text,
	})
}
