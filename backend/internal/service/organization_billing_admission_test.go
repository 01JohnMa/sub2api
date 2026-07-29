//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type organizationRateLimitLoaderStub struct {
	data *APIKeyRateLimitData
	err  error
}

func (s *organizationRateLimitLoaderStub) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	return s.data, s.err
}

func organizationAdmissionFixture() (*User, *APIKey) {
	organizationID := int64(41)
	role := OrganizationRoleMember
	user := &User{
		ID:               11,
		Status:           StatusActive,
		OrganizationID:   &organizationID,
		OrganizationRole: &role,
	}
	apiKey := &APIKey{
		ID:             21,
		UserID:         user.ID,
		Status:         StatusAPIKeyActive,
		OrganizationID: &organizationID,
		User:           user,
		OrganizationAuthorization: &OrganizationKeyAuthorization{
			APIKeyID:                 21,
			UserID:                   user.ID,
			OrganizationID:           organizationID,
			OrganizationStatus:       OrganizationStatusActive,
			AllocationID:             31,
			AllocationUserID:         user.ID,
			AllocationOrganizationID: organizationID,
			AllocationStatus:         domain.OrganizationAllocationStatusActive,
			Available:                "10.00000000",
			Reserved:                 "0.00000000",
		},
	}
	return user, apiKey
}

func TestCheckOrganizationPlatformEligibilitySkipsPersonalFunding(t *testing.T) {
	user, apiKey := organizationAdmissionFixture()
	svc := &BillingCacheService{
		cfg:                   &config.Config{},
		userPlatformQuotaRepo: &fakeQuotaRepo{},
	}

	require.NoError(t, svc.CheckOrganizationPlatformEligibility(
		context.Background(), user, apiKey, nil, "anthropic",
	))
}

func TestCheckOrganizationPlatformEligibilityRejectsSimpleMode(t *testing.T) {
	user, apiKey := organizationAdmissionFixture()
	svc := &BillingCacheService{cfg: &config.Config{RunMode: config.RunModeSimple}}

	err := svc.CheckOrganizationPlatformEligibility(context.Background(), user, apiKey, nil, "")
	require.ErrorIs(t, err, ErrOrganizationBillingUnavailable)
}

func TestCheckOrganizationPlatformEligibilityRejectsZeroAllocation(t *testing.T) {
	user, apiKey := organizationAdmissionFixture()
	apiKey.OrganizationAuthorization.Available = "0.00000000"
	svc := &BillingCacheService{cfg: &config.Config{}}

	err := svc.CheckOrganizationPlatformEligibility(context.Background(), user, apiKey, nil, "")
	require.ErrorIs(t, err, ErrOrganizationAllocationInsufficient)
}

func TestCheckOrganizationPlatformEligibilityRequiresQuotaRepository(t *testing.T) {
	user, apiKey := organizationAdmissionFixture()
	svc := &BillingCacheService{cfg: &config.Config{}}

	err := svc.CheckOrganizationPlatformEligibility(context.Background(), user, apiKey, nil, "anthropic")
	require.ErrorIs(t, err, ErrOrganizationBillingUnavailable)
}

func TestCheckOrganizationPlatformEligibilityAppliesQuotaForSubscriptionGroup(t *testing.T) {
	user, apiKey := organizationAdmissionFixture()
	zero := 0.0
	svc := &BillingCacheService{
		cfg: &config.Config{},
		userPlatformQuotaRepo: &fakeQuotaRepo{rec: &UserPlatformQuotaRecord{
			UserID:        user.ID,
			Platform:      "anthropic",
			DailyLimitUSD: &zero,
		}},
		userRPMCache:      &userRPMCacheStub{},
		userGroupRateRepo: &rpmOverrideRepoStub{},
	}
	group := &Group{ID: 51, Status: StatusActive, Hydrated: true, SubscriptionType: SubscriptionTypeSubscription}

	err := svc.CheckOrganizationPlatformEligibility(context.Background(), user, apiKey, group, "anthropic")
	require.ErrorIs(t, err, ErrUserPlatformDailyQuotaExhausted)
}

func TestCheckOrganizationPlatformEligibilityFailsClosedOnRateLoaderError(t *testing.T) {
	user, apiKey := organizationAdmissionFixture()
	apiKey.RateLimit5h = 1
	svc := &BillingCacheService{
		cfg:                   &config.Config{},
		apiKeyRateLimitLoader: &organizationRateLimitLoaderStub{err: errors.New("rate store unavailable")},
	}

	err := svc.CheckOrganizationPlatformEligibility(context.Background(), user, apiKey, nil, "")
	require.ErrorIs(t, err, ErrOrganizationBillingUnavailable)
}

func TestCheckOrganizationPlatformEligibilityAppliesAPIRateLimit(t *testing.T) {
	user, apiKey := organizationAdmissionFixture()
	apiKey.RateLimit5h = 1
	windowStart := time.Now()
	svc := &BillingCacheService{
		cfg: &config.Config{},
		apiKeyRateLimitLoader: &organizationRateLimitLoaderStub{data: &APIKeyRateLimitData{
			Usage5h:       1,
			Window5hStart: &windowStart,
		}},
	}

	err := svc.CheckOrganizationPlatformEligibility(context.Background(), user, apiKey, nil, "")
	require.ErrorIs(t, err, ErrAPIKeyRateLimit5hExceeded)
}

func TestCheckOrganizationPlatformEligibilityFailsClosedOnRPMError(t *testing.T) {
	user, apiKey := organizationAdmissionFixture()
	user.RPMLimit = 1
	svc := &BillingCacheService{
		cfg:          &config.Config{},
		userRPMCache: &userRPMCacheStub{userErr: errors.New("rpm cache unavailable")},
	}

	err := svc.CheckOrganizationPlatformEligibility(context.Background(), user, apiKey, nil, "")
	require.ErrorIs(t, err, ErrOrganizationBillingUnavailable)
	require.Equal(t, "ORGANIZATION_BILLING_UNAVAILABLE", infraerrors.Reason(err))
}

func TestCheckOrganizationPlatformEligibilityAppliesRPM(t *testing.T) {
	user, apiKey := organizationAdmissionFixture()
	user.RPMLimit = 1
	svc := &BillingCacheService{
		cfg:          &config.Config{},
		userRPMCache: &userRPMCacheStub{userCounts: []int{2}},
	}

	err := svc.CheckOrganizationPlatformEligibility(context.Background(), user, apiKey, nil, "")
	require.ErrorIs(t, err, ErrUserRPMExceeded)
}
