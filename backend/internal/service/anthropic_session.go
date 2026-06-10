package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// Anthropic
const (
	// anthropicSessionTTLSeconds Anthropic
	anthropicSessionTTLSeconds = 300

	// anthropicDigestSessionKeyPrefix Anthropic
	anthropicDigestSessionKeyPrefix = "anthropic:digest:"
)

// AnthropicSessionTTL
func AnthropicSessionTTL() time.Duration {
	return anthropicSessionTTLSeconds * time.Second
}

// BuildAnthropicDigestChain
// <hash>-u:<hash>-a:<hash>-u:<hash>-...
// s = system, u = user, a = assistant
func BuildAnthropicDigestChain(parsed *ParsedRequest) string {
	if parsed == nil {
		return ""
	}

	var parts []string

	if systemRaw := parsed.SystemRaw(); len(systemRaw) > 0 && string(systemRaw) != "null" {
		parts = append(parts, "s:"+shortHash(canonicalAnthropicDigestJSON(systemRaw)))
	}

	messages := parsed.MessagesRaw()
	if len(messages) > 0 {
		gjson.ParseBytes(messages).ForEach(func(_, msg gjson.Result) bool {
			prefix := rolePrefix(msg.Get("role").String())
			content := msg.Get("content")
			parts = append(parts, prefix+":"+shortHash(canonicalAnthropicDigestJSON([]byte(content.Raw))))
			return true
		})
	}

	return strings.Join(parts, "-")
}

// canonicalAnthropicDigestJSON
func canonicalAnthropicDigestJSON(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return canonical
}

// rolePrefix
func rolePrefix(role string) string {
	switch role {
	case "assistant":
		return "a"
	default:
		return "u"
	}
}

// GenerateAnthropicDigestSessionKey
// + uuid
func GenerateAnthropicDigestSessionKey(prefixHash, uuid string) string {
	prefix := prefixHash
	if len(prefixHash) >= 8 {
		prefix = prefixHash[:8]
	}
	uuidPart := uuid
	if len(uuid) >= 8 {
		uuidPart = uuid[:8]
	}
	return anthropicDigestSessionKeyPrefix + prefix + ":" + uuidPart
}
