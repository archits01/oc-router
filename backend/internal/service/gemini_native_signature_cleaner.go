package service

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

// CleanGeminiNativeThoughtSignatures
//
//
//
//
// CleanGeminiNativeThoughtSignatures replaces thoughtSignature fields with dummy signature
// in Gemini native API requests to avoid cross-account signature validation errors.
//
// When sticky session switches accounts (e.g., original account becomes unavailable),
// thoughtSignatures from the old account will cause validation failures on the new account.
// By replacing with dummy signature, we skip signature validation.
func CleanGeminiNativeThoughtSignatures(body []byte) []byte {
	if len(body) == 0 {
		return body
	}

	//
	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		//
		return body
	}

	//
	replaced := replaceThoughtSignaturesRecursive(data)

	result, err := json.Marshal(replaced)
	if err != nil {
		//
		return body
	}

	return result
}

// replaceThoughtSignaturesRecursive
func replaceThoughtSignaturesRecursive(data any) any {
	switch v := data.(type) {
	case map[string]any:
		//
		result := make(map[string]any, len(v))
		for key, value := range v {
			//
			if key == "thoughtSignature" {
				result[key] = antigravity.DummyThoughtSignature
				continue
			}
			result[key] = replaceThoughtSignaturesRecursive(value)
		}
		return result

	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = replaceThoughtSignaturesRecursive(item)
		}
		return result

	default:
		//
		return v
	}
}
