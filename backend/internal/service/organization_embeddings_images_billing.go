package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

var (
	ErrOrganizationEmbeddingsImagesUnsupportedRequest = infraerrors.BadRequest(
		"ORGANIZATION_EMBEDDINGS_IMAGES_UNSUPPORTED_REQUEST",
		"the organization Embeddings or Images request is not supported by the tested billing adapter",
	)
	ErrOrganizationEmbeddingsImagesNoCandidates = infraerrors.ServiceUnavailable(
		"ORGANIZATION_EMBEDDINGS_IMAGES_NO_CANDIDATES",
		"no frozen OpenAI Embeddings or Images candidate is available",
	)
	ErrOrganizationEmbeddingsImagesCandidateConflict = infraerrors.Conflict(
		"ORGANIZATION_EMBEDDINGS_IMAGES_CANDIDATE_CONFLICT",
		"a frozen organization Embeddings or Images candidate is no longer dispatchable",
	)
)

const (
	organizationEmbeddingsMaximumBatchInputs = 2048
	organizationEmbeddingsInputOverhead      = 1024
	organizationEmbeddingsMaximumInputBytes  = 8191

	organizationImagesMaximumOutputCount = 10
	organizationImagesMaximumInputCount  = 16
	organizationImagesMaximumInputPixels = 4096 * 4096
)

// OrganizationEmbeddingsRequestPolicy is the bounded subset accepted for
// organization-funded embeddings. The token ceiling deliberately derives from
// server-observed JSON bytes, rather than a client token count.
type OrganizationEmbeddingsRequestPolicy struct {
	RequestedModel     string `json:"requested_model"`
	InputCount         int    `json:"input_count"`
	InputTokenCeiling  int    `json:"input_token_ceiling"`
	MaximumInputTokens int    `json:"maximum_input_tokens"`
}

// OrganizationImagesRequestPolicy records every image bound the adapter can
// prove before dispatch. URL and undecodable edit inputs are excluded because
// their dimensions cannot be frozen safely.
type OrganizationImagesRequestPolicy struct {
	RequestedModel     string   `json:"requested_model"`
	RequestShape       string   `json:"request_shape"`
	OutputImageCount   int      `json:"output_image_count"`
	OutputSize         string   `json:"output_size"`
	InputImageCount    int      `json:"input_image_count"`
	InputImageSizes    []string `json:"input_image_sizes,omitempty"`
	MaximumInputPixels int      `json:"maximum_input_pixels"`
}

// OrganizationEmbeddingsImagesCandidatePricing is an immutable per-account
// price snapshot. Image costs support either explicit channel request pricing
// or explicitly configured group image pricing; legacy fallback prices are not
// safe enough for organization reservations.
type OrganizationEmbeddingsImagesCandidatePricing struct {
	Model              string            `json:"model"`
	EndpointFamily     string            `json:"endpoint_family"`
	RateMultiplier     float64           `json:"rate_multiplier"`
	AccountMultiplier  float64           `json:"account_multiplier"`
	InputTokenCeiling  int               `json:"input_token_ceiling,omitempty"`
	OutputImageCount   int               `json:"output_image_count,omitempty"`
	OutputSize         string            `json:"output_size,omitempty"`
	ImagePricingSource string            `json:"image_pricing_source,omitempty"`
	ImageTierPrices    map[string]string `json:"image_tier_prices,omitempty"`
	MaximumCost        string            `json:"maximum_cost"`
	Pricing            *ModelPricing     `json:"pricing,omitempty"`
	Resolved           *ResolvedPricing  `json:"resolved,omitempty"`
}

type OrganizationEmbeddingsImagesCandidate struct {
	AccountID          int64                                        `json:"account_id"`
	AccountType        string                                       `json:"account_type"`
	RequestedModel     string                                       `json:"requested_model"`
	ChannelMappedModel string                                       `json:"channel_mapped_model"`
	UpstreamModel      string                                       `json:"upstream_model"`
	EndpointFamily     string                                       `json:"endpoint_family"`
	RequestShape       string                                       `json:"request_shape"`
	Route              string                                       `json:"route"`
	ForwardBodyHash    string                                       `json:"forward_body_hash"`
	Pricing            OrganizationEmbeddingsImagesCandidatePricing `json:"pricing"`
	Account            *Account                                     `json:"-"`
	PlanningBody       []byte                                       `json:"-"`
	ForwardBody        []byte                                       `json:"-"`
	ParsedImages       *OpenAIImagesRequest                         `json:"-"`
}

type OrganizationEmbeddingsImagesCandidateSnapshot struct {
	EndpointFamily  string                                  `json:"endpoint_family"`
	RequestShape    string                                  `json:"request_shape"`
	Candidates      []OrganizationEmbeddingsImagesCandidate `json:"candidates"`
	MaximumAttempts int                                     `json:"maximum_attempts"`
}

type OrganizationEmbeddingsImagesCandidatePlanInput struct {
	APIKey          *APIKey
	Body            []byte
	ParsedImages    *OpenAIImagesRequest
	MaximumAttempts int
}

type OrganizationEmbeddingsImagesCandidatePlan struct {
	EndpointFamily            string
	RequestShape              string
	Model                     string
	EmbeddingsPolicy          *OrganizationEmbeddingsRequestPolicy
	ImagesPolicy              *OrganizationImagesRequestPolicy
	Candidates                []OrganizationEmbeddingsImagesCandidate
	CandidateSnapshot         []byte
	PricingSnapshot           []byte
	CandidateRouteMaximumCost string
	BillingFingerprint        string
	MaximumAttempts           int
}

func BuildOrganizationEmbeddingsRequestPolicy(body []byte) (OrganizationEmbeddingsRequestPolicy, error) {
	policy := OrganizationEmbeddingsRequestPolicy{}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return policy, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	model := gjson.GetBytes(body, "model")
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return policy, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	maximumInputTokens, ok := verifiedOrganizationEmbeddingModelLimit(model.String())
	if !ok {
		return policy, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	input := gjson.GetBytes(body, "input")
	count, inputBytes, ok := organizationEmbeddingsInputStats(input)
	if !ok || count <= 0 || count > organizationEmbeddingsMaximumBatchInputs {
		return policy, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	ceiling := inputBytes + organizationEmbeddingsInputOverhead
	if ceiling <= 0 || ceiling > maximumInputTokens || ceiling > organizationEmbeddingsMaximumInputBytes {
		return policy, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	return OrganizationEmbeddingsRequestPolicy{
		RequestedModel: strings.TrimSpace(model.String()), InputCount: count,
		InputTokenCeiling: ceiling, MaximumInputTokens: maximumInputTokens,
	}, nil
}

func organizationEmbeddingsInputCount(value gjson.Result) (int, bool) {
	count, _, ok := organizationEmbeddingsInputStats(value)
	return count, ok
}

func organizationEmbeddingsInputStats(value gjson.Result) (int, int, bool) {
	if !value.Exists() {
		return 0, 0, false
	}
	if value.Type == gjson.String {
		input := value.String()
		return 1, len(input), strings.TrimSpace(input) != ""
	}
	if !value.IsArray() {
		return 0, 0, false
	}
	items := value.Array()
	if len(items) == 0 {
		return 0, 0, false
	}
	inputBytes := 0
	for _, item := range items {
		input := item.String()
		if item.Type != gjson.String || strings.TrimSpace(input) == "" {
			return 0, 0, false
		}
		inputBytes += len(input)
	}
	return len(items), inputBytes, true
}

func verifiedOrganizationEmbeddingModelLimit(model string) (int, bool) {
	model = strings.ToLower(strings.TrimSpace(lastOpenAIModelSegment(model)))
	switch model {
	case "text-embedding-3-small", "text-embedding-3-large", "text-embedding-ada-002":
		return organizationEmbeddingsMaximumInputBytes, true
	default:
		return 0, false
	}
}

func BuildOrganizationImagesRequestPolicy(parsed *OpenAIImagesRequest) (OrganizationImagesRequestPolicy, error) {
	policy := OrganizationImagesRequestPolicy{}
	if parsed == nil || strings.TrimSpace(parsed.Model) == "" || parsed.N <= 0 || parsed.N > organizationImagesMaximumOutputCount {
		return policy, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	shape := OrganizationRequestShapeGenerations
	if parsed.IsEdits() {
		shape = OrganizationRequestShapeEdits
	} else if parsed.Endpoint != openAIImagesGenerationsEndpoint {
		return policy, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	outputSize, outputKnown := ClassifyImageBillingTier(parsed.Size)
	if parsed.ExplicitSize && !outputKnown {
		return policy, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	if !outputKnown {
		// ParseOpenAIImagesRequest applies the existing server-owned default.
		outputSize = ImageBillingSize2K
	}
	policy = OrganizationImagesRequestPolicy{
		RequestedModel: strings.TrimSpace(parsed.Model), RequestShape: shape,
		OutputImageCount: parsed.N, OutputSize: outputSize,
		MaximumInputPixels: organizationImagesMaximumInputPixels,
	}
	if shape == OrganizationRequestShapeGenerations {
		return policy, nil
	}
	if !parsed.Multipart || len(parsed.Uploads) == 0 || len(parsed.Uploads) > organizationImagesMaximumInputCount ||
		len(parsed.InputImageURLs) > 0 || strings.TrimSpace(parsed.MaskImageURL) != "" {
		return OrganizationImagesRequestPolicy{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	for _, upload := range parsed.Uploads {
		width, height, ok := organizationImageUploadDimensions(upload)
		if !ok || width*height > organizationImagesMaximumInputPixels {
			return OrganizationImagesRequestPolicy{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
		}
		policy.InputImageSizes = append(policy.InputImageSizes, fmt.Sprintf("%dx%d", width, height))
	}
	if parsed.MaskUpload != nil {
		width, height, ok := organizationImageUploadDimensions(*parsed.MaskUpload)
		if !ok || width*height > organizationImagesMaximumInputPixels {
			return OrganizationImagesRequestPolicy{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
		}
		policy.InputImageSizes = append(policy.InputImageSizes, fmt.Sprintf("%dx%d", width, height))
	}
	policy.InputImageCount = len(policy.InputImageSizes)
	return policy, nil
}

func organizationImageUploadDimensions(upload OpenAIImagesUpload) (int, int, bool) {
	if upload.Width > 0 && upload.Height > 0 {
		return upload.Width, upload.Height, true
	}
	if len(upload.Data) == 0 {
		return 0, 0, false
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(upload.Data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

func (s *OpenAIGatewayService) PlanOrganizationEmbeddingsCandidates(ctx context.Context, input OrganizationEmbeddingsImagesCandidatePlanInput) (*OrganizationEmbeddingsImagesCandidatePlan, error) {
	policy, err := BuildOrganizationEmbeddingsRequestPolicy(input.Body)
	if err != nil {
		return nil, err
	}
	return s.planOrganizationEmbeddingsImagesCandidates(ctx, input, OrganizationEndpointFamilyEmbeddings, OrganizationRequestShapeHTTP, policy.RequestedModel, &policy, nil)
}

func (s *OpenAIGatewayService) PlanOrganizationImagesCandidates(ctx context.Context, input OrganizationEmbeddingsImagesCandidatePlanInput) (*OrganizationEmbeddingsImagesCandidatePlan, error) {
	policy, err := BuildOrganizationImagesRequestPolicy(input.ParsedImages)
	if err != nil {
		return nil, err
	}
	return s.planOrganizationEmbeddingsImagesCandidates(ctx, input, OrganizationEndpointFamilyImages, policy.RequestShape, policy.RequestedModel, nil, &policy)
}

func (s *OpenAIGatewayService) planOrganizationEmbeddingsImagesCandidates(
	ctx context.Context,
	input OrganizationEmbeddingsImagesCandidatePlanInput,
	family, shape, requestedModel string,
	embeddings *OrganizationEmbeddingsRequestPolicy,
	images *OrganizationImagesRequestPolicy,
) (*OrganizationEmbeddingsImagesCandidatePlan, error) {
	if s == nil || s.accountRepo == nil || s.billingService == nil || input.APIKey == nil || input.APIKey.GroupID == nil {
		return nil, ErrOrganizationEmbeddingsImagesNoCandidates
	}
	if s.checkChannelPricingRestriction(ctx, input.APIKey.GroupID, requestedModel) {
		return nil, ErrOrganizationEmbeddingsImagesNoCandidates
	}
	accounts, err := s.listSchedulableAccounts(ctx, input.APIKey.GroupID)
	if err != nil {
		return nil, ErrOrganizationEmbeddingsImagesNoCandidates
	}
	sortOrganizationEmbeddingsImagesAccounts(accounts)
	maxAttempts := input.MaximumAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	if maxAttempts > organizationResponsesMaximumAttempts(s.cfg) {
		maxAttempts = organizationResponsesMaximumAttempts(s.cfg)
	}
	mapping, _ := s.ResolveChannelMappingAndRestrict(ctx, input.APIKey.GroupID, requestedModel)
	channelMappedModel := strings.TrimSpace(mapping.MappedModel)
	if channelMappedModel == "" {
		channelMappedModel = requestedModel
	}
	needsUpstreamRestrictionCheck := s.needsUpstreamChannelRestrictionCheck(ctx, input.APIKey.GroupID)
	candidates := make([]OrganizationEmbeddingsImagesCandidate, 0, maxAttempts)
	maximumCost := ""
	for index := range accounts {
		account := accounts[index]
		capability := OpenAIEndpointCapabilityEmbeddings
		if family == OrganizationEndpointFamilyImages {
			capability = ""
		}
		fresh := s.resolveFreshSchedulableOpenAIAccount(ctx, &account, requestedModel, false, capability)
		if fresh == nil || fresh.IsOpenAIPassthroughEnabled() {
			continue
		}
		account = *fresh
		if family == OrganizationEndpointFamilyImages && !accountSupportsOpenAICapabilities(&account, capability, input.ParsedImages.RequiredCapability) {
			continue
		}
		if family == OrganizationEndpointFamilyEmbeddings && account.Type != AccountTypeAPIKey {
			continue
		}
		if family == OrganizationEndpointFamilyImages && account.Type != AccountTypeAPIKey && account.Type != AccountTypeOAuth {
			continue
		}
		upstreamModel := channelMappedModel
		if account.Type == AccountTypeAPIKey {
			upstreamModel = account.GetMappedModel(channelMappedModel)
		}
		if strings.TrimSpace(upstreamModel) == "" || (family == OrganizationEndpointFamilyImages && !isOpenAIImageGenerationModel(upstreamModel)) {
			continue
		}
		if family == OrganizationEndpointFamilyEmbeddings {
			if _, ok := verifiedOrganizationEmbeddingModelLimit(upstreamModel); !ok {
				continue
			}
		}
		if needsUpstreamRestrictionCheck && s.channelService != nil && s.channelService.IsModelRestricted(ctx, *input.APIKey.GroupID, upstreamModel) {
			continue
		}
		multiplier := organizationEmbeddingsImagesRateMultiplier(ctx, s, input.APIKey)
		pricing, err := s.buildOrganizationEmbeddingsImagesCandidatePricing(ctx, input.APIKey, family, embeddings, images, upstreamModel, multiplier, account.BillingRateMultiplier())
		if err != nil {
			continue
		}
		forwardBody := append([]byte(nil), input.Body...)
		if family == OrganizationEndpointFamilyEmbeddings {
			forwardBody = s.ReplaceModelInBody(input.Body, channelMappedModel)
		}
		candidate := OrganizationEmbeddingsImagesCandidate{
			AccountID: account.ID, AccountType: account.Type, RequestedModel: requestedModel, ChannelMappedModel: channelMappedModel,
			UpstreamModel: upstreamModel, EndpointFamily: family, RequestShape: shape, Route: organizationEmbeddingsImagesRoute(family, account.Type),
			ForwardBodyHash: organizationEmbeddingsImagesBodyHash(forwardBody), Pricing: pricing, Account: &account,
			PlanningBody: append([]byte(nil), input.Body...), ForwardBody: forwardBody, ParsedImages: input.ParsedImages,
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
		return nil, ErrOrganizationEmbeddingsImagesNoCandidates
	}
	snapshot := OrganizationEmbeddingsImagesCandidateSnapshot{EndpointFamily: family, RequestShape: shape, Candidates: candidates, MaximumAttempts: len(candidates)}
	candidateSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return nil, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	pricingSnapshot, err := json.Marshal(map[string]any{"embeddings_policy": embeddings, "images_policy": images, "candidates": candidates})
	if err != nil {
		return nil, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	digest := sha256.Sum256(append(append(append([]byte(nil), input.Body...), candidateSnapshot...), pricingSnapshot...))
	return &OrganizationEmbeddingsImagesCandidatePlan{
		EndpointFamily: family, RequestShape: shape, Model: requestedModel, EmbeddingsPolicy: embeddings, ImagesPolicy: images,
		Candidates: candidates, CandidateSnapshot: candidateSnapshot, PricingSnapshot: pricingSnapshot,
		CandidateRouteMaximumCost: maximumCost, BillingFingerprint: hex.EncodeToString(digest[:]), MaximumAttempts: len(candidates),
	}, nil
}

func sortOrganizationEmbeddingsImagesAccounts(accounts []Account) {
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

func organizationEmbeddingsImagesRateMultiplier(ctx context.Context, s *OpenAIGatewayService, apiKey *APIKey) float64 {
	if s == nil || apiKey == nil || apiKey.User == nil || apiKey.Group == nil {
		return 1
	}
	if s.userGroupRateResolver != nil {
		return s.userGroupRateResolver.Resolve(ctx, apiKey.User.ID, apiKey.Group.ID, apiKey.Group.RateMultiplier)
	}
	return apiKey.Group.RateMultiplier
}

func (s *OpenAIGatewayService) buildOrganizationEmbeddingsImagesCandidatePricing(
	ctx context.Context,
	apiKey *APIKey,
	family string,
	embeddings *OrganizationEmbeddingsRequestPolicy,
	images *OrganizationImagesRequestPolicy,
	model string,
	multiplier, accountMultiplier float64,
) (OrganizationEmbeddingsImagesCandidatePricing, error) {
	if multiplier < 0 || accountMultiplier < 0 {
		return OrganizationEmbeddingsImagesCandidatePricing{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	if family == OrganizationEndpointFamilyEmbeddings {
		return s.buildOrganizationEmbeddingsCandidatePricing(ctx, apiKey, embeddings, model, multiplier, accountMultiplier)
	}
	return s.buildOrganizationImagesCandidatePricing(ctx, apiKey, images, model, multiplier, accountMultiplier)
}

func (s *OpenAIGatewayService) buildOrganizationEmbeddingsCandidatePricing(ctx context.Context, apiKey *APIKey, policy *OrganizationEmbeddingsRequestPolicy, model string, multiplier, accountMultiplier float64) (OrganizationEmbeddingsImagesCandidatePricing, error) {
	if policy == nil || policy.InputTokenCeiling <= 0 || s == nil || s.billingService == nil {
		return OrganizationEmbeddingsImagesCandidatePricing{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	var resolved *ResolvedPricing
	if s.resolver != nil {
		resolved = s.resolver.Resolve(ctx, PricingInput{Model: model, GroupID: apiKey.GroupID})
	}
	if resolved == nil {
		pricing, err := s.billingService.GetModelPricing(model)
		if err != nil || pricing == nil {
			return OrganizationEmbeddingsImagesCandidatePricing{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
		}
		resolved = &ResolvedPricing{Mode: BillingModeToken, BasePricing: pricing}
	}
	if resolved.Mode != BillingModeToken || resolved.BasePricing == nil || !organizationEmbeddingsPricingHasInputRate(resolved) {
		return OrganizationEmbeddingsImagesCandidatePricing{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	breakdown, err := s.billingService.CalculateCostUnified(CostInput{
		Ctx: ctx, Model: model, GroupID: apiKey.GroupID, Tokens: UsageTokens{InputTokens: policy.InputTokenCeiling},
		RateMultiplier: multiplier * accountMultiplier, Resolver: &ModelPricingResolver{}, Resolved: resolved,
	})
	if err != nil || breakdown == nil || math.IsNaN(breakdown.ActualCost) || math.IsInf(breakdown.ActualCost, 0) || breakdown.ActualCost < 0 {
		return OrganizationEmbeddingsImagesCandidatePricing{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	maximum, err := NormalizeOrganizationMoney(decimal.NewFromFloat(breakdown.ActualCost).RoundCeil(8).StringFixed(8), true)
	if err != nil {
		return OrganizationEmbeddingsImagesCandidatePricing{}, err
	}
	return OrganizationEmbeddingsImagesCandidatePricing{Model: model, EndpointFamily: OrganizationEndpointFamilyEmbeddings, RateMultiplier: multiplier, AccountMultiplier: accountMultiplier, InputTokenCeiling: policy.InputTokenCeiling, MaximumCost: maximum, Pricing: resolved.BasePricing, Resolved: resolved}, nil
}

func organizationEmbeddingsPricingHasInputRate(resolved *ResolvedPricing) bool {
	if resolved == nil || resolved.BasePricing == nil {
		return false
	}
	if resolved.BasePricing.InputPricePerToken > 0 || resolved.BasePricing.InputPricePerTokenPriority > 0 {
		return true
	}
	for _, interval := range resolved.Intervals {
		if interval.InputPrice != nil && *interval.InputPrice > 0 {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) buildOrganizationImagesCandidatePricing(ctx context.Context, apiKey *APIKey, policy *OrganizationImagesRequestPolicy, model string, multiplier, accountMultiplier float64) (OrganizationEmbeddingsImagesCandidatePricing, error) {
	if policy == nil || policy.OutputImageCount <= 0 || policy.OutputSize == "" || apiKey == nil || apiKey.Group == nil || s == nil || s.billingService == nil {
		return OrganizationEmbeddingsImagesCandidatePricing{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	effectiveMultiplier := multiplier * accountMultiplier
	if s.resolver != nil {
		resolved := s.resolver.Resolve(ctx, PricingInput{Model: model, GroupID: apiKey.GroupID})
		if resolved != nil && resolved.Source == PricingSourceChannel && (resolved.Mode == BillingModeImage || resolved.Mode == BillingModePerRequest) {
			breakdown, err := s.billingService.CalculateCostUnified(CostInput{Ctx: ctx, Model: model, GroupID: apiKey.GroupID, RequestCount: policy.OutputImageCount, SizeTier: policy.OutputSize, RateMultiplier: effectiveMultiplier, Resolver: &ModelPricingResolver{}, Resolved: resolved})
			if err == nil && breakdown != nil && finitePositive(breakdown.ActualCost) {
				maximum, moneyErr := NormalizeOrganizationMoney(decimal.NewFromFloat(breakdown.ActualCost).RoundCeil(8).StringFixed(8), true)
				if moneyErr == nil {
					return OrganizationEmbeddingsImagesCandidatePricing{Model: model, EndpointFamily: OrganizationEndpointFamilyImages, RateMultiplier: multiplier, AccountMultiplier: accountMultiplier, OutputImageCount: policy.OutputImageCount, OutputSize: policy.OutputSize, ImagePricingSource: PricingSourceChannel, MaximumCost: maximum, Resolved: resolved}, nil
				}
			}
		}
	}
	prices, ok := organizationExplicitGroupImageTierPrices(apiKey.Group, effectiveMultiplier)
	if !ok {
		return OrganizationEmbeddingsImagesCandidatePricing{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	maximum, err := organizationImagesCostFromTierPrices(prices, policy.OutputImageCount, policy.OutputSize)
	if err != nil {
		return OrganizationEmbeddingsImagesCandidatePricing{}, err
	}
	return OrganizationEmbeddingsImagesCandidatePricing{Model: model, EndpointFamily: OrganizationEndpointFamilyImages, RateMultiplier: multiplier, AccountMultiplier: accountMultiplier, OutputImageCount: policy.OutputImageCount, OutputSize: policy.OutputSize, ImagePricingSource: "group_image_price", ImageTierPrices: prices, MaximumCost: maximum}, nil
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func organizationExplicitGroupImageTierPrices(group *Group, multiplier float64) (map[string]string, bool) {
	if group == nil || multiplier < 0 {
		return nil, false
	}
	values := map[string]*float64{ImageBillingSize1K: group.ImagePrice1K, ImageBillingSize2K: group.ImagePrice2K, ImageBillingSize4K: group.ImagePrice4K}
	prices := make(map[string]string, len(values))
	for tier, value := range values {
		if value == nil || !finitePositive(*value) {
			continue
		}
		amount, err := NormalizeOrganizationRawActualCost(decimal.NewFromFloat(*value * multiplier).StringFixed(18))
		if err != nil {
			return nil, false
		}
		prices[tier] = amount
	}
	return prices, len(prices) > 0
}

func organizationImagesCostFromTierPrices(prices map[string]string, count int, tier string) (string, error) {
	unit, ok := prices[tier]
	if !ok || count <= 0 {
		return "", ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	decimalUnit, err := decimal.NewFromString(unit)
	if err != nil || !decimalUnit.IsPositive() {
		return "", ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	return NormalizeOrganizationMoney(decimalUnit.Mul(decimal.NewFromInt(int64(count))).RoundCeil(8).StringFixed(8), true)
}

func (s *OpenAIGatewayService) RevalidateOrganizationEmbeddingsImagesCandidate(ctx context.Context, apiKey *APIKey, candidate OrganizationEmbeddingsImagesCandidate) (*OrganizationEmbeddingsImagesCandidate, error) {
	if s == nil || s.accountRepo == nil || apiKey == nil || apiKey.GroupID == nil || candidate.AccountID <= 0 {
		return nil, ErrOrganizationEmbeddingsImagesCandidateConflict
	}
	account, err := s.accountRepo.GetByID(ctx, candidate.AccountID)
	if err != nil || account == nil || !openAIStickyAccountMatchesGroup(account, apiKey.GroupID) || account.IsOpenAIPassthroughEnabled() {
		return nil, ErrOrganizationEmbeddingsImagesCandidateConflict
	}
	capability := OpenAIEndpointCapabilityEmbeddings
	if candidate.EndpointFamily == OrganizationEndpointFamilyImages && candidate.ParsedImages != nil {
		capability = ""
	}
	account = s.resolveFreshSchedulableOpenAIAccount(ctx, account, candidate.RequestedModel, false, capability)
	if account == nil || account.ID != candidate.AccountID || account.Type != candidate.AccountType {
		return nil, ErrOrganizationEmbeddingsImagesCandidateConflict
	}
	if candidate.EndpointFamily == OrganizationEndpointFamilyImages && (candidate.ParsedImages == nil || !accountSupportsOpenAICapabilities(account, capability, candidate.ParsedImages.RequiredCapability)) {
		return nil, ErrOrganizationEmbeddingsImagesCandidateConflict
	}
	if candidate.EndpointFamily == OrganizationEndpointFamilyEmbeddings && account.Type != AccountTypeAPIKey {
		return nil, ErrOrganizationEmbeddingsImagesCandidateConflict
	}
	channelMapping, _ := s.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, candidate.RequestedModel)
	channelMappedModel := strings.TrimSpace(channelMapping.MappedModel)
	if channelMappedModel == "" {
		channelMappedModel = candidate.RequestedModel
	}
	upstreamModel := channelMappedModel
	if account.Type == AccountTypeAPIKey {
		upstreamModel = account.GetMappedModel(channelMappedModel)
	}
	expectedForwardBody := candidate.PlanningBody
	if candidate.EndpointFamily == OrganizationEndpointFamilyEmbeddings {
		expectedForwardBody = s.ReplaceModelInBody(candidate.PlanningBody, channelMappedModel)
	}
	if upstreamModel != candidate.UpstreamModel || channelMappedModel != candidate.ChannelMappedModel ||
		!bytes.Equal(expectedForwardBody, candidate.ForwardBody) || organizationEmbeddingsImagesBodyHash(candidate.ForwardBody) != candidate.ForwardBodyHash {
		return nil, ErrOrganizationEmbeddingsImagesCandidateConflict
	}
	if candidate.EndpointFamily == OrganizationEndpointFamilyEmbeddings {
		if _, ok := verifiedOrganizationEmbeddingModelLimit(upstreamModel); !ok {
			return nil, ErrOrganizationEmbeddingsImagesCandidateConflict
		}
	} else if !isOpenAIImageGenerationModel(upstreamModel) {
		return nil, ErrOrganizationEmbeddingsImagesCandidateConflict
	}
	if s.needsUpstreamChannelRestrictionCheck(ctx, apiKey.GroupID) && s.channelService != nil && s.channelService.IsModelRestricted(ctx, *apiKey.GroupID, upstreamModel) {
		return nil, ErrOrganizationEmbeddingsImagesCandidateConflict
	}
	multiplier := organizationEmbeddingsImagesRateMultiplier(ctx, s, apiKey)
	pricing, err := s.buildOrganizationEmbeddingsImagesCandidatePricing(ctx, apiKey, candidate.EndpointFamily, candidate.embeddingsPolicy(), candidate.imagesPolicy(), upstreamModel, multiplier, account.BillingRateMultiplier())
	if err != nil || !organizationEmbeddingsImagesPricingEqual(pricing, candidate.Pricing) {
		return nil, ErrOrganizationEmbeddingsImagesCandidateConflict
	}
	validated := candidate
	validated.Account = account
	validated.Pricing = pricing
	return &validated, nil
}

func (c OrganizationEmbeddingsImagesCandidate) embeddingsPolicy() *OrganizationEmbeddingsRequestPolicy {
	if c.EndpointFamily != OrganizationEndpointFamilyEmbeddings || c.Pricing.InputTokenCeiling <= 0 {
		return nil
	}
	maximum, ok := verifiedOrganizationEmbeddingModelLimit(c.UpstreamModel)
	if !ok {
		return nil
	}
	return &OrganizationEmbeddingsRequestPolicy{RequestedModel: c.RequestedModel, InputTokenCeiling: c.Pricing.InputTokenCeiling, MaximumInputTokens: maximum}
}

func (c OrganizationEmbeddingsImagesCandidate) imagesPolicy() *OrganizationImagesRequestPolicy {
	if c.EndpointFamily != OrganizationEndpointFamilyImages || c.ParsedImages == nil {
		return nil
	}
	policy, err := BuildOrganizationImagesRequestPolicy(c.ParsedImages)
	if err != nil {
		return nil
	}
	return &policy
}

func organizationEmbeddingsImagesPricingEqual(left, right OrganizationEmbeddingsImagesCandidatePricing) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func (s *OpenAIGatewayService) CalculateOrganizationEmbeddingsActualCost(candidate OrganizationEmbeddingsImagesCandidate, result *OpenAIForwardResult) (string, UsageTokens, error) {
	if s == nil || s.billingService == nil || result == nil || !result.UsageObserved || candidate.EndpointFamily != OrganizationEndpointFamilyEmbeddings || candidate.Pricing.Resolved == nil {
		return "", UsageTokens{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	inputTokens := result.Usage.InputTokens - result.Usage.CacheReadInputTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	tokens := UsageTokens{InputTokens: inputTokens, CacheCreationTokens: result.Usage.CacheCreationInputTokens, CacheReadTokens: result.Usage.CacheReadInputTokens}
	breakdown, err := s.billingService.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: candidate.Pricing.Model, Tokens: tokens, RateMultiplier: candidate.Pricing.RateMultiplier * candidate.Pricing.AccountMultiplier, Resolver: &ModelPricingResolver{}, Resolved: candidate.Pricing.Resolved})
	if err != nil || breakdown == nil || math.IsNaN(breakdown.ActualCost) || math.IsInf(breakdown.ActualCost, 0) || breakdown.ActualCost < 0 {
		return "", UsageTokens{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	cost, err := NormalizeOrganizationRawActualCost(fmt.Sprintf("%.18f", breakdown.ActualCost))
	return cost, tokens, err
}

func (s *OpenAIGatewayService) CalculateOrganizationImagesActualCost(candidate OrganizationEmbeddingsImagesCandidate, result *OpenAIForwardResult) (string, error) {
	if s == nil || s.billingService == nil || result == nil || candidate.EndpointFamily != OrganizationEndpointFamilyImages || result.ImageCount <= 0 {
		return "", ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	if result.ImageCount > candidate.Pricing.OutputImageCount {
		return "", ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	outputTier := candidate.Pricing.OutputSize
	if len(result.ImageOutputSizes) > 0 {
		for _, outputSize := range result.ImageOutputSizes {
			tier, ok := ClassifyImageBillingTier(outputSize)
			if !ok || imageTierRank(tier) > imageTierRank(candidate.Pricing.OutputSize) {
				return "", ErrOrganizationEmbeddingsImagesUnsupportedRequest
			}
			if imageTierRank(tier) > imageTierRank(outputTier) {
				outputTier = tier
			}
		}
	}
	if candidate.Pricing.ImagePricingSource == PricingSourceChannel && candidate.Pricing.Resolved != nil {
		breakdown, err := s.billingService.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: candidate.Pricing.Model, RequestCount: result.ImageCount, SizeTier: outputTier, RateMultiplier: candidate.Pricing.RateMultiplier * candidate.Pricing.AccountMultiplier, Resolver: &ModelPricingResolver{}, Resolved: candidate.Pricing.Resolved})
		if err != nil || breakdown == nil || !finitePositive(breakdown.ActualCost) {
			return "", ErrOrganizationEmbeddingsImagesUnsupportedRequest
		}
		return NormalizeOrganizationRawActualCost(fmt.Sprintf("%.18f", breakdown.ActualCost))
	}
	return organizationImagesCostFromTierPrices(candidate.Pricing.ImageTierPrices, result.ImageCount, outputTier)
}

func (p OrganizationEmbeddingsImagesCandidatePlan) NewExecutionReservationInput(apiKey *APIKey, clientRequestID string) (OrganizationExecutionReservationInput, error) {
	attribution, err := organizationEmbeddingsImagesExecutionAttribution(apiKey)
	if err != nil {
		return OrganizationExecutionReservationInput{}, err
	}
	return OrganizationExecutionReservationInput{Attribution: attribution, EndpointFamily: p.EndpointFamily, RequestShape: p.RequestShape, Model: p.Model, ClientRequestID: clientRequestID, CandidateSnapshot: p.CandidateSnapshot, PricingSnapshot: p.PricingSnapshot, CandidateRouteMaximumCost: p.CandidateRouteMaximumCost, BillingFingerprint: p.BillingFingerprint}, nil
}

func NewOrganizationEmbeddingsImagesPreDispatchRejectionInput(apiKey *APIKey, body []byte, family, shape, model, clientRequestID string, rejection error) (OrganizationExecutionReservationInput, error) {
	attribution, err := organizationEmbeddingsImagesExecutionAttribution(apiKey)
	if err != nil {
		return OrganizationExecutionReservationInput{}, err
	}
	reason := strings.ToLower(strings.TrimSpace(infraerrors.Reason(rejection)))
	if reason == "" {
		reason = "organization_embeddings_images_pre_dispatch_rejected"
	}
	proof := "request_policy_rejected:" + reason
	candidateSnapshot, err := json.Marshal(map[string]any{"endpoint_family": family, "request_shape": shape, "requested_model": strings.TrimSpace(model), "candidates": []any{}, "maximum_attempts": 0, "zero_proof": proof})
	if err != nil {
		return OrganizationExecutionReservationInput{}, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	pricingSnapshot, err := json.Marshal(map[string]any{"rejected": true, "reason": reason, "maximum_cost": "0.00000000"})
	if err != nil {
		return OrganizationExecutionReservationInput{}, ErrOrganizationExecutionInvariant.WithCause(err)
	}
	digest := sha256.Sum256(append(append(append([]byte(nil), body...), candidateSnapshot...), pricingSnapshot...))
	return OrganizationExecutionReservationInput{Attribution: attribution, EndpointFamily: family, RequestShape: shape, Model: strings.TrimSpace(model), ClientRequestID: clientRequestID, CandidateSnapshot: candidateSnapshot, PricingSnapshot: pricingSnapshot, CandidateRouteMaximumCost: "0.00000000", BillingFingerprint: hex.EncodeToString(digest[:]), PreDispatchZeroProof: proof}, nil
}

func organizationEmbeddingsImagesExecutionAttribution(apiKey *APIKey) (OrganizationExecutionTrustedAttribution, error) {
	if apiKey == nil || apiKey.OrganizationAuthorization == nil || apiKey.User == nil || apiKey.OrganizationID == nil || apiKey.GroupID == nil {
		return OrganizationExecutionTrustedAttribution{}, ErrOrganizationEmbeddingsImagesUnsupportedRequest
	}
	auth := apiKey.OrganizationAuthorization
	attribution := OrganizationExecutionTrustedAttribution{OrganizationID: auth.OrganizationID, OrganizationStatus: auth.OrganizationStatus, OrganizationBalanceVersionSnapshot: auth.OrganizationBalanceVersion, UserID: apiKey.User.ID, UserOrganizationID: dereferenceInt64(apiKey.User.OrganizationID), APIKeyID: apiKey.ID, APIKeyUserID: apiKey.UserID, APIKeyOrganizationID: dereferenceInt64(apiKey.OrganizationID), AllocationID: auth.AllocationID, AllocationUserID: auth.AllocationUserID, AllocationOrganizationID: auth.AllocationOrganizationID, AllocationStatus: auth.AllocationStatus, AllocationVersionSnapshot: auth.AllocationVersion}
	if err := ValidateOrganizationExecutionTrustedAttribution(attribution); err != nil {
		return OrganizationExecutionTrustedAttribution{}, err
	}
	return attribution, nil
}

func organizationEmbeddingsImagesBodyHash(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func organizationEmbeddingsImagesRoute(family, accountType string) string {
	if family == OrganizationEndpointFamilyImages && accountType == AccountTypeOAuth {
		return "codex_http_images"
	}
	return "openai_http_" + family
}
