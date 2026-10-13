package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestOrganizationSpendingCapabilityRegistryDefaultsEveryKnownShapeToDisabled(t *testing.T) {
	registry := NewOrganizationSpendingCapabilityRegistry(&config.Config{})
	verified := map[string]map[string]bool{
		OrganizationEndpointFamilyMessages: {
			OrganizationRequestShapeHTTP:      true,
			OrganizationRequestShapeStreaming: true,
		},
		OrganizationEndpointFamilyResponses: {
			OrganizationRequestShapeHTTP:                    true,
			OrganizationRequestShapeStreaming:               true,
			OrganizationRequestShapeWebSocketResponseCreate: true,
		},
		OrganizationEndpointFamilyChat: {
			OrganizationRequestShapeHTTP:      true,
			OrganizationRequestShapeStreaming: true,
		},
		OrganizationEndpointFamilyEmbeddings: {OrganizationRequestShapeHTTP: true},
		OrganizationEndpointFamilyImages: {
			OrganizationRequestShapeGenerations: true,
			OrganizationRequestShapeEdits:       true,
		},
		OrganizationEndpointFamilyGemini:      {OrganizationRequestShapeModelsAction: true},
		OrganizationEndpointFamilyAntigravity: {OrganizationRequestShapeHTTP: true, OrganizationRequestShapeStreaming: true},
	}

	for _, capability := range registry.Capabilities() {
		require.Equal(t, verified[capability.Family][capability.Shape], capability.CostCeilingSupported, "%s/%s", capability.Family, capability.Shape)
		require.False(t, capability.RolloutEnabled)
		err := registry.Require(capability.Family, capability.Shape)
		require.Error(t, err, "%s/%s", capability.Family, capability.Shape)
		require.True(t, infraerrors.IsForbidden(err))
		require.Equal(t, "ORGANIZATION_SPENDING_DISABLED", infraerrors.Reason(err))
	}
}

func TestOrganizationSpendingCapabilityRegistryEnablesVerifiedAdapterWaveInNonProduction(t *testing.T) {
	configured := map[string]map[string]bool{
		OrganizationEndpointFamilyMessages:    {OrganizationRequestShapeHTTP: true, OrganizationRequestShapeStreaming: true},
		OrganizationEndpointFamilyChat:        {OrganizationRequestShapeHTTP: true, OrganizationRequestShapeStreaming: true},
		OrganizationEndpointFamilyEmbeddings:  {OrganizationRequestShapeHTTP: true},
		OrganizationEndpointFamilyImages:      {OrganizationRequestShapeGenerations: true, OrganizationRequestShapeEdits: true},
		OrganizationEndpointFamilyGemini:      {OrganizationRequestShapeModelsAction: true},
		OrganizationEndpointFamilyAntigravity: {OrganizationRequestShapeHTTP: true, OrganizationRequestShapeStreaming: true},
	}
	cfg := &config.Config{Log: config.LogConfig{Environment: "test"}, Gateway: config.GatewayConfig{OrganizationSpending: config.OrganizationSpendingConfig{
		Enabled: true, Capabilities: configured,
	}}}
	registry := NewOrganizationSpendingCapabilityRegistry(cfg)

	for family, shapes := range configured {
		for shape := range shapes {
			require.NoError(t, registry.Require(family, shape), "%s/%s", family, shape)
		}
	}
}

func TestOrganizationSpendingCapabilityRegistryEnablesVerifiedMessagesCeilingFromConfig(t *testing.T) {
	cfg := &config.Config{Log: config.LogConfig{Environment: "development"}, Gateway: config.GatewayConfig{OrganizationSpending: config.OrganizationSpendingConfig{
		Enabled: true,
		Capabilities: map[string]map[string]bool{
			OrganizationEndpointFamilyMessages: {
				OrganizationRequestShapeHTTP:      true,
				OrganizationRequestShapeStreaming: true,
			},
		},
	}}}
	registry := NewOrganizationSpendingCapabilityRegistry(cfg)

	configured := map[string]OrganizationSpendingCapability{}
	for _, capability := range registry.Capabilities() {
		if capability.Family == OrganizationEndpointFamilyMessages {
			configured[capability.Shape] = capability
		}
	}
	for _, shape := range []string{OrganizationRequestShapeHTTP, OrganizationRequestShapeStreaming} {
		capability := configured[shape]
		require.True(t, capability.RolloutEnabled, shape)
		require.True(t, capability.CostCeilingSupported, shape)
		require.NoError(t, registry.Require(OrganizationEndpointFamilyMessages, shape), shape)
	}
}

func TestOrganizationSpendingCapabilityRegistryEnablesVerifiedResponsesShapesInNonProduction(t *testing.T) {
	cfg := &config.Config{Log: config.LogConfig{Environment: "staging"}, Gateway: config.GatewayConfig{OrganizationSpending: config.OrganizationSpendingConfig{
		Enabled: true,
		Capabilities: map[string]map[string]bool{
			OrganizationEndpointFamilyResponses: {
				OrganizationRequestShapeHTTP:                    true,
				OrganizationRequestShapeStreaming:               true,
				OrganizationRequestShapeWebSocketResponseCreate: true,
			},
		},
	}}}
	registry := NewOrganizationSpendingCapabilityRegistry(cfg)

	require.NoError(t, registry.Require(OrganizationEndpointFamilyResponses, OrganizationRequestShapeHTTP))
	require.NoError(t, registry.Require(OrganizationEndpointFamilyResponses, OrganizationRequestShapeStreaming))
	require.NoError(t, registry.Require(OrganizationEndpointFamilyResponses, OrganizationRequestShapeWebSocketResponseCreate))
}

func TestOrganizationSpendingCapabilityRegistryProductionRemainsDisabledEvenWhenConfigured(t *testing.T) {
	configured := map[string]map[string]bool{
		OrganizationEndpointFamilyResponses: {
			OrganizationRequestShapeHTTP:      true,
			OrganizationRequestShapeStreaming: true,
		},
		OrganizationEndpointFamilyChat:   {OrganizationRequestShapeHTTP: true},
		OrganizationEndpointFamilyGemini: {OrganizationRequestShapeModelsAction: true},
	}
	cfg := &config.Config{Log: config.LogConfig{Environment: "production"}, Gateway: config.GatewayConfig{OrganizationSpending: config.OrganizationSpendingConfig{
		Enabled:      true,
		Capabilities: configured,
	}}}
	registry := NewOrganizationSpendingCapabilityRegistry(cfg)

	for family, shapes := range configured {
		for shape := range shapes {
			err := registry.Require(family, shape)
			require.Error(t, err)
			require.Equal(t, "ORGANIZATION_SPENDING_DISABLED", infraerrors.Reason(err))
		}
	}
}

func TestOrganizationSpendingCapabilityRegistryRejectsUnknownShapeStably(t *testing.T) {
	registry := NewOrganizationSpendingCapabilityRegistry(&config.Config{Gateway: config.GatewayConfig{OrganizationSpending: config.OrganizationSpendingConfig{Enabled: true}}})

	err := registry.Require("unknown-family", "unknown-shape")

	require.Error(t, err)
	require.True(t, infraerrors.IsBadRequest(err))
	require.Equal(t, "ORGANIZATION_SPENDING_UNSUPPORTED_REQUEST", infraerrors.Reason(err))
}

func TestOrganizationSpendingCapabilityRegistryNilReceiverFailsClosed(t *testing.T) {
	var registry *OrganizationSpendingCapabilityRegistry

	err := registry.Require(OrganizationEndpointFamilyResponses, OrganizationRequestShapeHTTP)

	require.Error(t, err)
	require.Equal(t, "ORGANIZATION_SPENDING_DISABLED", infraerrors.Reason(err))
}
