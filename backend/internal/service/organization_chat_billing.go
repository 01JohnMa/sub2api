package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/tidwall/gjson"
)

var (
	ErrOrganizationChatUnsupportedRequest = infraerrors.BadRequest(
		"ORGANIZATION_CHAT_UNSUPPORTED_REQUEST",
		"the organization Chat Completions request is not supported by the tested billing adapter",
	)
	ErrOrganizationChatNoCandidates = infraerrors.ServiceUnavailable(
		"ORGANIZATION_CHAT_NO_CANDIDATES",
		"no frozen OpenAI Chat Completions candidate is available",
	)
	ErrOrganizationChatCandidateConflict = infraerrors.Conflict(
		"ORGANIZATION_CHAT_CANDIDATE_CONFLICT",
		"a frozen organization Chat Completions candidate is no longer dispatchable",
	)
)

const (
	OrganizationChatRouteResponsesBridge = "chat_to_responses"
	OrganizationChatRouteRawChat         = "openai_chat_completions"
)

type organizationChatDispatchRouteContextKey struct{}

func WithOrganizationChatDispatchRoute(ctx context.Context, route string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, organizationChatDispatchRouteContextKey{}, strings.TrimSpace(route))
}

func organizationChatDispatchRouteFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	route, _ := ctx.Value(organizationChatDispatchRouteContextKey{}).(string)
	return strings.TrimSpace(route)
}

// OrganizationChatRequestPolicy holds the bounded, client-facing Chat shape.
// The upstream may use the Responses bridge, but that must not produce a
// nested organization execution or change this external request contract.
type OrganizationChatRequestPolicy struct {
	RequestedModel               string   `json:"requested_model"`
	RequestShape                 string   `json:"request_shape"`
	Stream                       bool     `json:"stream"`
	OutputTokenCeiling           int      `json:"output_token_ceiling"`
	InputTokenCeiling            int      `json:"input_token_ceiling"`
	MaxInputTokens               int      `json:"max_input_tokens"`
	MaxOutputTokens              int      `json:"max_output_tokens"`
	UsedVerifiedDefaultOutputCap bool     `json:"used_verified_default_output_cap"`
	ServiceTier                  string   `json:"service_tier"`
	Tools                        []string `json:"tools,omitempty"`
}

type OrganizationChatCandidate struct {
	AccountID       int64                                 `json:"account_id"`
	AccountType     string                                `json:"account_type"`
	RequestedModel  string                                `json:"requested_model"`
	UpstreamModel   string                                `json:"upstream_model"`
	RequestShape    string                                `json:"request_shape"`
	ServiceTier     string                                `json:"service_tier"`
	Route           string                                `json:"route"`
	ForwardBodyHash string                                `json:"forward_body_hash"`
	Pricing         OrganizationResponsesCandidatePricing `json:"pricing"`
	Account         *Account                              `json:"-"`
	PlanningBody    []byte                                `json:"-"`
	ForwardBody     []byte                                `json:"-"`
}

type OrganizationChatCandidateSnapshot struct {
	EndpointFamily  string                      `json:"endpoint_family"`
	RequestShape    string                      `json:"request_shape"`
	Candidates      []OrganizationChatCandidate `json:"candidates"`
	MaximumAttempts int                         `json:"maximum_attempts"`
}

type OrganizationChatCandidatePlanInput struct {
	APIKey          *APIKey
	Body            []byte
	RequestShape    string
	MaximumAttempts int
}

type OrganizationChatCandidatePlan struct {
	Policy                    OrganizationChatRequestPolicy
	Candidates                []OrganizationChatCandidate
	CandidateSnapshot         []byte
	PricingSnapshot           []byte
	CandidateRouteMaximumCost string
	BillingFingerprint        string
	MaximumAttempts           int
}

func BuildOrganizationChatRequestPolicy(body []byte, requestShape string) (OrganizationChatRequestPolicy, error) {
	policy := OrganizationChatRequestPolicy{RequestShape: strings.TrimSpace(requestShape)}
	if policy.RequestShape != OrganizationRequestShapeHTTP && policy.RequestShape != OrganizationRequestShapeStreaming {
		return policy, ErrOrganizationChatUnsupportedRequest
	}
	if !gjson.ValidBytes(body) {
		return policy, ErrOrganizationChatUnsupportedRequest
	}
	modelValue := gjson.GetBytes(body, "model")
	model := strings.TrimSpace(modelValue.String())
	if modelValue.Type != gjson.String || model == "" {
		return policy, ErrOrganizationChatUnsupportedRequest
	}
	maxInput, maxOutput, ok := verifiedOrganizationResponsesModelLimits(model)
	if !ok {
		return policy, ErrOrganizationChatUnsupportedRequest
	}
	policy.RequestedModel = model
	policy.MaxInputTokens = maxInput
	policy.MaxOutputTokens = maxOutput

	streamValue := gjson.GetBytes(body, "stream")
	if streamValue.Exists() && streamValue.Type != gjson.True && streamValue.Type != gjson.False {
		return policy, ErrOrganizationChatUnsupportedRequest
	}
	policy.Stream = streamValue.Bool()
	if (policy.RequestShape == OrganizationRequestShapeStreaming) != policy.Stream ||
		(policy.Stream && !verifiedOrganizationResponsesModelSupportsStreaming(model)) {
		return policy, ErrOrganizationChatUnsupportedRequest
	}

	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() && gjson.GetBytes(body, "input").Exists() {
		responsesPolicy, err := BuildOrganizationResponsesRequestPolicy(body, requestShape, "/v1/responses")
		if err != nil {
			return policy, ErrOrganizationChatUnsupportedRequest
		}
		return organizationChatPolicyFromResponses(responsesPolicy), nil
	}
	if !messages.Exists() || messages.Type != gjson.JSON || !messages.IsArray() || organizationChatValueUsesUnboundedContent(messages.Raw) {
		return policy, ErrOrganizationChatUnsupportedRequest
	}
	if gjson.GetBytes(body, "input").Exists() || gjson.GetBytes(body, "response_format").Exists() || gjson.GetBytes(body, "logprobs").Exists() {
		return policy, ErrOrganizationChatUnsupportedRequest
	}
	if n := gjson.GetBytes(body, "n"); n.Exists() && (n.Type != gjson.Number || n.Int() != 1 || n.Float() != 1) {
		return policy, ErrOrganizationChatUnsupportedRequest
	}

	policy.InputTokenCeiling = organizationResponsesInputTokenCeiling(body)
	if policy.InputTokenCeiling > maxInput {
		return policy, ErrOrganizationChatUnsupportedRequest
	}
	maxTokens := gjson.GetBytes(body, "max_tokens")
	maxCompletionTokens := gjson.GetBytes(body, "max_completion_tokens")
	if maxTokens.Exists() && maxCompletionTokens.Exists() {
		return policy, ErrOrganizationChatUnsupportedRequest
	}
	outputValue := maxTokens
	if maxCompletionTokens.Exists() {
		outputValue = maxCompletionTokens
	}
	if outputValue.Exists() {
		if outputValue.Type != gjson.Number || outputValue.Float() != float64(outputValue.Int()) || outputValue.Int() <= 0 || outputValue.Int() > int64(maxOutput) {
			return policy, ErrOrganizationChatUnsupportedRequest
		}
		policy.OutputTokenCeiling = int(outputValue.Int())
	} else {
		policy.OutputTokenCeiling = maxOutput
		policy.UsedVerifiedDefaultOutputCap = true
	}

	serviceTierValue := gjson.GetBytes(body, "service_tier")
	if serviceTierValue.Exists() && serviceTierValue.Type != gjson.String {
		return policy, ErrOrganizationChatUnsupportedRequest
	}
	policy.ServiceTier = normalizeOrganizationResponsesServiceTier(serviceTierValue.String())
	if policy.ServiceTier == "unsupported" {
		return policy, ErrOrganizationChatUnsupportedRequest
	}
	if tools := gjson.GetBytes(body, "tools"); tools.Exists() {
		if tools.Type != gjson.JSON || !tools.IsArray() {
			return policy, ErrOrganizationChatUnsupportedRequest
		}
		for _, tool := range tools.Array() {
			if strings.ToLower(strings.TrimSpace(tool.Get("type").String())) != "function" ||
				strings.TrimSpace(tool.Get("function.name").String()) == "" {
				return policy, ErrOrganizationChatUnsupportedRequest
			}
			policy.Tools = append(policy.Tools, "function")
		}
	}
	return policy, nil
}

func organizationChatValueUsesUnboundedContent(raw string) bool {
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return true
	}
	return organizationChatContentIsUnbounded(value)
}

func organizationChatContentIsUnbounded(value any) bool {
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			if organizationChatContentIsUnbounded(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range current {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "image_url", "input_audio", "audio", "file_id", "file_data":
				return true
			}
			if organizationChatContentIsUnbounded(item) {
				return true
			}
		}
	}
	return false
}

func (s *OpenAIGatewayService) PlanOrganizationChatCandidates(ctx context.Context, input OrganizationChatCandidatePlanInput) (*OrganizationChatCandidatePlan, error) {
	if s == nil || s.accountRepo == nil || input.APIKey == nil || input.APIKey.GroupID == nil {
		return nil, ErrOrganizationChatNoCandidates
	}
	policy, err := BuildOrganizationChatRequestPolicy(input.Body, input.RequestShape)
	if err != nil {
		return nil, err
	}
	if s.checkChannelPricingRestriction(ctx, input.APIKey.GroupID, policy.RequestedModel) {
		return nil, ErrOrganizationChatNoCandidates
	}
	accounts, err := s.listSchedulableAccounts(ctx, input.APIKey.GroupID)
	if err != nil {
		return nil, ErrOrganizationChatNoCandidates
	}
	sort.SliceStable(accounts, func(i, j int) bool {
		if accounts[i].Priority != accounts[j].Priority {
			return accounts[i].Priority < accounts[j].Priority
		}
		if accounts[i].LastUsedAt == nil != (accounts[j].LastUsedAt == nil) {
			return accounts[i].LastUsedAt == nil
		}
		if accounts[i].LastUsedAt != nil && !accounts[i].LastUsedAt.Equal(*accounts[j].LastUsedAt) {
			return accounts[i].LastUsedAt.Before(*accounts[j].LastUsedAt)
		}
		return accounts[i].ID < accounts[j].ID
	})

	maxAttempts := input.MaximumAttempts
	if maxAttempts <= 0 || maxAttempts > organizationResponsesMaximumAttempts(s.cfg) {
		maxAttempts = organizationResponsesMaximumAttempts(s.cfg)
	}
	needsRestrictionCheck := s.needsUpstreamChannelRestrictionCheck(ctx, input.APIKey.GroupID)
	var candidates []OrganizationChatCandidate
	maximumCost := ""
	for _, accountValue := range accounts {
		if len(candidates) >= maxAttempts {
			break
		}
		account := accountValue
		if fresh := s.resolveFreshSchedulableOpenAIAccount(ctx, &account, policy.RequestedModel, false, OpenAIEndpointCapabilityChatCompletions); fresh != nil {
			account = *fresh
		} else {
			continue
		}
		if account.IsOpenAIPassthroughEnabled() || account.Type == AccountTypeSetupToken {
			continue
		}
		upstreamModel := resolveOpenAIAccountUpstreamModelForRequest(&account, policy.RequestedModel, false)
		if account.Type == AccountTypeOAuth {
			upstreamModel = normalizeCodexModel(upstreamModel)
		}
		if strings.TrimSpace(upstreamModel) == "" ||
			(needsRestrictionCheck && s.channelService != nil && s.channelService.IsModelRestricted(ctx, *input.APIKey.GroupID, upstreamModel)) {
			continue
		}
		candidateMaxInput, candidateMaxOutput, ok := verifiedOrganizationResponsesModelLimits(upstreamModel)
		if !ok || (policy.Stream && !verifiedOrganizationResponsesModelSupportsStreaming(upstreamModel)) {
			continue
		}
		effectiveBody, err := s.applyOpenAIFastPolicyToBody(ctx, &account, upstreamModel, input.Body)
		if err != nil {
			continue
		}
		effectivePolicy := policy
		effectivePolicy.MaxInputTokens = candidateMaxInput
		effectivePolicy.MaxOutputTokens = candidateMaxOutput
		effectivePolicy.InputTokenCeiling = organizationResponsesCandidateInputTokenCeiling(input.Body, policy.RequestedModel)
		if effectivePolicy.InputTokenCeiling > candidateMaxInput {
			continue
		}
		if account.Type == AccountTypeOAuth || effectivePolicy.OutputTokenCeiling > candidateMaxOutput {
			effectivePolicy.OutputTokenCeiling = candidateMaxOutput
		}
		effectivePolicy.ServiceTier = normalizeOrganizationResponsesServiceTier(gjson.GetBytes(effectiveBody, "service_tier").String())
		if effectivePolicy.ServiceTier == "unsupported" {
			continue
		}
		multiplier := s.organizationChatRateMultiplier(ctx, input.APIKey)
		pricing, err := buildOrganizationResponsesCandidatePricing(ctx, s.billingService, s.resolver, organizationChatPricingPolicy(effectivePolicy), upstreamModel, multiplier, []float64{account.BillingRateMultiplier()}, input.APIKey.GroupID)
		if err != nil {
			continue
		}
		for _, route := range organizationChatCandidateRoutes(&account) {
			candidate := OrganizationChatCandidate{
				AccountID: account.ID, AccountType: account.Type, RequestedModel: policy.RequestedModel, UpstreamModel: upstreamModel,
				RequestShape: policy.RequestShape, ServiceTier: effectivePolicy.ServiceTier, Route: route,
				ForwardBodyHash: organizationChatBodyHash(effectiveBody), Pricing: pricing, Account: &account,
				PlanningBody: append([]byte(nil), input.Body...), ForwardBody: append([]byte(nil), effectiveBody...),
			}
			candidates = append(candidates, candidate)
			if maximumCost == "" || compareOrganizationMoney(pricing.MaximumCost, maximumCost) > 0 {
				maximumCost = pricing.MaximumCost
			}
			if len(candidates) >= maxAttempts {
				break
			}
		}
	}
	if len(candidates) == 0 || maximumCost == "" {
		return nil, ErrOrganizationChatNoCandidates
	}
	snapshot := OrganizationChatCandidateSnapshot{
		EndpointFamily: OrganizationEndpointFamilyChat, RequestShape: policy.RequestShape, Candidates: candidates, MaximumAttempts: len(candidates),
	}
	candidateJSON, _ := json.Marshal(snapshot)
	pricingJSON, _ := json.Marshal(map[string]any{"policy": policy, "candidates": candidates})
	fingerprint := sha256.Sum256(append(append([]byte(nil), input.Body...), candidateJSON...))
	return &OrganizationChatCandidatePlan{
		Policy: policy, Candidates: candidates, CandidateSnapshot: candidateJSON, PricingSnapshot: pricingJSON,
		CandidateRouteMaximumCost: maximumCost, BillingFingerprint: hex.EncodeToString(fingerprint[:]), MaximumAttempts: len(candidates),
	}, nil
}

func (s *OpenAIGatewayService) RevalidateOrganizationChatCandidate(ctx context.Context, apiKey *APIKey, candidate OrganizationChatCandidate) (*OrganizationChatCandidate, error) {
	if s == nil || apiKey == nil || apiKey.GroupID == nil || candidate.AccountID <= 0 {
		return nil, ErrOrganizationChatCandidateConflict
	}
	account := candidate.Account
	if s.accountRepo != nil {
		latest, err := s.accountRepo.GetByID(ctx, candidate.AccountID)
		if err != nil || latest == nil {
			return nil, ErrOrganizationChatCandidateConflict
		}
		account = latest
	}
	account = s.resolveFreshSchedulableOpenAIAccount(ctx, account, candidate.RequestedModel, false, OpenAIEndpointCapabilityChatCompletions)
	if account == nil || account.IsOpenAIPassthroughEnabled() || !openAIStickyAccountMatchesGroup(account, apiKey.GroupID) {
		return nil, ErrOrganizationChatCandidateConflict
	}
	upstreamModel := resolveOpenAIAccountUpstreamModelForRequest(account, candidate.RequestedModel, false)
	if account.Type == AccountTypeOAuth {
		upstreamModel = normalizeCodexModel(upstreamModel)
	}
	if account.Type == AccountTypeSetupToken || strings.TrimSpace(upstreamModel) == "" ||
		(s.needsUpstreamChannelRestrictionCheck(ctx, apiKey.GroupID) && s.channelService != nil && s.channelService.IsModelRestricted(ctx, *apiKey.GroupID, upstreamModel)) {
		return nil, ErrOrganizationChatCandidateConflict
	}
	if account.ID != candidate.AccountID || account.BillingRateMultiplier() != candidate.Pricing.AccountMultiplier ||
		upstreamModel != candidate.UpstreamModel || !organizationChatCandidateSupportsRoute(account, candidate.Route) {
		return nil, ErrOrganizationChatCandidateConflict
	}
	policy, err := BuildOrganizationChatRequestPolicy(candidate.PlanningBody, candidate.RequestShape)
	if err != nil || policy.RequestedModel != candidate.RequestedModel {
		return nil, ErrOrganizationChatCandidateConflict
	}
	maxInput, maxOutput, ok := verifiedOrganizationResponsesModelLimits(upstreamModel)
	if !ok || (policy.Stream && !verifiedOrganizationResponsesModelSupportsStreaming(upstreamModel)) || policy.InputTokenCeiling > maxInput {
		return nil, ErrOrganizationChatCandidateConflict
	}
	policy.MaxInputTokens = maxInput
	policy.MaxOutputTokens = maxOutput
	policy.InputTokenCeiling = organizationResponsesCandidateInputTokenCeiling(candidate.PlanningBody, candidate.RequestedModel)
	if account.Type == AccountTypeOAuth || policy.OutputTokenCeiling > maxOutput {
		policy.OutputTokenCeiling = maxOutput
	}
	effectiveBody, err := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, candidate.PlanningBody)
	if err != nil || organizationChatBodyHash(effectiveBody) != candidate.ForwardBodyHash {
		return nil, ErrOrganizationChatCandidateConflict
	}
	policy.ServiceTier = normalizeOrganizationResponsesServiceTier(gjson.GetBytes(effectiveBody, "service_tier").String())
	if policy.ServiceTier == "unsupported" || policy.ServiceTier != candidate.ServiceTier {
		return nil, ErrOrganizationChatCandidateConflict
	}
	pricing, err := buildOrganizationResponsesCandidatePricing(ctx, s.billingService, s.resolver, organizationChatPricingPolicy(policy), upstreamModel, s.organizationChatRateMultiplier(ctx, apiKey), []float64{account.BillingRateMultiplier()}, apiKey.GroupID)
	pricingJSON, _ := json.Marshal(pricing)
	frozenPricingJSON, _ := json.Marshal(candidate.Pricing)
	if err != nil || !bytes.Equal(pricingJSON, frozenPricingJSON) {
		return nil, ErrOrganizationChatCandidateConflict
	}
	validated := candidate
	validated.Account = account
	validated.Pricing = pricing
	validated.ForwardBody = append([]byte(nil), effectiveBody...)
	return &validated, nil
}

func organizationChatCandidateRoutes(account *Account) []string {
	if account == nil {
		return nil
	}
	if account.Type != AccountTypeAPIKey {
		return []string{OrganizationChatRouteResponsesBridge}
	}
	switch openai_compat.ResolveResponsesSupport(account.Extra) {
	case openai_compat.ResponsesSupportNo:
		return []string{OrganizationChatRouteRawChat}
	case openai_compat.ResponsesSupportUnknown:
		return []string{OrganizationChatRouteResponsesBridge, OrganizationChatRouteRawChat}
	default:
		return []string{OrganizationChatRouteResponsesBridge}
	}
}

func organizationChatCandidateSupportsRoute(account *Account, route string) bool {
	for _, candidateRoute := range organizationChatCandidateRoutes(account) {
		if candidateRoute == route {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) organizationChatRateMultiplier(ctx context.Context, apiKey *APIKey) float64 {
	if apiKey == nil || apiKey.User == nil || apiKey.Group == nil {
		return 1
	}
	if s.userGroupRateResolver != nil {
		return s.userGroupRateResolver.Resolve(ctx, apiKey.User.ID, apiKey.Group.ID, apiKey.Group.RateMultiplier)
	}
	return apiKey.Group.RateMultiplier
}

func organizationChatPricingPolicy(policy OrganizationChatRequestPolicy) OrganizationResponsesRequestPolicy {
	return OrganizationResponsesRequestPolicy{
		RequestedModel: policy.RequestedModel, RequestShape: policy.RequestShape, Stream: policy.Stream,
		OutputTokenCeiling: policy.OutputTokenCeiling, InputTokenCeiling: policy.InputTokenCeiling,
		MaxInputTokens: policy.MaxInputTokens, MaxOutputTokens: policy.MaxOutputTokens,
		UsedVerifiedDefaultOutputCap: policy.UsedVerifiedDefaultOutputCap, ServiceTier: policy.ServiceTier, Tools: append([]string(nil), policy.Tools...),
	}
}

func organizationChatPolicyFromResponses(policy OrganizationResponsesRequestPolicy) OrganizationChatRequestPolicy {
	return OrganizationChatRequestPolicy{
		RequestedModel: policy.RequestedModel, RequestShape: policy.RequestShape, Stream: policy.Stream,
		OutputTokenCeiling: policy.OutputTokenCeiling, InputTokenCeiling: policy.InputTokenCeiling,
		MaxInputTokens: policy.MaxInputTokens, MaxOutputTokens: policy.MaxOutputTokens,
		UsedVerifiedDefaultOutputCap: policy.UsedVerifiedDefaultOutputCap, ServiceTier: policy.ServiceTier,
		Tools: append([]string(nil), policy.Tools...),
	}
}

func (s *OpenAIGatewayService) CalculateOrganizationChatActualCost(candidate OrganizationChatCandidate, result *OpenAIForwardResult) (string, UsageTokens, error) {
	if s == nil || s.billingService == nil || result == nil || !result.UsageObserved {
		return "", UsageTokens{}, ErrOrganizationChatUnsupportedRequest
	}
	inputTokens := result.Usage.InputTokens - result.Usage.CacheReadInputTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	tokens := UsageTokens{
		InputTokens: inputTokens, OutputTokens: result.Usage.OutputTokens,
		CacheCreationTokens: result.Usage.CacheCreationInputTokens, CacheReadTokens: result.Usage.CacheReadInputTokens,
		ImageOutputTokens: result.Usage.ImageOutputTokens,
	}
	if tokens.ImageOutputTokens > 0 || result.ImageCount > 0 {
		return "", UsageTokens{}, ErrOrganizationChatUnsupportedRequest
	}
	cost, err := candidate.Pricing.CalculateActualCost(s.billingService, tokens)
	return cost, tokens, err
}

func (p OrganizationChatCandidatePlan) NewExecutionReservationInput(apiKey *APIKey, clientRequestID string) (OrganizationExecutionReservationInput, error) {
	attribution, err := organizationResponsesExecutionAttribution(apiKey)
	if err != nil {
		return OrganizationExecutionReservationInput{}, err
	}
	return OrganizationExecutionReservationInput{
		Attribution: attribution, EndpointFamily: OrganizationEndpointFamilyChat, RequestShape: p.Policy.RequestShape,
		Model: p.Policy.RequestedModel, ClientRequestID: clientRequestID, CandidateSnapshot: p.CandidateSnapshot,
		PricingSnapshot: p.PricingSnapshot, CandidateRouteMaximumCost: p.CandidateRouteMaximumCost, BillingFingerprint: p.BillingFingerprint,
	}, nil
}

func NewOrganizationChatPreDispatchRejectionInput(apiKey *APIKey, body []byte, requestShape, clientRequestID string, rejection error) (OrganizationExecutionReservationInput, error) {
	attribution, err := organizationResponsesExecutionAttribution(apiKey)
	if err != nil {
		return OrganizationExecutionReservationInput{}, err
	}
	reason := strings.ToLower(strings.TrimSpace(infraerrors.Reason(rejection)))
	if reason == "" {
		reason = "organization_chat_pre_dispatch_rejected"
	}
	proof := "request_policy_rejected:" + reason
	candidateSnapshot, err := json.Marshal(map[string]any{
		"endpoint_family": OrganizationEndpointFamilyChat, "request_shape": requestShape,
		"requested_model": strings.TrimSpace(gjson.GetBytes(body, "model").String()), "candidates": []any{}, "maximum_attempts": 0, "zero_proof": proof,
	})
	if err != nil {
		return OrganizationExecutionReservationInput{}, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	pricingSnapshot, err := json.Marshal(map[string]any{"rejected": true, "reason": reason, "maximum_cost": "0.00000000"})
	if err != nil {
		return OrganizationExecutionReservationInput{}, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	fingerprint := sha256.Sum256(append(append(append([]byte(nil), body...), candidateSnapshot...), pricingSnapshot...))
	return OrganizationExecutionReservationInput{
		Attribution: attribution, EndpointFamily: OrganizationEndpointFamilyChat, RequestShape: requestShape,
		Model: strings.TrimSpace(gjson.GetBytes(body, "model").String()), ClientRequestID: clientRequestID,
		CandidateSnapshot: candidateSnapshot, PricingSnapshot: pricingSnapshot, CandidateRouteMaximumCost: "0.00000000",
		BillingFingerprint: hex.EncodeToString(fingerprint[:]), PreDispatchZeroProof: proof,
	}, nil
}

func organizationChatBodyHash(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}
