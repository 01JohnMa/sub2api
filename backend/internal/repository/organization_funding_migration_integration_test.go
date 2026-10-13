//go:build integration

package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestOrganizationFundingMigrationDefinesDecimalLedgerSchema(t *testing.T) {
	tx := testTx(t)

	requireColumn(t, tx, "organizations", "unallocated_pool", "numeric", 0, false)
	requireNumericPrecisionScale(t, tx, "organizations", "unallocated_pool", 20, 8)
	requireColumn(t, tx, "organizations", "balance_version", "bigint", 0, false)
	requireConstraintDefinitionContains(t, tx, "organizations", "organizations_unallocated_nonnegative_check", "unallocated_pool >=")
	requireConstraintDefinitionContains(t, tx, "organizations", "organizations_balance_version_nonnegative_check", "balance_version >=")

	requireColumn(t, tx, "organization_ledger_operations", "operation_id", "uuid", 0, false)
	requireColumn(t, tx, "organization_ledger_operations", "actor_ref_id", "bigint", 0, true)
	requireConstraintDefinitionContains(t, tx, "organization_ledger_operations", "org_ledger_operations_operation_type_check", "organization_credit", "consumption")
	requireConstraintDefinitionContains(t, tx, "organization_ledger_operations", "org_ledger_operations_authorization_scope_check", "platform_admin_jwt", "organization_admin_jwt")
	requireForeignKeyOnDelete(t, tx, "organization_ledger_operations", "organization_id", "organizations", "RESTRICT")
	requireForeignKeyOnDelete(t, tx, "organization_ledger_operations", "actor_ref_id", "users", "SET NULL")
	requireIndex(t, tx, "organization_ledger_operations", "org_ledger_operations_idempotency_scope_unique")

	requireColumn(t, tx, "organization_ledger_postings", "operation_ref_id", "bigint", 0, false)
	requireColumn(t, tx, "organization_ledger_postings", "allocation_ref_id", "bigint", 0, true)
	requireColumn(t, tx, "organization_ledger_postings", "target_user_ref_id", "bigint", 0, true)
	for _, column := range []string{"before_amount", "delta_amount", "after_amount"} {
		requireColumn(t, tx, "organization_ledger_postings", column, "numeric", 0, false)
		requireNumericPrecisionScale(t, tx, "organization_ledger_postings", column, 20, 8)
	}
	requireConstraintDefinitionContains(t, tx, "organization_ledger_postings", "org_ledger_postings_sequence_check", "posting_sequence >")
	requireConstraintDefinitionContains(t, tx, "organization_ledger_postings", "org_ledger_postings_amount_equation_check", "after_amount", "before_amount", "delta_amount")
	requireForeignKeyOnDelete(t, tx, "organization_ledger_postings", "operation_ref_id", "organization_ledger_operations", "RESTRICT")
	requireForeignKeyOnDelete(t, tx, "organization_ledger_postings", "organization_id", "organizations", "RESTRICT")
	requireForeignKeyOnDelete(t, tx, "organization_ledger_postings", "allocation_ref_id", "organization_allocations", "RESTRICT")
	requireForeignKeyOnDelete(t, tx, "organization_ledger_postings", "target_user_ref_id", "users", "SET NULL")
	requireIndex(t, tx, "organization_ledger_postings", "org_ledger_postings_operation_sequence_unique")

	var triggerCount int
	require.NoError(t, tx.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM pg_trigger
		WHERE tgrelid IN ('organization_ledger_operations'::regclass, 'organization_ledger_postings'::regclass)
		  AND tgname IN (
				'organization_ledger_operations_immutable',
				'organization_ledger_postings_immutable',
				'organization_ledger_postings_integrity'
		  )
	`).Scan(&triggerCount))
	require.Equal(t, 3, triggerCount)
}

func TestOrganizationFundingMigrationProtectsLedgerAndRestrictsAllocationAnchorDeletion(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	creator, organizationModel := createFundingOrganization(t, ctx, client, "migration-ledger", domain.OrganizationStatusActive)
	actor := mustCreateUser(t, client, &service.User{Email: "funding-ledger-actor@example.com", Role: domain.RoleAdmin})
	occurredAt := time.Now().UTC().Truncate(time.Microsecond)
	operationRefID, operationID := insertFundingLedgerOperation(t, ctx, client, organizationModel.ID, actor.ID, occurredAt, strings.Repeat("a", 64))
	postingID := insertUnallocatedFundingPosting(t, ctx, client, operationRefID, operationID, organizationModel.ID, occurredAt)

	requireFundingMigrationStatementFails(t, ctx, client, "UPDATE organization_ledger_operations SET internal_reason = 'changed' WHERE id = $1", operationRefID)
	requireFundingMigrationStatementFails(t, ctx, client, "DELETE FROM organization_ledger_operations WHERE id = $1", operationRefID)
	requireFundingMigrationStatementFails(t, ctx, client, "UPDATE organization_ledger_postings SET after_amount = 2 WHERE id = $1", postingID)
	requireFundingMigrationStatementFails(t, ctx, client, "DELETE FROM organization_ledger_postings WHERE id = $1", postingID)

	_, err := client.ExecContext(ctx, "DELETE FROM users WHERE id = $1", actor.ID)
	require.NoError(t, err)
	var actorRef sql.NullInt64
	var actorSnapshot int64
	require.NoError(t, scanOne(ctx, client, "SELECT actor_ref_id, actor_id_snapshot FROM organization_ledger_operations WHERE id = $1", []any{operationRefID}, &actorRef, &actorSnapshot))
	require.False(t, actorRef.Valid)
	require.Equal(t, actor.ID, actorSnapshot)

	// Keep the creator alive: the organization creator FK is intentionally RESTRICT.
	require.NotZero(t, creator.ID)

	target := mustCreateUser(t, client, &service.User{Email: "funding-ledger-target@example.com"})
	allocation, err := client.OrganizationAllocation.Create().
		SetOrganizationID(organizationModel.ID).
		SetUserIDSnapshot(target.ID).
		SetUserRefID(target.ID).
		SetStatus(domain.OrganizationAllocationStatusClosed).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)
	allocationOperationRefID, allocationOperationID := insertFundingLedgerOperation(t, ctx, client, organizationModel.ID, creator.ID, occurredAt.Add(time.Second), strings.Repeat("b", 64))
	allocationPostingID := insertAllocationFundingPosting(t, ctx, client, allocationOperationRefID, allocationOperationID, organizationModel.ID, allocation.ID, target.ID, occurredAt.Add(time.Second))

	requireFundingMigrationStatementFails(t, ctx, client, "DELETE FROM organization_allocations WHERE id = $1", allocation.ID)
	var allocationRef sql.NullInt64
	var allocationSnapshot int64
	require.NoError(t, scanOne(ctx, client, "SELECT allocation_ref_id, allocation_id_snapshot FROM organization_ledger_postings WHERE id = $1", []any{allocationPostingID}, &allocationRef, &allocationSnapshot))
	require.True(t, allocationRef.Valid)
	require.Equal(t, allocation.ID, allocationRef.Int64)
	require.Equal(t, allocation.ID, allocationSnapshot)
}

func TestOrganizationFundingMigrationRejectsUncorrelatedPostingAndDuplicateIdempotency(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	creator, firstOrganization := createFundingOrganization(t, ctx, client, "migration-correlated-a", domain.OrganizationStatusActive)
	_, secondOrganization := createFundingOrganization(t, ctx, client, "migration-correlated-b", domain.OrganizationStatusActive)
	occurredAt := time.Now().UTC().Truncate(time.Microsecond)
	operationRefID, operationID := insertFundingLedgerOperation(t, ctx, client, firstOrganization.ID, creator.ID, occurredAt, strings.Repeat("c", 64))

	requireFundingMigrationStatementFails(t, ctx, client, `
		INSERT INTO organization_ledger_postings (
			operation_ref_id, operation_id_snapshot, organization_id,
			posting_sequence, bucket_type, bucket_id_snapshot,
			currency, before_amount, delta_amount, after_amount,
			balance_version_before, balance_version_after, occurred_at
		) VALUES ($1, $2, $3, 1, $4, $5, 'USD', 0, 1, 1, 0, 1, $6)
	`, operationRefID, operationID, secondOrganization.ID, domain.OrganizationLedgerBucketUnallocated, secondOrganization.ID, occurredAt)

	requireFundingMigrationStatementFails(t, ctx, client, `
		INSERT INTO organization_ledger_postings (
			operation_ref_id, operation_id_snapshot, organization_id,
			posting_sequence, bucket_type, bucket_id_snapshot,
			currency, before_amount, delta_amount, after_amount,
			balance_version_before, balance_version_after, occurred_at
		) VALUES ($1, $2, $3, 0, $4, $5, 'USD', 0, 1, 1, 0, 1, $6)
	`, operationRefID, operationID, firstOrganization.ID, domain.OrganizationLedgerBucketUnallocated, firstOrganization.ID, occurredAt)

	requireFundingMigrationStatementFails(t, ctx, client, `
		INSERT INTO organization_ledger_operations (
			operation_id, correlation_id, organization_id, operation_type,
			authorization_scope, actor_id_snapshot, actor_ref_id,
			actor_display_snapshot, actor_role_snapshot, currency,
			idempotency_key_hash, canonical_payload_hash, internal_reason, occurred_at
		) VALUES ($1, $2, $3, 'organization_credit', 'platform_admin_jwt', $4, $4, 'actor', 'admin', 'USD', $5, $6, 'duplicate', $7)
	`, uuid.NewString(), uuid.NewString(), firstOrganization.ID, creator.ID, strings.Repeat("c", 64), strings.Repeat("d", 64), occurredAt)
}

func insertFundingLedgerOperation(
	t *testing.T,
	ctx context.Context,
	client *dbent.Client,
	organizationID, actorID int64,
	occurredAt time.Time,
	idempotencyHash string,
) (int64, string) {
	t.Helper()
	operationID := uuid.NewString()
	var operationRefID int64
	require.NoError(t, scanOne(ctx, client, `
		INSERT INTO organization_ledger_operations (
			operation_id, correlation_id, organization_id, operation_type,
			authorization_scope, actor_id_snapshot, actor_ref_id,
			actor_display_snapshot, actor_role_snapshot, currency,
			idempotency_key_hash, canonical_payload_hash, internal_reason, occurred_at
		) VALUES ($1, $2, $3, 'organization_credit', 'platform_admin_jwt', $4, $4, 'actor', 'admin', 'USD', $5, $6, 'migration test', $7)
		RETURNING id
	`, []any{operationID, uuid.NewString(), organizationID, actorID, idempotencyHash, strings.Repeat("e", 64), occurredAt}, &operationRefID))
	return operationRefID, operationID
}

func insertUnallocatedFundingPosting(t *testing.T, ctx context.Context, client *dbent.Client, operationRefID int64, operationID string, organizationID int64, occurredAt time.Time) int64 {
	t.Helper()
	var postingID int64
	require.NoError(t, scanOne(ctx, client, `
		INSERT INTO organization_ledger_postings (
			operation_ref_id, operation_id_snapshot, organization_id,
			posting_sequence, bucket_type, bucket_id_snapshot,
			currency, before_amount, delta_amount, after_amount,
			balance_version_before, balance_version_after, occurred_at
		) VALUES ($1, $2, $3, 1, $4, $5, 'USD', 0, 1, 1, 0, 1, $6)
		RETURNING id
	`, []any{operationRefID, operationID, organizationID, domain.OrganizationLedgerBucketUnallocated, organizationID, occurredAt}, &postingID))
	return postingID
}

func insertAllocationFundingPosting(t *testing.T, ctx context.Context, client *dbent.Client, operationRefID int64, operationID string, organizationID, allocationID, targetUserID int64, occurredAt time.Time) int64 {
	t.Helper()
	var postingID int64
	require.NoError(t, scanOne(ctx, client, `
		INSERT INTO organization_ledger_postings (
			operation_ref_id, operation_id_snapshot, organization_id,
			posting_sequence, bucket_type, bucket_id_snapshot,
			allocation_id_snapshot, allocation_ref_id, target_user_id_snapshot, target_user_ref_id,
			currency, before_amount, delta_amount, after_amount,
			balance_version_before, balance_version_after, occurred_at
		) VALUES ($1, $2, $3, 1, $4, $5, $5, $5, $6, $6, 'USD', 0, 1, 1, 0, 1, $7)
		RETURNING id
	`, []any{operationRefID, operationID, organizationID, domain.OrganizationLedgerBucketAllocationAvailable, allocationID, targetUserID, occurredAt}, &postingID))
	return postingID
}

func TestOrganizationFundingRepositoryRejectsNonCanonicalCommandBeforeMutation(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	creator, organizationModel := createFundingOrganization(t, ctx, tx.Client(), "canonical-command", domain.OrganizationStatusActive)
	repo := NewOrganizationFundingRepository(tx.Client())
	command := service.OrganizationCreditCommand{
		OrganizationID:       organizationModel.ID,
		Amount:               "1.0",
		Currency:             domain.OrganizationCurrencyUSD,
		OperationType:        domain.OrganizationFundingOperationCredit,
		AuthorizationScope:   domain.OrganizationFundingAuthorizationPlatformAdminJWT,
		IdempotencyKeyHash:   service.HashIdempotencyKey("noncanonical-command"),
		CanonicalPayloadHash: strings.Repeat("f", 64),
		Actor:                fundingActorForUser(creator),
		InternalReason:       "must reject before writes",
		OperationID:          uuid.NewString(),
		CorrelationID:        uuid.NewString(),
		OccurredAt:           time.Now().UTC(),
	}
	_, err := repo.Credit(ctx, command)
	require.ErrorIs(t, err, service.ErrOrganizationFundingInvariant)

	var operationCount int
	require.NoError(t, scanOne(ctx, tx.Client(), "SELECT COUNT(*) FROM organization_ledger_operations WHERE organization_id = $1", []any{organizationModel.ID}, &operationCount))
	require.Zero(t, operationCount)
}

func requireFundingMigrationStatementFails(t *testing.T, ctx context.Context, client *dbent.Client, query string, args ...any) {
	t.Helper()
	const savepoint = "organization_funding_expected_failure"
	_, err := client.ExecContext(ctx, "SAVEPOINT "+savepoint)
	require.NoError(t, err)
	_, err = client.ExecContext(ctx, query, args...)
	require.Error(t, err)
	_, err = client.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
	require.NoError(t, err)
	_, err = client.ExecContext(ctx, "RELEASE SAVEPOINT "+savepoint)
	require.NoError(t, err)
}
