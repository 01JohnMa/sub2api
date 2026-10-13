package service

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesChatFallbackBufferedMarksOnlyValidatedUsageObserved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name          string
		body          string
		usageObserved bool
	}{
		{
			name:          "validated usage",
			body:          `{"id":"chatcmpl_1","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
			usageObserved: true,
		},
		{
			name:          "missing usage",
			body:          `{"id":"chatcmpl_1","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`,
			usageObserved: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			result, err := (&OpenAIGatewayService{}).bufferChatCompletionsAsResponses(
				c,
				&http.Response{StatusCode: http.StatusOK, Header: http.Header{"x-request-id": []string{"req_1"}}, Body: io.NopCloser(bytes.NewBufferString(testCase.body))},
				"gpt-5.4", "gpt-5.4", "gpt-5.4", nil, nil, time.Now(),
			)

			require.NoError(t, err)
			require.Equal(t, testCase.usageObserved, result.UsageObserved)
		})
	}
}

func TestResponsesChatFallbackStreamRequiresTerminalUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name          string
		body          string
		usageObserved bool
	}{
		{
			name: "validated usage before done",
			body: "data: {\"id\":\"chatcmpl_1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n" +
				"data: {\"id\":\"chatcmpl_1\",\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n" +
				"data: [DONE]\n",
			usageObserved: true,
		},
		{
			name:          "usage without terminal done",
			body:          "data: {\"id\":\"chatcmpl_1\",\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n",
			usageObserved: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			result, err := (&OpenAIGatewayService{}).streamChatCompletionsAsResponses(
				c,
				&http.Response{StatusCode: http.StatusOK, Header: http.Header{"x-request-id": []string{"req_1"}}, Body: io.NopCloser(bytes.NewBufferString(testCase.body))},
				"gpt-5.4", "gpt-5.4", "gpt-5.4", nil, nil, time.Now(),
			)

			require.NoError(t, err)
			require.Equal(t, testCase.usageObserved, result.UsageObserved)
		})
	}
}
