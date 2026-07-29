//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	dborganization "github.com/Wei-Shaw/sub2api/ent/organization"
	"github.com/Wei-Shaw/sub2api/ent/organizationallocation"
	"github.com/Wei-Shaw/sub2api/ent/organizationledgeroperation"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestOrganizationMembershipRepositoryAddIsAtomicAndPreservesPlatformRole(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	creator := mustCreateUser(t, client, &service.User{Email: "membership-creator@example.com", Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: "membership-target@example.com", Role: service.RoleAdmin})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: target.ID, Key: "sk-membership-existing", Name: "existing"})

	organizationRepo := NewOrganizationRepository(client)
	organization := &service.Organization{
		Name: "Membership", Code: "membership", Status: service.OrganizationStatusActive, CreatedBy: creator.ID,
	}
	require.NoError(t, organizationRepo.Create(ctx, organization))

	repo := NewOrganizationMembershipRepository(client)
	member, err := repo.Add(ctx, organization.ID, target.ID, service.OrganizationRoleAdmin)
	require.NoError(t, err)
	require.Equal(t, target.ID, member.UserID)
	require.Equal(t, service.OrganizationRoleAdmin, member.OrganizationRole)
	require.Equal(t, "0.00000000", member.Allocation.Available)
	require.Equal(t, "0.00000000", member.Allocation.Reserved)

	storedUser, err := client.User.Query().Where(dbuser.IDEQ(target.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, service.RoleAdmin, storedUser.Role, "platform role must remain unchanged")
	require.NotNil(t, storedUser.OrganizationID)
	require.Equal(t, organization.ID, *storedUser.OrganizationID)
	require.NotNil(t, storedUser.OrganizationRole)
	require.Equal(t, service.OrganizationRoleAdmin, *storedUser.OrganizationRole)

	allocation, err := client.OrganizationAllocation.Query().
		Where(
			organizationallocation.OrganizationIDEQ(organization.ID),
			organizationallocation.UserRefIDEQ(target.ID),
			organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
		).
		Only(ctx)
	require.NoError(t, err)
	require.True(t, allocation.Available.Equal(decimal.Zero))
	require.True(t, allocation.Reserved.Equal(decimal.Zero))

	storedKey, err := client.APIKey.Query().Where(apikey.IDEQ(key.ID)).Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, storedKey.OrganizationID)
	require.Equal(t, organization.ID, *storedKey.OrganizationID)
}

func TestOrganizationMembershipRepositoryRemoveReclaimsFundsConvertsKeysAndReplays(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	actor := mustCreateUser(t, client, &service.User{Email: "membership-remove-actor@example.com", Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: "membership-remove-target@example.com", Balance: 55.25})
	key := mustCreateApiKey(t, client, &service.APIKey{
		UserID: target.ID, Key: "sk-membership-remove", Name: "preserved", Status: service.StatusAPIKeyActive,
		Quota: 9.5, QuotaUsed: 1.25, RateLimit5h: 2.5, RateLimit1d: 3.5, RateLimit7d: 4.5,
	})
	organizationRepo := NewOrganizationRepository(client)
	organizationModel := &service.Organization{
		Name: "Remove", Code: "membership-remove", Status: service.OrganizationStatusActive, CreatedBy: actor.ID,
	}
	require.NoError(t, organizationRepo.Create(ctx, organizationModel))
	membershipRepo := NewOrganizationMembershipRepository(client)
	member, err := membershipRepo.Add(ctx, organizationModel.ID, target.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	_, err = client.Organization.UpdateOneID(organizationModel.ID).
		SetUnallocatedPool(decimal.RequireFromString("5.00000000")).
		SetBalanceVersion(4).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.OrganizationAllocation.UpdateOneID(member.Allocation.ID).
		SetAvailable(decimal.RequireFromString("12.00000000")).
		SetVersion(3).
		Save(ctx)
	require.NoError(t, err)

	manager := service.NewOrganizationMembershipService(membershipRepo, nil, nil)
	removalActor := platformMemberRemovalActor(actor.ID)
	result, err := manager.Remove(ctx, removalActor, organizationModel.ID, target.ID, service.OrganizationMemberRemovalInput{PublicReason: "contract ended"}, "membership-remove-key")
	require.NoError(t, err)
	require.False(t, result.Replayed)
	require.Equal(t, "12.00000000", result.Reclaimed)
	require.Equal(t, "5.00000000", result.PoolBefore)
	require.Equal(t, "17.00000000", result.PoolAfter)
	require.Equal(t, int64(4), result.BalanceVersionBefore)
	require.Equal(t, int64(5), result.BalanceVersionAfter)
	require.Equal(t, []int64{key.ID}, result.APIKeyIDs)

	storedOrganization, err := client.Organization.Get(ctx, organizationModel.ID)
	require.NoError(t, err)
	require.Equal(t, "17.00000000", storedOrganization.UnallocatedPool.StringFixed(8))
	storedAllocation, err := client.OrganizationAllocation.Get(ctx, member.Allocation.ID)
	require.NoError(t, err)
	require.Equal(t, domain.OrganizationAllocationStatusClosed, storedAllocation.Status)
	require.Equal(t, "0.00000000", storedAllocation.Available.StringFixed(8))
	require.Equal(t, "0.00000000", storedAllocation.Reserved.StringFixed(8))
	storedUser, err := client.User.Get(ctx, target.ID)
	require.NoError(t, err)
	require.Nil(t, storedUser.OrganizationID)
	require.Nil(t, storedUser.OrganizationRole)
	require.InDelta(t, 55.25, storedUser.Balance, 0.00000001)
	storedKey, err := client.APIKey.Get(ctx, key.ID)
	require.NoError(t, err)
	require.Nil(t, storedKey.OrganizationID)
	require.Equal(t, key.Key, storedKey.Key)
	require.Equal(t, key.Name, storedKey.Name)
	require.Equal(t, key.Status, storedKey.Status)
	require.Equal(t, key.Quota, storedKey.Quota)
	require.Equal(t, key.QuotaUsed, storedKey.QuotaUsed)
	require.Equal(t, key.RateLimit5h, storedKey.RateLimit5h)
	require.Equal(t, key.RateLimit1d, storedKey.RateLimit1d)
	require.Equal(t, key.RateLimit7d, storedKey.RateLimit7d)

	operation, err := client.OrganizationLedgerOperation.Query().
		Where(
			organizationledgeroperation.OrganizationIDEQ(organizationModel.ID),
			organizationledgeroperation.OperationTypeEQ(domain.OrganizationFundingOperationMemberRemovalReclaim),
		).
		WithPostings().
		Only(ctx)
	require.NoError(t, err)
	postings, err := operation.Edges.PostingsOrErr()
	require.NoError(t, err)
	require.Len(t, postings, 2)

	replayed, err := manager.Remove(ctx, removalActor, organizationModel.ID, target.ID, service.OrganizationMemberRemovalInput{PublicReason: "contract ended"}, "membership-remove-key")
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, result.OperationID, replayed.OperationID)
	require.Equal(t, []int64{key.ID}, replayed.APIKeyIDs)
	storedOrganization, err = client.Organization.Get(ctx, organizationModel.ID)
	require.NoError(t, err)
	require.Equal(t, "17.00000000", storedOrganization.UnallocatedPool.StringFixed(8))

	_, err = manager.Remove(ctx, removalActor, organizationModel.ID, target.ID, service.OrganizationMemberRemovalInput{PublicReason: "different"}, "membership-remove-key")
	require.ErrorIs(t, err, service.ErrIdempotencyKeyConflict)
	_, err = client.User.UpdateOneID(actor.ID).SetRole(service.RoleUser).Save(ctx)
	require.NoError(t, err)
	_, err = manager.Remove(ctx, removalActor, organizationModel.ID, target.ID, service.OrganizationMemberRemovalInput{PublicReason: "contract ended"}, "membership-remove-key")
	require.ErrorIs(t, err, service.ErrOrganizationMemberRemovalForbidden)
}

func TestOrganizationMembershipRepositoryRemoveReturnsReservationConflictWhenAllocationIsLocked(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := time.Now().UnixNano()
	actor := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-remove-nowait-actor-%d@example.com", suffix), Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-remove-nowait-target-%d@example.com", suffix)})
	organizationRepo := NewOrganizationRepository(client)
	organizationModel := &service.Organization{
		Name: "Remove No Wait", Code: fmt.Sprintf("membership-remove-nowait-%d", suffix), Status: service.OrganizationStatusActive, CreatedBy: actor.ID,
	}
	require.NoError(t, organizationRepo.Create(ctx, organizationModel))
	membershipRepo := NewOrganizationMembershipRepository(client)
	member, err := membershipRepo.Add(ctx, organizationModel.ID, target.ID, service.OrganizationRoleMember)
	require.NoError(t, err)

	lockTx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = lockTx.Rollback() }()
	lockCtx := dbent.NewTxContext(ctx, lockTx)
	_, err = lockTx.Client().OrganizationAllocation.Query().
		Where(organizationallocation.IDEQ(member.Allocation.ID)).
		ForUpdate().
		Only(lockCtx)
	require.NoError(t, err)

	removeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	startedAt := time.Now()
	_, err = service.NewOrganizationMembershipService(membershipRepo, nil, nil).Remove(
		removeCtx,
		platformMemberRemovalActor(actor.ID),
		organizationModel.ID,
		target.ID,
		service.OrganizationMemberRemovalInput{},
		fmt.Sprintf("remove-nowait-%d", suffix),
	)

	require.ErrorIs(t, err, service.ErrOrganizationAllocationHasReservations)
	require.Less(t, time.Since(startedAt), 500*time.Millisecond)
}

func TestOrganizationMembershipRepositoryRemoveReturnsReservationConflictWhenExecutionHoldsOrganizationLock(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := time.Now().UnixNano()
	actor := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-remove-org-nowait-actor-%d@example.com", suffix), Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-remove-org-nowait-target-%d@example.com", suffix)})
	organizationRepo := NewOrganizationRepository(client)
	organizationModel := &service.Organization{
		Name: "Remove Organization No Wait", Code: fmt.Sprintf("membership-remove-org-nowait-%d", suffix), Status: service.OrganizationStatusActive, CreatedBy: actor.ID,
	}
	require.NoError(t, organizationRepo.Create(ctx, organizationModel))
	membershipRepo := NewOrganizationMembershipRepository(client)
	_, err := membershipRepo.Add(ctx, organizationModel.ID, target.ID, service.OrganizationRoleMember)
	require.NoError(t, err)

	lockTx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = lockTx.Rollback() }()
	lockCtx := dbent.NewTxContext(ctx, lockTx)
	_, _, err = lockExecutionOrganization(lockCtx, lockTx.Client(), organizationModel.ID)
	require.NoError(t, err)

	removeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	startedAt := time.Now()
	_, err = service.NewOrganizationMembershipService(membershipRepo, nil, nil).Remove(
		removeCtx,
		platformMemberRemovalActor(actor.ID),
		organizationModel.ID,
		target.ID,
		service.OrganizationMemberRemovalInput{},
		fmt.Sprintf("remove-org-nowait-%d", suffix),
	)

	require.ErrorIs(t, err, service.ErrOrganizationAllocationHasReservations)
	require.Less(t, time.Since(startedAt), 500*time.Millisecond)
}

func TestOrganizationMembershipRepositoryRemoveRejectsExternalTransactionOwnership(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := time.Now().UnixNano()
	actor := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-remove-external-tx-actor-%d@example.com", suffix), Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-remove-external-tx-target-%d@example.com", suffix)})
	organizationRepo := NewOrganizationRepository(client)
	organizationModel := &service.Organization{
		Name: "Remove External Transaction", Code: fmt.Sprintf("membership-remove-external-tx-%d", suffix), Status: service.OrganizationStatusActive, CreatedBy: actor.ID,
	}
	require.NoError(t, organizationRepo.Create(ctx, organizationModel))
	membershipRepo := NewOrganizationMembershipRepository(client)
	member, err := membershipRepo.Add(ctx, organizationModel.ID, target.ID, service.OrganizationRoleMember)
	require.NoError(t, err)

	externalTx, err := client.Tx(ctx)
	require.NoError(t, err)
	externalCtx := dbent.NewTxContext(ctx, externalTx)
	_, err = service.NewOrganizationMembershipService(membershipRepo, nil, nil).Remove(
		externalCtx,
		platformMemberRemovalActor(actor.ID),
		organizationModel.ID,
		target.ID,
		service.OrganizationMemberRemovalInput{},
		fmt.Sprintf("remove-external-tx-%d", suffix),
	)
	require.ErrorIs(t, err, service.ErrOrganizationMemberRemovalInvariant)
	require.NoError(t, externalTx.Rollback())

	storedUser, err := client.User.Get(ctx, target.ID)
	require.NoError(t, err)
	require.Equal(t, &organizationModel.ID, storedUser.OrganizationID)
	storedAllocation, err := client.OrganizationAllocation.Get(ctx, member.Allocation.ID)
	require.NoError(t, err)
	require.Equal(t, domain.OrganizationAllocationStatusActive, storedAllocation.Status)
}

func TestOrganizationMembershipRepositoryRemoveBlocksUnresolvedReservationsAndAllowsAfterRelease(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, client, "membership-remove-reserved")
	actor := mustCreateUser(t, client, &service.User{Email: "membership-remove-reserved-actor@example.com", Role: service.RoleAdmin})
	manager := service.NewOrganizationMembershipService(NewOrganizationMembershipRepository(client), nil, nil)

	_, err := manager.Remove(ctx, platformMemberRemovalActor(actor.ID), fixture.organization.ID, fixture.member.ID, service.OrganizationMemberRemovalInput{}, "reserved-removal")
	require.ErrorIs(t, err, service.ErrOrganizationAllocationHasReservations)
	storedUser, err := client.User.Get(ctx, fixture.member.ID)
	require.NoError(t, err)
	require.Equal(t, &fixture.organization.ID, storedUser.OrganizationID)

	final, err := fixture.repository.Terminalize(ctx, service.OrganizationTerminalizationCommand{
		ExecutionID:     fixture.command.ExecutionID,
		CorrelationID:   fixture.command.CorrelationID,
		OrganizationID:  fixture.organization.ID,
		UserID:          fixture.member.ID,
		APIKeyID:        fixture.key.ID,
		AllocationID:    fixture.allocation.ID,
		ReservationID:   fixture.reserved.ReservationID,
		TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError,
		ZeroProof:       "provider confirmed no billable result",
		OccurredAt:      time.Now().UTC(),
	})
	require.NoError(t, err)
	require.Equal(t, service.OrganizationReservationStateReleased, final.ReservationState)

	result, err := manager.Remove(ctx, platformMemberRemovalActor(actor.ID), fixture.organization.ID, fixture.member.ID, service.OrganizationMemberRemovalInput{}, "reserved-removal")
	require.NoError(t, err)
	require.Equal(t, "2.00000000", result.Reclaimed)
	storedAllocation, err := client.OrganizationAllocation.Get(ctx, fixture.allocation.ID)
	require.NoError(t, err)
	require.Equal(t, domain.OrganizationAllocationStatusClosed, storedAllocation.Status)
}

func TestOrganizationMembershipRepositoryRemoveRacesWithTerminalizationWithoutDoubleReclaim(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := time.Now().UnixNano()
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, client, fmt.Sprintf("rm-term-%d", suffix))
	actor := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-remove-terminalize-race-actor-%d@example.com", suffix), Role: service.RoleAdmin})
	manager := service.NewOrganizationMembershipService(NewOrganizationMembershipRepository(client), nil, nil)
	idempotencyKey := fmt.Sprintf("terminalize-race-removal-%d", suffix)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var terminalErr, removeErr error
	var removal *service.OrganizationMemberRemovalResult
	go func() {
		defer wg.Done()
		<-start
		_, terminalErr = fixture.repository.Terminalize(ctx, service.OrganizationTerminalizationCommand{
			ExecutionID:     fixture.command.ExecutionID,
			CorrelationID:   fixture.command.CorrelationID,
			OrganizationID:  fixture.organization.ID,
			UserID:          fixture.member.ID,
			APIKeyID:        fixture.key.ID,
			AllocationID:    fixture.allocation.ID,
			ReservationID:   fixture.reserved.ReservationID,
			TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError,
			ZeroProof:       "provider confirmed no billable result",
			OccurredAt:      time.Now().UTC(),
		})
	}()
	go func() {
		defer wg.Done()
		<-start
		removal, removeErr = manager.Remove(
			ctx,
			platformMemberRemovalActor(actor.ID),
			fixture.organization.ID,
			fixture.member.ID,
			service.OrganizationMemberRemovalInput{},
			idempotencyKey,
		)
	}()
	close(start)
	wg.Wait()

	require.NoError(t, terminalErr)
	if removeErr != nil {
		require.ErrorIs(t, removeErr, service.ErrOrganizationAllocationHasReservations)
		removal, removeErr = manager.Remove(
			ctx,
			platformMemberRemovalActor(actor.ID),
			fixture.organization.ID,
			fixture.member.ID,
			service.OrganizationMemberRemovalInput{},
			idempotencyKey,
		)
	}
	require.NoError(t, removeErr)
	require.Equal(t, "2.00000000", removal.Reclaimed)

	storedAllocation, err := client.OrganizationAllocation.Get(ctx, fixture.allocation.ID)
	require.NoError(t, err)
	require.Equal(t, domain.OrganizationAllocationStatusClosed, storedAllocation.Status)
	storedReservation, err := client.OrganizationReservation.Get(ctx, fixture.reserved.ReservationID)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationReservationStateReleased, storedReservation.Status)
	operationCount, err := client.OrganizationLedgerOperation.Query().
		Where(
			organizationledgeroperation.OrganizationIDEQ(fixture.organization.ID),
			organizationledgeroperation.OperationTypeEQ(domain.OrganizationFundingOperationMemberRemovalReclaim),
		).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, operationCount)
}

func TestOrganizationMembershipRepositoryRemoveBlocksPendingReconciliation(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, client, "membership-remove-pending")
	actor := mustCreateUser(t, client, &service.User{Email: "membership-remove-pending-actor@example.com", Role: service.RoleAdmin})
	account := mustCreateAccount(t, client, &service.Account{Name: "membership pending reconciliation account"})
	prepareOrganizationPendingReconciliationIntegration(t, ctx, fixture, account.ID)
	manager := service.NewOrganizationMembershipService(NewOrganizationMembershipRepository(client), nil, nil)

	_, err := manager.Remove(ctx, platformMemberRemovalActor(actor.ID), fixture.organization.ID, fixture.member.ID, service.OrganizationMemberRemovalInput{}, "pending-removal")
	require.ErrorIs(t, err, service.ErrOrganizationAllocationHasReservations)
	storedAllocation, err := client.OrganizationAllocation.Get(ctx, fixture.allocation.ID)
	require.NoError(t, err)
	require.Equal(t, domain.OrganizationAllocationStatusActive, storedAllocation.Status)
	require.Equal(t, "1.00000000", storedAllocation.Reserved.StringFixed(8))
}

func TestOrganizationMembershipRepositoryRemoveEnforcesOrganizationAdminAndArchivedBoundaries(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	platformActor := mustCreateUser(t, client, &service.User{Email: "membership-remove-platform@example.com", Role: service.RoleAdmin})
	organizationActor := mustCreateUser(t, client, &service.User{Email: "membership-remove-org-admin@example.com"})
	protectedAdmin := mustCreateUser(t, client, &service.User{Email: "membership-remove-protected@example.com"})
	member := mustCreateUser(t, client, &service.User{Email: "membership-remove-ordinary@example.com"})
	archivedMember := mustCreateUser(t, client, &service.User{Email: "membership-remove-archived@example.com"})
	foreignMember := mustCreateUser(t, client, &service.User{Email: "membership-remove-foreign@example.com"})
	organizationRepo := NewOrganizationRepository(client)
	organizationModel := &service.Organization{Name: "Boundary", Code: "membership-remove-boundary", Status: service.OrganizationStatusActive, CreatedBy: platformActor.ID}
	foreignOrganization := &service.Organization{Name: "Foreign", Code: "membership-remove-foreign", Status: service.OrganizationStatusActive, CreatedBy: platformActor.ID}
	require.NoError(t, organizationRepo.Create(ctx, organizationModel))
	require.NoError(t, organizationRepo.Create(ctx, foreignOrganization))
	repo := NewOrganizationMembershipRepository(client)
	_, err := repo.Add(ctx, organizationModel.ID, organizationActor.ID, service.OrganizationRoleAdmin)
	require.NoError(t, err)
	_, err = repo.Add(ctx, organizationModel.ID, protectedAdmin.ID, service.OrganizationRoleAdmin)
	require.NoError(t, err)
	_, err = repo.Add(ctx, organizationModel.ID, member.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	_, err = repo.Add(ctx, organizationModel.ID, archivedMember.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	_, err = repo.Add(ctx, foreignOrganization.ID, foreignMember.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	manager := service.NewOrganizationMembershipService(repo, nil, nil)
	organizationRemovalActor := service.OrganizationMemberRemovalActor{
		UserID: organizationActor.ID, DisplayName: "organization-admin", OrganizationRole: service.OrganizationRoleAdmin,
		AuthorizationScope: domain.OrganizationFundingAuthorizationOrganizationJWT, CurrentOrganizationID: organizationModel.ID, Active: true,
	}

	_, err = manager.Remove(ctx, organizationRemovalActor, organizationModel.ID, protectedAdmin.ID, service.OrganizationMemberRemovalInput{}, "protected-admin")
	require.ErrorIs(t, err, service.ErrOrganizationMemberRemovalForbidden)
	_, err = manager.Remove(ctx, organizationRemovalActor, organizationModel.ID, organizationActor.ID, service.OrganizationMemberRemovalInput{}, "self")
	require.ErrorIs(t, err, service.ErrOrganizationMemberRemovalForbidden)
	_, err = manager.Remove(ctx, organizationRemovalActor, organizationModel.ID, foreignMember.ID, service.OrganizationMemberRemovalInput{}, "foreign")
	require.ErrorIs(t, err, service.ErrOrganizationMemberNotFound)
	_, err = manager.Remove(ctx, organizationRemovalActor, organizationModel.ID, member.ID, service.OrganizationMemberRemovalInput{}, "ordinary")
	require.NoError(t, err)

	_, err = client.Organization.UpdateOneID(organizationModel.ID).SetStatus(service.OrganizationStatusArchived).Save(ctx)
	require.NoError(t, err)
	_, err = manager.Remove(ctx, platformMemberRemovalActor(platformActor.ID), organizationModel.ID, archivedMember.ID, service.OrganizationMemberRemovalInput{}, "archived-zero")
	require.NoError(t, err)
}

func TestOrganizationMembershipRepositoryRemoveRacesWithAPIKeyCreateAndAllowsLaterDelete(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := time.Now().UnixNano()
	actor := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-remove-key-actor-%d@example.com", suffix), Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-remove-key-target-%d@example.com", suffix)})
	organizationRepo := NewOrganizationRepository(client)
	organizationModel := &service.Organization{
		Name: "Remove Key Race", Code: fmt.Sprintf("membership-remove-key-race-%d", suffix), Status: service.OrganizationStatusActive, CreatedBy: actor.ID,
	}
	require.NoError(t, organizationRepo.Create(ctx, organizationModel))
	membershipRepo := NewOrganizationMembershipRepository(client)
	member, err := membershipRepo.Add(ctx, organizationModel.ID, target.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	manager := service.NewOrganizationMembershipService(membershipRepo, nil, nil)
	apiKeyRepo := NewAPIKeyRepository(client, integrationDB)
	key := &service.APIKey{
		UserID: target.ID, Key: fmt.Sprintf("sk-membership-remove-race-%d", suffix), Name: "remove race", Status: service.StatusAPIKeyActive,
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var removeErr, keyErr error
	go func() {
		defer wg.Done()
		<-start
		_, removeErr = manager.Remove(ctx, platformMemberRemovalActor(actor.ID), organizationModel.ID, target.ID, service.OrganizationMemberRemovalInput{}, fmt.Sprintf("remove-race-%d", suffix))
	}()
	go func() {
		defer wg.Done()
		<-start
		keyErr = apiKeyRepo.Create(ctx, key)
	}()
	close(start)
	wg.Wait()

	require.NoError(t, removeErr)
	require.NoError(t, keyErr)
	storedUser, err := client.User.Get(ctx, target.ID)
	require.NoError(t, err)
	require.Nil(t, storedUser.OrganizationID)
	storedKey, err := client.APIKey.Get(ctx, key.ID)
	require.NoError(t, err)
	require.Nil(t, storedKey.OrganizationID)
	storedAllocation, err := client.OrganizationAllocation.Get(ctx, member.Allocation.ID)
	require.NoError(t, err)
	require.Equal(t, domain.OrganizationAllocationStatusClosed, storedAllocation.Status)

	userRepo := NewUserRepository(client, integrationDB)
	require.NoError(t, userRepo.Delete(ctx, target.ID))
	storedAllocation, err = client.OrganizationAllocation.Get(ctx, member.Allocation.ID)
	require.NoError(t, err)
	require.Equal(t, domain.OrganizationAllocationStatusClosed, storedAllocation.Status)
	require.NotNil(t, storedAllocation.UserRefID)
	require.Equal(t, target.ID, *storedAllocation.UserRefID)
}

func platformMemberRemovalActor(userID int64) service.OrganizationMemberRemovalActor {
	return service.OrganizationMemberRemovalActor{
		UserID: userID, DisplayName: "platform-admin", PlatformRole: service.RoleAdmin,
		AuthorizationScope: domain.OrganizationFundingAuthorizationPlatformAdminJWT, Active: true,
	}
}

func TestOrganizationMembershipRepositoryAddCurrentMemberUsesNormalizedEmailAndRequiresActiveOrganization(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	creator := mustCreateUser(t, client, &service.User{Email: "current-member-creator@example.com", Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: "current-member-target@example.com", Role: service.RoleUser})
	organizationRepo := NewOrganizationRepository(client)
	active := &service.Organization{Name: "Current Member", Code: "current-member", Status: service.OrganizationStatusActive, CreatedBy: creator.ID}
	disabled := &service.Organization{Name: "Disabled Current Member", Code: "disabled-current-member", Status: service.OrganizationStatusDisabled, CreatedBy: creator.ID}
	require.NoError(t, organizationRepo.Create(ctx, active))
	require.NoError(t, organizationRepo.Create(ctx, disabled))

	repo := NewOrganizationMembershipRepository(client)
	member, err := repo.(service.CurrentOrganizationMembershipRepository).AddCurrentMember(ctx, active.ID, service.OrganizationMemberTarget{Email: " CURRENT-MEMBER-TARGET@EXAMPLE.COM "})

	require.NoError(t, err)
	require.Equal(t, target.ID, member.UserID)
	require.Equal(t, service.OrganizationRoleMember, member.OrganizationRole)
	require.Equal(t, "0.00000000", member.Allocation.Available)
	require.Equal(t, "0.00000000", member.Allocation.Reserved)

	disabledTarget := mustCreateUser(t, client, &service.User{Email: "disabled-current-member-target@example.com"})
	_, err = repo.(service.CurrentOrganizationMembershipRepository).AddCurrentMember(ctx, disabled.ID, service.OrganizationMemberTarget{UserID: &disabledTarget.ID})
	require.ErrorIs(t, err, service.ErrOrganizationUnavailable)
}

func TestOrganizationMembershipRepositoryAddCurrentMemberUsesOneIneligibleErrorForAllTargetStates(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	creator := mustCreateUser(t, client, &service.User{Email: "current-member-ineligible-creator@example.com", Role: service.RoleAdmin})
	organizationRepo := NewOrganizationRepository(client)
	organization := &service.Organization{Name: "Current Ineligible", Code: "current-member-ineligible", Status: service.OrganizationStatusActive, CreatedBy: creator.ID}
	other := &service.Organization{Name: "Other Ineligible", Code: "other-current-member-ineligible", Status: service.OrganizationStatusActive, CreatedBy: creator.ID}
	require.NoError(t, organizationRepo.Create(ctx, organization))
	require.NoError(t, organizationRepo.Create(ctx, other))

	organized := mustCreateUser(t, client, &service.User{Email: "current-member-organized@example.com"})
	deleted := mustCreateUser(t, client, &service.User{Email: "current-member-deleted@example.com"})
	allocated := mustCreateUser(t, client, &service.User{Email: "current-member-allocated@example.com"})
	repo := NewOrganizationMembershipRepository(client)
	currentRepo := repo.(service.CurrentOrganizationMembershipRepository)
	_, err := repo.Add(ctx, other.ID, organized.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(deleted.ID).
		SetDeletedAt(time.Now()).
		Save(mixins.AuthorizeUserDeletion(ctx))
	require.NoError(t, err)
	_, err = client.OrganizationAllocation.Create().
		SetOrganizationID(other.ID).
		SetUserIDSnapshot(allocated.ID).
		SetUserRefID(allocated.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)

	targets := map[string]service.OrganizationMemberTarget{
		"missing":    {Email: "current-member-missing@example.com"},
		"organized":  {UserID: &organized.ID},
		"deleted":    {Email: "CURRENT-MEMBER-DELETED@EXAMPLE.COM"},
		"allocation": {UserID: &allocated.ID},
	}
	var signature string
	for name, target := range targets {
		t.Run(name, func(t *testing.T) {
			_, addErr := currentRepo.AddCurrentMember(ctx, organization.ID, target)
			require.ErrorIs(t, addErr, service.ErrOrganizationMemberIneligible)
			status, body := infraerrors.ToHTTP(addErr)
			current := fmt.Sprintf("%d|%d|%s|%s", status, body.Code, body.Reason, body.Message)
			if signature == "" {
				signature = current
			}
			require.Equal(t, signature, current)
		})
	}
}

func TestOrganizationMembershipRepositoryAllowsDisabledAndRejectsArchived(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	creator := mustCreateUser(t, client, &service.User{Email: "membership-state-creator@example.com", Role: service.RoleAdmin})
	disabledTarget := mustCreateUser(t, client, &service.User{Email: "membership-disabled-target@example.com"})
	archivedTarget := mustCreateUser(t, client, &service.User{Email: "membership-archived-target@example.com"})

	organizationRepo := NewOrganizationRepository(client)
	disabled := &service.Organization{Name: "Disabled", Code: "membership-disabled", Status: service.OrganizationStatusDisabled, CreatedBy: creator.ID}
	archived := &service.Organization{Name: "Archived", Code: "membership-archived", Status: service.OrganizationStatusActive, CreatedBy: creator.ID}
	require.NoError(t, organizationRepo.Create(ctx, disabled))
	require.NoError(t, organizationRepo.Create(ctx, archived))
	_, err := client.Organization.UpdateOneID(archived.ID).SetStatus(service.OrganizationStatusArchived).Save(ctx)
	require.NoError(t, err)

	repo := NewOrganizationMembershipRepository(client)
	_, err = repo.Add(ctx, disabled.ID, disabledTarget.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	_, err = repo.Add(ctx, archived.ID, archivedTarget.ID, service.OrganizationRoleMember)
	require.ErrorIs(t, err, service.ErrOrganizationArchived)
}

func TestOrganizationMembershipRepositoryUsesSameIneligibleErrorForMissingAndOrganizedTargets(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	creator := mustCreateUser(t, client, &service.User{Email: "membership-ineligible-creator@example.com", Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: "membership-ineligible-target@example.com"})
	organizationRepo := NewOrganizationRepository(client)
	first := &service.Organization{Name: "First", Code: "membership-first", Status: service.OrganizationStatusActive, CreatedBy: creator.ID}
	second := &service.Organization{Name: "Second", Code: "membership-second", Status: service.OrganizationStatusActive, CreatedBy: creator.ID}
	require.NoError(t, organizationRepo.Create(ctx, first))
	require.NoError(t, organizationRepo.Create(ctx, second))

	repo := NewOrganizationMembershipRepository(client)
	_, err := repo.Add(ctx, first.ID, target.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	_, organizedErr := repo.Add(ctx, second.ID, target.ID, service.OrganizationRoleMember)
	_, missingErr := repo.Add(ctx, second.ID, 9_999_999, service.OrganizationRoleMember)

	require.ErrorIs(t, organizedErr, service.ErrOrganizationMemberIneligible)
	require.ErrorIs(t, missingErr, service.ErrOrganizationMemberIneligible)
}

func TestOrganizationMembershipRepositoryRoleChangeKeepsAllocationAndListIsScoped(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	creator := mustCreateUser(t, client, &service.User{Email: "membership-role-creator@example.com", Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: "membership-role-target@example.com", Role: service.RoleUser})
	otherTarget := mustCreateUser(t, client, &service.User{Email: "membership-role-other@example.com", Role: service.RoleUser})
	organizationRepo := NewOrganizationRepository(client)
	organization := &service.Organization{Name: "Role", Code: "membership-role", Status: service.OrganizationStatusActive, CreatedBy: creator.ID}
	other := &service.Organization{Name: "Other", Code: "membership-role-other", Status: service.OrganizationStatusActive, CreatedBy: creator.ID}
	require.NoError(t, organizationRepo.Create(ctx, organization))
	require.NoError(t, organizationRepo.Create(ctx, other))

	repo := NewOrganizationMembershipRepository(client)
	created, err := repo.Add(ctx, organization.ID, target.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	_, err = repo.Add(ctx, other.ID, otherTarget.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	changed, err := repo.ChangeRole(ctx, organization.ID, target.ID, service.OrganizationRoleAdmin)
	require.NoError(t, err)
	require.Equal(t, created.Allocation.ID, changed.Allocation.ID)
	require.Equal(t, service.OrganizationRoleAdmin, changed.OrganizationRole)

	items, page, err := repo.List(ctx, organization.ID, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, items, 1)
	require.Equal(t, target.ID, items[0].UserID)
	require.Equal(t, service.OrganizationRoleAdmin, items[0].OrganizationRole)

	_, err = client.Organization.UpdateOneID(organization.ID).SetStatus(service.OrganizationStatusArchived).SetUpdatedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	_, err = repo.ChangeRole(ctx, organization.ID, target.ID, service.OrganizationRoleMember)
	require.ErrorIs(t, err, service.ErrOrganizationArchived)
}

func TestOrganizationMembershipRepositoryRollsBackWhenKeyAttributionFails(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	creator := mustCreateUser(t, client, &service.User{Email: "membership-rollback-creator@example.com", Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: "membership-rollback-target@example.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: target.ID, Key: "sk-membership-rollback", Name: "rollback"})
	organizationRepo := NewOrganizationRepository(client)
	organization := &service.Organization{Name: "Rollback", Code: "membership-rollback", Status: service.OrganizationStatusActive, CreatedBy: creator.ID}
	require.NoError(t, organizationRepo.Create(ctx, organization))

	_, err := integrationDB.ExecContext(ctx, `
		CREATE FUNCTION fail_membership_key_attribution() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'forced membership key attribution failure';
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_membership_key_attribution_trigger
		BEFORE UPDATE OF organization_id ON api_keys
		FOR EACH ROW EXECUTE FUNCTION fail_membership_key_attribution();
	`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `
			DROP TRIGGER IF EXISTS fail_membership_key_attribution_trigger ON api_keys;
			DROP FUNCTION IF EXISTS fail_membership_key_attribution();
		`)
	})

	repo := NewOrganizationMembershipRepository(client)
	_, err = repo.Add(ctx, organization.ID, target.ID, service.OrganizationRoleMember)
	require.Error(t, err)

	storedUser, err := client.User.Query().Where(dbuser.IDEQ(target.ID)).Only(ctx)
	require.NoError(t, err)
	require.Nil(t, storedUser.OrganizationID)
	require.Nil(t, storedUser.OrganizationRole)
	allocationExists, err := client.OrganizationAllocation.Query().
		Where(organizationallocation.UserIDSnapshotEQ(target.ID)).
		Exist(ctx)
	require.NoError(t, err)
	require.False(t, allocationExists)
	storedKey, err := client.APIKey.Query().Where(apikey.IDEQ(key.ID)).Only(ctx)
	require.NoError(t, err)
	require.Nil(t, storedKey.OrganizationID)
}

func TestOrganizationMembershipRepositoryJoinRacesWithAPIKeyCreate(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := time.Now().UnixNano()
	creator := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-key-race-creator-%d@example.com", suffix), Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-key-race-target-%d@example.com", suffix)})
	organizationRepo := NewOrganizationRepository(client)
	organization := &service.Organization{
		Name: "Key Race", Code: fmt.Sprintf("membership-key-race-%d", suffix), Status: service.OrganizationStatusActive, CreatedBy: creator.ID,
	}
	require.NoError(t, organizationRepo.Create(ctx, organization))

	membershipRepo := NewOrganizationMembershipRepository(client)
	apiKeyRepo := NewAPIKeyRepository(client, integrationDB)
	key := &service.APIKey{
		UserID: target.ID,
		Key:    fmt.Sprintf("sk-membership-key-race-%d", suffix),
		Name:   "race",
		Status: service.StatusActive,
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var membershipErr, keyErr error
	go func() {
		defer wg.Done()
		<-start
		_, membershipErr = membershipRepo.Add(ctx, organization.ID, target.ID, service.OrganizationRoleMember)
	}()
	go func() {
		defer wg.Done()
		<-start
		keyErr = apiKeyRepo.Create(ctx, key)
	}()
	close(start)
	wg.Wait()

	require.NoError(t, membershipErr)
	require.NoError(t, keyErr)
	storedUser, err := client.User.Get(ctx, target.ID)
	require.NoError(t, err)
	require.Equal(t, &organization.ID, storedUser.OrganizationID)
	storedKey, err := client.APIKey.Get(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, &organization.ID, storedKey.OrganizationID)
	allocationCount, err := client.OrganizationAllocation.Query().
		Where(
			organizationallocation.UserIDSnapshotEQ(target.ID),
			organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
		).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, allocationCount)
}

func TestOrganizationMembershipRepositoryJoinRacesWithUserDelete(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := time.Now().UnixNano()
	creator := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-delete-race-creator-%d@example.com", suffix), Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-delete-race-target-%d@example.com", suffix)})
	organizationRepo := NewOrganizationRepository(client)
	organization := &service.Organization{
		Name: "Delete Race", Code: fmt.Sprintf("membership-delete-race-%d", suffix), Status: service.OrganizationStatusActive, CreatedBy: creator.ID,
	}
	require.NoError(t, organizationRepo.Create(ctx, organization))

	membershipRepo := NewOrganizationMembershipRepository(client)
	userRepo := NewUserRepository(client, integrationDB)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var membershipErr, deleteErr error
	go func() {
		defer wg.Done()
		<-start
		_, membershipErr = membershipRepo.Add(ctx, organization.ID, target.ID, service.OrganizationRoleMember)
	}()
	go func() {
		defer wg.Done()
		<-start
		deleteErr = userRepo.Delete(ctx, target.ID)
	}()
	close(start)
	wg.Wait()

	switch {
	case membershipErr == nil:
		require.ErrorIs(t, deleteErr, service.ErrOrganizationMembershipMustBeRemoved)
		storedUser, err := client.User.Get(ctx, target.ID)
		require.NoError(t, err)
		require.Equal(t, &organization.ID, storedUser.OrganizationID)
	case deleteErr == nil:
		require.ErrorIs(t, membershipErr, service.ErrOrganizationMemberIneligible)
		_, err := client.User.Get(ctx, target.ID)
		require.True(t, dbent.IsNotFound(err))
	default:
		t.Fatalf("unexpected concurrent result: membership=%v delete=%v", membershipErr, deleteErr)
	}
}

func TestUserDeletionGuardCompetesWithMembershipJoinLock(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := time.Now().UnixNano()
	creator := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-lock-creator-%d@example.com", suffix), Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-lock-target-%d@example.com", suffix)})
	organizationRepo := NewOrganizationRepository(client)
	organization := &service.Organization{
		Name: "Delete Lock", Code: fmt.Sprintf("membership-lock-%d", suffix), Status: service.OrganizationStatusActive, CreatedBy: creator.ID,
	}
	require.NoError(t, organizationRepo.Create(ctx, organization))

	joinTx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = joinTx.Rollback() }()
	joinCtx := dbent.NewTxContext(ctx, joinTx)
	_, err = joinTx.Client().Organization.Query().Where(dborganization.IDEQ(organization.ID)).ForUpdate().Only(joinCtx)
	require.NoError(t, err)
	_, err = joinTx.Client().User.Query().Where(dbuser.IDEQ(target.ID)).ForUpdate().Only(joinCtx)
	require.NoError(t, err)

	deleteTx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = deleteTx.Rollback() }()
	var result entsql.Result
	require.NoError(t, deleteTx.Client().Driver().Exec(ctx, "SET LOCAL lock_timeout = '100ms'", []any{}, &result))

	err = deleteTx.Client().User.DeleteOneID(target.ID).Exec(mixins.AuthorizeUserDeletion(ctx))
	var pqErr *pq.Error
	require.ErrorAs(t, err, &pqErr)
	require.Equal(t, pq.ErrorCode("55P03"), pqErr.Code)
	require.NoError(t, deleteTx.Rollback())

	membershipRepo := NewOrganizationMembershipRepository(client)
	_, err = membershipRepo.Add(joinCtx, organization.ID, target.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	require.NoError(t, joinTx.Commit())

	userRepo := NewUserRepository(client, integrationDB)
	err = userRepo.Delete(ctx, target.ID)
	require.ErrorIs(t, err, service.ErrOrganizationMembershipMustBeRemoved)
}

func TestUserDeletionGuardEmptyTargetDoesNotDeleteLaterOrganizationMember(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := time.Now().UnixNano()
	targetEmail := fmt.Sprintf("membership-phantom-target-%d@example.com", suffix)
	creator := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("membership-phantom-creator-%d@example.com", suffix), Role: service.RoleAdmin})
	organization, err := client.Organization.Create().
		SetName("Deletion Phantom Guard").
		SetCode(fmt.Sprintf("deletion-phantom-guard-%d", suffix)).
		SetStatus(service.OrganizationStatusActive).
		SetCreatedBy(creator.ID).
		Save(ctx)
	require.NoError(t, err)
	var targetID int64
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM organization_allocations WHERE organization_id = $1", organization.ID)
		if targetID != 0 {
			_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", targetID)
		}
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM organizations WHERE id = $1", organization.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", creator.ID)
	})

	guardPassed := make(chan struct{})
	resumeMutation := make(chan struct{})
	var resumeOnce sync.Once
	defer func() {
		resumeOnce.Do(func() { close(resumeMutation) })
	}()
	originalHook := dbuser.Hooks[1]
	dbuser.Hooks[1] = func(next dbent.Mutator) dbent.Mutator {
		pauseAfterGuard := dbent.MutateFunc(func(hookCtx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
			userMutation, ok := mutation.(*dbent.UserMutation)
			if !ok {
				return next.Mutate(hookCtx, mutation)
			}
			if _, deleting := userMutation.DeletedAt(); !deleting {
				return next.Mutate(hookCtx, mutation)
			}
			close(guardPassed)
			<-resumeMutation
			return next.Mutate(hookCtx, mutation)
		})
		return originalHook(pauseAfterGuard)
	}
	t.Cleanup(func() { dbuser.Hooks[1] = originalHook })

	deleteTx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = deleteTx.Rollback() }()
	deleteCtx := dbent.NewTxContext(ctx, deleteTx)
	type deleteResult struct {
		affected int
		err      error
	}
	deletion := make(chan deleteResult, 1)
	go func() {
		affected, deleteErr := deleteTx.Client().User.Update().
			Where(dbuser.EmailEQ(targetEmail)).
			SetDeletedAt(time.Now()).
			Save(mixins.AuthorizeUserDeletion(deleteCtx))
		deletion <- deleteResult{affected: affected, err: deleteErr}
	}()

	select {
	case <-guardPassed:
	case <-time.After(5 * time.Second):
		t.Fatal("user deletion guard did not reach the post-guard barrier")
	}

	target := mustCreateUser(t, client, &service.User{Email: targetEmail})
	targetID = target.ID
	membershipRepo := NewOrganizationMembershipRepository(client)
	_, err = membershipRepo.Add(ctx, organization.ID, target.ID, service.OrganizationRoleMember)
	require.NoError(t, err)

	resumeOnce.Do(func() { close(resumeMutation) })
	result := <-deletion
	require.NoError(t, result.err)
	require.Zero(t, result.affected)
	require.NoError(t, deleteTx.Commit())

	stored, err := client.User.Get(ctx, target.ID)
	require.NoError(t, err)
	require.Equal(t, &organization.ID, stored.OrganizationID)
	require.Nil(t, stored.DeletedAt)
}
