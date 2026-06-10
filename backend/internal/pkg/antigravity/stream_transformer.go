package antigravity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// BlockType
type BlockType int

const (
	BlockTypeNone BlockType = iota
	BlockTypeText
	BlockTypeThinking
	BlockTypeFunction
)

// UsageMapHook is a callback that can modify usage data before it's emitted in SSE events.
type UsageMapHook func(usageMap map[string]any)

// StreamingProcessor
type StreamingProcessor struct {
	blockType         BlockType
	blockIndex        int
	messageStartSent  bool
	messageStopSent   bool
	usedTool          bool
	pendingSignature  string
	trailingSignature string
	originalModel     string
	webSearchQueries  []string
	groundingChunks   []GeminiGroundingChunk
	usageMapHook      UsageMapHook

	//
	inputTokens       int
	outputTokens      int
	cacheReadTokens   int
	imageOutputTokens int
}

// NewStreamingProcessor
func NewStreamingProcessor(originalModel string) *StreamingProcessor {
	return &StreamingProcessor{
		blockType:     BlockTypeNone,
		originalModel: originalModel,
	}
}

// SetUsageMapHook sets an optional hook that modifies usage maps before they are emitted.
func (p *StreamingProcessor) SetUsageMapHook(fn UsageMapHook) {
	p.usageMapHook = fn
}

func usageToMap(u ClaudeUsage) map[string]any {
	m := map[string]any{
		"input_tokens":  u.InputTokens,
		"output_tokens": u.OutputTokens,
	}
	if u.CacheCreationInputTokens > 0 {
		m["cache_creation_input_tokens"] = u.CacheCreationInputTokens
	}
	if u.CacheReadInputTokens > 0 {
		m["cache_read_input_tokens"] = u.CacheReadInputTokens
	}
	if u.ImageOutputTokens > 0 {
		m["image_output_tokens"] = u.ImageOutputTokens
	}
	return m
}

// ProcessLine
func (p *StreamingProcessor) ProcessLine(line string) []byte {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "data:") {
		return nil
	}

	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "" || data == "[DONE]" {
		return nil
	}

	//
	var v1Resp V1InternalResponse
	if err := json.Unmarshal([]byte(data), &v1Resp); err != nil {
		//
		var directResp GeminiResponse
		if err2 := json.Unmarshal([]byte(data), &directResp); err2 != nil {
			return nil
		}
		v1Resp.Response = directResp
		v1Resp.ResponseID = directResp.ResponseID
		v1Resp.ModelVersion = directResp.ModelVersion
	}

	geminiResp := &v1Resp.Response

	var result bytes.Buffer

	//
	if !p.messageStartSent {
		_, _ = result.Write(p.emitMessageStart(&v1Resp))
	}

	//
	//
	//
	if geminiResp.UsageMetadata != nil {
		cached := geminiResp.UsageMetadata.CachedContentTokenCount
		p.inputTokens = geminiResp.UsageMetadata.PromptTokenCount - cached
		p.outputTokens = geminiResp.UsageMetadata.CandidatesTokenCount + geminiResp.UsageMetadata.ThoughtsTokenCount
		p.cacheReadTokens = cached
		p.imageOutputTokens = geminiResp.UsageMetadata.ImageOutputTokens()
	}

	//
	if len(geminiResp.Candidates) > 0 && geminiResp.Candidates[0].Content != nil {
		for _, part := range geminiResp.Candidates[0].Content.Parts {
			_, _ = result.Write(p.processPart(&part))
		}
	}

	if len(geminiResp.Candidates) > 0 {
		p.captureGrounding(geminiResp.Candidates[0].GroundingMetadata)
	}

	if len(geminiResp.Candidates) > 0 {
		finishReason := geminiResp.Candidates[0].FinishReason
		if finishReason == "MALFORMED_FUNCTION_CALL" {
			log.Printf("[Antigravity] MALFORMED_FUNCTION_CALL detected in stream for model %s", p.originalModel)
			if geminiResp.Candidates[0].Content != nil {
				if b, err := json.Marshal(geminiResp.Candidates[0].Content); err == nil {
					log.Printf("[Antigravity] Malformed content: %s", string(b))
				}
			}
		}
		if finishReason != "" {
			_, _ = result.Write(p.emitFinish(finishReason))
		}
	}

	return result.Bytes()
}

// Finish
// == false），
//
func (p *StreamingProcessor) Finish() ([]byte, *ClaudeUsage) {
	usage := &ClaudeUsage{
		InputTokens:          p.inputTokens,
		OutputTokens:         p.outputTokens,
		CacheReadInputTokens: p.cacheReadTokens,
		ImageOutputTokens:    p.imageOutputTokens,
	}

	if !p.messageStartSent {
		return nil, usage
	}

	var result bytes.Buffer
	if !p.messageStopSent {
		_, _ = result.Write(p.emitFinish(""))
	}

	return result.Bytes(), usage
}

// MessageStartSent
func (p *StreamingProcessor) MessageStartSent() bool {
	return p.messageStartSent
}

// emitMessageStart
func (p *StreamingProcessor) emitMessageStart(v1Resp *V1InternalResponse) []byte {
	if p.messageStartSent {
		return nil
	}

	usage := ClaudeUsage{}
	if v1Resp.Response.UsageMetadata != nil {
		cached := v1Resp.Response.UsageMetadata.CachedContentTokenCount
		usage.InputTokens = v1Resp.Response.UsageMetadata.PromptTokenCount - cached
		usage.OutputTokens = v1Resp.Response.UsageMetadata.CandidatesTokenCount + v1Resp.Response.UsageMetadata.ThoughtsTokenCount
		usage.CacheReadInputTokens = cached
		usage.ImageOutputTokens = v1Resp.Response.UsageMetadata.ImageOutputTokens()
	}

	responseID := v1Resp.ResponseID
	if responseID == "" {
		responseID = v1Resp.Response.ResponseID
	}
	if responseID == "" {
		responseID = "msg_" + generateRandomID()
	}

	var usageValue any = usage
	if p.usageMapHook != nil {
		usageMap := usageToMap(usage)
		p.usageMapHook(usageMap)
		usageValue = usageMap
	}

	message := map[string]any{
		"id":            responseID,
		"type":          "message",
		"role":          "assistant",
		"content":       []any{},
		"model":         p.originalModel,
		"stop_reason":   nil,
		"stop_sequence": nil,
		"usage":         usageValue,
	}

	event := map[string]any{
		"type":    "message_start",
		"message": message,
	}

	p.messageStartSent = true
	return p.formatSSE("message_start", event)
}

// processPart
func (p *StreamingProcessor) processPart(part *GeminiPart) []byte {
	var result bytes.Buffer
	signature := part.ThoughtSignature

	// 1. FunctionCall
	if part.FunctionCall != nil {
		//
		if p.trailingSignature != "" {
			_, _ = result.Write(p.endBlock())
			_, _ = result.Write(p.emitEmptyThinkingWithSignature(p.trailingSignature))
			p.trailingSignature = ""
		}

		_, _ = result.Write(p.processFunctionCall(part.FunctionCall, signature))
		return result.Bytes()
	}

	// 2. Text
	if part.Text != "" || part.Thought {
		if part.Thought {
			_, _ = result.Write(p.processThinking(part.Text, signature))
		} else {
			_, _ = result.Write(p.processText(part.Text, signature))
		}
	}

	// 3. InlineData (Image)
	if part.InlineData != nil && part.InlineData.Data != "" {
		markdownImg := fmt.Sprintf("![image](data:%s;base64,%s)",
			part.InlineData.MimeType, part.InlineData.Data)
		_, _ = result.Write(p.processText(markdownImg, ""))
	}

	return result.Bytes()
}

func (p *StreamingProcessor) captureGrounding(grounding *GeminiGroundingMetadata) {
	if grounding == nil {
		return
	}

	if len(grounding.WebSearchQueries) > 0 && len(p.webSearchQueries) == 0 {
		p.webSearchQueries = append([]string(nil), grounding.WebSearchQueries...)
	}

	if len(grounding.GroundingChunks) > 0 && len(p.groundingChunks) == 0 {
		p.groundingChunks = append([]GeminiGroundingChunk(nil), grounding.GroundingChunks...)
	}
}

// processThinking
func (p *StreamingProcessor) processThinking(text, signature string) []byte {
	var result bytes.Buffer

	//
	if p.trailingSignature != "" {
		_, _ = result.Write(p.endBlock())
		_, _ = result.Write(p.emitEmptyThinkingWithSignature(p.trailingSignature))
		p.trailingSignature = ""
	}

	//
	if p.blockType != BlockTypeThinking {
		_, _ = result.Write(p.startBlock(BlockTypeThinking, map[string]any{
			"type":     "thinking",
			"thinking": "",
		}))
	}

	if text != "" {
		_, _ = result.Write(p.emitDelta("thinking_delta", map[string]any{
			"thinking": text,
		}))
	}

	if signature != "" {
		p.pendingSignature = signature
	}

	return result.Bytes()
}

// processText
func (p *StreamingProcessor) processText(text, signature string) []byte {
	var result bytes.Buffer

	//
	if text == "" {
		if signature != "" {
			p.trailingSignature = signature
		}
		return nil
	}

	//
	if p.trailingSignature != "" {
		_, _ = result.Write(p.endBlock())
		_, _ = result.Write(p.emitEmptyThinkingWithSignature(p.trailingSignature))
		p.trailingSignature = ""
	}

	//
	if signature != "" {
		_, _ = result.Write(p.startBlock(BlockTypeText, map[string]any{
			"type": "text",
			"text": "",
		}))
		_, _ = result.Write(p.emitDelta("text_delta", map[string]any{
			"text": text,
		}))
		_, _ = result.Write(p.endBlock())
		_, _ = result.Write(p.emitEmptyThinkingWithSignature(signature))
		return result.Bytes()
	}

	// ()
	if p.blockType != BlockTypeText {
		_, _ = result.Write(p.startBlock(BlockTypeText, map[string]any{
			"type": "text",
			"text": "",
		}))
	}

	_, _ = result.Write(p.emitDelta("text_delta", map[string]any{
		"text": text,
	}))

	return result.Bytes()
}

// processFunctionCall
func (p *StreamingProcessor) processFunctionCall(fc *GeminiFunctionCall, signature string) []byte {
	var result bytes.Buffer

	p.usedTool = true

	toolID := fc.ID
	if toolID == "" {
		toolID = fmt.Sprintf("%s-%s", fc.Name, generateRandomID())
	}

	toolUse := map[string]any{
		"type":  "tool_use",
		"id":    toolID,
		"name":  fc.Name,
		"input": map[string]any{},
	}

	if signature != "" {
		toolUse["signature"] = signature
	}

	_, _ = result.Write(p.startBlock(BlockTypeFunction, toolUse))

	//
	if fc.Args != nil {
		argsJSON, _ := json.Marshal(fc.Args)
		_, _ = result.Write(p.emitDelta("input_json_delta", map[string]any{
			"partial_json": string(argsJSON),
		}))
	}

	_, _ = result.Write(p.endBlock())

	return result.Bytes()
}

// startBlock
func (p *StreamingProcessor) startBlock(blockType BlockType, contentBlock map[string]any) []byte {
	var result bytes.Buffer

	if p.blockType != BlockTypeNone {
		_, _ = result.Write(p.endBlock())
	}

	event := map[string]any{
		"type":          "content_block_start",
		"index":         p.blockIndex,
		"content_block": contentBlock,
	}

	_, _ = result.Write(p.formatSSE("content_block_start", event))
	p.blockType = blockType

	return result.Bytes()
}

// endBlock
func (p *StreamingProcessor) endBlock() []byte {
	if p.blockType == BlockTypeNone {
		return nil
	}

	var result bytes.Buffer

	// Thinking
	if p.blockType == BlockTypeThinking && p.pendingSignature != "" {
		_, _ = result.Write(p.emitDelta("signature_delta", map[string]any{
			"signature": p.pendingSignature,
		}))
		p.pendingSignature = ""
	}

	event := map[string]any{
		"type":  "content_block_stop",
		"index": p.blockIndex,
	}

	_, _ = result.Write(p.formatSSE("content_block_stop", event))

	p.blockIndex++
	p.blockType = BlockTypeNone

	return result.Bytes()
}

// emitDelta
func (p *StreamingProcessor) emitDelta(deltaType string, deltaContent map[string]any) []byte {
	delta := map[string]any{
		"type": deltaType,
	}
	for k, v := range deltaContent {
		delta[k] = v
	}

	event := map[string]any{
		"type":  "content_block_delta",
		"index": p.blockIndex,
		"delta": delta,
	}

	return p.formatSSE("content_block_delta", event)
}

// emitEmptyThinkingWithSignature
func (p *StreamingProcessor) emitEmptyThinkingWithSignature(signature string) []byte {
	var result bytes.Buffer

	_, _ = result.Write(p.startBlock(BlockTypeThinking, map[string]any{
		"type":     "thinking",
		"thinking": "",
	}))
	_, _ = result.Write(p.emitDelta("thinking_delta", map[string]any{
		"thinking": "",
	}))
	_, _ = result.Write(p.emitDelta("signature_delta", map[string]any{
		"signature": signature,
	}))
	_, _ = result.Write(p.endBlock())

	return result.Bytes()
}

// emitFinish
func (p *StreamingProcessor) emitFinish(finishReason string) []byte {
	var result bytes.Buffer

	_, _ = result.Write(p.endBlock())

	//
	if p.trailingSignature != "" {
		_, _ = result.Write(p.emitEmptyThinkingWithSignature(p.trailingSignature))
		p.trailingSignature = ""
	}

	if len(p.webSearchQueries) > 0 || len(p.groundingChunks) > 0 {
		groundingText := buildGroundingText(&GeminiGroundingMetadata{
			WebSearchQueries: p.webSearchQueries,
			GroundingChunks:  p.groundingChunks,
		})
		if groundingText != "" {
			_, _ = result.Write(p.startBlock(BlockTypeText, map[string]any{
				"type": "text",
				"text": "",
			}))
			_, _ = result.Write(p.emitDelta("text_delta", map[string]any{
				"text": groundingText,
			}))
			_, _ = result.Write(p.endBlock())
		}
	}

	//
	stopReason := "end_turn"
	if p.usedTool {
		stopReason = "tool_use"
	} else if finishReason == "MAX_TOKENS" {
		stopReason = "max_tokens"
	}

	usage := ClaudeUsage{
		InputTokens:          p.inputTokens,
		OutputTokens:         p.outputTokens,
		CacheReadInputTokens: p.cacheReadTokens,
		ImageOutputTokens:    p.imageOutputTokens,
	}

	var usageValue any = usage
	if p.usageMapHook != nil {
		usageMap := usageToMap(usage)
		p.usageMapHook(usageMap)
		usageValue = usageMap
	}

	deltaEvent := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": usageValue,
	}

	_, _ = result.Write(p.formatSSE("message_delta", deltaEvent))

	if !p.messageStopSent {
		stopEvent := map[string]any{
			"type": "message_stop",
		}
		_, _ = result.Write(p.formatSSE("message_stop", stopEvent))
		p.messageStopSent = true
	}

	return result.Bytes()
}

// formatSSE
func (p *StreamingProcessor) formatSSE(eventType string, data any) []byte {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil
	}

	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(jsonData)))
}
