package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRecordOpenAIForwardFallbackError_ClassifiesSanitizesAndDeduplicates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	cause := fmt.Errorf("stream read error: %w", io.ErrUnexpectedEOF)

	RecordOpenAIForwardFallbackError(c, cause)
	RecordOpenAIForwardFallbackError(c, cause)

	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "stream_read_error", events[0].Kind)
	require.Equal(t, 0, events[0].UpstreamStatusCode)
	require.Equal(t, PlatformOpenAI, events[0].Platform)
	require.Equal(t, "stream read error: unexpected EOF", events[0].Message)
	require.LessOrEqual(t, len(events[0].Message), openAIForwardFallbackMessageMaxBytes)
	_, ok = c.Get(OpsUpstreamStatusCodeKey)
	require.False(t, ok)

	message, ok := c.Get(OpsUpstreamErrorMessageKey)
	require.True(t, ok)
	require.Equal(t, events[0].Message, message)
}

func TestRecordOpenAIForwardFallbackError_DoesNotPersistUnknownRawDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	cause := errors.New("forward failed: Authorization: Bearer secret-token https://example.invalid/?access_token=secret-value")

	RecordOpenAIForwardFallbackError(c, cause)

	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "forward_error", events[0].Kind)
	require.Equal(t, "Upstream request failed", events[0].Message)
	require.NotContains(t, events[0].Message, "secret-token")
	require.NotContains(t, events[0].Message, "secret-value")
}

func TestRecordOpenAIForwardFallbackError_PreservesDistinctExistingEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Kind: "failover", Message: "rate limited"})

	RecordOpenAIForwardFallbackError(c, errors.New("stream usage incomplete: missing terminal event"))

	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 2)
	require.Equal(t, "failover", events[0].Kind)
	require.Equal(t, "stream_missing_terminal", events[1].Kind)
	require.True(t, strings.Contains(events[1].Message, "missing terminal event"))
}

func TestRecordOpenAIForwardFallbackError_SkipsClientCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []error{
		errors.Join(errors.New("stream usage incomplete"), context.Canceled),
		errors.New("stream usage incomplete: context canceled"),
		errors.New("stream usage incomplete after disconnect: unexpected EOF"),
	}
	for _, cause := range tests {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())

		RecordOpenAIForwardFallbackError(c, cause)

		_, ok := c.Get(OpsUpstreamErrorsKey)
		require.False(t, ok)
		_, ok = c.Get(OpsUpstreamErrorMessageKey)
		require.False(t, ok)
	}
}
