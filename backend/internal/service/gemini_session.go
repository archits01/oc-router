package service

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/cespare/xxhash/v2"
)

// shortHash + Base36
// XXHash64 %
func shortHash(data []byte) string {
	h := xxhash.Sum64(data)
	return strconv.FormatUint(h, 36)
}

// BuildGeminiDigestChain
// <hash>-u:<hash>-m:<hash>-u:<hash>-...
// s = systemInstruction, u = user, m = model
func BuildGeminiDigestChain(req *antigravity.GeminiRequest) string {
	if req == nil {
		return ""
	}

	var parts []string

	// 1. system instruction
	if req.SystemInstruction != nil && len(req.SystemInstruction.Parts) > 0 {
		partsData, _ := json.Marshal(req.SystemInstruction.Parts)
		parts = append(parts, "s:"+shortHash(partsData))
	}

	// 2. contents
	for _, c := range req.Contents {
		prefix := "u" // user
		if c.Role == "model" {
			prefix = "m"
		}
		partsData, _ := json.Marshal(c.Parts)
		parts = append(parts, prefix+":"+shortHash(partsData))
	}

	return strings.Join(parts, "-")
}

// GenerateGeminiPrefixHash
// + apiKeyID + ip + userAgent + platform + model
//
func GenerateGeminiPrefixHash(userID, apiKeyID int64, ip, userAgent, platform, model string) string {
	normalizedUserAgent := NormalizeSessionUserAgent(userAgent)
	combined := strconv.FormatInt(userID, 10) + ":" +
		strconv.FormatInt(apiKeyID, 10) + ":" +
		ip + ":" +
		normalizedUserAgent + ":" +
		platform + ":" +
		model

	hash := sha256.Sum256([]byte(combined))
	//
	return base64.RawURLEncoding.EncodeToString(hash[:12])
}

// ParseGeminiSessionValue
// {uuid}:{accountID}
func ParseGeminiSessionValue(value string) (uuid string, accountID int64, ok bool) {
	if value == "" {
		return "", 0, false
	}

	// ":" ":"）
	i := strings.LastIndex(value, ":")
	if i <= 0 || i >= len(value)-1 {
		return "", 0, false
	}

	uuid = value[:i]
	accountID, err := strconv.ParseInt(value[i+1:], 10, 64)
	if err != nil {
		return "", 0, false
	}

	return uuid, accountID, true
}

// FormatGeminiSessionValue
// {uuid}:{accountID}
func FormatGeminiSessionValue(uuid string, accountID int64) string {
	return uuid + ":" + strconv.FormatInt(accountID, 10)
}

// geminiDigestSessionKeyPrefix Gemini
const geminiDigestSessionKeyPrefix = "gemini:digest:"

// GenerateGeminiDigestSessionKey
// + uuid
//
func GenerateGeminiDigestSessionKey(prefixHash, uuid string) string {
	prefix := prefixHash
	if len(prefixHash) >= 8 {
		prefix = prefixHash[:8]
	}
	uuidPart := uuid
	if len(uuid) >= 8 {
		uuidPart = uuid[:8]
	}
	return geminiDigestSessionKeyPrefix + prefix + ":" + uuidPart
}
