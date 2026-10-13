package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type organizationLifecycleRepositoryStub struct {
	preview *OrganizationArchivePreview
	result  *OrganizationLifecycleResult
	err     error
	command OrganizationLifecycleCommand
}

func (s *organizationLifecycleRepositoryStub) GetArchivePreview(context.Context, int64) (*OrganizationArchivePreview, error) {
	return s.preview, s.err
}

func (s *organizationLifecycleRepositoryStub) Transition(_ context.Context, command OrganizationLifecycleCommand) (*OrganizationLifecycleResult, error) {
	s.command = command
	return s.result, s.err
}

type organizationLifecycleInvalidatorStub struct {
	userIDs []int64
}

func (*organizationLifecycleInvalidatorStub) InvalidateAuthCacheByKey(context.Context, string) {}
func (s *organizationLifecycleInvalidatorStub) InvalidateAuthCacheByUserID(_ context.Context, userID int64) {
	s.userIDs = append(s.userIDs, userID)
}
func (*organizationLifecycleInvalidatorStub) InvalidateAuthCacheByGroupID(context.Context, int64) {}

type organizationLifecycleBillingInvalidatorStub struct {
	balanceUserIDs []int64
	platforms      []string
	groups         []int64
	keys           []int64
	err            error
}

func (s *organizationLifecycleBillingInvalidatorStub) InvalidateUserBalance(_ context.Context, userID int64) error {
	s.balanceUserIDs = append(s.balanceUserIDs, userID)
	return s.err
}
func (s *organizationLifecycleBillingInvalidatorStub) InvalidateSubscription(_ context.Context, _, groupID int64) error {
	s.groups = append(s.groups, groupID)
	return s.err
}
func (s *organizationLifecycleBillingInvalidatorStub) InvalidateAPIKeyRateLimit(_ context.Context, keyID int64) error {
	s.keys = append(s.keys, keyID)
	return s.err
}
func (s *organizationLifecycleBillingInvalidatorStub) InvalidateUserPlatformQuota(_ context.Context, _ int64, platform string) error {
	s.platforms = append(s.platforms, platform)
	return s.err
}

func TestOrganizationLifecycleServiceTransitionsAndInvalidatesMemberCaches(t *testing.T) {
	repo := &organizationLifecycleRepositoryStub{result: &OrganizationLifecycleResult{
		Organization: &Organization{ID: 7, Status: OrganizationStatusDisabled},
		CacheTargets: []OrganizationMemberCacheTarget{{UserID: 4, APIKeyIDs: []int64{101}, GroupIDs: []int64{9}}},
	}}
	invalidator := &organizationLifecycleInvalidatorStub{}
	billing := &organizationLifecycleBillingInvalidatorStub{}
	service := NewOrganizationLifecycleService(repo, invalidator, billing)

	organization, err := service.Disable(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, OrganizationStatusDisabled, organization.Status)
	require.Equal(t, OrganizationStatusDisabled, repo.command.TargetStatus)
	require.Equal(t, []int64{4}, invalidator.userIDs)
	require.Equal(t, []int64{4}, billing.balanceUserIDs)
	require.Equal(t, []int64{101}, billing.keys)
	require.Equal(t, []int64{9}, billing.groups)
	require.NotEmpty(t, billing.platforms)
}

func TestOrganizationLifecycleServiceArchiveRequiresBoundConfirmation(t *testing.T) {
	updatedAt := time.Date(2026, 7, 30, 2, 0, 0, 123456789, time.UTC)
	repo := &organizationLifecycleRepositoryStub{result: &OrganizationLifecycleResult{Organization: &Organization{ID: 7, Status: OrganizationStatusArchived}}}
	service := NewOrganizationLifecycleService(repo, nil, nil)

	_, err := service.Archive(context.Background(), OrganizationLifecycleCommand{OrganizationID: 7})
	require.ErrorIs(t, err, ErrOrganizationArchiveConfirmationRequired)

	organization, err := service.Archive(context.Background(), OrganizationLifecycleCommand{
		OrganizationID:         7,
		ExpectedBalanceVersion: 3,
		ExpectedUpdatedAt:      updatedAt,
		ConfirmArchive:         true,
	})
	require.NoError(t, err)
	require.Equal(t, OrganizationStatusArchived, organization.Status)
	require.Equal(t, OrganizationStatusArchived, repo.command.TargetStatus)
	require.Equal(t, updatedAt, repo.command.ExpectedUpdatedAt)
}

func TestOrganizationLifecycleServiceIgnoresBillingInvalidationFailureAfterCommit(t *testing.T) {
	repo := &organizationLifecycleRepositoryStub{result: &OrganizationLifecycleResult{
		Organization: &Organization{ID: 7, Status: OrganizationStatusDisabled},
		CacheTargets: []OrganizationMemberCacheTarget{{UserID: 4, APIKeyIDs: []int64{101}, GroupIDs: []int64{9}}},
	}}
	billing := &organizationLifecycleBillingInvalidatorStub{err: context.DeadlineExceeded}
	organization, err := NewOrganizationLifecycleService(repo, nil, billing).Disable(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, OrganizationStatusDisabled, organization.Status)
	require.Equal(t, []int64{4}, billing.balanceUserIDs)
	require.Equal(t, []int64{101}, billing.keys)
	require.Equal(t, []int64{9}, billing.groups)
}
