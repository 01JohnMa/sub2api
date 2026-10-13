package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func newOrganizationProjectionTestClient(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *dbent.Client) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() {
		_ = client.Close()
		_ = db.Close()
	})
	return db, mock, client
}

func organizationProjectionOutboxRows(eventType, status string) *sqlmock.Rows {
	return organizationProjectionOutboxRowsWithCost(eventType, status, "0.12345679")
}

func organizationProjectionOutboxRowsWithCost(eventType, status, settledCost string) *sqlmock.Rows {
	occurredAt := time.Date(2026, 7, 31, 4, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{
		"id", "execution_id_snapshot", "event_type", "status", "user_id_snapshot", "api_key_id_snapshot",
		"endpoint_family", "organization_settled_cost", "occurred_at",
	}).AddRow(
		int64(17), "exec-17", eventType, status, int64(21), int64(22), service.OrganizationEndpointFamilyResponses, settledCost, occurredAt,
	)
}

func organizationProjectionDetailsRows(eventType string) *sqlmock.Rows {
	terminalOutcome := service.OrganizationExecutionTerminalOutcomeSuccess
	if eventType == "error_projection" {
		terminalOutcome = service.OrganizationExecutionTerminalOutcomeError
	}
	return sqlmock.NewRows([]string{
		"organization_id", "client_request_id", "account_id_snapshot", "model", "input_tokens", "output_tokens",
		"cache_creation_tokens", "cache_read_tokens", "raw_actual_cost", "terminal_outcome", "is_business_limited",
	}).AddRow(
		int64(9), "req-17", int64(23), "gpt-test", int64(4), int64(5), int64(6), int64(7), "0.1234567890", terminalOutcome, false,
	)
}

func expectOrganizationProjectionOutboxDetails(mock sqlmock.Sqlmock, eventType string) {
	mock.ExpectQuery("SELECT outbox.organization_id, execution.client_request_id").
		WithArgs(int64(17)).WillReturnRows(organizationProjectionDetailsRows(eventType))
}

func expectOrganizationProjectionOutboxDetailsWithoutAccount(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT outbox.organization_id, execution.client_request_id").
		WithArgs(int64(17)).
		WillReturnRows(sqlmock.NewRows([]string{
			"organization_id", "client_request_id", "account_id_snapshot", "model", "input_tokens", "output_tokens",
			"cache_creation_tokens", "cache_read_tokens", "raw_actual_cost", "terminal_outcome", "is_business_limited",
		}).AddRow(int64(9), "req-17", nil, "gpt-test", int64(0), int64(0), int64(0), int64(0), "0", service.OrganizationExecutionTerminalOutcomeSuccess, false))
}

func expectOrganizationProjectionOutboxLoad(mock sqlmock.Sqlmock, eventType, status string) {
	expectOrganizationProjectionOutboxLoadWithCost(mock, eventType, status, "0.12345679")
}

func expectOrganizationProjectionOutboxLoadWithCost(mock sqlmock.Sqlmock, eventType, status, settledCost string) {
	mock.ExpectQuery("SELECT outbox.id, outbox.execution_id_snapshot, outbox.event_type").
		WithArgs(int64(17)).WillReturnRows(organizationProjectionOutboxRowsWithCost(eventType, status, settledCost))
}

func expectOrganizationProjectionOutboxApplied(mock sqlmock.Sqlmock) {
	expectOrganizationProjectionOutboxStatus(mock, service.OrganizationAttemptAccountEffectStateApplied)
}

func expectOrganizationProjectionOutboxStatus(mock sqlmock.Sqlmock, status string) {
	mock.ExpectExec("UPDATE organization_execution_outboxes").
		WithArgs(status, sqlmock.AnyArg(), int64(17), service.OrganizationAttemptAccountEffectStatePending).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectOrganizationUsageProjectionTargets(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT id FROM users").WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(21)))
	mock.ExpectQuery("SELECT id FROM api_keys").WithArgs(int64(22)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(22)))
	mock.ExpectQuery("SELECT id FROM accounts").WithArgs(int64(23)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(23)))
}

func TestRedriveOrganizationUsageProjectionDoesNotRewriteLegacyRow(t *testing.T) {
	_, mock, client := newOrganizationProjectionTestClient(t)
	expectOrganizationProjectionOutboxLoad(mock, "usage_projection", service.OrganizationAttemptAccountEffectStatePending)
	expectOrganizationProjectionOutboxDetails(mock, "usage_projection")
	expectOrganizationUsageProjectionTargets(mock)
	mock.ExpectQuery("SELECT organization_id, execution_id FROM usage_logs").
		WithArgs("exec-17").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(int64(21), int64(22), int64(23), "organization:exec-17", "gpt-test", int64(4), int64(5), int64(6), int64(7), "0.1234567890", sqlmock.AnyArg(), int64(9), "exec-17").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(82)))
	expectOrganizationProjectionOutboxApplied(mock)

	result, err := redriveOrganizationExecutionOutbox(context.Background(), client, 17)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationAttemptAccountEffectStateApplied, result.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRedriveOrganizationUsageProjectionReusesExistingExecutionProjection(t *testing.T) {
	_, mock, client := newOrganizationProjectionTestClient(t)
	expectOrganizationProjectionOutboxLoad(mock, "usage_projection", service.OrganizationAttemptAccountEffectStatePending)
	expectOrganizationProjectionOutboxDetails(mock, "usage_projection")
	expectOrganizationUsageProjectionTargets(mock)
	mock.ExpectQuery("SELECT organization_id, execution_id FROM usage_logs").
		WithArgs("exec-17").
		WillReturnRows(sqlmock.NewRows([]string{"organization_id", "execution_id"}).AddRow(int64(9), "exec-17"))
	expectOrganizationProjectionOutboxApplied(mock)

	result, err := redriveOrganizationExecutionOutbox(context.Background(), client, 17)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationAttemptAccountEffectStateApplied, result.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRedriveOrganizationUsageProjectionDeletedTargetBecomesTerminalNoop(t *testing.T) {
	_, mock, client := newOrganizationProjectionTestClient(t)
	expectOrganizationProjectionOutboxLoad(mock, "usage_projection", service.OrganizationAttemptAccountEffectStatePending)
	expectOrganizationProjectionOutboxDetails(mock, "usage_projection")
	mock.ExpectQuery("SELECT id FROM users").WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(21)))
	mock.ExpectQuery("SELECT id FROM api_keys").WithArgs(int64(22)).WillReturnError(sql.ErrNoRows)
	expectOrganizationProjectionOutboxStatus(mock, service.OrganizationAttemptAccountEffectStateDeletedTargetNoop)

	result, err := redriveOrganizationExecutionOutbox(context.Background(), client, 17)
	require.NoError(t, err)
	require.True(t, result.DeletedTargetNoop)
	require.Equal(t, service.OrganizationAttemptAccountEffectStateDeletedTargetNoop, result.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRedriveOrganizationUsageProjectionWithoutAccountSnapshotZeroCostIsAppliedNoop(t *testing.T) {
	_, mock, client := newOrganizationProjectionTestClient(t)
	expectOrganizationProjectionOutboxLoadWithCost(mock, "usage_projection", service.OrganizationAttemptAccountEffectStatePending, "0.00000000")
	expectOrganizationProjectionOutboxDetailsWithoutAccount(mock)
	expectOrganizationProjectionOutboxApplied(mock)

	result, err := redriveOrganizationExecutionOutbox(context.Background(), client, 17)
	require.NoError(t, err)
	require.False(t, result.DeletedTargetNoop)
	require.Equal(t, service.OrganizationAttemptAccountEffectStateApplied, result.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRedriveOrganizationUsageProjectionWithoutAccountSnapshotPositiveCostRemainsPending(t *testing.T) {
	_, mock, client := newOrganizationProjectionTestClient(t)
	expectOrganizationProjectionOutboxLoad(mock, "usage_projection", service.OrganizationAttemptAccountEffectStatePending)
	expectOrganizationProjectionOutboxDetailsWithoutAccount(mock)

	_, err := redriveOrganizationExecutionOutbox(context.Background(), client, 17)
	require.ErrorIs(t, err, service.ErrOrganizationExecutionInvariant)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRedriveOrganizationUsageProjectionInvalidImmutableSnapshotRemainsPending(t *testing.T) {
	_, mock, client := newOrganizationProjectionTestClient(t)
	expectOrganizationProjectionOutboxLoad(mock, "usage_projection", service.OrganizationAttemptAccountEffectStatePending)
	mock.ExpectQuery("SELECT outbox.organization_id, execution.client_request_id").
		WithArgs(int64(17)).
		WillReturnRows(sqlmock.NewRows([]string{
			"organization_id", "client_request_id", "account_id_snapshot", "model", "input_tokens", "output_tokens",
			"cache_creation_tokens", "cache_read_tokens", "raw_actual_cost", "terminal_outcome", "is_business_limited",
		}).AddRow(int64(0), "req-17", int64(23), "gpt-test", int64(0), int64(0), int64(0), int64(0), "0", service.OrganizationExecutionTerminalOutcomeSuccess, false))

	_, err := redriveOrganizationExecutionOutbox(context.Background(), client, 17)
	require.ErrorIs(t, err, service.ErrOrganizationExecutionInvariant)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRedriveOrganizationErrorProjectionWritesExecutionScopedFallbackAndDuplicateReplayIsNoop(t *testing.T) {
	_, mock, client := newOrganizationProjectionTestClient(t)
	expectOrganizationProjectionOutboxLoad(mock, "error_projection", service.OrganizationAttemptAccountEffectStatePending)
	expectOrganizationProjectionOutboxDetails(mock, "error_projection")
	mock.ExpectQuery("SELECT organization_id, execution_id FROM ops_error_logs").WithArgs("exec-17").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("INSERT INTO ops_error_logs").
		WithArgs("exec-17", "req-17", int64(21), int64(22), int64(23), service.OrganizationEndpointFamilyResponses, "gpt-test", service.OrganizationEndpointFamilyResponses, "terminal", "organization_execution_error", 500, false, false, "organization execution terminal outcome: error", sqlmock.AnyArg(), int64(9), "exec-17").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(83)))
	expectOrganizationProjectionOutboxApplied(mock)

	result, err := redriveOrganizationExecutionOutbox(context.Background(), client, 17)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationAttemptAccountEffectStateApplied, result.Status)

	expectOrganizationProjectionOutboxLoad(mock, "error_projection", service.OrganizationAttemptAccountEffectStateApplied)
	replayed, err := redriveOrganizationExecutionOutbox(context.Background(), client, 17)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRedrivePendingOrganizationExecutionEffectsClaimsProjectionEvents(t *testing.T) {
	_, mock, client := newOrganizationProjectionTestClient(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id
		FROM organization_execution_outboxes
			WHERE status = $1
				AND event_type IN ('usage_projection', 'error_projection', 'api_key_quota', 'user_platform_quota')`)).
		WithArgs(service.OrganizationAttemptAccountEffectStatePending, 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	results, err := redrivePendingOrganizationExecutionEffects(context.Background(), client, 10)
	require.NoError(t, err)
	require.Empty(t, results)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRedriveOrganizationErrorProjectionLeavesPendingOnInsertFailure(t *testing.T) {
	_, mock, client := newOrganizationProjectionTestClient(t)
	expectOrganizationProjectionOutboxLoad(mock, "error_projection", service.OrganizationAttemptAccountEffectStatePending)
	expectOrganizationProjectionOutboxDetails(mock, "error_projection")
	mock.ExpectQuery("SELECT organization_id, execution_id FROM ops_error_logs").WithArgs("exec-17").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("INSERT INTO ops_error_logs").WillReturnError(errors.New("foreign key violation"))

	_, err := redriveOrganizationExecutionOutbox(context.Background(), client, 17)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVerifyOrganizationProjectionAttributionRejectsPartialImmutableFields(t *testing.T) {
	_, mock, client := newOrganizationProjectionTestClient(t)
	outbox := &organizationExecutionOutboxRecord{ExecutionID: "exec-17", OrganizationID: 9}
	mock.ExpectQuery("SELECT organization_id, execution_id FROM usage_logs").
		WithArgs("exec-17").
		WillReturnRows(sqlmock.NewRows([]string{"organization_id", "execution_id"}).AddRow(int64(9), nil))

	found, err := verifyOrganizationProjectionAttribution(context.Background(), client, "usage_logs", "execution_id = $1", []any{"exec-17"}, outbox, false)
	require.ErrorIs(t, err, service.ErrOrganizationExecutionConflict)
	require.True(t, found)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVerifyOrganizationProjectionAttributionAllowsMultipleErrorRowsForOneExecution(t *testing.T) {
	_, mock, client := newOrganizationProjectionTestClient(t)
	outbox := &organizationExecutionOutboxRecord{ExecutionID: "exec-17", OrganizationID: 9}
	mock.ExpectQuery("SELECT organization_id, execution_id FROM ops_error_logs").
		WithArgs("exec-17").
		WillReturnRows(sqlmock.NewRows([]string{"organization_id", "execution_id"}).
			AddRow(int64(9), "exec-17").
			AddRow(int64(9), "exec-17"))

	found, err := verifyOrganizationProjectionAttribution(context.Background(), client, "ops_error_logs", "execution_id = $1", []any{"exec-17"}, outbox, true)
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, mock.ExpectationsWereMet())
}
