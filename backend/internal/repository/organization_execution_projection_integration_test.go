//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestOrganizationExecutionProjectionPostgresRedrivesUsageExactlyOnce(t *testing.T) {
	ctx := context.Background()
	fixture := createOrganizationProjectionPostgresFixture(
		t,
		ctx,
		"projection-usage",
		service.OrganizationExecutionTerminalOutcomeSuccess,
		"0.25000000",
	)
	recovery := fixture.repository.(service.OrganizationExecutionRecoveryRepository)

	applied, err := recovery.RedriveExecutionOutbox(ctx, fixture.outboxID)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationAttemptAccountEffectStateApplied, applied.Status)
	require.False(t, applied.Replayed)

	replayed, err := recovery.RedriveExecutionOutbox(ctx, fixture.outboxID)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)

	var (
		count          int
		organizationID int64
		executionID    string
		requestID      string
		actualCost     string
		createdAt      time.Time
	)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM usage_logs WHERE execution_id = $1
	`, fixture.command.ExecutionID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT organization_id, execution_id::text, request_id, actual_cost::text, created_at
		FROM usage_logs
		WHERE execution_id = $1
	`, fixture.command.ExecutionID).Scan(&organizationID, &executionID, &requestID, &actualCost, &createdAt))
	require.Equal(t, fixture.organization.ID, organizationID)
	require.Equal(t, fixture.command.ExecutionID, executionID)
	require.Equal(t, "organization:"+fixture.command.ExecutionID, requestID)
	require.True(t, decimal.RequireFromString("0.25000000").Equal(decimal.RequireFromString(actualCost)))
	require.WithinDuration(t, fixture.occurredAt, createdAt, time.Microsecond)
}

func TestOrganizationExecutionProjectionPostgresRedrivesErrorExactlyOnce(t *testing.T) {
	ctx := context.Background()
	fixture := createOrganizationProjectionPostgresFixture(
		t,
		ctx,
		"projection-error",
		service.OrganizationExecutionTerminalOutcomeError,
		"0.00000000",
	)
	recovery := fixture.repository.(service.OrganizationExecutionRecoveryRepository)

	applied, err := recovery.RedriveExecutionOutbox(ctx, fixture.outboxID)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationAttemptAccountEffectStateApplied, applied.Status)
	require.False(t, applied.Replayed)

	replayed, err := recovery.RedriveExecutionOutbox(ctx, fixture.outboxID)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)

	var (
		count           int
		organizationID  int64
		executionID     string
		requestID       string
		clientRequestID sql.NullString
		errorType       string
		statusCode      int
		createdAt       time.Time
	)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ops_error_logs WHERE execution_id = $1
	`, fixture.command.ExecutionID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT organization_id, execution_id::text, request_id, client_request_id,
			error_type, status_code, created_at
		FROM ops_error_logs
		WHERE execution_id = $1
	`, fixture.command.ExecutionID).Scan(
		&organizationID,
		&executionID,
		&requestID,
		&clientRequestID,
		&errorType,
		&statusCode,
		&createdAt,
	))
	require.Equal(t, fixture.organization.ID, organizationID)
	require.Equal(t, fixture.command.ExecutionID, executionID)
	require.Equal(t, fixture.command.ExecutionID, requestID)
	require.False(t, clientRequestID.Valid)
	require.Equal(t, "organization_execution_error", errorType)
	require.Equal(t, 500, statusCode)
	require.WithinDuration(t, fixture.occurredAt, createdAt, time.Microsecond)

	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM usage_logs WHERE execution_id = $1
	`, fixture.command.ExecutionID).Scan(&count))
	require.Zero(t, count)
}

func TestOrganizationExecutionProjectionPostgresTwoWorkersClaimOneUsageEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fixture := createOrganizationProjectionPostgresFixture(
		t,
		ctx,
		"projection-workers",
		service.OrganizationExecutionTerminalOutcomeSuccess,
		"0.25000000",
	)
	markOtherOrganizationProjectionOutboxesApplied(t, ctx, fixture.command.ExecutionID, fixture.eventType)
	requireOrganizationProjectionPendingOutboxIDs(t, ctx, fixture.outboxID)
	barrier := installOrganizationUsageProjectionBarrier(t, ctx, fixture.command.ExecutionID)

	results := make(chan organizationProjectionWorkerResult, 1)
	client := testEntClient(t)
	go func() {
		repository := NewOrganizationExecutionRepository(client)
		applied, err := repository.RedrivePendingExecutionEffects(ctx, 1)
		results <- organizationProjectionWorkerResult{results: applied, err: err}
	}()

	barrier.requireBlocked(t, ctx)
	secondRepository := NewOrganizationExecutionRepository(client)
	secondApplied, err := secondRepository.RedrivePendingExecutionEffects(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, secondApplied)
	barrier.release(t, ctx)

	firstResult := <-results
	require.NoError(t, firstResult.err)
	require.Len(t, firstResult.results, 1)
	require.Equal(t, fixture.outboxID, firstResult.results[0].OutboxID)
	require.False(t, firstResult.results[0].Replayed)
	require.Equal(t, service.OrganizationAttemptAccountEffectStateApplied, firstResult.results[0].Status)

	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM usage_logs WHERE execution_id = $1
	`, fixture.command.ExecutionID).Scan(&count))
	require.Equal(t, 1, count)

	replayed, err := fixture.repository.(service.OrganizationExecutionRecoveryRepository).RedriveExecutionOutbox(ctx, fixture.outboxID)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
}

func TestOrganizationExecutionProjectionPostgresRollsBackBatchAndRetries(t *testing.T) {
	ctx := context.Background()
	first := createOrganizationProjectionPostgresFixture(
		t,
		ctx,
		"projection-rollback-first",
		service.OrganizationExecutionTerminalOutcomeSuccess,
		"0.25000000",
	)
	second := createOrganizationProjectionPostgresFixture(
		t,
		ctx,
		"projection-rollback-second",
		service.OrganizationExecutionTerminalOutcomeSuccess,
		"0.25000000",
	)
	require.Less(t, first.outboxID, second.outboxID)
	markOtherOrganizationProjectionOutboxesApplied(t, ctx, first.command.ExecutionID, first.eventType)
	markOtherOrganizationProjectionOutboxesApplied(t, ctx, second.command.ExecutionID, second.eventType)
	requireOrganizationProjectionPendingOutboxIDs(t, ctx, first.outboxID, second.outboxID)

	var conflictingUsageID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO usage_logs (user_id, api_key_id, account_id, request_id, model, created_at)
		VALUES ($1, $2, $3, $4, 'legacy-conflict', $5)
		RETURNING id
	`,
		second.member.ID,
		second.key.ID,
		second.accountID,
		"organization:"+second.command.ExecutionID,
		second.occurredAt,
	).Scan(&conflictingUsageID))

	repository := NewOrganizationExecutionRepository(testEntClient(t))
	barrier := installOrganizationUsageProjectionBarrier(t, ctx, first.command.ExecutionID)
	results := make(chan organizationProjectionWorkerResult, 1)
	go func() {
		applied, err := repository.RedrivePendingExecutionEffects(ctx, 2)
		results <- organizationProjectionWorkerResult{results: applied, err: err}
	}()
	barrier.requireBlocked(t, ctx)
	requireOrganizationUsageProjectionCount(t, ctx, first.command.ExecutionID, 0)
	barrier.release(t, ctx)
	failedResult := <-results
	require.Error(t, failedResult.err)
	require.Empty(t, failedResult.results)
	var uniqueViolation *pq.Error
	require.ErrorAs(t, failedResult.err, &uniqueViolation)
	require.Equal(t, pq.ErrorCode("23505"), uniqueViolation.Code)
	require.Equal(t, "idx_usage_logs_request_id_api_key_unique", uniqueViolation.Constraint)
	requireOrganizationProjectionOutboxStatus(t, ctx, first.outboxID, service.OrganizationAttemptAccountEffectStatePending)
	requireOrganizationProjectionOutboxStatus(t, ctx, second.outboxID, service.OrganizationAttemptAccountEffectStatePending)
	requireOrganizationUsageProjectionCount(t, ctx, first.command.ExecutionID, 0)
	requireOrganizationUsageProjectionCount(t, ctx, second.command.ExecutionID, 0)

	_, err := integrationDB.ExecContext(ctx, "DELETE FROM usage_logs WHERE id = $1", conflictingUsageID)
	require.NoError(t, err)
	applied, err := repository.RedrivePendingExecutionEffects(ctx, 2)
	require.NoError(t, err)
	require.Len(t, applied, 2)
	requireOrganizationProjectionOutboxStatus(t, ctx, first.outboxID, service.OrganizationAttemptAccountEffectStateApplied)
	requireOrganizationProjectionOutboxStatus(t, ctx, second.outboxID, service.OrganizationAttemptAccountEffectStateApplied)
	requireOrganizationUsageProjectionCount(t, ctx, first.command.ExecutionID, 1)
	requireOrganizationUsageProjectionCount(t, ctx, second.command.ExecutionID, 1)
}

func TestOrganizationTelemetryMigrationPostgresSupportsPartitionedUsageParent(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	const schemaName = "ticket23_projection_partition"
	require.NoError(t, execOrganizationProjectionSQL(ctx, tx, `CREATE SCHEMA `+schemaName))
	require.NoError(t, execOrganizationProjectionSQL(ctx, tx, `SET LOCAL search_path TO `+schemaName+`, pg_catalog`))
	require.NoError(t, execOrganizationProjectionSQL(ctx, tx, `
		CREATE TABLE usage_logs (
			id BIGINT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL
		) PARTITION BY RANGE (created_at)
	`))
	require.NoError(t, execOrganizationProjectionSQL(ctx, tx, `
		CREATE TABLE usage_logs_202607 PARTITION OF usage_logs
		FOR VALUES FROM ('2026-07-01') TO ('2026-08-01')
	`))
	require.NoError(t, execOrganizationProjectionSQL(ctx, tx, `
		CREATE TABLE ops_error_logs (
			id BIGINT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL
		)
	`))
	require.NoError(t, execOrganizationProjectionSQL(ctx, tx, `
		INSERT INTO usage_logs (id, created_at) VALUES (1, '2026-07-15T00:00:00Z')
	`))

	content, err := migrations.FS.ReadFile("160_organization_telemetry_attribution.sql")
	require.NoError(t, err)
	require.NoError(t, execOrganizationProjectionSQL(ctx, tx, string(content)))

	var (
		organizationID sql.NullInt64
		executionID    sql.NullString
	)
	require.NoError(t, tx.QueryRowContext(ctx, `
		SELECT organization_id, execution_id::text FROM usage_logs WHERE id = 1
	`).Scan(&organizationID, &executionID))
	require.False(t, organizationID.Valid)
	require.False(t, executionID.Valid)

	indexes := []struct {
		table     string
		name      string
		predicate string
		relkind   string
		children  int
	}{
		{table: "usage_logs", name: "idx_usage_logs_organization_created_at", predicate: "organization_id IS NOT NULL", relkind: "I", children: 1},
		{table: "usage_logs", name: "idx_usage_logs_execution_id", predicate: "execution_id IS NOT NULL", relkind: "I", children: 1},
		{table: "ops_error_logs", name: "idx_ops_error_logs_organization_created_at", predicate: "organization_id IS NOT NULL", relkind: "i"},
		{table: "ops_error_logs", name: "idx_ops_error_logs_execution_id", predicate: "execution_id IS NOT NULL", relkind: "i"},
	}
	for _, index := range indexes {
		var (
			relkind   string
			unique    bool
			predicate string
		)
		require.NoError(t, tx.QueryRowContext(ctx, `
			SELECT index_class.relkind::text, index_meta.indisunique,
				pg_get_expr(index_meta.indpred, index_meta.indrelid)
			FROM pg_class AS index_class
			JOIN pg_namespace AS namespace ON namespace.oid = index_class.relnamespace
			JOIN pg_index AS index_meta ON index_meta.indexrelid = index_class.oid
			JOIN pg_class AS indexed_table ON indexed_table.oid = index_meta.indrelid
			WHERE namespace.nspname = $1
				AND indexed_table.relname = $2
				AND index_class.relname = $3
		`, schemaName, index.table, index.name).Scan(&relkind, &unique, &predicate))
		require.Equal(t, index.relkind, relkind)
		require.False(t, unique)
		require.Contains(t, predicate, index.predicate)

		if index.children > 0 {
			var children int
			require.NoError(t, tx.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM pg_inherits AS inheritance
				JOIN pg_class AS parent_index ON parent_index.oid = inheritance.inhparent
				JOIN pg_namespace AS namespace ON namespace.oid = parent_index.relnamespace
				WHERE namespace.nspname = $1 AND parent_index.relname = $2
			`, schemaName, index.name).Scan(&children))
			require.Equal(t, index.children, children)
		}
	}
}

type organizationProjectionPostgresFixture struct {
	reservedOrganizationExecutionIntegrationFixture
	accountID  int64
	outboxID   int64
	eventType  string
	occurredAt time.Time
}

type organizationProjectionWorkerResult struct {
	results []service.OrganizationExecutionOutboxApplyResult
	err     error
}

const organizationUsageProjectionBarrierLockKey int64 = 23_160_001

type organizationUsageProjectionBarrier struct {
	connection *sql.Conn
	lockHeld   bool
}

func installOrganizationUsageProjectionBarrier(t *testing.T, ctx context.Context, executionID string) *organizationUsageProjectionBarrier {
	t.Helper()
	parsedExecutionID, err := uuid.Parse(executionID)
	require.NoError(t, err)

	_, err = integrationDB.ExecContext(ctx, `
		DROP TRIGGER IF EXISTS ticket23_usage_projection_barrier ON usage_logs
	`)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `DROP FUNCTION IF EXISTS ticket23_usage_projection_barrier()`)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		CREATE FUNCTION ticket23_usage_projection_barrier()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			IF NEW.execution_id::text = TG_ARGV[0] THEN
				PERFORM pg_advisory_xact_lock(23160001);
			END IF;
			RETURN NEW;
		END;
		$$
	`)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER ticket23_usage_projection_barrier
		BEFORE INSERT ON usage_logs
		FOR EACH ROW
		EXECUTE FUNCTION ticket23_usage_projection_barrier('%s')
	`, parsedExecutionID.String()))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, cleanupErr := integrationDB.ExecContext(cleanupCtx, `
			DROP TRIGGER IF EXISTS ticket23_usage_projection_barrier ON usage_logs
		`)
		require.NoError(t, cleanupErr)
		_, cleanupErr = integrationDB.ExecContext(cleanupCtx, `DROP FUNCTION IF EXISTS ticket23_usage_projection_barrier()`)
		require.NoError(t, cleanupErr)
	})

	connection, err := integrationDB.Conn(ctx)
	require.NoError(t, err)
	barrier := &organizationUsageProjectionBarrier{connection: connection}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if barrier.lockHeld {
			var unlocked bool
			_ = barrier.connection.QueryRowContext(cleanupCtx, `SELECT pg_advisory_unlock($1)`, organizationUsageProjectionBarrierLockKey).Scan(&unlocked)
			barrier.lockHeld = false
		}
		require.NoError(t, barrier.connection.Close())
	})
	_, err = connection.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, organizationUsageProjectionBarrierLockKey)
	require.NoError(t, err)
	barrier.lockHeld = true
	return barrier
}

func (b *organizationUsageProjectionBarrier) requireBlocked(t *testing.T, ctx context.Context) {
	t.Helper()
	require.Eventually(t, func() bool {
		var blocked bool
		err := integrationDB.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE datname = current_database()
					AND usename = current_user
					AND pid <> pg_backend_pid()
					AND wait_event_type = 'Lock'
					AND wait_event = 'advisory'
					AND query LIKE '%INSERT INTO usage_logs%'
			)
		`).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
}

func (b *organizationUsageProjectionBarrier) release(t *testing.T, ctx context.Context) {
	t.Helper()
	var unlocked bool
	require.NoError(t, b.connection.QueryRowContext(ctx, `
		SELECT pg_advisory_unlock($1)
	`, organizationUsageProjectionBarrierLockKey).Scan(&unlocked))
	require.True(t, unlocked)
	b.lockHeld = false
}

func createOrganizationProjectionPostgresFixture(
	t *testing.T,
	ctx context.Context,
	suffix string,
	terminalOutcome string,
	rawCost string,
) organizationProjectionPostgresFixture {
	t.Helper()
	client := testEntClient(t)
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, client, suffix)
	accountID := int64(0)
	t.Cleanup(func() {
		cleanupOrganizationProjectionPostgresFixture(
			t,
			fixture.organization.ID,
			fixture.organization.CreatedBy,
			accountID,
			fixture.command.ExecutionID,
		)
	})
	account := mustCreateAccount(t, client, &service.Account{Name: "organization projection " + suffix})
	accountID = account.ID
	attemptID := uuid.NewString()
	attempt, err := fixture.repository.RecordAttemptEvidence(ctx, organizationExecutionIntegrationEvidenceCommand(
		fixture,
		account.ID,
		attemptID,
		1,
		service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
		terminalOutcome,
		rawCost,
		`{"usage":"projection"}`,
	))
	require.NoError(t, err)
	occurredAt := time.Now().UTC().Truncate(time.Microsecond)
	final, err := fixture.repository.Terminalize(ctx, service.OrganizationTerminalizationCommand{
		ExecutionID:            fixture.command.ExecutionID,
		CorrelationID:          fixture.command.CorrelationID,
		OrganizationID:         fixture.organization.ID,
		UserID:                 fixture.member.ID,
		APIKeyID:               fixture.key.ID,
		AllocationID:           fixture.allocation.ID,
		ReservationID:          fixture.reserved.ReservationID,
		FinalAttemptID:         &attemptID,
		FinalAttemptEvidenceID: &attempt.AttemptEvidenceID,
		TerminalOutcome:        terminalOutcome,
		OccurredAt:             occurredAt,
	})
	require.NoError(t, err)
	require.Equal(t, occurredAt, final.OccurredAt)
	eventType := "usage_projection"
	if terminalOutcome == service.OrganizationExecutionTerminalOutcomeError {
		eventType = "error_projection"
	}
	var outboxID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT id FROM organization_execution_outboxes
		WHERE execution_id_snapshot = $1 AND event_type = $2
	`, fixture.command.ExecutionID, eventType).Scan(&outboxID))
	return organizationProjectionPostgresFixture{
		reservedOrganizationExecutionIntegrationFixture: fixture,
		accountID:  account.ID,
		outboxID:   outboxID,
		eventType:  eventType,
		occurredAt: occurredAt,
	}
}

func cleanupOrganizationProjectionPostgresFixture(t *testing.T, organizationID, creatorID, accountID int64, executionID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE execution_id = $1", executionID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM usage_logs WHERE execution_id = $1", executionID)
	require.NoError(t, err)
	cleanupOrganizationRolloutInvariantFixture(t, organizationID, creatorID, accountID)
}

func markOtherOrganizationProjectionOutboxesApplied(t *testing.T, ctx context.Context, executionID, eventType string) {
	t.Helper()
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE organization_execution_outboxes
		SET status = $1, applied_at = NOW(), updated_at = NOW()
		WHERE execution_id_snapshot = $2 AND event_type <> $3 AND status = $4
	`,
		service.OrganizationAttemptAccountEffectStateApplied,
		executionID,
		eventType,
		service.OrganizationAttemptAccountEffectStatePending,
	)
	require.NoError(t, err)
}

func requireOrganizationProjectionPendingOutboxIDs(t *testing.T, ctx context.Context, expected ...int64) {
	t.Helper()
	rows, err := integrationDB.QueryContext(ctx, `
		SELECT id
		FROM organization_execution_outboxes
		WHERE status = $1
			AND event_type IN ('usage_projection', 'error_projection', 'api_key_quota', 'user_platform_quota')
		ORDER BY id
		LIMIT $2
	`, service.OrganizationAttemptAccountEffectStatePending, len(expected))
	require.NoError(t, err)
	defer rows.Close()

	actual := make([]int64, 0, len(expected))
	for rows.Next() {
		var outboxID int64
		require.NoError(t, rows.Scan(&outboxID))
		actual = append(actual, outboxID)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, expected, actual)
}

func requireOrganizationProjectionOutboxStatus(t *testing.T, ctx context.Context, outboxID int64, expected string) {
	t.Helper()
	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status FROM organization_execution_outboxes WHERE id = $1
	`, outboxID).Scan(&status))
	require.Equal(t, expected, status)
}

func requireOrganizationUsageProjectionCount(t *testing.T, ctx context.Context, executionID string, expected int) {
	t.Helper()
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM usage_logs WHERE execution_id = $1
	`, executionID).Scan(&count))
	require.Equal(t, expected, count)
}

type organizationProjectionSQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func execOrganizationProjectionSQL(ctx context.Context, executor organizationProjectionSQLExecutor, statement string) error {
	_, err := executor.ExecContext(ctx, statement)
	return err
}
