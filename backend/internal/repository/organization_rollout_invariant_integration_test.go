//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/organizationallocation"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestOrganizationRolloutDisableReservationLinearizesAndConservesFunds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := testEntClient(t)
	suffix := fmt.Sprintf("rollout-disable-reserve-%d", time.Now().UnixNano())
	fixture := createOrganizationAllocationIntegrationFixture(t, ctx, client, suffix, "0.00000000", "0.00000000")
	var accountID int64
	t.Cleanup(func() {
		cleanupOrganizationRolloutInvariantFixture(t, fixture.organization.ID, fixture.creator.ID, accountID)
	})

	funding := service.NewOrganizationFundingService(NewOrganizationFundingRepository(client))
	_, err := funding.Credit(
		ctx,
		fundingActorForUser(fixture.creator),
		fixture.organization.ID,
		"2.00000000",
		service.OrganizationFundingReasons{InternalReason: "rollout invariant seed"},
		"rollout-invariant-credit-"+suffix,
	)
	require.NoError(t, err)
	allocationManager := newOrganizationAllocationIntegrationManager(t, client)
	preview, err := allocationManager.Preview(
		ctx,
		fixture.actorSnapshot(),
		fixture.organization.ID,
		domain.OrganizationFundingOperationCustomAllocation,
		service.OrganizationAllocationPreviewInput{Targets: []service.OrganizationAllocationTargetInput{{
			UserID: fixture.member.ID,
			Amount: "2.00000000",
		}}},
	)
	require.NoError(t, err)
	_, err = allocationManager.Execute(
		ctx,
		fixture.actorSnapshot(),
		fixture.organization.ID,
		domain.OrganizationFundingOperationCustomAllocation,
		service.OrganizationAllocationExecuteInput{PreviewToken: preview.PreviewToken, Confirmation: preview.Confirmation},
		"rollout-invariant-allocation-"+suffix,
	)
	require.NoError(t, err)

	key, err := client.APIKey.Create().
		SetUserID(fixture.member.ID).
		SetOrganizationID(fixture.organization.ID).
		SetKey(fmt.Sprintf("rollout-invariant-key-%d", time.Now().UnixNano())).
		SetName("organization rollout invariant key").
		SetStatus(service.StatusAPIKeyActive).
		Save(ctx)
	require.NoError(t, err)
	allocation, err := client.OrganizationAllocation.Query().
		Where(organizationallocation.UserIDSnapshotEQ(fixture.member.ID)).
		Only(ctx)
	require.NoError(t, err)
	organizationModel, err := client.Organization.Get(ctx, fixture.organization.ID)
	require.NoError(t, err)

	command := repositoryReservationCommand(time.Now().UTC())
	command.Attribution.OrganizationID = fixture.organization.ID
	command.Attribution.OrganizationBalanceVersionSnapshot = organizationModel.BalanceVersion
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

	var executionRepository service.OrganizationExecutionRepository = NewOrganizationExecutionRepository(client)
	lifecycleRepository := NewOrganizationLifecycleRepository(client)
	start := make(chan struct{})
	var wait sync.WaitGroup
	var reserveResult *service.OrganizationExecutionReservationResult
	var reserveErr error
	var disableErr error
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		reserveResult, reserveErr = executionRepository.Reserve(ctx, command)
	}()
	go func() {
		defer wait.Done()
		<-start
		_, disableErr = lifecycleRepository.Transition(ctx, service.OrganizationLifecycleCommand{
			OrganizationID: fixture.organization.ID,
			TargetStatus:   service.OrganizationStatusDisabled,
		})
	}()
	close(start)
	wait.Wait()
	require.NoError(t, reserveErr)
	require.NoError(t, disableErr)
	require.NotNil(t, reserveResult)

	disabled, err := client.Organization.Get(ctx, fixture.organization.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationStatusDisabled, disabled.Status)

	currentAllocation, err := client.OrganizationAllocation.Get(ctx, allocation.ID)
	require.NoError(t, err)
	afterDisableCommand := command
	afterDisableCommand.ExecutionID = uuid.NewString()
	afterDisableCommand.CorrelationID = uuid.NewString()
	afterDisableCommand.ClientRequestID = "rollout-invariant-after-disable-" + suffix
	afterDisableCommand.BillingFingerprint = repositoryExecutionHash(afterDisableCommand.ExecutionID)
	afterDisableCommand.AcceptedAt = time.Now().UTC()
	afterDisableCommand.Attribution.AllocationVersionSnapshot = currentAllocation.Version
	afterDisableResult, err := executionRepository.Reserve(ctx, afterDisableCommand)
	require.NoError(t, err)
	require.Equal(t, service.OrganizationExecutionReservationDispositionPreDispatchRejected, afterDisableResult.Disposition)
	require.Zero(t, afterDisableResult.ReservationID)
	require.NotNil(t, afterDisableResult.FinalEvidence)
	require.Equal(t, "0.00000000", afterDisableResult.FinalEvidence.OrganizationSettledCost)
	require.Equal(t, "stale_authoritative_attribution", afterDisableResult.FinalEvidence.ZeroProof)

	reserveWon := false
	switch reserveResult.Disposition {
	case service.OrganizationExecutionReservationDispositionPreDispatchRejected:
		require.Zero(t, reserveResult.ReservationID)
		require.NotNil(t, reserveResult.FinalEvidence)
		require.Equal(t, "0.00000000", reserveResult.FinalEvidence.OrganizationSettledCost)
		require.Equal(t, "stale_authoritative_attribution", reserveResult.FinalEvidence.ZeroProof)
	case service.OrganizationExecutionReservationDispositionReserved:
		reserveWon = true
		require.Equal(t, "1.00000000", reserveResult.ReservedAmount)
		account := mustCreateAccount(t, client, &service.Account{Name: "organization rollout invariant Account"})
		accountID = account.ID
		reservedFixture := reservedOrganizationExecutionIntegrationFixture{
			organization: fixture.organization,
			member:       fixture.member,
			key:          key,
			allocation:   allocation,
			command:      command,
			reserved:     reserveResult,
			repository:   executionRepository,
		}
		attemptID := uuid.NewString()
		attempt, evidenceErr := executionRepository.RecordAttemptEvidence(ctx, organizationExecutionIntegrationEvidenceCommand(
			reservedFixture,
			account.ID,
			attemptID,
			1,
			service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
			service.OrganizationExecutionTerminalOutcomeSuccess,
			"0.40000000",
			`{"usage":"rollout-disable-race"}`,
		))
		require.NoError(t, evidenceErr)
		recovery := executionRepository.(service.OrganizationExecutionRecoveryRepository)
		applied, redriveErr := recovery.RedriveAttemptAccountEffect(ctx, attemptID)
		require.NoError(t, redriveErr)
		require.False(t, applied.Replayed)
		require.NotNil(t, applied.PostingID)

		terminalCommand := service.OrganizationTerminalizationCommand{
			ExecutionID:            command.ExecutionID,
			CorrelationID:          command.CorrelationID,
			OrganizationID:         fixture.organization.ID,
			UserID:                 fixture.member.ID,
			APIKeyID:               key.ID,
			AllocationID:           allocation.ID,
			ReservationID:          reserveResult.ReservationID,
			FinalAttemptID:         &attemptID,
			FinalAttemptEvidenceID: &attempt.AttemptEvidenceID,
			TerminalOutcome:        service.OrganizationExecutionTerminalOutcomeSuccess,
			OccurredAt:             time.Now().UTC(),
		}
		final, terminalErr := executionRepository.Terminalize(ctx, terminalCommand)
		require.NoError(t, terminalErr)
		require.Equal(t, "0.40000000", final.OrganizationSettledCost)
		replayedFinal, terminalErr := executionRepository.Terminalize(ctx, terminalCommand)
		require.NoError(t, terminalErr)
		require.True(t, replayedFinal.Replayed)
		require.Equal(t, final.FinalEvidenceID, replayedFinal.FinalEvidenceID)

		replayedEffect, redriveErr := recovery.RedriveAttemptAccountEffect(ctx, attemptID)
		require.NoError(t, redriveErr)
		require.True(t, replayedEffect.Replayed)
		require.Equal(t, *applied.PostingID, *replayedEffect.PostingID)
		var effectCount, accountPostingCount int
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM organization_attempt_account_effects WHERE attempt_id_snapshot = $1
		`, attemptID).Scan(&effectCount))
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM organization_attempt_account_postings WHERE attempt_id_snapshot = $1
		`, attemptID).Scan(&accountPostingCount))
		require.Equal(t, 1, effectCount)
		require.Equal(t, 1, accountPostingCount)
	default:
		require.Failf(t, "unexpected reservation disposition", "got %q", reserveResult.Disposition)
	}

	var firstReservationCount, firstFinalCount, firstConsumptionCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM organization_reservations WHERE execution_id_snapshot = $1
	`, command.ExecutionID).Scan(&firstReservationCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM organization_final_execution_evidences WHERE execution_id_snapshot = $1
	`, command.ExecutionID).Scan(&firstFinalCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM organization_ledger_postings AS posting
		JOIN organization_ledger_operations AS operation ON operation.id = posting.operation_ref_id
		WHERE posting.execution_id_snapshot = $1 AND operation.operation_type = $2
	`, command.ExecutionID, domain.OrganizationFundingOperationConsumption).Scan(&firstConsumptionCount))
	if reserveWon {
		require.Equal(t, 1, firstReservationCount)
		require.Equal(t, 1, firstFinalCount)
		require.Equal(t, 1, firstConsumptionCount)
	} else {
		require.Zero(t, firstReservationCount)
		require.Equal(t, 1, firstFinalCount)
		require.Zero(t, firstConsumptionCount)
	}

	var rejectedReservationCount, rejectedFinalCount, rejectedConsumptionCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM organization_reservations WHERE execution_id_snapshot = $1
	`, afterDisableCommand.ExecutionID).Scan(&rejectedReservationCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM organization_final_execution_evidences WHERE execution_id_snapshot = $1
	`, afterDisableCommand.ExecutionID).Scan(&rejectedFinalCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM organization_ledger_postings AS posting
		JOIN organization_ledger_operations AS operation ON operation.id = posting.operation_ref_id
		WHERE posting.execution_id_snapshot = $1 AND operation.operation_type = $2
	`, afterDisableCommand.ExecutionID, domain.OrganizationFundingOperationConsumption).Scan(&rejectedConsumptionCount))
	require.Zero(t, rejectedReservationCount)
	require.Equal(t, 1, rejectedFinalCount)
	require.Zero(t, rejectedConsumptionCount)

	var creditedText, unallocatedText, availableText, reservedText, consumedText string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(posting.delta_amount), 0)::text
		FROM organization_ledger_postings AS posting
		JOIN organization_ledger_operations AS operation ON operation.id = posting.operation_ref_id
		WHERE posting.organization_id = $1
			AND operation.operation_type = $2
			AND posting.bucket_type = $3
	`, fixture.organization.ID, domain.OrganizationFundingOperationCredit, domain.OrganizationLedgerBucketUnallocated).Scan(&creditedText))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT unallocated_pool::text FROM organizations WHERE id = $1
	`, fixture.organization.ID).Scan(&unallocatedText))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(available), 0)::text, COALESCE(SUM(reserved), 0)::text
		FROM organization_allocations WHERE organization_id = $1
	`, fixture.organization.ID).Scan(&availableText, &reservedText))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(-posting.delta_amount), 0)::text
		FROM organization_ledger_postings AS posting
		JOIN organization_ledger_operations AS operation ON operation.id = posting.operation_ref_id
		WHERE posting.organization_id = $1
			AND operation.operation_type = $2
	`, fixture.organization.ID, domain.OrganizationFundingOperationConsumption).Scan(&consumedText))
	credited := decimal.RequireFromString(creditedText)
	unallocated := decimal.RequireFromString(unallocatedText)
	available := decimal.RequireFromString(availableText)
	reserved := decimal.RequireFromString(reservedText)
	consumed := decimal.RequireFromString(consumedText)
	require.Equal(t, "2.00000000", credited.StringFixed(domain.OrganizationMoneyScale))
	require.True(t, credited.Equal(unallocated.Add(available).Add(reserved).Add(consumed)))
	require.True(t, reserved.IsZero())
	if reserveWon {
		require.Equal(t, "1.60000000", available.StringFixed(domain.OrganizationMoneyScale))
		require.Equal(t, "0.40000000", consumed.StringFixed(domain.OrganizationMoneyScale))
	} else {
		require.Equal(t, "2.00000000", available.StringFixed(domain.OrganizationMoneyScale))
		require.True(t, consumed.IsZero())
	}

	var negativeOrganizationBuckets, negativeAllocationBuckets int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM organizations WHERE id = $1 AND unallocated_pool < 0
	`, fixture.organization.ID).Scan(&negativeOrganizationBuckets))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM organization_allocations
		WHERE organization_id = $1 AND (available < 0 OR reserved < 0)
	`, fixture.organization.ID).Scan(&negativeAllocationBuckets))
	require.Zero(t, negativeOrganizationBuckets)
	require.Zero(t, negativeAllocationBuckets)

	var duplicateFinalExecutions, duplicateConsumptionExecutions int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT execution_ref_id
			FROM organization_final_execution_evidences
			WHERE organization_id = $1
			GROUP BY execution_ref_id
			HAVING COUNT(*) > 1
		) AS duplicates
	`, fixture.organization.ID).Scan(&duplicateFinalExecutions))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT posting.execution_ref_id
			FROM organization_ledger_postings AS posting
			JOIN organization_ledger_operations AS operation ON operation.id = posting.operation_ref_id
			WHERE posting.organization_id = $1 AND operation.operation_type = $2
			GROUP BY posting.execution_ref_id
			HAVING COUNT(*) > 1
		) AS duplicates
	`, fixture.organization.ID, domain.OrganizationFundingOperationConsumption).Scan(&duplicateConsumptionExecutions))
	require.Zero(t, duplicateFinalExecutions)
	require.Zero(t, duplicateConsumptionExecutions)
}

func cleanupOrganizationRolloutInvariantFixture(t *testing.T, organizationID, creatorID, accountID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	_, err = tx.ExecContext(ctx, "SET LOCAL lock_timeout = '5s'")
	require.NoError(t, err)

	immutableTables := []string{
		"organization_attempt_account_effects",
		"organization_attempt_account_postings",
		"organization_execution_incidents",
		"organization_execution_reconciliation_audits",
		"organization_execution_outboxes",
		"organization_final_execution_evidences",
		"organization_attempt_evidences",
		"organization_execution_attempts",
		"organization_reservations",
		"organization_executions",
		"organization_ledger_postings",
		"organization_ledger_operations",
	}
	for _, table := range immutableTables {
		_, err := tx.ExecContext(ctx, "ALTER TABLE "+table+" DISABLE TRIGGER USER")
		require.NoError(t, err)
	}

	deleteStatements := []string{
		"DELETE FROM organization_execution_reconciliation_audits WHERE organization_id = $1",
		"DELETE FROM organization_execution_incidents WHERE organization_id = $1",
		"DELETE FROM organization_execution_outboxes WHERE organization_id = $1",
		"DELETE FROM organization_attempt_account_effects WHERE organization_id = $1",
		"DELETE FROM organization_attempt_account_postings WHERE organization_id = $1",
		"DELETE FROM organization_ledger_postings WHERE organization_id = $1",
		"DELETE FROM organization_ledger_operations WHERE organization_id = $1",
		"DELETE FROM organization_final_execution_evidences WHERE organization_id = $1",
		"DELETE FROM organization_attempt_evidences WHERE organization_id = $1",
		"DELETE FROM organization_execution_attempts WHERE organization_id = $1",
		"DELETE FROM organization_reservations WHERE organization_id = $1",
		"DELETE FROM organization_executions WHERE organization_id = $1",
		"DELETE FROM api_keys WHERE organization_id = $1",
		"DELETE FROM organization_allocations WHERE organization_id = $1",
		"DELETE FROM users WHERE organization_id = $1",
		"DELETE FROM organizations WHERE id = $1",
	}
	for _, statement := range deleteStatements {
		_, err := tx.ExecContext(ctx, statement, organizationID)
		require.NoError(t, err)
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM users WHERE id = $1", creatorID)
	require.NoError(t, err)
	if accountID > 0 {
		_, err = tx.ExecContext(ctx, "DELETE FROM accounts WHERE id = $1", accountID)
		require.NoError(t, err)
	}
	for index := len(immutableTables) - 1; index >= 0; index-- {
		_, err = tx.ExecContext(ctx, "ALTER TABLE "+immutableTables[index]+" ENABLE TRIGGER USER")
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
	committed = true
}
