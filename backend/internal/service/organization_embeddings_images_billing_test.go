package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildOrganizationEmbeddingsRequestPolicyBoundsBatchAndInput(t *testing.T) {
	policy, err := BuildOrganizationEmbeddingsRequestPolicy([]byte(`{"model":"text-embedding-3-small","input":["first","second"]}`))
	require.NoError(t, err)
	require.Equal(t, "text-embedding-3-small", policy.RequestedModel)
	require.Equal(t, 2, policy.InputCount)
	require.Positive(t, policy.InputTokenCeiling)

	_, err = BuildOrganizationEmbeddingsRequestPolicy([]byte(`{"model":"text-embedding-3-small","input":[[1,2]]}`))
	require.ErrorIs(t, err, ErrOrganizationEmbeddingsImagesUnsupportedRequest)
}

func TestOrganizationEmbeddingsInputCountAndTokenCeilingBoundaries(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		count int
		valid bool
	}{
		{name: "maximum batch", count: organizationEmbeddingsMaximumBatchInputs, valid: true},
		{name: "batch above maximum", count: organizationEmbeddingsMaximumBatchInputs + 1, valid: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			input := "[" + strings.TrimSuffix(strings.Repeat(`"x",`, testCase.count), ",") + "]"
			count, valid := organizationEmbeddingsInputCount(gjson.Parse(input))
			require.True(t, valid)
			require.Equal(t, testCase.count, count)
			_, err := BuildOrganizationEmbeddingsRequestPolicy([]byte(`{"model":"text-embedding-3-small","input":` + input + `}`))
			if testCase.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrOrganizationEmbeddingsImagesUnsupportedRequest)
			}
		})
	}

	prefix := `{"model":"text-embedding-3-small","input":"`
	suffix := `"}`
	maximumInputLength := organizationEmbeddingsMaximumInputBytes - organizationEmbeddingsInputOverhead
	accepted := []byte(prefix + strings.Repeat("x", maximumInputLength) + suffix)
	policy, err := BuildOrganizationEmbeddingsRequestPolicy(accepted)
	require.NoError(t, err)
	require.Equal(t, organizationEmbeddingsMaximumInputBytes, policy.InputTokenCeiling)

	rejected := []byte(prefix + strings.Repeat("x", maximumInputLength+1) + suffix)
	_, err = BuildOrganizationEmbeddingsRequestPolicy(rejected)
	require.ErrorIs(t, err, ErrOrganizationEmbeddingsImagesUnsupportedRequest)
}

func TestBuildOrganizationImagesRequestPolicyRejectsUnknownOutputBound(t *testing.T) {
	parsed := &OpenAIImagesRequest{Endpoint: openAIImagesGenerationsEndpoint, Model: "gpt-image-2", N: 1, Size: "unbounded", ExplicitSize: true}
	_, err := BuildOrganizationImagesRequestPolicy(parsed)
	require.ErrorIs(t, err, ErrOrganizationEmbeddingsImagesUnsupportedRequest)
}

func TestBuildOrganizationImagesRequestPolicyCapturesEditInputDimensions(t *testing.T) {
	parsed := &OpenAIImagesRequest{
		Endpoint:     openAIImagesEditsEndpoint,
		Model:        "gpt-image-2",
		N:            2,
		Size:         "1024x1024",
		ExplicitSize: true,
		Multipart:    true,
		Uploads: []OpenAIImagesUpload{{
			FileName: "source.png",
			Data:     minimalPNG(640, 480),
		}},
	}
	policy, err := BuildOrganizationImagesRequestPolicy(parsed)
	require.NoError(t, err)
	require.Equal(t, OrganizationRequestShapeEdits, policy.RequestShape)
	require.Equal(t, 2, policy.OutputImageCount)
	require.Equal(t, []string{"640x480"}, policy.InputImageSizes)
}

func TestBuildOrganizationImagesRequestPolicyBoundsOutputAndEditInputs(t *testing.T) {
	validGeneration := &OpenAIImagesRequest{Endpoint: openAIImagesGenerationsEndpoint, Model: "gpt-image-2", N: organizationImagesMaximumOutputCount, Size: "1024x1024", ExplicitSize: true}
	_, err := BuildOrganizationImagesRequestPolicy(validGeneration)
	require.NoError(t, err)

	overOutputCount := *validGeneration
	overOutputCount.N++
	_, err = BuildOrganizationImagesRequestPolicy(&overOutputCount)
	require.ErrorIs(t, err, ErrOrganizationEmbeddingsImagesUnsupportedRequest)

	validEdit := &OpenAIImagesRequest{Endpoint: openAIImagesEditsEndpoint, Model: "gpt-image-2", N: 1, Size: "1024x1024", ExplicitSize: true, Multipart: true}
	for index := 0; index < organizationImagesMaximumInputCount; index++ {
		validEdit.Uploads = append(validEdit.Uploads, OpenAIImagesUpload{Width: 1, Height: 1})
	}
	policy, err := BuildOrganizationImagesRequestPolicy(validEdit)
	require.NoError(t, err)
	require.Len(t, policy.InputImageSizes, organizationImagesMaximumInputCount)

	overInputCount := *validEdit
	overInputCount.Uploads = append(append([]OpenAIImagesUpload(nil), validEdit.Uploads...), OpenAIImagesUpload{Width: 1, Height: 1})
	_, err = BuildOrganizationImagesRequestPolicy(&overInputCount)
	require.ErrorIs(t, err, ErrOrganizationEmbeddingsImagesUnsupportedRequest)
}

func TestPlanOrganizationImagesCandidatesFreezesExplicitGroupPriceAndOutputCeiling(t *testing.T) {
	groupID := int64(42)
	imagePrice2K := 0.25
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	svc := &OpenAIGatewayService{
		accountRepo: &stubOpenAIAccountRepo{accounts: []Account{{
			ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
			Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
		}}},
		cfg:            cfg,
		billingService: NewBillingService(cfg, nil),
	}
	parsed := &OpenAIImagesRequest{
		Endpoint: openAIImagesGenerationsEndpoint, Model: "gpt-image-2", N: 2,
		Size: "2048x2048", ExplicitSize: true,
	}
	apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 1, ImagePrice2K: &imagePrice2K}}
	plan, err := svc.PlanOrganizationImagesCandidates(context.Background(), OrganizationEmbeddingsImagesCandidatePlanInput{APIKey: apiKey, Body: []byte(`{"model":"gpt-image-2","n":2,"size":"2048x2048"}`), ParsedImages: parsed, MaximumAttempts: 1})
	require.NoError(t, err)
	require.Len(t, plan.Candidates, 1)
	require.Equal(t, "0.50000000", plan.CandidateRouteMaximumCost)
	require.Equal(t, "group_image_price", plan.Candidates[0].Pricing.ImagePricingSource)
	require.NotContains(t, string(plan.CandidateSnapshot), "Credentials")

	actual, err := svc.CalculateOrganizationImagesActualCost(plan.Candidates[0], &OpenAIForwardResult{ImageCount: 1, ImageOutputSizes: []string{"2048x2048"}})
	require.NoError(t, err)
	require.Equal(t, "0.25000000", actual)

	_, err = svc.RevalidateOrganizationEmbeddingsImagesCandidate(context.Background(), apiKey, plan.Candidates[0])
	require.NoError(t, err)

	bodyDrift := plan.Candidates[0]
	bodyDrift.ForwardBody = []byte(`{"model":"gpt-image-2","n":1,"size":"2048x2048"}`)
	_, err = svc.RevalidateOrganizationEmbeddingsImagesCandidate(context.Background(), apiKey, bodyDrift)
	require.ErrorIs(t, err, ErrOrganizationEmbeddingsImagesCandidateConflict)

	priceDrift := plan.Candidates[0]
	priceDrift.Pricing.MaximumCost = "0.00000000"
	_, err = svc.RevalidateOrganizationEmbeddingsImagesCandidate(context.Background(), apiKey, priceDrift)
	require.ErrorIs(t, err, ErrOrganizationEmbeddingsImagesCandidateConflict)
}

func TestCalculateOrganizationImagesActualCostRejectsOutputAboveFrozenCeiling(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	svc := &OpenAIGatewayService{billingService: NewBillingService(cfg, nil)}
	candidate := OrganizationEmbeddingsImagesCandidate{
		EndpointFamily: OrganizationEndpointFamilyImages,
		Pricing: OrganizationEmbeddingsImagesCandidatePricing{
			EndpointFamily: OrganizationEndpointFamilyImages, OutputImageCount: 1, OutputSize: ImageBillingSize2K,
			ImagePricingSource: "group_image_price", ImageTierPrices: map[string]string{ImageBillingSize2K: "0.250000000000000000"},
		},
	}
	_, err := svc.CalculateOrganizationImagesActualCost(candidate, &OpenAIForwardResult{ImageCount: 1, ImageOutputSizes: []string{"3840x2160"}})
	require.ErrorIs(t, err, ErrOrganizationEmbeddingsImagesUnsupportedRequest)
}

func minimalPNG(width, height int) []byte {
	imageData := image.NewRGBA(image.Rect(0, 0, width, height))
	imageData.Set(0, 0, color.RGBA{A: 0xff})
	var buffer bytes.Buffer
	_ = png.Encode(&buffer, imageData)
	return buffer.Bytes()
}
