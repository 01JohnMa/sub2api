package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGatewayBillingTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c
}

func gatewayOrganizationAdmissionFixture() (*service.APIKey, *service.OrganizationKeyAuthorization) {
	organizationID := int64(41)
	role := service.OrganizationRoleMember
	user := &service.User{
		ID:               11,
		Status:           service.StatusActive,
		OrganizationID:   &organizationID,
		OrganizationRole: &role,
	}
	authorization := &service.OrganizationKeyAuthorization{
		APIKeyID:                 21,
		UserID:                   user.ID,
		OrganizationID:           organizationID,
		OrganizationStatus:       service.OrganizationStatusActive,
		AllocationID:             31,
		AllocationUserID:         user.ID,
		AllocationOrganizationID: organizationID,
		AllocationStatus:         domain.OrganizationAllocationStatusActive,
		Available:                "10.00000000",
		Reserved:                 "0.00000000",
	}
	return &service.APIKey{
		ID:                        authorization.APIKeyID,
		UserID:                    user.ID,
		Status:                    service.StatusAPIKeyActive,
		OrganizationID:            &organizationID,
		OrganizationAuthorization: authorization,
		User:                      user,
	}, authorization
}

func TestCheckGatewayBillingEligibilityKeepsPersonalPath(t *testing.T) {
	svc := service.NewBillingCacheService(
		nil, nil, nil, nil, nil, nil,
		&config.Config{RunMode: config.RunModeSimple}, nil,
	)
	t.Cleanup(svc.Stop)
	apiKey := &service.APIKey{User: &service.User{ID: 11}}

	err := checkGatewayBillingEligibility(context.Background(), newGatewayBillingTestContext(), svc, apiKey, nil, "anthropic")
	require.NoError(t, err)
}

func TestCheckGatewayBillingEligibilityRequiresTrustedOrganizationContext(t *testing.T) {
	svc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	apiKey, _ := gatewayOrganizationAdmissionFixture()

	err := checkGatewayBillingEligibility(context.Background(), newGatewayBillingTestContext(), svc, apiKey, nil, "")
	require.ErrorIs(t, err, service.ErrOrganizationBillingUnavailable)
}

func TestCheckGatewayBillingEligibilityReportsOrganizationDependencyFailure(t *testing.T) {
	apiKey, _ := gatewayOrganizationAdmissionFixture()
	err := checkGatewayBillingEligibility(context.Background(), newGatewayBillingTestContext(), nil, apiKey, nil, "")
	require.ErrorIs(t, err, service.ErrOrganizationBillingUnavailable)
}

func TestCheckGatewayBillingEligibilityUsesOrganizationPath(t *testing.T) {
	svc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	apiKey, authorization := gatewayOrganizationAdmissionFixture()
	c := newGatewayBillingTestContext()
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), authorization)

	err := checkGatewayBillingEligibility(context.Background(), c, svc, apiKey, nil, "")
	require.NoError(t, err)
}

func TestCheckGatewayBillingEligibilityRejectsMismatchedTrustedContext(t *testing.T) {
	svc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	apiKey, authorization := gatewayOrganizationAdmissionFixture()
	copyAuthorization := *authorization
	copyAuthorization.AllocationID++
	c := newGatewayBillingTestContext()
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), &copyAuthorization)

	err := checkGatewayBillingEligibility(context.Background(), c, svc, apiKey, nil, "")
	require.ErrorIs(t, err, service.ErrOrganizationBillingUnavailable)
}
