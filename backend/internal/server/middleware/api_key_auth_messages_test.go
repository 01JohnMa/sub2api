package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOrganizationSpendingGroupAllowedForMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		platform string
		path     string
		want     bool
	}{
		{name: "openai messages", platform: service.PlatformOpenAI, path: "/v1/messages", want: true},
		{name: "anthropic messages", platform: service.PlatformAnthropic, path: "/v1/messages", want: true},
		{name: "gemini messages", platform: service.PlatformGemini, path: "/v1/messages", want: true},
		{name: "antigravity messages", platform: service.PlatformAntigravity, path: "/v1/messages", want: false},
		{name: "anthropic responses", platform: service.PlatformAnthropic, path: "/v1/responses", want: false},
		{name: "openai responses", platform: service.PlatformOpenAI, path: "/v1/responses", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, nil)
			apiKey := &service.APIKey{Group: &service.Group{Platform: tt.platform}}
			require.Equal(t, tt.want, organizationSpendingGroupAllowed(c, apiKey))
		})
	}
}
