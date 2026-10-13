package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

var (
	ErrOrganizationGatewayChatUnsupportedRequest = infraerrors.BadRequest(
		"ORGANIZATION_GATEWAY_CHAT_UNSUPPORTED_REQUEST",
		"the organization non-OpenAI Chat Completions request is not supported by the tested billing adapter",
	)
	ErrOrganizationGatewayChatNoCandidates = infraerrors.ServiceUnavailable(
		"ORGANIZATION_GATEWAY_CHAT_NO_CANDIDATES",
		"no frozen non-OpenAI Chat Completions candidate is available",
	)
	ErrOrganizationGatewayChatCandidateConflict = infraerrors.Conflict(
		"ORGANIZATION_GATEWAY_CHAT_CANDIDATE_CONFLICT",
		"a frozen non-OpenAI Chat Completions candidate is no longer dispatchable",
	)
)

const (
	organizationGatewayChatDefaultOutputTokenCeiling = 65000
	organizationGatewayChatMaximumInputTokenCeiling  = 1_048_576
	organizationGatewayChatInputProtocolOverhead     = 16 * 1024
	organizationGatewayChatInputBodyMultiplier       = 2
	organizationGatewayChatDefaultMaximumAttempts    = 4
)

// OrganizationGatewayChatRequestPolicy is the bounded client-facing Chat
// contract used by the Anthropic and Gemini compatibility adapters. The exact
// converted provider payload stays owned by the forwarding service; this
// policy records only the values required to price the external request.
type OrganizationGatewayChatRequestPolicy struct {
	RequestedModel               string   `json:"requested_model"`
	RequestShape                 string   `json:"request_shape"`
	Stream                       bool     `json:"stream"`
	InputTokenCeiling            int      `json:"input_token_ceiling"`
	OutputTokenCeiling           int      `json:"output_token_ceiling"`
	UsedVerifiedDefaultOutputCap bool     `json:"used_verified_default_output_cap"`
	Tools                        []string `json:"tools,omitempty"`
}

type OrganizationGatewayChatRoute struct {
	AccountID         int64   `json:"account_id"`
	Platform          string  `json:"platform"`
	AccountType       string  `json:"account_type"`
	DispatchModel     string  `json:"dispatch_model"`
	UpstreamModel     string  `json:"upstream_model"`
	AccountMultiplier float64 `json:"account_multiplier"`
}

type OrganizationGatewayChatCandidate struct {
	AccountID        int64                              `json:"account_id"`
	RequestedModel   string                             `json:"requested_model"`
	DispatchModel    string                             `json:"dispatch_model"`
	UpstreamModel    string                             `json:"upstream_model"`
	Route            OrganizationGatewayChatRoute       `json:"route"`
	Pricing          OrganizationGeminiCandidatePricing `json:"pricing"`
	ForwardBodyHash  string                             `json:"forward_body_hash"`
	PlanningBodyHash string                             `json:"planning_body_hash"`
	ForwardBody      []byte                             `json:"-"`
	Account          *Account                           `json:"-"`
}

type OrganizationGatewayChatCandidateSnapshot struct {
	EndpointFamily  string                             `json:"endpoint_family"`
	RequestShape    string                             `json:"request_shape"`
	Platform        string                             `json:"platform"`
	Candidates      []OrganizationGatewayChatCandidate `json:"candidates"`
	MaximumAttempts int                                `json:"maximum_attempts"`
}

type OrganizationGatewayChatCandidatePlanInput struct {
	APIKey          *APIKey
	Body            []byte
	RequestShape    string
	MaximumAttempts int
}

type OrganizationGatewayChatCandidatePlan struct {
	Policy                    OrganizationGatewayChatRequestPolicy
	Candidates                []OrganizationGatewayChatCandidate
	SourceBody                []byte
	CandidateSnapshot         []byte
	PricingSnapshot           []byte
	CandidateRouteMaximumCost string
	BillingFingerprint        string
	MaximumAttempts           int
}

func BuildOrganizationGatewayChatRequestPolicy(body []byte, requestShape string) (OrganizationGatewayChatRequestPolicy, error) {
	policy := OrganizationGatewayChatRequestPolicy{RequestShape: strings.TrimSpace(requestShape)}
	if policy.RequestShape != OrganizationRequestShapeHTTP && policy.RequestShape != OrganizationRequestShapeStreaming || !json.Valid(body) {
		return policy, ErrOrganizationGatewayChatUnsupportedRequest
	}
	modelValue := gjson.GetBytes(body, "model")
	model := strings.TrimSpace(modelValue.String())
	if modelValue.Type != gjson.String || model == "" {
		return policy, ErrOrganizationGatewayChatUnsupportedRequest
	}
	streamValue := gjson.GetBytes(body, "stream")
	if streamValue.Exists() && streamValue.Type != gjson.True && streamValue.Type != gjson.False {
		return policy, ErrOrganizationGatewayChatUnsupportedRequest
	}
	policy.Stream = streamValue.Bool()
	if (policy.RequestShape == OrganizationRequestShapeStreaming) != policy.Stream {
		return policy, ErrOrganizationGatewayChatUnsupportedRequest
	}
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || messages.Type != gjson.JSON || !messages.IsArray() || organizationChatValueUsesUnboundedContent(messages.Raw) {
		return policy, ErrOrganizationGatewayChatUnsupportedRequest
	}
	if gjson.GetBytes(body, "input").Exists() || gjson.GetBytes(body, "response_format").Exists() || gjson.GetBytes(body, "logprobs").Exists() {
		return policy, ErrOrganizationGatewayChatUnsupportedRequest
	}
	if n := gjson.GetBytes(body, "n"); n.Exists() && (n.Type != gjson.Number || n.Int() != 1 || n.Float() != 1) {
		return policy, ErrOrganizationGatewayChatUnsupportedRequest
	}
	if streamOptions := gjson.GetBytes(body, "stream_options"); streamOptions.Exists() && (streamOptions.Type != gjson.JSON || !streamOptions.IsObject()) {
		return policy, ErrOrganizationGatewayChatUnsupportedRequest
	}
	if tools := gjson.GetBytes(body, "tools"); tools.Exists() {
		if tools.Type != gjson.JSON || !tools.IsArray() {
			return policy, ErrOrganizationGatewayChatUnsupportedRequest
		}
		for _, tool := range tools.Array() {
			if strings.ToLower(strings.TrimSpace(tool.Get("type").String())) != "function" || strings.TrimSpace(tool.Get("function.name").String()) == "" {
				return policy, ErrOrganizationGatewayChatUnsupportedRequest
			}
			policy.Tools = append(policy.Tools, "function")
		}
	}
	maxTokens := gjson.GetBytes(body, "max_tokens")
	maxCompletionTokens := gjson.GetBytes(body, "max_completion_tokens")
	if maxTokens.Exists() && maxCompletionTokens.Exists() {
		return policy, ErrOrganizationGatewayChatUnsupportedRequest
	}
	outputValue := maxTokens
	if maxCompletionTokens.Exists() {
		outputValue = maxCompletionTokens
	}
	if outputValue.Exists() {
		if outputValue.Type != gjson.Number || outputValue.Int() <= 0 || outputValue.Float() != float64(outputValue.Int()) || outputValue.Int() > organizationGatewayChatDefaultOutputTokenCeiling {
			return policy, ErrOrganizationGatewayChatUnsupportedRequest
		}
		policy.OutputTokenCeiling = int(outputValue.Int())
	} else {
		policy.OutputTokenCeiling = organizationGatewayChatDefaultOutputTokenCeiling
		policy.UsedVerifiedDefaultOutputCap = true
	}
	inputTokenCeiling, ok := organizationGatewayChatInputTokenCeiling(body)
	if !ok {
		return policy, ErrOrganizationGatewayChatUnsupportedRequest
	}
	policy.InputTokenCeiling = inputTokenCeiling
	policy.RequestedModel = model
	return policy, nil
}

func ApplyOrganizationGatewayChatOutputTokenCeiling(body []byte, policy OrganizationGatewayChatRequestPolicy, dispatchModel string) ([]byte, error) {
	if len(body) == 0 || !json.Valid(body) || policy.OutputTokenCeiling <= 0 || policy.OutputTokenCeiling > organizationGatewayChatDefaultOutputTokenCeiling {
		return nil, ErrOrganizationGatewayChatUnsupportedRequest
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil || root == nil {
		return nil, ErrOrganizationGatewayChatUnsupportedRequest
	}
	model, err := json.Marshal(strings.TrimSpace(dispatchModel))
	if err != nil || strings.TrimSpace(dispatchModel) == "" {
		return nil, ErrOrganizationGatewayChatUnsupportedRequest
	}
	root["model"] = model
	if !gjson.GetBytes(body, "max_tokens").Exists() && !gjson.GetBytes(body, "max_completion_tokens").Exists() {
		ceiling, marshalErr := json.Marshal(policy.OutputTokenCeiling)
		if marshalErr != nil {
			return nil, ErrOrganizationExecutionInvariant.WithCause(marshalErr)
		}
		root["max_tokens"] = ceiling
	}
	forwardBody, err := json.Marshal(root)
	if err != nil {
		return nil, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	return forwardBody, nil
}

func (s *GatewayService) PlanOrganizationGatewayChatCandidates(ctx context.Context, input OrganizationGatewayChatCandidatePlanInput) (*OrganizationGatewayChatCandidatePlan, error) {
	if s == nil || s.billingService == nil || s.accountRepo == nil || input.APIKey == nil || input.APIKey.GroupID == nil || input.APIKey.Group == nil {
		return nil, ErrOrganizationGatewayChatNoCandidates
	}
	policy, err := BuildOrganizationGatewayChatRequestPolicy(input.Body, input.RequestShape)
	if err != nil {
		return nil, err
	}
	if s.checkChannelPricingRestriction(ctx, input.APIKey.GroupID, policy.RequestedModel) {
		return nil, ErrOrganizationGatewayChatNoCandidates
	}
	platform := strings.TrimSpace(input.APIKey.Group.Platform)
	if platform != PlatformAnthropic && platform != PlatformGemini {
		return nil, ErrOrganizationGatewayChatUnsupportedRequest
	}
	accounts, _, err := s.listSchedulableAccounts(ctx, input.APIKey.GroupID, platform, true)
	if err != nil {
		return nil, ErrOrganizationGatewayChatNoCandidates
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
	maximumAttempts := input.MaximumAttempts
	if maximumAttempts <= 0 || maximumAttempts > organizationGatewayChatDefaultMaximumAttempts {
		maximumAttempts = organizationGatewayChatDefaultMaximumAttempts
	}
	mapping, _ := s.ResolveChannelMappingAndRestrict(ctx, input.APIKey.GroupID, policy.RequestedModel)
	dispatchModel := policy.RequestedModel
	if mapping.Mapped {
		dispatchModel = strings.TrimSpace(mapping.MappedModel)
	}
	if dispatchModel == "" {
		return nil, ErrOrganizationGatewayChatNoCandidates
	}
	forwardBody, err := ApplyOrganizationGatewayChatOutputTokenCeiling(input.Body, policy, dispatchModel)
	if err != nil {
		return nil, err
	}
	inputTokenCeiling, ok := organizationGatewayChatInputTokenCeiling(forwardBody)
	if !ok {
		return nil, ErrOrganizationGatewayChatUnsupportedRequest
	}
	policy.InputTokenCeiling = inputTokenCeiling
	multiplier := organizationGatewayChatRateMultiplier(s, ctx, input.APIKey)
	candidates := make([]OrganizationGatewayChatCandidate, 0, maximumAttempts)
	maximumCost := ""
	for index := range accounts {
		account := accounts[index]
		if account.Platform != platform || !s.isAccountSchedulableForSelection(&account) ||
			!s.isAccountSchedulableForModelSelection(ctx, &account, dispatchModel) ||
			!s.isModelSupportedByAccountWithContext(ctx, &account, dispatchModel) {
			continue
		}
		upstreamModel := organizationGatewayChatUpstreamModel(&account, dispatchModel)
		if upstreamModel == "" || (s.needsUpstreamChannelRestrictionCheck(ctx, input.APIKey.GroupID) && s.channelService != nil && s.channelService.IsModelRestricted(ctx, *input.APIKey.GroupID, upstreamModel)) {
			continue
		}
		pricing, pricingErr := buildOrganizationGatewayChatPricing(ctx, s.billingService, s.resolver, policy, upstreamModel, multiplier, account.BillingRateMultiplier(), input.APIKey.GroupID)
		if pricingErr != nil {
			continue
		}
		candidate := OrganizationGatewayChatCandidate{
			AccountID: account.ID, RequestedModel: policy.RequestedModel, DispatchModel: dispatchModel, UpstreamModel: upstreamModel,
			Route:   OrganizationGatewayChatRoute{AccountID: account.ID, Platform: account.Platform, AccountType: account.Type, DispatchModel: dispatchModel, UpstreamModel: upstreamModel, AccountMultiplier: account.BillingRateMultiplier()},
			Pricing: pricing, ForwardBodyHash: organizationGatewayChatBodyHash(forwardBody), PlanningBodyHash: organizationGatewayChatBodyHash(input.Body), ForwardBody: append([]byte(nil), forwardBody...), Account: &account,
		}
		candidates = append(candidates, candidate)
		if maximumCost == "" || compareOrganizationMoney(pricing.MaximumCost, maximumCost) > 0 {
			maximumCost = pricing.MaximumCost
		}
		if len(candidates) >= maximumAttempts {
			break
		}
	}
	if len(candidates) == 0 || maximumCost == "" {
		return nil, ErrOrganizationGatewayChatNoCandidates
	}
	snapshot := OrganizationGatewayChatCandidateSnapshot{EndpointFamily: OrganizationEndpointFamilyChat, RequestShape: policy.RequestShape, Platform: platform, Candidates: candidates, MaximumAttempts: len(candidates)}
	candidateSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return nil, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	pricingSnapshot, err := json.Marshal(map[string]any{"policy": policy, "candidates": candidates})
	if err != nil {
		return nil, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	fingerprint := sha256.Sum256(append(append(append([]byte(nil), input.Body...), candidateSnapshot...), pricingSnapshot...))
	return &OrganizationGatewayChatCandidatePlan{
		Policy: policy, Candidates: candidates, SourceBody: append([]byte(nil), input.Body...), CandidateSnapshot: candidateSnapshot, PricingSnapshot: pricingSnapshot,
		CandidateRouteMaximumCost: maximumCost, BillingFingerprint: hex.EncodeToString(fingerprint[:]), MaximumAttempts: len(candidates),
	}, nil
}

func (s *GatewayService) RevalidateOrganizationGatewayChatCandidate(ctx context.Context, apiKey *APIKey, candidate OrganizationGatewayChatCandidate, policy OrganizationGatewayChatRequestPolicy, sourceBody []byte) (*OrganizationGatewayChatCandidate, error) {
	if s == nil || s.accountRepo == nil || apiKey == nil || apiKey.GroupID == nil || apiKey.Group == nil || candidate.AccountID <= 0 || organizationGatewayChatBodyHash(sourceBody) != candidate.PlanningBodyHash {
		return nil, ErrOrganizationGatewayChatCandidateConflict
	}
	platform := strings.TrimSpace(apiKey.Group.Platform)
	if platform != PlatformAnthropic && platform != PlatformGemini {
		return nil, ErrOrganizationGatewayChatCandidateConflict
	}
	account, err := s.accountRepo.GetByID(ctx, candidate.AccountID)
	if err != nil || account == nil || !s.isAccountInGroup(account, apiKey.GroupID) || account.Platform != platform || !s.isAccountSchedulableForSelection(account) {
		return nil, ErrOrganizationGatewayChatCandidateConflict
	}
	mapping, _ := s.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, candidate.RequestedModel)
	dispatchModel := candidate.RequestedModel
	if mapping.Mapped {
		dispatchModel = strings.TrimSpace(mapping.MappedModel)
	}
	if dispatchModel == "" || dispatchModel != candidate.DispatchModel || !s.isAccountSchedulableForModelSelection(ctx, account, dispatchModel) || !s.isModelSupportedByAccountWithContext(ctx, account, dispatchModel) {
		return nil, ErrOrganizationGatewayChatCandidateConflict
	}
	forwardBody, err := ApplyOrganizationGatewayChatOutputTokenCeiling(sourceBody, policy, dispatchModel)
	if err != nil || organizationGatewayChatBodyHash(forwardBody) != candidate.ForwardBodyHash {
		return nil, ErrOrganizationGatewayChatCandidateConflict
	}
	upstreamModel := organizationGatewayChatUpstreamModel(account, dispatchModel)
	if upstreamModel == "" || upstreamModel != candidate.UpstreamModel ||
		(s.needsUpstreamChannelRestrictionCheck(ctx, apiKey.GroupID) && s.channelService != nil && s.channelService.IsModelRestricted(ctx, *apiKey.GroupID, upstreamModel)) {
		return nil, ErrOrganizationGatewayChatCandidateConflict
	}
	multiplier := organizationGatewayChatRateMultiplier(s, ctx, apiKey)
	pricing, err := buildOrganizationGatewayChatPricing(ctx, s.billingService, s.resolver, policy, upstreamModel, multiplier, account.BillingRateMultiplier(), apiKey.GroupID)
	if err != nil {
		return nil, ErrOrganizationGatewayChatCandidateConflict
	}
	currentPricing, _ := json.Marshal(pricing)
	frozenPricing, _ := json.Marshal(candidate.Pricing)
	if !bytes.Equal(currentPricing, frozenPricing) || account.Type != candidate.Route.AccountType || account.Platform != candidate.Route.Platform || account.BillingRateMultiplier() != candidate.Route.AccountMultiplier {
		return nil, ErrOrganizationGatewayChatCandidateConflict
	}
	validated := candidate
	validated.Account = account
	validated.Pricing = pricing
	validated.ForwardBody = forwardBody
	return &validated, nil
}

func (s *GatewayService) CalculateOrganizationGatewayChatActualCost(candidate OrganizationGatewayChatCandidate, result *ForwardResult) (string, UsageTokens, error) {
	if s == nil || s.billingService == nil || result == nil {
		return "", UsageTokens{}, ErrOrganizationGatewayChatUnsupportedRequest
	}
	tokens := UsageTokens{
		InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens,
		CacheCreationTokens: result.Usage.CacheCreationInputTokens, CacheReadTokens: result.Usage.CacheReadInputTokens,
		CacheCreation5mTokens: result.Usage.CacheCreation5mTokens, CacheCreation1hTokens: result.Usage.CacheCreation1hTokens,
		ImageOutputTokens: result.Usage.ImageOutputTokens,
	}
	if (tokens.InputTokens == 0 && tokens.OutputTokens == 0 && tokens.CacheCreationTokens == 0 && tokens.CacheReadTokens == 0 && tokens.CacheCreation5mTokens == 0 && tokens.CacheCreation1hTokens == 0) || tokens.ImageOutputTokens > 0 || result.ImageCount > 0 {
		return "", tokens, ErrOrganizationExecutionPendingReconciliation
	}
	cost, err := candidate.Pricing.CalculateActualCost(s.billingService, tokens)
	return cost, tokens, err
}

func (p OrganizationGatewayChatCandidatePlan) NewExecutionReservationInput(apiKey *APIKey, clientRequestID string) (OrganizationExecutionReservationInput, error) {
	attribution, err := organizationGeminiExecutionAttribution(apiKey)
	if err != nil {
		return OrganizationExecutionReservationInput{}, err
	}
	return OrganizationExecutionReservationInput{
		Attribution: attribution, EndpointFamily: OrganizationEndpointFamilyChat, RequestShape: p.Policy.RequestShape, Model: p.Policy.RequestedModel, ClientRequestID: clientRequestID,
		CandidateSnapshot: p.CandidateSnapshot, PricingSnapshot: p.PricingSnapshot, CandidateRouteMaximumCost: p.CandidateRouteMaximumCost, BillingFingerprint: p.BillingFingerprint,
	}, nil
}

func organizationGatewayChatUpstreamModel(account *Account, dispatchModel string) string {
	if account == nil {
		return ""
	}
	model := strings.TrimSpace(dispatchModel)
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		model = strings.TrimSpace(account.GetMappedModel(model))
	}
	if model == dispatchModel && account.Platform == PlatformAnthropic && account.Type == AccountTypeServiceAccount {
		model = normalizeVertexAnthropicModelID(claude.NormalizeModelID(model))
	} else if model == dispatchModel && account.Platform == PlatformAnthropic && account.Type != AccountTypeAPIKey {
		model = claude.NormalizeModelID(model)
	}
	return strings.TrimSpace(model)
}

func organizationGatewayChatRateMultiplier(s *GatewayService, ctx context.Context, apiKey *APIKey) float64 {
	if apiKey == nil || apiKey.User == nil || apiKey.Group == nil {
		return 1
	}
	if s != nil && s.userGroupRateResolver != nil {
		return s.userGroupRateResolver.Resolve(ctx, apiKey.User.ID, apiKey.Group.ID, apiKey.Group.RateMultiplier)
	}
	return apiKey.Group.RateMultiplier
}

func organizationGatewayChatPricingPolicy(policy OrganizationGatewayChatRequestPolicy) OrganizationGeminiRequestPolicy {
	return OrganizationGeminiRequestPolicy{
		EndpointFamily: OrganizationEndpointFamilyChat, RequestShape: policy.RequestShape, Action: "chat_completions", RequestedModel: policy.RequestedModel,
		Stream: policy.Stream, InputTokenCeiling: policy.InputTokenCeiling, OutputTokenCeiling: policy.OutputTokenCeiling,
	}
}

func buildOrganizationGatewayChatPricing(ctx context.Context, billing *BillingService, resolver *ModelPricingResolver, policy OrganizationGatewayChatRequestPolicy, model string, multiplier, accountMultiplier float64, groupID *int64) (OrganizationGeminiCandidatePricing, error) {
	pricingPolicy := organizationGatewayChatPricingPolicy(policy)
	if ctx == nil {
		ctx = context.Background()
	}
	if billing == nil || strings.TrimSpace(model) == "" || multiplier < 0 || accountMultiplier < 0 || pricingPolicy.InputTokenCeiling <= 0 || pricingPolicy.OutputTokenCeiling <= 0 {
		return OrganizationGeminiCandidatePricing{}, ErrOrganizationGatewayChatUnsupportedRequest
	}
	var resolved *ResolvedPricing
	if resolver != nil {
		resolved = resolver.Resolve(ctx, PricingInput{Model: model, GroupID: groupID})
	}
	if resolved != nil && (resolved.Mode != BillingModeToken || resolved.BasePricing == nil) {
		return OrganizationGeminiCandidatePricing{}, ErrOrganizationGatewayChatUnsupportedRequest
	}
	if resolved == nil {
		pricing, err := billing.GetModelPricing(model)
		if err != nil || pricing == nil {
			return OrganizationGeminiCandidatePricing{}, ErrOrganizationGatewayChatUnsupportedRequest
		}
		resolved = &ResolvedPricing{Mode: BillingModeToken, BasePricing: pricing}
	}
	breakdown, err := billing.CalculateCostUnified(CostInput{
		Ctx: ctx, Model: model,
		Tokens: UsageTokens{
			InputTokens: pricingPolicy.InputTokenCeiling, OutputTokens: pricingPolicy.OutputTokenCeiling,
			CacheCreationTokens: pricingPolicy.InputTokenCeiling, CacheReadTokens: pricingPolicy.InputTokenCeiling,
			CacheCreation5mTokens: pricingPolicy.InputTokenCeiling, CacheCreation1hTokens: pricingPolicy.InputTokenCeiling,
		},
		RateMultiplier: multiplier * accountMultiplier, Resolver: &ModelPricingResolver{}, Resolved: resolved,
	})
	if err != nil || breakdown == nil || breakdown.ActualCost < 0 {
		return OrganizationGeminiCandidatePricing{}, ErrOrganizationGatewayChatUnsupportedRequest
	}
	maximumCost, err := NormalizeOrganizationMoney(decimal.NewFromFloat(breakdown.ActualCost).RoundCeil(8).StringFixed(8), true)
	if err != nil {
		return OrganizationGeminiCandidatePricing{}, err
	}
	return OrganizationGeminiCandidatePricing{
		Model: model, RateMultiplier: multiplier, AccountMultiplier: accountMultiplier,
		InputTokenCeiling: pricingPolicy.InputTokenCeiling, OutputTokenCeiling: pricingPolicy.OutputTokenCeiling,
		MaximumCost: maximumCost, Pricing: resolved.BasePricing, Resolved: resolved,
	}, nil
}

func organizationGatewayChatBodyHash(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func organizationGatewayChatInputTokenCeiling(body []byte) (int, bool) {
	ceiling := (len(body) + organizationGatewayChatInputProtocolOverhead) * organizationGatewayChatInputBodyMultiplier
	return ceiling, ceiling > 0 && ceiling <= organizationGatewayChatMaximumInputTokenCeiling
}
