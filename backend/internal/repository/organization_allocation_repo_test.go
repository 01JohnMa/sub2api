package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/ent/organizationallocation"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newOrganizationAllocationSQLite(t *testing.T) *dbent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", strings.ReplaceAll(t.Name(), "/", "_")))
	require.NoError(t, err)
	db.SetMaxOpenConns(10)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestOrganizationAllocationRepositoryEqualAllocationIsAtomicAndIdempotent(t *testing.T) {
	client := newOrganizationAllocationSQLite(t)
	ctx := context.Background()
	organization, actor, members := createOrganizationAllocationFixture(t, ctx, client, "3.00000000")
	repo := NewOrganizationAllocationRepository(client)
	manager, err := service.NewOrganizationAllocationService(repo, strings.Repeat("preview-secret", 3))
	require.NoError(t, err)
	actorSnapshot := service.OrganizationAllocationActor{
		UserID:                actor.ID,
		DisplayName:           "org-admin",
		OrganizationRole:      domain.OrganizationRoleAdmin,
		AuthorizationScope:    domain.OrganizationFundingAuthorizationOrganizationJWT,
		CurrentOrganizationID: organization.ID,
		Active:                true,
	}

	preview, err := manager.Preview(ctx, actorSnapshot, organization.ID, domain.OrganizationFundingOperationEqualAllocation, service.OrganizationAllocationPreviewInput{})
	require.NoError(t, err)
	require.Len(t, preview.Confirmation.Targets, len(members))
	require.Equal(t, "1.00000000", preview.Confirmation.Targets[0].Delta)
	require.Equal(t, "0.00000000", preview.Confirmation.PoolAfter)

	input := service.OrganizationAllocationExecuteInput{PreviewToken: preview.PreviewToken, Confirmation: preview.Confirmation}
	result, err := manager.Execute(ctx, actorSnapshot, organization.ID, domain.OrganizationFundingOperationEqualAllocation, input, "equal-1")
	require.NoError(t, err)
	require.Equal(t, service.OrganizationAllocationOutcomeCommitted, result.Outcome)
	require.Len(t, result.Postings, 1+len(members))

	replayed, err := manager.Execute(ctx, actorSnapshot, organization.ID, domain.OrganizationFundingOperationEqualAllocation, input, "equal-1")
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, result.OperationID, replayed.OperationID)
	require.Equal(t, result.CorrelationID, replayed.CorrelationID)

	changed := input
	changed.Confirmation.PublicReason = "different payload"
	_, err = manager.Execute(ctx, actorSnapshot, organization.ID, domain.OrganizationFundingOperationEqualAllocation, changed, "equal-1")
	require.ErrorIs(t, err, service.ErrIdempotencyKeyConflict)

	storedOrganization, err := client.Organization.Get(ctx, organization.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), storedOrganization.BalanceVersion)
	require.Equal(t, "0.00000000", storedOrganization.UnallocatedPool.StringFixed(domain.OrganizationMoneyScale))
	allocations, err := client.OrganizationAllocation.Query().Where().Order(dbent.Asc("user_id_snapshot")).All(ctx)
	require.NoError(t, err)
	var total decimal.Decimal
	for _, allocation := range allocations {
		total = total.Add(allocation.Available)
		if allocation.Available.IsPositive() {
			require.Equal(t, int64(1), allocation.Version)
		}
	}
	require.Equal(t, "9.00000000", total.Add(storedOrganization.UnallocatedPool).StringFixed(domain.OrganizationMoneyScale))
}

func TestOrganizationAllocationRepositoryCustomAndReclaimPreserveReservedFunds(t *testing.T) {
	client := newOrganizationAllocationSQLite(t)
	ctx := context.Background()
	organization, actor, members := createOrganizationAllocationFixture(t, ctx, client, "10.00000000")
	member := members[0]
	_, err := client.OrganizationAllocation.Update().
		Where(
			organizationallocation.OrganizationIDEQ(organization.ID),
			organizationallocation.UserIDSnapshotEQ(member.ID),
		).
		SetReserved(decimal.RequireFromString("0.50000000")).
		Save(ctx)
	require.NoError(t, err)

	repo := NewOrganizationAllocationRepository(client)
	manager, err := service.NewOrganizationAllocationService(repo, strings.Repeat("preview-secret", 3))
	require.NoError(t, err)
	actorSnapshot := service.OrganizationAllocationActor{
		UserID:                actor.ID,
		DisplayName:           "org-admin",
		OrganizationRole:      domain.OrganizationRoleAdmin,
		AuthorizationScope:    domain.OrganizationFundingAuthorizationOrganizationJWT,
		CurrentOrganizationID: organization.ID,
		Active:                true,
	}

	customPreview, err := manager.Preview(ctx, actorSnapshot, organization.ID, domain.OrganizationFundingOperationCustomAllocation, service.OrganizationAllocationPreviewInput{
		Targets: []service.OrganizationAllocationTargetInput{{UserID: member.ID, Amount: "2"}},
	})
	require.NoError(t, err)
	customResult, err := manager.Execute(ctx, actorSnapshot, organization.ID, domain.OrganizationFundingOperationCustomAllocation,
		service.OrganizationAllocationExecuteInput{PreviewToken: customPreview.PreviewToken, Confirmation: customPreview.Confirmation}, "custom-1")
	require.NoError(t, err)
	require.Equal(t, service.OrganizationAllocationOutcomeCommitted, customResult.Outcome)
	require.Len(t, customResult.Postings, 2)

	allocation, err := client.OrganizationAllocation.Query().Where(organizationallocation.UserIDSnapshotEQ(member.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "3.00000000", allocation.Available.StringFixed(domain.OrganizationMoneyScale))
	require.Equal(t, "0.50000000", allocation.Reserved.StringFixed(domain.OrganizationMoneyScale))
	require.Equal(t, int64(1), allocation.Version)

	reclaimPreview, err := manager.Preview(ctx, actorSnapshot, organization.ID, domain.OrganizationFundingOperationReclaim, service.OrganizationAllocationPreviewInput{
		Targets: []service.OrganizationAllocationTargetInput{{UserID: member.ID, Amount: "1"}},
	})
	require.NoError(t, err)
	reclaimResult, err := manager.Execute(ctx, actorSnapshot, organization.ID, domain.OrganizationFundingOperationReclaim,
		service.OrganizationAllocationExecuteInput{PreviewToken: reclaimPreview.PreviewToken, Confirmation: reclaimPreview.Confirmation}, "reclaim-1")
	require.NoError(t, err)
	require.Equal(t, service.OrganizationAllocationOutcomeCommitted, reclaimResult.Outcome)
	require.Len(t, reclaimResult.Postings, 2)

	allocation, err = client.OrganizationAllocation.Query().Where(organizationallocation.UserIDSnapshotEQ(member.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "2.00000000", allocation.Available.StringFixed(domain.OrganizationMoneyScale))
	require.Equal(t, "0.50000000", allocation.Reserved.StringFixed(domain.OrganizationMoneyScale))
	require.Equal(t, int64(2), allocation.Version)
	organization, err = client.Organization.Get(ctx, organization.ID)
	require.NoError(t, err)
	require.Equal(t, "9.00000000", organization.UnallocatedPool.StringFixed(domain.OrganizationMoneyScale))
}

func TestOrganizationAllocationRepositoryNoOpPersistsReplayWithoutMoneyMovement(t *testing.T) {
	tests := []struct {
		name       string
		pool       string
		wantReason string
		prepare    func(context.Context, *dbent.Client, *dbent.Organization, []*dbent.User)
		mutate     func(context.Context, *dbent.Client, *dbent.Organization, []*dbent.User)
	}{
		{
			name: "zero pool", pool: "0.00000000", wantReason: service.OrganizationAllocationNoOpZeroPool,
			mutate: func(ctx context.Context, client *dbent.Client, organization *dbent.Organization, _ []*dbent.User) {
				_, err := client.Organization.UpdateOneID(organization.ID).SetUnallocatedPool(decimal.NewFromInt(5)).SetBalanceVersion(1).Save(ctx)
				require.NoError(t, err)
			},
		},
		{
			name: "no eligible members", pool: "10.00000000", wantReason: service.OrganizationAllocationNoOpNoEligible,
			prepare: func(ctx context.Context, client *dbent.Client, _ *dbent.Organization, members []*dbent.User) {
				for _, member := range members {
					_, err := client.User.UpdateOneID(member.ID).SetOrganizationRole(domain.OrganizationRoleAdmin).Save(ctx)
					require.NoError(t, err)
				}
			},
			mutate: func(ctx context.Context, client *dbent.Client, _ *dbent.Organization, members []*dbent.User) {
				_, err := client.User.UpdateOneID(members[0].ID).SetOrganizationRole(domain.OrganizationRoleMember).Save(ctx)
				require.NoError(t, err)
			},
		},
		{
			name: "below precision", pool: "0.00000001", wantReason: service.OrganizationAllocationNoOpBelowPrecision,
			mutate: func(ctx context.Context, client *dbent.Client, organization *dbent.Organization, _ []*dbent.User) {
				_, err := client.Organization.UpdateOneID(organization.ID).SetUnallocatedPool(decimal.NewFromInt(3)).SetBalanceVersion(1).Save(ctx)
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newOrganizationAllocationSQLite(t)
			ctx := context.Background()
			organization, actor, members := createOrganizationAllocationFixture(t, ctx, client, tt.pool)
			if tt.prepare != nil {
				tt.prepare(ctx, client, organization, members)
			}
			manager, err := service.NewOrganizationAllocationService(NewOrganizationAllocationRepository(client), strings.Repeat("preview-secret", 3))
			require.NoError(t, err)
			actorSnapshot := service.OrganizationAllocationActor{
				UserID:                actor.ID,
				DisplayName:           "org-admin",
				OrganizationRole:      domain.OrganizationRoleAdmin,
				AuthorizationScope:    domain.OrganizationFundingAuthorizationOrganizationJWT,
				CurrentOrganizationID: organization.ID,
				Active:                true,
			}
			preview, err := manager.Preview(ctx, actorSnapshot, organization.ID, domain.OrganizationFundingOperationEqualAllocation, service.OrganizationAllocationPreviewInput{})
			require.NoError(t, err)
			require.Equal(t, tt.wantReason, preview.Confirmation.NoOpReason)
			input := service.OrganizationAllocationExecuteInput{PreviewToken: preview.PreviewToken, Confirmation: preview.Confirmation}
			result, err := manager.Execute(ctx, actorSnapshot, organization.ID, domain.OrganizationFundingOperationEqualAllocation, input, "no-op-1")
			require.NoError(t, err)
			require.Equal(t, service.OrganizationAllocationOutcomeNoOp, result.Outcome)
			require.NotEmpty(t, result.OperationID)
			require.NotEmpty(t, result.CorrelationID)
			require.Empty(t, result.Postings)

			storedOrganization, err := client.Organization.Get(ctx, organization.ID)
			require.NoError(t, err)
			require.Zero(t, storedOrganization.BalanceVersion)
			operationCount, err := client.OrganizationLedgerOperation.Query().Count(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, operationCount)
			operation, err := client.OrganizationLedgerOperation.Query().Only(ctx)
			require.NoError(t, err)
			require.NotEmpty(t, operation.NoOpResultSnapshot)
			postingCount, err := client.OrganizationLedgerPosting.Query().Count(ctx)
			require.NoError(t, err)
			require.Zero(t, postingCount)

			if tt.mutate != nil {
				tt.mutate(ctx, client, organization, members)
			}
			replayed, err := manager.Execute(ctx, actorSnapshot, organization.ID, domain.OrganizationFundingOperationEqualAllocation, input, "no-op-1")
			require.NoError(t, err)
			require.True(t, replayed.Replayed)
			require.Equal(t, result.OperationID, replayed.OperationID)
			require.Equal(t, result.CorrelationID, replayed.CorrelationID)
			require.Equal(t, result.NoOpReason, replayed.NoOpReason)
			postingCount, err = client.OrganizationLedgerPosting.Query().Count(ctx)
			require.NoError(t, err)
			require.Zero(t, postingCount)
		})
	}
}

func createOrganizationAllocationFixture(t *testing.T, ctx context.Context, client *dbent.Client, pool string) (*dbent.Organization, *dbent.User, []*dbent.User) {
	t.Helper()
	creator, err := client.User.Create().
		SetEmail("allocation-fixture-creator@example.com").
		SetPasswordHash("hash").
		SetRole(domain.RoleAdmin).
		SetStatus(domain.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	organization, err := client.Organization.Create().
		SetName("Allocation Fixture").
		SetCode("allocation-fixture").
		SetStatus(domain.OrganizationStatusActive).
		SetCreatedBy(creator.ID).
		SetUnallocatedPool(decimal.RequireFromString(pool)).
		SetBalanceVersion(0).
		Save(ctx)
	require.NoError(t, err)

	actor, err := client.User.Create().
		SetEmail("allocation-fixture-admin@example.com").
		SetPasswordHash("hash").
		SetRole(domain.RoleUser).
		SetStatus(domain.StatusActive).
		SetOrganizationID(organization.ID).
		SetOrganizationRole(domain.OrganizationRoleAdmin).
		Save(ctx)
	require.NoError(t, err)

	members := make([]*dbent.User, 0, 3)
	for index := 0; index < 3; index++ {
		member, createErr := client.User.Create().
			SetEmail(fmt.Sprintf("allocation-fixture-member-%d@example.com", index)).
			SetPasswordHash("hash").
			SetRole(domain.RoleUser).
			SetStatus(domain.StatusActive).
			SetOrganizationID(organization.ID).
			SetOrganizationRole(domain.OrganizationRoleMember).
			Save(ctx)
		require.NoError(t, createErr)
		members = append(members, member)
	}

	_, err = client.OrganizationAllocation.Create().
		SetOrganizationID(organization.ID).
		SetUserIDSnapshot(actor.ID).
		SetUserRefID(actor.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)
	for index, member := range members {
		_, err = client.OrganizationAllocation.Create().
			SetOrganizationID(organization.ID).
			SetUserIDSnapshot(member.ID).
			SetUserRefID(member.ID).
			SetStatus(domain.OrganizationAllocationStatusActive).
			SetAvailable(decimal.NewFromInt(int64(index + 1))).
			SetReserved(decimal.Zero).
			Save(ctx)
		require.NoError(t, err)
	}
	return organization, actor, members
}
