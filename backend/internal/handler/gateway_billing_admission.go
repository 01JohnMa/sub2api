package handler

import (
	"context"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// checkGatewayBillingEligibility selects the funding-independent organization
// admission path only from the trusted middleware context. Any organization
// attribution without that context fails closed instead of reaching personal
// balance or subscription checks.
func checkGatewayBillingEligibility(
	ctx context.Context,
	c *gin.Context,
	billing *service.BillingCacheService,
	apiKey *service.APIKey,
	subscription *service.UserSubscription,
	platform string,
) error {
	authorization, hasTrustedOrganization := middleware2.GetOrganizationKeyAuthorizationFromContext(c)
	hasOrganization := hasTrustedOrganization || hasOrganizationBillingAttribution(apiKey)
	if billing == nil {
		if hasOrganization {
			return service.ErrOrganizationBillingUnavailable
		}
		return service.ErrBillingServiceUnavailable
	}
	if apiKey == nil || apiKey.User == nil {
		if hasOrganization {
			return service.ErrOrganizationBillingUnavailable
		}
		return service.ErrBillingServiceUnavailable
	}
	if hasTrustedOrganization {
		if apiKey.OrganizationAuthorization == nil ||
			authorization.APIKeyID != apiKey.ID ||
			*authorization != *apiKey.OrganizationAuthorization {
			return service.ErrOrganizationBillingUnavailable
		}
		return billing.CheckOrganizationPlatformEligibility(ctx, apiKey.User, apiKey, apiKey.Group, platform)
	}
	if hasOrganization {
		return service.ErrOrganizationBillingUnavailable
	}
	return billing.CheckBillingEligibility(ctx, apiKey.User, apiKey, apiKey.Group, subscription, platform)
}

func hasOrganizationBillingAttribution(apiKey *service.APIKey) bool {
	if apiKey == nil {
		return false
	}
	if apiKey.OrganizationID != nil || apiKey.OrganizationAuthorization != nil {
		return true
	}
	return apiKey.User != nil && (apiKey.User.OrganizationID != nil || apiKey.User.OrganizationRole != nil)
}
