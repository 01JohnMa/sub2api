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
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestOrganizationExecutionPostgresReserveMovesOnlyMemberAllocationBuckets(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createOrganizationAllocationIntegrationFixture(t, ctx, tx.Client(), fmt.Sprintf("execution-reserve-%d", time.Now().UnixNano()), "0.00000000", "2.00000000")
	key, err := tx.Client().APIKey.Create().
		SetUserID(fixture.member.ID).
		SetOrganizationID(fixture.organization.ID).
		SetKey(fmt.Sprintf("execution-key-%d", time.Now().UnixNano())).
		SetName("organization execution test key").
		SetStatus(service.StatusAPIKeyActive).
		Save(ctx)
	require.NoError(t, err)
	allocation, err := tx.Client().OrganizationAllocation.Query().
		Where(organizationallocation.UserIDSnapshotEQ(fixture.member.ID)).
		Only(ctx)
	require.NoError(t, err)

	command := repositoryReservationCommand(time.Now().UTC())
	command.Attribution.OrganizationID = fixture.organization.ID
	command.Attribution.OrganizationBalanceVersionSnapshot = 0
	command.Attribution.UserID = fixture.member.ID
	command.Attribution.UserOrganizationID = fixture.organization.ID
	command.Attribution.APIKeyID = key.ID
	command.Attribution.APIKeyUserID = fixture.member.ID
	command.Attribution.APIKeyOrganizationID = fixture.organization.ID
	command.Attribution.AllocationID = allocation.ID
	command.Attribution.AllocationUserID = fixture.member.ID
	command.Attribution.AllocationOrganizationID = fixture.organization.ID
	command.Attribution.AllocationStatus = domain.OrganizationAllocationStatusActive
	command.Attribution.AllocationVersionSnapshot = allocation.Version

	result, err := NewOrganizationExecutionRepository(tx.Client()).Reserve(ctx, command)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationExecutionReservationDispositionReserved, result.Disposition)
	require.Equal(t, "1.00000000", result.ReservedAmount)

	updated, err := tx.Client().OrganizationAllocation.Get(ctx, allocation.ID)
	require.NoError(t, err)
	require.Equal(t, "1.00000000", updated.Available.StringFixed(domain.OrganizationMoneyScale))
	require.Equal(t, "1.00000000", updated.Reserved.StringFixed(domain.OrganizationMoneyScale))
	require.Equal(t, allocation.Version+1, updated.Version)
	var reservationCount, postingCount int
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM organization_reservations WHERE execution_id_snapshot = $1`, []any{command.ExecutionID}, &reservationCount))
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM organization_ledger_postings WHERE execution_id_snapshot = $1`, []any{command.ExecutionID}, &postingCount))
	require.Equal(t, 1, reservationCount)
	require.Equal(t, 2, postingCount)
}

func TestOrganizationExecutionPostgresTerminalizesFromImmutableAttemptEvidence(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createOrganizationAllocationIntegrationFixture(t, ctx, tx.Client(), fmt.Sprintf("execution-terminal-%d", time.Now().UnixNano()), "0.00000000", "2.00000000")
	key, err := tx.Client().APIKey.Create().
		SetUserID(fixture.member.ID).
		SetOrganizationID(fixture.organization.ID).
		SetKey(fmt.Sprintf("execution-terminal-key-%d", time.Now().UnixNano())).
		SetName("organization terminal test key").
		SetStatus(service.StatusAPIKeyActive).
		Save(ctx)
	require.NoError(t, err)
	allocation, err := tx.Client().OrganizationAllocation.Query().
		Where(organizationallocation.UserIDSnapshotEQ(fixture.member.ID)).
		Only(ctx)
	require.NoError(t, err)
	account := mustCreateAccount(t, tx.Client(), &service.Account{Name: "organization execution account"})

	command := repositoryReservationCommand(time.Now().UTC())
	command.Attribution.OrganizationID = fixture.organization.ID
	command.Attribution.OrganizationBalanceVersionSnapshot = 0
	command.Attribution.UserID = fixture.member.ID
	command.Attribution.UserOrganizationID = fixture.organization.ID
	command.Attribution.APIKeyID = key.ID
	command.Attribution.APIKeyUserID = fixture.member.ID
	command.Attribution.APIKeyOrganizationID = fixture.organization.ID
	command.Attribution.AllocationID = allocation.ID
	command.Attribution.AllocationUserID = fixture.member.ID
	command.Attribution.AllocationOrganizationID = fixture.organization.ID
	command.Attribution.AllocationStatus = domain.OrganizationAllocationStatusActive
	command.Attribution.AllocationVersionSnapshot = allocation.Version

	repository := NewOrganizationExecutionRepository(tx.Client())
	reserved, err := repository.Reserve(ctx, command)
	require.NoError(t, err)
	attemptID := uuid.NewString()
	providerCorrelationID := uuid.NewString()
	evidenceHash := repositoryExecutionHash(`{"usage":"final"}`)
	attempt, err := repository.RecordAttemptEvidence(ctx, service.OrganizationAttemptEvidenceCommand{
		ExecutionID:           command.ExecutionID,
		AttemptID:             attemptID,
		CorrelationID:         command.CorrelationID,
		ReservationID:         reserved.ReservationID,
		OrganizationID:        fixture.organization.ID,
		UserID:                fixture.member.ID,
		APIKeyID:              key.ID,
		AllocationID:          allocation.ID,
		AttemptSequence:       1,
		AccountID:             account.ID,
		RouteSnapshotJSON:     `{}`,
		RouteSnapshotHash:     repositoryExecutionHash(`{}`),
		PricingSnapshotJSON:   command.PricingSnapshotJSON,
		PricingSnapshotHash:   command.PricingSnapshotHash,
		ProviderRequestID:     "provider-request-id",
		ProviderCorrelationID: providerCorrelationID,
		Provenance:            service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
		TerminalOutcome:       service.OrganizationExecutionTerminalOutcomeSuccess,
		ProvenanceHash:        evidenceHash,
		EvidenceJSON:          `{"usage":"final"}`,
		EvidenceHash:          evidenceHash,
		RawActualCost:         "0.100000001",
		ProviderObservedAt:    time.Now().UTC(),
		IngestedAt:            time.Now().UTC(),
	})
	require.NoError(t, err)
	final, err := repository.Terminalize(ctx, service.OrganizationTerminalizationCommand{
		ExecutionID:            command.ExecutionID,
		CorrelationID:          command.CorrelationID,
		OrganizationID:         fixture.organization.ID,
		UserID:                 fixture.member.ID,
		APIKeyID:               key.ID,
		AllocationID:           allocation.ID,
		ReservationID:          reserved.ReservationID,
		FinalAttemptID:         &attemptID,
		FinalAttemptEvidenceID: &attempt.AttemptEvidenceID,
		TerminalOutcome:        service.OrganizationExecutionTerminalOutcomeSuccess,
		OccurredAt:             time.Now().UTC(),
	})
	require.NoError(t, err)
	require.Equal(t, service.OrganizationReservationStateSettled, final.ReservationState)
	require.Equal(t, "0.10000000", final.OrganizationSettledCost)
	require.NotNil(t, final.ConsumptionPostingID)

	updated, err := tx.Client().OrganizationAllocation.Get(ctx, allocation.ID)
	require.NoError(t, err)
	require.Equal(t, "1.90000000", updated.Available.StringFixed(domain.OrganizationMoneyScale))
	require.Equal(t, "0.00000000", updated.Reserved.StringFixed(domain.OrganizationMoneyScale))
	var evidenceCount, consumptionCount int
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM organization_final_execution_evidences WHERE execution_id_snapshot = $1`, []any{command.ExecutionID}, &evidenceCount))
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM organization_ledger_postings WHERE final_evidence_id_snapshot = $1`, []any{final.FinalEvidenceID}, &consumptionCount))
	require.Equal(t, 1, evidenceCount)
	require.Equal(t, 1, consumptionCount)
}

func TestOrganizationExecutionPostgresRejectsJSONNullZeroProof(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, tx.Client(), "execution-null-proof")

	var executionRefID int64
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT id FROM organization_executions WHERE execution_id = $1`, []any{fixture.command.ExecutionID}, &executionRefID))
	_, err := tx.Client().ExecContext(ctx, `
		INSERT INTO organization_final_execution_evidences (
			execution_id_snapshot, execution_ref_id, organization_id,
			allocation_id_snapshot, allocation_ref_id,
			user_id_snapshot, user_ref_id, api_key_id_snapshot, api_key_ref_id,
			endpoint_family, request_shape, raw_actual_cost,
			organization_settled_cost, terminal_outcome, is_business_limited,
			zero_cost_proof, occurred_at
		) VALUES ($1, $2, $3, $4, $4, $5, $5, $6, $6, $7, $8, 0, 0, $9, FALSE, 'null'::jsonb, $10)
	`, fixture.command.ExecutionID, executionRefID, fixture.organization.ID, fixture.allocation.ID,
		fixture.member.ID, fixture.key.ID, fixture.command.EndpointFamily, fixture.command.RequestShape,
		service.OrganizationExecutionTerminalOutcomeError, time.Now().UTC())
	require.ErrorContains(t, err, "organization_final_execution_evidences_zero_proof_check")
}

func TestOrganizationExecutionPostgresRejectsFinalEvidenceBeforeReservationTerminalState(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, tx.Client(), "execution-terminal-state")

	var executionRefID int64
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT id FROM organization_executions WHERE execution_id = $1`, []any{fixture.command.ExecutionID}, &executionRefID))
	_, err := tx.Client().ExecContext(ctx, `
		INSERT INTO organization_final_execution_evidences (
			execution_id_snapshot, execution_ref_id, organization_id,
			allocation_id_snapshot, allocation_ref_id,
			user_id_snapshot, user_ref_id, api_key_id_snapshot, api_key_ref_id,
			endpoint_family, request_shape, raw_actual_cost,
			organization_settled_cost, terminal_outcome, is_business_limited,
			zero_cost_proof, occurred_at
		) VALUES ($1, $2, $3, $4, $4, $5, $5, $6, $6, $7, $8, 0, 0, $9, FALSE, $10::jsonb, $11)
	`, fixture.command.ExecutionID, executionRefID, fixture.organization.ID, fixture.allocation.ID,
		fixture.member.ID, fixture.key.ID, fixture.command.EndpointFamily, fixture.command.RequestShape,
		service.OrganizationExecutionTerminalOutcomeError, `{"reason":"known_zero"}`, time.Now().UTC())
	require.NoError(t, err)
	_, err = tx.Client().ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	require.ErrorContains(t, err, "organization zero-cost final evidence is inconsistent with terminal reservation state")
}

func TestOrganizationExecutionPostgresRejectsFinalEvidenceThatChangesAttemptCost(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, tx.Client(), "execution-evidence-cost")
	account := mustCreateAccount(t, tx.Client(), &service.Account{Name: "organization evidence derivation account"})
	attemptID := uuid.NewString()
	providerCorrelationID := uuid.NewString()
	evidenceHash := repositoryExecutionHash(`{"usage":"final"}`)
	attempt, err := fixture.repository.RecordAttemptEvidence(ctx, service.OrganizationAttemptEvidenceCommand{
		ExecutionID:           fixture.command.ExecutionID,
		AttemptID:             attemptID,
		CorrelationID:         fixture.command.CorrelationID,
		ReservationID:         fixture.reserved.ReservationID,
		OrganizationID:        fixture.organization.ID,
		UserID:                fixture.member.ID,
		APIKeyID:              fixture.key.ID,
		AllocationID:          fixture.allocation.ID,
		AttemptSequence:       1,
		AccountID:             account.ID,
		RouteSnapshotJSON:     `{}`,
		RouteSnapshotHash:     repositoryExecutionHash(`{}`),
		PricingSnapshotJSON:   fixture.command.PricingSnapshotJSON,
		PricingSnapshotHash:   fixture.command.PricingSnapshotHash,
		ProviderRequestID:     "provider-request-id",
		ProviderCorrelationID: providerCorrelationID,
		Provenance:            service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
		TerminalOutcome:       service.OrganizationExecutionTerminalOutcomeSuccess,
		ProvenanceHash:        evidenceHash,
		EvidenceJSON:          `{"usage":"final"}`,
		EvidenceHash:          evidenceHash,
		RawActualCost:         "0.100000001",
		InputTokens:           1,
		OutputTokens:          2,
		ProviderObservedAt:    time.Now().UTC(),
		IngestedAt:            time.Now().UTC(),
	})
	require.NoError(t, err)

	var executionRefID, attemptRefID int64
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT id FROM organization_executions WHERE execution_id = $1`, []any{fixture.command.ExecutionID}, &executionRefID))
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT id FROM organization_execution_attempts WHERE attempt_id = $1`, []any{attemptID}, &attemptRefID))
	_, err = tx.Client().ExecContext(ctx, `
		INSERT INTO organization_final_execution_evidences (
			execution_id_snapshot, execution_ref_id, organization_id,
			allocation_id_snapshot, allocation_ref_id,
			user_id_snapshot, user_ref_id, api_key_id_snapshot, api_key_ref_id,
			final_attempt_id_snapshot, final_attempt_ref_id,
			final_attempt_evidence_id_snapshot, final_attempt_evidence_ref_id,
			account_id_snapshot, account_ref_id, endpoint_family, request_shape,
			input_tokens, output_tokens, raw_actual_cost, organization_settled_cost,
			terminal_outcome, is_business_limited, occurred_at
		) VALUES ($1, $2, $3, $4, $4, $5, $5, $6, $6, $7, $8, $9, $9, $10, $10, $11, $12, 1, 2, 0.2, 0.2, $13, FALSE, $14)
	`, fixture.command.ExecutionID, executionRefID, fixture.organization.ID, fixture.allocation.ID,
		fixture.member.ID, fixture.key.ID, attemptID, attemptRefID, attempt.AttemptEvidenceID,
		account.ID, fixture.command.EndpointFamily, fixture.command.RequestShape,
		service.OrganizationExecutionTerminalOutcomeSuccess, time.Now().UTC())
	require.ErrorContains(t, err, "organization final evidence attempt evidence is inconsistent")
}

func TestOrganizationExecutionPostgresServiceReturnsInsufficientAllocationProof(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createOrganizationAllocationIntegrationFixture(t, ctx, tx.Client(), fmt.Sprintf("execution-insufficient-%d", time.Now().UnixNano()), "0.00000000", "0.00000000")
	key, err := tx.Client().APIKey.Create().
		SetUserID(fixture.member.ID).
		SetOrganizationID(fixture.organization.ID).
		SetKey(fmt.Sprintf("execution-insufficient-key-%d", time.Now().UnixNano())).
		SetName("organization insufficient execution test key").
		SetStatus(service.StatusAPIKeyActive).
		Save(ctx)
	require.NoError(t, err)
	allocation, err := tx.Client().OrganizationAllocation.Query().
		Where(organizationallocation.UserIDSnapshotEQ(fixture.member.ID)).
		Only(ctx)
	require.NoError(t, err)
	command := repositoryReservationCommand(time.Now().UTC())
	command.Attribution.OrganizationID = fixture.organization.ID
	command.Attribution.OrganizationBalanceVersionSnapshot = 0
	command.Attribution.UserID = fixture.member.ID
	command.Attribution.UserOrganizationID = fixture.organization.ID
	command.Attribution.APIKeyID = key.ID
	command.Attribution.APIKeyUserID = fixture.member.ID
	command.Attribution.APIKeyOrganizationID = fixture.organization.ID
	command.Attribution.AllocationID = allocation.ID
	command.Attribution.AllocationUserID = fixture.member.ID
	command.Attribution.AllocationOrganizationID = fixture.organization.ID
	command.Attribution.AllocationStatus = domain.OrganizationAllocationStatusActive
	command.Attribution.AllocationVersionSnapshot = allocation.Version

	result, err := service.NewOrganizationExecutionService(NewOrganizationExecutionRepository(tx.Client())).Reserve(ctx, command)

	require.NoError(t, err)
	require.Equal(t, service.OrganizationExecutionReservationDispositionPreDispatchRejected, result.Disposition)
	require.Equal(t, "organization_allocation_insufficient", result.FinalEvidence.ZeroProof)
	require.Zero(t, result.ReservationID)
	var reservationCount int
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM organization_reservations WHERE execution_id_snapshot = $1`, []any{command.ExecutionID}, &reservationCount))
	require.Zero(t, reservationCount)
}

func TestOrganizationExecutionPostgresRedrivesAccountPostingOnceAndClassifiesOverage(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, tx.Client(), "execution-account-redrive")
	account := mustCreateAccount(t, tx.Client(), &service.Account{Name: "organization Account redrive"})
	attemptID := uuid.NewString()
	attempt, err := fixture.repository.RecordAttemptEvidence(ctx, organizationExecutionIntegrationEvidenceCommand(
		fixture, account.ID, attemptID, 1, service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
		service.OrganizationExecutionTerminalOutcomeSuccess, "4.250000001", `{"usage":"overage"}`,
	))
	require.NoError(t, err)
	recovery := fixture.repository.(service.OrganizationExecutionRecoveryRepository)
	applied, err := recovery.RedriveAttemptAccountEffect(ctx, attemptID)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationAttemptAccountEffectStateApplied, applied.Status)
	require.Equal(t, "4.250000001", applied.RawActualCost)
	require.Equal(t, "3.250000001", applied.PlatformOverageCost)
	require.NotNil(t, applied.PostingID)

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
		TerminalOutcome:        service.OrganizationExecutionTerminalOutcomeSuccess,
		OccurredAt:             time.Now().UTC(),
	})
	require.NoError(t, err)
	require.Equal(t, "1.00000000", final.OrganizationSettledCost)

	replayed, err := recovery.RedriveAttemptAccountEffect(ctx, attemptID)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, *applied.PostingID, *replayed.PostingID)
	var postingCount, overageIncidentCount int
	var quotaUsed string
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM organization_attempt_account_postings WHERE attempt_id_snapshot = $1`, []any{attemptID}, &postingCount))
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM organization_execution_incidents WHERE execution_id_snapshot = $1 AND incident_type = $2`, []any{fixture.command.ExecutionID, service.OrganizationExecutionIncidentPlatformOverage}, &overageIncidentCount))
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COALESCE((extra->>'quota_used')::numeric, 0)::text FROM accounts WHERE id = $1`, []any{account.ID}, &quotaUsed))
	require.Equal(t, 1, postingCount)
	require.Equal(t, 1, overageIncidentCount)
	require.Equal(t, "4.250000001", quotaUsed)
}

func TestOrganizationExecutionPostgresDeletedAccountIsTerminalNoopWithPosting(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, tx.Client(), "execution-account-deleted")
	account := mustCreateAccount(t, tx.Client(), &service.Account{Name: "organization deleted Account"})
	attemptID := uuid.NewString()
	attempt, err := fixture.repository.RecordAttemptEvidence(ctx, organizationExecutionIntegrationEvidenceCommand(
		fixture, account.ID, attemptID, 1, service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
		service.OrganizationExecutionTerminalOutcomeError, "0.50000000", `{"usage":"disconnect"}`,
	))
	require.NoError(t, err)
	_, err = fixture.repository.Terminalize(ctx, service.OrganizationTerminalizationCommand{
		ExecutionID:            fixture.command.ExecutionID,
		CorrelationID:          fixture.command.CorrelationID,
		OrganizationID:         fixture.organization.ID,
		UserID:                 fixture.member.ID,
		APIKeyID:               fixture.key.ID,
		AllocationID:           fixture.allocation.ID,
		ReservationID:          fixture.reserved.ReservationID,
		FinalAttemptID:         &attemptID,
		FinalAttemptEvidenceID: &attempt.AttemptEvidenceID,
		TerminalOutcome:        service.OrganizationExecutionTerminalOutcomeError,
		OccurredAt:             time.Now().UTC(),
	})
	require.NoError(t, err)
	_, err = tx.Client().ExecContext(ctx, `UPDATE accounts SET deleted_at = NOW() WHERE id = $1`, account.ID)
	require.NoError(t, err)

	result, err := fixture.repository.(service.OrganizationExecutionRecoveryRepository).RedriveAttemptAccountEffect(ctx, attemptID)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationAttemptAccountEffectStateDeletedTargetNoop, result.Status)
	require.True(t, result.DeletedTargetNoop)
	require.NotNil(t, result.PostingID)
	var postingCount int
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM organization_attempt_account_postings WHERE attempt_id_snapshot = $1`, []any{attemptID}, &postingCount))
	require.Equal(t, 1, postingCount)
}

func TestOrganizationExecutionPostgresAccountReferenceLockSerializesDeletion(t *testing.T) {
	account := mustCreateAccount(t, testEntClient(t), &service.Account{Name: "organization Account deletion lock"})
	tx, err := integrationEntClient.Tx(context.Background())
	require.NoError(t, err)
	ctx := dbent.NewTxContext(context.Background(), tx)
	reference, err := currentExecutionAccountRef(ctx, tx.Client(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, reference)

	deleteDone := make(chan error, 1)
	go func() {
		deleteCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, deleteErr := integrationDB.ExecContext(deleteCtx, `UPDATE accounts SET deleted_at = NOW() WHERE id = $1`, account.ID)
		deleteDone <- deleteErr
	}()

	select {
	case deleteErr := <-deleteDone:
		require.NoError(t, tx.Rollback())
		require.Failf(t, "Account deletion was not serialized", "delete completed while Account reference row lock was held: %v", deleteErr)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, tx.Rollback())
	require.NoError(t, <-deleteDone)
}

func TestOrganizationExecutionPostgresDeletedUserAndAPIKeyOutboxesAreTerminalNoops(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, tx.Client(), "execution-outbox-deleted-targets")
	account := mustCreateAccount(t, tx.Client(), &service.Account{Name: "organization deleted outbox targets"})
	attemptID := uuid.NewString()
	attempt, err := fixture.repository.RecordAttemptEvidence(ctx, organizationExecutionIntegrationEvidenceCommand(
		fixture, account.ID, attemptID, 1, service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
		service.OrganizationExecutionTerminalOutcomeSuccess, "0.50000000", `{"usage":"deleted-targets"}`,
	))
	require.NoError(t, err)
	_, err = fixture.repository.Terminalize(ctx, service.OrganizationTerminalizationCommand{
		ExecutionID:            fixture.command.ExecutionID,
		CorrelationID:          fixture.command.CorrelationID,
		OrganizationID:         fixture.organization.ID,
		UserID:                 fixture.member.ID,
		APIKeyID:               fixture.key.ID,
		AllocationID:           fixture.allocation.ID,
		ReservationID:          fixture.reserved.ReservationID,
		FinalAttemptID:         &attemptID,
		FinalAttemptEvidenceID: &attempt.AttemptEvidenceID,
		TerminalOutcome:        service.OrganizationExecutionTerminalOutcomeSuccess,
		OccurredAt:             time.Now().UTC(),
	})
	require.NoError(t, err)

	_, err = tx.Client().ExecContext(ctx, `UPDATE api_keys SET deleted_at = NOW() WHERE id = $1`, fixture.key.ID)
	require.NoError(t, err)
	_, err = tx.Client().ExecContext(ctx, `UPDATE users SET deleted_at = NOW() WHERE id = $1`, fixture.member.ID)
	require.NoError(t, err)

	var apiKeyOutboxID, userOutboxID int64
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT id FROM organization_execution_outboxes WHERE execution_id_snapshot = $1 AND event_type = 'api_key_quota'`, []any{fixture.command.ExecutionID}, &apiKeyOutboxID))
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT id FROM organization_execution_outboxes WHERE execution_id_snapshot = $1 AND event_type = 'user_platform_quota'`, []any{fixture.command.ExecutionID}, &userOutboxID))
	recovery := fixture.repository.(service.OrganizationExecutionRecoveryRepository)

	for _, outboxID := range []int64{apiKeyOutboxID, userOutboxID} {
		applied, err := recovery.RedriveExecutionOutbox(ctx, outboxID)
		require.NoError(t, err)
		require.Equal(t, service.OrganizationAttemptAccountEffectStateDeletedTargetNoop, applied.Status)
		require.True(t, applied.DeletedTargetNoop)
		require.False(t, applied.Replayed)

		replayed, err := recovery.RedriveExecutionOutbox(ctx, outboxID)
		require.NoError(t, err)
		require.Equal(t, service.OrganizationAttemptAccountEffectStateDeletedTargetNoop, replayed.Status)
		require.True(t, replayed.DeletedTargetNoop)
		require.True(t, replayed.Replayed)
	}

	var userQuotaCount int
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM user_platform_quotas WHERE user_id = $1`, []any{fixture.member.ID}, &userQuotaCount))
	require.Zero(t, userQuotaCount)
}

func TestOrganizationExecutionPostgresConflictingProvenanceRemainsPendingIncident(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, tx.Client(), "execution-provenance-conflict")
	account := mustCreateAccount(t, tx.Client(), &service.Account{Name: "organization provenance conflict"})
	attemptID := uuid.NewString()
	providerRequestID := "provider-reconcile-request"
	providerCorrelationID := uuid.NewString()
	recovery := fixture.repository.(service.OrganizationExecutionRecoveryRepository)
	require.NoError(t, recovery.RecordPendingAttempt(ctx, service.OrganizationPendingAttemptCommand{
		ExecutionID:           fixture.command.ExecutionID,
		AttemptID:             attemptID,
		CorrelationID:         fixture.command.CorrelationID,
		ReservationID:         fixture.reserved.ReservationID,
		OrganizationID:        fixture.organization.ID,
		UserID:                fixture.member.ID,
		APIKeyID:              fixture.key.ID,
		AllocationID:          fixture.allocation.ID,
		AttemptSequence:       1,
		AccountID:             account.ID,
		RouteSnapshotJSON:     `{}`,
		RouteSnapshotHash:     repositoryExecutionHash(`{}`),
		PricingSnapshotJSON:   fixture.command.PricingSnapshotJSON,
		PricingSnapshotHash:   fixture.command.PricingSnapshotHash,
		ProviderRequestID:     providerRequestID,
		ProviderCorrelationID: providerCorrelationID,
		DispatchedAt:          time.Now().UTC(),
	}))
	queryEvidence := organizationExecutionIntegrationEvidenceCommand(
		fixture, account.ID, attemptID, 1, service.OrganizationAttemptEvidenceProvenanceProviderQuery,
		service.OrganizationExecutionTerminalOutcomeSuccess, "0.50000000", `{"usage":"query"}`,
	)
	queryEvidence.ProviderRequestID = providerRequestID
	queryEvidence.ProviderCorrelationID = providerCorrelationID
	_, err := fixture.repository.RecordAttemptEvidence(ctx, queryEvidence)
	require.NoError(t, err)
	directEvidence := organizationExecutionIntegrationEvidenceCommand(
		fixture, account.ID, attemptID, 1, service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
		service.OrganizationExecutionTerminalOutcomeSuccess, "0.60000000", `{"usage":"direct"}`,
	)
	directEvidence.ProviderRequestID = providerRequestID
	directEvidence.ProviderCorrelationID = providerCorrelationID
	_, err = fixture.repository.RecordAttemptEvidence(ctx, directEvidence)
	require.NoError(t, err)

	_, err = recovery.ReconcilePendingExecution(ctx, organizationExecutionIntegrationReconciliationCommand(fixture, time.Now().UTC()))
	require.ErrorIs(t, err, service.ErrOrganizationExecutionPendingReconciliation)
	var finalCount, incidentCount int
	var reservationStatus, effectStatus string
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM organization_final_execution_evidences WHERE execution_id_snapshot = $1`, []any{fixture.command.ExecutionID}, &finalCount))
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM organization_execution_incidents WHERE execution_id_snapshot = $1 AND incident_type = $2`, []any{fixture.command.ExecutionID, service.OrganizationExecutionIncidentProvenanceConflict}, &incidentCount))
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT status FROM organization_reservations WHERE id = $1`, []any{fixture.reserved.ReservationID}, &reservationStatus))
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT status FROM organization_attempt_account_effects WHERE attempt_id_snapshot = $1`, []any{attemptID}, &effectStatus))
	require.Zero(t, finalCount)
	require.Equal(t, 1, incidentCount)
	require.Equal(t, service.OrganizationReservationStatePendingReconciliation, reservationStatus)
	require.Equal(t, service.OrganizationAttemptAccountEffectStateOperatorAttention, effectStatus)
}

func TestOrganizationExecutionPostgresReconciliationUsesEvidenceAndWritesImmutableAudit(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, tx.Client(), "execution-reconcile-audit")
	account := mustCreateAccount(t, tx.Client(), &service.Account{Name: "organization reconcile audit"})
	attemptID := uuid.NewString()
	providerRequestID := "provider-reconcile-audit"
	providerCorrelationID := uuid.NewString()
	recovery := fixture.repository.(service.OrganizationExecutionRecoveryRepository)
	require.NoError(t, recovery.RecordPendingAttempt(ctx, service.OrganizationPendingAttemptCommand{
		ExecutionID:           fixture.command.ExecutionID,
		AttemptID:             attemptID,
		CorrelationID:         fixture.command.CorrelationID,
		ReservationID:         fixture.reserved.ReservationID,
		OrganizationID:        fixture.organization.ID,
		UserID:                fixture.member.ID,
		APIKeyID:              fixture.key.ID,
		AllocationID:          fixture.allocation.ID,
		AttemptSequence:       1,
		AccountID:             account.ID,
		RouteSnapshotJSON:     `{}`,
		RouteSnapshotHash:     repositoryExecutionHash(`{}`),
		PricingSnapshotJSON:   fixture.command.PricingSnapshotJSON,
		PricingSnapshotHash:   fixture.command.PricingSnapshotHash,
		ProviderRequestID:     providerRequestID,
		ProviderCorrelationID: providerCorrelationID,
		DispatchedAt:          time.Now().UTC(),
	}))
	evidence := organizationExecutionIntegrationEvidenceCommand(
		fixture, account.ID, attemptID, 1, service.OrganizationAttemptEvidenceProvenanceProviderQuery,
		service.OrganizationExecutionTerminalOutcomeError, "0.25000000", `{"usage":"reconciled"}`,
	)
	evidence.ProviderRequestID = providerRequestID
	evidence.ProviderCorrelationID = providerCorrelationID
	providerObservedAt := fixture.command.AcceptedAt.UTC().Add(time.Minute).Truncate(time.Microsecond)
	evidence.ProviderObservedAt = providerObservedAt
	_, err := fixture.repository.RecordAttemptEvidence(ctx, evidence)
	require.NoError(t, err)
	reconciledAt := time.Now().UTC()
	command := organizationExecutionIntegrationReconciliationCommand(fixture, reconciledAt)
	final, err := recovery.ReconcilePendingExecution(ctx, command)
	require.NoError(t, err)
	require.NotNil(t, final.ReconciledAt)
	require.True(t, final.ReconciledAt.Equal(reconciledAt))
	require.True(t, final.OccurredAt.Equal(providerObservedAt))
	require.Equal(t, "0.25000000", final.OrganizationSettledCost)
	replayed, err := recovery.ReconcilePendingExecution(ctx, command)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, final.FinalEvidenceID, replayed.FinalEvidenceID)
	conflictCommand := command
	conflictCommand.CanonicalPayloadHash = repositoryExecutionHash(`{"execution_id":"different"}`)
	_, err = recovery.ReconcilePendingExecution(ctx, conflictCommand)
	require.ErrorIs(t, err, service.ErrIdempotencyKeyConflict)
	var auditCount int
	var action, sourceReason, rawCost, settledCost, reservationBefore, reservationAfter, idempotencyKeyHash string
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM organization_execution_reconciliation_audits WHERE execution_id_snapshot = $1`, []any{fixture.command.ExecutionID}, &auditCount))
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `
		SELECT action, internal_reason, raw_actual_cost::text, organization_settled_cost::text,
			reservation_status_before, reservation_status_after, idempotency_key_hash
		FROM organization_execution_reconciliation_audits WHERE execution_id_snapshot = $1
	`, []any{fixture.command.ExecutionID}, &action, &sourceReason, &rawCost, &settledCost, &reservationBefore, &reservationAfter, &idempotencyKeyHash))
	require.Equal(t, 1, auditCount)
	require.Equal(t, service.OrganizationReservationStateSettled, action)
	require.Contains(t, sourceReason, service.OrganizationAttemptEvidenceProvenanceProviderQuery)
	require.Equal(t, "0.25", rawCost)
	require.Equal(t, "0.25000000", settledCost)
	require.Equal(t, service.OrganizationReservationStatePendingReconciliation, reservationBefore)
	require.Equal(t, service.OrganizationReservationStateSettled, reservationAfter)
	require.Equal(t, command.IdempotencyKeyHash, idempotencyKeyHash)
}

func TestOrganizationExecutionPostgresConcurrentReconciliationRejectsScopedKeyReuse(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	first := createReservedOrganizationExecutionIntegrationFixture(t, ctx, client, "execution-reconcile-key-race")
	secondMember, err := client.User.Create().
		SetEmail(fmt.Sprintf("execution-reconcile-key-race-%d@example.com", time.Now().UnixNano())).
		SetPasswordHash("hash").
		SetRole(domain.RoleUser).
		SetStatus(domain.StatusActive).
		SetOrganizationID(first.organization.ID).
		SetOrganizationRole(domain.OrganizationRoleMember).
		Save(ctx)
	require.NoError(t, err)
	secondAllocation, err := client.OrganizationAllocation.Create().
		SetOrganizationID(first.organization.ID).
		SetUserIDSnapshot(secondMember.ID).
		SetUserRefID(secondMember.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.RequireFromString("1.00000000")).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)
	secondKey, err := client.APIKey.Create().
		SetUserID(secondMember.ID).
		SetOrganizationID(first.organization.ID).
		SetKey(fmt.Sprintf("execution-reconcile-key-race-%d", time.Now().UnixNano())).
		SetName("organization reconciliation scoped key race").
		SetStatus(service.StatusAPIKeyActive).
		Save(ctx)
	require.NoError(t, err)
	secondCommand := first.command
	secondCommand.ExecutionID = uuid.NewString()
	secondCommand.CorrelationID = uuid.NewString()
	secondCommand.ClientRequestID = "execution-reconcile-key-race-second"
	secondCommand.BillingFingerprint = repositoryExecutionHash(secondCommand.ExecutionID)
	secondCommand.AcceptedAt = time.Now().UTC()
	secondCommand.Attribution.UserID = secondMember.ID
	secondCommand.Attribution.UserOrganizationID = first.organization.ID
	secondCommand.Attribution.APIKeyID = secondKey.ID
	secondCommand.Attribution.APIKeyUserID = secondMember.ID
	secondCommand.Attribution.APIKeyOrganizationID = first.organization.ID
	secondCommand.Attribution.AllocationID = secondAllocation.ID
	secondCommand.Attribution.AllocationUserID = secondMember.ID
	secondCommand.Attribution.AllocationOrganizationID = first.organization.ID
	secondCommand.Attribution.AllocationVersionSnapshot = secondAllocation.Version
	secondReserved, err := first.repository.Reserve(ctx, secondCommand)
	require.NoError(t, err)
	second := reservedOrganizationExecutionIntegrationFixture{
		organization: first.organization,
		member:       secondMember,
		key:          secondKey,
		allocation:   secondAllocation,
		command:      secondCommand,
		reserved:     secondReserved,
		repository:   first.repository,
	}

	prepareOrganizationPendingReconciliationIntegration(t, ctx, first, mustCreateAccount(t, client, &service.Account{Name: "first scoped key Account"}).ID)
	prepareOrganizationPendingReconciliationIntegration(t, ctx, second, mustCreateAccount(t, client, &service.Account{Name: "second scoped key Account"}).ID)
	commands := []service.OrganizationPendingReconciliationCommand{
		organizationExecutionIntegrationReconciliationCommand(first, time.Now().UTC()),
		organizationExecutionIntegrationReconciliationCommand(second, time.Now().UTC()),
	}
	commands[1].IdempotencyKeyHash = commands[0].IdempotencyKeyHash
	results := make([]*service.OrganizationFinalExecutionEvidenceResult, len(commands))
	errorsByExecution := make([]error, len(commands))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := range commands {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], errorsByExecution[index] = first.repository.(service.OrganizationExecutionRecoveryRepository).ReconcilePendingExecution(ctx, commands[index])
		}(index)
	}
	close(start)
	wait.Wait()

	settled, conflicts := 0, 0
	for index, executionErr := range errorsByExecution {
		if executionErr == nil {
			settled++
			require.NotNil(t, results[index])
			continue
		}
		if errors.Is(executionErr, service.ErrIdempotencyKeyConflict) {
			conflicts++
			continue
		}
		require.NoError(t, executionErr)
	}
	require.Equal(t, 1, settled)
	require.Equal(t, 1, conflicts)
	var auditCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM organization_execution_reconciliation_audits
		WHERE organization_id = $1 AND idempotency_key_hash = $2
	`, first.organization.ID, commands[0].IdempotencyKeyHash).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

func TestOrganizationExecutionPostgresPersistsSecondFailoverAttempt(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	fixture := createReservedOrganizationExecutionIntegrationFixture(t, ctx, tx.Client(), "execution-second-attempt")
	firstAccount := mustCreateAccount(t, tx.Client(), &service.Account{Name: "organization first failover Account"})
	secondAccount := mustCreateAccount(t, tx.Client(), &service.Account{Name: "organization second failover Account"})
	firstAttemptID := uuid.NewString()
	secondAttemptID := uuid.NewString()
	_, err := fixture.repository.RecordAttemptEvidence(ctx, organizationExecutionIntegrationEvidenceCommand(
		fixture, firstAccount.ID, firstAttemptID, 1, service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
		service.OrganizationExecutionTerminalOutcomeError, "0.10000000", `{"status":"failed"}`,
	))
	require.NoError(t, err)
	secondEvidence, err := fixture.repository.RecordAttemptEvidence(ctx, organizationExecutionIntegrationEvidenceCommand(
		fixture, secondAccount.ID, secondAttemptID, 2, service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
		service.OrganizationExecutionTerminalOutcomeSuccess, "0.40000000", `{"status":"success"}`,
	))
	require.NoError(t, err)
	final, err := fixture.repository.Terminalize(ctx, service.OrganizationTerminalizationCommand{
		ExecutionID:            fixture.command.ExecutionID,
		CorrelationID:          fixture.command.CorrelationID,
		OrganizationID:         fixture.organization.ID,
		UserID:                 fixture.member.ID,
		APIKeyID:               fixture.key.ID,
		AllocationID:           fixture.allocation.ID,
		ReservationID:          fixture.reserved.ReservationID,
		FinalAttemptID:         &secondAttemptID,
		FinalAttemptEvidenceID: &secondEvidence.AttemptEvidenceID,
		TerminalOutcome:        service.OrganizationExecutionTerminalOutcomeSuccess,
		OccurredAt:             time.Now().UTC(),
	})
	require.NoError(t, err)
	require.Equal(t, secondAttemptID, *final.FinalAttemptID)
	var attemptCount, postingCount int
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `SELECT COUNT(*) FROM organization_execution_attempts WHERE execution_id_snapshot = $1`, []any{fixture.command.ExecutionID}, &attemptCount))
	require.NoError(t, scanExecutionOne(ctx, tx.Client(), `
		SELECT COUNT(*) FROM organization_ledger_postings
		WHERE execution_id_snapshot = $1 AND final_evidence_id_snapshot IS NOT NULL
	`, []any{fixture.command.ExecutionID}, &postingCount))
	require.Equal(t, 2, attemptCount)
	require.Equal(t, 1, postingCount)
}

func organizationExecutionIntegrationEvidenceCommand(
	fixture reservedOrganizationExecutionIntegrationFixture,
	accountID int64,
	attemptID string,
	sequence int,
	provenance string,
	terminalOutcome string,
	rawCost string,
	evidenceJSON string,
) service.OrganizationAttemptEvidenceCommand {
	evidenceHash := repositoryExecutionHash(evidenceJSON)
	return service.OrganizationAttemptEvidenceCommand{
		ExecutionID:           fixture.command.ExecutionID,
		AttemptID:             attemptID,
		CorrelationID:         fixture.command.CorrelationID,
		ReservationID:         fixture.reserved.ReservationID,
		OrganizationID:        fixture.organization.ID,
		UserID:                fixture.member.ID,
		APIKeyID:              fixture.key.ID,
		AllocationID:          fixture.allocation.ID,
		AttemptSequence:       sequence,
		AccountID:             accountID,
		RouteSnapshotJSON:     `{}`,
		RouteSnapshotHash:     repositoryExecutionHash(`{}`),
		PricingSnapshotJSON:   fixture.command.PricingSnapshotJSON,
		PricingSnapshotHash:   fixture.command.PricingSnapshotHash,
		ProviderRequestID:     fmt.Sprintf("provider-request-%s-%d", attemptID, sequence),
		ProviderCorrelationID: uuid.NewString(),
		Provenance:            provenance,
		TerminalOutcome:       terminalOutcome,
		ProvenanceHash:        evidenceHash,
		EvidenceJSON:          evidenceJSON,
		EvidenceHash:          evidenceHash,
		RawActualCost:         rawCost,
		InputTokens:           4,
		OutputTokens:          2,
		ProviderObservedAt:    time.Now().UTC(),
		IngestedAt:            time.Now().UTC(),
	}
}

func organizationExecutionIntegrationReconciliationCommand(fixture reservedOrganizationExecutionIntegrationFixture, reconciledAt time.Time) service.OrganizationPendingReconciliationCommand {
	return service.OrganizationPendingReconciliationCommand{
		ExecutionID:          fixture.command.ExecutionID,
		OperationID:          uuid.NewString(),
		CorrelationID:        uuid.NewString(),
		IdempotencyKeyHash:   repositoryExecutionHash("reconcile-key-" + fixture.command.ExecutionID),
		CanonicalPayloadHash: repositoryExecutionHash(`{"execution_id":"` + fixture.command.ExecutionID + `","action":"rule_computed_reconcile"}`),
		Actor: service.OrganizationReconciliationActor{
			UserID:             fixture.member.ID,
			DisplayName:        "platform admin",
			PlatformRole:       domain.RoleAdmin,
			AuthorizationScope: domain.OrganizationFundingAuthorizationPlatformAdminJWT,
			Active:             true,
		},
		ReconciledAt: reconciledAt,
	}
}

func prepareOrganizationPendingReconciliationIntegration(t *testing.T, ctx context.Context, fixture reservedOrganizationExecutionIntegrationFixture, accountID int64) {
	t.Helper()
	attemptID := uuid.NewString()
	providerRequestID := "provider-pending-" + attemptID
	providerCorrelationID := uuid.NewString()
	recovery := fixture.repository.(service.OrganizationExecutionRecoveryRepository)
	require.NoError(t, recovery.RecordPendingAttempt(ctx, service.OrganizationPendingAttemptCommand{
		ExecutionID:           fixture.command.ExecutionID,
		AttemptID:             attemptID,
		CorrelationID:         fixture.command.CorrelationID,
		ReservationID:         fixture.reserved.ReservationID,
		OrganizationID:        fixture.organization.ID,
		UserID:                fixture.member.ID,
		APIKeyID:              fixture.key.ID,
		AllocationID:          fixture.allocation.ID,
		AttemptSequence:       1,
		AccountID:             accountID,
		RouteSnapshotJSON:     `{}`,
		RouteSnapshotHash:     repositoryExecutionHash(`{}`),
		PricingSnapshotJSON:   fixture.command.PricingSnapshotJSON,
		PricingSnapshotHash:   fixture.command.PricingSnapshotHash,
		ProviderRequestID:     providerRequestID,
		ProviderCorrelationID: providerCorrelationID,
		DispatchedAt:          time.Now().UTC(),
	}))
	evidence := organizationExecutionIntegrationEvidenceCommand(
		fixture, accountID, attemptID, 1, service.OrganizationAttemptEvidenceProvenanceProviderQuery,
		service.OrganizationExecutionTerminalOutcomeSuccess, "0.25000000", `{"usage":"scoped-key-race"}`,
	)
	evidence.ProviderRequestID = providerRequestID
	evidence.ProviderCorrelationID = providerCorrelationID
	_, err := fixture.repository.RecordAttemptEvidence(ctx, evidence)
	require.NoError(t, err)
}

type reservedOrganizationExecutionIntegrationFixture struct {
	organization *service.Organization
	member       *dbent.User
	key          *dbent.APIKey
	allocation   *dbent.OrganizationAllocation
	command      service.OrganizationExecutionReservationCommand
	reserved     *service.OrganizationExecutionReservationResult
	repository   service.OrganizationExecutionRepository
}

func createReservedOrganizationExecutionIntegrationFixture(t *testing.T, ctx context.Context, client *dbent.Client, suffix string) reservedOrganizationExecutionIntegrationFixture {
	t.Helper()
	fixture := createOrganizationAllocationIntegrationFixture(t, ctx, client, fmt.Sprintf("%s-%d", suffix, time.Now().UnixNano()), "0.00000000", "2.00000000")
	key, err := client.APIKey.Create().
		SetUserID(fixture.member.ID).
		SetOrganizationID(fixture.organization.ID).
		SetKey(fmt.Sprintf("%s-key-%d", suffix, time.Now().UnixNano())).
		SetName("organization execution constraint test key").
		SetStatus(service.StatusAPIKeyActive).
		Save(ctx)
	require.NoError(t, err)
	allocation, err := client.OrganizationAllocation.Query().
		Where(organizationallocation.UserIDSnapshotEQ(fixture.member.ID)).
		Only(ctx)
	require.NoError(t, err)
	command := repositoryReservationCommand(time.Now().UTC())
	command.Attribution.OrganizationID = fixture.organization.ID
	command.Attribution.OrganizationBalanceVersionSnapshot = 0
	command.Attribution.UserID = fixture.member.ID
	command.Attribution.UserOrganizationID = fixture.organization.ID
	command.Attribution.APIKeyID = key.ID
	command.Attribution.APIKeyUserID = fixture.member.ID
	command.Attribution.APIKeyOrganizationID = fixture.organization.ID
	command.Attribution.AllocationID = allocation.ID
	command.Attribution.AllocationUserID = fixture.member.ID
	command.Attribution.AllocationOrganizationID = fixture.organization.ID
	command.Attribution.AllocationStatus = domain.OrganizationAllocationStatusActive
	command.Attribution.AllocationVersionSnapshot = allocation.Version
	repository := NewOrganizationExecutionRepository(client)
	reserved, err := repository.Reserve(ctx, command)
	require.NoError(t, err)
	return reservedOrganizationExecutionIntegrationFixture{
		organization: fixture.organization,
		member:       fixture.member,
		key:          key,
		allocation:   allocation,
		command:      command,
		reserved:     reserved,
		repository:   repository,
	}
}
