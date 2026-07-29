package service

import (
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	OrganizationEndpointFamilyMessages      = "messages"
	OrganizationEndpointFamilyResponses     = "responses"
	OrganizationEndpointFamilyChat          = "chat_completions"
	OrganizationEndpointFamilyEmbeddings    = "embeddings"
	OrganizationEndpointFamilyImages        = "images"
	OrganizationEndpointFamilyGemini        = "gemini"
	OrganizationEndpointFamilyAntigravity   = "antigravity"
	OrganizationEndpointFamilyTokenCounting = "count_tokens"

	OrganizationRequestShapeHTTP                    = "http"
	OrganizationRequestShapeStreaming               = "streaming"
	OrganizationRequestShapeWebSocketResponseCreate = "websocket_response_create"
	OrganizationRequestShapeGenerations             = "generations"
	OrganizationRequestShapeEdits                   = "edits"
	OrganizationRequestShapeModelsAction            = "models_action"
	OrganizationRequestShapeProbe                   = "probe"
)

// OrganizationSpendingCapability is a static endpoint/request-shape entry.
// Cost ceilings are introduced by the concrete billing adapters; before then
// every entry remains disabled, even when the global switch is accidentally on.
type OrganizationSpendingCapability struct {
	Family               string
	Shape                string
	CostCeilingSupported bool
	RolloutEnabled       bool
}

type organizationSpendingCapabilityDefinition struct {
	family               string
	shape                string
	costCeilingSupported bool
}

// Keep ceiling readiness in code, next to the adapter implementation. A
// deployment config may opt into a proven shape, but it cannot claim that an
// unverified cost calculator is safe for organization funds.
var organizationSpendingCapabilityDefinitions = []organizationSpendingCapabilityDefinition{
	{family: OrganizationEndpointFamilyMessages, shape: OrganizationRequestShapeHTTP},
	{family: OrganizationEndpointFamilyMessages, shape: OrganizationRequestShapeStreaming},
	{family: OrganizationEndpointFamilyResponses, shape: OrganizationRequestShapeHTTP, costCeilingSupported: true},
	{family: OrganizationEndpointFamilyResponses, shape: OrganizationRequestShapeStreaming, costCeilingSupported: true},
	{family: OrganizationEndpointFamilyResponses, shape: OrganizationRequestShapeWebSocketResponseCreate},
	{family: OrganizationEndpointFamilyChat, shape: OrganizationRequestShapeHTTP},
	{family: OrganizationEndpointFamilyChat, shape: OrganizationRequestShapeStreaming},
	{family: OrganizationEndpointFamilyEmbeddings, shape: OrganizationRequestShapeHTTP},
	{family: OrganizationEndpointFamilyImages, shape: OrganizationRequestShapeGenerations},
	{family: OrganizationEndpointFamilyImages, shape: OrganizationRequestShapeEdits},
	{family: OrganizationEndpointFamilyGemini, shape: OrganizationRequestShapeModelsAction},
	{family: OrganizationEndpointFamilyAntigravity, shape: OrganizationRequestShapeHTTP},
	{family: OrganizationEndpointFamilyAntigravity, shape: OrganizationRequestShapeStreaming},
	{family: OrganizationEndpointFamilyTokenCounting, shape: OrganizationRequestShapeProbe},
}

// OrganizationSpendingCapabilityRegistry centralizes rollout decisions so
// later adapters cannot silently enable an unbounded request shape.
type OrganizationSpendingCapabilityRegistry struct {
	globalEnabled bool
	enabled       map[string]map[string]bool
}

func NewOrganizationSpendingCapabilityRegistry(cfg *config.Config) *OrganizationSpendingCapabilityRegistry {
	registry := &OrganizationSpendingCapabilityRegistry{enabled: map[string]map[string]bool{}}
	if cfg == nil {
		return registry
	}
	environment := strings.ToLower(strings.TrimSpace(cfg.Log.Environment))
	nonProduction := environment != "" && environment != "production" && environment != "prod"
	registry.globalEnabled = cfg.Gateway.OrganizationSpending.Enabled && nonProduction
	for family, shapes := range cfg.Gateway.OrganizationSpending.Capabilities {
		copied := make(map[string]bool, len(shapes))
		for shape, enabled := range shapes {
			copied[shape] = enabled
		}
		registry.enabled[family] = copied
	}
	return registry
}

func (r *OrganizationSpendingCapabilityRegistry) Capabilities() []OrganizationSpendingCapability {
	capabilities := make([]OrganizationSpendingCapability, 0, len(organizationSpendingCapabilityDefinitions))
	for _, definition := range organizationSpendingCapabilityDefinitions {
		rolloutEnabled := false
		if r != nil {
			rolloutEnabled = r.globalEnabled && r.enabled[definition.family][definition.shape]
		}
		capabilities = append(capabilities, OrganizationSpendingCapability{
			Family:               definition.family,
			Shape:                definition.shape,
			CostCeilingSupported: definition.costCeilingSupported,
			RolloutEnabled:       rolloutEnabled,
		})
	}
	sort.Slice(capabilities, func(i, j int) bool {
		if capabilities[i].Family == capabilities[j].Family {
			return capabilities[i].Shape < capabilities[j].Shape
		}
		return capabilities[i].Family < capabilities[j].Family
	})
	return capabilities
}

// Require returns a stable error for disabled and unknown request shapes. Only
// explicitly enabled, cost-bounded adapters may pass organization gateway auth.
func (r *OrganizationSpendingCapabilityRegistry) Require(family, shape string) error {
	if r == nil {
		return infraerrors.Forbidden("ORGANIZATION_SPENDING_DISABLED", "organization spending is not enabled for this endpoint request shape")
	}
	capability, ok := r.capability(family, shape)
	if !ok {
		return infraerrors.BadRequest("ORGANIZATION_SPENDING_UNSUPPORTED_REQUEST", "organization spending does not support this endpoint request shape")
	}
	if !capability.CostCeilingSupported || !capability.RolloutEnabled {
		return infraerrors.Forbidden("ORGANIZATION_SPENDING_DISABLED", "organization spending is not enabled for this endpoint request shape")
	}
	return nil
}

func (r *OrganizationSpendingCapabilityRegistry) capability(family, shape string) (OrganizationSpendingCapability, bool) {
	for _, capability := range r.Capabilities() {
		if capability.Family == family && capability.Shape == shape {
			return capability, true
		}
	}
	return OrganizationSpendingCapability{}, false
}
