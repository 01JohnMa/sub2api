package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestOrganizationNonBillableCapabilityRequiresExplicitNonProductionGate(t *testing.T) {
	cfg := &config.Config{Log: config.LogConfig{Environment: "development"}}
	cfg.Gateway.OrganizationSpending.Enabled = true
	cfg.Gateway.OrganizationSpending.Capabilities = map[string]map[string]bool{
		OrganizationEndpointFamilyModels:        {OrganizationRequestShapeHTTP: true},
		OrganizationEndpointFamilyUsage:         {OrganizationRequestShapeHTTP: true},
		OrganizationEndpointFamilyTokenCounting: {OrganizationRequestShapeProbe: true},
	}
	registry := NewOrganizationSpendingCapabilityRegistry(cfg)
	require.NoError(t, registry.RequireNonBillable(OrganizationEndpointFamilyModels, OrganizationRequestShapeHTTP))
	require.NoError(t, registry.RequireNonBillable(OrganizationEndpointFamilyUsage, OrganizationRequestShapeHTTP))
	require.NoError(t, registry.RequireNonBillable(OrganizationEndpointFamilyTokenCounting, OrganizationRequestShapeProbe))
	require.Error(t, registry.Require(OrganizationEndpointFamilyTokenCounting, OrganizationRequestShapeProbe))

	cfg.Log.Environment = "production"
	registry = NewOrganizationSpendingCapabilityRegistry(cfg)
	require.Error(t, registry.RequireNonBillable(OrganizationEndpointFamilyModels, OrganizationRequestShapeHTTP))
}

func TestOrganizationNonBillableCapabilitiesAreEnumeratedWithoutDuplicateProbe(t *testing.T) {
	cfg := &config.Config{Log: config.LogConfig{Environment: "test"}}
	cfg.Gateway.OrganizationSpending.Enabled = true
	cfg.Gateway.OrganizationSpending.Capabilities = map[string]map[string]bool{
		OrganizationEndpointFamilyModels:        {OrganizationRequestShapeHTTP: true},
		OrganizationEndpointFamilyUsage:         {OrganizationRequestShapeHTTP: true},
		OrganizationEndpointFamilyTokenCounting: {OrganizationRequestShapeProbe: true},
	}
	registry := NewOrganizationSpendingCapabilityRegistry(cfg)
	counts := map[string]int{}
	for _, capability := range registry.Capabilities() {
		key := capability.Family + "\x00" + capability.Shape
		counts[key]++
		if capability.Family == OrganizationEndpointFamilyModels || capability.Family == OrganizationEndpointFamilyUsage {
			require.False(t, capability.CostCeilingSupported)
			require.True(t, capability.RolloutEnabled)
		}
	}
	require.Equal(t, 1, counts[OrganizationEndpointFamilyModels+"\x00"+OrganizationRequestShapeHTTP])
	require.Equal(t, 1, counts[OrganizationEndpointFamilyUsage+"\x00"+OrganizationRequestShapeHTTP])
	require.Equal(t, 1, counts[OrganizationEndpointFamilyTokenCounting+"\x00"+OrganizationRequestShapeProbe])
}

func TestCheckOrganizationNonBillablePlatformEligibilityAllowsZeroAllocation(t *testing.T) {
	organizationID := int64(41)
	role := OrganizationRoleMember
	user := &User{ID: 11, Status: StatusActive, OrganizationID: &organizationID, OrganizationRole: &role}
	apiKey := &APIKey{
		ID: 21, UserID: user.ID, Status: StatusAPIKeyActive, OrganizationID: &organizationID, User: user,
		OrganizationAuthorization: &OrganizationKeyAuthorization{
			APIKeyID: 21, UserID: user.ID, OrganizationID: organizationID, OrganizationStatus: OrganizationStatusActive,
			AllocationID: 31, AllocationUserID: user.ID, AllocationOrganizationID: organizationID,
			AllocationStatus: domain.OrganizationAllocationStatusActive, Available: "10.00000000", Reserved: "0.00000000",
		},
	}
	apiKey.OrganizationAuthorization.Available = "0.00000000"
	svc := &BillingCacheService{cfg: &config.Config{RunMode: config.RunModeStandard}}

	require.NoError(t, svc.CheckOrganizationNonBillablePlatformEligibility(context.Background(), user, apiKey, nil, ""))
}
