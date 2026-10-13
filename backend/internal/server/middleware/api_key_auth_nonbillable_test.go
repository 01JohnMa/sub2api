package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOrganizationNonBillableCapabilityRouteMatrix(t *testing.T) {
	tests := []struct {
		method string
		path   string
		family string
		shape  string
	}{
		{http.MethodGet, "/models", service.OrganizationEndpointFamilyModels, service.OrganizationRequestShapeHTTP},
		{http.MethodGet, "/v1/models", service.OrganizationEndpointFamilyModels, service.OrganizationRequestShapeHTTP},
		{http.MethodGet, "/antigravity/models", service.OrganizationEndpointFamilyModels, service.OrganizationRequestShapeHTTP},
		{http.MethodGet, "/antigravity/v1/models", service.OrganizationEndpointFamilyModels, service.OrganizationRequestShapeHTTP},
		{http.MethodGet, "/v1/usage", service.OrganizationEndpointFamilyUsage, service.OrganizationRequestShapeHTTP},
		{http.MethodGet, "/antigravity/v1/usage", service.OrganizationEndpointFamilyUsage, service.OrganizationRequestShapeHTTP},
		{http.MethodPost, "/v1/messages/count_tokens", service.OrganizationEndpointFamilyTokenCounting, service.OrganizationRequestShapeProbe},
		{http.MethodGet, "/v1beta/models", service.OrganizationEndpointFamilyModels, service.OrganizationRequestShapeHTTP},
		{http.MethodGet, "/v1beta/models/gemini-2.5-pro", service.OrganizationEndpointFamilyModels, service.OrganizationRequestShapeHTTP},
		{http.MethodGet, "/antigravity/v1beta/models", service.OrganizationEndpointFamilyModels, service.OrganizationRequestShapeHTTP},
		{http.MethodPost, "/v1beta/models/gemini-2.5-pro:countTokens", service.OrganizationEndpointFamilyTokenCounting, service.OrganizationRequestShapeProbe},
		{http.MethodPost, "/antigravity/v1beta/models/gemini-2.5-pro/countTokens", service.OrganizationEndpointFamilyTokenCounting, service.OrganizationRequestShapeProbe},
	}

	gin.SetMode(gin.TestMode)
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(tt.method, tt.path, nil)
			require.Equal(t, []organizationCapabilityRequest{{family: tt.family, shape: tt.shape}}, organizationNonBillableCapabilityRequests(c))
			require.True(t, isOrganizationNonBillableRequest(c))
		})
	}
}

func TestOrganizationNonBillableCapabilityRouteMatrixRejectsBillableActions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{
		"/v1/messages",
		"/v1beta/models/gemini-2.5-pro:generateContent",
		"/antigravity/v1beta/models/gemini-2.5-pro:streamGenerateContent",
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, path, nil)
		require.Empty(t, organizationNonBillableCapabilityRequests(c), path)
	}
}
