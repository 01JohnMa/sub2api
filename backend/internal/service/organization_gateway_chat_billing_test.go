package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type organizationGatewayChatAccountRepoStub struct {
	AccountRepository
	accounts []Account
}

func (r *organizationGatewayChatAccountRepoStub) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, _ string) ([]Account, error) {
	return append([]Account(nil), r.accounts...), nil
}

func (r *organizationGatewayChatAccountRepoStub) GetByID(_ context.Context, accountID int64) (*Account, error) {
	for index := range r.accounts {
		if r.accounts[index].ID == accountID {
			account := r.accounts[index]
			return &account, nil
		}
	}
	return nil, nil
}

func TestOrganizationGatewayChatPlanFreezesAnthropicStreamCandidate(t *testing.T) {
	groupID := int64(42)
	organizationID := int64(88)
	role := OrganizationRoleMember
	accountMultiplier := 1.1
	account := Account{
		ID: 55, Name: "anthropic", Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{groupID}, AccountGroups: []AccountGroup{{GroupID: groupID}},
		RateMultiplier: &accountMultiplier,
		Credentials:    map[string]any{"api_key": "test-key", "model_mapping": map[string]any{"claude-client": "claude-sonnet-4"}},
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	billing := NewBillingService(cfg, nil)
	gateway := &GatewayService{accountRepo: &organizationGatewayChatAccountRepoStub{accounts: []Account{account}}, cfg: cfg, billingService: billing}
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
	body := []byte(`{"model":"claude-client","messages":[{"role":"user","content":"hello"}],"stream":true,"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`)

	plan, err := gateway.PlanOrganizationGatewayChatCandidates(context.Background(), OrganizationGatewayChatCandidatePlanInput{
		APIKey: apiKey, Body: body, RequestShape: OrganizationRequestShapeStreaming, MaximumAttempts: 2,
	})

	require.NoError(t, err)
	require.Len(t, plan.Candidates, 1)
	require.True(t, plan.Policy.Stream)
	require.Equal(t, []string{"function"}, plan.Policy.Tools)
	require.Equal(t, organizationGatewayChatDefaultOutputTokenCeiling, plan.Policy.OutputTokenCeiling)
	require.True(t, plan.Policy.UsedVerifiedDefaultOutputCap)
	require.Equal(t, PlatformAnthropic, plan.Candidates[0].Route.Platform)
	require.Equal(t, "claude-client", plan.Candidates[0].DispatchModel)
	require.Equal(t, "claude-sonnet-4", plan.Candidates[0].UpstreamModel)
	require.Equal(t, 1.25, plan.Candidates[0].Pricing.RateMultiplier)
	require.Equal(t, 1.1, plan.Candidates[0].Pricing.AccountMultiplier)
	require.EqualValues(t, organizationGatewayChatDefaultOutputTokenCeiling, gjson.GetBytes(plan.Candidates[0].ForwardBody, "max_tokens").Int())
	require.Equal(t, (len(plan.Candidates[0].ForwardBody)+organizationGatewayChatInputProtocolOverhead)*organizationGatewayChatInputBodyMultiplier, plan.Policy.InputTokenCeiling)
	require.NotContains(t, string(plan.CandidateSnapshot), "test-key")

	validated, err := gateway.RevalidateOrganizationGatewayChatCandidate(context.Background(), apiKey, plan.Candidates[0], plan.Policy, body)
	require.NoError(t, err)
	require.Equal(t, plan.Candidates[0].ForwardBodyHash, validated.ForwardBodyHash)

	actual, tokens, err := gateway.CalculateOrganizationGatewayChatActualCost(plan.Candidates[0], &ForwardResult{
		Usage: ClaudeUsage{InputTokens: 12, OutputTokens: 4, CacheCreationInputTokens: 2, CacheReadInputTokens: 3},
	})
	require.NoError(t, err)
	require.NotEmpty(t, actual)
	require.Equal(t, 12, tokens.InputTokens)

	_, _, err = gateway.CalculateOrganizationGatewayChatActualCost(plan.Candidates[0], &ForwardResult{})
	require.ErrorIs(t, err, ErrOrganizationExecutionPendingReconciliation)
}

func TestBuildOrganizationGatewayChatRequestPolicyRejectsUnsafeShapes(t *testing.T) {
	_, err := BuildOrganizationGatewayChatRequestPolicy(
		[]byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`),
		OrganizationRequestShapeHTTP,
	)
	require.ErrorIs(t, err, ErrOrganizationGatewayChatUnsupportedRequest)

	_, err = BuildOrganizationGatewayChatRequestPolicy(
		[]byte(`{"model":"claude-sonnet-4","messages":[],"max_tokens":32,"max_completion_tokens":32}`),
		OrganizationRequestShapeHTTP,
	)
	require.ErrorIs(t, err, ErrOrganizationGatewayChatUnsupportedRequest)
}

type organizationGatewayChatFailingWriter struct {
	gin.ResponseWriter
}

func (organizationGatewayChatFailingWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func (organizationGatewayChatFailingWriter) WriteString(string) (int, error) {
	return 0, errors.New("client disconnected")
}

func TestOrganizationGatewayChatAnthropicStreamMarksClientDisconnectWithObservedUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Writer = organizationGatewayChatFailingWriter{ResponseWriter: c.Writer}
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"anthropic-disconnect"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_disconnect","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4","stop_reason":"","usage":{"input_tokens":8}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleCCStreamingFromAnthropic(resp, c, "claude-sonnet-4", "claude-sonnet-4", nil, time.Now(), true)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 8, result.Usage.InputTokens)
}

type organizationGatewayChatUnexpectedEOFBody struct {
	reader *strings.Reader
}

func (b *organizationGatewayChatUnexpectedEOFBody) Read(p []byte) (int, error) {
	if b.reader.Len() > 0 {
		return b.reader.Read(p)
	}
	return 0, io.ErrUnexpectedEOF
}

func (*organizationGatewayChatUnexpectedEOFBody) Close() error {
	return nil
}

func TestOrganizationGatewayChatAnthropicPartialStreamReturnsReconciliationSignal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"anthropic-partial"}},
		Body: &organizationGatewayChatUnexpectedEOFBody{reader: strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_partial","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4","stop_reason":"","usage":{"input_tokens":8}}}`,
			``,
		}, "\n"))},
	}

	result, err := (&GatewayService{}).handleCCStreamingFromAnthropic(resp, c, "claude-sonnet-4", "claude-sonnet-4", nil, time.Now(), true)

	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.NotNil(t, result)
	require.False(t, result.ClientDisconnect)
	require.Equal(t, 8, result.Usage.InputTokens)
}

func TestOrganizationGatewayChatAnthropicAttemptReturnsTypedLocalFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	result, err := (&GatewayService{}).ForwardAsChatCompletionsOrganizationAttempt(context.Background(), c, nil, []byte(`{`), nil)

	require.Nil(t, result)
	var localErr *OpenAILocalPreDispatchError
	require.ErrorAs(t, err, &localErr)
	require.Equal(t, "local_policy_rejected", localErr.ZeroProof)
	require.False(t, c.Writer.Written())
}
