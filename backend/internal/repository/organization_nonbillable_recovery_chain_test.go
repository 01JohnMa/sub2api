package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOrganizationNonBillablePendingRecoveryReconciliationChainIsZeroCostAndIdempotent(t *testing.T) {
	db, mock, repository := newNonBillableRepositoryTest(t)
	t.Cleanup(func() { _ = db.Close() })
	acceptedAt := time.Date(2026, time.July, 31, 3, 0, 0, 123456000, time.UTC)
	dispatchedAt := acceptedAt.Add(time.Second)
	observedAt := acceptedAt.Add(2 * time.Second)
	ingestedAt := acceptedAt.Add(3 * time.Second)
	reconciledAt := acceptedAt.Add(4 * time.Second)
	execution := repositoryNonBillableCommand(service.OrganizationEndpointFamilyTokenCounting, acceptedAt)
	pending := repositoryNonBillablePendingCommand(execution, dispatchedAt)
	evidenceJSON := `{"provider_status":"complete","usage":{"cache_creation":3,"cache_read":5,"input":37,"output":2}}`
	evidenceHash := nonBillableTestHash(evidenceJSON)
	recovery := service.OrganizationNonBillableRecoveryEvidenceCommand{
		ExecutionID: execution.ExecutionID, OrganizationID: execution.Attribution.OrganizationID, AttemptID: pending.AttemptID,
		TerminalOutcome: service.OrganizationExecutionTerminalOutcomeSuccess, EvidenceJSON: evidenceJSON, EvidenceHash: evidenceHash,
		InputTokens: 37, OutputTokens: 2, CacheCreationTokens: 3, CacheReadTokens: 5,
		ProviderObservedAt: observedAt, IngestedAt: ingestedAt,
	}
	evidence := service.OrganizationNonBillableAttemptEvidenceCommand{
		ExecutionID: execution.ExecutionID, AttemptID: pending.AttemptID, CorrelationID: execution.CorrelationID,
		OrganizationID: execution.Attribution.OrganizationID, UserID: execution.Attribution.UserID,
		APIKeyID: execution.Attribution.APIKeyID, AllocationID: execution.Attribution.AllocationID,
		AttemptSequence: pending.AttemptSequence, AccountID: pending.AccountID,
		RouteSnapshotJSON: pending.RouteSnapshotJSON, RouteSnapshotHash: pending.RouteSnapshotHash,
		PricingSnapshotJSON: pending.PricingSnapshotJSON, PricingSnapshotHash: pending.PricingSnapshotHash,
		ProviderRequestID: pending.ProviderRequestID, ProviderCorrelationID: pending.ProviderCorrelationID,
		Provenance:      service.OrganizationAttemptEvidenceProvenanceProviderQuery,
		TerminalOutcome: recovery.TerminalOutcome, ProvenanceHash: evidenceHash,
		EvidenceJSON: evidenceJSON, EvidenceHash: evidenceHash, RawActualCost: "0",
		InputTokens: recovery.InputTokens, OutputTokens: recovery.OutputTokens,
		CacheCreationTokens: recovery.CacheCreationTokens, CacheReadTokens: recovery.CacheReadTokens,
		ProviderObservedAt: observedAt, IngestedAt: ingestedAt,
	}
	reconcile := service.OrganizationPendingReconciliationCommand{
		ExecutionID: execution.ExecutionID, OrganizationID: execution.Attribution.OrganizationID,
		OperationID: uuid.NewString(), CorrelationID: uuid.NewString(),
		IdempotencyKeyHash: strings.Repeat("d", 64), CanonicalPayloadHash: strings.Repeat("e", 64),
		Actor: service.OrganizationReconciliationActor{
			UserID: 42, DisplayName: "platform admin", PlatformRole: domain.RoleAdmin,
			AuthorizationScope: domain.OrganizationFundingAuthorizationPlatformAdminJWT, Active: true,
		},
		ReconciledAt: reconciledAt,
	}

	// The provider was reached but its terminal usage was initially unknown, so
	// the writer persists one dispatched attempt and leaves the parent pending.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStateAccepted))
	mock.ExpectQuery("SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id").
		WithArgs(pending.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(pending.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id FROM accounts WHERE id = \\$1 AND deleted_at IS NULL FOR UPDATE").
		WithArgs(pending.AccountID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(pending.AccountID))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM organization_execution_attempts WHERE execution_ref_id = \\$1").
		WithArgs(int64(101)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery("INSERT INTO organization_execution_attempts").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(102)))
	mock.ExpectExec("UPDATE organization_executions SET status = \\$1").
		WithArgs(service.OrganizationExecutionStatePendingReconciliation, int64(101), service.OrganizationExecutionStateAccepted, service.OrganizationExecutionStateDispatched, service.OrganizationExecutionStateFinalUsageObserved).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repository.RecordNonBillablePendingAttempt(context.Background(), pending))

	// Recovery accepts only the stored attempt identity. The control-plane
	// command supplies evidence and usage, while route/account/provider identity
	// is derived from the locked execution and attempt rows.
	mock.ExpectBegin()
	expectNonBillableRecoveryAttempt(mock, execution, pending, service.OrganizationExecutionStatePendingReconciliation)
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(pending.AttemptID, service.OrganizationAttemptEvidenceProvenanceProviderQuery, evidenceHash).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(pending.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id FROM accounts WHERE id = \\$1 AND deleted_at IS NULL FOR UPDATE").
		WithArgs(pending.AccountID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(pending.AccountID))
	mock.ExpectQuery("SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id").
		WithArgs(pending.AttemptID).WillReturnRows(nonBillableAttemptRows(pending, int64(102), int64(101)))
	mock.ExpectQuery("INSERT INTO organization_attempt_evidences").
		WithArgs(pending.AttemptID, int64(102), execution.ExecutionID, int64(101), execution.Attribution.OrganizationID, pending.AccountID, pending.AccountID,
			pending.RouteSnapshotJSON, pending.RouteSnapshotHash, pending.PricingSnapshotJSON, pending.PricingSnapshotHash,
			pending.ProviderCorrelationID, pending.ProviderRequestID, service.OrganizationAttemptEvidenceProvenanceProviderQuery,
			recovery.TerminalOutcome, evidenceHash, evidenceJSON, evidenceHash,
			recovery.InputTokens, recovery.OutputTokens, recovery.CacheCreationTokens, recovery.CacheReadTokens, "0", observedAt, ingestedAt).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(103)))
	mock.ExpectCommit()

	recorded, err := repository.RecordNonBillableRecoveryEvidence(context.Background(), recovery)
	require.NoError(t, err)
	require.Equal(t, int64(103), recorded.AttemptEvidenceID)
	require.Equal(t, pending.AttemptID, recorded.AttemptID)
	require.False(t, recorded.Replayed)

	// An identical provider-query retry reuses the evidence row and writes
	// neither a second attempt nor a second evidence record.
	mock.ExpectBegin()
	expectNonBillableRecoveryAttempt(mock, execution, pending, service.OrganizationExecutionStatePendingReconciliation)
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("FROM organization_attempt_evidences AS evidence").
		WithArgs(pending.AttemptID, service.OrganizationAttemptEvidenceProvenanceProviderQuery, evidenceHash).
		WillReturnRows(nonBillableRecoveryEvidenceReplayRows(execution, pending, evidence, int64(101), int64(103)))
	mock.ExpectCommit()

	replayedEvidence, err := repository.RecordNonBillableRecoveryEvidence(context.Background(), recovery)
	require.NoError(t, err)
	require.True(t, replayedEvidence.Replayed)
	require.Equal(t, recorded.AttemptEvidenceID, replayedEvidence.AttemptEvidenceID)

	// Reconciliation consumes that one provider-query evidence row. Final cost
	// columns are SQL constants (zero), and no reservation/consumption posting is
	// created for a non-billable execution.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStatePendingReconciliation))
	expectNonBillableAllocationLock(mock, execution.Attribution.AllocationID, execution.Attribution.AllocationVersionSnapshot)
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("SELECT execution_ref_id, canonical_payload_hash").
		WithArgs(execution.Attribution.OrganizationID, reconcile.Actor.AuthorizationScope, reconcile.IdempotencyKeyHash).
		WillReturnRows(sqlmock.NewRows([]string{"execution_ref_id"}))
	mock.ExpectQuery("SELECT id, execution_ref_id, execution_id_snapshot, organization_id").
		WithArgs(int64(101)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)").
		WithArgs(int64(101)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT evidence.id, attempt.attempt_id, attempt.id, attempt.attempt_sequence").
		WithArgs(int64(101)).WillReturnRows(nonBillableReconciliationCandidateRows(execution, pending, evidence, int64(102), int64(103)))
	expectNonBillableAllocationLock(mock, execution.Attribution.AllocationID, execution.Attribution.AllocationVersionSnapshot)
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStatePendingReconciliation))
	mock.ExpectQuery("SELECT id, execution_ref_id, execution_id_snapshot, organization_id").
		WithArgs(int64(101)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT evidence.id, attempt.attempt_id, attempt.id, evidence.execution_ref_id").
		WithArgs(int64(103), pending.AttemptID).WillReturnRows(nonBillableFinalAttemptEvidenceRows(execution, pending, evidence, int64(102), int64(103)))
	mock.ExpectQuery("SELECT COUNT\\(DISTINCT attempt.id\\), COUNT\\(evidence.id\\)").
		WithArgs(int64(101)).WillReturnRows(sqlmock.NewRows([]string{"attempt_count", "evidence_count"}).AddRow(1, 1))
	mock.ExpectQuery("SELECT id FROM users WHERE id = \\$1 AND deleted_at IS NULL").
		WithArgs(execution.Attribution.UserID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(execution.Attribution.UserID))
	mock.ExpectQuery("SELECT id FROM api_keys WHERE id = \\$1 AND deleted_at IS NULL").
		WithArgs(execution.Attribution.APIKeyID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(execution.Attribution.APIKeyID))
	mock.ExpectQuery("SELECT id FROM accounts WHERE id = \\$1 AND deleted_at IS NULL FOR UPDATE").
		WithArgs(pending.AccountID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(pending.AccountID))
	mock.ExpectQuery("INSERT INTO organization_final_execution_evidences").
		WithArgs(execution.ExecutionID, int64(101), execution.Attribution.OrganizationID,
			execution.Attribution.AllocationID, execution.Attribution.AllocationID,
			execution.Attribution.UserID, execution.Attribution.UserID,
			execution.Attribution.APIKeyID, execution.Attribution.APIKeyID,
			pending.AttemptID, int64(102), int64(103), int64(103),
			pending.AccountID, pending.AccountID,
			execution.EndpointFamily, execution.RequestShape, nil,
			recovery.InputTokens, recovery.OutputTokens, recovery.CacheCreationTokens, recovery.CacheReadTokens,
			recovery.TerminalOutcome, false, true, nil, observedAt, reconciledAt).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(104)))
	mock.ExpectExec("UPDATE organization_executions SET status = \\$1").
		WithArgs(service.OrganizationExecutionStateReleased, observedAt, int64(101), service.OrganizationExecutionStateAccepted, service.OrganizationExecutionStateFinalUsageObserved, service.OrganizationExecutionStatePendingReconciliation).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO organization_execution_outboxes").WillReturnResult(sqlmock.NewResult(105, 1))
	mock.ExpectQuery("SELECT id FROM users WHERE id = \\$1 AND deleted_at IS NULL").
		WithArgs(reconcile.Actor.UserID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(reconcile.Actor.UserID))
	mock.ExpectExec("INSERT INTO organization_execution_reconciliation_audits").WillReturnResult(sqlmock.NewResult(106, 1))
	mock.ExpectCommit()

	final, err := repository.ReconcilePendingExecution(context.Background(), reconcile)
	require.NoError(t, err)
	require.Equal(t, int64(104), final.FinalEvidenceID)
	require.Equal(t, service.OrganizationReservationStateReleased, final.ReservationState)
	require.Equal(t, "0", final.RawActualCost)
	require.Equal(t, "0.00000000", final.OrganizationSettledCost)
	require.Nil(t, final.ConsumptionPostingID)
	require.NotNil(t, final.FinalAttemptID)
	require.NotNil(t, final.FinalAttemptEvidenceID)
	require.Equal(t, pending.AttemptID, *final.FinalAttemptID)
	require.Equal(t, recorded.AttemptEvidenceID, *final.FinalAttemptEvidenceID)
	require.NotNil(t, final.ReconciledAt)
	require.False(t, final.Replayed)

	// The same reconciliation operation is an exact replay: no second final
	// evidence, outbox, audit, or posting is emitted.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStateReleased))
	expectNonBillableAllocationLock(mock, execution.Attribution.AllocationID, execution.Attribution.AllocationVersionSnapshot)
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), service.OrganizationExecutionStateReleased))
	mock.ExpectQuery("SELECT execution_ref_id, canonical_payload_hash").
		WithArgs(execution.Attribution.OrganizationID, reconcile.Actor.AuthorizationScope, reconcile.IdempotencyKeyHash).
		WillReturnRows(sqlmock.NewRows([]string{"execution_ref_id", "canonical_payload_hash"}).AddRow(int64(101), reconcile.CanonicalPayloadHash))
	mock.ExpectQuery("SELECT id, execution_ref_id, execution_id_snapshot, organization_id").
		WithArgs(int64(101)).WillReturnRows(nonBillableReconciledFinalRows(execution, pending, evidence, int64(101), int64(102), int64(103), int64(104), observedAt, reconciledAt))
	mock.ExpectCommit()
	mock.ExpectClose()

	replayedFinal, err := repository.ReconcilePendingExecution(context.Background(), reconcile)
	require.NoError(t, err)
	require.True(t, replayedFinal.Replayed)
	require.Equal(t, final.FinalEvidenceID, replayedFinal.FinalEvidenceID)
	require.Equal(t, final.RawActualCost, replayedFinal.RawActualCost)
	require.Equal(t, final.OrganizationSettledCost, replayedFinal.OrganizationSettledCost)
	require.Nil(t, replayedFinal.ConsumptionPostingID)
	require.NoError(t, db.Close())
	require.NoError(t, mock.ExpectationsWereMet())
}

func expectNonBillableRecoveryAttempt(mock sqlmock.Sqlmock, execution service.OrganizationNonBillableExecutionCommand, pending service.OrganizationNonBillablePendingAttemptCommand, state string) {
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(execution.ExecutionID).WillReturnRows(nonBillableExecutionRows(execution, int64(101), state))
	mock.ExpectQuery("SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id").
		WithArgs(pending.AttemptID).WillReturnRows(nonBillableAttemptRows(pending, int64(102), int64(101)))
}

func nonBillableRecoveryEvidenceReplayRows(execution service.OrganizationNonBillableExecutionCommand, pending service.OrganizationNonBillablePendingAttemptCommand, evidence service.OrganizationNonBillableAttemptEvidenceCommand, executionRefID, evidenceID int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "attempt_id", "execution_id", "execution_ref_id", "organization_id", "attempt_sequence",
		"account_id", "route_snapshot_hash", "pricing_snapshot_hash", "provider_request_id", "provider_correlation_id",
		"evidence_source", "terminal_outcome", "provenance_hash", "evidence_hash", "input_tokens", "output_tokens",
		"cache_creation_tokens", "cache_read_tokens", "raw_actual_cost", "provider_observed_at", "ingested_at",
	}).AddRow(evidenceID, pending.AttemptID, execution.ExecutionID, executionRefID, execution.Attribution.OrganizationID, pending.AttemptSequence,
		pending.AccountID, pending.RouteSnapshotHash, pending.PricingSnapshotHash, pending.ProviderRequestID, pending.ProviderCorrelationID,
		evidence.Provenance, evidence.TerminalOutcome, evidence.ProvenanceHash, evidence.EvidenceHash, evidence.InputTokens, evidence.OutputTokens,
		evidence.CacheCreationTokens, evidence.CacheReadTokens, evidence.RawActualCost, evidence.ProviderObservedAt, evidence.IngestedAt)
}
