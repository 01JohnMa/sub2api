package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestOrganizationSpendingCapabilityRegistryDefaultsEveryKnownShapeToDisabled(t *testing.T) {
	registry := NewOrganizationSpendingCapabilityRegistry(&config.Config{})

	for _, capability := range registry.Capabilities() {
		if capability.Family == OrganizationEndpointFamilyResponses &&
			(capability.Shape == OrganizationRequestShapeHTTP || capability.Shape == OrganizationRequestShapeStreaming) {
			require.True(t, capability.CostCeilingSupported)
		} else {
			require.False(t, capability.CostCeilingSupported)
		}
		require.False(t, capability.RolloutEnabled)
		err := registry.Require(capability.Family, capability.Shape)
		require.Error(t, err, "%s/%s", capability.Family, capability.Shape)
		require.True(t, infraerrors.IsForbidden(err))
		require.Equal(t, "ORGANIZATION_SPENDING_DISABLED", infraerrors.Reason(err))
	}
}

func TestOrganizationSpendingCapabilityRegistryCannotEnableUnverifiedCeilingFromConfig(t *testing.T) {
	cfg := &config.Config{Log: config.LogConfig{Environment: "development"}, Gateway: config.GatewayConfig{OrganizationSpending: config.OrganizationSpendingConfig{
		Enabled: true,
		Capabilities: map[string]map[string]bool{
			OrganizationEndpointFamilyMessages: {OrganizationRequestShapeHTTP: true},
		},
	}}}
	registry := NewOrganizationSpendingCapabilityRegistry(cfg)

	var configured OrganizationSpendingCapability
	for _, capability := range registry.Capabilities() {
		if capability.Family == OrganizationEndpointFamilyMessages && capability.Shape == OrganizationRequestShapeHTTP {
			configured = capability
			break
		}
	}
	require.True(t, configured.RolloutEnabled)
	require.False(t, configured.CostCeilingSupported)

	err := registry.Require(OrganizationEndpointFamilyMessages, OrganizationRequestShapeHTTP)
	require.Error(t, err)
	require.Equal(t, "ORGANIZATION_SPENDING_DISABLED", infraerrors.Reason(err))
}

func TestOrganizationSpendingCapabilityRegistryEnablesOnlyVerifiedResponsesHTTPInNonProduction(t *testing.T) {
	cfg := &config.Config{Log: config.LogConfig{Environment: "staging"}, Gateway: config.GatewayConfig{OrganizationSpending: config.OrganizationSpendingConfig{
		Enabled: true,
		Capabilities: map[string]map[string]bool{
			OrganizationEndpointFamilyResponses: {
				OrganizationRequestShapeHTTP:      true,
				OrganizationRequestShapeStreaming: true,
			},
		},
	}}}
	registry := NewOrganizationSpendingCapabilityRegistry(cfg)

	require.NoError(t, registry.Require(OrganizationEndpointFamilyResponses, OrganizationRequestShapeHTTP))
	require.NoError(t, registry.Require(OrganizationEndpointFamilyResponses, OrganizationRequestShapeStreaming))
	err := registry.Require(OrganizationEndpointFamilyResponses, OrganizationRequestShapeWebSocketResponseCreate)
	require.Error(t, err)
	require.Equal(t, "ORGANIZATION_SPENDING_DISABLED", infraerrors.Reason(err))
}

func TestOrganizationSpendingCapabilityRegistryProductionRemainsDisabledEvenWhenConfigured(t *testing.T) {
	cfg := &config.Config{Log: config.LogConfig{Environment: "production"}, Gateway: config.GatewayConfig{OrganizationSpending: config.OrganizationSpendingConfig{
		Enabled: true,
		Capabilities: map[string]map[string]bool{
			OrganizationEndpointFamilyResponses: {
				OrganizationRequestShapeHTTP:      true,
				OrganizationRequestShapeStreaming: true,
			},
		},
	}}}
	registry := NewOrganizationSpendingCapabilityRegistry(cfg)

	for _, shape := range []string{OrganizationRequestShapeHTTP, OrganizationRequestShapeStreaming} {
		err := registry.Require(OrganizationEndpointFamilyResponses, shape)
		require.Error(t, err)
		require.Equal(t, "ORGANIZATION_SPENDING_DISABLED", infraerrors.Reason(err))
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
