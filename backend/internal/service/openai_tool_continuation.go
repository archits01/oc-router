package service

import (
	"strings"

	"github.com/tidwall/gjson"
)

// ToolContinuationSignals
type ToolContinuationSignals struct {
	HasFunctionCallOutput              bool
	HasFunctionCallOutputMissingCallID bool
	HasToolCallContext                 bool
	HasItemReference                   bool
	HasItemReferenceForAllCallIDs      bool
	FunctionCallOutputCallIDs          []string
}

// FunctionCallOutputValidation
type FunctionCallOutputValidation struct {
	HasFunctionCallOutput              bool
	HasToolCallContext                 bool
	HasFunctionCallOutputMissingCallID bool
	HasItemReferenceForAllCallIDs      bool
}

func isCodexToolCallContextItemType(typ string) bool {
	switch strings.TrimSpace(typ) {
	case "tool_call",
		"function_call",
		"local_shell_call",
		"tool_search_call",
		"custom_tool_call",
		"mcp_tool_call":
		return true
	default:
		return false
	}
}

func isCodexToolCallOutputItemType(typ string) bool {
	switch strings.TrimSpace(typ) {
	case "function_call_output",
		"tool_search_output",
		"custom_tool_call_output",
		"mcp_tool_call_output":
		return true
	default:
		return false
	}
}

// NeedsToolContinuation
//
//
func NeedsToolContinuation(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	if hasNonEmptyString(reqBody["previous_response_id"]) {
		return true
	}
	if hasToolsSignal(reqBody) {
		return true
	}
	if hasToolChoiceSignal(reqBody) {
		return true
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if isCodexToolCallItemType(itemType) || itemType == "item_reference" {
			return true
		}
	}
	return false
}

// AnalyzeToolContinuationSignals
//
// （function_call_output/tool_search_output/custom_tool_call_output/mcp_tool_call_output）。
func AnalyzeToolContinuationSignals(reqBody map[string]any) ToolContinuationSignals {
	signals := ToolContinuationSignals{}
	if reqBody == nil {
		return signals
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return signals
	}

	var callIDs map[string]struct{}
	var referenceIDs map[string]struct{}

	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		switch {
		case isCodexToolCallContextItemType(itemType):
			callID, _ := itemMap["call_id"].(string)
			if strings.TrimSpace(callID) != "" {
				signals.HasToolCallContext = true
			}
		case isCodexToolCallOutputItemType(itemType):
			signals.HasFunctionCallOutput = true
			callID, _ := itemMap["call_id"].(string)
			callID = strings.TrimSpace(callID)
			if callID == "" {
				signals.HasFunctionCallOutputMissingCallID = true
				continue
			}
			if callIDs == nil {
				callIDs = make(map[string]struct{})
			}
			callIDs[callID] = struct{}{}
		case itemType == "item_reference":
			signals.HasItemReference = true
			idValue, _ := itemMap["id"].(string)
			idValue = strings.TrimSpace(idValue)
			if idValue == "" {
				continue
			}
			if referenceIDs == nil {
				referenceIDs = make(map[string]struct{})
			}
			referenceIDs[idValue] = struct{}{}
		}
	}

	if len(callIDs) == 0 {
		return signals
	}
	signals.FunctionCallOutputCallIDs = make([]string, 0, len(callIDs))
	allReferenced := len(referenceIDs) > 0
	for callID := range callIDs {
		signals.FunctionCallOutputCallIDs = append(signals.FunctionCallOutputCallIDs, callID)
		if allReferenced {
			if _, ok := referenceIDs[callID]; !ok {
				allReferenced = false
			}
		}
	}
	signals.HasItemReferenceForAllCallIDs = allReferenced
	return signals
}

// ValidateFunctionCallOutputContextBytes
func ValidateFunctionCallOutputContextBytes(body []byte) FunctionCallOutputValidation {
	result := FunctionCallOutputValidation{}
	if len(body) == 0 {
		return result
	}
	// handler
	input := parseRawJSONView(body).Get("input")
	if !input.IsArray() {
		return result
	}

	var callIDs map[string]struct{}
	var referenceIDs map[string]struct{}
	input.ForEach(func(_, item gjson.Result) bool {
		if !item.IsObject() {
			return true
		}
		itemType := item.Get("type").String()
		switch {
		case isCodexToolCallOutputItemType(itemType):
			result.HasFunctionCallOutput = true
			callID := strings.TrimSpace(item.Get("call_id").String())
			if callID == "" {
				result.HasFunctionCallOutputMissingCallID = true
				return true
			}
			if callIDs == nil {
				callIDs = make(map[string]struct{})
			}
			callIDs[callID] = struct{}{}
		case isCodexToolCallContextItemType(itemType):
			if strings.TrimSpace(item.Get("call_id").String()) != "" {
				result.HasToolCallContext = true
			}
		case itemType == "item_reference":
			idValue := strings.TrimSpace(item.Get("id").String())
			if idValue == "" {
				return true
			}
			if referenceIDs == nil {
				referenceIDs = make(map[string]struct{})
			}
			referenceIDs[idValue] = struct{}{}
		}
		return !result.HasFunctionCallOutput || !result.HasToolCallContext
	})
	if !result.HasFunctionCallOutput || result.HasToolCallContext || len(callIDs) == 0 || len(referenceIDs) == 0 {
		return result
	}
	allReferenced := true
	for callID := range callIDs {
		if _, ok := referenceIDs[callID]; !ok {
			allReferenced = false
			break
		}
	}
	result.HasItemReferenceForAllCallIDs = allReferenced
	return result
}

// ValidateFunctionCallOutputContext
// 3)
//
func ValidateFunctionCallOutputContext(reqBody map[string]any) FunctionCallOutputValidation {
	result := FunctionCallOutputValidation{}
	if reqBody == nil {
		return result
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return result
	}

	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		switch {
		case isCodexToolCallOutputItemType(itemType):
			result.HasFunctionCallOutput = true
		case isCodexToolCallContextItemType(itemType):
			callID, _ := itemMap["call_id"].(string)
			if strings.TrimSpace(callID) != "" {
				result.HasToolCallContext = true
			}
		}
		if result.HasFunctionCallOutput && result.HasToolCallContext {
			return result
		}
	}

	if !result.HasFunctionCallOutput || result.HasToolCallContext {
		return result
	}

	callIDs := make(map[string]struct{})
	referenceIDs := make(map[string]struct{})
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		switch {
		case isCodexToolCallOutputItemType(itemType):
			callID, _ := itemMap["call_id"].(string)
			callID = strings.TrimSpace(callID)
			if callID == "" {
				result.HasFunctionCallOutputMissingCallID = true
				continue
			}
			callIDs[callID] = struct{}{}
		case itemType == "item_reference":
			idValue, _ := itemMap["id"].(string)
			idValue = strings.TrimSpace(idValue)
			if idValue == "" {
				continue
			}
			referenceIDs[idValue] = struct{}{}
		}
	}

	if len(callIDs) == 0 || len(referenceIDs) == 0 {
		return result
	}
	allReferenced := true
	for callID := range callIDs {
		if _, ok := referenceIDs[callID]; !ok {
			allReferenced = false
			break
		}
	}
	result.HasItemReferenceForAllCallIDs = allReferenced
	return result
}

// HasFunctionCallOutput
//
func HasFunctionCallOutput(reqBody map[string]any) bool {
	return AnalyzeToolContinuationSignals(reqBody).HasFunctionCallOutput
}

// HasToolCallContext
func HasToolCallContext(reqBody map[string]any) bool {
	return AnalyzeToolContinuationSignals(reqBody).HasToolCallContext
}

// FunctionCallOutputCallIDs
//
func FunctionCallOutputCallIDs(reqBody map[string]any) []string {
	return AnalyzeToolContinuationSignals(reqBody).FunctionCallOutputCallIDs
}

// HasFunctionCallOutputMissingCallID
func HasFunctionCallOutputMissingCallID(reqBody map[string]any) bool {
	return AnalyzeToolContinuationSignals(reqBody).HasFunctionCallOutputMissingCallID
}

// HasItemReferenceForCallIDs
func HasItemReferenceForCallIDs(reqBody map[string]any, callIDs []string) bool {
	if reqBody == nil || len(callIDs) == 0 {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	referenceIDs := make(map[string]struct{})
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "item_reference" {
			continue
		}
		idValue, _ := itemMap["id"].(string)
		idValue = strings.TrimSpace(idValue)
		if idValue == "" {
			continue
		}
		referenceIDs[idValue] = struct{}{}
	}
	if len(referenceIDs) == 0 {
		return false
	}
	for _, callID := range callIDs {
		if _, ok := referenceIDs[strings.TrimSpace(callID)]; !ok {
			return false
		}
	}
	return true
}

// hasNonEmptyString
func hasNonEmptyString(value any) bool {
	stringValue, ok := value.(string)
	return ok && strings.TrimSpace(stringValue) != ""
}

// hasToolsSignal
func hasToolsSignal(reqBody map[string]any) bool {
	raw, exists := reqBody["tools"]
	if !exists || raw == nil {
		return false
	}
	if tools, ok := raw.([]any); ok {
		return len(tools) > 0
	}
	return false
}

// hasToolChoiceSignal
func hasToolChoiceSignal(reqBody map[string]any) bool {
	raw, exists := reqBody["tool_choice"]
	if !exists || raw == nil {
		return false
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value) != ""
	case map[string]any:
		return len(value) > 0
	default:
		return false
	}
}
