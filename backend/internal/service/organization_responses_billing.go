package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

var (
	ErrOrganizationResponsesUnsupportedRequest = infraerrors.BadRequest(
		"ORGANIZATION_RESPONSES_UNSUPPORTED_REQUEST",
		"the organization Responses request is not supported by the tested billing adapter",
	)
	ErrOrganizationResponsesNoCandidates = infraerrors.ServiceUnavailable(
		"ORGANIZATION_RESPONSES_NO_CANDIDATES",
		"no frozen OpenAI Responses candidate is available",
	)
	ErrOrganizationResponsesCandidateConflict = infraerrors.Conflict(
		"ORGANIZATION_RESPONSES_CANDIDATE_CONFLICT",
		"a frozen organization Responses candidate is no longer dispatchable",
	)
	ErrOrganizationResponsesWSFailoverUnsupported = infraerrors.ServiceUnavailable(
		"ORGANIZATION_RESPONSES_WS_FAILOVER_UNSUPPORTED",
		"organization Responses WebSocket cannot safely retry simultaneous or unreserved candidates",
	)
)

const (
	organizationResponsesMaximumInputBodyMultiplier = 2
	organizationResponsesInputProtocolOverhead      = 1024
	organizationResponsesDefaultMaximumAttempts     = 4
	OrganizationResponsesRouteChatFallback          = "responses_to_chat"
)

type OrganizationResponsesRequestPolicy struct {
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

type OrganizationResponsesCandidatePricing struct {
	Model              string           `json:"model"`
	ServiceTier        string           `json:"service_tier"`
	RateMultiplier     float64          `json:"rate_multiplier"`
	AccountMultiplier  float64          `json:"account_multiplier"`
	InputTokenCeiling  int              `json:"input_token_ceiling"`
	OutputTokenCeiling int              `json:"output_token_ceiling"`
	MaximumCost        string           `json:"maximum_cost"`
	Pricing            *ModelPricing    `json:"pricing"`
	Resolved           *ResolvedPricing `json:"resolved,omitempty"`
}

type OrganizationResponsesCandidate struct {
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

type OrganizationResponsesCandidateSnapshot struct {
	EndpointFamily  string                           `json:"endpoint_family"`
	RequestShape    string                           `json:"request_shape"`
	Candidates      []OrganizationResponsesCandidate `json:"candidates"`
	MaximumAttempts int                              `json:"maximum_attempts"`
}

type OrganizationResponsesCandidatePlanInput struct {
	APIKey             *APIKey
	Body               []byte
	RequestShape       string
	RequestPath        string
	GroupID            *int64
	MaximumAttempts    int
	PreferredAccountID int64
}

type OrganizationResponsesCandidatePlan struct {
	Policy                    OrganizationResponsesRequestPolicy
	Candidates                []OrganizationResponsesCandidate
	CandidateSnapshot         []byte
	PricingSnapshot           []byte
	CandidateRouteMaximumCost string
	BillingFingerprint        string
	MaximumAttempts           int
}

func BuildOrganizationResponsesRequestPolicy(body []byte, requestShape, requestPath string) (OrganizationResponsesRequestPolicy, error) {
	policy := OrganizationResponsesRequestPolicy{RequestShape: strings.TrimSpace(requestShape)}
	if policy.RequestShape != OrganizationRequestShapeHTTP &&
		policy.RequestShape != OrganizationRequestShapeStreaming &&
		policy.RequestShape != OrganizationRequestShapeWebSocketResponseCreate {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	path := strings.TrimRight(strings.TrimSpace(requestPath), "/")
	if !isOrganizationResponsesHTTPPath(path) || !gjson.ValidBytes(body) {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	modelValue := gjson.GetBytes(body, "model")
	model := strings.TrimSpace(modelValue.String())
	if modelValue.Type != gjson.String || model == "" {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	policy.RequestedModel = model
	streamValue := gjson.GetBytes(body, "stream")
	if streamValue.Exists() && streamValue.Type != gjson.True && streamValue.Type != gjson.False {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	policy.Stream = streamValue.Bool()
	if policy.RequestShape != OrganizationRequestShapeWebSocketResponseCreate &&
		(policy.RequestShape == OrganizationRequestShapeStreaming) != policy.Stream {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	backgroundValue := gjson.GetBytes(body, "background")
	if backgroundValue.Exists() && backgroundValue.Type != gjson.True && backgroundValue.Type != gjson.False {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	if (policy.RequestShape != OrganizationRequestShapeWebSocketResponseCreate && gjson.GetBytes(body, "previous_response_id").Exists()) ||
		backgroundValue.Bool() || gjson.GetBytes(body, "max_completion_tokens").Exists() {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	for _, key := range []string{"conversation", "prompt"} {
		if _, exists := envelope[key]; exists {
			return policy, ErrOrganizationResponsesUnsupportedRequest
		}
	}
	if input, exists := envelope["input"]; exists && organizationResponsesInputUsesUnboundedContent(input) {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}

	maxInput, maxOutput, ok := verifiedOrganizationResponsesModelLimits(model)
	if !ok {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	if (policy.Stream || policy.RequestShape == OrganizationRequestShapeWebSocketResponseCreate) && !verifiedOrganizationResponsesModelSupportsStreaming(model) {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	policy.MaxInputTokens = maxInput
	policy.MaxOutputTokens = maxOutput
	policy.InputTokenCeiling = organizationResponsesInputTokenCeiling(body)
	if policy.InputTokenCeiling > maxInput {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	outputValue := gjson.GetBytes(body, "max_output_tokens")
	if outputValue.Exists() {
		if outputValue.Type != gjson.Number || outputValue.Float() != float64(outputValue.Int()) || outputValue.Int() <= 0 || outputValue.Int() > int64(maxOutput) {
			return policy, ErrOrganizationResponsesUnsupportedRequest
		}
		policy.OutputTokenCeiling = int(outputValue.Int())
	} else {
		policy.OutputTokenCeiling = maxOutput
		policy.UsedVerifiedDefaultOutputCap = true
	}
	serviceTierValue := gjson.GetBytes(body, "service_tier")
	if serviceTierValue.Exists() && serviceTierValue.Type != gjson.String {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	policy.ServiceTier = normalizeOrganizationResponsesServiceTier(serviceTierValue.String())
	if policy.ServiceTier == "unsupported" {
		return policy, ErrOrganizationResponsesUnsupportedRequest
	}
	if tools := gjson.GetBytes(body, "tools"); tools.Exists() {
		if tools.Type != gjson.JSON || !tools.IsArray() {
			return policy, ErrOrganizationResponsesUnsupportedRequest
		}
		for _, tool := range tools.Array() {
			toolType := strings.ToLower(strings.TrimSpace(tool.Get("type").String()))
			if toolType != "function" && toolType != "custom" {
				return policy, ErrOrganizationResponsesUnsupportedRequest
			}
			policy.Tools = append(policy.Tools, toolType)
		}
	}
	return policy, nil
}

func isOrganizationResponsesHTTPPath(path string) bool {
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	return path == "/v1/responses" || path == "/responses" || path == "/backend-api/codex/responses"
}

func organizationResponsesInputTokenCeiling(body []byte) int {
	ceiling := len(body)*organizationResponsesMaximumInputBodyMultiplier + organizationResponsesInputProtocolOverhead
	if ceiling < 1 {
		return 1
	}
	return ceiling
}

func organizationResponsesInputUsesUnboundedContent(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	return organizationResponsesInputValueUsesUnboundedContent(value)
}

func organizationResponsesInputValueUsesUnboundedContent(value any) bool {
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			if organizationResponsesInputValueUsesUnboundedContent(item) {
				return true
			}
		}
	case map[string]any:
		typeValue := strings.ToLower(strings.TrimSpace(fmt.Sprint(current["type"])))
		switch typeValue {
		case "input_image", "input_file", "input_audio", "computer_call", "computer_call_output", "item_reference":
			return true
		}
		for _, item := range current {
			if organizationResponsesInputValueUsesUnboundedContent(item) {
				return true
			}
		}
	}
	return false
}

func normalizeOrganizationResponsesServiceTier(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "flex", "priority":
		return strings.ToLower(strings.TrimSpace(value))
	case "fast":
		return "priority"
	default:
		return "unsupported"
	}
}

func verifiedOrganizationResponsesModelLimits(model string) (int, int, bool) {
	model, ok := verifiedOrganizationResponsesModelBase(model)
	if !ok {
		return 0, 0, false
	}
	switch model {
	case "gpt-5.5-pro", "gpt-5.4-pro":
		return 922000, 128000, true
	case "gpt-5.4-mini", "gpt-5.4-nano":
		return 272000, 128000, true
	case "gpt-5.5", "gpt-5.4":
		return 922000, 128000, true
	case "gpt-5.3-codex-spark", "gpt-5.3-codex", "gpt-5.2-codex",
		"gpt-5.1-codex-max", "gpt-5.1-codex-mini", "gpt-5.1-codex", "gpt-5-codex":
		return 272000, 128000, true
	default:
		return 0, 0, false
	}
}

func verifiedOrganizationResponsesModelBase(model string) (string, bool) {
	model = strings.ToLower(strings.TrimSpace(lastOpenAIModelSegment(model)))
	model = strings.TrimSuffix(model, "-openai-compact")
	verifiedSnapshots := map[string]string{
		"gpt-5.5-pro-2026-04-23":  "gpt-5.5-pro",
		"gpt-5.5-2026-04-23":      "gpt-5.5",
		"gpt-5.4-pro-2026-03-05":  "gpt-5.4-pro",
		"gpt-5.4-2026-03-05":      "gpt-5.4",
		"gpt-5.4-mini-2026-03-17": "gpt-5.4-mini",
		"gpt-5.4-nano-2026-03-17": "gpt-5.4-nano",
	}
	if base, ok := verifiedSnapshots[model]; ok {
		return base, true
	}
	bases := []string{
		"gpt-5.3-codex-spark", "gpt-5.1-codex-max", "gpt-5.1-codex-mini",
		"gpt-5.5-pro", "gpt-5.4-pro", "gpt-5.4-mini", "gpt-5.4-nano",
		"gpt-5.3-codex", "gpt-5.2-codex", "gpt-5.1-codex", "gpt-5-codex",
		"gpt-5.5", "gpt-5.4",
	}
	for _, base := range bases {
		if model == base {
			return base, true
		}
		if !strings.HasPrefix(model, base+"-") {
			continue
		}
		suffix := strings.TrimPrefix(model, base+"-")
		switch suffix {
		case "none", "minimal", "low", "medium", "high", "xhigh":
			return base, true
		}
	}
	return "", false
}

func verifiedOrganizationResponsesModelSupportsStreaming(model string) bool {
	model, ok := verifiedOrganizationResponsesModelBase(model)
	return ok && model != "gpt-5.5-pro"
}

func organizationResponsesCandidateInputTokenCeiling(body []byte, model string) int {
	ceiling := organizationResponsesInputTokenCeiling(body)
	instructions := gjson.GetBytes(body, "instructions")
	if !instructions.Exists() || instructions.Type != gjson.String || strings.TrimSpace(instructions.String()) == "" {
		// Forward injects a server-owned instructions prompt when the client omits
		// one. Reserve for that prompt and small metadata additions before dispatch
		// so the upstream body cannot outrun the frozen cap on any account type.
		ceiling += (len(defaultCodexSynthInstructions(model)) + 4096) * organizationResponsesMaximumInputBodyMultiplier
	}
	return ceiling
}

func BuildOrganizationResponsesCandidatePricing(billing *BillingService, resolver *ModelPricingResolver, policy OrganizationResponsesRequestPolicy, model string, multiplier float64, accountMultipliers ...float64) (OrganizationResponsesCandidatePricing, error) {
	return buildOrganizationResponsesCandidatePricing(context.Background(), billing, resolver, policy, model, multiplier, accountMultipliers, nil)
}

func buildOrganizationResponsesCandidatePricing(ctx context.Context, billing *BillingService, resolver *ModelPricingResolver, policy OrganizationResponsesRequestPolicy, model string, multiplier float64, accountMultipliers []float64, groupID *int64) (OrganizationResponsesCandidatePricing, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if billing == nil || multiplier < 0 || policy.InputTokenCeiling <= 0 || policy.OutputTokenCeiling <= 0 {
		return OrganizationResponsesCandidatePricing{}, ErrOrganizationResponsesUnsupportedRequest
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return OrganizationResponsesCandidatePricing{}, ErrOrganizationResponsesUnsupportedRequest
	}
	var resolved *ResolvedPricing
	if resolver != nil {
		resolved = resolver.Resolve(ctx, PricingInput{Model: model, GroupID: groupID})
	}
	if resolved != nil && (resolved.Mode != BillingModeToken || resolved.BasePricing == nil) {
		return OrganizationResponsesCandidatePricing{}, ErrOrganizationResponsesUnsupportedRequest
	}
	if resolved == nil {
		pricing, err := billing.GetModelPricing(model)
		if err != nil || pricing == nil {
			return OrganizationResponsesCandidatePricing{}, ErrOrganizationResponsesUnsupportedRequest
		}
		resolved = &ResolvedPricing{Mode: BillingModeToken, BasePricing: pricing}
	}
	pricing := resolved.BasePricing
	if pricing == nil {
		return OrganizationResponsesCandidatePricing{}, ErrOrganizationResponsesUnsupportedRequest
	}
	accountMultiplier := 1.0
	if len(accountMultipliers) > 0 {
		accountMultiplier = accountMultipliers[0]
	}
	if accountMultiplier < 0 {
		return OrganizationResponsesCandidatePricing{}, ErrOrganizationResponsesUnsupportedRequest
	}
	// The candidate snapshot stores the member/group multiplier and the account
	// multiplier separately, while the frozen ceiling uses their product.
	tokens := UsageTokens{
		InputTokens:         policy.InputTokenCeiling,
		OutputTokens:        policy.OutputTokenCeiling,
		CacheCreationTokens: policy.InputTokenCeiling,
		CacheReadTokens:     policy.InputTokenCeiling,
	}
	resolverForSnapshot := &ModelPricingResolver{}
	breakdown, err := billing.CalculateCostUnified(CostInput{
		Ctx:            ctx,
		Model:          model,
		Tokens:         tokens,
		RateMultiplier: multiplier * accountMultiplier,
		ServiceTier:    policy.ServiceTier,
		Resolver:       resolverForSnapshot,
		Resolved:       resolved,
	})
	if err != nil || breakdown == nil || breakdown.ActualCost < 0 {
		return OrganizationResponsesCandidatePricing{}, ErrOrganizationResponsesUnsupportedRequest
	}
	maximum, err := NormalizeOrganizationMoney(decimal.NewFromFloat(breakdown.ActualCost).RoundCeil(8).StringFixed(8), true)
	if err != nil {
		return OrganizationResponsesCandidatePricing{}, err
	}
	return OrganizationResponsesCandidatePricing{
		Model:              model,
		ServiceTier:        policy.ServiceTier,
		RateMultiplier:     multiplier,
		AccountMultiplier:  accountMultiplier,
		InputTokenCeiling:  policy.InputTokenCeiling,
		OutputTokenCeiling: policy.OutputTokenCeiling,
		MaximumCost:        maximum,
		Pricing:            pricing,
		Resolved:           resolved,
	}, nil
}

func (p OrganizationResponsesCandidatePricing) CalculateActualCost(billing *BillingService, tokens UsageTokens) (string, error) {
	if billing == nil || p.Pricing == nil || tokens.InputTokens < 0 || tokens.OutputTokens < 0 {
		return "", ErrOrganizationResponsesUnsupportedRequest
	}
	resolver := &ModelPricingResolver{}
	resolved := p.Resolved
	if resolved == nil {
		resolved = &ResolvedPricing{Mode: BillingModeToken, BasePricing: p.Pricing}
	}
	breakdown, err := billing.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          p.Model,
		Tokens:         tokens,
		RateMultiplier: p.RateMultiplier * p.AccountMultiplier,
		ServiceTier:    p.ServiceTier,
		Resolver:       resolver,
		Resolved:       resolved,
	})
	if err != nil || breakdown == nil {
		return "", err
	}
	return NormalizeOrganizationRawActualCost(fmt.Sprintf("%.18f", breakdown.ActualCost))
}

func (s *OpenAIGatewayService) PlanOrganizationResponsesCandidates(ctx context.Context, input OrganizationResponsesCandidatePlanInput) (*OrganizationResponsesCandidatePlan, error) {
	if s == nil || s.accountRepo == nil || input.APIKey == nil || input.APIKey.GroupID == nil {
		return nil, ErrOrganizationResponsesNoCandidates
	}
	policy, err := BuildOrganizationResponsesRequestPolicy(input.Body, input.RequestShape, input.RequestPath)
	if err != nil {
		return nil, err
	}
	if s.checkChannelPricingRestriction(ctx, input.APIKey.GroupID, policy.RequestedModel) {
		return nil, ErrOrganizationResponsesNoCandidates
	}
	needsUpstreamRestrictionCheck := s.needsUpstreamChannelRestrictionCheck(ctx, input.APIKey.GroupID)
	accounts, err := s.listSchedulableAccounts(ctx, input.APIKey.GroupID)
	if err != nil {
		return nil, ErrOrganizationResponsesNoCandidates
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
	if input.PreferredAccountID > 0 {
		for index := range accounts {
			if accounts[index].ID != input.PreferredAccountID {
				continue
			}
			preferred := accounts[index]
			copy(accounts[1:index+1], accounts[:index])
			accounts[0] = preferred
			break
		}
	}
	maxAttempts := input.MaximumAttempts
	if maxAttempts <= 0 || maxAttempts > organizationResponsesMaximumAttempts(s.cfg) {
		maxAttempts = organizationResponsesMaximumAttempts(s.cfg)
	}

	var candidates []OrganizationResponsesCandidate
	var maximumCost string
	for _, accountValue := range accounts {
		account := accountValue
		if fresh := s.resolveFreshSchedulableOpenAIAccount(ctx, &account, policy.RequestedModel, false, OpenAIEndpointCapabilityChatCompletions); fresh != nil {
			account = *fresh
		} else {
			continue
		}
		if account.IsOpenAIPassthroughEnabled() {
			continue
		}
		if account.Type == AccountTypeSetupToken {
			continue
		}
		upstreamModel := resolveOpenAIAccountUpstreamModelForRequest(&account, policy.RequestedModel, false)
		if account.Type == AccountTypeOAuth || account.Type == AccountTypeSetupToken {
			upstreamModel = normalizeCodexModel(upstreamModel)
		}
		if strings.TrimSpace(upstreamModel) == "" {
			continue
		}
		if needsUpstreamRestrictionCheck && s.channelService != nil && s.channelService.IsModelRestricted(ctx, *input.APIKey.GroupID, upstreamModel) {
			continue
		}
		candidateMaxInput, candidateMaxOutput, ok := verifiedOrganizationResponsesModelLimits(upstreamModel)
		if !ok || ((policy.Stream || policy.RequestShape == OrganizationRequestShapeWebSocketResponseCreate) && !verifiedOrganizationResponsesModelSupportsStreaming(upstreamModel)) {
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
		multiplier := 1.0
		if input.APIKey.User != nil && input.APIKey.Group != nil {
			resolverRate := s.userGroupRateResolver
			if resolverRate != nil {
				multiplier = resolverRate.Resolve(ctx, input.APIKey.User.ID, input.APIKey.Group.ID, input.APIKey.Group.RateMultiplier)
			} else {
				multiplier = input.APIKey.Group.RateMultiplier
			}
		}
		pricing, err := buildOrganizationResponsesCandidatePricing(ctx, s.billingService, s.resolver, effectivePolicy, upstreamModel, multiplier, []float64{account.BillingRateMultiplier()}, input.APIKey.GroupID)
		if err != nil {
			continue
		}
		candidate := OrganizationResponsesCandidate{
			AccountID: account.ID, AccountType: account.Type, RequestedModel: policy.RequestedModel,
			UpstreamModel: upstreamModel, RequestShape: policy.RequestShape, ServiceTier: effectivePolicy.ServiceTier, Route: organizationResponsesAccountRoute(&account),
			ForwardBodyHash: organizationResponsesBodyHash(effectiveBody), Pricing: pricing, Account: &account,
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
	if len(candidates) == 0 || maximumCost == "" {
		return nil, ErrOrganizationResponsesNoCandidates
	}
	snapshot := OrganizationResponsesCandidateSnapshot{EndpointFamily: OrganizationEndpointFamilyResponses, RequestShape: policy.RequestShape, Candidates: candidates, MaximumAttempts: len(candidates)}
	candidateJSON, _ := json.Marshal(snapshot)
	pricingJSON, _ := json.Marshal(map[string]any{"policy": policy, "candidates": candidates})
	billingFingerprint := sha256.Sum256(append(append([]byte{}, input.Body...), candidateJSON...))
	return &OrganizationResponsesCandidatePlan{
		Policy: policy, Candidates: candidates, CandidateSnapshot: candidateJSON, PricingSnapshot: pricingJSON,
		CandidateRouteMaximumCost: maximumCost, BillingFingerprint: hex.EncodeToString(billingFingerprint[:]), MaximumAttempts: len(candidates),
	}, nil
}

func (s *OpenAIGatewayService) RevalidateOrganizationResponsesCandidate(ctx context.Context, apiKey *APIKey, candidate OrganizationResponsesCandidate) (*OrganizationResponsesCandidate, error) {
	if s == nil || apiKey == nil || apiKey.GroupID == nil || candidate.AccountID <= 0 {
		return nil, ErrOrganizationResponsesCandidateConflict
	}
	account := candidate.Account
	if s.accountRepo != nil {
		latest, err := s.accountRepo.GetByID(ctx, candidate.AccountID)
		if err != nil || latest == nil {
			return nil, ErrOrganizationResponsesCandidateConflict
		}
		account = latest
	}
	account = s.resolveFreshSchedulableOpenAIAccount(ctx, account, candidate.RequestedModel, false, OpenAIEndpointCapabilityChatCompletions)
	if account == nil || account.IsOpenAIPassthroughEnabled() {
		return nil, ErrOrganizationResponsesCandidateConflict
	}
	if !openAIStickyAccountMatchesGroup(account, apiKey.GroupID) {
		return nil, ErrOrganizationResponsesCandidateConflict
	}
	upstreamModel := resolveOpenAIAccountUpstreamModelForRequest(account, candidate.RequestedModel, false)
	if account != nil && (account.Type == AccountTypeOAuth || account.Type == AccountTypeSetupToken) {
		upstreamModel = normalizeCodexModel(upstreamModel)
	}
	if account.Type == AccountTypeSetupToken || strings.TrimSpace(upstreamModel) == "" {
		return nil, ErrOrganizationResponsesCandidateConflict
	}
	if s.needsUpstreamChannelRestrictionCheck(ctx, apiKey.GroupID) && s.channelService != nil &&
		s.channelService.IsModelRestricted(ctx, *apiKey.GroupID, upstreamModel) {
		return nil, ErrOrganizationResponsesCandidateConflict
	}
	if account.ID != candidate.AccountID ||
		account.IsOpenAIPassthroughEnabled() ||
		organizationResponsesAccountRoute(account) != candidate.Route ||
		upstreamModel != candidate.UpstreamModel ||
		account.BillingRateMultiplier() != candidate.Pricing.AccountMultiplier {
		return nil, ErrOrganizationResponsesCandidateConflict
	}
	multiplier := 1.0
	if apiKey.User != nil && apiKey.Group != nil {
		if s.userGroupRateResolver != nil {
			multiplier = s.userGroupRateResolver.Resolve(ctx, apiKey.User.ID, apiKey.Group.ID, apiKey.Group.RateMultiplier)
		} else {
			multiplier = apiKey.Group.RateMultiplier
		}
	}
	if multiplier != candidate.Pricing.RateMultiplier {
		return nil, ErrOrganizationResponsesCandidateConflict
	}
	candidateMaxInput, candidateMaxOutput, ok := verifiedOrganizationResponsesModelLimits(upstreamModel)
	if !ok || ((candidate.RequestShape == OrganizationRequestShapeStreaming || candidate.RequestShape == OrganizationRequestShapeWebSocketResponseCreate) && !verifiedOrganizationResponsesModelSupportsStreaming(upstreamModel)) {
		return nil, ErrOrganizationResponsesCandidateConflict
	}
	effectiveBody, err := s.applyOpenAIFastPolicyToBody(ctx, account, candidate.UpstreamModel, candidate.PlanningBody)
	if err != nil || organizationResponsesBodyHash(effectiveBody) != candidate.ForwardBodyHash {
		return nil, ErrOrganizationResponsesCandidateConflict
	}
	policy := OrganizationResponsesRequestPolicy{
		RequestedModel: candidate.RequestedModel, RequestShape: candidate.RequestShape,
		InputTokenCeiling: organizationResponsesCandidateInputTokenCeiling(candidate.PlanningBody, candidate.RequestedModel), OutputTokenCeiling: candidate.Pricing.OutputTokenCeiling,
		MaxInputTokens: candidateMaxInput, MaxOutputTokens: candidateMaxOutput,
		ServiceTier: normalizeOrganizationResponsesServiceTier(gjson.GetBytes(effectiveBody, "service_tier").String()),
	}
	if policy.InputTokenCeiling > candidateMaxInput {
		return nil, ErrOrganizationResponsesCandidateConflict
	}
	if account.Type == AccountTypeOAuth || policy.OutputTokenCeiling > candidateMaxOutput {
		policy.OutputTokenCeiling = candidateMaxOutput
	}
	currentPricing, err := buildOrganizationResponsesCandidatePricing(ctx, s.billingService, s.resolver, policy, candidate.UpstreamModel, multiplier, []float64{account.BillingRateMultiplier()}, apiKey.GroupID)
	currentPricingJSON, _ := json.Marshal(currentPricing)
	frozenPricingJSON, _ := json.Marshal(candidate.Pricing)
	if err != nil || !bytes.Equal(currentPricingJSON, frozenPricingJSON) {
		return nil, ErrOrganizationResponsesCandidateConflict
	}
	validated := candidate
	validated.Account = account
	validated.Pricing = currentPricing
	validated.ForwardBody = append([]byte(nil), effectiveBody...)
	return &validated, nil
}

type organizationResponsesDispatchContextKey struct{}

func WithOrganizationResponsesDispatchContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, organizationResponsesDispatchContextKey{}, true)
}

func isOrganizationResponsesDispatchContext(ctx context.Context) bool {
	enabled, _ := ctx.Value(organizationResponsesDispatchContextKey{}).(bool)
	return enabled
}

func organizationResponsesBodyHash(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func (s *OpenAIGatewayService) CalculateOrganizationResponsesActualCost(candidate OrganizationResponsesCandidate, result *OpenAIForwardResult) (string, UsageTokens, error) {
	if s == nil || s.billingService == nil || result == nil || !result.UsageObserved {
		return "", UsageTokens{}, ErrOrganizationResponsesUnsupportedRequest
	}
	actualInput := result.Usage.InputTokens - result.Usage.CacheReadInputTokens
	if actualInput < 0 {
		actualInput = 0
	}
	tokens := UsageTokens{
		InputTokens:         actualInput,
		OutputTokens:        result.Usage.OutputTokens,
		CacheCreationTokens: result.Usage.CacheCreationInputTokens,
		CacheReadTokens:     result.Usage.CacheReadInputTokens,
		ImageOutputTokens:   result.Usage.ImageOutputTokens,
	}
	if tokens.ImageOutputTokens > 0 || result.ImageCount > 0 {
		return "", UsageTokens{}, ErrOrganizationResponsesUnsupportedRequest
	}
	cost, err := candidate.Pricing.CalculateActualCost(s.billingService, tokens)
	return cost, tokens, err
}

func organizationResponsesMaximumAttempts(cfg *config.Config) int {
	if cfg == nil {
		return organizationResponsesDefaultMaximumAttempts
	}
	if value := cfg.Gateway.MaxAccountSwitches; value > 0 {
		return value + 1
	}
	return organizationResponsesDefaultMaximumAttempts
}

func organizationResponsesAccountRoute(account *Account) string {
	if account == nil {
		return ""
	}
	if account.Type == AccountTypeOAuth || account.Type == AccountTypeSetupToken {
		return "codex_http_responses"
	}
	if account.Type == AccountTypeAPIKey && !openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return OrganizationResponsesRouteChatFallback
	}
	return "openai_http_responses"
}

func compareOrganizationMoney(left, right string) int {
	leftDecimal, leftErr := decimal.NewFromString(left)
	rightDecimal, rightErr := decimal.NewFromString(right)
	if leftErr != nil || rightErr != nil {
		return 0
	}
	return leftDecimal.Cmp(rightDecimal)
}

func (p OrganizationResponsesCandidatePlan) NewExecutionReservationInput(apiKey *APIKey, clientRequestID string) (OrganizationExecutionReservationInput, error) {
	attribution, err := organizationResponsesExecutionAttribution(apiKey)
	if err != nil {
		return OrganizationExecutionReservationInput{}, err
	}
	input := OrganizationExecutionReservationInput{
		Attribution:    attribution,
		EndpointFamily: OrganizationEndpointFamilyResponses, RequestShape: p.Policy.RequestShape, ClientRequestID: clientRequestID,
		Model:             p.Policy.RequestedModel,
		CandidateSnapshot: p.CandidateSnapshot, PricingSnapshot: p.PricingSnapshot, CandidateRouteMaximumCost: p.CandidateRouteMaximumCost, BillingFingerprint: p.BillingFingerprint,
	}
	return input, nil
}

func NewOrganizationResponsesPreDispatchRejectionInput(apiKey *APIKey, body []byte, requestShape, requestPath, clientRequestID string, rejection error) (OrganizationExecutionReservationInput, error) {
	attribution, err := organizationResponsesExecutionAttribution(apiKey)
	if err != nil {
		return OrganizationExecutionReservationInput{}, err
	}
	reason := strings.ToLower(strings.TrimSpace(infraerrors.Reason(rejection)))
	if reason == "" {
		reason = "organization_responses_pre_dispatch_rejected"
	}
	proof := "request_policy_rejected:" + reason
	candidateSnapshot, err := json.Marshal(map[string]any{
		"endpoint_family":  OrganizationEndpointFamilyResponses,
		"request_shape":    requestShape,
		"request_path":     strings.TrimRight(strings.TrimSpace(requestPath), "/"),
		"requested_model":  strings.TrimSpace(gjson.GetBytes(body, "model").String()),
		"candidates":       []any{},
		"maximum_attempts": 0,
		"zero_proof":       proof,
	})
	if err != nil {
		return OrganizationExecutionReservationInput{}, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	pricingSnapshot, err := json.Marshal(map[string]any{
		"rejected":     true,
		"reason":       reason,
		"maximum_cost": "0.00000000",
	})
	if err != nil {
		return OrganizationExecutionReservationInput{}, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	fingerprintSource := append(append(append([]byte(nil), body...), candidateSnapshot...), pricingSnapshot...)
	fingerprint := sha256.Sum256(fingerprintSource)
	return OrganizationExecutionReservationInput{
		Attribution:               attribution,
		EndpointFamily:            OrganizationEndpointFamilyResponses,
		RequestShape:              requestShape,
		Model:                     strings.TrimSpace(gjson.GetBytes(body, "model").String()),
		ClientRequestID:           clientRequestID,
		CandidateSnapshot:         candidateSnapshot,
		PricingSnapshot:           pricingSnapshot,
		CandidateRouteMaximumCost: "0.00000000",
		BillingFingerprint:        hex.EncodeToString(fingerprint[:]),
		PreDispatchZeroProof:      proof,
	}, nil
}

func organizationResponsesExecutionAttribution(apiKey *APIKey) (OrganizationExecutionTrustedAttribution, error) {
	if apiKey == nil || apiKey.OrganizationAuthorization == nil || apiKey.User == nil || apiKey.OrganizationID == nil || apiKey.GroupID == nil {
		return OrganizationExecutionTrustedAttribution{}, ErrOrganizationResponsesUnsupportedRequest
	}
	auth := apiKey.OrganizationAuthorization
	attribution := OrganizationExecutionTrustedAttribution{
		OrganizationID: auth.OrganizationID, OrganizationStatus: auth.OrganizationStatus, OrganizationBalanceVersionSnapshot: auth.OrganizationBalanceVersion,
		UserID: apiKey.User.ID, UserOrganizationID: dereferenceInt64(apiKey.User.OrganizationID), APIKeyID: apiKey.ID, APIKeyUserID: apiKey.UserID,
		APIKeyOrganizationID: dereferenceInt64(apiKey.OrganizationID), AllocationID: auth.AllocationID, AllocationUserID: auth.AllocationUserID,
		AllocationOrganizationID: auth.AllocationOrganizationID, AllocationStatus: auth.AllocationStatus, AllocationVersionSnapshot: auth.AllocationVersion,
	}
	if err := ValidateOrganizationExecutionTrustedAttribution(attribution); err != nil {
		return OrganizationExecutionTrustedAttribution{}, err
	}
	return attribution, nil
}

func dereferenceInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
