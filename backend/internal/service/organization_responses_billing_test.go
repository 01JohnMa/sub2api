package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type organizationResponsesChannelRepoStub struct {
	ChannelRepository
	channel        Channel
	groupPlatforms map[int64]string
}

func (r *organizationResponsesChannelRepoStub) ListAll(context.Context) ([]Channel, error) {
	return []Channel{r.channel}, nil
}

func (r *organizationResponsesChannelRepoStub) GetGroupPlatforms(context.Context, []int64) (map[int64]string, error) {
	return r.groupPlatforms, nil
}

func TestBuildOrganizationResponsesRequestPolicyUsesVerifiedDefaultOutputCap(t *testing.T) {
	policy, err := BuildOrganizationResponsesRequestPolicy([]byte(`{
		"model":"gpt-5.4",
		"stream":true,
		"input":"hello",
		"tools":[{"type":"function","name":"lookup"},{"type":"custom","name":"shell"}],
		"service_tier":"fast"
	}`), OrganizationRequestShapeStreaming, "/backend-api/codex/responses")

	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", policy.RequestedModel)
	require.Equal(t, 128000, policy.OutputTokenCeiling)
	require.True(t, policy.UsedVerifiedDefaultOutputCap)
	require.Equal(t, "priority", policy.ServiceTier)
	require.Greater(t, policy.InputTokenCeiling, 0)
	require.LessOrEqual(t, policy.InputTokenCeiling, policy.MaxInputTokens)
}

func TestBuildOrganizationResponsesRequestPolicyTreatsHTTPAliasesIdentically(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":32}`)
	var baseline OrganizationResponsesRequestPolicy
	for index, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
		policy, err := BuildOrganizationResponsesRequestPolicy(body, OrganizationRequestShapeHTTP, path)
		require.NoError(t, err)
		if index == 0 {
			baseline = policy
			continue
		}
		require.Equal(t, baseline, policy)
	}
}

func TestBuildOrganizationResponsesRequestPolicyRejectsUnboundedOrUnsupportedShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
		path string
	}{
		{name: "unknown model without cap", body: `{"model":"unknown-model","input":"hi"}`, path: "/v1/responses"},
		{name: "unverified dated snapshot", body: `{"model":"gpt-5.4-9999-12-31","input":"hi"}`, path: "/v1/responses"},
		{name: "output cap exceeds verified model limit", body: `{"model":"gpt-5.4","input":"hi","max_output_tokens":128001}`, path: "/v1/responses"},
		{name: "fractional output cap", body: `{"model":"gpt-5.4","input":"hi","max_output_tokens":64.5}`, path: "/v1/responses"},
		{name: "non-string model", body: `{"model":54,"input":"hi"}`, path: "/v1/responses"},
		{name: "non-string service tier", body: `{"model":"gpt-5.4","input":"hi","service_tier":1}`, path: "/v1/responses"},
		{name: "auto service tier has no frozen price", body: `{"model":"gpt-5.4","input":"hi","service_tier":"auto"}`, path: "/v1/responses"},
		{name: "scale service tier has no token price", body: `{"model":"gpt-5.4","input":"hi","service_tier":"scale"}`, path: "/v1/responses"},
		{name: "non-boolean background", body: `{"model":"gpt-5.4","input":"hi","background":"false"}`, path: "/v1/responses"},
		{name: "hosted web search tool", body: `{"model":"gpt-5.4","input":"hi","tools":[{"type":"web_search"}]}`, path: "/v1/responses"},
		{name: "image generation tool", body: `{"model":"gpt-5.4","input":"hi","tools":[{"type":"image_generation"}]}`, path: "/v1/responses"},
		{name: "previous response chain", body: `{"model":"gpt-5.4","input":"hi","previous_response_id":"resp_1"}`, path: "/v1/responses"},
		{name: "conversation state", body: `{"model":"gpt-5.4","input":"hi","conversation":"conv_1"}`, path: "/v1/responses"},
		{name: "server prompt template", body: `{"model":"gpt-5.4","input":"hi","prompt":{"id":"pmpt_1"}}`, path: "/v1/responses"},
		{name: "remote image input", body: `{"model":"gpt-5.4","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.test/image.png"}]}]}`, path: "/v1/responses"},
		{name: "remote file input", body: `{"model":"gpt-5.4","input":[{"role":"user","content":[{"type":"input_file","file_id":"file_1"}]}]}`, path: "/v1/responses"},
		{name: "server item reference", body: `{"model":"gpt-5.4","input":[{"type":"item_reference","id":"item_1"}]}`, path: "/v1/responses"},
		{name: "background execution", body: `{"model":"gpt-5.4","input":"hi","background":true}`, path: "/v1/responses"},
		{name: "non boolean background", body: `{"model":"gpt-5.4","input":"hi","background":"false"}`, path: "/v1/responses"},
		{name: "fractional output cap", body: `{"model":"gpt-5.4","input":"hi","max_output_tokens":12.5}`, path: "/v1/responses"},
		{name: "compact subresource", body: `{"model":"gpt-5.4","input":"hi"}`, path: "/v1/responses/compact"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildOrganizationResponsesRequestPolicy([]byte(tt.body), OrganizationRequestShapeHTTP, tt.path)
			require.Error(t, err)
			require.Equal(t, "ORGANIZATION_RESPONSES_UNSUPPORTED_REQUEST", infraerrors.Reason(err))
		})
	}
}

func TestOrganizationResponsesCandidatePricingCeilingAndActualCostUseSameSnapshot(t *testing.T) {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	billing := NewBillingService(cfg, nil)
	policy, err := BuildOrganizationResponsesRequestPolicy(
		[]byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":64,"service_tier":"flex"}`),
		OrganizationRequestShapeHTTP,
		"/responses",
	)
	require.NoError(t, err)

	pricing, err := BuildOrganizationResponsesCandidatePricing(billing, nil, policy, "gpt-5.4", 1.25)
	require.NoError(t, err)
	require.NotEmpty(t, pricing.MaximumCost)

	actual, err := pricing.CalculateActualCost(billing, UsageTokens{InputTokens: 12, OutputTokens: 7})
	require.NoError(t, err)
	require.NotEmpty(t, actual)
	requireOrganizationMoneyLessOrEqual(t, actual, pricing.MaximumCost)
}

func TestPlanOrganizationResponsesCandidatesFreezesNativeResponsesRoutesAndExcludesChatFallback(t *testing.T) {
	groupID := int64(42)
	organizationID := int64(88)
	role := OrganizationRoleMember
	accountMultiplier := 1.1
	repo := &stubOpenAIAccountRepo{accounts: []Account{
		{ID: 1, Name: "oauth", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 2, Priority: 1, GroupIDs: []int64{42}, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.3-codex"}}},
		{ID: 2, Name: "chat fallback", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2, Priority: 2, Extra: map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions)}},
		{ID: 3, Name: "native responses", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2, Priority: 3, GroupIDs: []int64{42}, RateMultiplier: &accountMultiplier, Extra: map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)}},
	}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 1
	billing := NewBillingService(cfg, nil)
	svc := &OpenAIGatewayService{accountRepo: repo, cfg: cfg, billingService: billing}
	apiKey := &APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID,
		User:  &User{ID: 7, Status: StatusActive, OrganizationID: &organizationID, OrganizationRole: &role},
		Group: &Group{ID: groupID, Status: StatusActive, Hydrated: true, Platform: PlatformOpenAI, RateMultiplier: 1.25},
	}

	body := []byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":64}`)
	plan, err := svc.PlanOrganizationResponsesCandidates(context.Background(), OrganizationResponsesCandidatePlanInput{
		APIKey: apiKey, Body: body,
		RequestShape: OrganizationRequestShapeHTTP, RequestPath: "/v1/responses", MaximumAttempts: 2,
	})

	require.NoError(t, err)
	require.Len(t, plan.Candidates, 2)
	require.Equal(t, []int64{1, 3}, []int64{plan.Candidates[0].AccountID, plan.Candidates[1].AccountID})
	require.Equal(t, 2, plan.MaximumAttempts)
	require.NotEmpty(t, plan.CandidateSnapshot)
	require.NotContains(t, string(plan.CandidateSnapshot), "chat fallback")
	require.NotContains(t, string(plan.CandidateSnapshot), "credentials")
	expectedInputCeiling := organizationResponsesInputTokenCeiling(body) + (len(defaultCodexSynthInstructions(plan.Policy.RequestedModel))+4096)*organizationResponsesMaximumInputBodyMultiplier
	require.Equal(t, expectedInputCeiling, plan.Candidates[0].Pricing.InputTokenCeiling, "instructions ceiling must use the model Forward sees before account mapping")
	require.Equal(t, 128000, plan.Candidates[0].Pricing.OutputTokenCeiling, "OAuth removes the client cap and must reserve the verified model maximum")
	require.Greater(t, plan.Candidates[0].Pricing.InputTokenCeiling, plan.Policy.InputTokenCeiling, "OAuth server instructions must be included in the ceiling")
	require.Greater(t, plan.Candidates[1].Pricing.InputTokenCeiling, plan.Policy.InputTokenCeiling, "API key server instructions must be included in the ceiling")
	require.Equal(t, 64, plan.Candidates[1].Pricing.OutputTokenCeiling)
	require.Equal(t, 1.25, plan.Candidates[1].Pricing.RateMultiplier)
	require.Equal(t, 1.1, plan.Candidates[1].Pricing.AccountMultiplier)
}

func TestBuildOrganizationResponsesRequestPolicyRejectsUnsupportedModelStreaming(t *testing.T) {
	_, err := BuildOrganizationResponsesRequestPolicy(
		[]byte(`{"model":"gpt-5.5-pro","input":"hello","stream":true}`),
		OrganizationRequestShapeStreaming,
		"/v1/responses",
	)

	require.ErrorIs(t, err, ErrOrganizationResponsesUnsupportedRequest)
}

func TestPlanOrganizationResponsesCandidatesRejectsUnverifiedMappedModel(t *testing.T) {
	groupID := int64(42)
	repo := &stubOpenAIAccountRepo{accounts: []Account{{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4-unverified"}},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
	}}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	svc := &OpenAIGatewayService{accountRepo: repo, cfg: cfg, billingService: NewBillingService(cfg, nil)}

	_, err := svc.PlanOrganizationResponsesCandidates(context.Background(), OrganizationResponsesCandidatePlanInput{
		APIKey: &APIKey{GroupID: &groupID}, Body: []byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":64}`),
		RequestShape: OrganizationRequestShapeHTTP, RequestPath: "/v1/responses", MaximumAttempts: 1,
	})

	require.ErrorIs(t, err, ErrOrganizationResponsesNoCandidates)
}

func TestPlanOrganizationResponsesCandidatesExcludesOpenAIPassthrough(t *testing.T) {
	groupID := int64(42)
	repo := &stubOpenAIAccountRepo{accounts: []Account{
		{
			ID: 1, Name: "passthrough", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
			Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.3-codex"}},
			Extra: map[string]any{
				"openai_passthrough":                true,
				openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses),
			},
		},
		{
			ID: 2, Name: "native", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 2,
			Extra: map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
		},
		{
			ID: 3, Name: "setup-token", Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 3,
			Extra: map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
		},
	}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	svc := &OpenAIGatewayService{accountRepo: repo, cfg: cfg, billingService: NewBillingService(cfg, nil)}

	plan, err := svc.PlanOrganizationResponsesCandidates(context.Background(), OrganizationResponsesCandidatePlanInput{
		APIKey: &APIKey{GroupID: &groupID}, Body: []byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":64}`),
		RequestShape: OrganizationRequestShapeHTTP, RequestPath: "/v1/responses", MaximumAttempts: 2,
	})

	require.NoError(t, err)
	require.Len(t, plan.Candidates, 1)
	require.Equal(t, int64(2), plan.Candidates[0].AccountID)
	require.NotContains(t, string(plan.CandidateSnapshot), "passthrough")
	require.NotContains(t, string(plan.CandidateSnapshot), "setup-token")
}

func TestPlanOrganizationResponsesCandidatesAppliesUpstreamChannelRestrictionPerAccount(t *testing.T) {
	groupID := int64(42)
	channelService := NewChannelService(&organizationResponsesChannelRepoStub{channel: Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{groupID},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceUpstream,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"gpt-5.3-codex"}},
		},
	}, groupPlatforms: map[int64]string{groupID: PlatformOpenAI}}, nil, nil, nil)
	repo := &stubOpenAIAccountRepo{accounts: []Account{
		{
			ID: 1, Name: "restricted", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
			Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}},
			Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
		},
		{
			ID: 2, Name: "allowed", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 2,
			Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.3-codex"}},
			Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
		},
	}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	svc := &OpenAIGatewayService{
		accountRepo: repo, channelService: channelService, cfg: cfg,
		billingService: NewBillingService(cfg, nil),
	}

	plan, err := svc.PlanOrganizationResponsesCandidates(context.Background(), OrganizationResponsesCandidatePlanInput{
		APIKey: &APIKey{GroupID: &groupID}, Body: []byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":64}`),
		RequestShape: OrganizationRequestShapeHTTP, RequestPath: "/v1/responses", MaximumAttempts: 2,
	})

	require.NoError(t, err)
	require.Len(t, plan.Candidates, 1)
	require.Equal(t, int64(2), plan.Candidates[0].AccountID)
	require.Equal(t, "gpt-5.3-codex", plan.Candidates[0].UpstreamModel)
	require.NotContains(t, string(plan.CandidateSnapshot), "restricted")
}

func TestPlanOrganizationResponsesCandidatesFreezesEffectiveFastPolicy(t *testing.T) {
	groupID := int64(42)
	repo := &stubOpenAIAccountRepo{accounts: []Account{{
		ID: 1, Name: "native", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{42},
	}}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	billing := NewBillingService(cfg, nil)
	svc := &OpenAIGatewayService{accountRepo: repo, cfg: cfg, billingService: billing}
	policyService := newOpenAIGatewayServiceWithSettings(t, openAIFastFilterPriorityPolicy())
	svc.settingService = policyService.settingService
	apiKey := &APIKey{ID: 9, UserID: 7, GroupID: &groupID, User: &User{ID: 7, Status: StatusActive}, Group: &Group{ID: groupID, Status: StatusActive, Hydrated: true, Platform: PlatformOpenAI, RateMultiplier: 1}}
	body := []byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":64,"service_tier":"priority"}`)
	filterCtx := withOpenAIFastPolicyContext(context.Background(), openAIFastFilterPriorityPolicy())

	plan, err := svc.PlanOrganizationResponsesCandidates(filterCtx, OrganizationResponsesCandidatePlanInput{
		APIKey: apiKey, Body: body, RequestShape: OrganizationRequestShapeHTTP, RequestPath: "/v1/responses", MaximumAttempts: 1,
	})

	require.NoError(t, err)
	require.Len(t, plan.Candidates, 1)
	candidate := plan.Candidates[0]
	require.Empty(t, candidate.ServiceTier)
	require.Empty(t, candidate.Pricing.ServiceTier)
	require.False(t, gjson.GetBytes(candidate.ForwardBody, "service_tier").Exists())
	_, err = svc.RevalidateOrganizationResponsesCandidate(filterCtx, apiKey, candidate)
	require.NoError(t, err)
	svc.settingService = nil
	_, err = svc.RevalidateOrganizationResponsesCandidate(context.Background(), apiKey, candidate)
	require.ErrorIs(t, err, ErrOrganizationResponsesCandidateConflict)
}

func requireOrganizationMoneyLessOrEqual(t *testing.T, actual, ceiling string) {
	t.Helper()
	actualCanonical, err := NormalizeOrganizationRawActualCost(actual)
	require.NoError(t, err)
	ceilingCanonical, err := NormalizeOrganizationMoney(ceiling, true)
	require.NoError(t, err)
	actualDecimal, err := decimal.NewFromString(actualCanonical)
	require.NoError(t, err)
	ceilingDecimal, err := decimal.NewFromString(ceilingCanonical)
	require.NoError(t, err)
	require.False(t, actualDecimal.GreaterThan(ceilingDecimal))
}
