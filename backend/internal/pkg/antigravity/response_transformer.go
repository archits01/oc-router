package antigravity

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"
)

// TransformGeminiToClaude
func TransformGeminiToClaude(geminiResp []byte, originalModel string) ([]byte, *ClaudeUsage, error) {
	//
	var v1Resp V1InternalResponse
	if err := json.Unmarshal(geminiResp, &v1Resp); err != nil {
		//
		var directResp GeminiResponse
		if err2 := json.Unmarshal(geminiResp, &directResp); err2 != nil {
			return nil, nil, fmt.Errorf("parse gemini response: %w", err)
		}
		v1Resp.Response = directResp
		v1Resp.ResponseID = directResp.ResponseID
		v1Resp.ModelVersion = directResp.ModelVersion
	} else if len(v1Resp.Response.Candidates) == 0 {
		//
		var directResp GeminiResponse
		if err2 := json.Unmarshal(geminiResp, &directResp); err2 != nil {
			return nil, nil, fmt.Errorf("parse gemini response as direct: %w", err2)
		}
		v1Resp.Response = directResp
		v1Resp.ResponseID = directResp.ResponseID
		v1Resp.ModelVersion = directResp.ModelVersion
	}

	processor := NewNonStreamingProcessor()
	claudeResp := processor.Process(&v1Resp.Response, v1Resp.ResponseID, originalModel)

	respBytes, err := json.Marshal(claudeResp)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal claude response: %w", err)
	}

	return respBytes, &claudeResp.Usage, nil
}

// NonStreamingProcessor
type NonStreamingProcessor struct {
	contentBlocks     []ClaudeContentItem
	textBuilder       string
	thinkingBuilder   string
	thinkingSignature string
	trailingSignature string
	hasToolCall       bool
}

// NewNonStreamingProcessor
func NewNonStreamingProcessor() *NonStreamingProcessor {
	return &NonStreamingProcessor{
		contentBlocks: make([]ClaudeContentItem, 0),
	}
}

// Process
func (p *NonStreamingProcessor) Process(geminiResp *GeminiResponse, responseID, originalModel string) *ClaudeResponse {
	//
	var parts []GeminiPart
	if len(geminiResp.Candidates) > 0 && geminiResp.Candidates[0].Content != nil {
		parts = geminiResp.Candidates[0].Content.Parts
	}

	//
	for _, part := range parts {
		p.processPart(&part)
	}

	if len(geminiResp.Candidates) > 0 {
		if grounding := geminiResp.Candidates[0].GroundingMetadata; grounding != nil {
			p.processGrounding(grounding)
		}
	}

	p.flushThinking()
	p.flushText()

	//
	if p.trailingSignature != "" {
		p.contentBlocks = append(p.contentBlocks, ClaudeContentItem{
			Type:      "thinking",
			Thinking:  "",
			Signature: p.trailingSignature,
		})
	}

	return p.buildResponse(geminiResp, responseID, originalModel)
}

// processPart
func (p *NonStreamingProcessor) processPart(part *GeminiPart) {
	signature := part.ThoughtSignature

	// 1. FunctionCall
	if part.FunctionCall != nil {
		p.flushThinking()
		p.flushText()

		//
		if p.trailingSignature != "" {
			p.contentBlocks = append(p.contentBlocks, ClaudeContentItem{
				Type:      "thinking",
				Thinking:  "",
				Signature: p.trailingSignature,
			})
			p.trailingSignature = ""
		}

		p.hasToolCall = true

		//
		toolID := part.FunctionCall.ID
		if toolID == "" {
			toolID = fmt.Sprintf("%s-%s", part.FunctionCall.Name, generateRandomID())
		}

		item := ClaudeContentItem{
			Type:  "tool_use",
			ID:    toolID,
			Name:  part.FunctionCall.Name,
			Input: part.FunctionCall.Args,
		}

		if signature != "" {
			item.Signature = signature
		}

		p.contentBlocks = append(p.contentBlocks, item)
		return
	}

	// 2. Text
	if part.Text != "" || part.Thought {
		if part.Thought {
			// Thinking part
			p.flushText()

			//
			if p.trailingSignature != "" {
				p.flushThinking()
				p.contentBlocks = append(p.contentBlocks, ClaudeContentItem{
					Type:      "thinking",
					Thinking:  "",
					Signature: p.trailingSignature,
				})
				p.trailingSignature = ""
			}

			p.thinkingBuilder += part.Text
			if signature != "" {
				p.thinkingSignature = signature
			}
		} else {
			//
			if part.Text == "" {
				//
				if signature != "" {
					p.trailingSignature = signature
				}
				return
			}

			p.flushThinking()

			//
			if p.trailingSignature != "" {
				p.flushText()
				p.contentBlocks = append(p.contentBlocks, ClaudeContentItem{
					Type:      "thinking",
					Thinking:  "",
					Signature: p.trailingSignature,
				})
				p.trailingSignature = ""
			}

			//
			if signature != "" {
				p.contentBlocks = append(p.contentBlocks, ClaudeContentItem{
					Type: "text",
					Text: part.Text,
				})
				p.contentBlocks = append(p.contentBlocks, ClaudeContentItem{
					Type:      "thinking",
					Thinking:  "",
					Signature: signature,
				})
			} else {
				// () -
				p.textBuilder += part.Text
			}
		}
	}

	// 3. InlineData (Image)
	if part.InlineData != nil && part.InlineData.Data != "" {
		p.flushThinking()
		markdownImg := fmt.Sprintf("![image](data:%s;base64,%s)",
			part.InlineData.MimeType, part.InlineData.Data)
		p.textBuilder += markdownImg
		p.flushText()
	}
}

func (p *NonStreamingProcessor) processGrounding(grounding *GeminiGroundingMetadata) {
	groundingText := buildGroundingText(grounding)
	if groundingText == "" {
		return
	}

	p.flushThinking()
	p.flushText()
	p.textBuilder += groundingText
	p.flushText()
}

// flushText
func (p *NonStreamingProcessor) flushText() {
	if p.textBuilder == "" {
		return
	}

	p.contentBlocks = append(p.contentBlocks, ClaudeContentItem{
		Type: "text",
		Text: p.textBuilder,
	})
	p.textBuilder = ""
}

// flushThinking
func (p *NonStreamingProcessor) flushThinking() {
	if p.thinkingBuilder == "" && p.thinkingSignature == "" {
		return
	}

	p.contentBlocks = append(p.contentBlocks, ClaudeContentItem{
		Type:      "thinking",
		Thinking:  p.thinkingBuilder,
		Signature: p.thinkingSignature,
	})
	p.thinkingBuilder = ""
	p.thinkingSignature = ""
}

// buildResponse
func (p *NonStreamingProcessor) buildResponse(geminiResp *GeminiResponse, responseID, originalModel string) *ClaudeResponse {
	var finishReason string
	if len(geminiResp.Candidates) > 0 {
		finishReason = geminiResp.Candidates[0].FinishReason
		if finishReason == "MALFORMED_FUNCTION_CALL" {
			log.Printf("[Antigravity] MALFORMED_FUNCTION_CALL detected in response for model %s", originalModel)
			if geminiResp.Candidates[0].Content != nil {
				if b, err := json.Marshal(geminiResp.Candidates[0].Content); err == nil {
					log.Printf("[Antigravity] Malformed content: %s", string(b))
				}
			}
		}
	}

	stopReason := "end_turn"
	if p.hasToolCall {
		stopReason = "tool_use"
	} else if finishReason == "MAX_TOKENS" {
		stopReason = "max_tokens"
	}

	//
	//
	usage := ClaudeUsage{}
	if geminiResp.UsageMetadata != nil {
		cached := geminiResp.UsageMetadata.CachedContentTokenCount
		usage.InputTokens = geminiResp.UsageMetadata.PromptTokenCount - cached
		usage.OutputTokens = geminiResp.UsageMetadata.CandidatesTokenCount + geminiResp.UsageMetadata.ThoughtsTokenCount
		usage.CacheReadInputTokens = cached
		usage.ImageOutputTokens = geminiResp.UsageMetadata.ImageOutputTokens()
	}

	respID := responseID
	if respID == "" {
		respID = geminiResp.ResponseID
	}
	if respID == "" {
		respID = "msg_" + generateRandomID()
	}

	return &ClaudeResponse{
		ID:         respID,
		Type:       "message",
		Role:       "assistant",
		Model:      originalModel,
		Content:    p.contentBlocks,
		StopReason: stopReason,
		Usage:      usage,
	}
}

func buildGroundingText(grounding *GeminiGroundingMetadata) string {
	if grounding == nil {
		return ""
	}

	var builder strings.Builder

	if len(grounding.WebSearchQueries) > 0 {
		_, _ = builder.WriteString("\n\n---\nWeb search queries: ")
		_, _ = builder.WriteString(strings.Join(grounding.WebSearchQueries, ", "))
	}

	if len(grounding.GroundingChunks) > 0 {
		var links []string
		for i, chunk := range grounding.GroundingChunks {
			if chunk.Web == nil {
				continue
			}
			title := strings.TrimSpace(chunk.Web.Title)
			if title == "" {
				title = "Source"
			}
			uri := strings.TrimSpace(chunk.Web.URI)
			if uri == "" {
				uri = "#"
			}
			links = append(links, fmt.Sprintf("[%d] [%s](%s)", i+1, title, uri))
		}

		if len(links) > 0 {
			_, _ = builder.WriteString("\n\nSources:\n")
			_, _ = builder.WriteString(strings.Join(links, "\n"))
		}
	}

	return builder.String()
}

// fallbackCounter
var fallbackCounter uint64

// generateRandomID
func generateRandomID() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	id := make([]byte, 12)
	randBytes := make([]byte, 12)
	if _, err := rand.Read(randBytes); err != nil {
		//
		//
		cnt := atomic.AddUint64(&fallbackCounter, 1)
		seed := uint64(time.Now().UnixNano()) ^ cnt
		seed ^= uint64(len(err.Error())) << 32
		for i := range id {
			seed ^= seed << 13
			seed ^= seed >> 7
			seed ^= seed << 17
			id[i] = chars[int(seed)%len(chars)]
		}
		return string(id)
	}
	for i, b := range randBytes {
		id[i] = chars[int(b)%len(chars)]
	}
	return string(id)
}
