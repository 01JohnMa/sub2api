package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type organizationGeminiCandidateAccountRepoStub struct {
	AccountRepository
	accounts []Account
}

func (r *organizationGeminiCandidateAccountRepoStub) GetByID(_ context.Context, accountID int64) (*Account, error) {
	for index := range r.accounts {
		if r.accounts[index].ID == accountID {
			account := r.accounts[index]
			return &account, nil
		}
	}
	return nil, nil
}

func (r *organizationGeminiCandidateAccountRepoStub) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	return append([]Account(nil), r.accounts...), nil
}

func (r *organizationGeminiCandidateAccountRepoStub) ListSchedulableByGroupIDAndPlatforms(context.Context, int64, []string) ([]Account, error) {
	return append([]Account(nil), r.accounts...), nil
}

func TestBuildOrganizationGeminiRequestPolicyBoundsNativeOutput(t *testing.T) {
	t.Parallel()

	policy, err := BuildOrganizationGeminiRequestPolicy(OrganizationGeminiRequestPolicyInput{
		EndpointFamily: OrganizationEndpointFamilyGemini,
		RequestShape:   OrganizationRequestShapeModelsAction,
		Action:         "streamGenerateContent",
		RequestedModel: "gemini-3.1-pro",
		Body:           []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`),
	})
	require.NoError(t, err)
	require.True(t, policy.Stream)
	require.Equal(t, organizationGeminiDefaultOutputTokenCeiling, policy.OutputTokenCeiling)
	require.Greater(t, policy.InputTokenCeiling, 0)

	forwardBody, err := ApplyOrganizationGeminiOutputTokenCeiling([]byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`), policy)
	require.NoError(t, err)
	require.EqualValues(t, organizationGeminiDefaultOutputTokenCeiling, gjson.GetBytes(forwardBody, "generationConfig.maxOutputTokens").Int())
}

func TestBuildOrganizationGeminiRequestPolicyRejectsUnboundedOrUnsupportedNativeShape(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		action string
		body   []byte
	}{
		{
			name:   "unsupported action",
			action: "countTokens",
			body:   []byte(`{"contents":[]}`),
		},
		{
			name:   "output ceiling exceeds verified server cap",
			action: "generateContent",
			body:   []byte(`{"contents":[],"generationConfig":{"maxOutputTokens":65001}}`),
		},
		{
			name:   "invalid JSON",
			action: "generateContent",
			body:   []byte(`{"contents":`),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildOrganizationGeminiRequestPolicy(OrganizationGeminiRequestPolicyInput{
				EndpointFamily: OrganizationEndpointFamilyGemini,
				RequestShape:   OrganizationRequestShapeModelsAction,
				Action:         tc.action,
				RequestedModel: "gemini-3.1-pro",
				Body:           tc.body,
			})
			require.ErrorIs(t, err, ErrOrganizationGeminiUnsupportedRequest)
		})
	}
}

func TestBuildOrganizationGeminiRequestPolicySharesAntigravityAliasBounds(t *testing.T) {
	t.Parallel()

	policy, err := BuildOrganizationGeminiRequestPolicy(OrganizationGeminiRequestPolicyInput{
		EndpointFamily: OrganizationEndpointFamilyAntigravity,
		RequestShape:   OrganizationRequestShapeStreaming,
		Action:         "messages",
		RequestedModel: "claude-sonnet-4",
		Body:           []byte(`{"model":"claude-sonnet-4","max_tokens":512,"messages":[{"role":"user","content":"hello"}]}`),
	})
	require.NoError(t, err)
	require.True(t, policy.Stream)
	require.Equal(t, 512, policy.OutputTokenCeiling)
}

func TestBuildOrganizationGeminiRequestPolicyUsesAntigravityV1BetaShapes(t *testing.T) {
	t.Parallel()

	policy, err := BuildOrganizationGeminiRequestPolicy(OrganizationGeminiRequestPolicyInput{
		EndpointFamily: OrganizationEndpointFamilyAntigravity,
		RequestShape:   OrganizationRequestShapeStreaming,
		Action:         "streamGenerateContent",
		RequestedModel: "gemini-3.1-pro",
		Body:           []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`),
	})
	require.NoError(t, err)
	require.True(t, policy.Stream)

	forwardBody, err := ApplyOrganizationGeminiOutputTokenCeiling([]byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`), policy)
	require.NoError(t, err)
	require.EqualValues(t, organizationGeminiDefaultOutputTokenCeiling, gjson.GetBytes(forwardBody, "generationConfig.maxOutputTokens").Int())
}

func TestCanonicalizeOrganizationGeminiForwardBodyFreezesNativeCompatibilityTransforms(t *testing.T) {
	t.Parallel()

	policy := OrganizationGeminiRequestPolicy{Action: "generateContent"}
	body, err := canonicalizeOrganizationGeminiForwardBody([]byte(`{"contents":[{"parts":[]},{"parts":[{"functionCall":{"name":"lookup","args":{}}}]}]}`), policy)
	require.NoError(t, err)
	require.Len(t, gjson.GetBytes(body, "contents").Array(), 1)
	require.NotEmpty(t, gjson.GetBytes(body, "contents.0.parts.0.thoughtSignature").String())
}

func TestPrepareOrganizationGeminiPayloadFreezesDispatchHashWithoutSnapshotPayload(t *testing.T) {
	t.Parallel()

	account := &Account{Platform: PlatformAntigravity, Credentials: map[string]any{"project_id": "project-test", "access_token": "secret-token"}}
	prepared, err := (&AntigravityGatewayService{}).PrepareOrganizationGeminiPayload(account, "gemini-2.5-flash", "generateContent", []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`))
	require.NoError(t, err)
	require.NotEmpty(t, prepared.Payload)
	require.Equal(t, "streamGenerateContent", prepared.UpstreamAction)

	candidate := OrganizationGeminiCandidate{ForwardBody: prepared.Payload, ForwardBodyHash: organizationGeminiBodyHash(prepared.Payload), PreparedPayload: true, IdentitySummary: prepared.IdentitySummary}
	snapshot, err := json.Marshal(candidate)
	require.NoError(t, err)
	require.NotContains(t, string(snapshot), "project-test")
	require.NotContains(t, string(snapshot), "secret-token")
	require.NotContains(t, string(snapshot), "contents")
	require.Contains(t, string(snapshot), candidate.ForwardBodyHash)
}

func TestPrepareOrganizationMessagesPayloadFreezesThinkingModelAndSnapshot(t *testing.T) {
	t.Parallel()

	account := &Account{Platform: PlatformAntigravity, Credentials: map[string]any{
		"project_id": "project-test", "access_token": "secret-token",
		"model_mapping": map[string]any{"claude-sonnet-4-5": "claude-sonnet-4-5"},
	}}
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":64,"thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hello"}]}`)
	prepared, err := (&AntigravityGatewayService{}).PrepareOrganizationMessagesPayload(context.Background(), account, body)
	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4-5-thinking", prepared.DispatchModel)
	require.NotEmpty(t, prepared.Payload)
	require.Contains(t, string(prepared.Payload), "You are Antigravity")

	candidate := OrganizationGeminiCandidate{ForwardBody: prepared.Payload, ForwardBodyHash: organizationGeminiBodyHash(prepared.Payload), PreparedPayload: true, IdentitySummary: prepared.IdentitySummary}
	snapshot, err := json.Marshal(candidate)
	require.NoError(t, err)
	require.NotContains(t, string(snapshot), "project-test")
	require.NotContains(t, string(snapshot), "secret-token")
	require.NotContains(t, string(snapshot), "You are Antigravity")
	require.Contains(t, string(snapshot), candidate.ForwardBodyHash)
}

func TestOrganizationGeminiPlanPreparesEveryAntigravityOAuthCandidate(t *testing.T) {
	groupID := int64(42)
	organizationID := int64(88)
	role := OrganizationRoleMember
	account := Account{
		ID: 55, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive,
		Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}, Extra: map[string]any{"mixed_scheduling": true},
		Credentials: map[string]any{
			"project_id": "project-test", "access_token": "secret-token",
			"model_mapping": map[string]any{
				"gemini-3.1-pro":    "gemini-3.1-pro",
				"claude-sonnet-4-5": "claude-sonnet-4-5",
			},
		},
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	gateway := &GatewayService{
		accountRepo:    &organizationGeminiCandidateAccountRepoStub{accounts: []Account{account}},
		billingService: NewBillingService(cfg, nil),
		cfg:            cfg,
	}
	apiKey := &APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID,
		User:  &User{ID: 7, Status: StatusActive, OrganizationID: &organizationID, OrganizationRole: &role},
		Group: &Group{ID: groupID, Status: StatusActive, Hydrated: true, Platform: PlatformGemini, RateMultiplier: 1},
		OrganizationAuthorization: &OrganizationKeyAuthorization{
			APIKeyID: 9, UserID: 7, OrganizationID: organizationID, OrganizationStatus: OrganizationStatusActive,
			AllocationID: 77, AllocationUserID: 7, AllocationOrganizationID: organizationID,
			AllocationStatus: domain.OrganizationAllocationStatusActive, Available: "10.00000000",
		},
	}

	for _, testCase := range []struct {
		name           string
		endpointFamily string
		requestShape   string
		action         string
		requestedModel string
		upstreamModel  string
		body           []byte
	}{
		{
			name:           "Gemini mixed Antigravity account",
			endpointFamily: OrganizationEndpointFamilyGemini,
			requestShape:   OrganizationRequestShapeModelsAction,
			action:         "generateContent",
			requestedModel: "gemini-3.1-pro",
			upstreamModel:  "gemini-3.1-pro",
			body:           []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`),
		},
		{
			name:           "Antigravity messages account",
			endpointFamily: OrganizationEndpointFamilyAntigravity,
			requestShape:   OrganizationRequestShapeHTTP,
			action:         "messages",
			requestedModel: "claude-sonnet-4-5",
			upstreamModel:  "claude-sonnet-4-5",
			body:           []byte(`{"model":"claude-sonnet-4-5","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			preparedCalls := 0
			plan, err := gateway.PlanOrganizationGeminiCandidates(context.Background(), OrganizationGeminiCandidatePlanInput{
				APIKey: apiKey, Body: testCase.body, EndpointFamily: testCase.endpointFamily,
				RequestShape: testCase.requestShape, Action: testCase.action, RequestedModel: testCase.requestedModel,
				PrepareCandidate: func(_ *Account, _ string, _ string, action string, _ []byte) ([]byte, string, error) {
					preparedCalls++
					return []byte(`{"frozen_action":"` + action + `"}`), "redacted-identity", nil
				},
			})

			require.NoError(t, err)
			require.Equal(t, 1, preparedCalls)
			require.Len(t, plan.Candidates, 1)
			require.True(t, plan.Candidates[0].PreparedPayload)
			require.Equal(t, testCase.upstreamModel, plan.Candidates[0].UpstreamModel)
			require.Equal(t, []byte(`{"frozen_action":"`+testCase.action+`"}`), plan.Candidates[0].ForwardBody)
			require.NotContains(t, string(plan.CandidateSnapshot), "frozen_action")
		})
	}

	_, err := gateway.PlanOrganizationGeminiCandidates(context.Background(), OrganizationGeminiCandidatePlanInput{
		APIKey: apiKey, Body: []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`),
		EndpointFamily: OrganizationEndpointFamilyGemini, RequestShape: OrganizationRequestShapeModelsAction,
		Action: "generateContent", RequestedModel: "gemini-3.1-pro",
	})
	require.ErrorIs(t, err, ErrOrganizationGeminiNoCandidates)

	_, err = gateway.PlanOrganizationGeminiCandidates(context.Background(), OrganizationGeminiCandidatePlanInput{
		APIKey: apiKey, Body: []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`),
		EndpointFamily: OrganizationEndpointFamilyGemini, RequestShape: OrganizationRequestShapeModelsAction,
		Action: "generateContent", RequestedModel: "gemini-3.1-pro",
		PrepareCandidate: func(*Account, string, string, string, []byte) ([]byte, string, error) {
			return nil, "", nil
		},
	})
	require.ErrorIs(t, err, ErrOrganizationGeminiNoCandidates)
}

func TestOrganizationGeminiCandidatePricingUsesOneFrozenModelAndCeiling(t *testing.T) {
	t.Parallel()

	policy, err := BuildOrganizationGeminiRequestPolicy(OrganizationGeminiRequestPolicyInput{
		EndpointFamily: OrganizationEndpointFamilyGemini,
		RequestShape:   OrganizationRequestShapeModelsAction,
		Action:         "generateContent",
		RequestedModel: "gemini-3.1-pro",
		Body:           []byte(`{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":256}}`),
	})
	require.NoError(t, err)

	billing := NewBillingService(&config.Config{}, nil)
	pricing, err := BuildOrganizationGeminiCandidatePricing(billing, nil, policy, "gemini-3.1-pro", 1, 1)
	require.NoError(t, err)
	require.Equal(t, "gemini-3.1-pro", pricing.Model)
	require.Equal(t, 256, pricing.OutputTokenCeiling)
	require.NotEqual(t, "0.00000000", pricing.MaximumCost)

	actual, err := pricing.CalculateActualCost(billing, UsageTokens{InputTokens: 1, OutputTokens: 1})
	require.NoError(t, err)
	require.NotEqual(t, "0.000000000000000000", actual)
}

func TestOrganizationGeminiCandidateSnapshotOmitsAccountCredentials(t *testing.T) {
	t.Parallel()

	candidate := OrganizationGeminiCandidate{
		AccountID:      12,
		Account:        &Account{ID: 12, Credentials: map[string]any{"access_token": "redacted"}},
		RequestedModel: "gemini-3.1-pro",
		UpstreamModel:  "gemini-3.1-pro",
		Route:          OrganizationGeminiRoute{AccountID: 12, Platform: PlatformGemini},
	}

	snapshot, err := json.Marshal(candidate)
	require.NoError(t, err)
	require.NotContains(t, string(snapshot), "access_token")
	require.NotContains(t, string(snapshot), "redacted")
}
