//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/organizationallocation"
	"github.com/Wei-Shaw/sub2api/ent/organizationledgeroperation"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestOrganizationAllocationPostgresNoOpEnvelopeIsExcludedFromLedger(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createOrganizationAllocationIntegrationFixture(t, ctx, tx.Client(), "no-op-ledger", "0.00000000", "1.00000000")
	manager := newOrganizationAllocationIntegrationManager(t, tx.Client())
	preview, err := manager.Preview(ctx, fixture.actorSnapshot(), fixture.organization.ID, domain.OrganizationFundingOperationEqualAllocation, service.OrganizationAllocationPreviewInput{})
	require.NoError(t, err)
	result, err := manager.Execute(ctx, fixture.actorSnapshot(), fixture.organization.ID, domain.OrganizationFundingOperationEqualAllocation,
		service.OrganizationAllocationExecuteInput{PreviewToken: preview.PreviewToken, Confirmation: preview.Confirmation}, "postgres-no-op-ledger")
	require.NoError(t, err)
	require.Equal(t, service.OrganizationAllocationOutcomeNoOp, result.Outcome)
	require.Empty(t, result.Postings)

	entries, page, err := NewOrganizationFundingRepository(tx.Client()).ListLedger(ctx, fixture.organization.ID, pagination.PaginationParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Zero(t, page.Total)
	require.Empty(t, entries)
	operationCount, err := tx.Client().OrganizationLedgerOperation.Query().Where(organizationledgeroperation.OrganizationIDEQ(fixture.organization.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, operationCount)
	postingCount, err := tx.Client().OrganizationLedgerPosting.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, postingCount)
}

func TestOrganizationAllocationPostgresConcurrentCustomCannotOverAllocate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := testEntClient(t)
	fixture := createOrganizationAllocationIntegrationFixture(t, ctx, client, fmt.Sprintf("custom-race-%d", time.Now().UnixNano()), "10.00000000", "0.00000000")
	t.Cleanup(func() { cleanupOrganizationAllocationIntegrationFixture(t, fixture) })
	manager := newOrganizationAllocationIntegrationManager(t, client)
	input := service.OrganizationAllocationPreviewInput{Targets: []service.OrganizationAllocationTargetInput{{UserID: fixture.member.ID, Amount: "7"}}}
	previewA, err := manager.Preview(ctx, fixture.actorSnapshot(), fixture.organization.ID, domain.OrganizationFundingOperationCustomAllocation, input)
	require.NoError(t, err)
	previewB, err := manager.Preview(ctx, fixture.actorSnapshot(), fixture.organization.ID, domain.OrganizationFundingOperationCustomAllocation, input)
	require.NoError(t, err)

	errors := executeOrganizationAllocationConcurrently(ctx, manager, fixture.actorSnapshot(), fixture.organization.ID, []concurrentAllocationExecution{
		{operationType: domain.OrganizationFundingOperationCustomAllocation, key: "custom-race-a", preview: previewA},
		{operationType: domain.OrganizationFundingOperationCustomAllocation, key: "custom-race-b", preview: previewB},
	})
	requireExactlyOneAllocationCommitAndOneStale(t, errors)

	organizationEntity, err := client.Organization.Get(ctx, fixture.organization.ID)
	require.NoError(t, err)
	require.Equal(t, "3.00000000", organizationEntity.UnallocatedPool.StringFixed(domain.OrganizationMoneyScale))
	allocation, err := client.OrganizationAllocation.Query().Where(organizationallocation.UserIDSnapshotEQ(fixture.member.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "7.00000000", allocation.Available.StringFixed(domain.OrganizationMoneyScale))
	require.False(t, organizationEntity.UnallocatedPool.IsNegative())
	require.False(t, allocation.Available.IsNegative())
	operationCount, err := client.OrganizationLedgerOperation.Query().Where(organizationledgeroperation.OrganizationIDEQ(fixture.organization.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, operationCount)
}

func TestOrganizationAllocationPostgresAllocationAndReclaimSerializeAndConserve(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := testEntClient(t)
	fixture := createOrganizationAllocationIntegrationFixture(t, ctx, client, fmt.Sprintf("allocate-reclaim-race-%d", time.Now().UnixNano()), "5.00000000", "5.00000000")
	t.Cleanup(func() { cleanupOrganizationAllocationIntegrationFixture(t, fixture) })
	manager := newOrganizationAllocationIntegrationManager(t, client)
	customPreview, err := manager.Preview(ctx, fixture.actorSnapshot(), fixture.organization.ID, domain.OrganizationFundingOperationCustomAllocation,
		service.OrganizationAllocationPreviewInput{Targets: []service.OrganizationAllocationTargetInput{{UserID: fixture.member.ID, Amount: "5"}}})
	require.NoError(t, err)
	reclaimPreview, err := manager.Preview(ctx, fixture.actorSnapshot(), fixture.organization.ID, domain.OrganizationFundingOperationReclaim,
		service.OrganizationAllocationPreviewInput{Targets: []service.OrganizationAllocationTargetInput{{UserID: fixture.member.ID, Amount: "5"}}})
	require.NoError(t, err)

	errors := executeOrganizationAllocationConcurrently(ctx, manager, fixture.actorSnapshot(), fixture.organization.ID, []concurrentAllocationExecution{
		{operationType: domain.OrganizationFundingOperationCustomAllocation, key: "allocate-race", preview: customPreview},
		{operationType: domain.OrganizationFundingOperationReclaim, key: "reclaim-race", preview: reclaimPreview},
	})
	requireExactlyOneAllocationCommitAndOneStale(t, errors)

	organizationEntity, err := client.Organization.Get(ctx, fixture.organization.ID)
	require.NoError(t, err)
	allocation, err := client.OrganizationAllocation.Query().Where(organizationallocation.UserIDSnapshotEQ(fixture.member.ID)).Only(ctx)
	require.NoError(t, err)
	require.False(t, organizationEntity.UnallocatedPool.IsNegative())
	require.False(t, allocation.Available.IsNegative())
	require.Equal(t, "10.00000000", organizationEntity.UnallocatedPool.Add(allocation.Available).Add(allocation.Reserved).StringFixed(domain.OrganizationMoneyScale))
	require.Equal(t, int64(1), organizationEntity.BalanceVersion)
}

func TestOrganizationAllocationPostgresIdempotencyScopeSeparatesOrganizationAndOperation(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixtureA := createOrganizationAllocationIntegrationFixture(t, ctx, tx.Client(), "idempotency-scope-a", "5.00000000", "2.00000000")
	fixtureB := createOrganizationAllocationIntegrationFixture(t, ctx, tx.Client(), "idempotency-scope-b", "5.00000000", "2.00000000")
	manager := newOrganizationAllocationIntegrationManager(t, tx.Client())
	const rawKey = "shared-raw-key"

	previewA, err := manager.Preview(ctx, fixtureA.actorSnapshot(), fixtureA.organization.ID, domain.OrganizationFundingOperationCustomAllocation,
		service.OrganizationAllocationPreviewInput{Targets: []service.OrganizationAllocationTargetInput{{UserID: fixtureA.member.ID, Amount: "1"}}})
	require.NoError(t, err)
	inputA := service.OrganizationAllocationExecuteInput{PreviewToken: previewA.PreviewToken, Confirmation: previewA.Confirmation}
	first, err := manager.Execute(ctx, fixtureA.actorSnapshot(), fixtureA.organization.ID, domain.OrganizationFundingOperationCustomAllocation, inputA, rawKey)
	require.NoError(t, err)
	replay, err := manager.Execute(ctx, fixtureA.actorSnapshot(), fixtureA.organization.ID, domain.OrganizationFundingOperationCustomAllocation, inputA, rawKey)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, first.OperationID, replay.OperationID)

	reclaimPreview, err := manager.Preview(ctx, fixtureA.actorSnapshot(), fixtureA.organization.ID, domain.OrganizationFundingOperationReclaim,
		service.OrganizationAllocationPreviewInput{Targets: []service.OrganizationAllocationTargetInput{{UserID: fixtureA.member.ID, Amount: "1"}}})
	require.NoError(t, err)
	_, err = manager.Execute(ctx, fixtureA.actorSnapshot(), fixtureA.organization.ID, domain.OrganizationFundingOperationReclaim,
		service.OrganizationAllocationExecuteInput{PreviewToken: reclaimPreview.PreviewToken, Confirmation: reclaimPreview.Confirmation}, rawKey)
	require.NoError(t, err)

	previewB, err := manager.Preview(ctx, fixtureB.actorSnapshot(), fixtureB.organization.ID, domain.OrganizationFundingOperationCustomAllocation,
		service.OrganizationAllocationPreviewInput{Targets: []service.OrganizationAllocationTargetInput{{UserID: fixtureB.member.ID, Amount: "1"}}})
	require.NoError(t, err)
	_, err = manager.Execute(ctx, fixtureB.actorSnapshot(), fixtureB.organization.ID, domain.OrganizationFundingOperationCustomAllocation,
		service.OrganizationAllocationExecuteInput{PreviewToken: previewB.PreviewToken, Confirmation: previewB.Confirmation}, rawKey)
	require.NoError(t, err)

	operationCount, err := tx.Client().OrganizationLedgerOperation.Query().
		Where(organizationledgeroperation.OrganizationIDIn(fixtureA.organization.ID, fixtureB.organization.ID)).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, operationCount)
}

type organizationAllocationIntegrationFixture struct {
	creator      *service.User
	organization *service.Organization
	actor        *dbent.User
	member       *dbent.User
}

func (f organizationAllocationIntegrationFixture) actorSnapshot() service.OrganizationAllocationActor {
	return service.OrganizationAllocationActor{
		UserID:                f.actor.ID,
		DisplayName:           "organization-admin",
		OrganizationRole:      domain.OrganizationRoleAdmin,
		AuthorizationScope:    domain.OrganizationFundingAuthorizationOrganizationJWT,
		CurrentOrganizationID: f.organization.ID,
		Active:                true,
	}
}

func createOrganizationAllocationIntegrationFixture(t *testing.T, ctx context.Context, client *dbent.Client, suffix, pool, available string) organizationAllocationIntegrationFixture {
	t.Helper()
	creator, organizationModel := createFundingOrganization(t, ctx, client, suffix, domain.OrganizationStatusActive)
	_, err := client.Organization.UpdateOneID(organizationModel.ID).
		SetUnallocatedPool(decimal.RequireFromString(pool)).
		SetBalanceVersion(0).
		Save(ctx)
	require.NoError(t, err)
	actor, err := client.User.Create().
		SetEmail(fmt.Sprintf("allocation-admin-%s@example.com", suffix)).
		SetPasswordHash("hash").
		SetRole(domain.RoleUser).
		SetStatus(domain.StatusActive).
		SetOrganizationID(organizationModel.ID).
		SetOrganizationRole(domain.OrganizationRoleAdmin).
		Save(ctx)
	require.NoError(t, err)
	member, err := client.User.Create().
		SetEmail(fmt.Sprintf("allocation-member-%s@example.com", suffix)).
		SetPasswordHash("hash").
		SetRole(domain.RoleUser).
		SetStatus(domain.StatusActive).
		SetOrganizationID(organizationModel.ID).
		SetOrganizationRole(domain.OrganizationRoleMember).
		Save(ctx)
	require.NoError(t, err)
	for _, target := range []struct {
		userID    int64
		available decimal.Decimal
	}{
		{userID: actor.ID, available: decimal.Zero},
		{userID: member.ID, available: decimal.RequireFromString(available)},
	} {
		_, err = client.OrganizationAllocation.Create().
			SetOrganizationID(organizationModel.ID).
			SetUserIDSnapshot(target.userID).
			SetUserRefID(target.userID).
			SetStatus(domain.OrganizationAllocationStatusActive).
			SetAvailable(target.available).
			SetReserved(decimal.Zero).
			Save(ctx)
		require.NoError(t, err)
	}
	return organizationAllocationIntegrationFixture{creator: creator, organization: organizationModel, actor: actor, member: member}
}

func newOrganizationAllocationIntegrationManager(t *testing.T, client *dbent.Client) *service.OrganizationAllocationService {
	t.Helper()
	manager, err := service.NewOrganizationAllocationService(NewOrganizationAllocationRepository(client), "organization-allocation-integration-preview-secret")
	require.NoError(t, err)
	return manager
}

type concurrentAllocationExecution struct {
	operationType string
	key           string
	preview       *service.OrganizationAllocationPreview
}

func executeOrganizationAllocationConcurrently(ctx context.Context, manager *service.OrganizationAllocationService, actor service.OrganizationAllocationActor, organizationID int64, executions []concurrentAllocationExecution) []error {
	errors := make([]error, len(executions))
	var wg sync.WaitGroup
	for index := range executions {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			execution := executions[index]
			_, errors[index] = manager.Execute(ctx, actor, organizationID, execution.operationType,
				service.OrganizationAllocationExecuteInput{PreviewToken: execution.preview.PreviewToken, Confirmation: execution.preview.Confirmation}, execution.key)
		}(index)
	}
	wg.Wait()
	return errors
}

func requireExactlyOneAllocationCommitAndOneStale(t *testing.T, executionErrors []error) {
	t.Helper()
	require.Len(t, executionErrors, 2)
	committed := 0
	stale := 0
	for _, err := range executionErrors {
		if err == nil {
			committed++
			continue
		}
		if errors.Is(err, service.ErrOrganizationAllocationStalePreview) {
			stale++
			continue
		}
		require.NoError(t, err)
	}
	require.Equal(t, 1, committed)
	require.Equal(t, 1, stale)
}

func cleanupOrganizationAllocationIntegrationFixture(t *testing.T, fixture organizationAllocationIntegrationFixture) {
	t.Helper()
	ctx := context.Background()
	_, err := integrationDB.ExecContext(ctx, "ALTER TABLE organization_ledger_postings DISABLE TRIGGER USER")
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "ALTER TABLE organization_ledger_operations DISABLE TRIGGER USER")
	require.NoError(t, err)
	defer func() {
		_, _ = integrationDB.ExecContext(ctx, "ALTER TABLE organization_ledger_operations ENABLE TRIGGER USER")
		_, _ = integrationDB.ExecContext(ctx, "ALTER TABLE organization_ledger_postings ENABLE TRIGGER USER")
	}()

	_, err = integrationDB.ExecContext(ctx, "DELETE FROM organization_ledger_postings WHERE organization_id = $1", fixture.organization.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM organization_ledger_operations WHERE organization_id = $1", fixture.organization.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM organization_allocations WHERE organization_id = $1", fixture.organization.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id IN ($1, $2)", fixture.actor.ID, fixture.member.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM organizations WHERE id = $1", fixture.organization.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", fixture.creator.ID)
	require.NoError(t, err)
}
