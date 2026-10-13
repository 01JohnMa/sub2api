//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/organization"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func fundingActorForUser(user *service.User) service.OrganizationFundingActor {
	displayName := user.Username
	if displayName == "" {
		displayName = "platform-admin"
	}
	return service.OrganizationFundingActor{
		UserID:             user.ID,
		DisplayName:        displayName,
		PlatformRole:       domain.RoleAdmin,
		AuthorizationScope: domain.OrganizationFundingAuthorizationPlatformAdminJWT,
		Active:             true,
	}
}

func createFundingOrganization(t *testing.T, ctx context.Context, client *dbent.Client, suffix string, status string) (*service.User, *service.Organization) {
	t.Helper()
	creator := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("funding-admin-%s@example.com", suffix),
		Role:  domain.RoleAdmin,
	})
	organizationModel := &service.Organization{
		Name:      "Funding " + suffix,
		Code:      "funding-" + suffix,
		Status:    status,
		CreatedBy: creator.ID,
	}
	require.NoError(t, NewOrganizationRepository(client).Create(ctx, organizationModel))
	return creator, organizationModel
}

func TestOrganizationFundingRepositoryCreditsActiveAndDisabledAndRejectsArchived(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	creator, organizationModel := createFundingOrganization(t, ctx, tx.Client(), "states", domain.OrganizationStatusActive)
	funding := service.NewOrganizationFundingService(NewOrganizationFundingRepository(tx.Client()))

	first, err := funding.Credit(ctx, fundingActorForUser(creator), organizationModel.ID, "0.10000000", service.OrganizationFundingReasons{InternalReason: "first credit"}, "funding-states-first")
	require.NoError(t, err)
	require.Equal(t, "0.00000000", first.PoolBefore)
	require.Equal(t, "0.10000000", first.PoolAfter)

	_, err = tx.Client().Organization.UpdateOneID(organizationModel.ID).SetStatus(domain.OrganizationStatusDisabled).Save(ctx)
	require.NoError(t, err)
	second, err := funding.Credit(ctx, fundingActorForUser(creator), organizationModel.ID, "0.20000000", service.OrganizationFundingReasons{InternalReason: "disabled credit"}, "funding-states-disabled")
	require.NoError(t, err)
	require.Equal(t, "0.30000000", second.PoolAfter)

	summary, err := funding.GetSummary(ctx, organizationModel.ID)
	require.NoError(t, err)
	require.Equal(t, domain.OrganizationStatusDisabled, summary.Status)
	require.Equal(t, "0.30000000", summary.Unallocated)
	require.Equal(t, "0.30000000", summary.TotalRemaining)
	require.Equal(t, "0.00000000", summary.Available)
	require.Equal(t, "0.00000000", summary.Reserved)
	require.Equal(t, "0.00000000", summary.Consumed)

	_, err = tx.Client().Organization.UpdateOneID(organizationModel.ID).SetStatus(domain.OrganizationStatusArchived).Save(ctx)
	require.NoError(t, err)
	_, err = funding.Credit(ctx, fundingActorForUser(creator), organizationModel.ID, "1", service.OrganizationFundingReasons{InternalReason: "must reject"}, "funding-states-archived")
	require.ErrorIs(t, err, service.ErrOrganizationArchived)

	entries, page, err := funding.ListLedger(ctx, organizationModel.ID, pagination.PaginationParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.Len(t, entries, 2)
	for _, entry := range entries {
		require.Equal(t, organizationModel.ID, entry.OrganizationID)
		require.Equal(t, domain.OrganizationFundingOperationCredit, entry.OperationType)
	}
}

func TestOrganizationFundingRepositoryIdempotencyReplaysOnlySamePayload(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	creator, organizationModel := createFundingOrganization(t, ctx, tx.Client(), "idempotency", domain.OrganizationStatusActive)
	funding := service.NewOrganizationFundingService(NewOrganizationFundingRepository(tx.Client()))
	actor := fundingActorForUser(creator)

	first, err := funding.Credit(ctx, actor, organizationModel.ID, "1.25000000", service.OrganizationFundingReasons{InternalReason: "approved"}, "funding-idempotency-key")
	require.NoError(t, err)
	replay, err := funding.Credit(ctx, actor, organizationModel.ID, "1.25", service.OrganizationFundingReasons{InternalReason: "approved"}, "funding-idempotency-key")
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, first.OperationID, replay.OperationID)
	require.Equal(t, first.PoolAfter, replay.PoolAfter)

	_, err = funding.Credit(ctx, actor, organizationModel.ID, "1.26", service.OrganizationFundingReasons{InternalReason: "approved"}, "funding-idempotency-key")
	require.ErrorIs(t, err, service.ErrIdempotencyKeyConflict)

	summary, err := funding.GetSummary(ctx, organizationModel.ID)
	require.NoError(t, err)
	require.Equal(t, "1.25000000", summary.Unallocated)

	_, err = tx.Client().Organization.UpdateOneID(organizationModel.ID).SetStatus(domain.OrganizationStatusArchived).Save(ctx)
	require.NoError(t, err)
	_, err = funding.Credit(ctx, actor, organizationModel.ID, "1.25", service.OrganizationFundingReasons{InternalReason: "approved"}, "funding-idempotency-key")
	require.ErrorIs(t, err, service.ErrOrganizationArchived)
}

func TestOrganizationFundingRepositoryScopesSummaryAndLedgerToOrganization(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	creatorA, organizationA := createFundingOrganization(t, ctx, tx.Client(), "scope-a", domain.OrganizationStatusActive)
	creatorB, organizationB := createFundingOrganization(t, ctx, tx.Client(), "scope-b", domain.OrganizationStatusActive)
	funding := service.NewOrganizationFundingService(NewOrganizationFundingRepository(tx.Client()))

	_, err := funding.Credit(ctx, fundingActorForUser(creatorA), organizationA.ID, "3", service.OrganizationFundingReasons{InternalReason: "org a"}, "funding-scope-a")
	require.NoError(t, err)
	_, err = funding.Credit(ctx, fundingActorForUser(creatorB), organizationB.ID, "7", service.OrganizationFundingReasons{InternalReason: "org b"}, "funding-scope-b")
	require.NoError(t, err)

	summary, err := funding.GetSummary(ctx, organizationA.ID)
	require.NoError(t, err)
	require.Equal(t, "3.00000000", summary.Unallocated)
	entries, page, err := funding.ListLedger(ctx, organizationA.ID, pagination.PaginationParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, entries, 1)
	require.Equal(t, organizationA.ID, entries[0].OrganizationID)
	require.NotEqual(t, organizationB.ID, entries[0].OrganizationID)
}

func TestOrganizationFundingRepositoryConcurrentCreditsRemainExact(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := testEntClient(t)
	suffix := fmt.Sprintf("concurrent-%d", time.Now().UnixNano())
	creator, organizationModel := createFundingOrganization(t, ctx, client, suffix, domain.OrganizationStatusActive)
	t.Cleanup(func() {
		cleanupOrganizationFundingConcurrentFixture(t, organizationModel.ID, creator.ID)
	})
	funding := service.NewOrganizationFundingService(NewOrganizationFundingRepository(client))
	actor := fundingActorForUser(creator)

	const credits = 12
	errCh := make(chan error, credits)
	var wg sync.WaitGroup
	for i := 0; i < credits; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := funding.Credit(
				ctx,
				actor,
				organizationModel.ID,
				"0.10000000",
				service.OrganizationFundingReasons{InternalReason: fmt.Sprintf("concurrent credit %d", index)},
				fmt.Sprintf("funding-concurrent-%d", index),
			)
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	summary, err := funding.GetSummary(ctx, organizationModel.ID)
	require.NoError(t, err)
	require.Equal(t, "1.20000000", summary.Unallocated)
	require.Equal(t, int64(credits), summary.BalanceVersion)

	entries, page, err := funding.ListLedger(ctx, organizationModel.ID, pagination.PaginationParams{Page: 1, PageSize: credits + 1})
	require.NoError(t, err)
	require.Equal(t, int64(credits), page.Total)
	require.Len(t, entries, credits)

	stored, err := client.Organization.Query().Where(organization.IDEQ(organizationModel.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "1.20000000", stored.UnallocatedPool.StringFixed(domain.OrganizationMoneyScale))
}

func cleanupOrganizationFundingConcurrentFixture(t *testing.T, organizationID, userID int64) {
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

	_, err = integrationDB.ExecContext(ctx, "DELETE FROM organization_ledger_postings WHERE organization_id = $1", organizationID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM organization_ledger_operations WHERE organization_id = $1", organizationID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM organizations WHERE id = $1", organizationID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", userID)
	require.NoError(t, err)
}
