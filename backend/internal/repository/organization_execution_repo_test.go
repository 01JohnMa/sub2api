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
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestOrganizationExecutionReserveWritesCASReservationAndBalancedPostings(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repository := NewOrganizationExecutionRepository(client)
	command := repositoryReservationCommand(time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status, balance_version FROM organizations WHERE id = $1 FOR SHARE")).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "balance_version"}).AddRow(service.OrganizationStatusActive, int64(91)))
	mock.ExpectQuery("SELECT id, organization_id, deleted_at FROM users WHERE id = \\$1 FOR UPDATE").
		WithArgs(int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "deleted_at"}).AddRow(int64(2), int64(1), nil))
	mock.ExpectQuery("SELECT id, user_id, organization_id, status, deleted_at, expires_at FROM api_keys WHERE id = \\$1 FOR UPDATE").
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "organization_id", "status", "deleted_at", "expires_at"}).AddRow(int64(3), int64(2), int64(1), service.StatusAPIKeyActive, nil, nil))
	mock.ExpectQuery("SELECT id, organization_id, user_id_snapshot, user_ref_id, status, available, reserved, version").
		WithArgs(int64(4)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "organization_id", "user_id_snapshot", "user_ref_id", "status", "available", "reserved", "version"}).AddRow(int64(4), int64(1), int64(2), int64(2), domain.OrganizationAllocationStatusActive, "10.00000000", "0.00000000", int64(9)))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(command.ExecutionID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("INSERT INTO organization_executions").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectQuery("SELECT id, execution_id, correlation_id, organization_id").
		WithArgs(command.ExecutionID).
		WillReturnRows(executionMockRows(command, int64(11), int64(4), int64(2), int64(3)))
	mock.ExpectQuery("INSERT INTO organization_reservations").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(12)))
	mock.ExpectQuery("SELECT id, execution_ref_id, execution_id_snapshot, organization_id").
		WithArgs(int64(12)).
		WillReturnRows(reservationMockRows(command, int64(12), int64(11)))
	mock.ExpectQuery("INSERT INTO organization_ledger_operations").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(13)))
	mock.ExpectExec("INSERT INTO organization_ledger_postings").WillReturnResult(sqlmock.NewResult(21, 1))
	mock.ExpectExec("INSERT INTO organization_ledger_postings").WillReturnResult(sqlmock.NewResult(22, 1))
	mock.ExpectExec("UPDATE organization_allocations").
		WithArgs("9.00000000", "1.00000000", int64(10), int64(4), int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE organization_executions").
		WithArgs(service.OrganizationExecutionStateReserved, int64(11), service.OrganizationExecutionStateAccepted).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectClose()

	result, err := repository.Reserve(context.Background(), command)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationExecutionReservationDispositionReserved, result.Disposition)
	require.Equal(t, int64(12), result.ReservationID)
	require.Equal(t, "1.00000000", result.ReservedAmount)
}

func TestSettleOrganizationExecutionCostUsesHalfUpAndReservationCap(t *testing.T) {
	ceiling := decimal.RequireFromString("1.00000000")
	require.Equal(t, "0.12345679", fixedExecutionMoney(settleOrganizationExecutionCost(decimal.RequireFromString("0.123456785"), ceiling)))
	require.Equal(t, "1.00000000", fixedExecutionMoney(settleOrganizationExecutionCost(decimal.RequireFromString("4.250000001"), ceiling)))
}

func TestSameOrganizationExecutionCommandRejectsFingerprintAndSnapshotConflicts(t *testing.T) {
	command := repositoryReservationCommand(time.Now().UTC())
	record := &organizationExecutionRecord{
		ExecutionID:          command.ExecutionID,
		CorrelationID:        command.CorrelationID,
		OrganizationID:       command.Attribution.OrganizationID,
		AllocationIDSnapshot: nullableExecutionInt64(command.Attribution.AllocationID),
		UserIDSnapshot:       command.Attribution.UserID,
		APIKeyIDSnapshot:     command.Attribution.APIKeyID,
		EndpointFamily:       command.EndpointFamily,
		RequestShape:         command.RequestShape,
		BillingFingerprint:   command.BillingFingerprint,
		CandidateSnapshot:    command.CandidateSnapshotJSON,
		CandidateSetHash:     command.CandidateSetHash,
		PricingSnapshot:      command.PricingSnapshotJSON,
		PricingSnapshotHash:  command.PricingSnapshotHash,
		CandidateCeiling:     command.CandidateRouteMaximumCost,
	}
	require.True(t, sameOrganizationExecutionCommand(record, command))
	command.BillingFingerprint = strings.Repeat("b", 64)
	require.False(t, sameOrganizationExecutionCommand(record, command))
	require.NotEqual(t, executionLedgerHash("execution-a", "reservation"), executionLedgerHash("execution-b", "reservation"))
}

func TestOrganizationAttemptEvidenceReplayRequiresTheCompleteImmutableEvidence(t *testing.T) {
	observedAt := time.Date(2026, time.July, 28, 12, 0, 0, 123456000, time.UTC)
	command := service.OrganizationAttemptEvidenceCommand{
		ExecutionID:           uuid.NewString(),
		AttemptID:             uuid.NewString(),
		OrganizationID:        1,
		AttemptSequence:       1,
		AccountID:             9,
		RouteSnapshotHash:     strings.Repeat("a", 64),
		PricingSnapshotHash:   strings.Repeat("b", 64),
		ProviderRequestID:     "provider-request",
		ProviderCorrelationID: uuid.NewString(),
		Provenance:            service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
		TerminalOutcome:       service.OrganizationExecutionTerminalOutcomeSuccess,
		ProvenanceHash:        strings.Repeat("c", 64),
		EvidenceHash:          strings.Repeat("c", 64),
		InputTokens:           1,
		OutputTokens:          2,
		CacheCreationTokens:   3,
		CacheReadTokens:       4,
		RawActualCost:         "0.12500000",
		ProviderObservedAt:    observedAt,
		IngestedAt:            observedAt,
	}
	record := &organizationAttemptEvidenceReplayRecord{
		AttemptID:           command.AttemptID,
		ExecutionID:         command.ExecutionID,
		OrganizationID:      command.OrganizationID,
		AttemptSequence:     command.AttemptSequence,
		AccountID:           sql.NullInt64{Int64: command.AccountID, Valid: true},
		RouteSnapshotHash:   command.RouteSnapshotHash,
		PricingSnapshotHash: command.PricingSnapshotHash,
		ProviderRequestID:   command.ProviderRequestID,
		ProviderCorrelation: command.ProviderCorrelationID,
		EvidenceSource:      command.Provenance,
		TerminalOutcome:     command.TerminalOutcome,
		ProvenanceHash:      command.ProvenanceHash,
		EvidenceHash:        command.EvidenceHash,
		InputTokens:         command.InputTokens,
		OutputTokens:        command.OutputTokens,
		CacheCreationTokens: command.CacheCreationTokens,
		CacheReadTokens:     command.CacheReadTokens,
		RawActualCost:       command.RawActualCost,
		ProviderObservedAt:  observedAt,
		IngestedAt:          observedAt,
	}
	require.True(t, sameAttemptEvidenceCommand(record, command))
	record.ProviderRequestID = "different-request"
	require.False(t, sameAttemptEvidenceCommand(record, command))
}

func TestZeroCostTerminalReplayRequiresTheSameProof(t *testing.T) {
	final := &organizationFinalEvidenceRecord{
		TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError,
		ZeroCostProof:   sql.NullString{String: `{"reason":"known_pre_dispatch_failure"}`, Valid: true},
	}
	command := service.OrganizationTerminalizationCommand{
		TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError,
		ZeroProof:       "known_pre_dispatch_failure",
	}
	require.True(t, sameTerminalization(final, command))
	command.ZeroProof = "different_reason"
	require.False(t, sameTerminalization(final, command))
}

func TestCurrentExecutionAccountRefUsesSQLNullForSoftDeletedAccount(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	mock.ExpectQuery("SELECT id FROM accounts WHERE id = \\$1 AND deleted_at IS NULL").
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectClose()

	ref, err := currentExecutionAccountRef(context.Background(), client, 9)
	require.NoError(t, err)
	require.Nil(t, ref)
}

func TestOrganizationAttemptEvidenceSelectionPrefersAdapterAndRejectsConflictingUsage(t *testing.T) {
	base := organizationAttemptEvidenceCandidate{
		ID:              1,
		AttemptID:       uuid.NewString(),
		AttemptRefID:    10,
		AttemptSequence: 1,
		EvidenceSource:  service.OrganizationAttemptEvidenceProvenanceProviderQuery,
		TerminalOutcome: service.OrganizationExecutionTerminalOutcomeSuccess,
		RawActualCost:   "1.25000000",
		InputTokens:     10,
		OutputTokens:    5,
	}
	adapter := base
	adapter.ID = 2
	adapter.EvidenceSource = service.OrganizationAttemptEvidenceProvenanceAdapterDirect

	selection := selectOrganizationAttemptEvidence([]organizationAttemptEvidenceCandidate{base, adapter})[1]
	require.NotNil(t, selection.Selected)
	require.Equal(t, adapter.ID, selection.Selected.ID)
	require.Empty(t, selection.Conflicts)

	conflict := base
	conflict.ID = 3
	conflict.RawActualCost = "1.50000000"
	selection = selectOrganizationAttemptEvidence([]organizationAttemptEvidenceCandidate{adapter, conflict})[1]
	require.Equal(t, adapter.ID, selection.Selected.ID)
	require.Equal(t, []int64{conflict.ID}, selection.Conflicts)
}

func TestOrganizationAttemptPlatformOverageKeepsRawAccountCostAndCapsOnlyMemberSettlement(t *testing.T) {
	raw := decimal.RequireFromString("4.250000001")
	ceiling := decimal.RequireFromString("1.00000000")
	require.Equal(t, "1.00000000", fixedExecutionMoney(settleOrganizationExecutionCost(raw, ceiling)))
	require.Equal(t, "3.250000001", raw.Sub(ceiling).String())
}

func repositoryReservationCommand(acceptedAt time.Time) service.OrganizationExecutionReservationCommand {
	candidateJSON := "{}"
	hash := repositoryExecutionHash(candidateJSON)
	return service.OrganizationExecutionReservationCommand{
		ExecutionID:               uuid.NewString(),
		CorrelationID:             uuid.NewString(),
		EndpointFamily:            "chat_completions",
		RequestShape:              "default",
		CandidateSnapshotJSON:     candidateJSON,
		CandidateSetHash:          hash,
		PricingSnapshotJSON:       candidateJSON,
		PricingSnapshotHash:       hash,
		CandidateRouteMaximumCost: "1.00000000",
		BillingFingerprint:        strings.Repeat("a", 64),
		AcceptedAt:                acceptedAt,
		Attribution: service.OrganizationExecutionTrustedAttribution{
			OrganizationID:                     1,
			OrganizationStatus:                 service.OrganizationStatusActive,
			OrganizationBalanceVersionSnapshot: 7,
			UserID:                             2,
			UserOrganizationID:                 1,
			APIKeyID:                           3,
			APIKeyUserID:                       2,
			APIKeyOrganizationID:               1,
			AllocationID:                       4,
			AllocationUserID:                   2,
			AllocationOrganizationID:           1,
			AllocationStatus:                   domain.OrganizationAllocationStatusActive,
			AllocationVersionSnapshot:          9,
		},
	}
}

func executionMockRows(command service.OrganizationExecutionReservationCommand, executionID, allocationID, userID, keyID int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "execution_id", "correlation_id", "organization_id", "allocation_id_snapshot", "allocation_ref_id", "user_id_snapshot", "user_ref_id", "api_key_id_snapshot", "api_key_ref_id", "endpoint_family", "request_shape", "billing_fingerprint", "candidate_snapshot", "candidate_set_hash", "pricing_snapshot", "pricing_snapshot_hash", "candidate_ceiling", "status", "accepted_at", "terminalized_at",
	}).AddRow(executionID, command.ExecutionID, command.CorrelationID, command.Attribution.OrganizationID, allocationID, allocationID, userID, userID, keyID, keyID, command.EndpointFamily, command.RequestShape, command.BillingFingerprint, command.CandidateSnapshotJSON, command.CandidateSetHash, command.PricingSnapshotJSON, command.PricingSnapshotHash, command.CandidateRouteMaximumCost, service.OrganizationExecutionStateAccepted, command.AcceptedAt, nil)
}

func reservationMockRows(command service.OrganizationExecutionReservationCommand, reservationID, executionID int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "execution_ref_id", "execution_id_snapshot", "organization_id", "allocation_id_snapshot", "allocation_ref_id", "allocation_version_snapshot", "organization_balance_version_snapshot", "pricing_snapshot_hash", "reserved_amount", "settled_amount", "released_amount", "status",
	}).AddRow(reservationID, executionID, command.ExecutionID, command.Attribution.OrganizationID, command.Attribution.AllocationID, command.Attribution.AllocationID, command.Attribution.AllocationVersionSnapshot, command.Attribution.OrganizationBalanceVersionSnapshot, command.PricingSnapshotHash, command.CandidateRouteMaximumCost, "0.00000000", "0.00000000", service.OrganizationReservationStateReserved)
}

func nullableExecutionInt64(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: true}
}

func repositoryExecutionHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
