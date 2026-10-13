package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOrganizationNonBillableGeminiCountTokensDoesNotRetryTransportFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &geminiCompatHTTPUpstreamStub{err: errors.New("transport unavailable")}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := organizationNonBillableGeminiAccount()
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:countTokens", bytes.NewReader(body))

	result, err := svc.ForwardNative(WithOrganizationNonBillableCountTokens(context.Background()), c, account, "gemini-2.5-pro", "countTokens", false, body)

	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, 1, upstream.calls)
	require.NotContains(t, recorder.Body.String(), "totalTokens")
}

func TestOrganizationNonBillableGeminiCountTokensDoesNotRetryHTTPFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"temporary"}}`)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := organizationNonBillableGeminiAccount()
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:countTokens", bytes.NewReader(body))

	_, err := svc.ForwardNative(WithOrganizationNonBillableCountTokens(context.Background()), c, account, "gemini-2.5-pro", "countTokens", false, body)

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.StatusCode)
	require.JSONEq(t, `{"error":{"message":"temporary"}}`, string(failoverErr.ResponseBody))
	require.Equal(t, "application/json", failoverErr.ResponseHeaders.Get("Content-Type"))
	require.Equal(t, 1, upstream.calls)
	require.NotContains(t, recorder.Body.String(), "totalTokens")
}

func TestGeminiModelsReadDistinguishesLocalBuildAndTransportErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Run("local build error", func(t *testing.T) {
		upstream := &geminiCompatHTTPUpstreamStub{}
		svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
		account := organizationNonBillableGeminiAccount()
		account.Credentials["base_url"] = "://invalid"

		result, err := svc.ForwardAIStudioGET(context.Background(), account, "/v1beta/models")

		require.Error(t, err)
		require.Nil(t, result)
		require.False(t, IsUpstreamNoHTTPResponse(err))
		require.Zero(t, upstream.calls)
	})

	t.Run("transport error", func(t *testing.T) {
		upstream := &geminiCompatHTTPUpstreamStub{err: errors.New("transport unavailable")}
		svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}

		result, err := svc.ForwardAIStudioGET(context.Background(), organizationNonBillableGeminiAccount(), "/v1beta/models")

		require.Error(t, err)
		require.Nil(t, result)
		require.True(t, IsUpstreamNoHTTPResponse(err))
		require.Equal(t, 1, upstream.calls)
	})
}

func TestOrganizationNonBillableGeminiCountTokensMarksCredentialFailurePreDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &geminiCompatHTTPUpstreamStub{}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := organizationNonBillableGeminiAccount()
	account.Credentials = map[string]any{}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:countTokens", bytes.NewReader(body))

	result, err := svc.ForwardNative(WithOrganizationNonBillableCountTokens(context.Background()), c, account, "gemini-2.5-pro", "countTokens", false, body)

	require.Error(t, err)
	require.Nil(t, result)
	require.True(t, IsUpstreamPreDispatchError(err))
	require.Zero(t, upstream.calls)
}

func TestOrganizationNonBillableCountTokensReadFailureIsOutcomeUnknown(t *testing.T) {
	err := organizationCountTokensOutcomeUnknownError(WithOrganizationNonBillableCountTokens(context.Background()), errors.New("response truncated"))
	require.True(t, IsUpstreamOutcomeUnknown(err))
	require.Error(t, err)
}

func TestOrganizationNonBillableGeminiCountTokensReadFailureIsTypedUnknown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       failingOrganizationReadCloser{err: errors.New("response truncated")},
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:countTokens", bytes.NewReader(body))

	result, err := svc.ForwardNative(WithOrganizationNonBillableCountTokens(context.Background()), c, organizationNonBillableGeminiAccount(), "gemini-2.5-pro", "countTokens", false, body)

	require.Nil(t, result)
	require.Error(t, err)
	require.True(t, IsUpstreamOutcomeUnknown(err))
}

func TestGeminiModelsReadRejectsPartialOrOversizedBodiesAsUnknown(t *testing.T) {
	tests := []struct {
		name string
		body io.ReadCloser
	}{
		{name: "read failure", body: failingOrganizationReadCloser{err: errors.New("read failed")}},
		{name: "oversized", body: io.NopCloser(strings.NewReader(strings.Repeat("x", (8<<20)+1)))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: tt.body}}
			svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}

			result, err := svc.ForwardAIStudioGET(context.Background(), organizationNonBillableGeminiAccount(), "/v1beta/models")

			require.Nil(t, result)
			require.Error(t, err)
			require.True(t, IsUpstreamOutcomeUnknown(err))
		})
	}
}

func TestOrganizationNonBillableProviderRequestIDIsSentUpstream(t *testing.T) {
	providerRequestID := "provider-correlation-test"
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"models":[]}`)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	ctx := WithOrganizationNonBillableProviderRequestID(context.Background(), providerRequestID)

	result, err := svc.ForwardAIStudioGET(ctx, organizationNonBillableGeminiAccount(), "/v1beta/models")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, providerRequestID, upstream.lastReq.Header.Get("X-Client-Request-ID"))
}

type failingOrganizationReadCloser struct {
	err error
}

func (r failingOrganizationReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (failingOrganizationReadCloser) Close() error {
	return nil
}

func organizationNonBillableGeminiAccount() *Account {
	return &Account{
		ID: 91, Platform: PlatformGemini, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"api_key": "test-key"},
	}
}
