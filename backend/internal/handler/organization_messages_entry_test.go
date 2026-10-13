package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOrganizationMessagesEntryPrecedesPersonalDispatchChecks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	body := []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`)

	t.Run("anthropic gateway", func(t *testing.T) {
		apiKey := organizationGatewayChatOrganizationAPIKey()
		handler := NewGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil)
		recorder, c := organizationMessagesEntryContext(apiKey, body)

		handler.Messages(c)

		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		require.Contains(t, recorder.Body.String(), "organization_billing_unavailable")
	})

	t.Run("openai gateway", func(t *testing.T) {
		apiKey := organizationGatewayChatOrganizationAPIKey()
		apiKey.Group.Platform = service.PlatformOpenAI
		apiKey.Group.AllowMessagesDispatch = false
		handler := NewOpenAIGatewayHandler(nil, nil, nil, nil, nil, nil, nil, cfg)
		recorder, c := organizationMessagesEntryContext(apiKey, body)

		handler.Messages(c)

		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		require.Contains(t, recorder.Body.String(), "organization_billing_unavailable")
		require.NotContains(t, recorder.Body.String(), "does not allow /v1/messages dispatch")
	})
}

func TestOrganizationMessagesAdaptersRequireExplicitZeroCostProof(t *testing.T) {
	gin.SetMode(gin.TestMode)
	candidate := service.OrganizationMessagesCandidate{
		AccountID: 1,
		Route:     service.OrganizationMessagesRoute{AccountID: 1, Platform: service.PlatformAnthropic},
	}
	request := service.OrganizationExecutionDispatchRequest{ProviderRequestID: "provider-request"}

	t.Run("gateway unproven failure remains pending", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		adapter := &organizationGatewayMessagesAdapter{c: c}
		attempt, err := adapter.resultForForward(
			request, candidate, nil,
			&service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable},
			c.Writer.Size(),
		)

		require.Error(t, err)
		require.True(t, attempt.Dispatched)
		require.True(t, attempt.PendingReconciliation)
	})

	t.Run("gateway transport failure remains pending", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		adapter := &organizationGatewayMessagesAdapter{c: c}
		attempt, err := adapter.resultForForward(
			request, candidate, nil,
			&service.UpstreamFailoverError{NoHTTPResponse: true, ZeroCostProven: true},
			c.Writer.Size(),
		)

		require.Error(t, err)
		require.True(t, attempt.Dispatched)
		require.True(t, attempt.PendingReconciliation)
	})

	t.Run("gateway proven failure terminalizes only before response commit", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		adapter := &organizationGatewayMessagesAdapter{c: c}
		writerSizeBefore := c.Writer.Size()
		attempt, err := adapter.resultForForward(
			request, candidate, nil,
			&service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, ZeroCostProven: true},
			writerSizeBefore,
		)

		require.NoError(t, err)
		require.False(t, attempt.PendingReconciliation)
		require.Equal(t, "0", attempt.Observation.RawActualCost)

		_, writeErr := c.Writer.WriteString("partial response")
		require.NoError(t, writeErr)
		attempt, err = adapter.resultForForward(
			request, candidate, nil,
			&service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, ZeroCostProven: true},
			writerSizeBefore,
		)
		require.Error(t, err)
		require.True(t, attempt.PendingReconciliation)
	})

	t.Run("openai adapter uses the same proof gate", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		adapter := &organizationOpenAIMessagesAdapter{c: c}
		writerSizeBefore := c.Writer.Size()
		attempt, err := adapter.resultForForward(
			request, candidate, nil,
			&service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable},
			writerSizeBefore,
		)
		require.Error(t, err)
		require.True(t, attempt.PendingReconciliation)

		attempt, err = adapter.resultForForward(
			request, candidate, nil,
			&service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, ZeroCostProven: true},
			writerSizeBefore,
		)
		require.NoError(t, err)
		require.False(t, attempt.PendingReconciliation)
		require.Equal(t, "0", attempt.Observation.RawActualCost)
	})
}

func organizationMessagesEntryContext(apiKey *service.APIKey, body []byte) (*httptest.ResponseRecorder, *gin.Context) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)
	return recorder, c
}
