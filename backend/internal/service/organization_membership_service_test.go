package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type organizationMembershipRepositoryStub struct {
	member         *OrganizationMember
	err            error
	addOrgID       int64
	addUserID      int64
	addRole        string
	currentOrgID   int64
	currentTarget  OrganizationMemberTarget
	changeOrgID    int64
	changeUserID   int64
	changeRole     string
	listOrgID      int64
	listPagination pagination.PaginationParams
	removeCommand  OrganizationMemberRemovalCommand
	removeResult   *OrganizationMemberRemovalResult
}

func (s *organizationMembershipRepositoryStub) List(_ context.Context, organizationID int64, params pagination.PaginationParams) ([]OrganizationMember, *pagination.PaginationResult, error) {
	s.listOrgID = organizationID
	s.listPagination = params
	if s.err != nil {
		return nil, nil, s.err
	}
	if s.member == nil {
		return nil, &pagination.PaginationResult{}, nil
	}
	return []OrganizationMember{*s.member}, &pagination.PaginationResult{Total: 1, Page: 1, PageSize: 20, Pages: 1}, nil
}

func (s *organizationMembershipRepositoryStub) Add(_ context.Context, organizationID, userID int64, role string) (*OrganizationMember, error) {
	s.addOrgID = organizationID
	s.addUserID = userID
	s.addRole = role
	return s.member, s.err
}

func (s *organizationMembershipRepositoryStub) AddCurrentMember(_ context.Context, organizationID int64, target OrganizationMemberTarget) (*OrganizationMember, error) {
	s.currentOrgID = organizationID
	s.currentTarget = target
	return s.member, s.err
}

func (s *organizationMembershipRepositoryStub) ChangeRole(_ context.Context, organizationID, userID int64, role string) (*OrganizationMember, error) {
	s.changeOrgID = organizationID
	s.changeUserID = userID
	s.changeRole = role
	return s.member, s.err
}

func (s *organizationMembershipRepositoryStub) Remove(_ context.Context, command OrganizationMemberRemovalCommand) (*OrganizationMemberRemovalResult, error) {
	s.removeCommand = command
	return s.removeResult, s.err
}

type organizationMembershipInvalidatorStub struct {
	userIDs    []int64
	contextErr error
}

func (s *organizationMembershipInvalidatorStub) InvalidateAuthCacheByKey(context.Context, string) {}

func (s *organizationMembershipInvalidatorStub) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	s.userIDs = append(s.userIDs, userID)
	s.contextErr = ctx.Err()
}

func (s *organizationMembershipInvalidatorStub) InvalidateAuthCacheByGroupID(context.Context, int64) {
}

type organizationMembershipBillingInvalidatorStub struct {
	balanceUserIDs      []int64
	subscriptionTargets [][2]int64
	apiKeyIDs           []int64
	platformTargets     []struct {
		userID   int64
		platform string
	}
	contextErrors []error
}

func (s *organizationMembershipBillingInvalidatorStub) InvalidateUserBalance(ctx context.Context, userID int64) error {
	s.balanceUserIDs = append(s.balanceUserIDs, userID)
	s.contextErrors = append(s.contextErrors, ctx.Err())
	return nil
}

func (s *organizationMembershipBillingInvalidatorStub) InvalidateSubscription(ctx context.Context, userID, groupID int64) error {
	s.subscriptionTargets = append(s.subscriptionTargets, [2]int64{userID, groupID})
	s.contextErrors = append(s.contextErrors, ctx.Err())
	return nil
}

func (s *organizationMembershipBillingInvalidatorStub) InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error {
	s.apiKeyIDs = append(s.apiKeyIDs, keyID)
	s.contextErrors = append(s.contextErrors, ctx.Err())
	return nil
}

func (s *organizationMembershipBillingInvalidatorStub) InvalidateUserPlatformQuota(ctx context.Context, userID int64, platform string) error {
	s.platformTargets = append(s.platformTargets, struct {
		userID   int64
		platform string
	}{userID: userID, platform: platform})
	s.contextErrors = append(s.contextErrors, ctx.Err())
	return nil
}

func TestOrganizationMembershipServiceAddNormalizesRoleAndInvalidatesAfterCommit(t *testing.T) {
	repo := &organizationMembershipRepositoryStub{member: &OrganizationMember{UserID: 7}}
	invalidator := &organizationMembershipInvalidatorStub{}
	svc := NewOrganizationMembershipService(repo, invalidator, nil)

	member, err := svc.Add(context.Background(), 3, 7, " ORGANIZATION_ADMIN ")

	require.NoError(t, err)
	require.Equal(t, int64(7), member.UserID)
	require.Equal(t, int64(3), repo.addOrgID)
	require.Equal(t, int64(7), repo.addUserID)
	require.Equal(t, OrganizationRoleAdmin, repo.addRole)
	require.Equal(t, []int64{7}, invalidator.userIDs)
}

func TestOrganizationMembershipServiceAddDoesNotInvalidateOnFailure(t *testing.T) {
	repo := &organizationMembershipRepositoryStub{err: errors.New("write failed")}
	invalidator := &organizationMembershipInvalidatorStub{}
	svc := NewOrganizationMembershipService(repo, invalidator, nil)

	_, err := svc.Add(context.Background(), 3, 7, OrganizationRoleMember)

	require.Error(t, err)
	require.Empty(t, invalidator.userIDs)
}

func TestOrganizationMembershipServiceAddCurrentMemberNormalizesEmailAndInvalidatesAfterCommit(t *testing.T) {
	repo := &organizationMembershipRepositoryStub{member: &OrganizationMember{UserID: 7, OrganizationRole: OrganizationRoleMember}}
	invalidator := &organizationMembershipInvalidatorStub{}
	svc := NewOrganizationMembershipService(repo, invalidator, nil)

	member, err := svc.AddCurrentMember(context.Background(), 3, OrganizationMemberTarget{Email: " Member@Example.COM "})

	require.NoError(t, err)
	require.Equal(t, int64(7), member.UserID)
	require.Equal(t, int64(3), repo.currentOrgID)
	require.Nil(t, repo.currentTarget.UserID)
	require.Equal(t, "member@example.com", repo.currentTarget.Email)
	require.Equal(t, []int64{7}, invalidator.userIDs)
}

func TestOrganizationMembershipServiceAddCurrentMemberRejectsAmbiguousTarget(t *testing.T) {
	repo := &organizationMembershipRepositoryStub{}
	svc := NewOrganizationMembershipService(repo, nil, nil)
	userID := int64(7)

	_, err := svc.AddCurrentMember(context.Background(), 3, OrganizationMemberTarget{UserID: &userID, Email: "member@example.com"})

	require.ErrorIs(t, err, ErrInvalidOrganizationMemberTarget)
	require.Zero(t, repo.currentOrgID)
}

func TestOrganizationMembershipServiceRejectsInvalidRoleBeforeRepository(t *testing.T) {
	repo := &organizationMembershipRepositoryStub{}
	svc := NewOrganizationMembershipService(repo, nil, nil)

	_, err := svc.Add(context.Background(), 3, 7, "owner")

	require.ErrorIs(t, err, ErrInvalidOrganizationRole)
	require.Zero(t, repo.addOrgID)
}

func TestOrganizationMembershipServiceChangeRolePreservesScopedTarget(t *testing.T) {
	repo := &organizationMembershipRepositoryStub{member: &OrganizationMember{UserID: 7, OrganizationRole: OrganizationRoleMember}}
	invalidator := &organizationMembershipInvalidatorStub{}
	svc := NewOrganizationMembershipService(repo, invalidator, nil)

	_, err := svc.ChangeRole(context.Background(), 3, 7, OrganizationRoleMember)

	require.NoError(t, err)
	require.Equal(t, int64(3), repo.changeOrgID)
	require.Equal(t, int64(7), repo.changeUserID)
	require.Equal(t, OrganizationRoleMember, repo.changeRole)
	require.Equal(t, []int64{7}, invalidator.userIDs)
}

func TestOrganizationMembershipServiceRemoveBuildsScopedCommandAndInvalidatesCachesAfterCommit(t *testing.T) {
	repo := &organizationMembershipRepositoryStub{removeResult: &OrganizationMemberRemovalResult{
		OperationID:          "11111111-1111-1111-1111-111111111111",
		CorrelationID:        "22222222-2222-2222-2222-222222222222",
		OrganizationID:       3,
		UserID:               7,
		AllocationID:         11,
		OperationType:        "member_removal_reclaim",
		Currency:             "USD",
		Outcome:              "committed",
		Reclaimed:            "2.50000000",
		PoolBefore:           "7.50000000",
		PoolAfter:            "10.00000000",
		BalanceVersionBefore: 4,
		BalanceVersionAfter:  5,
		CanonicalPayloadHash: strings.Repeat("a", 64),
		APIKeyIDs:            []int64{21, 22},
		GroupIDs:             []int64{31, 31, 32},
	}}
	authInvalidator := &organizationMembershipInvalidatorStub{}
	billingInvalidator := &organizationMembershipBillingInvalidatorStub{}
	svc := NewOrganizationMembershipService(repo, authInvalidator, billingInvalidator)
	actor := OrganizationMemberRemovalActor{
		UserID:                5,
		DisplayName:           "organization-admin-5",
		OrganizationRole:      OrganizationRoleAdmin,
		AuthorizationScope:    "organization_admin_jwt",
		CurrentOrganizationID: 3,
		Active:                true,
	}

	result, err := svc.Remove(context.Background(), actor, 3, 7, OrganizationMemberRemovalInput{PublicReason: " offboarding "}, " removal-key ")

	require.NoError(t, err)
	require.Equal(t, int64(7), result.UserID)
	require.Equal(t, int64(3), repo.removeCommand.OrganizationID)
	require.Equal(t, int64(7), repo.removeCommand.TargetUserID)
	require.Equal(t, "offboarding", repo.removeCommand.PublicReason)
	require.NotEmpty(t, repo.removeCommand.IdempotencyKeyHash)
	require.NotEmpty(t, repo.removeCommand.CanonicalPayloadHash)
	require.Equal(t, []int64{7}, authInvalidator.userIDs)
	require.Equal(t, []int64{7}, billingInvalidator.balanceUserIDs)
	require.Equal(t, [][2]int64{{7, 31}, {7, 32}}, billingInvalidator.subscriptionTargets)
	require.Equal(t, []int64{21, 22}, billingInvalidator.apiKeyIDs)
	require.Len(t, billingInvalidator.platformTargets, len(AllowedQuotaPlatforms))
	for index, platform := range AllowedQuotaPlatforms {
		require.Equal(t, int64(7), billingInvalidator.platformTargets[index].userID)
		require.Equal(t, platform, billingInvalidator.platformTargets[index].platform)
	}
}

func TestOrganizationMembershipServiceRemoveDoesNotInvalidateCachesOnFailure(t *testing.T) {
	repo := &organizationMembershipRepositoryStub{err: errors.New("write failed")}
	authInvalidator := &organizationMembershipInvalidatorStub{}
	billingInvalidator := &organizationMembershipBillingInvalidatorStub{}
	svc := NewOrganizationMembershipService(repo, authInvalidator, billingInvalidator)
	actor := OrganizationMemberRemovalActor{
		UserID:             5,
		DisplayName:        "platform-admin-5",
		PlatformRole:       RoleAdmin,
		AuthorizationScope: "platform_admin_jwt",
		Active:             true,
	}

	_, err := svc.Remove(context.Background(), actor, 3, 7, OrganizationMemberRemovalInput{}, "removal-key")

	require.Error(t, err)
	require.Empty(t, authInvalidator.userIDs)
	require.Empty(t, billingInvalidator.balanceUserIDs)
	require.Empty(t, billingInvalidator.subscriptionTargets)
	require.Empty(t, billingInvalidator.apiKeyIDs)
	require.Empty(t, billingInvalidator.platformTargets)
}

func TestOrganizationMembershipServiceRemoveRejectsInvalidActorBeforeRepository(t *testing.T) {
	repo := &organizationMembershipRepositoryStub{}
	svc := NewOrganizationMembershipService(repo, nil, nil)

	_, err := svc.Remove(context.Background(), OrganizationMemberRemovalActor{}, 3, 7, OrganizationMemberRemovalInput{}, "removal-key")

	require.ErrorIs(t, err, ErrOrganizationMemberRemovalForbidden)
	require.Zero(t, repo.removeCommand.OrganizationID)
}

func TestOrganizationMembershipServiceRemoveInvalidatesCachesAfterRequestCancellation(t *testing.T) {
	repo := &organizationMembershipRepositoryStub{removeResult: &OrganizationMemberRemovalResult{
		OperationID:          "11111111-1111-1111-1111-111111111111",
		CorrelationID:        "22222222-2222-2222-2222-222222222222",
		OrganizationID:       3,
		UserID:               7,
		AllocationID:         11,
		OperationType:        "member_removal_reclaim",
		Currency:             "USD",
		Outcome:              "committed",
		Reclaimed:            "2.50000000",
		PoolBefore:           "7.50000000",
		PoolAfter:            "10.00000000",
		BalanceVersionBefore: 4,
		BalanceVersionAfter:  5,
		CanonicalPayloadHash: strings.Repeat("a", 64),
		APIKeyIDs:            []int64{21},
		GroupIDs:             []int64{31},
	}}
	authInvalidator := &organizationMembershipInvalidatorStub{}
	billingInvalidator := &organizationMembershipBillingInvalidatorStub{}
	svc := NewOrganizationMembershipService(repo, authInvalidator, billingInvalidator)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Remove(ctx, OrganizationMemberRemovalActor{
		UserID:             5,
		DisplayName:        "platform-admin-5",
		PlatformRole:       RoleAdmin,
		AuthorizationScope: "platform_admin_jwt",
		Active:             true,
	}, 3, 7, OrganizationMemberRemovalInput{}, "removal-key")

	require.NoError(t, err)
	require.NoError(t, authInvalidator.contextErr)
	require.NotEmpty(t, billingInvalidator.contextErrors)
	for _, contextErr := range billingInvalidator.contextErrors {
		require.NoError(t, contextErr)
	}
}
