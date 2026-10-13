package service

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

const openAIForwardFallbackMessageMaxBytes = 512

// RecordOpenAIForwardFallbackError preserves the internal cause when the
// Responses handler must synthesize a generic client-facing failure.
func RecordOpenAIForwardFallbackError(c *gin.Context, cause error) {
	if c == nil || cause == nil {
		return
	}

	kind, message, record := classifyOpenAIForwardFallbackError(cause)
	if !record {
		return
	}

	setOpsUpstreamError(c, 0, message, "")
	if hasMatchingOpsUpstreamError(c, kind, message) {
		return
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform: PlatformOpenAI,
		Kind:     kind,
		Message:  message,
	})
}

func classifyOpenAIForwardFallbackError(cause error) (kind, message string, record bool) {
	if cause == nil || errors.Is(cause, context.Canceled) {
		return "", "", false
	}

	lower := strings.ToLower(strings.TrimSpace(cause.Error()))
	if strings.Contains(lower, "after disconnect") ||
		strings.Contains(lower, "client disconnected") ||
		strings.Contains(lower, "context canceled") {
		return "", "", false
	}

	switch {
	case errors.Is(cause, context.DeadlineExceeded) || strings.Contains(lower, "context deadline exceeded"):
		kind = "stream_deadline_exceeded"
		message = "stream deadline exceeded"
	case strings.Contains(lower, "missing terminal event") || strings.Contains(lower, "ended before a terminal event"):
		kind = "stream_missing_terminal"
		message = "stream usage incomplete: missing terminal event"
	case strings.HasPrefix(lower, "stream read error") || strings.Contains(lower, "unexpected eof"):
		kind = "stream_read_error"
		detail := sanitizeStreamError(cause)
		if detail == "" {
			detail = "upstream connection error"
		}
		message = "stream read error: " + detail
	case strings.HasPrefix(lower, "upstream response failed"):
		kind = "upstream_response_failed"
		message = "upstream response failed"
	default:
		kind = "forward_error"
		message = "Upstream request failed"
	}
	message = truncateString(message, openAIForwardFallbackMessageMaxBytes)
	return kind, message, true
}

func hasMatchingOpsUpstreamError(c *gin.Context, kind, message string) bool {
	raw, ok := c.Get(OpsUpstreamErrorsKey)
	if !ok {
		return false
	}
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	if !ok {
		return false
	}
	for _, event := range events {
		if event != nil && event.Kind == kind && event.Message == message {
			return true
		}
	}
	return false
}
