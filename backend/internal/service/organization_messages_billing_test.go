package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildOrganizationMessagesRequestPolicyRequiresExplicitBoundedMaxTokens(t *testing.T) {
	tests := []string{
		`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}]}`,
		`{"model":"claude-sonnet-4","max_tokens":0,"messages":[{"role":"user","content":"hello"}]}`,
		`{"model":"claude-sonnet-4","max_tokens":1.5,"messages":[{"role":"user","content":"hello"}]}`,
		`{"model":"claude-sonnet-4","max_tokens":"64","messages":[{"role":"user","content":"hello"}]}`,
		`{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"abc"}}]}]}`,
		`{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`,
	}
	for _, body := range tests {
		_, err := BuildOrganizationMessagesRequestPolicy([]byte(body), OrganizationRequestShapeHTTP)
		require.ErrorIs(t, err, ErrOrganizationMessagesUnsupportedRequest)
	}

	policy, err := BuildOrganizationMessagesRequestPolicy(
		[]byte(`{"model":"claude-sonnet-4","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`),
		OrganizationRequestShapeStreaming,
	)
	require.NoError(t, err)
	require.Equal(t, 64, policy.OutputTokenCeiling)
	require.False(t, policy.UsedVerifiedDefaultOutputCap)
	require.Equal(t, []string{"custom"}, policy.Tools)
}

func TestPlanOrganizationGatewayMessagesCandidateFreezesExactlyOneMappedAttempt(t *testing.T) {
	groupID := int64(42)
	organizationID := int64(88)
	role := OrganizationRoleMember
	accountMultiplier := 1.1
	accounts := []Account{
		{
			ID: 55, Name: "anthropic", Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 2, Priority: 1,
			GroupIDs: []int64{groupID}, AccountGroups: []AccountGroup{{GroupID: groupID}}, RateMultiplier: &accountMultiplier,
			Credentials: map[string]any{"api_key": "test-key", "model_mapping": map[string]any{"claude-client": "claude-sonnet-4"}},
		},
		{
			ID: 66, Name: "second", Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 2, Priority: 2,
			GroupIDs: []int64{groupID}, AccountGroups: []AccountGroup{{GroupID: groupID}}, Credentials: map[string]any{"api_key": "second-key"},
		},
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	gateway := &GatewayService{accountRepo: &organizationGatewayChatAccountRepoStub{accounts: accounts}, cfg: cfg, billingService: NewBillingService(cfg, nil)}
	apiKey := &APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID,
		User:  &User{ID: 7, Status: StatusActive, OrganizationID: &organizationID, OrganizationRole: &role},
		Group: &Group{ID: groupID, Status: StatusActive, Hydrated: true, Platform: PlatformAnthropic, RateMultiplier: 1.25},
		OrganizationAuthorization: &OrganizationKeyAuthorization{
			APIKeyID: 9, UserID: 7, OrganizationID: organizationID, OrganizationStatus: OrganizationStatusActive,
			AllocationID: 77, AllocationUserID: 7, AllocationOrganizationID: organizationID,
			AllocationStatus: domain.OrganizationAllocationStatusActive, Available: "10.00000000",
		},
	}
	body := []byte(`{"model":"claude-client","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)

	plan, err := gateway.PlanOrganizationGatewayMessagesCandidate(context.Background(), apiKey, body, OrganizationRequestShapeHTTP)
	require.NoError(t, err)
	require.Len(t, plan.Candidates, 1)
	require.Equal(t, 1, plan.MaximumAttempts)
	require.Equal(t, int64(55), plan.Candidates[0].AccountID)
	require.Equal(t, "claude-sonnet-4", plan.Candidates[0].UpstreamModel)
	require.Equal(t, OrganizationEndpointFamilyMessages, plan.EndpointFamily)
	require.NotContains(t, string(plan.CandidateSnapshot), "test-key")
	require.NotContains(t, string(plan.CandidateSnapshot), "second-key")
}

func TestOrganizationGatewayMessagesPricingCeilingCoversExtendedCacheClasses(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	billing := NewBillingService(cfg, nil)
	policy := OrganizationMessagesRequestPolicy{
		RequestedModel: "claude-sonnet-4", RequestShape: OrganizationRequestShapeHTTP,
		InputTokenCeiling: 128, OutputTokenCeiling: 64,
	}

	pricing, err := buildOrganizationMessagesPricing(
		context.Background(), billing, nil, policy, "claude-sonnet-4", 1, 1, nil, PlatformAnthropic, "",
	)
	require.NoError(t, err)
	gateway := &GatewayService{billingService: billing}
	actual, _, err := gateway.CalculateOrganizationGatewayMessagesActualCost(OrganizationMessagesCandidate{Pricing: pricing}, &ForwardResult{
		Usage: ClaudeUsage{
			InputTokens: 128, OutputTokens: 64,
			CacheCreationInputTokens: 128, CacheReadInputTokens: 128,
			CacheCreation5mTokens: 128, CacheCreation1hTokens: 128,
		},
	})
	require.NoError(t, err)
	require.LessOrEqual(t, compareOrganizationMoney(actual, pricing.MaximumCost), 0)
}

func TestPlanOrganizationOpenAIMessagesCandidateUsesMappedModelCapAndOneAttempt(t *testing.T) {
	groupID := int64(42)
	organizationID := int64(88)
	role := OrganizationRoleMember
	repo := &stubOpenAIAccountRepo{accounts: []Account{
		{
			ID: 1, Name: "openai", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 2, Priority: 1, GroupIDs: []int64{groupID},
			Credentials: map[string]any{"api_key": "test-key", "model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}},
			Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
		},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 2, GroupIDs: []int64{groupID}},
	}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	svc := &OpenAIGatewayService{accountRepo: repo, cfg: cfg, billingService: NewBillingService(cfg, nil)}
	apiKey := &APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID,
		User: &User{ID: 7, Status: StatusActive, OrganizationID: &organizationID, OrganizationRole: &role},
		Group: &Group{ID: groupID, Status: StatusActive, Hydrated: true, Platform: PlatformOpenAI, RateMultiplier: 1,
			AllowMessagesDispatch: true, MessagesDispatchModelConfig: OpenAIMessagesDispatchModelConfig{SonnetMappedModel: "gpt-5.4"}},
	}

	plan, err := svc.PlanOrganizationOpenAIMessagesCandidate(
		context.Background(), apiKey,
		[]byte(`{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`),
		OrganizationRequestShapeHTTP,
	)
	require.NoError(t, err)
	require.Len(t, plan.Candidates, 1)
	require.Equal(t, 1, plan.MaximumAttempts)
	require.Equal(t, "gpt-5.4", plan.Candidates[0].DispatchModel)
	require.Equal(t, "gpt-5.4", plan.Candidates[0].UpstreamModel)

	_, err = svc.PlanOrganizationOpenAIMessagesCandidate(
		context.Background(), apiKey,
		[]byte(`{"model":"claude-sonnet-4","max_tokens":128001,"messages":[{"role":"user","content":"hello"}]}`),
		OrganizationRequestShapeHTTP,
	)
	require.ErrorIs(t, err, ErrOrganizationMessagesNoCandidates)
}

func TestOrganizationOpenAIMessagesAccountSupportRejectsUnboundedOAuthTemplate(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.ForcedCodexInstructionsTemplate = "prefix {{ .ExistingInstructions }} {{ .ExistingInstructions }}"
	svc := &OpenAIGatewayService{cfg: cfg}

	require.True(t, svc.organizationOpenAIMessagesAccountSupported(&Account{Type: AccountTypeAPIKey}))
	require.False(t, svc.organizationOpenAIMessagesAccountSupported(&Account{Type: AccountTypeOAuth}))

	cfg.Gateway.ForcedCodexInstructionsTemplate = ""
	require.True(t, svc.organizationOpenAIMessagesAccountSupported(&Account{Type: AccountTypeOAuth}))
}

func TestOrganizationMessagesProviderWrappersUseOneHTTPAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("openai disables continuation replay", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{responses: []*http.Response{{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"previous_response_not_found","message":"previous response not found"}}`)),
		}}}
		svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}}
		account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://api.openai.com/v1"}}
		svc.bindOpenAICompatSessionResponseID(context.Background(), nil, account, "stable-cache-key", "resp_missing")
		body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"second"}],"stream":false}`)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

		result, err := svc.ForwardAsAnthropicOrganizationAttempt(context.Background(), c, account, body, "stable-cache-key", "gpt-5.4")
		require.Nil(t, result)
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.Len(t, upstream.requests, 1)
		require.False(t, c.Writer.Written())
	})

	t.Run("gemini disables transport retries", func(t *testing.T) {
		httpStub := &geminiCompatHTTPUpstreamStub{err: io.ErrUnexpectedEOF}
		svc := &GeminiMessagesCompatService{httpUpstream: httpStub, cfg: &config.Config{}}
		account := &Account{ID: 2, Platform: PlatformGemini, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "gemini-key"}}
		body := []byte(`{"model":"gemini-2.5-flash","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

		result, err := svc.ForwardOrganizationMessagesAttempt(context.Background(), c, account, body)
		require.Nil(t, result)
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.True(t, failoverErr.NoHTTPResponse)
		require.Equal(t, 1, httpStub.calls)
		require.False(t, c.Writer.Written())
	})
}

func TestOrganizationOpenAIMessagesFastPolicyBlockIsTypedPreDispatchZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
		ServiceTier: OpenAIFastTierPriority, Action: BetaPolicyActionBlock, Scope: BetaPolicyScopeAll,
		ModelWhitelist: []string{"gpt-5.4"}, FallbackAction: BetaPolicyActionPass,
	}}}
	svc := newOpenAIGatewayServiceWithSettings(t, settings)
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "unused"}}
	body := []byte(`{"model":"claude-sonnet-4","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("anthropic-beta", claude.BetaFastMode)

	result, err := svc.ForwardAsAnthropicOrganizationAttempt(context.Background(), c, account, body, "", "gpt-5.4")
	require.Nil(t, result)
	var localErr *OpenAILocalPreDispatchError
	require.ErrorAs(t, err, &localErr)
	require.Equal(t, "local_policy_rejected", localErr.ZeroProof)
	require.False(t, c.Writer.Written())
}
