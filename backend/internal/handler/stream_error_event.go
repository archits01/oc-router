package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// responsesFailedError
type responsesFailedError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// responsesFailedBody
// Output `[]` `null`。
type responsesFailedBody struct {
	ID     string               `json:"id"`
	Object string               `json:"object"`
	Model  string               `json:"model,omitempty"`
	Status string               `json:"status"`
	Output []any                `json:"output"`
	Error  responsesFailedError `json:"error"`
}

// responsesFailedEvent
//
type responsesFailedEvent struct {
	Type     string              `json:"type"`
	Response responsesFailedBody `json:"response"`
}

// writeResponsesFailedSSE emits a `response.failed` SSE event in the OpenAI
// Responses API protocol after the stream has already started.
//
//
// HTTP 200
// `event: error`
// Codex CLI `response.completed/failed/incomplete/cancelled`
// "stream closed before response.completed"。
//
//
//
//
//
//
//
//
//
func writeResponsesFailedSSE(c *gin.Context, errType, message string) bool {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return false
	}

	payload, err := json.Marshal(responsesFailedEvent{
		Type: "response.failed",
		Response: responsesFailedBody{
			ID:     synthesizeResponseID(c),
			Object: "response",
			Model:  requestModel(c),
			Status: "failed",
			Output: []any{},
			Error: responsesFailedError{
				Code:    mapResponsesErrorCode(errType),
				Message: message,
			},
		},
	})
	if err != nil {
		_ = c.Error(err)
		return true
	}

	if _, err := fmt.Fprintf(c.Writer, "event: response.failed\ndata: %s\n\n", payload); err != nil {
		_ = c.Error(err)
		return true
	}
	flusher.Flush()
	return true
}

// inboundIsResponses
//
// (c) == EndpointResponses
// NormalizeInboundEndpoint "/v1/responses"
//
// ("/responses", ...) ("/responses", ...)
// () "/v1/"
//
//
//   - /v1/responses
//   - /v1/responses/compact
//   - /responses
//   - /responses/compact
//   - /backend-api/codex/responses
//   - /backend-api/codex/responses/compact
func inboundIsResponses(c *gin.Context) bool {
	if c == nil {
		return false
	}
	p := strings.TrimRight(c.FullPath(), "/")
	if p == "" && c.Request != nil && c.Request.URL != nil {
		p = strings.TrimRight(c.Request.URL.Path, "/")
	}
	if p == "" {
		return false
	}
	return strings.HasSuffix(p, "/responses") || strings.Contains(p, "/responses/")
}

// synthesizeResponseID
//
//
func synthesizeResponseID(c *gin.Context) string {
	if c != nil && c.Request != nil {
		if rid, ok := c.Request.Context().Value(ctxkey.RequestID).(string); ok {
			if rid = strings.TrimSpace(rid); rid != "" {
				return "resp_" + strings.ReplaceAll(rid, "-", "")
			}
		}
	}
	return "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// requestModel
// ""；caller
func requestModel(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if v, ok := c.Get(opsModelKey); ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// mapResponsesErrorCode
func mapResponsesErrorCode(errType string) string {
	switch errType {
	case "rate_limit_error":
		return "rate_limit_exceeded"
	case "invalid_request_error":
		return "invalid_request"
	case "permission_error":
		return "permission_denied"
	case "authentication_error":
		return "authentication_failed"
	case "upstream_error":
		return "upstream_error"
	case "server_error", "api_error", "":
		return "server_error"
	default:
		return errType
	}
}
