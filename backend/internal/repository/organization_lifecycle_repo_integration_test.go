//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type lifecycleNoopAuthInvalidator struct{}

func (lifecycleNoopAuthInvalidator) InvalidateAuthCacheByKey(context.Context, string)    {}
func (lifecycleNoopAuthInvalidator) InvalidateAuthCacheByUserID(context.Context, int64)  {}
func (lifecycleNoopAuthInvalidator) InvalidateAuthCacheByGroupID(context.Context, int64) {}

type lifecycleFailingBillingInvalidator struct {
	balanceUsers []int64
	keyIDs       []int64
}

func (s *lifecycleFailingBillingInvalidator) InvalidateUserBalance(_ context.Context, userID int64) error {
	s.balanceUsers = append(s.balanceUsers, userID)
	return errors.New("redis unavailable")
}
func (*lifecycleFailingBillingInvalidator) InvalidateSubscription(context.Context, int64, int64) error {
	return errors.New("redis unavailable")
}
func (s *lifecycleFailingBillingInvalidator) InvalidateAPIKeyRateLimit(_ context.Context, keyID int64) error {
	s.keyIDs = append(s.keyIDs, keyID)
	return errors.New("redis unavailable")
}
func (*lifecycleFailingBillingInvalidator) InvalidateUserPlatformQuota(context.Context, int64, string) error {
	return errors.New("redis unavailable")
}

func createLifecycleOrganization(t *testing.T, client *dbent.Client, suffix string) (*service.User, *service.Organization) {
	t.Helper()
	creator := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("lifecycle-admin-%s@example.com", suffix), Role: service.RoleAdmin})
	organizationModel := &service.Organization{Name: "Lifecycle " + suffix, Code: "lifecycle-" + suffix, Status: service.OrganizationStatusActive, CreatedBy: creator.ID}
	require.NoError(t, NewOrganizationRepository(client).Create(context.Background(), organizationModel))
	return creator, organizationModel
}

func TestOrganizationLifecycleRepositoryTransitionsAndArchiveConditions(t *testing.T) {
	client := testEntClient(t)
	ctx := context.Background()
	creator, organizationModel := createLifecycleOrganization(t, client, "states")
	repo := NewOrganizationLifecycleRepository(client)

	disabled, err := repo.Transition(ctx, service.OrganizationLifecycleCommand{OrganizationID: organizationModel.ID, TargetStatus: service.OrganizationStatusDisabled})
	require.NoError(t, err)
	require.Equal(t, service.OrganizationStatusDisabled, disabled.Organization.Status)
	require.Equal(t, []int64{}, disabled.AffectedUserIDs)

	reactivated, err := repo.Transition(ctx, service.OrganizationLifecycleCommand{OrganizationID: organizationModel.ID, TargetStatus: service.OrganizationStatusActive})
	require.NoError(t, err)
	require.Equal(t, service.OrganizationStatusActive, reactivated.Organization.Status)

	member := mustCreateUser(t, client, &service.User{Email: "lifecycle-member-states@example.com"})
	_, err = client.User.UpdateOneID(member.ID).SetOrganizationID(organizationModel.ID).SetOrganizationRole(domain.OrganizationRoleMember).Save(ctx)
	require.NoError(t, err)
	allocation, err := client.OrganizationAllocation.Create().SetOrganizationID(organizationModel.ID).SetUserIDSnapshot(member.ID).SetUserRefID(member.ID).SetStatus(domain.OrganizationAllocationStatusActive).SetAvailable(decimal.RequireFromString("1.00000000")).SetReserved(decimal.Zero).Save(ctx)
	require.NoError(t, err)

	preview, err := repo.GetArchivePreview(ctx, organizationModel.ID)
	require.NoError(t, err)
	_, err = repo.Transition(ctx, service.OrganizationLifecycleCommand{OrganizationID: organizationModel.ID, TargetStatus: service.OrganizationStatusArchived, ConfirmArchive: true, ExpectedBalanceVersion: preview.BalanceVersion, ExpectedUpdatedAt: preview.UpdatedAt})
	require.ErrorIs(t, err, service.ErrOrganizationBalanceNotSettled)

	_, err = client.OrganizationAllocation.UpdateOneID(allocation.ID).SetAvailable(decimal.Zero).Save(ctx)
	require.NoError(t, err)
	preview, err = repo.GetArchivePreview(ctx, organizationModel.ID)
	require.NoError(t, err)
	archived, err := repo.Transition(ctx, service.OrganizationLifecycleCommand{OrganizationID: organizationModel.ID, TargetStatus: service.OrganizationStatusArchived, ConfirmArchive: true, ExpectedBalanceVersion: preview.BalanceVersion, ExpectedUpdatedAt: preview.UpdatedAt})
	require.NoError(t, err)
	require.Equal(t, service.OrganizationStatusArchived, archived.Organization.Status)
	require.Equal(t, []int64{member.ID}, archived.AffectedUserIDs)

	_, err = repo.Transition(ctx, service.OrganizationLifecycleCommand{OrganizationID: organizationModel.ID, TargetStatus: service.OrganizationStatusActive})
	require.ErrorIs(t, err, service.ErrOrganizationArchived)
	_ = creator
}

func TestOrganizationLifecycleRepositoryRejectsStaleArchiveConfirmationAndReservations(t *testing.T) {
	client := testEntClient(t)
	ctx := context.Background()
	_, organizationModel := createLifecycleOrganization(t, client, "conflict")
	repo := NewOrganizationLifecycleRepository(client)
	preview, err := repo.GetArchivePreview(ctx, organizationModel.ID)
	require.NoError(t, err)

	_, err = client.Organization.UpdateOneID(organizationModel.ID).SetUnallocatedPool(decimal.RequireFromString("2.50000000")).AddBalanceVersion(1).Save(ctx)
	require.NoError(t, err)
	_, err = repo.Transition(ctx, service.OrganizationLifecycleCommand{OrganizationID: organizationModel.ID, TargetStatus: service.OrganizationStatusArchived, ConfirmArchive: true, ExpectedBalanceVersion: preview.BalanceVersion, ExpectedUpdatedAt: preview.UpdatedAt})
	require.ErrorIs(t, err, service.ErrOrganizationArchiveVersionConflict)
}

func TestOrganizationLifecycleCacheFailureDoesNotChangeAuthoritativeDisabledState(t *testing.T) {
	client := testEntClient(t)
	ctx := context.Background()
	_, organizationModel := createLifecycleOrganization(t, client, "cache-failure")
	member := mustCreateUser(t, client, &service.User{Email: "lifecycle-cache-failure-member@example.com"})
	_, err := client.User.UpdateOneID(member.ID).
		SetOrganizationID(organizationModel.ID).
		SetOrganizationRole(domain.OrganizationRoleMember).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.OrganizationAllocation.Create().
		SetOrganizationID(organizationModel.ID).
		SetUserIDSnapshot(member.ID).
		SetUserRefID(member.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)
	key, err := client.APIKey.Create().
		SetUserID(member.ID).
		SetOrganizationID(organizationModel.ID).
		SetKey("sk-lifecycle-cache-failure").
		SetName("lifecycle cache failure").
		SetStatus(service.StatusAPIKeyActive).
		Save(ctx)
	require.NoError(t, err)

	billing := &lifecycleFailingBillingInvalidator{}
	lifecycle := service.NewOrganizationLifecycleService(
		NewOrganizationLifecycleRepository(client),
		lifecycleNoopAuthInvalidator{},
		billing,
	)
	disabled, err := lifecycle.Disable(ctx, organizationModel.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationStatusDisabled, disabled.Status)
	require.Equal(t, []int64{member.ID}, billing.balanceUsers)
	require.Equal(t, []int64{key.ID}, billing.keyIDs)

	authoritative, err := NewAPIKeyRepository(client, integrationDB).GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, authoritative.OrganizationAuthorization)
	require.Equal(t, service.OrganizationStatusDisabled, authoritative.OrganizationAuthorization.OrganizationStatus)
}
