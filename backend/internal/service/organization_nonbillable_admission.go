package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// CheckOrganizationNonBillablePlatformEligibility applies the organization
// and shared platform guards without consulting personal funding or requiring
// positive organization allocation availability.
func (s *BillingCacheService) CheckOrganizationNonBillablePlatformEligibility(
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
