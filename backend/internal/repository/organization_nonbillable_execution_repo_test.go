package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestOrganizationNonBillableBeginAllowsZeroAvailableAllocationWithoutReservation(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	command := repositoryNonBillableCommand(service.OrganizationEndpointFamilyModels, time.Date(2026, time.July, 31, 2, 0, 0, 0, time.UTC))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status, balance_version FROM organizations WHERE id = $1 FOR SHARE")).
		WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"status", "balance_version"}).AddRow(service.OrganizationStatusActive, int64(8)))
	mock.ExpectQuery("SELECT id, organization_id, deleted_at FROM users WHERE id = \\$1 FOR UPDATE").
		WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "deleted_at"}).AddRow(int64(2), int64(1), nil))
	mock.ExpectQuery("SELECT id, user_id, organization_id, status, deleted_at, expires_at FROM api_keys WHERE id = \\$1 FOR UPDATE").
		WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "organization_id", "status", "deleted_at", "expires_at"}).AddRow(int64(3), int64(2), int64(1), service.StatusAPIKeyActive, nil, nil))
	mock.ExpectQuery("SELECT id, organization_id, user_id_snapshot, user_ref_id, status, available, reserved, version").
		WithArgs(int64(4)).WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "user_id_snapshot", "user_ref_id", "status", "available", "reserved", "version"}).
		AddRow(int64(4), int64(1), int64(2), int64(2), domain.OrganizationAllocationStatusActive, "0.00000000", "0.00000000", int64(9)))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(command.ExecutionID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("INSERT INTO organization_executions").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(command.ExecutionID).WillReturnRows(nonBillableExecutionRows(command, int64(11), service.OrganizationExecutionStateAccepted))
	mock.ExpectCommit()
	mock.ExpectClose()

	result, err := repository.BeginNonBillableExecution(context.Background(), command)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationNonBillableExecutionDispositionStarted, result.Disposition)
	require.Equal(t, service.OrganizationExecutionStateAccepted, result.State)
	require.NoError(t, db.Close())
}

func TestOrganizationNonBillableStaleAllocationVersionFinalizesZeroCostError(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	acceptedAt := time.Date(2026, time.July, 31, 2, 5, 0, 0, time.UTC)
	command := repositoryNonBillableCommand(service.OrganizationEndpointFamilyUsage, acceptedAt)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status, balance_version FROM organizations WHERE id = $1 FOR SHARE")).
		WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"status", "balance_version"}).AddRow(service.OrganizationStatusActive, int64(8)))
	mock.ExpectQuery("SELECT id, organization_id, deleted_at FROM users WHERE id = \\$1 FOR UPDATE").
		WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "deleted_at"}).AddRow(int64(2), int64(1), nil))
	mock.ExpectQuery("SELECT id, user_id, organization_id, status, deleted_at, expires_at FROM api_keys WHERE id = \\$1 FOR UPDATE").
		WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "organization_id", "status", "deleted_at", "expires_at"}).AddRow(int64(3), int64(2), int64(1), service.StatusAPIKeyActive, nil, nil))
	mock.ExpectQuery("SELECT id, organization_id, user_id_snapshot, user_ref_id, status, available, reserved, version").
		WithArgs(int64(4)).WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "user_id_snapshot", "user_ref_id", "status", "available", "reserved", "version"}).
		AddRow(int64(4), int64(1), int64(2), int64(2), domain.OrganizationAllocationStatusActive, "0.00000000", "0.00000000", int64(8)))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").WithArgs(command.ExecutionID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("INSERT INTO organization_executions").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(71)))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").WithArgs(command.ExecutionID).WillReturnRows(nonBillableExecutionRowsWithRef(command, int64(71), service.OrganizationExecutionStateAccepted, nil))

	// The stale parent is terminalized as a proven-zero error in the same transaction.
	mock.ExpectQuery("SELECT id, organization_id, user_id_snapshot, user_ref_id, status, available, reserved, version").
		WithArgs(int64(4)).WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "user_id_snapshot", "user_ref_id", "status", "available", "reserved", "version"}).
		AddRow(int64(4), int64(1), int64(2), int64(2), domain.OrganizationAllocationStatusActive, "0.00000000", "0.00000000", int64(8)))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").WithArgs(command.ExecutionID).WillReturnRows(nonBillableExecutionRowsWithRef(command, int64(71), service.OrganizationExecutionStateAccepted, nil))
	mock.ExpectQuery("SELECT id, execution_ref_id, execution_id_snapshot, organization_id").WithArgs(int64(71)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM organization_execution_attempts WHERE execution_ref_id = \\$1").WithArgs(int64(71)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery("SELECT id FROM users WHERE id = \\$1 AND deleted_at IS NULL").WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(2)))
	mock.ExpectQuery("SELECT id FROM api_keys WHERE id = \\$1 AND deleted_at IS NULL").WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(3)))
	mock.ExpectQuery("INSERT INTO organization_final_execution_evidences").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(72)))
	mock.ExpectExec("UPDATE organization_executions SET status = \\$1").WithArgs(service.OrganizationExecutionStateReleased, acceptedAt, int64(71), service.OrganizationExecutionStateAccepted, service.OrganizationExecutionStateFinalUsageObserved).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO organization_execution_outboxes").WillReturnResult(sqlmock.NewResult(73, 1))
	mock.ExpectCommit()
	mock.ExpectClose()

	result, err := repository.BeginNonBillableExecution(context.Background(), command)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationNonBillableExecutionDispositionFinalized, result.Disposition)
	require.NotNil(t, result.FinalEvidence)
	require.Equal(t, "stale_authoritative_attribution", result.FinalEvidence.ZeroProof)
	require.NoError(t, db.Close())
}

func TestOrganizationNonBillableReplayRechecksCurrentAllocationVersion(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	command := repositoryNonBillableCommand(service.OrganizationEndpointFamilyUsage, time.Date(2026, time.July, 31, 2, 7, 0, 0, time.UTC))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status, balance_version FROM organizations WHERE id = $1 FOR SHARE")).
		WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"status", "balance_version"}).AddRow(service.OrganizationStatusActive, int64(8)))
	mock.ExpectQuery("SELECT id, organization_id, deleted_at FROM users WHERE id = \\$1 FOR UPDATE").
		WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "deleted_at"}).AddRow(int64(2), int64(1), nil))
	mock.ExpectQuery("SELECT id, user_id, organization_id, status, deleted_at, expires_at FROM api_keys WHERE id = \\$1 FOR UPDATE").
		WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "organization_id", "status", "deleted_at", "expires_at"}).AddRow(int64(3), int64(2), int64(1), service.StatusAPIKeyActive, nil, nil))
	mock.ExpectQuery("SELECT id, organization_id, user_id_snapshot, user_ref_id, status, available, reserved, version").
		WithArgs(int64(4)).WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "user_id_snapshot", "user_ref_id", "status", "available", "reserved", "version"}).
		AddRow(int64(4), int64(1), int64(2), int64(2), domain.OrganizationAllocationStatusActive, "0.00000000", "0.00000000", int64(8)))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(command.ExecutionID).WillReturnRows(nonBillableExecutionRows(command, int64(74), service.OrganizationExecutionStateReleased))
	mock.ExpectRollback()
	mock.ExpectClose()

	result, err := repository.BeginNonBillableExecution(context.Background(), command)
	require.Nil(t, result)
	require.ErrorIs(t, err, service.ErrOrganizationBillingUnavailable)
	require.NoError(t, db.Close())
}

func TestOrganizationNonBillableCountTokensAttemptAndFinalKeepTokensWithoutFinancialWrites(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	acceptedAt := time.Date(2026, time.July, 31, 2, 10, 0, 123456000, time.UTC)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyTokenCounting, acceptedAt)
	attempt := repositoryNonBillableAttemptCommand(execution, acceptedAt.Add(time.Second))

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(21), service.OrganizationExecutionStateAccepted))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(attempt.AttemptID, attempt.Provenance, attempt.ProvenanceHash).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(attempt.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id FROM accounts WHERE id = \\$1 AND deleted_at IS NULL FOR UPDATE").
		WithArgs(attempt.AccountID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(attempt.AccountID))
	mock.ExpectQuery("SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id").
		WithArgs(attempt.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM organization_execution_attempts WHERE execution_ref_id = \\$1").
		WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery("INSERT INTO organization_execution_attempts").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(22)))
	mock.ExpectQuery("INSERT INTO organization_attempt_evidences").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(23)))
	mock.ExpectExec("UPDATE organization_executions SET status = \\$1").
		WithArgs(service.OrganizationExecutionStateDispatched, int64(21), service.OrganizationExecutionStateAccepted).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE organization_executions SET status = \\$1").
		WithArgs(service.OrganizationExecutionStateFinalUsageObserved, int64(21), service.OrganizationExecutionStateAccepted, service.OrganizationExecutionStateDispatched).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	attemptResult, err := repository.RecordNonBillableAttemptEvidence(context.Background(), attempt)
	require.NoError(t, err)
	require.Equal(t, int64(23), attemptResult.AttemptEvidenceID)

	terminal := service.OrganizationNonBillableTerminalizationCommand{
		ExecutionID: execution.ExecutionID, CorrelationID: execution.CorrelationID,
		OrganizationID: 1, UserID: 2, APIKeyID: 3, AllocationID: 4,
		FinalAttemptID: &attempt.AttemptID, FinalAttemptEvidenceID: &attemptResult.AttemptEvidenceID,
		TerminalOutcome: service.OrganizationExecutionTerminalOutcomeSuccess, OccurredAt: acceptedAt.Add(2 * time.Second),
	}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, organization_id, user_id_snapshot, user_ref_id, status, available, reserved, version").
		WithArgs(int64(4)).WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "user_id_snapshot", "user_ref_id", "status", "available", "reserved", "version"}).
		AddRow(int64(4), int64(1), int64(2), int64(2), domain.OrganizationAllocationStatusActive, "0.00000000", "0.00000000", int64(9)))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(21), service.OrganizationExecutionStateFinalUsageObserved))
	mock.ExpectQuery("SELECT id, execution_ref_id, execution_id_snapshot, organization_id").
		WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT evidence.id, attempt.attempt_id, attempt.id, evidence.execution_ref_id").
		WithArgs(int64(23), attempt.AttemptID).WillReturnRows(sqlmock.NewRows([]string{
		"id", "attempt_id", "attempt_ref_id", "execution_ref_id", "organization_id", "account_id", "evidence_source", "provenance_hash", "terminal_outcome", "raw_actual_cost", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
	}).AddRow(int64(23), attempt.AttemptID, int64(22), int64(21), int64(1), int64(9), attempt.Provenance, attempt.ProvenanceHash, attempt.TerminalOutcome, "0", int64(37), int64(2), int64(3), int64(5)))
	mock.ExpectQuery("SELECT COUNT\\(DISTINCT attempt.id\\), COUNT\\(evidence.id\\)").
		WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"attempt_count", "evidence_count"}).AddRow(int64(1), int64(1)))
	mock.ExpectQuery("SELECT id FROM users WHERE id = \\$1 AND deleted_at IS NULL").
		WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(2)))
	mock.ExpectQuery("SELECT id FROM api_keys WHERE id = \\$1 AND deleted_at IS NULL").
		WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(3)))
	mock.ExpectQuery("SELECT id FROM accounts WHERE id = \\$1 AND deleted_at IS NULL FOR UPDATE").
		WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9)))
	mock.ExpectQuery("INSERT INTO organization_final_execution_evidences").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(24)))
	mock.ExpectExec("UPDATE organization_executions SET status = \\$1").
		WithArgs(service.OrganizationExecutionStateReleased, terminal.OccurredAt, int64(21), service.OrganizationExecutionStateAccepted, service.OrganizationExecutionStateFinalUsageObserved).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO organization_execution_outboxes").WillReturnResult(sqlmock.NewResult(25, 1))
	mock.ExpectCommit()
	mock.ExpectClose()

	final, err := repository.TerminalizeNonBillableExecution(context.Background(), terminal)
	require.NoError(t, err)
	require.Equal(t, int64(37), final.InputTokens)
	require.Equal(t, int64(2), final.OutputTokens)
	require.Equal(t, int64(3), final.CacheCreationTokens)
	require.Equal(t, int64(5), final.CacheReadTokens)
	require.True(t, final.IsCountTokensProbe)
	require.Equal(t, "0.00000000", final.OrganizationSettledCost)
	require.Nil(t, final.ConsumptionPostingID)
	require.NoError(t, db.Close())
}

func TestOrganizationNonBillableRepositoryRejectsInvalidAttemptBeforeTransaction(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyTokenCounting, time.Now().UTC())
	command := repositoryNonBillableAttemptCommand(execution, time.Now().UTC())
	command.AttemptSequence = 2
	mock.ExpectClose()

	_, err := repository.RecordNonBillableAttemptEvidence(context.Background(), command)
	require.ErrorIs(t, err, service.ErrOrganizationExecutionInvalid)
	require.NoError(t, db.Close())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOrganizationNonBillablePendingAcceptsProviderQueryEvidence(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	acceptedAt := time.Date(2026, time.July, 31, 2, 15, 0, 123456000, time.UTC)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyTokenCounting, acceptedAt)
	pending := repositoryNonBillablePendingCommand(execution, acceptedAt.Add(time.Second))
	evidence := repositoryNonBillableAttemptCommand(execution, acceptedAt.Add(2*time.Second))
	evidence.AttemptID = pending.AttemptID
	evidence.AccountID = pending.AccountID
	evidence.RouteSnapshotJSON = pending.RouteSnapshotJSON
	evidence.RouteSnapshotHash = pending.RouteSnapshotHash
	evidence.PricingSnapshotJSON = pending.PricingSnapshotJSON
	evidence.PricingSnapshotHash = pending.PricingSnapshotHash
	evidence.ProviderRequestID = pending.ProviderRequestID
	evidence.ProviderCorrelationID = pending.ProviderCorrelationID
	evidence.Provenance = service.OrganizationAttemptEvidenceProvenanceProviderQuery

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(81), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(evidence.AttemptID, evidence.Provenance, evidence.ProvenanceHash).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(evidence.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id FROM accounts WHERE id = \\$1 AND deleted_at IS NULL FOR UPDATE").
		WithArgs(evidence.AccountID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(evidence.AccountID))
	mock.ExpectQuery("SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id").
		WithArgs(evidence.AttemptID).WillReturnRows(nonBillableAttemptRows(pending, int64(82), int64(81)))
	mock.ExpectQuery("INSERT INTO organization_attempt_evidences").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(83)))
	mock.ExpectCommit()
	mock.ExpectClose()

	result, err := repository.RecordNonBillableAttemptEvidence(context.Background(), evidence)
	require.NoError(t, err)
	require.Equal(t, int64(83), result.AttemptEvidenceID)
	require.NoError(t, db.Close())
}

func TestOrganizationNonBillableRecoveryEvidenceDerivesStoredAttemptIdentity(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	acceptedAt := time.Date(2026, time.July, 31, 2, 17, 0, 123456000, time.UTC)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyTokenCounting, acceptedAt)
	pending := repositoryNonBillablePendingCommand(execution, acceptedAt.Add(time.Second))
	evidenceJSON := `{"provider_status":"complete","usage":{"input":37}}`
	evidenceHash := nonBillableTestHash(evidenceJSON)
	command := service.OrganizationNonBillableRecoveryEvidenceCommand{
		ExecutionID: execution.ExecutionID, OrganizationID: execution.Attribution.OrganizationID, AttemptID: pending.AttemptID,
		TerminalOutcome: service.OrganizationExecutionTerminalOutcomeSuccess, EvidenceJSON: evidenceJSON, EvidenceHash: evidenceHash,
		InputTokens: 37, OutputTokens: 2, CacheCreationTokens: 3, CacheReadTokens: 5,
		ProviderObservedAt: acceptedAt.Add(2 * time.Second), IngestedAt: acceptedAt.Add(3 * time.Second),
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(81), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id").
		WithArgs(pending.AttemptID).WillReturnRows(nonBillableAttemptRows(pending, int64(82), int64(81)))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(81), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(pending.AttemptID, service.OrganizationAttemptEvidenceProvenanceProviderQuery, evidenceHash).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(pending.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id FROM accounts WHERE id = \\$1 AND deleted_at IS NULL FOR UPDATE").
		WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9)))
	mock.ExpectQuery("SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id").
		WithArgs(pending.AttemptID).WillReturnRows(nonBillableAttemptRows(pending, int64(82), int64(81)))
	mock.ExpectQuery("INSERT INTO organization_attempt_evidences").
		WithArgs(pending.AttemptID, int64(82), execution.ExecutionID, int64(81), int64(1), int64(9), int64(9),
			pending.RouteSnapshotJSON, pending.RouteSnapshotHash, pending.PricingSnapshotJSON, pending.PricingSnapshotHash,
			pending.ProviderCorrelationID, pending.ProviderRequestID, service.OrganizationAttemptEvidenceProvenanceProviderQuery,
			service.OrganizationExecutionTerminalOutcomeSuccess, evidenceHash, evidenceJSON, evidenceHash, int64(37), int64(2), int64(3), int64(5), "0",
			command.ProviderObservedAt, command.IngestedAt).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(83)))
	mock.ExpectCommit()
	mock.ExpectClose()

	result, err := repository.RecordNonBillableRecoveryEvidence(context.Background(), command)
	require.NoError(t, err)
	require.Equal(t, int64(83), result.AttemptEvidenceID)
	require.Equal(t, pending.AttemptID, result.AttemptID)
	require.False(t, result.Replayed)
	require.NoError(t, db.Close())
}

func TestOrganizationNonBillableRecoveryEvidenceRejectsAttemptFromAnotherExecution(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	acceptedAt := time.Date(2026, time.July, 31, 2, 18, 0, 123456000, time.UTC)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyTokenCounting, acceptedAt)
	pending := repositoryNonBillablePendingCommand(execution, acceptedAt.Add(time.Second))
	command := service.OrganizationNonBillableRecoveryEvidenceCommand{
		ExecutionID: execution.ExecutionID, OrganizationID: execution.Attribution.OrganizationID, AttemptID: pending.AttemptID,
		TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, EvidenceJSON: "{}", EvidenceHash: nonBillableTestHash("{}"),
		ProviderObservedAt: acceptedAt.Add(2 * time.Second), IngestedAt: acceptedAt.Add(3 * time.Second),
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(81), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id").
		WithArgs(pending.AttemptID).WillReturnRows(nonBillableAttemptRows(pending, int64(82), int64(999)))
	mock.ExpectRollback()
	mock.ExpectClose()

	_, err := repository.RecordNonBillableRecoveryEvidence(context.Background(), command)
	require.ErrorIs(t, err, service.ErrOrganizationExecutionConflict)
	require.NoError(t, db.Close())
}

func TestOrganizationNonBillableProviderQueryReplayIgnoresOnlyIngestTimestamp(t *testing.T) {
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyTokenCounting, time.Now().UTC())
	command := repositoryNonBillableAttemptCommand(execution, time.Now().UTC())
	command.Provenance = service.OrganizationAttemptEvidenceProvenanceProviderQuery
	record := &organizationAttemptEvidenceReplayRecord{
		ID: 83, AttemptID: command.AttemptID, ExecutionID: command.ExecutionID, ExecutionRefID: 81,
		OrganizationID: command.OrganizationID, AttemptSequence: command.AttemptSequence, AccountID: sql.NullInt64{Int64: command.AccountID, Valid: true},
		RouteSnapshotHash: command.RouteSnapshotHash, PricingSnapshotHash: command.PricingSnapshotHash,
		ProviderRequestID: command.ProviderRequestID, ProviderCorrelation: command.ProviderCorrelationID,
		EvidenceSource: command.Provenance, TerminalOutcome: command.TerminalOutcome, ProvenanceHash: command.ProvenanceHash, EvidenceHash: command.EvidenceHash,
		InputTokens: command.InputTokens, OutputTokens: command.OutputTokens, CacheCreationTokens: command.CacheCreationTokens, CacheReadTokens: command.CacheReadTokens,
		RawActualCost: command.RawActualCost, ProviderObservedAt: command.ProviderObservedAt.UTC().Truncate(time.Microsecond), IngestedAt: command.IngestedAt.UTC().Truncate(time.Microsecond),
	}

	retry := command
	retry.IngestedAt = command.IngestedAt.Add(time.Minute)
	require.True(t, sameNonBillableAttemptEvidence(record, retry))
	retry.OutputTokens++
	require.False(t, sameNonBillableAttemptEvidence(record, retry))
}

func TestOrganizationNonBillablePendingIsVisibleWithoutReservation(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	acceptedAt := time.Date(2026, time.July, 31, 2, 18, 0, 123456000, time.UTC)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyTokenCounting, acceptedAt)
	pending := repositoryNonBillablePendingCommand(execution, acceptedAt.Add(time.Second))

	mock.ExpectQuery("FROM organization_executions AS execution").
		WithArgs(int64(1), execution.ExecutionID, service.OrganizationExecutionStatePendingReconciliation, service.OrganizationReservationStatePendingReconciliation).
		WillReturnRows(sqlmock.NewRows([]string{"execution_ref_id", "execution_id", "organization_id", "allocation_id", "reservation_id", "candidate_ceiling", "reserved_amount", "accepted_at"}).
			AddRow(int64(91), execution.ExecutionID, int64(1), int64(4), int64(0), "0.00000000", "0", acceptedAt))
	mock.ExpectQuery("SELECT attempt_id, attempt_sequence, status").
		WithArgs(int64(91)).WillReturnRows(sqlmock.NewRows([]string{"attempt_id", "attempt_sequence", "status"}).
		AddRow(pending.AttemptID, 1, service.OrganizationExecutionStateDispatched))
	mock.ExpectQuery("SELECT evidence.id, attempt.attempt_id, attempt.id, attempt.attempt_sequence").
		WithArgs(int64(91)).WillReturnRows(sqlmock.NewRows([]string{
		"id", "attempt_id", "attempt_ref_id", "attempt_sequence", "execution_id", "execution_ref_id", "organization_id", "account_id", "evidence_source", "provenance_hash", "evidence_hash", "terminal_outcome", "raw_actual_cost", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "provider_observed_at",
	}))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM organization_executions AS execution").
		WithArgs(int64(1), service.OrganizationExecutionStatePendingReconciliation, service.OrganizationReservationStatePendingReconciliation).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("SELECT execution.id, execution.execution_id, execution.organization_id").
		WithArgs(int64(1), service.OrganizationExecutionStatePendingReconciliation, service.OrganizationReservationStatePendingReconciliation, 1, 0).
		WillReturnRows(sqlmock.NewRows([]string{"execution_ref_id", "execution_id", "organization_id", "allocation_id", "reservation_id", "candidate_ceiling", "reserved_amount", "accepted_at"}).
			AddRow(int64(91), execution.ExecutionID, int64(1), int64(4), int64(0), "0.00000000", "0", acceptedAt))
	mock.ExpectQuery("SELECT attempt_id, attempt_sequence, status").
		WithArgs(int64(91)).WillReturnRows(sqlmock.NewRows([]string{"attempt_id", "attempt_sequence", "status"}).
		AddRow(pending.AttemptID, 1, service.OrganizationExecutionStateDispatched))
	mock.ExpectQuery("SELECT evidence.id, attempt.attempt_id, attempt.id, attempt.attempt_sequence").
		WithArgs(int64(91)).WillReturnRows(sqlmock.NewRows([]string{
		"id", "attempt_id", "attempt_ref_id", "attempt_sequence", "execution_id", "execution_ref_id", "organization_id", "account_id", "evidence_source", "provenance_hash", "evidence_hash", "terminal_outcome", "raw_actual_cost", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "provider_observed_at",
	}))
	item, err := repository.GetPendingReconciliation(context.Background(), 1, execution.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, int64(0), item.ReservationID)
	require.Equal(t, "0", item.ReservedAmount)
	require.Equal(t, 1, item.AttemptCount)
	require.Empty(t, item.AllowedResolution)

	items, page, err := repository.ListPendingReconciliations(context.Background(), 1, service.OrganizationPendingReconciliationListParams{Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(1), page.Total)
	require.Equal(t, int64(0), items[0].ReservationID)
	require.NoError(t, mock.ExpectationsWereMet())
	_ = db
}

func TestOrganizationNonBillablePendingReconciliationFinalizesAndReplays(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	acceptedAt := time.Date(2026, time.July, 31, 2, 19, 0, 123456000, time.UTC)
	observedAt := acceptedAt.Add(2 * time.Second)
	reconciledAt := acceptedAt.Add(3 * time.Second)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyTokenCounting, acceptedAt)
	pending := repositoryNonBillablePendingCommand(execution, acceptedAt.Add(time.Second))
	evidence := repositoryNonBillableAttemptCommand(execution, observedAt)
	evidence.AttemptID = pending.AttemptID
	evidence.AccountID = pending.AccountID
	evidence.RouteSnapshotJSON = pending.RouteSnapshotJSON
	evidence.RouteSnapshotHash = pending.RouteSnapshotHash
	evidence.PricingSnapshotJSON = pending.PricingSnapshotJSON
	evidence.PricingSnapshotHash = pending.PricingSnapshotHash
	evidence.ProviderRequestID = pending.ProviderRequestID
	evidence.ProviderCorrelationID = pending.ProviderCorrelationID
	evidence.Provenance = service.OrganizationAttemptEvidenceProvenanceProviderQuery
	command := service.OrganizationPendingReconciliationCommand{
		ExecutionID: execution.ExecutionID, OrganizationID: execution.Attribution.OrganizationID,
		OperationID: uuid.NewString(), CorrelationID: uuid.NewString(),
		IdempotencyKeyHash: strings.Repeat("b", 64), CanonicalPayloadHash: strings.Repeat("c", 64),
		Actor: service.OrganizationReconciliationActor{
			UserID: 42, DisplayName: "platform admin", PlatformRole: domain.RoleAdmin,
			AuthorizationScope: domain.OrganizationFundingAuthorizationPlatformAdminJWT, Active: true,
		},
		ReconciledAt: reconciledAt,
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStatePendingReconciliation))
	expectNonBillableAllocationLock(mock, int64(4), int64(9))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("SELECT execution_ref_id, canonical_payload_hash").
		WithArgs(int64(1), command.Actor.AuthorizationScope, command.IdempotencyKeyHash).WillReturnRows(sqlmock.NewRows([]string{"execution_ref_id"}))
	mock.ExpectQuery("SELECT id, execution_ref_id, execution_id_snapshot, organization_id").
		WithArgs(int64(101)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)").
		WithArgs(int64(101)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT evidence.id, attempt.attempt_id, attempt.id, attempt.attempt_sequence").
		WithArgs(int64(101)).WillReturnRows(nonBillableReconciliationCandidateRows(execution, pending, evidence, int64(102), int64(103)))
	expectNonBillableAllocationLock(mock, int64(4), int64(9))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("SELECT id, execution_ref_id, execution_id_snapshot, organization_id").
		WithArgs(int64(101)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT evidence.id, attempt.attempt_id, attempt.id, evidence.execution_ref_id").
		WithArgs(int64(103), pending.AttemptID).WillReturnRows(nonBillableFinalAttemptEvidenceRows(execution, pending, evidence, int64(102), int64(103)))
	mock.ExpectQuery("SELECT COUNT\\(DISTINCT attempt.id\\), COUNT\\(evidence.id\\)").
		WithArgs(int64(101)).WillReturnRows(sqlmock.NewRows([]string{"attempt_count", "evidence_count"}).AddRow(1, 1))
	mock.ExpectQuery("SELECT id FROM users WHERE id = \\$1 AND deleted_at IS NULL").
		WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(2)))
	mock.ExpectQuery("SELECT id FROM api_keys WHERE id = \\$1 AND deleted_at IS NULL").
		WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(3)))
	mock.ExpectQuery("SELECT id FROM accounts WHERE id = \\$1 AND deleted_at IS NULL FOR UPDATE").
		WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9)))
	mock.ExpectQuery("INSERT INTO organization_final_execution_evidences").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(104)))
	mock.ExpectExec("UPDATE organization_executions SET status = \\$1").
		WithArgs(service.OrganizationExecutionStateReleased, observedAt, int64(101), service.OrganizationExecutionStateAccepted, service.OrganizationExecutionStateFinalUsageObserved, service.OrganizationExecutionStatePendingReconciliation).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO organization_execution_outboxes").WillReturnResult(sqlmock.NewResult(105, 1))
	mock.ExpectQuery("SELECT id FROM users WHERE id = \\$1 AND deleted_at IS NULL").
		WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mock.ExpectExec("INSERT INTO organization_execution_reconciliation_audits").WillReturnResult(sqlmock.NewResult(106, 1))
	mock.ExpectCommit()

	final, err := repository.ReconcilePendingExecution(context.Background(), command)
	require.NoError(t, err)
	require.Equal(t, int64(104), final.FinalEvidenceID)
	require.Equal(t, service.OrganizationReservationStateReleased, final.ReservationState)
	require.Equal(t, "0.00000000", final.OrganizationSettledCost)
	require.NotNil(t, final.ReconciledAt)
	require.False(t, final.Replayed)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStateReleased))
	expectNonBillableAllocationLock(mock, int64(4), int64(9))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStateReleased))
	mock.ExpectQuery("SELECT execution_ref_id, canonical_payload_hash").
		WithArgs(int64(1), command.Actor.AuthorizationScope, command.IdempotencyKeyHash).
		WillReturnRows(sqlmock.NewRows([]string{"execution_ref_id", "canonical_payload_hash"}).AddRow(int64(101), command.CanonicalPayloadHash))
	mock.ExpectQuery("SELECT id, execution_ref_id, execution_id_snapshot, organization_id").
		WithArgs(int64(101)).WillReturnRows(nonBillableReconciledFinalRows(execution, pending, evidence, int64(101), int64(102), int64(103), int64(104), observedAt, reconciledAt))
	mock.ExpectCommit()
	mock.ExpectClose()

	replayed, err := repository.ReconcilePendingExecution(context.Background(), command)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, int64(104), replayed.FinalEvidenceID)
	require.NoError(t, db.Close())
}

func TestOrganizationNonBillableModelsAttemptFinalIsNotCountTokensProbe(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	acceptedAt := time.Date(2026, time.July, 31, 2, 20, 0, 123456000, time.UTC)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyModels, acceptedAt)
	attempt := repositoryNonBillableAttemptCommand(execution, acceptedAt.Add(time.Second))
	attempt.InputTokens, attempt.OutputTokens, attempt.CacheCreationTokens, attempt.CacheReadTokens = 0, 0, 0, 0
	evidenceID := int64(53)
	terminal := service.OrganizationNonBillableTerminalizationCommand{
		ExecutionID: execution.ExecutionID, CorrelationID: execution.CorrelationID,
		OrganizationID: 1, UserID: 2, APIKeyID: 3, AllocationID: 4,
		FinalAttemptID: &attempt.AttemptID, FinalAttemptEvidenceID: &evidenceID,
		TerminalOutcome: service.OrganizationExecutionTerminalOutcomeSuccess, OccurredAt: acceptedAt.Add(2 * time.Second),
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, organization_id, user_id_snapshot, user_ref_id, status, available, reserved, version").
		WithArgs(int64(4)).WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "user_id_snapshot", "user_ref_id", "status", "available", "reserved", "version"}).
		AddRow(int64(4), int64(1), int64(2), int64(2), domain.OrganizationAllocationStatusActive, "0.00000000", "0.00000000", int64(10)))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(51), service.OrganizationExecutionStateFinalUsageObserved))
	mock.ExpectQuery("SELECT id, execution_ref_id, execution_id_snapshot, organization_id").
		WithArgs(int64(51)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT evidence.id, attempt.attempt_id, attempt.id, evidence.execution_ref_id").
		WithArgs(evidenceID, attempt.AttemptID).WillReturnRows(sqlmock.NewRows([]string{
		"id", "attempt_id", "attempt_ref_id", "execution_ref_id", "organization_id", "account_id", "evidence_source", "provenance_hash", "terminal_outcome", "raw_actual_cost", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
	}).AddRow(evidenceID, attempt.AttemptID, int64(52), int64(51), int64(1), int64(9), attempt.Provenance, attempt.ProvenanceHash, attempt.TerminalOutcome, "0", int64(0), int64(0), int64(0), int64(0)))
	mock.ExpectQuery("SELECT COUNT\\(DISTINCT attempt.id\\), COUNT\\(evidence.id\\)").
		WithArgs(int64(51)).WillReturnRows(sqlmock.NewRows([]string{"attempt_count", "evidence_count"}).AddRow(int64(1), int64(1)))
	mock.ExpectQuery("SELECT id FROM users WHERE id = \\$1 AND deleted_at IS NULL").WithArgs(int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(2)))
	mock.ExpectQuery("SELECT id FROM api_keys WHERE id = \\$1 AND deleted_at IS NULL").WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(3)))
	mock.ExpectQuery("SELECT id FROM accounts WHERE id = \\$1 AND deleted_at IS NULL FOR UPDATE").WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9)))
	mock.ExpectQuery("INSERT INTO organization_final_execution_evidences").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(54)))
	mock.ExpectExec("UPDATE organization_executions SET status = \\$1").
		WithArgs(service.OrganizationExecutionStateReleased, terminal.OccurredAt, int64(51), service.OrganizationExecutionStateAccepted, service.OrganizationExecutionStateFinalUsageObserved).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO organization_execution_outboxes").WillReturnResult(sqlmock.NewResult(55, 1))
	mock.ExpectCommit()
	mock.ExpectClose()

	final, err := repository.TerminalizeNonBillableExecution(context.Background(), terminal)
	require.NoError(t, err)
	require.False(t, final.IsCountTokensProbe)
	require.Equal(t, int64(4), final.AllocationID)
	require.NoError(t, db.Close())
}

func TestOrganizationNonBillableTerminalReplayDoesNotCreateSecondFinalOrOutbox(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	occurredAt := time.Date(2026, time.July, 31, 2, 25, 0, 123456000, time.UTC)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyUsage, occurredAt)
	terminal := service.OrganizationNonBillableTerminalizationCommand{
		ExecutionID: execution.ExecutionID, CorrelationID: execution.CorrelationID,
		OrganizationID: 1, UserID: 2, APIKeyID: 3, AllocationID: 4,
		TerminalOutcome: service.OrganizationExecutionTerminalOutcomeSuccess, ZeroProof: "local_usage_response", OccurredAt: occurredAt,
	}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, organization_id, user_id_snapshot, user_ref_id, status, available, reserved, version").
		WithArgs(int64(4)).WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "user_id_snapshot", "user_ref_id", "status", "available", "reserved", "version"}).
		AddRow(int64(4), int64(1), int64(2), int64(2), domain.OrganizationAllocationStatusActive, "0.00000000", "0.00000000", int64(9)))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(61), service.OrganizationExecutionStateReleased))
	mock.ExpectQuery("SELECT id, execution_ref_id, execution_id_snapshot, organization_id").
		WithArgs(int64(61)).WillReturnRows(nonBillableFinalRows(execution, int64(62), terminal, false))
	mock.ExpectCommit()
	mock.ExpectClose()

	final, err := repository.TerminalizeNonBillableExecution(context.Background(), terminal)
	require.NoError(t, err)
	require.True(t, final.Replayed)
	require.Equal(t, int64(62), final.FinalEvidenceID)
	require.NoError(t, db.Close())
}

func TestOrganizationNonBillablePendingAttemptIsIdempotentAndNeverFinalizes(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	acceptedAt := time.Date(2026, time.July, 31, 2, 30, 0, 123456000, time.UTC)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyTokenCounting, acceptedAt)
	pending := repositoryNonBillablePendingCommand(execution, acceptedAt.Add(time.Second))

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(31), service.OrganizationExecutionStateAccepted))
	mock.ExpectQuery("SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id").
		WithArgs(pending.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(pending.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id FROM accounts WHERE id = \\$1 AND deleted_at IS NULL FOR UPDATE").
		WithArgs(pending.AccountID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(pending.AccountID))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM organization_execution_attempts WHERE execution_ref_id = \\$1").
		WithArgs(int64(31)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery("INSERT INTO organization_execution_attempts").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(32)))
	mock.ExpectExec("UPDATE organization_executions SET status = \\$1").
		WithArgs(service.OrganizationExecutionStatePendingReconciliation, int64(31), service.OrganizationExecutionStateAccepted, service.OrganizationExecutionStateDispatched, service.OrganizationExecutionStateFinalUsageObserved).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repository.RecordNonBillablePendingAttempt(context.Background(), pending))

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(31), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id").
		WithArgs(pending.AttemptID).WillReturnRows(nonBillableAttemptRows(pending, int64(32), int64(31)))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(pending.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT dispatched_at FROM organization_execution_attempts WHERE id = \\$1").
		WithArgs(int64(32)).WillReturnRows(sqlmock.NewRows([]string{"dispatched_at"}).AddRow(pending.DispatchedAt))
	mock.ExpectCommit()
	mock.ExpectClose()

	require.NoError(t, repository.RecordNonBillablePendingAttempt(context.Background(), pending))
	require.NoError(t, db.Close())
}

func TestOrganizationNonBillablePendingReplayRejectsDifferentIdentity(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	acceptedAt := time.Date(2026, time.July, 31, 2, 40, 0, 123456000, time.UTC)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyModels, acceptedAt)
	original := repositoryNonBillablePendingCommand(execution, acceptedAt.Add(time.Second))
	conflict := original
	conflict.AccountID = 10

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(41), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id").
		WithArgs(conflict.AttemptID).WillReturnRows(nonBillableAttemptRows(original, int64(42), int64(41)))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(conflict.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	mock.ExpectClose()

	err := repository.RecordNonBillablePendingAttempt(context.Background(), conflict)
	require.ErrorIs(t, err, service.ErrOrganizationExecutionConflict)
	require.NoError(t, db.Close())
}

func TestOrganizationNonBillablePendingReplayRejectsDifferentDispatchTimestamp(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	acceptedAt := time.Date(2026, time.July, 31, 2, 45, 0, 123456000, time.UTC)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyTokenCounting, acceptedAt)
	original := repositoryNonBillablePendingCommand(execution, acceptedAt.Add(time.Second))
	conflict := original
	conflict.DispatchedAt = original.DispatchedAt.Add(time.Second)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(45), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id").
		WithArgs(conflict.AttemptID).WillReturnRows(nonBillableAttemptRows(original, int64(46), int64(45)))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(conflict.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT dispatched_at FROM organization_execution_attempts WHERE id = \\$1").
		WithArgs(int64(46)).WillReturnRows(sqlmock.NewRows([]string{"dispatched_at"}).AddRow(original.DispatchedAt))
	mock.ExpectRollback()
	mock.ExpectClose()

	err := repository.RecordNonBillablePendingAttempt(context.Background(), conflict)
	require.ErrorIs(t, err, service.ErrOrganizationExecutionConflict)
	require.NoError(t, db.Close())
}

func newNonBillableRepositoryTest(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *organizationExecutionRepository) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	return db, mock, NewOrganizationExecutionRepository(client)
}

func repositoryNonBillableCommand(family string, acceptedAt time.Time) service.OrganizationNonBillableExecutionCommand {
	canonical := "{}"
	hash := nonBillableTestHash(canonical)
	shape := service.OrganizationRequestShapeHTTP
	if family == service.OrganizationEndpointFamilyTokenCounting {
		shape = service.OrganizationRequestShapeProbe
	}
	return service.OrganizationNonBillableExecutionCommand{
		ExecutionID: uuid.NewString(), CorrelationID: uuid.NewString(), EndpointFamily: family, RequestShape: shape,
		CandidateSnapshotJSON: canonical, CandidateSetHash: hash, PricingSnapshotJSON: canonical, PricingSnapshotHash: hash,
		BillingFingerprint: strings.Repeat("a", 64), AcceptedAt: acceptedAt,
		Attribution: service.OrganizationNonBillableExecutionTrustedAttribution{
			OrganizationID: 1, OrganizationStatus: service.OrganizationStatusActive,
			UserID: 2, UserOrganizationID: 1, APIKeyID: 3, APIKeyUserID: 2, APIKeyOrganizationID: 1,
			AllocationID: 4, AllocationUserID: 2, AllocationOrganizationID: 1,
			AllocationStatus: domain.OrganizationAllocationStatusActive, AllocationVersionSnapshot: 9,
		},
	}
}

func repositoryNonBillableAttemptCommand(execution service.OrganizationNonBillableExecutionCommand, observedAt time.Time) service.OrganizationNonBillableAttemptEvidenceCommand {
	canonical := "{}"
	hash := nonBillableTestHash(canonical)
	return service.OrganizationNonBillableAttemptEvidenceCommand{
		ExecutionID: execution.ExecutionID, AttemptID: uuid.NewString(), CorrelationID: execution.CorrelationID,
		OrganizationID: 1, UserID: 2, APIKeyID: 3, AllocationID: 4, AttemptSequence: 1, AccountID: 9,
		RouteSnapshotJSON: canonical, RouteSnapshotHash: hash, PricingSnapshotJSON: execution.PricingSnapshotJSON, PricingSnapshotHash: execution.PricingSnapshotHash,
		ProviderRequestID: "provider-request", ProviderCorrelationID: uuid.NewString(), Provenance: service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
		TerminalOutcome: service.OrganizationExecutionTerminalOutcomeSuccess, ProvenanceHash: hash, EvidenceJSON: canonical, EvidenceHash: hash,
		RawActualCost: "0", InputTokens: 37, OutputTokens: 2, CacheCreationTokens: 3, CacheReadTokens: 5,
		ProviderObservedAt: observedAt, IngestedAt: observedAt,
	}
}

func repositoryNonBillablePendingCommand(execution service.OrganizationNonBillableExecutionCommand, dispatchedAt time.Time) service.OrganizationNonBillablePendingAttemptCommand {
	canonical := "{}"
	hash := nonBillableTestHash(canonical)
	return service.OrganizationNonBillablePendingAttemptCommand{
		ExecutionID: execution.ExecutionID, AttemptID: uuid.NewString(), CorrelationID: execution.CorrelationID,
		OrganizationID: 1, UserID: 2, APIKeyID: 3, AllocationID: 4, AttemptSequence: 1, AccountID: 9,
		RouteSnapshotJSON: canonical, RouteSnapshotHash: hash, PricingSnapshotJSON: execution.PricingSnapshotJSON, PricingSnapshotHash: execution.PricingSnapshotHash,
		ProviderRequestID: "provider-request", ProviderCorrelationID: uuid.NewString(), DispatchedAt: dispatchedAt,
	}
}

func nonBillableExecutionRows(command service.OrganizationNonBillableExecutionCommand, id int64, status string) *sqlmock.Rows {
	return nonBillableExecutionRowsWithRef(command, id, status, command.Attribution.AllocationID)
}

func nonBillableExecutionRowsWithRef(command service.OrganizationNonBillableExecutionCommand, id int64, status string, allocationRef any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "execution_id", "correlation_id", "organization_id", "allocation_id_snapshot", "allocation_ref_id", "user_id_snapshot", "user_ref_id", "api_key_id_snapshot", "api_key_ref_id",
		"endpoint_family", "request_shape", "model", "client_request_id", "billing_fingerprint", "candidate_snapshot", "candidate_set_hash",
		"pricing_snapshot", "pricing_snapshot_hash", "candidate_ceiling", "is_billable", "status", "accepted_at", "terminalized_at",
	}).AddRow(id, command.ExecutionID, command.CorrelationID, command.Attribution.OrganizationID, command.Attribution.AllocationID, allocationRef, command.Attribution.UserID, command.Attribution.UserID,
		command.Attribution.APIKeyID, command.Attribution.APIKeyID, command.EndpointFamily, command.RequestShape, nil, nil, command.BillingFingerprint,
		command.CandidateSnapshotJSON, command.CandidateSetHash, command.PricingSnapshotJSON, command.PricingSnapshotHash, "0.00000000", false, status, command.AcceptedAt, nil)
}

func nonBillableAttemptRows(command service.OrganizationNonBillablePendingAttemptCommand, attemptID, executionRefID int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "attempt_id", "execution_id_snapshot", "execution_ref_id", "organization_id", "attempt_sequence", "account_id_snapshot", "account_ref_id",
		"route_snapshot", "route_snapshot_hash", "pricing_snapshot", "pricing_snapshot_hash", "provider_correlation_id", "provider_request_id", "status",
	}).AddRow(attemptID, command.AttemptID, command.ExecutionID, executionRefID, command.OrganizationID, command.AttemptSequence, command.AccountID, command.AccountID,
		command.RouteSnapshotJSON, command.RouteSnapshotHash, command.PricingSnapshotJSON, command.PricingSnapshotHash, command.ProviderCorrelationID, command.ProviderRequestID, service.OrganizationExecutionStateDispatched)
}

func nonBillableFinalRows(command service.OrganizationNonBillableExecutionCommand, id int64, terminal service.OrganizationNonBillableTerminalizationCommand, probe bool) *sqlmock.Rows {
	proof := `{"reason":"` + terminal.ZeroProof + `"}`
	return sqlmock.NewRows([]string{
		"id", "execution_ref_id", "execution_id_snapshot", "organization_id", "allocation_id_snapshot", "allocation_ref_id", "user_id_snapshot", "api_key_id_snapshot", "final_attempt_id_snapshot", "final_attempt_evidence_id_snapshot", "terminal_outcome", "is_business_limited", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "raw_actual_cost", "organization_settled_cost", "is_count_tokens_probe", "zero_cost_proof", "occurred_at", "reconciled_at",
	}).AddRow(id, int64(61), command.ExecutionID, command.Attribution.OrganizationID, command.Attribution.AllocationID, command.Attribution.AllocationID, command.Attribution.UserID, command.Attribution.APIKeyID, nil, nil, terminal.TerminalOutcome, terminal.BusinessLimited, int64(0), int64(0), int64(0), int64(0), "0", "0.00000000", probe, proof, terminal.OccurredAt, nil)
}

func expectNonBillableAllocationLock(mock sqlmock.Sqlmock, allocationID, version int64) {
	mock.ExpectQuery("SELECT id, organization_id, user_id_snapshot, user_ref_id, status, available, reserved, version").
		WithArgs(allocationID).WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "user_id_snapshot", "user_ref_id", "status", "available", "reserved", "version"}).
		AddRow(allocationID, int64(1), int64(2), int64(2), domain.OrganizationAllocationStatusActive, "0.00000000", "0.00000000", version))
}

func nonBillableReconciliationCandidateRows(execution service.OrganizationNonBillableExecutionCommand, pending service.OrganizationNonBillablePendingAttemptCommand, evidence service.OrganizationNonBillableAttemptEvidenceCommand, attemptRefID, evidenceID int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "attempt_id", "attempt_ref_id", "attempt_sequence", "execution_id", "execution_ref_id", "organization_id", "account_id", "evidence_source", "provenance_hash", "evidence_hash", "terminal_outcome", "raw_actual_cost", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "provider_observed_at",
	}).AddRow(evidenceID, pending.AttemptID, attemptRefID, 1, execution.ExecutionID, int64(101), int64(1), int64(9), evidence.Provenance, evidence.ProvenanceHash, evidence.EvidenceHash, evidence.TerminalOutcome, "0", evidence.InputTokens, evidence.OutputTokens, evidence.CacheCreationTokens, evidence.CacheReadTokens, evidence.ProviderObservedAt)
}

func nonBillableFinalAttemptEvidenceRows(execution service.OrganizationNonBillableExecutionCommand, pending service.OrganizationNonBillablePendingAttemptCommand, evidence service.OrganizationNonBillableAttemptEvidenceCommand, attemptRefID, evidenceID int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "attempt_id", "attempt_ref_id", "execution_ref_id", "organization_id", "account_id", "evidence_source", "provenance_hash", "terminal_outcome", "raw_actual_cost", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
	}).AddRow(evidenceID, pending.AttemptID, attemptRefID, int64(101), int64(1), int64(9), evidence.Provenance, evidence.ProvenanceHash, evidence.TerminalOutcome, "0", evidence.InputTokens, evidence.OutputTokens, evidence.CacheCreationTokens, evidence.CacheReadTokens)
}

func nonBillableReconciledFinalRows(execution service.OrganizationNonBillableExecutionCommand, pending service.OrganizationNonBillablePendingAttemptCommand, evidence service.OrganizationNonBillableAttemptEvidenceCommand, executionRefID, attemptRefID, evidenceID, finalID int64, occurredAt, reconciledAt time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "execution_ref_id", "execution_id_snapshot", "organization_id", "allocation_id_snapshot", "allocation_ref_id", "user_id_snapshot", "api_key_id_snapshot", "final_attempt_id_snapshot", "final_attempt_evidence_id_snapshot", "terminal_outcome", "is_business_limited", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "raw_actual_cost", "organization_settled_cost", "is_count_tokens_probe", "zero_cost_proof", "occurred_at", "reconciled_at",
	}).AddRow(finalID, executionRefID, execution.ExecutionID, int64(1), int64(4), int64(4), int64(2), int64(3), pending.AttemptID, evidenceID, evidence.TerminalOutcome, false, evidence.InputTokens, evidence.OutputTokens, evidence.CacheCreationTokens, evidence.CacheReadTokens, "0", "0.00000000", true, nil, occurredAt, reconciledAt)
}

func nonBillableTestHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
