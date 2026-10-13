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
	"github.com/tidwall/gjson"
)

var (
	ErrOrganizationMessagesUnsupportedRequest = infraerrors.BadRequest(
		"ORGANIZATION_MESSAGES_UNSUPPORTED_REQUEST",
		"the organization Messages request is not supported by the tested billing adapter",
	)
	ErrOrganizationMessagesNoCandidates = infraerrors.ServiceUnavailable(
		"ORGANIZATION_MESSAGES_NO_CANDIDATES",
		"no frozen organization Messages candidate is available",
	)
	ErrOrganizationMessagesCandidateConflict = infraerrors.Conflict(
		"ORGANIZATION_MESSAGES_CANDIDATE_CONFLICT",
		"the frozen organization Messages candidate is no longer dispatchable",
	)
)

const (
	organizationMessagesInputProtocolOverhead    = 16 * 1024
	organizationMessagesInputBodyMultiplier      = 2
	organizationMessagesMaximumInputTokenCeiling = 1_048_576
	organizationMessagesMaximumAttempts          = 1
)

type OrganizationMessagesRequestPolicy struct {
	RequestedModel               string   `json:"requested_model"`
	RequestShape                 string   `json:"request_shape"`
	Stream                       bool     `json:"stream"`
	InputTokenCeiling            int      `json:"input_token_ceiling"`
	OutputTokenCeiling           int      `json:"output_token_ceiling"`
	UsedVerifiedDefaultOutputCap bool     `json:"used_verified_default_output_cap"`
	Tools                        []string `json:"tools,omitempty"`
}

type OrganizationMessagesRoute struct {
	AccountID         int64   `json:"account_id"`
	Platform          string  `json:"platform"`
	AccountType       string  `json:"account_type"`
	DispatchModel     string  `json:"dispatch_model"`
	UpstreamModel     string  `json:"upstream_model"`
	AccountMultiplier float64 `json:"account_multiplier"`
}

type OrganizationMessagesCandidate struct {
	AccountID        int64                                 `json:"account_id"`
	RequestedModel   string                                `json:"requested_model"`
	DispatchModel    string                                `json:"dispatch_model"`
	UpstreamModel    string                                `json:"upstream_model"`
	Route            OrganizationMessagesRoute             `json:"route"`
	Pricing          OrganizationResponsesCandidatePricing `json:"pricing"`
	ForwardBodyHash  string                                `json:"forward_body_hash"`
	PlanningBodyHash string                                `json:"planning_body_hash"`
	ForwardBody      []byte                                `json:"-"`
	Account          *Account                              `json:"-"`
}

type OrganizationMessagesCandidateSnapshot struct {
	EndpointFamily  string                          `json:"endpoint_family"`
	RequestShape    string                          `json:"request_shape"`
	Platform        string                          `json:"platform"`
	Candidates      []OrganizationMessagesCandidate `json:"candidates"`
	MaximumAttempts int                             `json:"maximum_attempts"`
}

type OrganizationMessagesCandidatePlan struct {
	EndpointFamily            string
	Policy                    OrganizationMessagesRequestPolicy
	Candidates                []OrganizationMessagesCandidate
	SourceBody                []byte
	CandidateSnapshot         []byte
	PricingSnapshot           []byte
	CandidateRouteMaximumCost string
	BillingFingerprint        string
	MaximumAttempts           int
}

func BuildOrganizationMessagesRequestPolicy(body []byte, requestShape string) (OrganizationMessagesRequestPolicy, error) {
	policy := OrganizationMessagesRequestPolicy{RequestShape: strings.TrimSpace(requestShape)}
	if (policy.RequestShape != OrganizationRequestShapeHTTP && policy.RequestShape != OrganizationRequestShapeStreaming) || !json.Valid(body) {
		return policy, ErrOrganizationMessagesUnsupportedRequest
	}
	modelValue := gjson.GetBytes(body, "model")
	policy.RequestedModel = strings.TrimSpace(modelValue.String())
	if modelValue.Type != gjson.String || policy.RequestedModel == "" {
		return policy, ErrOrganizationMessagesUnsupportedRequest
	}
	streamValue := gjson.GetBytes(body, "stream")
	if streamValue.Exists() && streamValue.Type != gjson.True && streamValue.Type != gjson.False {
		return policy, ErrOrganizationMessagesUnsupportedRequest
	}
	policy.Stream = streamValue.Bool()
	if (policy.RequestShape == OrganizationRequestShapeStreaming) != policy.Stream {
		return policy, ErrOrganizationMessagesUnsupportedRequest
	}
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || messages.Type != gjson.JSON || !messages.IsArray() || organizationMessagesValueUsesUnsupportedContent(messages.Raw) {
		return policy, ErrOrganizationMessagesUnsupportedRequest
	}
	maxTokens := gjson.GetBytes(body, "max_tokens")
	if !maxTokens.Exists() || maxTokens.Type != gjson.Number || maxTokens.Float() != float64(maxTokens.Int()) || maxTokens.Int() <= 0 {
		return policy, ErrOrganizationMessagesUnsupportedRequest
	}
	policy.OutputTokenCeiling = int(maxTokens.Int())
	policy.InputTokenCeiling = (len(body) + organizationMessagesInputProtocolOverhead) * organizationMessagesInputBodyMultiplier
	if policy.InputTokenCeiling <= 0 || policy.InputTokenCeiling > organizationMessagesMaximumInputTokenCeiling {
		return policy, ErrOrganizationMessagesUnsupportedRequest
	}
	if tools := gjson.GetBytes(body, "tools"); tools.Exists() {
		if tools.Type != gjson.JSON || !tools.IsArray() {
			return policy, ErrOrganizationMessagesUnsupportedRequest
		}
		for _, tool := range tools.Array() {
			toolType := strings.ToLower(strings.TrimSpace(tool.Get("type").String()))
			if strings.Contains(toolType, "web_search") || (toolType != "" && toolType != "custom") || strings.TrimSpace(tool.Get("name").String()) == "" {
				return policy, ErrOrganizationMessagesUnsupportedRequest
			}
			inputSchema := tool.Get("input_schema")
			if !inputSchema.Exists() || inputSchema.Type != gjson.JSON || !inputSchema.IsObject() {
				return policy, ErrOrganizationMessagesUnsupportedRequest
			}
			policy.Tools = append(policy.Tools, "custom")
		}
	}
	return policy, nil
}

func organizationMessagesValueUsesUnsupportedContent(raw string) bool {
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return true
	}
	return organizationMessagesContentIsUnsupported(value)
}

func organizationMessagesContentIsUnsupported(value any) bool {
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			if organizationMessagesContentIsUnsupported(item) {
				return true
			}
		}
	case map[string]any:
		blockType := strings.ToLower(strings.TrimSpace(stringValue(current["type"])))
		switch blockType {
		case "image", "document", "server_tool_use", "web_search_tool_result":
			return true
		}
		for key, item := range current {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "image_url", "file_id", "file_data", "input_audio", "audio":
				return true
			}
			if organizationMessagesContentIsUnsupported(item) {
				return true
			}
		}
	}
	return false
}

func (s *GatewayService) PlanOrganizationGatewayMessagesCandidate(ctx context.Context, apiKey *APIKey, body []byte, requestShape string) (*OrganizationMessagesCandidatePlan, error) {
	if s == nil || s.accountRepo == nil || s.billingService == nil || apiKey == nil || apiKey.GroupID == nil || apiKey.Group == nil {
		return nil, ErrOrganizationMessagesNoCandidates
	}
	policy, err := BuildOrganizationMessagesRequestPolicy(body, requestShape)
	if err != nil {
		return nil, err
	}
	platform := strings.TrimSpace(apiKey.Group.Platform)
	if platform != PlatformAnthropic && platform != PlatformGemini {
		return nil, ErrOrganizationMessagesUnsupportedRequest
	}
	if s.checkChannelPricingRestriction(ctx, apiKey.GroupID, policy.RequestedModel) {
		return nil, ErrOrganizationMessagesNoCandidates
	}
	accounts, _, err := s.listSchedulableAccounts(ctx, apiKey.GroupID, platform, true)
	if err != nil {
		return nil, ErrOrganizationMessagesNoCandidates
	}
	sortOrganizationMessagesAccounts(accounts)
	dispatchModel := policy.RequestedModel
	if mapping, _ := s.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, policy.RequestedModel); mapping.Mapped {
		dispatchModel = strings.TrimSpace(mapping.MappedModel)
	}
	if dispatchModel == "" {
		return nil, ErrOrganizationMessagesNoCandidates
	}
	multiplier := organizationGatewayChatRateMultiplier(s, ctx, apiKey)
	for index := range accounts {
		account := accounts[index]
		if account.Platform != platform || account.IsBedrock() || account.IsAnthropicAPIKeyPassthroughEnabled() || !s.isAccountSchedulableForSelection(&account) ||
			!s.isAccountSchedulableForModelSelection(ctx, &account, dispatchModel) || !s.isModelSupportedByAccountWithContext(ctx, &account, dispatchModel) {
			continue
		}
		upstreamModel := organizationGatewayChatUpstreamModel(&account, dispatchModel)
		if upstreamModel == "" || policy.OutputTokenCeiling > organizationGatewayChatDefaultOutputTokenCeiling {
			continue
		}
		if s.needsUpstreamChannelRestrictionCheck(ctx, apiKey.GroupID) && s.channelService != nil && s.channelService.IsModelRestricted(ctx, *apiKey.GroupID, upstreamModel) {
			continue
		}
		pricing, err := buildOrganizationMessagesPricing(ctx, s.billingService, s.resolver, policy, upstreamModel, multiplier, account.BillingRateMultiplier(), apiKey.GroupID, platform, "")
		if err != nil {
			continue
		}
		forwardBody := s.ReplaceModelInBody(body, dispatchModel)
		candidate := newOrganizationMessagesCandidate(policy, platform, dispatchModel, upstreamModel, body, forwardBody, account, pricing)
		return buildOrganizationMessagesPlan(policy, platform, body, candidate)
	}
	return nil, ErrOrganizationMessagesNoCandidates
}

func (s *OpenAIGatewayService) PlanOrganizationOpenAIMessagesCandidate(ctx context.Context, apiKey *APIKey, body []byte, requestShape string) (*OrganizationMessagesCandidatePlan, error) {
	if s == nil || s.accountRepo == nil || s.billingService == nil || apiKey == nil || apiKey.GroupID == nil || apiKey.Group == nil || apiKey.Group.Platform != PlatformOpenAI {
		return nil, ErrOrganizationMessagesNoCandidates
	}
	policy, err := BuildOrganizationMessagesRequestPolicy(body, requestShape)
	if err != nil {
		return nil, err
	}
	normalizedModel := NormalizeOpenAICompatRequestedModel(policy.RequestedModel)
	preferredMappedModel := strings.TrimSpace(apiKey.Group.ResolveMessagesDispatchModel(policy.RequestedModel))
	if normalizedModel == "" || preferredMappedModel == "" || s.checkChannelPricingRestriction(ctx, apiKey.GroupID, preferredMappedModel) {
		return nil, ErrOrganizationMessagesNoCandidates
	}
	accounts, err := s.listSchedulableAccounts(ctx, apiKey.GroupID)
	if err != nil {
		return nil, ErrOrganizationMessagesNoCandidates
	}
	sortOrganizationMessagesAccounts(accounts)
	for index := range accounts {
		account := accounts[index]
		fresh := s.resolveFreshSchedulableOpenAIAccount(ctx, &account, preferredMappedModel, false, OpenAIEndpointCapabilityChatCompletions)
		if !s.organizationOpenAIMessagesAccountSupported(fresh) {
			continue
		}
		account = *fresh
		dispatchModel := resolveOpenAIForwardModel(&account, normalizedModel, preferredMappedModel)
		upstreamModel := normalizeOpenAIModelForUpstream(&account, dispatchModel)
		_, outputCap, verified := verifiedOrganizationResponsesModelLimits(upstreamModel)
		if !verified || policy.OutputTokenCeiling > outputCap || (policy.Stream && !verifiedOrganizationResponsesModelSupportsStreaming(upstreamModel)) {
			continue
		}
		if s.needsUpstreamChannelRestrictionCheck(ctx, apiKey.GroupID) && s.channelService != nil && s.channelService.IsModelRestricted(ctx, *apiKey.GroupID, upstreamModel) {
			continue
		}
		multiplier := organizationOpenAIMessagesRateMultiplier(s, ctx, apiKey)
		pricing, err := buildOrganizationMessagesPricing(ctx, s.billingService, s.resolver, policy, upstreamModel, multiplier, account.BillingRateMultiplier(), apiKey.GroupID, PlatformOpenAI, "priority")
		if err != nil {
			continue
		}
		candidate := newOrganizationMessagesCandidate(policy, PlatformOpenAI, dispatchModel, upstreamModel, body, body, account, pricing)
		candidate.Route.AccountType = account.Type
		return buildOrganizationMessagesPlan(policy, PlatformOpenAI, body, candidate)
	}
	return nil, ErrOrganizationMessagesNoCandidates
}

func (s *GatewayService) RevalidateOrganizationGatewayMessagesCandidate(ctx context.Context, apiKey *APIKey, candidate OrganizationMessagesCandidate, policy OrganizationMessagesRequestPolicy, sourceBody []byte) (*OrganizationMessagesCandidate, error) {
	if s == nil || s.accountRepo == nil || s.billingService == nil || apiKey == nil || apiKey.GroupID == nil || apiKey.Group == nil ||
		organizationMessagesBodyHash(sourceBody) != candidate.PlanningBodyHash {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	platform := strings.TrimSpace(apiKey.Group.Platform)
	if platform != PlatformAnthropic && platform != PlatformGemini || platform != candidate.Route.Platform {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	account, err := s.accountRepo.GetByID(ctx, candidate.AccountID)
	if err != nil || account == nil || account.IsBedrock() || account.IsAnthropicAPIKeyPassthroughEnabled() || account.Platform != platform || !s.isAccountInGroup(account, apiKey.GroupID) || !s.isAccountSchedulableForSelection(account) {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	dispatchModel := policy.RequestedModel
	if mapping, _ := s.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, policy.RequestedModel); mapping.Mapped {
		dispatchModel = strings.TrimSpace(mapping.MappedModel)
	}
	upstreamModel := organizationGatewayChatUpstreamModel(account, dispatchModel)
	if dispatchModel != candidate.DispatchModel || upstreamModel != candidate.UpstreamModel || policy.OutputTokenCeiling > organizationGatewayChatDefaultOutputTokenCeiling ||
		!s.isAccountSchedulableForModelSelection(ctx, account, dispatchModel) || !s.isModelSupportedByAccountWithContext(ctx, account, dispatchModel) {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	if s.needsUpstreamChannelRestrictionCheck(ctx, apiKey.GroupID) && s.channelService != nil && s.channelService.IsModelRestricted(ctx, *apiKey.GroupID, upstreamModel) {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	pricing, err := buildOrganizationMessagesPricing(ctx, s.billingService, s.resolver, policy, upstreamModel, organizationGatewayChatRateMultiplier(s, ctx, apiKey), account.BillingRateMultiplier(), apiKey.GroupID, platform, "")
	forwardBody := s.ReplaceModelInBody(sourceBody, dispatchModel)
	if err != nil || !sameOrganizationMessagesPricing(pricing, candidate.Pricing) || organizationMessagesBodyHash(forwardBody) != candidate.ForwardBodyHash || account.Type != candidate.Route.AccountType || account.BillingRateMultiplier() != candidate.Route.AccountMultiplier {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	validated := candidate
	validated.Account = account
	validated.Pricing = pricing
	validated.ForwardBody = append([]byte(nil), forwardBody...)
	return &validated, nil
}

func (s *OpenAIGatewayService) RevalidateOrganizationOpenAIMessagesCandidate(ctx context.Context, apiKey *APIKey, candidate OrganizationMessagesCandidate, policy OrganizationMessagesRequestPolicy, sourceBody []byte) (*OrganizationMessagesCandidate, error) {
	if s == nil || s.accountRepo == nil || s.billingService == nil || apiKey == nil || apiKey.GroupID == nil || apiKey.Group == nil || apiKey.Group.Platform != PlatformOpenAI ||
		organizationMessagesBodyHash(sourceBody) != candidate.PlanningBodyHash {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	normalizedModel := NormalizeOpenAICompatRequestedModel(policy.RequestedModel)
	preferredMappedModel := strings.TrimSpace(apiKey.Group.ResolveMessagesDispatchModel(policy.RequestedModel))
	if normalizedModel == "" || preferredMappedModel == "" {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	account, err := s.accountRepo.GetByID(ctx, candidate.AccountID)
	if err != nil || account == nil {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	account = s.resolveFreshSchedulableOpenAIAccount(ctx, account, preferredMappedModel, false, OpenAIEndpointCapabilityChatCompletions)
	if !s.organizationOpenAIMessagesAccountSupported(account) || !openAIStickyAccountMatchesGroup(account, apiKey.GroupID) {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	dispatchModel := resolveOpenAIForwardModel(account, normalizedModel, preferredMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, dispatchModel)
	_, outputCap, verified := verifiedOrganizationResponsesModelLimits(upstreamModel)
	if !verified || policy.OutputTokenCeiling > outputCap || dispatchModel != candidate.DispatchModel || upstreamModel != candidate.UpstreamModel || (policy.Stream && !verifiedOrganizationResponsesModelSupportsStreaming(upstreamModel)) {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	if s.needsUpstreamChannelRestrictionCheck(ctx, apiKey.GroupID) && s.channelService != nil && s.channelService.IsModelRestricted(ctx, *apiKey.GroupID, upstreamModel) {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	pricing, err := buildOrganizationMessagesPricing(ctx, s.billingService, s.resolver, policy, upstreamModel, organizationOpenAIMessagesRateMultiplier(s, ctx, apiKey), account.BillingRateMultiplier(), apiKey.GroupID, PlatformOpenAI, "priority")
	if err != nil || !sameOrganizationMessagesPricing(pricing, candidate.Pricing) || account.Type != candidate.Route.AccountType || account.BillingRateMultiplier() != candidate.Route.AccountMultiplier {
		return nil, ErrOrganizationMessagesCandidateConflict
	}
	validated := candidate
	validated.Account = account
	validated.Pricing = pricing
	validated.ForwardBody = append([]byte(nil), sourceBody...)
	return &validated, nil
}

func (s *GatewayService) CalculateOrganizationGatewayMessagesActualCost(candidate OrganizationMessagesCandidate, result *ForwardResult) (string, UsageTokens, error) {
	if s == nil || s.billingService == nil || result == nil {
		return "", UsageTokens{}, ErrOrganizationMessagesUnsupportedRequest
	}
	tokens := UsageTokens{
		InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens,
		CacheCreationTokens: result.Usage.CacheCreationInputTokens, CacheReadTokens: result.Usage.CacheReadInputTokens,
		CacheCreation5mTokens: result.Usage.CacheCreation5mTokens, CacheCreation1hTokens: result.Usage.CacheCreation1hTokens,
		ImageOutputTokens: result.Usage.ImageOutputTokens,
	}
	if organizationMessagesUsageUnknown(tokens, result.ImageCount) {
		return "", tokens, ErrOrganizationExecutionPendingReconciliation
	}
	cost, err := candidate.Pricing.CalculateActualCost(s.billingService, tokens)
	return cost, tokens, err
}

func (s *OpenAIGatewayService) CalculateOrganizationOpenAIMessagesActualCost(candidate OrganizationMessagesCandidate, result *OpenAIForwardResult) (string, UsageTokens, error) {
	if s == nil || s.billingService == nil || result == nil || !result.UsageObserved {
		return "", UsageTokens{}, ErrOrganizationExecutionPendingReconciliation
	}
	actualInput := result.Usage.InputTokens - result.Usage.CacheReadInputTokens
	if actualInput < 0 {
		actualInput = 0
	}
	tokens := UsageTokens{
		InputTokens: actualInput, OutputTokens: result.Usage.OutputTokens,
		CacheCreationTokens: result.Usage.CacheCreationInputTokens, CacheReadTokens: result.Usage.CacheReadInputTokens,
		ImageOutputTokens: result.Usage.ImageOutputTokens,
	}
	if organizationMessagesUsageUnknown(tokens, result.ImageCount) {
		return "", tokens, ErrOrganizationExecutionPendingReconciliation
	}
	pricing := candidate.Pricing
	pricing.ServiceTier = ""
	if result.ServiceTier != nil {
		pricing.ServiceTier = normalizeOrganizationResponsesServiceTier(*result.ServiceTier)
		if pricing.ServiceTier == "unsupported" {
			return "", tokens, ErrOrganizationExecutionPendingReconciliation
		}
	}
	cost, err := pricing.CalculateActualCost(s.billingService, tokens)
	return cost, tokens, err
}

func (p OrganizationMessagesCandidatePlan) NewExecutionReservationInput(apiKey *APIKey, clientRequestID string) (OrganizationExecutionReservationInput, error) {
	attribution, err := organizationResponsesExecutionAttribution(apiKey)
	if err != nil {
		return OrganizationExecutionReservationInput{}, err
	}
	return OrganizationExecutionReservationInput{
		Attribution: attribution, EndpointFamily: OrganizationEndpointFamilyMessages, RequestShape: p.Policy.RequestShape,
		Model: p.Policy.RequestedModel, ClientRequestID: clientRequestID,
		CandidateSnapshot: p.CandidateSnapshot, PricingSnapshot: p.PricingSnapshot,
		CandidateRouteMaximumCost: p.CandidateRouteMaximumCost, BillingFingerprint: p.BillingFingerprint,
	}, nil
}

func NewOrganizationMessagesPreDispatchRejectionInput(apiKey *APIKey, body []byte, requestShape, clientRequestID string, rejection error) (OrganizationExecutionReservationInput, error) {
	attribution, err := organizationResponsesExecutionAttribution(apiKey)
	if err != nil {
		return OrganizationExecutionReservationInput{}, err
	}
	reason := strings.ToLower(strings.TrimSpace(infraerrors.Reason(rejection)))
	if reason == "" {
		reason = "organization_messages_pre_dispatch_rejected"
	}
	proof := "request_policy_rejected:" + reason
	candidateSnapshot, err := json.Marshal(map[string]any{
		"endpoint_family": OrganizationEndpointFamilyMessages, "request_shape": requestShape,
		"requested_model": strings.TrimSpace(gjson.GetBytes(body, "model").String()),
		"candidates":      []any{}, "maximum_attempts": 0, "zero_proof": proof,
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
		Attribution: attribution, EndpointFamily: OrganizationEndpointFamilyMessages, RequestShape: requestShape,
		Model: strings.TrimSpace(gjson.GetBytes(body, "model").String()), ClientRequestID: clientRequestID,
		CandidateSnapshot: candidateSnapshot, PricingSnapshot: pricingSnapshot,
		CandidateRouteMaximumCost: "0.00000000", BillingFingerprint: hex.EncodeToString(fingerprint[:]), PreDispatchZeroProof: proof,
	}, nil
}

func buildOrganizationMessagesPricing(ctx context.Context, billing *BillingService, resolver *ModelPricingResolver, policy OrganizationMessagesRequestPolicy, model string, multiplier, accountMultiplier float64, groupID *int64, platform, ceilingServiceTier string) (OrganizationResponsesCandidatePricing, error) {
	if platform == PlatformAnthropic || platform == PlatformGemini {
		gatewayPricing, err := buildOrganizationGatewayChatPricing(ctx, billing, resolver, OrganizationGatewayChatRequestPolicy{
			RequestedModel: policy.RequestedModel, RequestShape: policy.RequestShape, Stream: policy.Stream,
			InputTokenCeiling: policy.InputTokenCeiling, OutputTokenCeiling: policy.OutputTokenCeiling,
			UsedVerifiedDefaultOutputCap: policy.UsedVerifiedDefaultOutputCap, Tools: append([]string(nil), policy.Tools...),
		}, model, multiplier, accountMultiplier, groupID)
		if err != nil {
			return OrganizationResponsesCandidatePricing{}, err
		}
		return OrganizationResponsesCandidatePricing{
			Model: gatewayPricing.Model, RateMultiplier: gatewayPricing.RateMultiplier, AccountMultiplier: gatewayPricing.AccountMultiplier,
			InputTokenCeiling: gatewayPricing.InputTokenCeiling, OutputTokenCeiling: gatewayPricing.OutputTokenCeiling,
			MaximumCost: gatewayPricing.MaximumCost, Pricing: gatewayPricing.Pricing, Resolved: gatewayPricing.Resolved,
		}, nil
	}
	if platform != PlatformOpenAI {
		return OrganizationResponsesCandidatePricing{}, ErrOrganizationMessagesUnsupportedRequest
	}
	responsesPolicy := OrganizationResponsesRequestPolicy{
		RequestedModel: policy.RequestedModel, RequestShape: policy.RequestShape, Stream: policy.Stream,
		InputTokenCeiling: policy.InputTokenCeiling, OutputTokenCeiling: policy.OutputTokenCeiling,
		ServiceTier: ceilingServiceTier,
	}
	return buildOrganizationResponsesCandidatePricing(ctx, billing, resolver, responsesPolicy, model, multiplier, []float64{accountMultiplier}, groupID)
}

func newOrganizationMessagesCandidate(policy OrganizationMessagesRequestPolicy, platform, dispatchModel, upstreamModel string, sourceBody, forwardBody []byte, account Account, pricing OrganizationResponsesCandidatePricing) OrganizationMessagesCandidate {
	return OrganizationMessagesCandidate{
		AccountID: account.ID, RequestedModel: policy.RequestedModel, DispatchModel: dispatchModel, UpstreamModel: upstreamModel,
		Route: OrganizationMessagesRoute{
			AccountID: account.ID, Platform: platform, AccountType: account.Type, DispatchModel: dispatchModel,
			UpstreamModel: upstreamModel, AccountMultiplier: account.BillingRateMultiplier(),
		},
		Pricing: pricing, ForwardBodyHash: organizationMessagesBodyHash(forwardBody), PlanningBodyHash: organizationMessagesBodyHash(sourceBody),
		ForwardBody: append([]byte(nil), forwardBody...), Account: &account,
	}
}

func buildOrganizationMessagesPlan(policy OrganizationMessagesRequestPolicy, platform string, body []byte, candidate OrganizationMessagesCandidate) (*OrganizationMessagesCandidatePlan, error) {
	snapshot := OrganizationMessagesCandidateSnapshot{
		EndpointFamily: OrganizationEndpointFamilyMessages, RequestShape: policy.RequestShape, Platform: platform,
		Candidates: []OrganizationMessagesCandidate{candidate}, MaximumAttempts: organizationMessagesMaximumAttempts,
	}
	candidateSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return nil, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	pricingSnapshot, err := json.Marshal(map[string]any{"policy": policy, "candidates": []OrganizationMessagesCandidate{candidate}})
	if err != nil {
		return nil, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	fingerprint := sha256.Sum256(append(append(append([]byte(nil), body...), candidateSnapshot...), pricingSnapshot...))
	return &OrganizationMessagesCandidatePlan{
		EndpointFamily: OrganizationEndpointFamilyMessages, Policy: policy, Candidates: []OrganizationMessagesCandidate{candidate}, SourceBody: append([]byte(nil), body...),
		CandidateSnapshot: candidateSnapshot, PricingSnapshot: pricingSnapshot, CandidateRouteMaximumCost: candidate.Pricing.MaximumCost,
		BillingFingerprint: hex.EncodeToString(fingerprint[:]), MaximumAttempts: organizationMessagesMaximumAttempts,
	}, nil
}

func sortOrganizationMessagesAccounts(accounts []Account) {
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
}

func organizationOpenAIMessagesRateMultiplier(s *OpenAIGatewayService, ctx context.Context, apiKey *APIKey) float64 {
	if apiKey == nil || apiKey.User == nil || apiKey.Group == nil {
		return 1
	}
	if s != nil && s.userGroupRateResolver != nil {
		return s.userGroupRateResolver.Resolve(ctx, apiKey.User.ID, apiKey.Group.ID, apiKey.Group.RateMultiplier)
	}
	return apiKey.Group.RateMultiplier
}

func (s *OpenAIGatewayService) organizationOpenAIMessagesAccountSupported(account *Account) bool {
	if account == nil || account.IsOpenAIPassthroughEnabled() || account.Type == AccountTypeSetupToken {
		return false
	}
	// OAuth applies the configured template after reservation. Until the exact
	// transformed provider body is frozen, a non-empty template has no bounded
	// input ceiling and must remain outside the tested organization adapter.
	return account.Type != AccountTypeOAuth || s == nil || s.cfg == nil || strings.TrimSpace(s.cfg.Gateway.ForcedCodexInstructionsTemplate) == ""
}

func organizationMessagesUsageUnknown(tokens UsageTokens, imageCount int) bool {
	return (tokens.InputTokens == 0 && tokens.OutputTokens == 0 && tokens.CacheCreationTokens == 0 && tokens.CacheReadTokens == 0 && tokens.CacheCreation5mTokens == 0 && tokens.CacheCreation1hTokens == 0) ||
		tokens.ImageOutputTokens > 0 || imageCount > 0
}

func organizationMessagesBodyHash(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func sameOrganizationMessagesPricing(left, right OrganizationResponsesCandidatePricing) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	text, _ := value.(string)
	return text
}
