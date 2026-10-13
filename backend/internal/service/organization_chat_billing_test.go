package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildOrganizationChatRequestPolicyFreezesBoundedChatShape(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.4",
		"messages":[{"role":"user","content":"hello"}],
		"stream":true,
		"tools":[{"type":"function","function":{"name":"lookup"}}],
		"max_completion_tokens":64,
		"service_tier":"fast"
	}`)

	policy, err := BuildOrganizationChatRequestPolicy(body, OrganizationRequestShapeStreaming)

	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", policy.RequestedModel)
	require.True(t, policy.Stream)
	require.Equal(t, 64, policy.OutputTokenCeiling)
	require.Equal(t, "priority", policy.ServiceTier)
	require.Equal(t, []string{"function"}, policy.Tools)
	require.Greater(t, policy.InputTokenCeiling, 0)
	require.LessOrEqual(t, policy.InputTokenCeiling, policy.MaxInputTokens)
}

func TestBuildOrganizationChatRequestPolicyRejectsUnboundedOrUnsupportedShapes(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		shape string
	}{
		{name: "missing messages", body: `{"model":"gpt-5.4"}`, shape: OrganizationRequestShapeHTTP},
		{name: "multiple choices", body: `{"model":"gpt-5.4","messages":[],"n":2}`, shape: OrganizationRequestShapeHTTP},
		{name: "unsupported hosted tool", body: `{"model":"gpt-5.4","messages":[],"tools":[{"type":"web_search"}]}`, shape: OrganizationRequestShapeHTTP},
		{name: "non boolean stream", body: `{"model":"gpt-5.4","messages":[],"stream":"true"}`, shape: OrganizationRequestShapeHTTP},
		{name: "shape stream mismatch", body: `{"model":"gpt-5.4","messages":[],"stream":true}`, shape: OrganizationRequestShapeHTTP},
		{name: "output cap over model limit", body: `{"model":"gpt-5.4","messages":[],"max_tokens":128001}`, shape: OrganizationRequestShapeHTTP},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildOrganizationChatRequestPolicy([]byte(tt.body), tt.shape)
			require.ErrorIs(t, err, ErrOrganizationChatUnsupportedRequest)
		})
	}
}

func TestBuildOrganizationChatRequestPolicySupportsResponsesShapeBridge(t *testing.T) {
	policy, err := BuildOrganizationChatRequestPolicy(
		[]byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":64}`),
		OrganizationRequestShapeHTTP,
	)

	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", policy.RequestedModel)
	require.Equal(t, 64, policy.OutputTokenCeiling)
	require.Equal(t, OrganizationRequestShapeHTTP, policy.RequestShape)
}

func TestPlanOrganizationChatCandidatesFreezesResponsesBridgeAndRawChatFallback(t *testing.T) {
	groupID := int64(42)
	organizationID := int64(88)
	role := OrganizationRoleMember
	accountMultiplier := 1.1
	repo := &stubOpenAIAccountRepo{accounts: []Account{
		{ID: 1, Name: "responses bridge", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 2, Priority: 1, GroupIDs: []int64{42}, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.3-codex"}}},
		{ID: 2, Name: "raw chat", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2, Priority: 2, GroupIDs: []int64{42}, RateMultiplier: &accountMultiplier, Extra: map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions)}},
	}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 1
	svc := &OpenAIGatewayService{accountRepo: repo, cfg: cfg, billingService: NewBillingService(cfg, nil)}
	apiKey := &APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID,
		User:  &User{ID: 7, Status: StatusActive, OrganizationID: &organizationID, OrganizationRole: &role},
		Group: &Group{ID: groupID, Status: StatusActive, Hydrated: true, Platform: PlatformOpenAI, RateMultiplier: 1.25},
		OrganizationAuthorization: &OrganizationKeyAuthorization{
			APIKeyID: 9, UserID: 7, OrganizationID: organizationID, OrganizationStatus: OrganizationStatusActive,
			OrganizationBalanceVersion: 1, AllocationID: 77, AllocationUserID: 7, AllocationOrganizationID: organizationID,
			AllocationStatus: "active", AllocationVersion: 1, Available: "10.00000000", Reserved: "0.00000000",
		},
	}

	plan, err := svc.PlanOrganizationChatCandidates(context.Background(), OrganizationChatCandidatePlanInput{
		APIKey: apiKey, Body: []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":64}`),
		RequestShape: OrganizationRequestShapeHTTP, MaximumAttempts: 2,
	})

	require.NoError(t, err)
	require.Len(t, plan.Candidates, 2)
	require.Equal(t, OrganizationChatRouteResponsesBridge, plan.Candidates[0].Route)
	require.Equal(t, OrganizationChatRouteRawChat, plan.Candidates[1].Route)
	require.Equal(t, 2, plan.MaximumAttempts)
	require.NotEmpty(t, plan.CandidateSnapshot)
	require.NotEmpty(t, plan.PricingSnapshot)
	require.NotEmpty(t, plan.CandidateRouteMaximumCost)
	require.NotContains(t, string(plan.CandidateSnapshot), "credentials")
	require.Equal(t, 1.25, plan.Candidates[0].Pricing.RateMultiplier)
	require.Equal(t, accountMultiplier, plan.Candidates[1].Pricing.AccountMultiplier)

	reservation, err := plan.NewExecutionReservationInput(apiKey, "client-request")
	require.NoError(t, err)
	require.Equal(t, OrganizationEndpointFamilyChat, reservation.EndpointFamily)
	require.Equal(t, OrganizationRequestShapeHTTP, reservation.RequestShape)
	require.JSONEq(t, string(plan.CandidateSnapshot), string(reservation.CandidateSnapshot))
	require.JSONEq(t, string(plan.PricingSnapshot), string(reservation.PricingSnapshot))
}

func TestPlanOrganizationChatCandidatesFreezesAccountFastPolicyEffectiveBody(t *testing.T) {
	groupID := int64(42)
	organizationID := int64(88)
	role := OrganizationRoleMember
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	svc := newOpenAIGatewayServiceWithSettings(t, openAIFastFilterPriorityPolicy())
	svc.accountRepo = &stubOpenAIAccountRepo{accounts: []Account{{
		ID: 1, Name: "filtered responses", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{groupID},
		Extra: map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
	}}}
	svc.cfg = cfg
	svc.billingService = NewBillingService(cfg, nil)
	apiKey := &APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID,
		User:  &User{ID: 7, Status: StatusActive, OrganizationID: &organizationID, OrganizationRole: &role},
		Group: &Group{ID: groupID, Status: StatusActive, Hydrated: true, Platform: PlatformOpenAI, RateMultiplier: 1},
		OrganizationAuthorization: &OrganizationKeyAuthorization{
			APIKeyID: 9, UserID: 7, OrganizationID: organizationID, OrganizationStatus: OrganizationStatusActive,
			OrganizationBalanceVersion: 1, AllocationID: 77, AllocationUserID: 7, AllocationOrganizationID: organizationID,
			AllocationStatus: "active", AllocationVersion: 1, Available: "10.00000000", Reserved: "0.00000000",
		},
	}
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":64,"service_tier":"fast"}`)

	plan, err := svc.PlanOrganizationChatCandidates(context.Background(), OrganizationChatCandidatePlanInput{
		APIKey: apiKey, Body: body, RequestShape: OrganizationRequestShapeHTTP, MaximumAttempts: 1,
	})

	require.NoError(t, err)
	require.Len(t, plan.Candidates, 1)
	candidate := plan.Candidates[0]
	require.Empty(t, candidate.ServiceTier)
	require.Empty(t, candidate.Pricing.ServiceTier)
	require.False(t, gjson.GetBytes(candidate.ForwardBody, "service_tier").Exists())
	require.Equal(t, organizationChatBodyHash(candidate.ForwardBody), candidate.ForwardBodyHash)
	require.NotEqual(t, organizationChatBodyHash(body), candidate.ForwardBodyHash)
}

func TestPlanOrganizationChatCandidatesFreezesUnknownResponsesRouteAndRawFallbackAttempts(t *testing.T) {
	groupID := int64(42)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	svc := &OpenAIGatewayService{
		accountRepo: &stubOpenAIAccountRepo{accounts: []Account{{
			ID: 1, Name: "unknown responses", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{groupID},
		}}},
		cfg: cfg, billingService: NewBillingService(cfg, nil),
	}
	apiKey := organizationChatAuthorizedAPIKey()
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":64}`)

	plan, err := svc.PlanOrganizationChatCandidates(context.Background(), OrganizationChatCandidatePlanInput{
		APIKey: apiKey, Body: body, RequestShape: OrganizationRequestShapeHTTP, MaximumAttempts: 2,
	})

	require.NoError(t, err)
	require.Len(t, plan.Candidates, 2)
	require.Equal(t, int64(1), plan.Candidates[0].AccountID)
	require.Equal(t, int64(1), plan.Candidates[1].AccountID)
	require.Equal(t, OrganizationChatRouteResponsesBridge, plan.Candidates[0].Route)
	require.Equal(t, OrganizationChatRouteRawChat, plan.Candidates[1].Route)
	require.Equal(t, 2, plan.MaximumAttempts)
}

func TestOrganizationChatPreDispatchRejectionUsesChatExecutionIdentity(t *testing.T) {
	apiKey := organizationChatAuthorizedAPIKey()

	input, err := NewOrganizationChatPreDispatchRejectionInput(
		apiKey,
		[]byte(`{"model":"gpt-5.4","messages":[]}`),
		OrganizationRequestShapeHTTP,
		"client-request",
		ErrOrganizationChatUnsupportedRequest,
	)

	require.NoError(t, err)
	require.Equal(t, OrganizationEndpointFamilyChat, input.EndpointFamily)
	require.Equal(t, "0.00000000", input.CandidateRouteMaximumCost)
	require.Contains(t, input.PreDispatchZeroProof, "organization_chat_unsupported_request")
}

func organizationChatAuthorizedAPIKey() *APIKey {
	organizationID := int64(88)
	groupID := int64(42)
	role := OrganizationRoleMember
	return &APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID, Status: StatusActive,
		User:  &User{ID: 7, Status: StatusActive, OrganizationID: &organizationID, OrganizationRole: &role},
		Group: &Group{ID: groupID, Status: StatusActive, Hydrated: true, Platform: PlatformOpenAI, RateMultiplier: 1},
		OrganizationAuthorization: &OrganizationKeyAuthorization{
			APIKeyID: 9, UserID: 7, OrganizationID: organizationID, OrganizationStatus: OrganizationStatusActive,
			OrganizationBalanceVersion: 1, AllocationID: 77, AllocationUserID: 7, AllocationOrganizationID: organizationID,
			AllocationStatus: "active", AllocationVersion: 1, Available: "10.00000000", Reserved: "0.00000000",
		},
	}
}
