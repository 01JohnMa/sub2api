package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexModelsRejectsManifestRelayLoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models?client_version=0.144.0", nil)
	c.Request.Header.Set(service.CodexModelsManifestHopHeader, "1")

	(&OpenAIGatewayHandler{}).CodexModels(c)

	require.Equal(t, http.StatusLoopDetected, recorder.Code)
	require.Contains(t, recorder.Body.String(), "manifest relay loop detected")
}
