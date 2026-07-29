package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	// ErrOrganizationBillingUnavailable is returned when an organization request
	// cannot safely run the platform admission checks. It deliberately does not
	// fall back to personal balance or subscription billing.
	ErrOrganizationBillingUnavailable = infraerrors.ServiceUnavailable(
		"ORGANIZATION_BILLING_UNAVAILABLE",
		"organization billing dependencies are unavailable",
	)
	ErrOrganizationAllocationInsufficient = infraerrors.Conflict(
		"ORGANIZATION_ALLOCATION_INSUFFICIENT",
		"organization allocation is insufficient",
	)
)

// CheckOrganizationPlatformEligibility runs the non-monetary admission guards
// for an organization-attributed billable HTTP execution. Personal balance and
// subscription eligibility are intentionally absent from this path. Durable
// reservation and final settlement are owned by the endpoint adapter tickets.
func (s *BillingCacheService) CheckOrganizationPlatformEligibility(
	ctx context.Context,
	user *User,
	apiKey *APIKey,
	group *Group,
	platform string,
) error {
	if s == nil || s.cfg == nil || s.cfg.RunMode == config.RunModeSimple {
		return ErrOrganizationBillingUnavailable
	}
	if user == nil || apiKey == nil || apiKey.User == nil || apiKey.User.ID != user.ID ||
		apiKey.UserID != user.ID || !user.IsActive() || !apiKey.IsActive() {
		return ErrOrganizationBillingUnavailable
	}
	if apiKey.IsExpired() {
		return ErrAPIKeyExpired
	}
	if apiKey.IsQuotaExhausted() {
		return ErrAPIKeyQuotaExhausted
	}
	if group != nil && (!group.Hydrated || !group.IsActive()) {
		return ErrOrganizationBillingUnavailable
	}
	authorization := apiKey.OrganizationAuthorization
	if authorization == nil || apiKey.OrganizationID == nil || user.OrganizationID == nil ||
		user.OrganizationRole == nil ||
		(*user.OrganizationRole != OrganizationRoleAdmin && *user.OrganizationRole != OrganizationRoleMember) ||
		authorization.APIKeyID != apiKey.ID || authorization.UserID != user.ID ||
		authorization.OrganizationID <= 0 || authorization.OrganizationID != *apiKey.OrganizationID ||
		authorization.OrganizationID != *user.OrganizationID ||
		authorization.OrganizationStatus != OrganizationStatusActive || authorization.OrganizationBalanceVersion < 0 ||
		authorization.AllocationID <= 0 || authorization.AllocationUserID != user.ID ||
		authorization.AllocationOrganizationID != authorization.OrganizationID ||
		authorization.AllocationStatus != domain.OrganizationAllocationStatusActive || authorization.AllocationVersion < 0 {
		return ErrOrganizationBillingUnavailable
	}
	available, err := NormalizeOrganizationMoney(authorization.Available, false)
	if err != nil {
		return ErrOrganizationBillingUnavailable.WithCause(err)
	}
	if available == "0.00000000" {
		return ErrOrganizationAllocationInsufficient
	}

	// These are the platform guards that remain shared with personal traffic.
	// Missing dependencies are unsafe for an organization request and therefore
	// fail closed before any counter is touched.
	if platform != "" && s.userPlatformQuotaRepo == nil {
		return ErrOrganizationBillingUnavailable
	}
	if apiKey.HasRateLimits() && s.cache == nil && s.apiKeyRateLimitLoader == nil {
		return ErrOrganizationBillingUnavailable
	}
	if group != nil && s.userRPMCache == nil {
		return ErrOrganizationBillingUnavailable
	}
	if user.RPMLimit > 0 && s.userRPMCache == nil {
		return ErrOrganizationBillingUnavailable
	}
	if group != nil && user.UserGroupRPMOverride == nil && s.userGroupRateRepo == nil {
		return ErrOrganizationBillingUnavailable
	}

	if err := s.checkOrganizationUserPlatformQuotaEligibility(ctx, user.ID, platform); err != nil {
		return err
	}
	if apiKey.HasRateLimits() {
		if err := s.checkOrganizationAPIKeyRateLimits(ctx, apiKey); err != nil {
			return err
		}
	}
	return s.checkOrganizationRPM(ctx, user, group)
}
