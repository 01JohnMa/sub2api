package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIEnsureForwardErrorResponse_RecordsStreamCauseInOps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, EndpointResponses, nil)
	_, err := c.Writer.WriteString(":\n\n")
	require.NoError(t, err)

	h := &OpenAIGatewayHandler{}
	wrote := h.ensureForwardErrorResponse(c, false, errors.New("stream usage incomplete: missing terminal event"))

	require.True(t, wrote)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "event: response.failed\n")
	require.Contains(t, w.Body.String(), "Upstream request failed")
	require.NotContains(t, w.Body.String(), "missing terminal event")

	raw, ok := c.Get(service.OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*service.OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "stream_missing_terminal", events[0].Kind)
	require.Contains(t, events[0].Message, "missing terminal event")
	require.Equal(t, 0, events[0].UpstreamStatusCode)
}
