package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type organizationMessagesHTTPClientUpstream struct {
	client *http.Client
	calls  atomic.Int32
}

func (u *organizationMessagesHTTPClientUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls.Add(1)
	return u.client.Do(req)
}

func (u *organizationMessagesHTTPClientUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestForwardOrganizationMessagesAttemptAnthropicUsesOneHTTPAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name             string
		handler          http.HandlerFunc
		wantStatus       int
		wantNoHTTPResult bool
	}{
		{
			name: "retryable 503",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`))
			},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name: "transport disconnect",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				hijacker, ok := w.(http.Hijacker)
				if !ok {
					http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
					return
				}
				conn, _, err := hijacker.Hijack()
				if err != nil {
					return
				}
				_ = conn.Close()
			},
			wantNoHTTPResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var received atomic.Int32
			upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received.Add(1)
				tt.handler(w, r)
			}))
			defer upstreamServer.Close()

			httpUpstream := &organizationMessagesHTTPClientUpstream{client: upstreamServer.Client()}
			cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
				Enabled:           false,
				AllowInsecureHTTP: true,
			}}}
			svc := &GatewayService{
				cfg:                 cfg,
				httpUpstream:        httpUpstream,
				tlsFPProfileService: &TLSFingerprintProfileService{},
			}
			account := &Account{
				ID:          23,
				Name:        "organization-anthropic-attempt",
				Platform:    PlatformAnthropic,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{
					"api_key":  "test-key",
					"base_url": upstreamServer.URL,
				},
			}
			body := []byte(`{"model":"claude-sonnet-4","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`)
			parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), domain.PlatformAnthropic)
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

			result, err := svc.ForwardOrganizationMessagesAttempt(context.Background(), c, account, parsed)
			require.Nil(t, result)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.Equal(t, tt.wantStatus, failoverErr.StatusCode)
			require.Equal(t, tt.wantNoHTTPResult, failoverErr.NoHTTPResponse)
			require.EqualValues(t, 1, received.Load())
			require.EqualValues(t, 1, httpUpstream.calls.Load())
			require.False(t, c.Writer.Written())
			require.Empty(t, recorder.Body.String())
		})
	}
}
