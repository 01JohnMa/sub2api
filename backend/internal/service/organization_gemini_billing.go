package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

var (
	ErrOrganizationGeminiUnsupportedRequest = infraerrors.BadRequest(
		"ORGANIZATION_GEMINI_UNSUPPORTED_REQUEST",
		"the organization Gemini request is not supported by the tested billing adapter",
	)
	ErrOrganizationGeminiNoCandidates = infraerrors.ServiceUnavailable(
		"ORGANIZATION_GEMINI_NO_CANDIDATES",
		"no frozen Gemini or Antigravity candidate is available",
	)
	ErrOrganizationGeminiCandidateConflict = infraerrors.Conflict(
		"ORGANIZATION_GEMINI_CANDIDATE_CONFLICT",
		"a frozen organization Gemini candidate is no longer dispatchable",
	)
)

const (
	organizationGeminiDefaultOutputTokenCeiling = 65000
	organizationGeminiMaximumInputTokenCeiling  = 1_048_576
	organizationGeminiInputProtocolOverhead     = 16 * 1024
	organizationGeminiInputBodyMultiplier       = 2
	organizationGeminiDefaultMaximumAttempts    = 4
)

// OrganizationGeminiRequestPolicy contains only server-validated facts used
// to calculate a candidate ceiling. It is serialised with the reservation and
// never rebuilt from a later mutable request body.
type OrganizationGeminiRequestPolicy struct {
	EndpointFamily     string `json:"endpoint_family"`
	RequestShape       string `json:"request_shape"`
	Action             string `json:"action"`
	RequestedModel     string `json:"requested_model"`
	Stream             bool   `json:"stream"`
	InputTokenCeiling  int    `json:"input_token_ceiling"`
	OutputTokenCeiling int    `json:"output_token_ceiling"`
}

type OrganizationGeminiRequestPolicyInput struct {
	EndpointFamily string
	RequestShape   string
	Action         string
	RequestedModel string
	Body           []byte
}

type OrganizationGeminiCandidatePricing struct {
	Model              string           `json:"model"`
	RateMultiplier     float64          `json:"rate_multiplier"`
	AccountMultiplier  float64          `json:"account_multiplier"`
	InputTokenCeiling  int              `json:"input_token_ceiling"`
	OutputTokenCeiling int              `json:"output_token_ceiling"`
	MaximumCost        string           `json:"maximum_cost"`
	Pricing            *ModelPricing    `json:"pricing"`
	Resolved           *ResolvedPricing `json:"resolved"`
}

type OrganizationGeminiRoute struct {
	AccountID         int64   `json:"account_id"`
	Platform          string  `json:"platform"`
	AccountType       string  `json:"account_type"`
	UpstreamModel     string  `json:"upstream_model"`
	AccountMultiplier float64 `json:"account_multiplier"`
}

type OrganizationGeminiCandidate struct {
	AccountID        int64                              `json:"account_id"`
	RequestedModel   string                             `json:"requested_model"`
	DispatchModel    string                             `json:"dispatch_model"`
	UpstreamModel    string                             `json:"upstream_model"`
	Route            OrganizationGeminiRoute            `json:"route"`
	Pricing          OrganizationGeminiCandidatePricing `json:"pricing"`
	ForwardBodyHash  string                             `json:"forward_body_hash"`
	PlanningBodyHash string                             `json:"planning_body_hash"`
	ForwardBody      []byte                             `json:"-"`
	PreparedPayload  bool                               `json:"prepared_payload"`
	IdentitySummary  string                             `json:"identity_summary,omitempty"`
	Account          *Account                           `json:"-"`
}

type OrganizationGeminiCandidateSnapshot struct {
	EndpointFamily  string                        `json:"endpoint_family"`
	RequestShape    string                        `json:"request_shape"`
	Action          string                        `json:"action"`
	Candidates      []OrganizationGeminiCandidate `json:"candidates"`
	MaximumAttempts int                           `json:"maximum_attempts"`
}

type OrganizationGeminiCandidatePlanInput struct {
	APIKey           *APIKey
	Body             []byte
	EndpointFamily   string
	RequestShape     string
	Action           string
	RequestedModel   string
	MaximumAttempts  int
	PrepareCandidate func(account *Account, dispatchModel, upstreamModel, action string, body []byte) ([]byte, string, error)
}

type OrganizationGeminiCandidatePlan struct {
	Policy                    OrganizationGeminiRequestPolicy
	Candidates                []OrganizationGeminiCandidate
	ForwardBody               []byte
	CandidateSnapshot         []byte
	PricingSnapshot           []byte
	CandidateRouteMaximumCost string
	BillingFingerprint        string
	MaximumAttempts           int
}

func BuildOrganizationGeminiRequestPolicy(input OrganizationGeminiRequestPolicyInput) (OrganizationGeminiRequestPolicy, error) {
	endpointFamily := strings.TrimSpace(input.EndpointFamily)
	requestShape := strings.TrimSpace(input.RequestShape)
	action := strings.TrimSpace(input.Action)
	model := strings.TrimSpace(input.RequestedModel)
	if model == "" || len(input.Body) == 0 || !json.Valid(input.Body) {
		return OrganizationGeminiRequestPolicy{}, ErrOrganizationGeminiUnsupportedRequest
	}

	stream := false
	switch endpointFamily {
	case OrganizationEndpointFamilyGemini:
		if requestShape != OrganizationRequestShapeModelsAction || (action != "generateContent" && action != "streamGenerateContent") {
			return OrganizationGeminiRequestPolicy{}, ErrOrganizationGeminiUnsupportedRequest
		}
		stream = action == "streamGenerateContent"
	case OrganizationEndpointFamilyAntigravity:
		if requestShape != OrganizationRequestShapeHTTP && requestShape != OrganizationRequestShapeStreaming {
			return OrganizationGeminiRequestPolicy{}, ErrOrganizationGeminiUnsupportedRequest
		}
		switch action {
		case "messages":
			stream = requestShape == OrganizationRequestShapeStreaming
		case "generateContent":
			stream = false
		case "streamGenerateContent":
			stream = true
		default:
			return OrganizationGeminiRequestPolicy{}, ErrOrganizationGeminiUnsupportedRequest
		}
	default:
		return OrganizationGeminiRequestPolicy{}, ErrOrganizationGeminiUnsupportedRequest
	}

	inputCeiling := (len(input.Body) + organizationGeminiInputProtocolOverhead) * organizationGeminiInputBodyMultiplier
	if inputCeiling <= 0 || inputCeiling > organizationGeminiMaximumInputTokenCeiling {
		return OrganizationGeminiRequestPolicy{}, ErrOrganizationGeminiUnsupportedRequest
	}
	outputCeiling, ok := organizationGeminiOutputTokenCeiling(endpointFamily, action, input.Body)
	if !ok || outputCeiling <= 0 || outputCeiling > organizationGeminiDefaultOutputTokenCeiling {
		return OrganizationGeminiRequestPolicy{}, ErrOrganizationGeminiUnsupportedRequest
	}

	return OrganizationGeminiRequestPolicy{
		EndpointFamily: endpointFamily, RequestShape: requestShape, Action: action, RequestedModel: model,
		Stream: stream, InputTokenCeiling: inputCeiling, OutputTokenCeiling: outputCeiling,
	}, nil
}

func organizationGeminiOutputTokenCeiling(endpointFamily, action string, body []byte) (int, bool) {
	path := "generationConfig.maxOutputTokens"
	if endpointFamily == OrganizationEndpointFamilyAntigravity && !organizationGeminiNativeAction(action) {
		path = "max_tokens"
	}
	value := gjson.GetBytes(body, path)
	if !value.Exists() {
		return organizationGeminiDefaultOutputTokenCeiling, true
	}
	if value.Type != gjson.Number {
		return 0, false
	}
	ceiling := int(value.Int())
	if int64(ceiling) != value.Int() || ceiling <= 0 {
		return 0, false
	}
	return ceiling, true
}

func organizationGeminiNativeAction(action string) bool {
	return action == "generateContent" || action == "streamGenerateContent"
}

// ApplyOrganizationGeminiOutputTokenCeiling materializes the server default
// in the forwarded payload. Reservation is consequently bounded by the exact
// body dispatched to every frozen candidate, rather than an unenforced local
// assumption about an upstream default.
func ApplyOrganizationGeminiOutputTokenCeiling(body []byte, policy OrganizationGeminiRequestPolicy) ([]byte, error) {
	if len(body) == 0 || !json.Valid(body) {
		return nil, ErrOrganizationGeminiUnsupportedRequest
	}
	if policy.OutputTokenCeiling <= 0 || policy.OutputTokenCeiling > organizationGeminiDefaultOutputTokenCeiling {
		return nil, ErrOrganizationGeminiUnsupportedRequest
	}
	exists := gjson.GetBytes(body, "generationConfig.maxOutputTokens").Exists()
	if policy.EndpointFamily == OrganizationEndpointFamilyAntigravity && !organizationGeminiNativeAction(policy.Action) {
		exists = gjson.GetBytes(body, "max_tokens").Exists()
	}
	if exists {
		return append([]byte(nil), body...), nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil || root == nil {
		return nil, ErrOrganizationGeminiUnsupportedRequest
	}
	ceiling, err := json.Marshal(policy.OutputTokenCeiling)
	if err != nil {
		return nil, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	if policy.EndpointFamily == OrganizationEndpointFamilyAntigravity && !organizationGeminiNativeAction(policy.Action) {
		root["max_tokens"] = ceiling
		bounded, err := json.Marshal(root)
		if err != nil {
			return nil, ErrOrganizationExecutionInvariant.WithCause(err)
		}
		return bounded, nil
	}
	var generationConfig map[string]json.RawMessage
	if raw, found := root["generationConfig"]; found {
		if err := json.Unmarshal(raw, &generationConfig); err != nil || generationConfig == nil {
			return nil, ErrOrganizationGeminiUnsupportedRequest
		}
	} else {
		generationConfig = make(map[string]json.RawMessage)
	}
	generationConfig["maxOutputTokens"] = ceiling
	encodedConfig, err := json.Marshal(generationConfig)
	if err != nil {
		return nil, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	root["generationConfig"] = encodedConfig
	bounded, err := json.Marshal(root)
	if err != nil {
		return nil, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	return bounded, nil
}

func BuildOrganizationGeminiCandidatePricing(
	billing *BillingService,
	resolver *ModelPricingResolver,
	policy OrganizationGeminiRequestPolicy,
	model string,
	multiplier float64,
	accountMultipliers ...float64,
) (OrganizationGeminiCandidatePricing, error) {
	return buildOrganizationGeminiCandidatePricing(context.Background(), billing, resolver, policy, model, multiplier, accountMultipliers, nil)
}

func buildOrganizationGeminiCandidatePricing(
	ctx context.Context,
	billing *BillingService,
	resolver *ModelPricingResolver,
	policy OrganizationGeminiRequestPolicy,
	model string,
	multiplier float64,
	accountMultipliers []float64,
	groupID *int64,
) (OrganizationGeminiCandidatePricing, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	model = strings.TrimSpace(model)
	if billing == nil || model == "" || multiplier < 0 || policy.InputTokenCeiling <= 0 || policy.OutputTokenCeiling <= 0 {
		return OrganizationGeminiCandidatePricing{}, ErrOrganizationGeminiUnsupportedRequest
	}
	accountMultiplier := 1.0
	if len(accountMultipliers) > 0 {
		accountMultiplier = accountMultipliers[0]
	}
	if accountMultiplier < 0 {
		return OrganizationGeminiCandidatePricing{}, ErrOrganizationGeminiUnsupportedRequest
	}

	var resolved *ResolvedPricing
	if resolver != nil {
		resolved = resolver.Resolve(ctx, PricingInput{Model: model, GroupID: groupID})
	}
	if resolved != nil && (resolved.Mode != BillingModeToken || resolved.BasePricing == nil) {
		return OrganizationGeminiCandidatePricing{}, ErrOrganizationGeminiUnsupportedRequest
	}
	if resolved == nil {
		pricing, err := billing.GetModelPricing(model)
		if err != nil || pricing == nil {
			return OrganizationGeminiCandidatePricing{}, ErrOrganizationGeminiUnsupportedRequest
		}
		resolved = &ResolvedPricing{Mode: BillingModeToken, BasePricing: pricing}
	}
	pricing := resolved.BasePricing
	if pricing == nil {
		return OrganizationGeminiCandidatePricing{}, ErrOrganizationGeminiUnsupportedRequest
	}

	// Reserve each input-side pricing class at its bounded maximum. This is
	// deliberately conservative because Gemini may report cache attribution.
	breakdown, err := billing.CalculateCostUnified(CostInput{
		Ctx: ctx, Model: model,
		Tokens: UsageTokens{
			InputTokens: policy.InputTokenCeiling, OutputTokens: policy.OutputTokenCeiling,
			CacheCreationTokens: policy.InputTokenCeiling, CacheReadTokens: policy.InputTokenCeiling,
		},
		RateMultiplier: multiplier * accountMultiplier,
		Resolver:       &ModelPricingResolver{},
		Resolved:       resolved,
	})
	if err != nil || breakdown == nil || breakdown.ActualCost < 0 {
		return OrganizationGeminiCandidatePricing{}, ErrOrganizationGeminiUnsupportedRequest
	}
	maximum, err := NormalizeOrganizationMoney(decimal.NewFromFloat(breakdown.ActualCost).RoundCeil(8).StringFixed(8), true)
	if err != nil {
		return OrganizationGeminiCandidatePricing{}, err
	}
	return OrganizationGeminiCandidatePricing{
		Model: model, RateMultiplier: multiplier, AccountMultiplier: accountMultiplier,
		InputTokenCeiling: policy.InputTokenCeiling, OutputTokenCeiling: policy.OutputTokenCeiling,
		MaximumCost: maximum, Pricing: pricing, Resolved: resolved,
	}, nil
}

func (p OrganizationGeminiCandidatePricing) CalculateActualCost(billing *BillingService, tokens UsageTokens) (string, error) {
	if billing == nil || p.Pricing == nil || tokens.InputTokens < 0 || tokens.OutputTokens < 0 ||
		tokens.CacheCreationTokens < 0 || tokens.CacheReadTokens < 0 {
		return "", ErrOrganizationGeminiUnsupportedRequest
	}
	resolved := p.Resolved
	if resolved == nil {
		resolved = &ResolvedPricing{Mode: BillingModeToken, BasePricing: p.Pricing}
	}
	breakdown, err := billing.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: p.Model, Tokens: tokens,
		RateMultiplier: p.RateMultiplier * p.AccountMultiplier,
		Resolver:       &ModelPricingResolver{}, Resolved: resolved,
	})
	if err != nil || breakdown == nil {
		return "", err
	}
	return NormalizeOrganizationRawActualCost(fmt.Sprintf("%.18f", breakdown.ActualCost))
}

func (s *GatewayService) CalculateOrganizationGeminiActualCost(candidate OrganizationGeminiCandidate, result *ForwardResult) (string, UsageTokens, error) {
	if s == nil || s.billingService == nil || result == nil {
		return "", UsageTokens{}, ErrOrganizationGeminiUnsupportedRequest
	}
	tokens := UsageTokens{
		InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens,
		CacheCreationTokens: result.Usage.CacheCreationInputTokens, CacheReadTokens: result.Usage.CacheReadInputTokens,
		ImageOutputTokens: result.Usage.ImageOutputTokens,
	}
	if tokens.InputTokens == 0 && tokens.OutputTokens == 0 && tokens.CacheCreationTokens == 0 && tokens.CacheReadTokens == 0 ||
		tokens.ImageOutputTokens > 0 || result.ImageCount > 0 {
		return "", tokens, ErrOrganizationExecutionPendingReconciliation
	}
	cost, err := candidate.Pricing.CalculateActualCost(s.billingService, tokens)
	return cost, tokens, err
}

func (s *GatewayService) PlanOrganizationGeminiCandidates(ctx context.Context, input OrganizationGeminiCandidatePlanInput) (*OrganizationGeminiCandidatePlan, error) {
	if s == nil || s.billingService == nil || s.accountRepo == nil || input.APIKey == nil || input.APIKey.GroupID == nil {
		return nil, ErrOrganizationGeminiNoCandidates
	}
	policy, err := BuildOrganizationGeminiRequestPolicy(OrganizationGeminiRequestPolicyInput{
		EndpointFamily: input.EndpointFamily, RequestShape: input.RequestShape, Action: input.Action,
		RequestedModel: input.RequestedModel, Body: input.Body,
	})
	if err != nil {
		return nil, err
	}
	forwardBody, err := ApplyOrganizationGeminiOutputTokenCeiling(input.Body, policy)
	if err != nil {
		return nil, err
	}
	forwardBody, err = canonicalizeOrganizationGeminiForwardBody(forwardBody, policy)
	if err != nil {
		return nil, err
	}
	policy, err = BuildOrganizationGeminiRequestPolicy(OrganizationGeminiRequestPolicyInput{
		EndpointFamily: input.EndpointFamily, RequestShape: input.RequestShape, Action: input.Action,
		RequestedModel: input.RequestedModel, Body: forwardBody,
	})
	if err != nil {
		return nil, err
	}
	if s.checkChannelPricingRestriction(ctx, input.APIKey.GroupID, policy.RequestedModel) {
		return nil, ErrOrganizationGeminiNoCandidates
	}

	platform := PlatformGemini
	hasForcePlatform := false
	if policy.EndpointFamily == OrganizationEndpointFamilyAntigravity {
		platform = PlatformAntigravity
		hasForcePlatform = true
	}
	accounts, useMixed, err := s.listSchedulableAccounts(ctx, input.APIKey.GroupID, platform, hasForcePlatform)
	if err != nil {
		return nil, ErrOrganizationGeminiNoCandidates
	}
	maximumAttempts := input.MaximumAttempts
	if maximumAttempts <= 0 {
		maximumAttempts = organizationGeminiDefaultMaximumAttempts
	}

	groupMapping, _ := s.ResolveChannelMappingAndRestrict(ctx, input.APIKey.GroupID, policy.RequestedModel)
	scheduledModel := policy.RequestedModel
	if groupMapping.Mapped {
		scheduledModel = strings.TrimSpace(groupMapping.MappedModel)
	}
	if scheduledModel == "" {
		return nil, ErrOrganizationGeminiNoCandidates
	}
	multiplier := organizationGeminiRateMultiplier(s, ctx, input.APIKey)
	candidates := make([]OrganizationGeminiCandidate, 0, maximumAttempts)
	maximumCost := ""
	for i := range accounts {
		account := accounts[i]
		if !s.isAccountAllowedForPlatform(&account, platform, useMixed) || !s.isAccountSchedulableForSelection(&account) ||
			!s.isAccountSchedulableForModelSelection(ctx, &account, scheduledModel) ||
			!s.isModelSupportedByAccountWithContext(ctx, &account, scheduledModel) {
			continue
		}
		upstreamModel := organizationGeminiUpstreamModel(&account, policy.EndpointFamily, scheduledModel)
		upstreamModel = organizationGeminiEffectiveDispatchModel(policy, candidateBodyForOrganizationGeminiModelResolution(forwardBody, scheduledModel, policy), upstreamModel)
		if strings.TrimSpace(upstreamModel) == "" {
			continue
		}
		if s.needsUpstreamChannelRestrictionCheck(ctx, input.APIKey.GroupID) && s.channelService != nil &&
			s.channelService.IsModelRestricted(ctx, *input.APIKey.GroupID, upstreamModel) {
			continue
		}
		pricing, err := buildOrganizationGeminiCandidatePricing(ctx, s.billingService, s.resolver, policy, upstreamModel, multiplier, []float64{account.BillingRateMultiplier()}, input.APIKey.GroupID)
		if err != nil {
			continue
		}
		candidateBody := forwardBody
		if policy.EndpointFamily == OrganizationEndpointFamilyAntigravity && scheduledModel != policy.RequestedModel {
			candidateBody = s.ReplaceModelInBody(forwardBody, scheduledModel)
		}
		preparedPayload := false
		identitySummary := ""
		requiresPreparedPayload := account.Platform == PlatformAntigravity && account.Type != AccountTypeAPIKey
		if requiresPreparedPayload {
			if input.PrepareCandidate == nil {
				continue
			}
			candidateBody, identitySummary, err = input.PrepareCandidate(&account, scheduledModel, upstreamModel, policy.Action, candidateBody)
			if err != nil || len(candidateBody) == 0 {
				continue
			}
			preparedPayload = true
		}
		candidate := OrganizationGeminiCandidate{
			AccountID: account.ID, RequestedModel: policy.RequestedModel, DispatchModel: scheduledModel, UpstreamModel: upstreamModel,
			Route:   OrganizationGeminiRoute{AccountID: account.ID, Platform: account.Platform, AccountType: account.Type, UpstreamModel: upstreamModel, AccountMultiplier: account.BillingRateMultiplier()},
			Pricing: pricing, ForwardBodyHash: organizationGeminiBodyHash(candidateBody), PlanningBodyHash: organizationGeminiBodyHash(forwardBody), ForwardBody: candidateBody, PreparedPayload: preparedPayload, IdentitySummary: identitySummary, Account: &account,
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
		return nil, ErrOrganizationGeminiNoCandidates
	}
	snapshot := OrganizationGeminiCandidateSnapshot{
		EndpointFamily: policy.EndpointFamily, RequestShape: policy.RequestShape, Action: policy.Action,
		Candidates: candidates, MaximumAttempts: len(candidates),
	}
	candidateSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return nil, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	pricingSnapshot, err := json.Marshal(map[string]any{"policy": policy, "candidates": candidates})
	if err != nil {
		return nil, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	fingerprint := sha256.Sum256(append(append(append([]byte(nil), forwardBody...), candidateSnapshot...), pricingSnapshot...))
	return &OrganizationGeminiCandidatePlan{
		Policy: policy, Candidates: candidates, ForwardBody: forwardBody, CandidateSnapshot: candidateSnapshot, PricingSnapshot: pricingSnapshot,
		CandidateRouteMaximumCost: maximumCost, BillingFingerprint: hex.EncodeToString(fingerprint[:]), MaximumAttempts: len(candidates),
	}, nil
}

// canonicalizeOrganizationGeminiForwardBody applies the deterministic native
// Gemini compatibility transforms before the reservation snapshot is built.
// The forwarding services repeat these transforms defensively, but their input
// is now the same canonical body that was priced and hashed here.
func canonicalizeOrganizationGeminiForwardBody(body []byte, policy OrganizationGeminiRequestPolicy) ([]byte, error) {
	if len(body) == 0 || !json.Valid(body) {
		return nil, ErrOrganizationGeminiUnsupportedRequest
	}
	if !organizationGeminiNativeAction(policy.Action) {
		return append([]byte(nil), body...), nil
	}
	filtered, err := filterEmptyPartsFromGeminiRequest(body)
	if err != nil {
		return nil, ErrOrganizationGeminiUnsupportedRequest
	}
	return ensureGeminiFunctionCallThoughtSignatures(filtered), nil
}

func (s *GatewayService) RevalidateOrganizationGeminiCandidate(ctx context.Context, apiKey *APIKey, candidate OrganizationGeminiCandidate, policy OrganizationGeminiRequestPolicy, body []byte) (*OrganizationGeminiCandidate, error) {
	if s == nil || s.accountRepo == nil || apiKey == nil || apiKey.GroupID == nil || candidate.AccountID <= 0 ||
		organizationGeminiBodyHash(body) != candidate.PlanningBodyHash {
		return nil, ErrOrganizationGeminiCandidateConflict
	}
	account, err := s.accountRepo.GetByID(ctx, candidate.AccountID)
	if err != nil || account == nil || account.ID != candidate.AccountID || !s.isAccountInGroup(account, apiKey.GroupID) ||
		!s.isAccountSchedulableForSelection(account) || !s.isAccountSchedulableForModelSelection(ctx, account, candidate.UpstreamModel) {
		return nil, ErrOrganizationGeminiCandidateConflict
	}
	platform := PlatformGemini
	useMixed := true
	if policy.EndpointFamily == OrganizationEndpointFamilyAntigravity {
		platform = PlatformAntigravity
		useMixed = false
	}
	if !s.isAccountAllowedForPlatform(account, platform, useMixed) {
		return nil, ErrOrganizationGeminiCandidateConflict
	}
	groupMapping, _ := s.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, candidate.RequestedModel)
	scheduledModel := candidate.RequestedModel
	if groupMapping.Mapped {
		scheduledModel = strings.TrimSpace(groupMapping.MappedModel)
	}
	if scheduledModel != candidate.DispatchModel {
		return nil, ErrOrganizationGeminiCandidateConflict
	}
	upstreamModel := organizationGeminiUpstreamModel(account, policy.EndpointFamily, scheduledModel)
	upstreamModel = organizationGeminiEffectiveDispatchModel(policy, body, upstreamModel)
	forwardBody := body
	if candidate.PreparedPayload {
		if organizationGeminiBodyHash(candidate.ForwardBody) != candidate.ForwardBodyHash || candidate.IdentitySummary != organizationGeminiAntigravityIdentitySummary(account, upstreamModel) {
			return nil, ErrOrganizationGeminiCandidateConflict
		}
		forwardBody = candidate.ForwardBody
	} else if policy.EndpointFamily == OrganizationEndpointFamilyAntigravity && scheduledModel != candidate.RequestedModel {
		forwardBody = s.ReplaceModelInBody(body, scheduledModel)
	}
	if organizationGeminiBodyHash(forwardBody) != candidate.ForwardBodyHash {
		return nil, ErrOrganizationGeminiCandidateConflict
	}
	if upstreamModel != candidate.UpstreamModel || !s.isModelSupportedByAccountWithContext(ctx, account, scheduledModel) ||
		(s.needsUpstreamChannelRestrictionCheck(ctx, apiKey.GroupID) && s.channelService != nil && s.channelService.IsModelRestricted(ctx, *apiKey.GroupID, upstreamModel)) {
		return nil, ErrOrganizationGeminiCandidateConflict
	}
	multiplier := organizationGeminiRateMultiplier(s, ctx, apiKey)
	pricing, err := buildOrganizationGeminiCandidatePricing(ctx, s.billingService, s.resolver, policy, upstreamModel, multiplier, []float64{account.BillingRateMultiplier()}, apiKey.GroupID)
	if err != nil {
		return nil, ErrOrganizationGeminiCandidateConflict
	}
	currentPricing, _ := json.Marshal(pricing)
	frozenPricing, _ := json.Marshal(candidate.Pricing)
	if !bytes.Equal(currentPricing, frozenPricing) || account.Platform != candidate.Route.Platform || account.Type != candidate.Route.AccountType {
		return nil, ErrOrganizationGeminiCandidateConflict
	}
	validated := candidate
	validated.Account = account
	validated.Pricing = pricing
	validated.ForwardBody = forwardBody
	return &validated, nil
}

func organizationGeminiUpstreamModel(account *Account, endpointFamily, dispatchModel string) string {
	if account == nil {
		return ""
	}
	if endpointFamily == OrganizationEndpointFamilyAntigravity || account.Platform == PlatformAntigravity || account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		return account.GetMappedModel(dispatchModel)
	}
	return dispatchModel
}

func candidateBodyForOrganizationGeminiModelResolution(body []byte, scheduledModel string, policy OrganizationGeminiRequestPolicy) []byte {
	if policy.EndpointFamily == OrganizationEndpointFamilyAntigravity && policy.Action == "messages" && scheduledModel != policy.RequestedModel {
		return ReplaceModelInBody(body, scheduledModel)
	}
	return body
}

func organizationGeminiEffectiveDispatchModel(policy OrganizationGeminiRequestPolicy, body []byte, model string) string {
	if policy.EndpointFamily == OrganizationEndpointFamilyAntigravity && policy.Action == "messages" && model == "claude-sonnet-4-5" {
		thinking := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "thinking.type").String()))
		if thinking == "enabled" || thinking == "adaptive" {
			return "claude-sonnet-4-5-thinking"
		}
	}
	return model
}

func organizationGeminiAntigravityIdentitySummary(account *Account, upstreamModel string) string {
	projectID := ""
	if account != nil {
		projectID = strings.TrimSpace(account.GetCredential("project_id"))
	}
	digest := sha256.Sum256([]byte(projectID + "|" + strings.TrimSpace(upstreamModel) + "|antigravity_identity_v1"))
	return hex.EncodeToString(digest[:])
}

func (p OrganizationGeminiCandidatePlan) NewExecutionReservationInput(apiKey *APIKey, clientRequestID string) (OrganizationExecutionReservationInput, error) {
	attribution, err := organizationGeminiExecutionAttribution(apiKey)
	if err != nil {
		return OrganizationExecutionReservationInput{}, err
	}
	return OrganizationExecutionReservationInput{
		Attribution: attribution, EndpointFamily: p.Policy.EndpointFamily, RequestShape: p.Policy.RequestShape,
		Model: p.Policy.RequestedModel, ClientRequestID: clientRequestID,
		CandidateSnapshot: p.CandidateSnapshot, PricingSnapshot: p.PricingSnapshot,
		CandidateRouteMaximumCost: p.CandidateRouteMaximumCost, BillingFingerprint: p.BillingFingerprint,
	}, nil
}

func NewOrganizationGeminiPreDispatchRejectionInput(apiKey *APIKey, body []byte, endpointFamily, requestShape, action, requestedModel, clientRequestID string, rejection error) (OrganizationExecutionReservationInput, error) {
	attribution, err := organizationGeminiExecutionAttribution(apiKey)
	if err != nil {
		return OrganizationExecutionReservationInput{}, err
	}
	reason := strings.ToLower(strings.TrimSpace(infraerrors.Reason(rejection)))
	if reason == "" {
		reason = "organization_gemini_pre_dispatch_rejected"
	}
	proof := "request_policy_rejected:" + reason
	candidateSnapshot, err := json.Marshal(map[string]any{
		"endpoint_family": endpointFamily, "request_shape": requestShape, "action": action,
		"requested_model": strings.TrimSpace(requestedModel), "candidates": []any{}, "maximum_attempts": 0, "zero_proof": proof,
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
		Attribution: attribution, EndpointFamily: endpointFamily, RequestShape: requestShape, Model: strings.TrimSpace(requestedModel), ClientRequestID: clientRequestID,
		CandidateSnapshot: candidateSnapshot, PricingSnapshot: pricingSnapshot, CandidateRouteMaximumCost: "0.00000000",
		BillingFingerprint: hex.EncodeToString(fingerprint[:]), PreDispatchZeroProof: proof,
	}, nil
}

func organizationGeminiExecutionAttribution(apiKey *APIKey) (OrganizationExecutionTrustedAttribution, error) {
	if apiKey == nil || apiKey.OrganizationAuthorization == nil || apiKey.User == nil || apiKey.OrganizationID == nil {
		return OrganizationExecutionTrustedAttribution{}, ErrOrganizationGeminiUnsupportedRequest
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

func organizationGeminiRateMultiplier(s *GatewayService, ctx context.Context, apiKey *APIKey) float64 {
	if s == nil || apiKey == nil || apiKey.User == nil || apiKey.Group == nil {
		return 1
	}
	if s.userGroupRateResolver != nil {
		return s.userGroupRateResolver.Resolve(ctx, apiKey.User.ID, apiKey.Group.ID, apiKey.Group.RateMultiplier)
	}
	return apiKey.Group.RateMultiplier
}

func organizationGeminiBodyHash(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}
