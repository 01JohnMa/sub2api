package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// organizationExecutionRepository owns the database transaction for the
// bounded, multi-attempt organization settlement flow. It deliberately uses
// raw SQL for the lock order and for rows not yet represented by generated Ent
// code during this additive migration.
type organizationExecutionRepository struct {
	client *dbent.Client
}

func NewOrganizationExecutionRepository(client *dbent.Client) service.OrganizationExecutionRepository {
	return &organizationExecutionRepository{client: client}
}

func (r *organizationExecutionRepository) Reserve(ctx context.Context, command service.OrganizationExecutionReservationCommand) (*service.OrganizationExecutionReservationResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := service.ValidateOrganizationExecutionReservationCommand(command); err != nil {
		return nil, err
	}

	return withExecutionTransaction(r, ctx, "reserve organization execution", func(txCtx context.Context, client *dbent.Client) (*service.OrganizationExecutionReservationResult, error) {
		return reserveOrganizationExecution(txCtx, client, command)
	})
}

func (r *organizationExecutionRepository) RecordAttemptEvidence(ctx context.Context, command service.OrganizationAttemptEvidenceCommand) (*service.OrganizationAttemptEvidenceResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := service.ValidateOrganizationAttemptEvidenceCommand(command); err != nil {
		return nil, err
	}

	return withExecutionTransaction(r, ctx, "record organization attempt evidence", func(txCtx context.Context, client *dbent.Client) (*service.OrganizationAttemptEvidenceResult, error) {
		return recordOrganizationAttemptEvidence(txCtx, client, command)
	})
}

func (r *organizationExecutionRepository) Terminalize(ctx context.Context, command service.OrganizationTerminalizationCommand) (*service.OrganizationFinalExecutionEvidenceResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := service.ValidateOrganizationTerminalizationCommand(command); err != nil {
		return nil, err
	}

	return withExecutionTransaction(r, ctx, "terminalize organization execution", func(txCtx context.Context, client *dbent.Client) (*service.OrganizationFinalExecutionEvidenceResult, error) {
		return terminalizeOrganizationExecution(txCtx, client, command)
	})
}

func (r *organizationExecutionRepository) RecordPendingAttempt(ctx context.Context, command service.OrganizationPendingAttemptCommand) error {
	if r == nil || r.client == nil {
		return service.ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := service.ValidateOrganizationPendingAttemptCommand(command); err != nil {
		return err
	}
	_, err := withExecutionTransaction(r, ctx, "record pending organization attempt", func(txCtx context.Context, client *dbent.Client) (struct{}, error) {
		return struct{}{}, recordPendingOrganizationAttempt(txCtx, client, command)
	})
	return err
}

func (r *organizationExecutionRepository) ReconcilePendingExecution(ctx context.Context, command service.OrganizationPendingReconciliationCommand) (*service.OrganizationFinalExecutionEvidenceResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationExecutionRepositoryUnavailable
	}
	return withExecutionTransaction(r, ctx, "reconcile pending organization execution", func(txCtx context.Context, client *dbent.Client) (*service.OrganizationFinalExecutionEvidenceResult, error) {
		return reconcilePendingOrganizationExecution(txCtx, client, command)
	})
}

func (r *organizationExecutionRepository) RedriveAttemptAccountEffect(ctx context.Context, attemptID string) (*service.OrganizationAttemptAccountEffectResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationExecutionRepositoryUnavailable
	}
	return withExecutionTransaction(r, ctx, "redrive organization attempt Account effect", func(txCtx context.Context, client *dbent.Client) (*service.OrganizationAttemptAccountEffectResult, error) {
		return redriveOrganizationAttemptAccountEffect(txCtx, client, strings.TrimSpace(attemptID))
	})
}

func (r *organizationExecutionRepository) RedrivePendingAttemptAccountEffects(ctx context.Context, limit int) ([]service.OrganizationAttemptAccountEffectResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationExecutionRepositoryUnavailable
	}
	if limit <= 0 || limit > 1000 {
		return nil, service.ErrOrganizationExecutionInvalid
	}
	return withExecutionTransaction(r, ctx, "redrive pending organization attempt Account effects", func(txCtx context.Context, client *dbent.Client) ([]service.OrganizationAttemptAccountEffectResult, error) {
		return redrivePendingOrganizationAttemptAccountEffects(txCtx, client, limit)
	})
}

func (r *organizationExecutionRepository) RedriveExecutionOutbox(ctx context.Context, outboxID int64) (*service.OrganizationExecutionOutboxApplyResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationExecutionRepositoryUnavailable
	}
	if outboxID <= 0 {
		return nil, service.ErrOrganizationExecutionInvalid
	}
	return withExecutionTransaction(r, ctx, "redrive organization execution outbox", func(txCtx context.Context, client *dbent.Client) (*service.OrganizationExecutionOutboxApplyResult, error) {
		return redriveOrganizationExecutionOutbox(txCtx, client, outboxID)
	})
}

func withExecutionTransaction[T any](
	r *organizationExecutionRepository,
	ctx context.Context,
	operation string,
	fn func(context.Context, *dbent.Client) (T, error),
) (T, error) {
	var zero T
	if existing := dbent.TxFromContext(ctx); existing != nil {
		return fn(ctx, existing.Client())
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return zero, executionStorageError("begin "+operation, err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := fn(dbent.NewTxContext(ctx, tx), tx.Client())
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, executionStorageError("commit "+operation, err)
	}
	return result, nil
}

type organizationExecutionRecord struct {
	ID                   int64
	ExecutionID          string
	CorrelationID        string
	OrganizationID       int64
	AllocationIDSnapshot sql.NullInt64
	AllocationRefID      sql.NullInt64
	UserIDSnapshot       int64
	UserRefID            sql.NullInt64
	APIKeyIDSnapshot     int64
	APIKeyRefID          sql.NullInt64
	EndpointFamily       string
	RequestShape         string
	BillingFingerprint   string
	CandidateSnapshot    string
	CandidateSetHash     string
	PricingSnapshot      string
	PricingSnapshotHash  string
	CandidateCeiling     string
	Status               string
	AcceptedAt           time.Time
	TerminalizedAt       sql.NullTime
}

type organizationReservationRecord struct {
	ID                   int64
	ExecutionRefID       int64
	ExecutionIDSnapshot  string
	OrganizationID       int64
	AllocationIDSnapshot int64
	AllocationRefID      sql.NullInt64
	AllocationVersion    int64
	OrganizationVersion  int64
	PricingSnapshotHash  string
	ReservedAmount       string
	SettledAmount        string
	ReleasedAmount       string
	Status               string
}

type organizationAllocationRecord struct {
	ID             int64
	OrganizationID int64
	UserSnapshot   int64
	UserRefID      sql.NullInt64
	Status         string
	Available      string
	Reserved       string
	Version        int64
}

type organizationExecutionAttemptRecord struct {
	ID                    int64
	AttemptID             string
	ExecutionID           string
	ExecutionRefID        int64
	OrganizationID        int64
	AttemptSequence       int
	AccountID             sql.NullInt64
	AccountRefID          sql.NullInt64
	RouteSnapshotJSON     string
	RouteSnapshotHash     string
	PricingSnapshotJSON   string
	PricingSnapshotHash   string
	ProviderCorrelationID string
	ProviderRequestID     string
	Status                string
}

type organizationExecutionAttemptInsert struct {
	AttemptID             string
	ExecutionID           string
	ExecutionRefID        int64
	OrganizationID        int64
	AttemptSequence       int
	AccountID             int64
	AccountRefID          *int64
	RouteSnapshotJSON     string
	RouteSnapshotHash     string
	PricingSnapshotJSON   string
	PricingSnapshotHash   string
	ProviderCorrelationID string
	ProviderRequestID     string
	Status                string
	DispatchedAt          time.Time
}

type organizationFinalEvidenceRecord struct {
	ID                     int64
	ExecutionRefID         int64
	ExecutionID            string
	OrganizationID         int64
	AllocationIDSnapshot   sql.NullInt64
	UserIDSnapshot         int64
	APIKeyIDSnapshot       int64
	FinalAttemptID         sql.NullString
	FinalAttemptEvidenceID sql.NullInt64
	TerminalOutcome        string
	BusinessLimited        bool
	RawActualCost          string
	SettledCost            string
	ZeroCostProof          sql.NullString
	OccurredAt             time.Time
	ReconciledAt           sql.NullTime
}

func reserveOrganizationExecution(ctx context.Context, client *dbent.Client, command service.OrganizationExecutionReservationCommand) (*service.OrganizationExecutionReservationResult, error) {
	organizationStatus, _, err := lockExecutionOrganization(ctx, client, command.Attribution.OrganizationID)
	if err != nil {
		return nil, executionStorageError("lock organization for reservation", err)
	}

	userCurrent, userOrganizationID, userDeleted, err := lockExecutionUser(ctx, client, command.Attribution.UserID)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("lock execution user", err)
	}
	keyCurrent, keyUserID, keyOrganizationID, keyStatus, keyDeleted, keyExpired, err := lockExecutionAPIKey(ctx, client, command.Attribution.APIKeyID)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("lock execution API key", err)
	}
	allocation, err := lockExecutionAllocation(ctx, client, command.Attribution.AllocationID)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("lock execution allocation", err)
	}
	// Check the execution after the shared organization and exclusive
	// User/Key/allocation locks. Concurrent retries now serialize through this
	// point: a later retry sees the committed row instead of racing the unique
	// execution ID insert.
	existing, err := loadExecutionByUUID(ctx, client, command.ExecutionID, true)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("load execution replay", err)
	}
	if existing != nil {
		return replayOrganizationReservation(ctx, client, command, existing)
	}

	stale := organizationStatus != service.OrganizationStatusActive ||
		!userCurrent || userDeleted || !userOrganizationID.Valid || userOrganizationID.Int64 != command.Attribution.OrganizationID ||
		!keyCurrent || keyDeleted || keyExpired || keyUserID != command.Attribution.UserID || !keyOrganizationID.Valid || keyOrganizationID.Int64 != command.Attribution.OrganizationID || keyStatus != service.StatusAPIKeyActive ||
		allocation == nil || allocation.OrganizationID != command.Attribution.OrganizationID || allocation.UserSnapshot != command.Attribution.UserID ||
		!allocation.UserRefID.Valid || allocation.UserRefID.Int64 != command.Attribution.UserID || allocation.Status != domain.OrganizationAllocationStatusActive ||
		allocation.Version != command.Attribution.AllocationVersionSnapshot

	if stale {
		execution, err := insertOrganizationExecution(ctx, client, command, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		return preDispatchRejectedExecution(ctx, client, command, execution, "stale_authoritative_attribution")
	}

	reservedAmount, err := parseExecutionMoney(command.CandidateRouteMaximumCost)
	if err != nil {
		return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	available, err := parseExecutionMoney(allocation.Available)
	if err != nil {
		return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	reserved, err := parseExecutionMoney(allocation.Reserved)
	if err != nil {
		return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	if available.LessThan(reservedAmount) {
		execution, err := insertOrganizationExecution(ctx, client, command, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		return preDispatchRejectedExecution(ctx, client, command, execution, "organization_allocation_insufficient")
	}

	allocationID := allocation.ID
	userID := command.Attribution.UserID
	keyID := command.Attribution.APIKeyID
	execution, err := insertOrganizationExecution(ctx, client, command, &allocationID, &userID, &keyID)
	if err != nil {
		return nil, err
	}

	reservation, err := insertOrganizationReservation(ctx, client, command, execution, allocationID)
	if err != nil {
		return nil, err
	}

	availableAfter := available.Sub(reservedAmount)
	reservedAfter := reserved.Add(reservedAmount)
	versionAfter := allocation.Version + 1
	operation, err := insertExecutionLedgerOperation(ctx, client, execution, domain.OrganizationFundingOperationReservation, command.AcceptedAt, executionLedgerHash(execution.ExecutionID, "reservation"), "execution_reservation")
	if err != nil {
		return nil, err
	}
	if err := insertExecutionLedgerPosting(ctx, client, executionLedgerPostingInput{
		OperationID:    operation.ID,
		OperationUUID:  operation.OperationID,
		OrganizationID: command.Attribution.OrganizationID,
		Sequence:       1,
		BucketType:     domain.OrganizationLedgerBucketAllocationAvailable,
		BucketID:       allocationID,
		AllocationID:   allocationID,
		TargetUserID:   &userID,
		ExecutionID:    command.ExecutionID,
		ExecutionRefID: execution.ID,
		Before:         available,
		Delta:          reservedAmount.Neg(),
		After:          availableAfter,
		VersionBefore:  allocation.Version,
		VersionAfter:   versionAfter,
		OccurredAt:     command.AcceptedAt,
	}); err != nil {
		return nil, err
	}
	if err := insertExecutionLedgerPosting(ctx, client, executionLedgerPostingInput{
		OperationID:    operation.ID,
		OperationUUID:  operation.OperationID,
		OrganizationID: command.Attribution.OrganizationID,
		Sequence:       2,
		BucketType:     domain.OrganizationLedgerBucketAllocationReserved,
		BucketID:       allocationID,
		AllocationID:   allocationID,
		TargetUserID:   &userID,
		ExecutionID:    command.ExecutionID,
		ExecutionRefID: execution.ID,
		Before:         reserved,
		Delta:          reservedAmount,
		After:          reservedAfter,
		VersionBefore:  allocation.Version,
		VersionAfter:   versionAfter,
		OccurredAt:     command.AcceptedAt,
	}); err != nil {
		return nil, err
	}
	if err := updateExecutionAllocation(ctx, client, allocation, availableAfter, reservedAfter, versionAfter); err != nil {
		return nil, err
	}
	updateResult, err := client.ExecContext(ctx, `
		UPDATE organization_executions
		SET status = $1, updated_at = NOW()
		WHERE id = $2 AND status = $3
	`, service.OrganizationExecutionStateReserved, execution.ID, service.OrganizationExecutionStateAccepted)
	if err != nil {
		return nil, executionStorageError("mark execution reserved", err)
	}
	if err := requireSingleExecutionMutation("mark execution reserved", updateResult); err != nil {
		return nil, err
	}

	return &service.OrganizationExecutionReservationResult{
		ExecutionID:    command.ExecutionID,
		CorrelationID:  command.CorrelationID,
		OrganizationID: command.Attribution.OrganizationID,
		UserID:         command.Attribution.UserID,
		APIKeyID:       command.Attribution.APIKeyID,
		AllocationID:   command.Attribution.AllocationID,
		ReservationID:  reservation.ID,
		ReservedAmount: fixedExecutionMoney(reservedAmount),
		State:          service.OrganizationReservationStateReserved,
		Disposition:    service.OrganizationExecutionReservationDispositionReserved,
	}, nil
}

func replayOrganizationReservation(ctx context.Context, client *dbent.Client, command service.OrganizationExecutionReservationCommand, execution *organizationExecutionRecord) (*service.OrganizationExecutionReservationResult, error) {
	if !sameOrganizationExecutionCommand(execution, command) {
		return nil, service.ErrOrganizationExecutionConflict
	}
	final, err := loadFinalEvidenceByExecution(ctx, client, execution.ID, true)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("load execution final evidence", err)
	}
	if final != nil {
		reservation, reservationErr := loadReservationByExecution(ctx, client, execution.ID, true)
		if reservationErr != nil && reservationErr != sql.ErrNoRows {
			return nil, executionStorageError("load finalized reservation", reservationErr)
		}
		finalResult, err := organizationFinalEvidenceResult(ctx, client, execution, reservation, final, true)
		if err != nil {
			return nil, err
		}
		if reservation == nil {
			return &service.OrganizationExecutionReservationResult{
				ExecutionID:    command.ExecutionID,
				CorrelationID:  command.CorrelationID,
				OrganizationID: command.Attribution.OrganizationID,
				UserID:         command.Attribution.UserID,
				APIKeyID:       command.Attribution.APIKeyID,
				AllocationID:   command.Attribution.AllocationID,
				Disposition:    service.OrganizationExecutionReservationDispositionPreDispatchRejected,
				FinalEvidence:  finalResult,
				Replayed:       true,
			}, nil
		}
		return &service.OrganizationExecutionReservationResult{
			ExecutionID:    command.ExecutionID,
			CorrelationID:  command.CorrelationID,
			OrganizationID: command.Attribution.OrganizationID,
			UserID:         command.Attribution.UserID,
			APIKeyID:       command.Attribution.APIKeyID,
			AllocationID:   command.Attribution.AllocationID,
			ReservationID:  reservation.ID,
			ReservedAmount: reservation.ReservedAmount,
			State:          reservation.Status,
			Disposition:    service.OrganizationExecutionReservationDispositionFinalized,
			FinalEvidence:  finalResult,
			Replayed:       true,
		}, nil
	}

	reservation, err := loadReservationByExecution(ctx, client, execution.ID, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrOrganizationExecutionInvariant
		}
		return nil, executionStorageError("load reservation replay", err)
	}
	if execution.Status != service.OrganizationExecutionStateReserved || reservation.Status != service.OrganizationReservationStateReserved {
		// An evidence-bearing but non-terminal execution needs the later recovery
		// ticket. Returning a conflict is safer than dispatching it a second time.
		return nil, service.ErrOrganizationExecutionConflict
	}
	return &service.OrganizationExecutionReservationResult{
		ExecutionID:    command.ExecutionID,
		CorrelationID:  command.CorrelationID,
		OrganizationID: command.Attribution.OrganizationID,
		UserID:         command.Attribution.UserID,
		APIKeyID:       command.Attribution.APIKeyID,
		AllocationID:   command.Attribution.AllocationID,
		ReservationID:  reservation.ID,
		ReservedAmount: reservation.ReservedAmount,
		State:          service.OrganizationReservationStateReserved,
		Disposition:    service.OrganizationExecutionReservationDispositionReserved,
		Replayed:       true,
	}, nil
}

func preDispatchRejectedExecution(ctx context.Context, client *dbent.Client, command service.OrganizationExecutionReservationCommand, execution *organizationExecutionRecord, reason string) (*service.OrganizationExecutionReservationResult, error) {
	occurredAt := time.Now().UTC()
	proof, err := json.Marshal(map[string]string{"reason": reason})
	if err != nil {
		return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	var finalID int64
	err = scanExecutionOne(ctx, client, `
		INSERT INTO organization_final_execution_evidences (
			execution_id_snapshot, execution_ref_id, organization_id,
			allocation_id_snapshot, allocation_ref_id,
			user_id_snapshot, user_ref_id, api_key_id_snapshot, api_key_ref_id,
			endpoint_family, request_shape, raw_actual_cost,
			organization_settled_cost, terminal_outcome, is_business_limited,
			zero_cost_proof, occurred_at
		) VALUES (
			$1, $2, $3, $4, NULL,
			$5, NULL, $6, NULL,
			$7, $8, 0,
			0, $9, FALSE, $10, $11
		)
		RETURNING id
	`, []any{
		command.ExecutionID, execution.ID, command.Attribution.OrganizationID, command.Attribution.AllocationID,
		command.Attribution.UserID, command.Attribution.APIKeyID,
		command.EndpointFamily, command.RequestShape, service.OrganizationExecutionTerminalOutcomeError, proof, occurredAt,
	}, &finalID)
	if err != nil {
		return nil, executionStorageError("insert stale final evidence", err)
	}
	updateResult, err := client.ExecContext(ctx, `
		UPDATE organization_executions
		SET status = $1, terminalized_at = $2, updated_at = NOW()
		WHERE id = $3 AND status = $4
	`, service.OrganizationExecutionStateReleased, occurredAt, execution.ID, service.OrganizationExecutionStateAccepted)
	if err != nil {
		return nil, executionStorageError("terminalize stale execution", err)
	}
	if err := requireSingleExecutionMutation("terminalize stale execution", updateResult); err != nil {
		return nil, err
	}
	if err := insertExecutionOutboxes(ctx, client, execution, finalID, service.OrganizationExecutionTerminalOutcomeError, occurredAt); err != nil {
		return nil, err
	}
	final := &organizationFinalEvidenceRecord{
		ID:                   finalID,
		ExecutionRefID:       execution.ID,
		ExecutionID:          command.ExecutionID,
		OrganizationID:       command.Attribution.OrganizationID,
		AllocationIDSnapshot: sql.NullInt64{Int64: command.Attribution.AllocationID, Valid: true},
		UserIDSnapshot:       command.Attribution.UserID,
		APIKeyIDSnapshot:     command.Attribution.APIKeyID,
		TerminalOutcome:      service.OrganizationExecutionTerminalOutcomeError,
		RawActualCost:        "0",
		SettledCost:          "0.00000000",
		ZeroCostProof:        sql.NullString{String: string(proof), Valid: true},
		OccurredAt:           occurredAt,
	}
	finalResult, err := organizationFinalEvidenceResult(ctx, client, execution, nil, final, false)
	if err != nil {
		return nil, err
	}
	return &service.OrganizationExecutionReservationResult{
		ExecutionID:    command.ExecutionID,
		CorrelationID:  command.CorrelationID,
		OrganizationID: command.Attribution.OrganizationID,
		UserID:         command.Attribution.UserID,
		APIKeyID:       command.Attribution.APIKeyID,
		AllocationID:   command.Attribution.AllocationID,
		Disposition:    service.OrganizationExecutionReservationDispositionPreDispatchRejected,
		FinalEvidence:  finalResult,
	}, nil
}

func recordOrganizationAttemptEvidence(ctx context.Context, client *dbent.Client, command service.OrganizationAttemptEvidenceCommand) (*service.OrganizationAttemptEvidenceResult, error) {
	allocation, reservation, execution, err := lockExecutionAndReservationForAttempt(ctx, client, command.AllocationID, command.ExecutionID, command.ReservationID)
	if err != nil {
		return nil, err
	}
	if !attemptAllocationMatchesCommand(allocation, command) || !attemptCommandMatchesReservation(command, execution, reservation) {
		return nil, service.ErrOrganizationExecutionConflict
	}
	existing, err := loadAttemptEvidenceByIdentity(ctx, client, command.AttemptID, command.Provenance, command.ProvenanceHash, true)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("load attempt evidence replay", err)
	}
	if existing != nil {
		if existing.ExecutionRefID != execution.ID || !sameAttemptEvidenceCommand(existing, command) {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return &service.OrganizationAttemptEvidenceResult{AttemptEvidenceID: existing.ID, ExecutionID: command.ExecutionID, AttemptID: command.AttemptID, Replayed: true}, nil
	}
	if reservation.Status != service.OrganizationReservationStateReserved && reservation.Status != service.OrganizationReservationStatePendingReconciliation {
		return nil, service.ErrOrganizationExecutionConflict
	}
	if execution.Status != service.OrganizationExecutionStateReserved && execution.Status != service.OrganizationExecutionStateDispatched && execution.Status != service.OrganizationExecutionStateFinalUsageObserved && execution.Status != service.OrganizationExecutionStatePendingReconciliation {
		return nil, service.ErrOrganizationExecutionConflict
	}

	accountRefID, err := currentExecutionAccountRef(ctx, client, command.AccountID)
	if err != nil {
		return nil, err
	}
	attempt, err := loadExecutionAttemptByUUID(ctx, client, command.AttemptID, true)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("load execution attempt", err)
	}
	if attempt == nil {
		attempt, err = insertOrganizationExecutionAttempt(ctx, client, organizationExecutionAttemptInsert{
			AttemptID:             command.AttemptID,
			ExecutionID:           command.ExecutionID,
			ExecutionRefID:        execution.ID,
			OrganizationID:        command.OrganizationID,
			AttemptSequence:       command.AttemptSequence,
			AccountID:             command.AccountID,
			AccountRefID:          accountRefID,
			RouteSnapshotJSON:     command.RouteSnapshotJSON,
			RouteSnapshotHash:     command.RouteSnapshotHash,
			PricingSnapshotJSON:   command.PricingSnapshotJSON,
			PricingSnapshotHash:   command.PricingSnapshotHash,
			ProviderCorrelationID: command.ProviderCorrelationID,
			ProviderRequestID:     command.ProviderRequestID,
			Status:                service.OrganizationExecutionStateFinalUsageObserved,
			DispatchedAt:          command.IngestedAt,
		})
		if err != nil {
			return nil, err
		}
	} else if !attemptMatchesEvidenceCommand(attempt, command, execution.ID) {
		return nil, service.ErrOrganizationExecutionConflict
	}

	var evidenceID int64
	err = scanExecutionOne(ctx, client, `
		INSERT INTO organization_attempt_evidences (
			attempt_id_snapshot, attempt_ref_id, execution_id_snapshot, execution_ref_id,
			organization_id, account_id_snapshot, account_ref_id,
			route_snapshot, route_snapshot_hash, pricing_snapshot, pricing_snapshot_hash,
				provider_correlation_id, provider_request_id, evidence_source, terminal_outcome, provenance_hash,
				evidence, evidence_hash, input_tokens, output_tokens, cache_creation_tokens,
			cache_read_tokens, raw_actual_cost, provider_observed_at, ingested_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7,
			$8::jsonb, $9, $10::jsonb, $11,
				$12, $13, $14, $15, $16,
				$17::jsonb, $18, $19, $20, $21,
				$22, $23, $24, $25
		)
		RETURNING id
	`, []any{
		command.AttemptID, attempt.ID, command.ExecutionID, execution.ID,
		command.OrganizationID, command.AccountID, accountRefID,
		command.RouteSnapshotJSON, command.RouteSnapshotHash, command.PricingSnapshotJSON, command.PricingSnapshotHash,
		command.ProviderCorrelationID, command.ProviderRequestID, command.Provenance, command.TerminalOutcome, command.ProvenanceHash,
		command.EvidenceJSON, command.EvidenceHash, command.InputTokens, command.OutputTokens, command.CacheCreationTokens,
		command.CacheReadTokens, command.RawActualCost, command.ProviderObservedAt, command.IngestedAt,
	}, &evidenceID)
	if err != nil {
		return nil, executionStorageError("insert execution attempt evidence", err)
	}
	if err := ensureOrganizationAttemptAccountEffect(ctx, client, attempt, execution, command.AccountID, accountRefID); err != nil {
		return nil, err
	}
	if execution.Status != service.OrganizationExecutionStatePendingReconciliation {
		if execution.Status == service.OrganizationExecutionStateReserved {
			updateResult, err := client.ExecContext(ctx, `
				UPDATE organization_executions
				SET status = $1, updated_at = NOW()
				WHERE id = $2 AND status = $3
			`, service.OrganizationExecutionStateDispatched, execution.ID, service.OrganizationExecutionStateReserved)
			if err != nil {
				return nil, executionStorageError("mark execution dispatched", err)
			}
			if err := requireSingleExecutionMutation("mark execution dispatched", updateResult); err != nil {
				return nil, err
			}
		}
		if execution.Status == service.OrganizationExecutionStateReserved || execution.Status == service.OrganizationExecutionStateDispatched {
			updateResult, err := client.ExecContext(ctx, `
				UPDATE organization_executions
				SET status = $1, updated_at = NOW()
				WHERE id = $2 AND status IN ($3, $4)
			`, service.OrganizationExecutionStateFinalUsageObserved, execution.ID,
				service.OrganizationExecutionStateReserved, service.OrganizationExecutionStateDispatched)
			if err != nil {
				return nil, executionStorageError("mark execution final usage observed", err)
			}
			if err := requireSingleExecutionMutation("mark execution final usage observed", updateResult); err != nil {
				return nil, err
			}
		}
	}
	return &service.OrganizationAttemptEvidenceResult{AttemptEvidenceID: evidenceID, ExecutionID: command.ExecutionID, AttemptID: command.AttemptID}, nil
}

func recordPendingOrganizationAttempt(ctx context.Context, client *dbent.Client, command service.OrganizationPendingAttemptCommand) error {
	allocation, reservation, execution, err := lockExecutionAndReservationForAttempt(ctx, client, command.AllocationID, command.ExecutionID, command.ReservationID)
	if err != nil {
		return err
	}
	if allocation == nil || allocation.ID != command.AllocationID || allocation.OrganizationID != command.OrganizationID || allocation.UserSnapshot != command.UserID ||
		reservation.ID != command.ReservationID || reservation.ExecutionRefID != execution.ID || reservation.OrganizationID != command.OrganizationID ||
		execution.ExecutionID != command.ExecutionID || execution.CorrelationID != command.CorrelationID || execution.UserIDSnapshot != command.UserID || execution.APIKeyIDSnapshot != command.APIKeyID {
		return service.ErrOrganizationExecutionConflict
	}
	existing, err := loadExecutionAttemptByUUID(ctx, client, command.AttemptID, true)
	if err != nil && err != sql.ErrNoRows {
		return executionStorageError("load pending execution attempt replay", err)
	}
	accountRefID, err := currentExecutionAccountRef(ctx, client, command.AccountID)
	if err != nil {
		return err
	}
	if existing == nil {
		_, err = insertOrganizationExecutionAttempt(ctx, client, organizationExecutionAttemptInsert{
			AttemptID:             command.AttemptID,
			ExecutionID:           command.ExecutionID,
			ExecutionRefID:        execution.ID,
			OrganizationID:        command.OrganizationID,
			AttemptSequence:       command.AttemptSequence,
			AccountID:             command.AccountID,
			AccountRefID:          accountRefID,
			RouteSnapshotJSON:     command.RouteSnapshotJSON,
			RouteSnapshotHash:     command.RouteSnapshotHash,
			PricingSnapshotJSON:   command.PricingSnapshotJSON,
			PricingSnapshotHash:   command.PricingSnapshotHash,
			ProviderCorrelationID: command.ProviderCorrelationID,
			ProviderRequestID:     command.ProviderRequestID,
			Status:                service.OrganizationExecutionStateDispatched,
			DispatchedAt:          command.DispatchedAt,
		})
		if err != nil {
			return err
		}
	} else if !attemptMatchesPendingCommand(existing, command, execution.ID) {
		return service.ErrOrganizationExecutionConflict
	}
	if reservation.Status == service.OrganizationReservationStatePendingReconciliation && execution.Status == service.OrganizationExecutionStatePendingReconciliation {
		return nil
	}
	if reservation.Status != service.OrganizationReservationStateReserved ||
		(execution.Status != service.OrganizationExecutionStateReserved && execution.Status != service.OrganizationExecutionStateDispatched && execution.Status != service.OrganizationExecutionStateFinalUsageObserved) {
		return service.ErrOrganizationExecutionConflict
	}
	reservationResult, err := client.ExecContext(ctx, `
		UPDATE organization_reservations
		SET status = $1, updated_at = NOW()
		WHERE id = $2 AND status = $3
	`, service.OrganizationReservationStatePendingReconciliation, reservation.ID, service.OrganizationReservationStateReserved)
	if err != nil {
		return executionStorageError("mark reservation pending reconciliation", err)
	}
	if err := requireSingleExecutionMutation("mark reservation pending reconciliation", reservationResult); err != nil {
		return err
	}
	executionResult, err := client.ExecContext(ctx, `
		UPDATE organization_executions
		SET status = $1, updated_at = NOW()
		WHERE id = $2 AND status IN ($3, $4, $5)
	`, service.OrganizationExecutionStatePendingReconciliation, execution.ID,
		service.OrganizationExecutionStateReserved, service.OrganizationExecutionStateDispatched, service.OrganizationExecutionStateFinalUsageObserved)
	if err != nil {
		return executionStorageError("mark execution pending reconciliation", err)
	}
	if err := requireSingleExecutionMutation("mark execution pending reconciliation", executionResult); err != nil {
		return err
	}
	return nil
}

func attemptMatchesPendingCommand(attempt *organizationExecutionAttemptRecord, command service.OrganizationPendingAttemptCommand, executionRefID int64) bool {
	return attempt != nil && attempt.ExecutionRefID == executionRefID && attempt.ExecutionID == command.ExecutionID &&
		attempt.OrganizationID == command.OrganizationID && attempt.AttemptSequence == command.AttemptSequence &&
		attempt.AccountID.Valid && attempt.AccountID.Int64 == command.AccountID &&
		attempt.RouteSnapshotHash == command.RouteSnapshotHash && attempt.PricingSnapshotHash == command.PricingSnapshotHash &&
		attempt.ProviderRequestID == command.ProviderRequestID && attempt.ProviderCorrelationID == command.ProviderCorrelationID
}

type organizationAttemptEvidenceCandidate struct {
	ID                  int64
	AttemptID           string
	AttemptRefID        int64
	AttemptSequence     int
	ExecutionID         string
	ExecutionRefID      int64
	OrganizationID      int64
	AccountID           int64
	EvidenceSource      string
	ProvenanceHash      string
	EvidenceHash        string
	TerminalOutcome     string
	RawActualCost       string
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	ProviderObservedAt  time.Time
}

type organizationAttemptEvidenceSelection struct {
	Selected  *organizationAttemptEvidenceCandidate
	Conflicts []int64
}

func reconcilePendingOrganizationExecution(ctx context.Context, client *dbent.Client, command service.OrganizationPendingReconciliationCommand) (*service.OrganizationFinalExecutionEvidenceResult, error) {
	if !validOrganizationReconciliationCommand(command) {
		return nil, service.ErrOrganizationExecutionReconciliationForbidden
	}
	execution, err := loadExecutionByUUID(ctx, client, strings.TrimSpace(command.ExecutionID), false)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return nil, executionStorageError("load reconciliation execution", err)
	}
	reservation, err := loadReservationByExecution(ctx, client, execution.ID, false)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return nil, executionStorageError("load reconciliation reservation", err)
	}
	allocation, err := lockExecutionAllocation(ctx, client, reservation.AllocationIDSnapshot)
	if err != nil {
		return nil, executionStorageError("lock reconciliation allocation", err)
	}
	reservation, err = loadReservationByID(ctx, client, reservation.ID, true)
	if err != nil {
		return nil, executionStorageError("lock reconciliation reservation", err)
	}
	execution, err = loadExecutionByUUID(ctx, client, execution.ExecutionID, true)
	if err != nil {
		return nil, executionStorageError("lock reconciliation execution", err)
	}
	existingAudit, err := loadOrganizationExecutionReconciliationAuditByScopedKey(ctx, client, execution.OrganizationID, command.Actor.AuthorizationScope, command.IdempotencyKeyHash)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if existingAudit != nil && (existingAudit.ExecutionRefID != execution.ID || existingAudit.CanonicalPayloadHash != command.CanonicalPayloadHash) {
		return nil, service.ErrIdempotencyKeyConflict
	}
	final, err := loadFinalEvidenceByExecution(ctx, client, execution.ID, true)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("load reconciliation final replay", err)
	}
	if final != nil {
		if existingAudit == nil {
			return nil, service.ErrIdempotencyKeyConflict
		}
		return organizationFinalEvidenceResult(ctx, client, execution, reservation, final, true)
	}
	if existingAudit != nil {
		return nil, service.ErrOrganizationExecutionInvariant
	}
	if allocation == nil || reservation.Status != service.OrganizationReservationStatePendingReconciliation || execution.Status != service.OrganizationExecutionStatePendingReconciliation {
		return nil, service.ErrOrganizationExecutionConflict
	}
	candidates, err := loadOrganizationAttemptEvidenceCandidates(ctx, client, execution.ID, nil, true)
	if err != nil {
		return nil, err
	}
	selections := selectOrganizationAttemptEvidence(candidates)
	if len(selections) == 0 {
		return nil, service.ErrOrganizationExecutionPendingReconciliation
	}
	var finalSelection *organizationAttemptEvidenceCandidate
	sequences := make([]int, 0, len(selections))
	for sequence := range selections {
		sequences = append(sequences, sequence)
	}
	sort.Ints(sequences)
	for _, sequence := range sequences {
		selection := selections[sequence]
		if len(selection.Conflicts) > 0 {
			if err := persistProvenanceConflict(ctx, client, execution, selection.Selected, selection.Conflicts, command.ReconciledAt); err != nil {
				return nil, err
			}
			return nil, service.ErrOrganizationExecutionPendingReconciliation
		}
		if selection.Selected != nil {
			finalSelection = selection.Selected
		}
	}
	if finalSelection == nil || finalSelection.TerminalOutcome == service.OrganizationAttemptEvidenceTerminalOutcomeUnknown {
		return nil, service.ErrOrganizationExecutionPendingReconciliation
	}
	occurredAt := finalSelection.ProviderObservedAt.UTC()
	if occurredAt.Before(execution.AcceptedAt.UTC()) {
		occurredAt = execution.AcceptedAt.UTC()
	}
	reconciledAt := command.ReconciledAt.UTC()
	result, err := terminalizeOrganizationExecution(ctx, client, service.OrganizationTerminalizationCommand{
		ExecutionID:            execution.ExecutionID,
		CorrelationID:          execution.CorrelationID,
		OrganizationID:         execution.OrganizationID,
		UserID:                 execution.UserIDSnapshot,
		APIKeyID:               execution.APIKeyIDSnapshot,
		AllocationID:           reservation.AllocationIDSnapshot,
		ReservationID:          reservation.ID,
		FinalAttemptID:         pointerRepositoryString(finalSelection.AttemptID),
		FinalAttemptEvidenceID: pointerRepositoryInt64(finalSelection.ID),
		TerminalOutcome:        finalSelection.TerminalOutcome,
		OccurredAt:             occurredAt,
		ReconciledAt:           &reconciledAt,
	})
	if err != nil {
		return nil, err
	}
	if err := insertOrganizationExecutionReconciliationAudit(ctx, client, execution, reservation, finalSelection, command, result, reconciledAt); err != nil {
		return nil, err
	}
	return result, nil
}

func insertOrganizationExecutionReconciliationAudit(
	ctx context.Context,
	client *dbent.Client,
	execution *organizationExecutionRecord,
	reservation *organizationReservationRecord,
	evidence *organizationAttemptEvidenceCandidate,
	command service.OrganizationPendingReconciliationCommand,
	final *service.OrganizationFinalExecutionEvidenceResult,
	reconciledAt time.Time,
) error {
	if execution == nil || reservation == nil || evidence == nil || final == nil {
		return service.ErrOrganizationExecutionInvariant
	}
	action := final.ReservationState
	if action != service.OrganizationReservationStateSettled && action != service.OrganizationReservationStateReleased {
		return service.ErrOrganizationExecutionInvariant
	}
	actorRefID, err := currentExecutionUserRef(ctx, client, command.Actor.UserID)
	if err != nil {
		return err
	}
	internalReason := "pending execution resolved from immutable " + evidence.EvidenceSource + " evidence using fixed provenance precedence"
	writeResult, err := client.ExecContext(ctx, `
		INSERT INTO organization_execution_reconciliation_audits (
			operation_id, correlation_id, idempotency_key_hash, canonical_payload_hash,
			execution_id_snapshot, execution_ref_id, organization_id,
			final_evidence_id_snapshot, final_evidence_ref_id,
			selected_attempt_id_snapshot, selected_attempt_ref_id,
			selected_evidence_id_snapshot, selected_evidence_ref_id,
			actor_id_snapshot, actor_ref_id, actor_display_snapshot,
			actor_role_snapshot, authorization_scope, action,
			raw_actual_cost, organization_settled_cost, reservation_amount,
			reservation_status_before, reservation_status_after,
			public_reason, internal_reason, reconciled_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $8, $9, $10, $11, $11,
			$12, $13, $14, $15, $16, $17, $18, $19, $20, $21,
			$22, $23, $24, $25
		)
		ON CONFLICT DO NOTHING
	`, command.OperationID, command.CorrelationID, command.IdempotencyKeyHash, command.CanonicalPayloadHash,
		execution.ExecutionID, execution.ID, execution.OrganizationID,
		final.FinalEvidenceID, evidence.AttemptID, evidence.AttemptRefID, evidence.ID,
		command.Actor.UserID, actorRefID, strings.TrimSpace(command.Actor.DisplayName), command.Actor.PlatformRole,
		command.Actor.AuthorizationScope, action, final.RawActualCost, final.OrganizationSettledCost, reservation.ReservedAmount,
		service.OrganizationReservationStatePendingReconciliation, final.ReservationState,
		"pending execution resolved from trusted evidence", internalReason, reconciledAt.UTC())
	if err != nil {
		return executionStorageError("insert execution reconciliation audit", err)
	}
	rowsAffected, err := writeResult.RowsAffected()
	if err != nil {
		return executionStorageError("inspect execution reconciliation audit write", err)
	}
	if rowsAffected == 0 {
		existing, loadErr := loadOrganizationExecutionReconciliationAuditByScopedKey(ctx, client, execution.OrganizationID, command.Actor.AuthorizationScope, command.IdempotencyKeyHash)
		if loadErr != nil && loadErr != sql.ErrNoRows {
			return loadErr
		}
		if existing == nil || existing.ExecutionRefID != execution.ID || existing.CanonicalPayloadHash != command.CanonicalPayloadHash {
			return service.ErrIdempotencyKeyConflict
		}
	}
	return nil
}

func validOrganizationReconciliationCommand(command service.OrganizationPendingReconciliationCommand) bool {
	return strings.TrimSpace(command.ExecutionID) != "" && validOrganizationExecutionRepositoryUUID(command.OperationID) && validOrganizationExecutionRepositoryUUID(command.CorrelationID) &&
		validOrganizationExecutionRepositoryHash(command.IdempotencyKeyHash) && validOrganizationExecutionRepositoryHash(command.CanonicalPayloadHash) &&
		!command.ReconciledAt.IsZero() && command.Actor.UserID > 0 && command.Actor.Active &&
		command.Actor.AuthorizationScope == domain.OrganizationFundingAuthorizationPlatformAdminJWT && command.Actor.PlatformRole == domain.RoleAdmin && strings.TrimSpace(command.Actor.DisplayName) != ""
}

type organizationExecutionReconciliationAuditIdentity struct {
	ExecutionRefID       int64
	CanonicalPayloadHash string
}

func loadOrganizationExecutionReconciliationAuditByScopedKey(
	ctx context.Context,
	client *dbent.Client,
	organizationID int64,
	authorizationScope string,
	idempotencyKeyHash string,
) (*organizationExecutionReconciliationAuditIdentity, error) {
	record := &organizationExecutionReconciliationAuditIdentity{}
	err := scanExecutionOne(ctx, client, `
		SELECT execution_ref_id, canonical_payload_hash
		FROM organization_execution_reconciliation_audits
		WHERE organization_id = $1 AND authorization_scope = $2 AND idempotency_key_hash = $3
	`, []any{organizationID, authorizationScope, idempotencyKeyHash}, &record.ExecutionRefID, &record.CanonicalPayloadHash)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func validOrganizationExecutionRepositoryUUID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}

func validOrganizationExecutionRepositoryHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func loadOrganizationAttemptEvidenceCandidates(ctx context.Context, client *dbent.Client, executionRefID int64, attemptRefID *int64, forUpdate bool) ([]organizationAttemptEvidenceCandidate, error) {
	query := `
		SELECT evidence.id, attempt.attempt_id, attempt.id, attempt.attempt_sequence,
			evidence.execution_id_snapshot, evidence.execution_ref_id, evidence.organization_id,
			evidence.account_id_snapshot, evidence.evidence_source, evidence.provenance_hash,
			evidence.evidence_hash, evidence.terminal_outcome, evidence.raw_actual_cost::text,
			evidence.input_tokens, evidence.output_tokens, evidence.cache_creation_tokens,
			evidence.cache_read_tokens, evidence.provider_observed_at
		FROM organization_attempt_evidences AS evidence
		JOIN organization_execution_attempts AS attempt ON attempt.id = evidence.attempt_ref_id
		WHERE evidence.execution_ref_id = $1`
	args := []any{executionRefID}
	if attemptRefID != nil {
		query += " AND evidence.attempt_ref_id = $2"
		args = append(args, *attemptRefID)
	}
	query += " ORDER BY attempt.attempt_sequence, evidence.ingested_at, evidence.id"
	if forUpdate {
		query += " FOR UPDATE OF evidence, attempt"
	}
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, executionStorageError("load attempt evidence candidates", err)
	}
	defer func() { _ = rows.Close() }()
	var candidates []organizationAttemptEvidenceCandidate
	for rows.Next() {
		var candidate organizationAttemptEvidenceCandidate
		if err := rows.Scan(
			&candidate.ID, &candidate.AttemptID, &candidate.AttemptRefID, &candidate.AttemptSequence,
			&candidate.ExecutionID, &candidate.ExecutionRefID, &candidate.OrganizationID,
			&candidate.AccountID, &candidate.EvidenceSource, &candidate.ProvenanceHash,
			&candidate.EvidenceHash, &candidate.TerminalOutcome, &candidate.RawActualCost,
			&candidate.InputTokens, &candidate.OutputTokens, &candidate.CacheCreationTokens,
			&candidate.CacheReadTokens, &candidate.ProviderObservedAt,
		); err != nil {
			return nil, executionStorageError("scan attempt evidence candidates", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, executionStorageError("iterate attempt evidence candidates", err)
	}
	return candidates, nil
}

func selectOrganizationAttemptEvidence(candidates []organizationAttemptEvidenceCandidate) map[int]organizationAttemptEvidenceSelection {
	grouped := make(map[int][]organizationAttemptEvidenceCandidate)
	for _, candidate := range candidates {
		grouped[candidate.AttemptSequence] = append(grouped[candidate.AttemptSequence], candidate)
	}
	selections := make(map[int]organizationAttemptEvidenceSelection, len(grouped))
	for sequence, group := range grouped {
		sort.SliceStable(group, func(i, j int) bool {
			return organizationEvidencePrecedence(group[i].EvidenceSource) > organizationEvidencePrecedence(group[j].EvidenceSource)
		})
		selection := organizationAttemptEvidenceSelection{}
		if len(group) > 0 {
			selected := group[0]
			selection.Selected = &selected
			for i := 1; i < len(group); i++ {
				if !sameOrganizationAttemptUsage(selected, group[i]) {
					selection.Conflicts = append(selection.Conflicts, group[i].ID)
				}
			}
		}
		selections[sequence] = selection
	}
	return selections
}

func organizationEvidencePrecedence(source string) int {
	switch source {
	case service.OrganizationAttemptEvidenceProvenanceAdapterDirect:
		return 2
	case service.OrganizationAttemptEvidenceProvenanceProviderQuery:
		return 1
	default:
		return 0
	}
}

func sameOrganizationAttemptUsage(left, right organizationAttemptEvidenceCandidate) bool {
	leftCost, leftErr := decimal.NewFromString(left.RawActualCost)
	rightCost, rightErr := decimal.NewFromString(right.RawActualCost)
	return leftErr == nil && rightErr == nil && leftCost.Equal(rightCost) &&
		left.TerminalOutcome == right.TerminalOutcome && left.InputTokens == right.InputTokens && left.OutputTokens == right.OutputTokens &&
		left.CacheCreationTokens == right.CacheCreationTokens && left.CacheReadTokens == right.CacheReadTokens
}

func persistProvenanceConflict(ctx context.Context, client *dbent.Client, execution *organizationExecutionRecord, selected *organizationAttemptEvidenceCandidate, conflicts []int64, occurredAt time.Time) error {
	if execution == nil || selected == nil {
		return service.ErrOrganizationExecutionInvariant
	}
	details, err := json.Marshal(map[string]any{
		"selected_evidence_id":     selected.ID,
		"conflicting_evidence_ids": conflicts,
	})
	if err != nil {
		return service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	if _, err := client.ExecContext(ctx, `
		INSERT INTO organization_execution_incidents (
			execution_id_snapshot, execution_ref_id, organization_id,
			attempt_id_snapshot, attempt_ref_id, incident_type,
			public_reason, internal_reason, details, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10)
		ON CONFLICT (execution_ref_id, incident_type, attempt_id_snapshot) DO NOTHING
	`, execution.ExecutionID, execution.ID, execution.OrganizationID, selected.AttemptID, selected.AttemptRefID,
		service.OrganizationExecutionIncidentProvenanceConflict,
		"provider evidence requires review", "immutable evidence sources disagree on cost, tokens, or terminal outcome", string(details), occurredAt.UTC()); err != nil {
		return executionStorageError("insert provenance conflict incident", err)
	}
	if _, err := client.ExecContext(ctx, `
		UPDATE organization_attempt_account_effects
		SET status = $1, audit_reason = $2, applied_at = $3, updated_at = NOW()
		WHERE attempt_ref_id = $4 AND status = $5
	`, service.OrganizationAttemptAccountEffectStateOperatorAttention,
		"provenance conflict prevents deterministic Account posting", occurredAt.UTC(), selected.AttemptRefID, service.OrganizationAttemptAccountEffectStatePending); err != nil {
		return executionStorageError("mark provenance conflict Account effect", err)
	}
	return nil
}

type organizationAttemptAccountEffectRecord struct {
	ID                  int64
	AttemptID           string
	AttemptRefID        int64
	ExecutionID         string
	ExecutionRefID      int64
	OrganizationID      int64
	AccountID           int64
	AccountRefID        sql.NullInt64
	Status              string
	SelectedEvidenceID  sql.NullInt64
	PostingID           sql.NullInt64
	RawActualCost       sql.NullString
	PlatformOverageCost sql.NullString
	ReservationCeiling  string
}

func redriveOrganizationAttemptAccountEffect(ctx context.Context, client *dbent.Client, attemptID string) (*service.OrganizationAttemptAccountEffectResult, error) {
	effectSnapshot, err := loadOrganizationAttemptAccountEffect(ctx, client, attemptID, false)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return nil, executionStorageError("load attempt Account effect", err)
	}
	reservation, err := loadReservationByExecution(ctx, client, effectSnapshot.ExecutionRefID, false)
	if err != nil {
		return nil, executionStorageError("load Account effect reservation", err)
	}
	if _, err := lockExecutionAllocation(ctx, client, reservation.AllocationIDSnapshot); err != nil {
		return nil, executionStorageError("lock Account effect allocation", err)
	}
	if _, err := loadReservationByID(ctx, client, reservation.ID, true); err != nil {
		return nil, executionStorageError("lock Account effect reservation", err)
	}
	execution, err := loadExecutionByUUID(ctx, client, effectSnapshot.ExecutionID, true)
	if err != nil {
		return nil, executionStorageError("lock Account effect execution", err)
	}
	effect, err := loadOrganizationAttemptAccountEffect(ctx, client, attemptID, true)
	if err != nil {
		return nil, executionStorageError("lock attempt Account effect", err)
	}
	if effect.Status != service.OrganizationAttemptAccountEffectStatePending {
		return organizationAttemptAccountEffectResult(effect, true), nil
	}
	candidates, err := loadOrganizationAttemptEvidenceCandidates(ctx, client, effect.ExecutionRefID, &effect.AttemptRefID, true)
	if err != nil {
		return nil, err
	}
	selection := selectOrganizationAttemptEvidence(candidates)
	selected := selectionForAttemptRef(selection, candidates, effect.AttemptRefID)
	if selected.Selected == nil {
		return nil, service.ErrOrganizationExecutionPendingReconciliation
	}
	if len(selected.Conflicts) > 0 {
		if err := persistProvenanceConflict(ctx, client, execution, selected.Selected, selected.Conflicts, time.Now().UTC()); err != nil {
			return nil, err
		}
		effect.Status = service.OrganizationAttemptAccountEffectStateOperatorAttention
		return organizationAttemptAccountEffectResult(effect, false), nil
	}
	rawCost, err := decimal.NewFromString(selected.Selected.RawActualCost)
	if err != nil || rawCost.IsNegative() {
		return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	ceiling, err := parseExecutionMoney(effect.ReservationCeiling)
	if err != nil {
		return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	overage := decimal.Zero
	classification := "normal"
	if rawCost.GreaterThan(ceiling) {
		overage = rawCost.Sub(ceiling)
		classification = "platform_overage"
	}
	accountRefID, err := currentExecutionAccountRef(ctx, client, effect.AccountID)
	if err != nil {
		return nil, err
	}
	postingID, err := insertOrganizationAttemptAccountPosting(ctx, client, effect, selected.Selected, accountRefID, rawCost, ceiling, overage, classification)
	if err != nil {
		return nil, err
	}
	if overage.GreaterThan(decimal.Zero) {
		if err := persistPlatformOverageIncident(ctx, client, execution, selected.Selected, rawCost, ceiling, overage); err != nil {
			return nil, err
		}
	}
	status := service.OrganizationAttemptAccountEffectStateApplied
	deletedTarget := accountRefID == nil
	if deletedTarget {
		status = service.OrganizationAttemptAccountEffectStateDeletedTargetNoop
	} else if err := incrementOrganizationAttemptAccountQuota(ctx, client, effect.AccountID, rawCost); err != nil {
		return nil, err
	}
	appliedAt := time.Now().UTC()
	auditReason := "Account cost posting and live quota update applied"
	if deletedTarget {
		auditReason = "Account target was deleted; immutable cost posting retained and live quota update skipped"
	}
	result, err := client.ExecContext(ctx, `
		UPDATE organization_attempt_account_effects
		SET selected_evidence_id_snapshot = $1, selected_evidence_ref_id = $1,
			posting_id_snapshot = $2, posting_ref_id = $2,
			raw_actual_cost = $3, platform_overage_cost = $4,
			status = $5, audit_reason = $6, applied_at = $7, updated_at = NOW()
		WHERE id = $8 AND status = $9
	`, selected.Selected.ID, postingID, rawCost.String(), overage.String(), status, auditReason, appliedAt, effect.ID, service.OrganizationAttemptAccountEffectStatePending)
	if err != nil {
		return nil, executionStorageError("apply attempt Account effect", err)
	}
	if err := requireSingleExecutionMutation("apply attempt Account effect", result); err != nil {
		return nil, err
	}
	effect.Status = status
	effect.SelectedEvidenceID = sql.NullInt64{Int64: selected.Selected.ID, Valid: true}
	effect.PostingID = sql.NullInt64{Int64: postingID, Valid: true}
	effect.RawActualCost = sql.NullString{String: rawCost.String(), Valid: true}
	effect.PlatformOverageCost = sql.NullString{String: overage.String(), Valid: true}
	return organizationAttemptAccountEffectResult(effect, false), nil
}

func selectionForAttemptRef(selections map[int]organizationAttemptEvidenceSelection, candidates []organizationAttemptEvidenceCandidate, attemptRefID int64) organizationAttemptEvidenceSelection {
	for _, candidate := range candidates {
		if candidate.AttemptRefID == attemptRefID {
			return selections[candidate.AttemptSequence]
		}
	}
	return organizationAttemptEvidenceSelection{}
}

func loadOrganizationAttemptAccountEffect(ctx context.Context, client *dbent.Client, attemptID string, forUpdate bool) (*organizationAttemptAccountEffectRecord, error) {
	query := `
		SELECT effect.id, effect.attempt_id_snapshot, effect.attempt_ref_id,
			effect.execution_id_snapshot, effect.execution_ref_id, effect.organization_id,
			effect.account_id_snapshot, effect.account_ref_id, effect.status,
			effect.selected_evidence_id_snapshot, effect.posting_id_snapshot,
			effect.raw_actual_cost::text, effect.platform_overage_cost::text,
			reservation.reserved_amount::text
		FROM organization_attempt_account_effects AS effect
		JOIN organization_reservations AS reservation ON reservation.execution_ref_id = effect.execution_ref_id
		WHERE effect.attempt_id_snapshot = $1`
	if forUpdate {
		query += " FOR UPDATE OF effect"
	}
	record := &organizationAttemptAccountEffectRecord{}
	err := scanExecutionOne(ctx, client, query, []any{attemptID},
		&record.ID, &record.AttemptID, &record.AttemptRefID,
		&record.ExecutionID, &record.ExecutionRefID, &record.OrganizationID,
		&record.AccountID, &record.AccountRefID, &record.Status,
		&record.SelectedEvidenceID, &record.PostingID,
		&record.RawActualCost, &record.PlatformOverageCost, &record.ReservationCeiling,
	)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func insertOrganizationAttemptAccountPosting(ctx context.Context, client *dbent.Client, effect *organizationAttemptAccountEffectRecord, evidence *organizationAttemptEvidenceCandidate, accountRefID *int64, rawCost, ceiling, overage decimal.Decimal, classification string) (int64, error) {
	var postingID int64
	err := scanExecutionOne(ctx, client, `
		INSERT INTO organization_attempt_account_postings (
			attempt_id_snapshot, attempt_ref_id, evidence_id_snapshot, evidence_ref_id,
			execution_id_snapshot, execution_ref_id, organization_id,
			account_id_snapshot, account_ref_id, raw_actual_cost,
			reservation_ceiling, platform_overage_cost, classification, occurred_at
		) VALUES ($1, $2, $3, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (attempt_ref_id) DO NOTHING
		RETURNING id
	`, []any{
		effect.AttemptID, effect.AttemptRefID, evidence.ID,
		effect.ExecutionID, effect.ExecutionRefID, effect.OrganizationID,
		effect.AccountID, accountRefID, rawCost.String(), fixedExecutionMoney(ceiling), overage.String(), classification, evidence.ProviderObservedAt.UTC(),
	}, &postingID)
	if err == sql.ErrNoRows {
		if err := scanExecutionOne(ctx, client, `SELECT id FROM organization_attempt_account_postings WHERE attempt_ref_id = $1`, []any{effect.AttemptRefID}, &postingID); err != nil {
			return 0, executionStorageError("load replayed attempt Account posting", err)
		}
		return postingID, nil
	}
	if err != nil {
		return 0, executionStorageError("insert attempt Account posting", err)
	}
	return postingID, nil
}

func persistPlatformOverageIncident(ctx context.Context, client *dbent.Client, execution *organizationExecutionRecord, evidence *organizationAttemptEvidenceCandidate, rawCost, ceiling, overage decimal.Decimal) error {
	details, err := json.Marshal(map[string]string{
		"raw_actual_cost":       rawCost.String(),
		"reservation_ceiling":   fixedExecutionMoney(ceiling),
		"platform_overage_cost": overage.String(),
	})
	if err != nil {
		return service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	if _, err := client.ExecContext(ctx, `
		INSERT INTO organization_execution_incidents (
			execution_id_snapshot, execution_ref_id, organization_id,
			attempt_id_snapshot, attempt_ref_id, incident_type,
			public_reason, internal_reason, details, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10)
		ON CONFLICT (execution_ref_id, incident_type, attempt_id_snapshot) DO NOTHING
	`, execution.ExecutionID, execution.ID, execution.OrganizationID, evidence.AttemptID, evidence.AttemptRefID,
		service.OrganizationExecutionIncidentPlatformOverage,
		"provider cost exceeded the reserved ceiling", "organization settlement was capped; the difference remains platform cost", string(details), time.Now().UTC()); err != nil {
		return executionStorageError("insert platform overage incident", err)
	}
	return nil
}

func incrementOrganizationAttemptAccountQuota(ctx context.Context, client *dbent.Client, accountID int64, rawCost decimal.Decimal) error {
	result, err := client.ExecContext(ctx, `
		UPDATE accounts
		SET extra = COALESCE(extra, '{}'::jsonb)
			|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1::numeric),
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`, rawCost.String(), accountID)
	if err != nil {
		return executionStorageError("increment attempt Account quota", err)
	}
	if err := requireSingleExecutionMutation("increment attempt Account quota", result); err != nil {
		return err
	}
	return nil
}

func organizationAttemptAccountEffectResult(effect *organizationAttemptAccountEffectRecord, replayed bool) *service.OrganizationAttemptAccountEffectResult {
	result := &service.OrganizationAttemptAccountEffectResult{
		EffectID:          effect.ID,
		AttemptID:         effect.AttemptID,
		ExecutionID:       effect.ExecutionID,
		AccountID:         effect.AccountID,
		Status:            effect.Status,
		DeletedTargetNoop: effect.Status == service.OrganizationAttemptAccountEffectStateDeletedTargetNoop,
		Replayed:          replayed,
	}
	if effect.PostingID.Valid {
		value := effect.PostingID.Int64
		result.PostingID = &value
	}
	if effect.RawActualCost.Valid {
		result.RawActualCost = effect.RawActualCost.String
	}
	if effect.PlatformOverageCost.Valid {
		result.PlatformOverageCost = effect.PlatformOverageCost.String
	}
	return result
}

func redrivePendingOrganizationAttemptAccountEffects(ctx context.Context, client *dbent.Client, limit int) ([]service.OrganizationAttemptAccountEffectResult, error) {
	if limit <= 0 {
		return nil, service.ErrOrganizationExecutionInvalid
	}
	rows, err := client.QueryContext(ctx, `
		SELECT attempt_id_snapshot
		FROM organization_attempt_account_effects
		WHERE status = $1
		ORDER BY created_at, id
		LIMIT $2
	`, service.OrganizationAttemptAccountEffectStatePending, limit)
	if err != nil {
		return nil, executionStorageError("claim pending attempt Account effects", err)
	}
	var attemptIDs []string
	for rows.Next() {
		var attemptID string
		if err := rows.Scan(&attemptID); err != nil {
			_ = rows.Close()
			return nil, executionStorageError("scan pending attempt Account effect", err)
		}
		attemptIDs = append(attemptIDs, attemptID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, executionStorageError("iterate pending attempt Account effects", err)
	}
	if err := rows.Close(); err != nil {
		return nil, executionStorageError("close pending attempt Account effects", err)
	}
	results := make([]service.OrganizationAttemptAccountEffectResult, 0, len(attemptIDs))
	for _, attemptID := range attemptIDs {
		result, err := redriveOrganizationAttemptAccountEffect(ctx, client, attemptID)
		if err != nil {
			return nil, err
		}
		results = append(results, *result)
	}
	return results, nil
}

type organizationExecutionOutboxRecord struct {
	ID             int64
	ExecutionID    string
	EventType      string
	Status         string
	UserID         int64
	APIKeyID       int64
	EndpointFamily string
	SettledCost    string
	OccurredAt     time.Time
}

func redriveOrganizationExecutionOutbox(ctx context.Context, client *dbent.Client, outboxID int64) (*service.OrganizationExecutionOutboxApplyResult, error) {
	outbox, err := loadOrganizationExecutionOutbox(ctx, client, outboxID, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return nil, executionStorageError("load execution outbox", err)
	}
	if outbox.Status != service.OrganizationAttemptAccountEffectStatePending {
		return organizationExecutionOutboxApplyResult(outbox, true), nil
	}
	settledCost, err := decimal.NewFromString(outbox.SettledCost)
	if err != nil || settledCost.IsNegative() {
		return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	deletedTarget := false
	switch outbox.EventType {
	case "api_key_quota":
		var id int64
		err := scanExecutionOne(ctx, client, `SELECT id FROM api_keys WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, []any{outbox.APIKeyID}, &id)
		if err == sql.ErrNoRows {
			deletedTarget = true
		} else if err != nil {
			return nil, executionStorageError("lock execution outbox API key", err)
		} else if settledCost.GreaterThan(decimal.Zero) {
			if _, err := client.ExecContext(ctx, `UPDATE api_keys SET quota_used = quota_used + $1::numeric, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL`, fixedExecutionMoney(settledCost), outbox.APIKeyID); err != nil {
				return nil, executionStorageError("apply execution API key quota", err)
			}
		}
	case "user_platform_quota":
		var id int64
		err := scanExecutionOne(ctx, client, `SELECT id FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, []any{outbox.UserID}, &id)
		if err == sql.ErrNoRows {
			deletedTarget = true
		} else if err != nil {
			return nil, executionStorageError("lock execution outbox user", err)
		} else if settledCost.GreaterThan(decimal.Zero) {
			platform, ok := organizationQuotaPlatform(outbox.EndpointFamily)
			if !ok {
				return nil, service.ErrOrganizationExecutionInvariant
			}
			cost, _ := settledCost.Float64()
			if err := NewUserPlatformQuotaRepository(client).IncrementUsageWithReset(ctx, outbox.UserID, platform, cost, outbox.OccurredAt.UTC()); err != nil {
				return nil, executionStorageError("apply execution user platform quota", err)
			}
		}
	default:
		return nil, service.ErrOrganizationExecutionInvalid
	}
	status := service.OrganizationAttemptAccountEffectStateApplied
	if deletedTarget {
		status = service.OrganizationAttemptAccountEffectStateDeletedTargetNoop
	}
	result, err := client.ExecContext(ctx, `
		UPDATE organization_execution_outboxes
		SET status = $1, applied_at = $2, updated_at = NOW()
		WHERE id = $3 AND status = $4
	`, status, time.Now().UTC(), outbox.ID, service.OrganizationAttemptAccountEffectStatePending)
	if err != nil {
		return nil, executionStorageError("apply execution outbox", err)
	}
	if err := requireSingleExecutionMutation("apply execution outbox", result); err != nil {
		return nil, err
	}
	outbox.Status = status
	return organizationExecutionOutboxApplyResult(outbox, false), nil
}

func loadOrganizationExecutionOutbox(ctx context.Context, client *dbent.Client, outboxID int64, forUpdate bool) (*organizationExecutionOutboxRecord, error) {
	query := `
		SELECT outbox.id, outbox.execution_id_snapshot, outbox.event_type, outbox.status,
			final.user_id_snapshot, final.api_key_id_snapshot,
			execution.endpoint_family, final.organization_settled_cost::text, final.occurred_at
		FROM organization_execution_outboxes AS outbox
		JOIN organization_final_execution_evidences AS final ON final.id = outbox.final_evidence_id_snapshot
		JOIN organization_executions AS execution ON execution.id = outbox.execution_ref_id
		WHERE outbox.id = $1`
	if forUpdate {
		query += " FOR UPDATE OF outbox"
	}
	record := &organizationExecutionOutboxRecord{}
	err := scanExecutionOne(ctx, client, query, []any{outboxID},
		&record.ID, &record.ExecutionID, &record.EventType, &record.Status,
		&record.UserID, &record.APIKeyID, &record.EndpointFamily, &record.SettledCost, &record.OccurredAt,
	)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func organizationQuotaPlatform(endpointFamily string) (string, bool) {
	switch endpointFamily {
	case service.OrganizationEndpointFamilyMessages:
		return service.PlatformAnthropic, true
	case service.OrganizationEndpointFamilyResponses, service.OrganizationEndpointFamilyChat, service.OrganizationEndpointFamilyEmbeddings, service.OrganizationEndpointFamilyImages:
		return service.PlatformOpenAI, true
	case service.OrganizationEndpointFamilyGemini:
		return service.PlatformGemini, true
	case service.OrganizationEndpointFamilyAntigravity:
		return service.PlatformAntigravity, true
	default:
		return "", false
	}
}

func organizationExecutionOutboxApplyResult(outbox *organizationExecutionOutboxRecord, replayed bool) *service.OrganizationExecutionOutboxApplyResult {
	return &service.OrganizationExecutionOutboxApplyResult{
		OutboxID:          outbox.ID,
		ExecutionID:       outbox.ExecutionID,
		EventType:         outbox.EventType,
		Status:            outbox.Status,
		DeletedTargetNoop: outbox.Status == service.OrganizationAttemptAccountEffectStateDeletedTargetNoop,
		Replayed:          replayed,
	}
}

func pointerRepositoryString(value string) *string {
	return &value
}

func pointerRepositoryInt64(value int64) *int64 {
	return &value
}

func terminalizeOrganizationExecution(ctx context.Context, client *dbent.Client, command service.OrganizationTerminalizationCommand) (*service.OrganizationFinalExecutionEvidenceResult, error) {
	allocation, err := lockExecutionAllocation(ctx, client, command.AllocationID)
	if err != nil {
		return nil, executionStorageError("lock allocation for terminalization", err)
	}
	if allocation == nil {
		return nil, service.ErrOrganizationExecutionConflict
	}
	reservation, err := loadReservationByID(ctx, client, command.ReservationID, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return nil, executionStorageError("lock reservation for terminalization", err)
	}
	execution, err := loadExecutionByUUID(ctx, client, command.ExecutionID, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return nil, executionStorageError("lock execution for terminalization", err)
	}
	if !terminalizationMatches(command, allocation, reservation, execution) {
		return nil, service.ErrOrganizationExecutionConflict
	}

	final, err := loadFinalEvidenceByExecution(ctx, client, execution.ID, true)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("load terminal replay", err)
	}
	if final != nil {
		if !sameTerminalization(final, command) {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return organizationFinalEvidenceResult(ctx, client, execution, reservation, final, true)
	}
	if (reservation.Status != service.OrganizationReservationStateReserved && reservation.Status != service.OrganizationReservationStatePendingReconciliation) ||
		(execution.Status != service.OrganizationExecutionStateReserved && execution.Status != service.OrganizationExecutionStateFinalUsageObserved && execution.Status != service.OrganizationExecutionStatePendingReconciliation) {
		return nil, service.ErrOrganizationExecutionConflict
	}

	var evidence *organizationAttemptEvidenceRecord
	if command.FinalAttemptID != nil {
		evidence, err = loadFinalAttemptEvidence(ctx, client, *command.FinalAttemptID, *command.FinalAttemptEvidenceID, true)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, service.ErrOrganizationExecutionConflict
			}
			return nil, executionStorageError("lock final attempt evidence", err)
		}
		if evidence.ExecutionRefID != execution.ID || evidence.OrganizationID != command.OrganizationID || evidence.AttemptID != *command.FinalAttemptID ||
			(evidence.TerminalOutcome != service.OrganizationAttemptEvidenceTerminalOutcomeUnknown && evidence.TerminalOutcome != command.TerminalOutcome) {
			return nil, service.ErrOrganizationExecutionConflict
		}
	}

	reservedAmount, err := parseExecutionMoney(reservation.ReservedAmount)
	if err != nil {
		return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	allocationAvailable, err := parseExecutionMoney(allocation.Available)
	if err != nil {
		return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	allocationReserved, err := parseExecutionMoney(allocation.Reserved)
	if err != nil {
		return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	if allocationReserved.LessThan(reservedAmount) {
		return nil, service.ErrOrganizationExecutionInvariant
	}

	rawActualCost := decimal.Zero
	inputTokens, outputTokens := int64(0), int64(0)
	cacheCreationTokens, cacheReadTokens := int64(0), int64(0)
	userRefID, err := currentExecutionUserRef(ctx, client, command.UserID)
	if err != nil {
		return nil, err
	}
	apiKeyRefID, err := currentExecutionAPIKeyRef(ctx, client, command.APIKeyID)
	if err != nil {
		return nil, err
	}
	var finalAttemptRefID, finalAttemptEvidenceRefID, accountIDSnapshot, accountRefID *int64
	var finalAttemptUUID *string
	if evidence != nil {
		rawActualCost, err = decimal.NewFromString(evidence.RawActualCost)
		if err != nil || rawActualCost.IsNegative() {
			return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
		}
		inputTokens, outputTokens = evidence.InputTokens, evidence.OutputTokens
		cacheCreationTokens, cacheReadTokens = evidence.CacheCreation, evidence.CacheRead
		attemptID := evidence.AttemptID
		finalAttemptUUID = &attemptID
		attemptRef := evidence.AttemptRefID
		finalAttemptRefID = &attemptRef
		evidenceID := evidence.ID
		finalAttemptEvidenceRefID = &evidenceID
		if evidence.AccountID.Valid {
			value := evidence.AccountID.Int64
			accountIDSnapshot = &value
			accountRefID, err = currentExecutionAccountRef(ctx, client, value)
			if err != nil {
				return nil, err
			}
		}
	}
	settledAmount := decimal.Zero
	if evidence != nil {
		settledAmount = settleOrganizationExecutionCost(rawActualCost, reservedAmount)
	}
	releasedAmount := reservedAmount.Sub(settledAmount)
	terminalState := service.OrganizationReservationStateReleased
	if settledAmount.GreaterThan(decimal.Zero) {
		terminalState = service.OrganizationReservationStateSettled
	}

	var zeroProof any
	var zeroProofJSON string
	if evidence == nil {
		proof, err := json.Marshal(map[string]string{"reason": command.ZeroProof})
		if err != nil {
			return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
		}
		zeroProofJSON = string(proof)
		zeroProof = zeroProofJSON
	}
	var finalID int64
	err = scanExecutionOne(ctx, client, `
		INSERT INTO organization_final_execution_evidences (
			execution_id_snapshot, execution_ref_id, organization_id,
			allocation_id_snapshot, allocation_ref_id,
			user_id_snapshot, user_ref_id, api_key_id_snapshot, api_key_ref_id,
			final_attempt_id_snapshot, final_attempt_ref_id,
			final_attempt_evidence_id_snapshot, final_attempt_evidence_ref_id,
			account_id_snapshot, account_ref_id, endpoint_family, request_shape,
			input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
			raw_actual_cost, organization_settled_cost, terminal_outcome,
				is_business_limited, zero_cost_proof, occurred_at, reconciled_at
		) VALUES (
			$1, $2, $3,
			$4, $5,
			$6, $7, $8, $9,
			$10, $11,
			$12, $13,
			$14, $15, $16, $17,
			$18, $19, $20, $21,
			$22, $23, $24,
				$25, $26::jsonb, $27, $28
		)
		RETURNING id
	`, []any{
		execution.ExecutionID, execution.ID, command.OrganizationID,
		command.AllocationID, command.AllocationID,
		command.UserID, userRefID, command.APIKeyID, apiKeyRefID,
		finalAttemptUUID, finalAttemptRefID,
		command.FinalAttemptEvidenceID, finalAttemptEvidenceRefID,
		accountIDSnapshot, accountRefID, execution.EndpointFamily, execution.RequestShape,
		inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens,
		rawActualCost.String(), fixedExecutionMoney(settledAmount), command.TerminalOutcome,
		command.BusinessLimited, zeroProof, command.OccurredAt.UTC(), command.ReconciledAt,
	}, &finalID)
	if err != nil {
		return nil, executionStorageError("insert final execution evidence", err)
	}

	currentVersion := allocation.Version
	finalAvailable := allocationAvailable.Add(releasedAmount)
	finalReserved := allocationReserved.Sub(reservedAmount)
	if finalReserved.IsNegative() || finalAvailable.IsNegative() {
		return nil, service.ErrOrganizationExecutionInvariant
	}
	if settledAmount.GreaterThan(decimal.Zero) {
		consumptionVersion := currentVersion + 1
		operation, err := insertExecutionLedgerOperation(ctx, client, execution, domain.OrganizationFundingOperationConsumption, command.OccurredAt, executionLedgerHash(execution.ExecutionID, "consumption"), "execution_consumption")
		if err != nil {
			return nil, err
		}
		if err := insertExecutionLedgerPosting(ctx, client, executionLedgerPostingInput{
			OperationID:     operation.ID,
			OperationUUID:   operation.OperationID,
			OrganizationID:  command.OrganizationID,
			Sequence:        1,
			BucketType:      domain.OrganizationLedgerBucketAllocationReserved,
			BucketID:        allocation.ID,
			AllocationID:    allocation.ID,
			TargetUserID:    nullableCurrentUser(allocation.UserRefID),
			ExecutionID:     execution.ExecutionID,
			ExecutionRefID:  execution.ID,
			FinalEvidenceID: &finalID,
			Before:          allocationReserved,
			Delta:           settledAmount.Neg(),
			After:           allocationReserved.Sub(settledAmount),
			VersionBefore:   currentVersion,
			VersionAfter:    consumptionVersion,
			OccurredAt:      command.OccurredAt,
		}); err != nil {
			return nil, err
		}
		currentVersion = consumptionVersion
	}
	if releasedAmount.GreaterThan(decimal.Zero) {
		releaseVersion := currentVersion + 1
		operation, err := insertExecutionLedgerOperation(ctx, client, execution, domain.OrganizationFundingOperationRelease, command.OccurredAt, executionLedgerHash(execution.ExecutionID, "release"), "execution_reservation_release")
		if err != nil {
			return nil, err
		}
		reservedBeforeRelease := allocationReserved.Sub(settledAmount)
		if err := insertExecutionLedgerPosting(ctx, client, executionLedgerPostingInput{
			OperationID:    operation.ID,
			OperationUUID:  operation.OperationID,
			OrganizationID: command.OrganizationID,
			Sequence:       1,
			BucketType:     domain.OrganizationLedgerBucketAllocationReserved,
			BucketID:       allocation.ID,
			AllocationID:   allocation.ID,
			TargetUserID:   nullableCurrentUser(allocation.UserRefID),
			ExecutionID:    execution.ExecutionID,
			ExecutionRefID: execution.ID,
			Before:         reservedBeforeRelease,
			Delta:          releasedAmount.Neg(),
			After:          finalReserved,
			VersionBefore:  currentVersion,
			VersionAfter:   releaseVersion,
			OccurredAt:     command.OccurredAt,
		}); err != nil {
			return nil, err
		}
		if err := insertExecutionLedgerPosting(ctx, client, executionLedgerPostingInput{
			OperationID:    operation.ID,
			OperationUUID:  operation.OperationID,
			OrganizationID: command.OrganizationID,
			Sequence:       2,
			BucketType:     domain.OrganizationLedgerBucketAllocationAvailable,
			BucketID:       allocation.ID,
			AllocationID:   allocation.ID,
			TargetUserID:   nullableCurrentUser(allocation.UserRefID),
			ExecutionID:    execution.ExecutionID,
			ExecutionRefID: execution.ID,
			Before:         allocationAvailable,
			Delta:          releasedAmount,
			After:          finalAvailable,
			VersionBefore:  currentVersion,
			VersionAfter:   releaseVersion,
			OccurredAt:     command.OccurredAt,
		}); err != nil {
			return nil, err
		}
		currentVersion = releaseVersion
	}
	if err := updateExecutionAllocation(ctx, client, allocation, finalAvailable, finalReserved, currentVersion); err != nil {
		return nil, err
	}
	updateResult, err := client.ExecContext(ctx, `
		UPDATE organization_reservations
		SET status = $1, settled_amount = $2, released_amount = $3,
			terminalized_at = $4, updated_at = NOW()
		WHERE id = $5 AND status = $6
	`, terminalState, fixedExecutionMoney(settledAmount), fixedExecutionMoney(releasedAmount), command.OccurredAt.UTC(), reservation.ID, reservation.Status)
	if err != nil {
		return nil, executionStorageError("terminalize reservation", err)
	}
	if err := requireSingleExecutionMutation("terminalize reservation", updateResult); err != nil {
		return nil, err
	}
	executionTerminalState := service.OrganizationExecutionStateReleased
	if settledAmount.GreaterThan(decimal.Zero) {
		executionTerminalState = service.OrganizationExecutionStateSettled
	}
	updateResult, err = client.ExecContext(ctx, `
		UPDATE organization_executions
		SET status = $1, terminalized_at = $2, updated_at = NOW()
		WHERE id = $3 AND status IN ($4, $5)
	`, executionTerminalState, command.OccurredAt.UTC(), execution.ID, execution.Status, execution.Status)
	if err != nil {
		return nil, executionStorageError("terminalize execution", err)
	}
	if err := requireSingleExecutionMutation("terminalize execution", updateResult); err != nil {
		return nil, err
	}
	if err := insertExecutionOutboxes(ctx, client, execution, finalID, command.TerminalOutcome, command.OccurredAt); err != nil {
		return nil, err
	}

	final = &organizationFinalEvidenceRecord{
		ID:                   finalID,
		ExecutionRefID:       execution.ID,
		ExecutionID:          execution.ExecutionID,
		OrganizationID:       command.OrganizationID,
		AllocationIDSnapshot: sql.NullInt64{Int64: command.AllocationID, Valid: true},
		UserIDSnapshot:       command.UserID,
		APIKeyIDSnapshot:     command.APIKeyID,
		TerminalOutcome:      command.TerminalOutcome,
		BusinessLimited:      command.BusinessLimited,
		RawActualCost:        rawActualCost.String(),
		SettledCost:          fixedExecutionMoney(settledAmount),
		OccurredAt:           command.OccurredAt.UTC(),
	}
	if command.ReconciledAt != nil {
		final.ReconciledAt = sql.NullTime{Time: command.ReconciledAt.UTC(), Valid: true}
	}
	if zeroProofJSON != "" {
		final.ZeroCostProof = sql.NullString{String: zeroProofJSON, Valid: true}
	}
	if command.FinalAttemptID != nil {
		final.FinalAttemptID = sql.NullString{String: *command.FinalAttemptID, Valid: true}
		final.FinalAttemptEvidenceID = sql.NullInt64{Int64: *command.FinalAttemptEvidenceID, Valid: true}
	}
	reservation.Status = terminalState
	return organizationFinalEvidenceResult(ctx, client, execution, reservation, final, false)
}

type organizationAttemptEvidenceRecord struct {
	ID              int64
	AttemptID       string
	AttemptRefID    int64
	ExecutionRefID  int64
	OrganizationID  int64
	AccountID       sql.NullInt64
	EvidenceSource  string
	ProvenanceHash  string
	TerminalOutcome string
	RawActualCost   string
	InputTokens     int64
	OutputTokens    int64
	CacheCreation   int64
	CacheRead       int64
}

type organizationAttemptEvidenceReplayRecord struct {
	ID                  int64
	AttemptID           string
	ExecutionID         string
	ExecutionRefID      int64
	OrganizationID      int64
	AttemptSequence     int
	AccountID           sql.NullInt64
	RouteSnapshotHash   string
	PricingSnapshotHash string
	ProviderRequestID   string
	ProviderCorrelation string
	EvidenceSource      string
	TerminalOutcome     string
	ProvenanceHash      string
	EvidenceHash        string
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	RawActualCost       string
	ProviderObservedAt  time.Time
	IngestedAt          time.Time
}

func lockExecutionOrganization(ctx context.Context, client *dbent.Client, organizationID int64) (string, int64, error) {
	var status string
	var version int64
	err := scanExecutionOne(ctx, client, `SELECT status, balance_version FROM organizations WHERE id = $1 FOR SHARE`, []any{organizationID}, &status, &version)
	return status, version, err
}

func lockExecutionUser(ctx context.Context, client *dbent.Client, userID int64) (bool, sql.NullInt64, bool, error) {
	var id int64
	var organizationID sql.NullInt64
	var deletedAt sql.NullTime
	err := scanExecutionOne(ctx, client, `SELECT id, organization_id, deleted_at FROM users WHERE id = $1 FOR UPDATE`, []any{userID}, &id, &organizationID, &deletedAt)
	if err != nil {
		return false, sql.NullInt64{}, false, err
	}
	return id == userID, organizationID, deletedAt.Valid, nil
}

func lockExecutionAPIKey(ctx context.Context, client *dbent.Client, keyID int64) (bool, int64, sql.NullInt64, string, bool, bool, error) {
	var id, userID int64
	var organizationID sql.NullInt64
	var status string
	var deletedAt, expiresAt sql.NullTime
	err := scanExecutionOne(ctx, client, `SELECT id, user_id, organization_id, status, deleted_at, expires_at FROM api_keys WHERE id = $1 FOR UPDATE`, []any{keyID}, &id, &userID, &organizationID, &status, &deletedAt, &expiresAt)
	if err != nil {
		return false, 0, sql.NullInt64{}, "", false, false, err
	}
	return id == keyID, userID, organizationID, status, deletedAt.Valid, expiresAt.Valid && !expiresAt.Time.After(time.Now()), nil
}

func lockExecutionAllocation(ctx context.Context, client *dbent.Client, allocationID int64) (*organizationAllocationRecord, error) {
	record := &organizationAllocationRecord{}
	err := scanExecutionOne(ctx, client, `
		SELECT id, organization_id, user_id_snapshot, user_ref_id, status, available, reserved, version
		FROM organization_allocations WHERE id = $1 FOR UPDATE
	`, []any{allocationID}, &record.ID, &record.OrganizationID, &record.UserSnapshot, &record.UserRefID, &record.Status, &record.Available, &record.Reserved, &record.Version)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func currentExecutionUserRef(ctx context.Context, client *dbent.Client, userID int64) (*int64, error) {
	var id int64
	err := scanExecutionOne(ctx, client, `SELECT id FROM users WHERE id = $1 AND deleted_at IS NULL`, []any{userID}, &id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, executionStorageError("load final user reference", err)
	}
	return &id, nil
}

func currentExecutionAPIKeyRef(ctx context.Context, client *dbent.Client, keyID int64) (*int64, error) {
	var id int64
	err := scanExecutionOne(ctx, client, `SELECT id FROM api_keys WHERE id = $1 AND deleted_at IS NULL`, []any{keyID}, &id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, executionStorageError("load final API key reference", err)
	}
	return &id, nil
}

func currentExecutionAccountRef(ctx context.Context, client *dbent.Client, accountID int64) (*int64, error) {
	var id int64
	err := scanExecutionOne(ctx, client, `SELECT id FROM accounts WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, []any{accountID}, &id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, executionStorageError("load final account reference", err)
	}
	return &id, nil
}

func loadExecutionByUUID(ctx context.Context, client *dbent.Client, executionID string, forUpdate bool) (*organizationExecutionRecord, error) {
	query := `
		SELECT id, execution_id, correlation_id, organization_id,
			allocation_id_snapshot, allocation_ref_id, user_id_snapshot, user_ref_id,
			api_key_id_snapshot, api_key_ref_id, endpoint_family, request_shape,
			billing_fingerprint, candidate_snapshot::text, candidate_set_hash,
			pricing_snapshot::text, pricing_snapshot_hash, candidate_ceiling::text,
			status, accepted_at, terminalized_at
		FROM organization_executions WHERE execution_id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	record := &organizationExecutionRecord{}
	err := scanExecutionOne(ctx, client, query, []any{executionID},
		&record.ID, &record.ExecutionID, &record.CorrelationID, &record.OrganizationID,
		&record.AllocationIDSnapshot, &record.AllocationRefID, &record.UserIDSnapshot, &record.UserRefID,
		&record.APIKeyIDSnapshot, &record.APIKeyRefID, &record.EndpointFamily, &record.RequestShape,
		&record.BillingFingerprint, &record.CandidateSnapshot, &record.CandidateSetHash,
		&record.PricingSnapshot, &record.PricingSnapshotHash, &record.CandidateCeiling,
		&record.Status, &record.AcceptedAt, &record.TerminalizedAt,
	)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func loadReservationByExecution(ctx context.Context, client *dbent.Client, executionRefID int64, forUpdate bool) (*organizationReservationRecord, error) {
	query := `
		SELECT id, execution_ref_id, execution_id_snapshot, organization_id,
			allocation_id_snapshot, allocation_ref_id, allocation_version_snapshot,
			organization_balance_version_snapshot, pricing_snapshot_hash, reserved_amount::text,
			settled_amount::text, released_amount::text, status
		FROM organization_reservations WHERE execution_ref_id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	return scanOrganizationReservation(ctx, client, query, executionRefID)
}

func loadReservationByID(ctx context.Context, client *dbent.Client, reservationID int64, forUpdate bool) (*organizationReservationRecord, error) {
	query := `
		SELECT id, execution_ref_id, execution_id_snapshot, organization_id,
			allocation_id_snapshot, allocation_ref_id, allocation_version_snapshot,
			organization_balance_version_snapshot, pricing_snapshot_hash, reserved_amount::text,
			settled_amount::text, released_amount::text, status
		FROM organization_reservations WHERE id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	return scanOrganizationReservation(ctx, client, query, reservationID)
}

func scanOrganizationReservation(ctx context.Context, client *dbent.Client, query string, id int64) (*organizationReservationRecord, error) {
	record := &organizationReservationRecord{}
	err := scanExecutionOne(ctx, client, query, []any{id},
		&record.ID, &record.ExecutionRefID, &record.ExecutionIDSnapshot, &record.OrganizationID,
		&record.AllocationIDSnapshot, &record.AllocationRefID, &record.AllocationVersion,
		&record.OrganizationVersion, &record.PricingSnapshotHash, &record.ReservedAmount, &record.SettledAmount, &record.ReleasedAmount, &record.Status,
	)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func loadFinalEvidenceByExecution(ctx context.Context, client *dbent.Client, executionRefID int64, forUpdate bool) (*organizationFinalEvidenceRecord, error) {
	query := `
		SELECT id, execution_ref_id, execution_id_snapshot, organization_id,
			allocation_id_snapshot, user_id_snapshot, api_key_id_snapshot,
			final_attempt_id_snapshot, final_attempt_evidence_id_snapshot,
			terminal_outcome, is_business_limited, raw_actual_cost::text,
			organization_settled_cost::text, zero_cost_proof::text, occurred_at, reconciled_at
		FROM organization_final_execution_evidences WHERE execution_ref_id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	record := &organizationFinalEvidenceRecord{}
	err := scanExecutionOne(ctx, client, query, []any{executionRefID},
		&record.ID, &record.ExecutionRefID, &record.ExecutionID, &record.OrganizationID,
		&record.AllocationIDSnapshot, &record.UserIDSnapshot, &record.APIKeyIDSnapshot,
		&record.FinalAttemptID, &record.FinalAttemptEvidenceID,
		&record.TerminalOutcome, &record.BusinessLimited, &record.RawActualCost,
		&record.SettledCost, &record.ZeroCostProof, &record.OccurredAt, &record.ReconciledAt,
	)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func insertOrganizationExecution(ctx context.Context, client *dbent.Client, command service.OrganizationExecutionReservationCommand, allocationRefID, userRefID, keyRefID *int64) (*organizationExecutionRecord, error) {
	var id int64
	err := scanExecutionOne(ctx, client, `
		INSERT INTO organization_executions (
			execution_id, correlation_id, organization_id,
			allocation_id_snapshot, allocation_ref_id, user_id_snapshot, user_ref_id,
			api_key_id_snapshot, api_key_ref_id, endpoint_family, request_shape,
			client_request_id, billing_fingerprint, candidate_snapshot,
			candidate_set_hash, pricing_snapshot, pricing_snapshot_hash,
			candidate_ceiling, is_billable, status, accepted_at
		) VALUES (
			$1, $2, $3,
			$4, $5, $6, $7,
			$8, $9, $10, $11,
			$12, $13, $14::jsonb,
			$15, $16::jsonb, $17,
			$18, TRUE, $19, $20
		)
		RETURNING id
	`, []any{
		command.ExecutionID, command.CorrelationID, command.Attribution.OrganizationID,
		command.Attribution.AllocationID, allocationRefID, command.Attribution.UserID, userRefID,
		command.Attribution.APIKeyID, keyRefID, command.EndpointFamily, command.RequestShape,
		nullableExecutionString(command.ClientRequestID), command.BillingFingerprint, command.CandidateSnapshotJSON,
		command.CandidateSetHash, command.PricingSnapshotJSON, command.PricingSnapshotHash,
		command.CandidateRouteMaximumCost, service.OrganizationExecutionStateAccepted, command.AcceptedAt.UTC(),
	}, &id)
	if err != nil {
		return nil, executionStorageError("insert execution", err)
	}
	return loadExecutionByUUID(ctx, client, command.ExecutionID, false)
}

func insertOrganizationReservation(ctx context.Context, client *dbent.Client, command service.OrganizationExecutionReservationCommand, execution *organizationExecutionRecord, allocationRefID int64) (*organizationReservationRecord, error) {
	var reservationID int64
	err := scanExecutionOne(ctx, client, `
		INSERT INTO organization_reservations (
			execution_id_snapshot, execution_ref_id, organization_id,
			allocation_id_snapshot, allocation_ref_id, allocation_version_snapshot,
			organization_balance_version_snapshot, billing_fingerprint,
			candidate_snapshot, candidate_set_hash, pricing_snapshot, pricing_snapshot_hash,
			reserved_amount, status, reserved_at
		) VALUES (
			$1, $2, $3,
			$4, $5, $6,
			$7, $8,
			$9::jsonb, $10, $11::jsonb, $12,
			$13, $14, $15
		)
		RETURNING id
	`, []any{
		command.ExecutionID, execution.ID, command.Attribution.OrganizationID,
		command.Attribution.AllocationID, allocationRefID, command.Attribution.AllocationVersionSnapshot,
		command.Attribution.OrganizationBalanceVersionSnapshot, command.BillingFingerprint,
		command.CandidateSnapshotJSON, command.CandidateSetHash, command.PricingSnapshotJSON, command.PricingSnapshotHash,
		command.CandidateRouteMaximumCost, service.OrganizationReservationStateReserved, command.AcceptedAt.UTC(),
	}, &reservationID)
	if err != nil {
		return nil, executionStorageError("insert execution reservation", err)
	}
	return loadReservationByID(ctx, client, reservationID, false)
}

type executionLedgerOperation struct {
	ID          int64
	OperationID string
}

func insertExecutionLedgerOperation(ctx context.Context, client *dbent.Client, execution *organizationExecutionRecord, operationType string, occurredAt time.Time, idempotencyHash, reason string) (*executionLedgerOperation, error) {
	operationID := uuid.NewString()
	record := &executionLedgerOperation{OperationID: operationID}
	err := scanExecutionOne(ctx, client, `
		INSERT INTO organization_ledger_operations (
			operation_id, correlation_id, organization_id, operation_type,
			authorization_scope, actor_id_snapshot, actor_ref_id,
			actor_execution_id_snapshot, actor_execution_ref_id,
			actor_display_snapshot, actor_role_snapshot, currency,
			idempotency_key_hash, canonical_payload_hash, internal_reason, occurred_at
		) VALUES (
			$1, $2, $3, $4,
			$5, 0, NULL,
			$6, $7,
			$8, $9, $10,
			$11, $12, $13, $14
		)
		RETURNING id
	`, []any{
		operationID, execution.CorrelationID, execution.OrganizationID, operationType,
		domain.OrganizationFundingAuthorizationSystem, execution.ExecutionID, execution.ID,
		"system_execution", "system_execution", domain.OrganizationCurrencyUSD,
		idempotencyHash, executionLedgerHash(execution.ExecutionID, operationType+":"+reason), reason, occurredAt.UTC(),
	}, &record.ID)
	if err != nil {
		return nil, executionStorageError("insert execution ledger operation", err)
	}
	return record, nil
}

type executionLedgerPostingInput struct {
	OperationID     int64
	OperationUUID   string
	OrganizationID  int64
	Sequence        int
	BucketType      string
	BucketID        int64
	AllocationID    int64
	TargetUserID    *int64
	ExecutionID     string
	ExecutionRefID  int64
	FinalEvidenceID *int64
	Before          decimal.Decimal
	Delta           decimal.Decimal
	After           decimal.Decimal
	VersionBefore   int64
	VersionAfter    int64
	OccurredAt      time.Time
}

func insertExecutionLedgerPosting(ctx context.Context, client *dbent.Client, input executionLedgerPostingInput) error {
	if input.Delta.IsZero() || !input.Before.Add(input.Delta).Equal(input.After) || input.Before.IsNegative() || input.After.IsNegative() {
		return service.ErrOrganizationExecutionInvariant
	}
	_, err := client.ExecContext(ctx, `
		INSERT INTO organization_ledger_postings (
			operation_ref_id, operation_id_snapshot, organization_id, posting_sequence,
			bucket_type, bucket_id_snapshot, allocation_id_snapshot, allocation_ref_id,
			target_user_id_snapshot, target_user_ref_id,
			execution_id_snapshot, execution_ref_id,
			final_evidence_id_snapshot, final_evidence_ref_id,
			currency, before_amount, delta_amount, after_amount,
			balance_version_before, balance_version_after, occurred_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10,
			$11, $12,
			$13, $14,
			$15, $16, $17, $18,
			$19, $20, $21
		)
	`, input.OperationID, input.OperationUUID, input.OrganizationID, input.Sequence,
		input.BucketType, input.BucketID, input.AllocationID, input.AllocationID,
		input.TargetUserID, input.TargetUserID,
		input.ExecutionID, input.ExecutionRefID,
		input.FinalEvidenceID, input.FinalEvidenceID,
		domain.OrganizationCurrencyUSD, fixedExecutionMoney(input.Before), fixedExecutionMoney(input.Delta), fixedExecutionMoney(input.After),
		input.VersionBefore, input.VersionAfter, input.OccurredAt.UTC())
	if err != nil {
		return executionStorageError("insert execution ledger posting", err)
	}
	return nil
}

func updateExecutionAllocation(ctx context.Context, client *dbent.Client, allocation *organizationAllocationRecord, available, reserved decimal.Decimal, version int64) error {
	result, err := client.ExecContext(ctx, `
		UPDATE organization_allocations
		SET available = $1, reserved = $2, version = $3, updated_at = NOW()
		WHERE id = $4 AND version = $5
	`, fixedExecutionMoney(available), fixedExecutionMoney(reserved), version, allocation.ID, allocation.Version)
	if err != nil {
		return executionStorageError("update execution allocation", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		if err == nil {
			err = fmt.Errorf("expected one allocation update, affected %d", rows)
		}
		return service.ErrOrganizationExecutionConflict.WithCause(err)
	}
	return nil
}

func lockExecutionAndReservationForAttempt(ctx context.Context, client *dbent.Client, allocationID int64, executionID string, reservationID int64) (*organizationAllocationRecord, *organizationReservationRecord, *organizationExecutionRecord, error) {
	allocation, err := lockExecutionAllocation(ctx, client, allocationID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, nil, service.ErrOrganizationExecutionConflict
		}
		return nil, nil, nil, executionStorageError("lock attempt allocation", err)
	}
	reservation, err := loadReservationByID(ctx, client, reservationID, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, nil, service.ErrOrganizationExecutionConflict
		}
		return nil, nil, nil, executionStorageError("lock attempt reservation", err)
	}
	execution, err := loadExecutionByUUID(ctx, client, executionID, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, nil, service.ErrOrganizationExecutionConflict
		}
		return nil, nil, nil, executionStorageError("lock attempt execution", err)
	}
	return allocation, reservation, execution, nil
}

func loadExecutionAttemptByUUID(ctx context.Context, client *dbent.Client, attemptID string, forUpdate bool) (*organizationExecutionAttemptRecord, error) {
	query := `
		SELECT id, attempt_id, execution_id_snapshot, execution_ref_id, organization_id,
			attempt_sequence, account_id_snapshot, account_ref_id,
			route_snapshot::text, route_snapshot_hash, pricing_snapshot::text, pricing_snapshot_hash,
			provider_correlation_id, provider_request_id, status
		FROM organization_execution_attempts
		WHERE attempt_id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	record := &organizationExecutionAttemptRecord{}
	err := scanExecutionOne(ctx, client, query, []any{attemptID},
		&record.ID, &record.AttemptID, &record.ExecutionID, &record.ExecutionRefID, &record.OrganizationID,
		&record.AttemptSequence, &record.AccountID, &record.AccountRefID,
		&record.RouteSnapshotJSON, &record.RouteSnapshotHash, &record.PricingSnapshotJSON, &record.PricingSnapshotHash,
		&record.ProviderCorrelationID, &record.ProviderRequestID, &record.Status,
	)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func insertOrganizationExecutionAttempt(ctx context.Context, client *dbent.Client, input organizationExecutionAttemptInsert) (*organizationExecutionAttemptRecord, error) {
	var id int64
	err := scanExecutionOne(ctx, client, `
		INSERT INTO organization_execution_attempts (
			attempt_id, execution_id_snapshot, execution_ref_id, organization_id,
			attempt_sequence, account_id_snapshot, account_ref_id,
			route_snapshot, route_snapshot_hash, pricing_snapshot, pricing_snapshot_hash,
			provider_correlation_id, provider_request_id, status, dispatched_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7,
			$8::jsonb, $9, $10::jsonb, $11,
			$12, $13, $14, $15
		)
		RETURNING id
	`, []any{
		input.AttemptID, input.ExecutionID, input.ExecutionRefID, input.OrganizationID,
		input.AttemptSequence, input.AccountID, input.AccountRefID,
		input.RouteSnapshotJSON, input.RouteSnapshotHash, input.PricingSnapshotJSON, input.PricingSnapshotHash,
		input.ProviderCorrelationID, input.ProviderRequestID, input.Status, input.DispatchedAt.UTC(),
	}, &id)
	if err != nil {
		return nil, executionStorageError("insert execution attempt", err)
	}
	return &organizationExecutionAttemptRecord{
		ID:                    id,
		AttemptID:             input.AttemptID,
		ExecutionID:           input.ExecutionID,
		ExecutionRefID:        input.ExecutionRefID,
		OrganizationID:        input.OrganizationID,
		AttemptSequence:       input.AttemptSequence,
		AccountID:             sql.NullInt64{Int64: input.AccountID, Valid: true},
		AccountRefID:          nullableExecutionSQLInt64(input.AccountRefID),
		RouteSnapshotJSON:     input.RouteSnapshotJSON,
		RouteSnapshotHash:     input.RouteSnapshotHash,
		PricingSnapshotJSON:   input.PricingSnapshotJSON,
		PricingSnapshotHash:   input.PricingSnapshotHash,
		ProviderCorrelationID: input.ProviderCorrelationID,
		ProviderRequestID:     input.ProviderRequestID,
		Status:                input.Status,
	}, nil
}

func attemptMatchesEvidenceCommand(attempt *organizationExecutionAttemptRecord, command service.OrganizationAttemptEvidenceCommand, executionRefID int64) bool {
	return attempt != nil && attempt.ExecutionRefID == executionRefID && attempt.ExecutionID == command.ExecutionID &&
		attempt.OrganizationID == command.OrganizationID && attempt.AttemptSequence == command.AttemptSequence &&
		attempt.AccountID.Valid && attempt.AccountID.Int64 == command.AccountID &&
		attempt.RouteSnapshotHash == command.RouteSnapshotHash && attempt.PricingSnapshotHash == command.PricingSnapshotHash &&
		attempt.ProviderRequestID == command.ProviderRequestID && attempt.ProviderCorrelationID == command.ProviderCorrelationID
}

func ensureOrganizationAttemptAccountEffect(ctx context.Context, client *dbent.Client, attempt *organizationExecutionAttemptRecord, execution *organizationExecutionRecord, accountID int64, accountRefID *int64) error {
	if attempt == nil || execution == nil {
		return service.ErrOrganizationExecutionInvariant
	}
	if _, err := client.ExecContext(ctx, `
		INSERT INTO organization_attempt_account_effects (
			attempt_id_snapshot, attempt_ref_id, execution_id_snapshot,
			execution_ref_id, organization_id, account_id_snapshot, account_ref_id, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (attempt_ref_id) DO NOTHING
	`, attempt.AttemptID, attempt.ID, execution.ExecutionID, execution.ID, execution.OrganizationID, accountID, accountRefID, service.OrganizationAttemptAccountEffectStatePending); err != nil {
		return executionStorageError("insert attempt Account effect", err)
	}
	return nil
}

func loadAttemptEvidenceForReplay(ctx context.Context, client *dbent.Client, attemptID string, forUpdate bool) (*organizationAttemptEvidenceReplayRecord, error) {
	return loadAttemptEvidenceByIdentity(ctx, client, attemptID, "", "", forUpdate)
}

func loadAttemptEvidenceByIdentity(ctx context.Context, client *dbent.Client, attemptID, evidenceSource, provenanceHash string, forUpdate bool) (*organizationAttemptEvidenceReplayRecord, error) {
	query := `
			SELECT evidence.id, evidence.attempt_id_snapshot, evidence.execution_id_snapshot,
				evidence.execution_ref_id, evidence.organization_id, attempt.attempt_sequence,
				evidence.account_id_snapshot, evidence.route_snapshot_hash, evidence.pricing_snapshot_hash,
				evidence.provider_request_id, evidence.provider_correlation_id,
				evidence.evidence_source, evidence.terminal_outcome, evidence.provenance_hash, evidence.evidence_hash,
				evidence.input_tokens, evidence.output_tokens, evidence.cache_creation_tokens,
				evidence.cache_read_tokens, evidence.raw_actual_cost::text,
				evidence.provider_observed_at, evidence.ingested_at
			FROM organization_attempt_evidences AS evidence
			JOIN organization_execution_attempts AS attempt ON attempt.id = evidence.attempt_ref_id
			WHERE evidence.attempt_id_snapshot = $1`
	args := []any{attemptID}
	if evidenceSource != "" || provenanceHash != "" {
		query += " AND evidence.evidence_source = $2 AND evidence.provenance_hash = $3"
		args = append(args, evidenceSource, provenanceHash)
	} else {
		query += " ORDER BY evidence.ingested_at, evidence.id LIMIT 1"
	}
	if forUpdate {
		query += " FOR UPDATE OF evidence, attempt"
	}
	record := &organizationAttemptEvidenceReplayRecord{}
	err := scanExecutionOne(ctx, client, query, args,
		&record.ID, &record.AttemptID, &record.ExecutionID, &record.ExecutionRefID, &record.OrganizationID, &record.AttemptSequence,
		&record.AccountID, &record.RouteSnapshotHash, &record.PricingSnapshotHash,
		&record.ProviderRequestID, &record.ProviderCorrelation, &record.EvidenceSource, &record.TerminalOutcome, &record.ProvenanceHash, &record.EvidenceHash,
		&record.InputTokens, &record.OutputTokens, &record.CacheCreationTokens, &record.CacheReadTokens, &record.RawActualCost,
		&record.ProviderObservedAt, &record.IngestedAt,
	)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func loadFinalAttemptEvidence(ctx context.Context, client *dbent.Client, attemptID string, evidenceID int64, forUpdate bool) (*organizationAttemptEvidenceRecord, error) {
	query := `
		SELECT evidence.id, attempt.attempt_id, attempt.id, evidence.execution_ref_id,
				evidence.organization_id, evidence.account_id_snapshot, evidence.evidence_source,
				evidence.provenance_hash, evidence.terminal_outcome, evidence.raw_actual_cost::text,
			evidence.input_tokens, evidence.output_tokens, evidence.cache_creation_tokens,
			evidence.cache_read_tokens
		FROM organization_attempt_evidences AS evidence
		JOIN organization_execution_attempts AS attempt ON attempt.id = evidence.attempt_ref_id
		WHERE evidence.id = $1 AND attempt.attempt_id = $2`
	if forUpdate {
		query += " FOR UPDATE OF evidence, attempt"
	}
	record := &organizationAttemptEvidenceRecord{}
	err := scanExecutionOne(ctx, client, query, []any{evidenceID, attemptID},
		&record.ID, &record.AttemptID, &record.AttemptRefID, &record.ExecutionRefID,
		&record.OrganizationID, &record.AccountID, &record.EvidenceSource, &record.ProvenanceHash, &record.TerminalOutcome, &record.RawActualCost,
		&record.InputTokens, &record.OutputTokens, &record.CacheCreation, &record.CacheRead,
	)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func insertExecutionOutboxes(ctx context.Context, client *dbent.Client, execution *organizationExecutionRecord, finalEvidenceID int64, terminalOutcome string, occurredAt time.Time) error {
	eventType := "usage_projection"
	if terminalOutcome == service.OrganizationExecutionTerminalOutcomeError {
		eventType = "error_projection"
	}
	payload, err := json.Marshal(map[string]any{
		"execution_id":      execution.ExecutionID,
		"final_evidence_id": finalEvidenceID,
		"organization_id":   execution.OrganizationID,
		"terminal_outcome":  terminalOutcome,
	})
	if err != nil {
		return service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	for _, event := range []string{eventType, "api_key_quota", "user_platform_quota"} {
		if _, err := client.ExecContext(ctx, `
			INSERT INTO organization_execution_outboxes (
				execution_id_snapshot, execution_ref_id, final_evidence_id_snapshot,
				final_evidence_ref_id, organization_id, event_type, payload, status, occurred_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, 'pending', $8)
		`, execution.ExecutionID, execution.ID, finalEvidenceID, finalEvidenceID, execution.OrganizationID, event, string(payload), occurredAt.UTC()); err != nil {
			return executionStorageError("insert execution outbox", err)
		}
	}
	return nil
}

func organizationFinalEvidenceResult(ctx context.Context, client *dbent.Client, execution *organizationExecutionRecord, reservation *organizationReservationRecord, final *organizationFinalEvidenceRecord, replayed bool) (*service.OrganizationFinalExecutionEvidenceResult, error) {
	if execution == nil || final == nil || execution.ID != final.ExecutionRefID || execution.ExecutionID != final.ExecutionID || execution.OrganizationID != final.OrganizationID ||
		!final.AllocationIDSnapshot.Valid || final.UserIDSnapshot != execution.UserIDSnapshot || final.APIKeyIDSnapshot != execution.APIKeyIDSnapshot {
		return nil, service.ErrOrganizationExecutionInvariant
	}
	var consumptionID *int64
	if final.SettledCost != "0" && final.SettledCost != "0.00000000" {
		var id int64
		err := scanExecutionOne(ctx, client, `SELECT id FROM organization_ledger_postings WHERE final_evidence_id_snapshot = $1`, []any{final.ID}, &id)
		if err != nil {
			return nil, executionStorageError("load consumption posting", err)
		}
		consumptionID = &id
	}
	result := &service.OrganizationFinalExecutionEvidenceResult{
		FinalEvidenceID:         final.ID,
		ExecutionID:             execution.ExecutionID,
		CorrelationID:           execution.CorrelationID,
		OrganizationID:          execution.OrganizationID,
		UserID:                  final.UserIDSnapshot,
		APIKeyID:                final.APIKeyIDSnapshot,
		AllocationID:            final.AllocationIDSnapshot.Int64,
		TerminalOutcome:         final.TerminalOutcome,
		BusinessLimited:         final.BusinessLimited,
		RawActualCost:           final.RawActualCost,
		OrganizationSettledCost: fixedExecutionString(final.SettledCost),
		OccurredAt:              final.OccurredAt,
		Replayed:                replayed,
	}
	if final.ReconciledAt.Valid {
		value := final.ReconciledAt.Time
		result.ReconciledAt = &value
	}
	if final.ZeroCostProof.Valid {
		var proof map[string]string
		if err := json.Unmarshal([]byte(final.ZeroCostProof.String), &proof); err != nil || len(proof) != 1 || strings.TrimSpace(proof["reason"]) == "" {
			return nil, service.ErrOrganizationExecutionInvariant
		}
		result.ZeroProof = strings.TrimSpace(proof["reason"])
	}
	if final.FinalAttemptID.Valid {
		value := final.FinalAttemptID.String
		result.FinalAttemptID = &value
	}
	if final.FinalAttemptEvidenceID.Valid {
		value := final.FinalAttemptEvidenceID.Int64
		result.FinalAttemptEvidenceID = &value
	}
	if reservation != nil {
		result.ReservationID = reservation.ID
		result.ReservationState = reservation.Status
		result.ConsumptionPostingID = consumptionID
	}
	return result, nil
}

func sameOrganizationExecutionCommand(execution *organizationExecutionRecord, command service.OrganizationExecutionReservationCommand) bool {
	return execution != nil &&
		execution.ExecutionID == command.ExecutionID &&
		execution.CorrelationID == command.CorrelationID &&
		execution.OrganizationID == command.Attribution.OrganizationID &&
		execution.AllocationIDSnapshot.Valid && execution.AllocationIDSnapshot.Int64 == command.Attribution.AllocationID &&
		execution.UserIDSnapshot == command.Attribution.UserID && execution.APIKeyIDSnapshot == command.Attribution.APIKeyID &&
		execution.EndpointFamily == command.EndpointFamily && execution.RequestShape == command.RequestShape &&
		execution.BillingFingerprint == command.BillingFingerprint && execution.CandidateSnapshot == command.CandidateSnapshotJSON &&
		execution.CandidateSetHash == command.CandidateSetHash && execution.PricingSnapshot == command.PricingSnapshotJSON &&
		execution.PricingSnapshotHash == command.PricingSnapshotHash && fixedExecutionString(execution.CandidateCeiling) == command.CandidateRouteMaximumCost
}

func attemptCommandMatchesReservation(command service.OrganizationAttemptEvidenceCommand, execution *organizationExecutionRecord, reservation *organizationReservationRecord) bool {
	return execution != nil && reservation != nil &&
		execution.ExecutionID == command.ExecutionID && execution.CorrelationID == command.CorrelationID &&
		execution.OrganizationID == command.OrganizationID && execution.UserIDSnapshot == command.UserID && execution.APIKeyIDSnapshot == command.APIKeyID &&
		execution.AllocationIDSnapshot.Valid && execution.AllocationIDSnapshot.Int64 == command.AllocationID &&
		reservation.ID == command.ReservationID && reservation.ExecutionRefID == execution.ID && reservation.OrganizationID == command.OrganizationID &&
		reservation.AllocationIDSnapshot == command.AllocationID && reservation.PricingSnapshotHash == command.PricingSnapshotHash
}

func attemptAllocationMatchesCommand(allocation *organizationAllocationRecord, command service.OrganizationAttemptEvidenceCommand) bool {
	return allocation != nil && allocation.ID == command.AllocationID && allocation.OrganizationID == command.OrganizationID &&
		allocation.UserSnapshot == command.UserID && allocation.Status == domain.OrganizationAllocationStatusActive
}

func sameAttemptEvidenceCommand(record *organizationAttemptEvidenceReplayRecord, command service.OrganizationAttemptEvidenceCommand) bool {
	if record == nil || !record.AccountID.Valid {
		return false
	}
	rawActualCost, err := decimal.NewFromString(record.RawActualCost)
	if err != nil {
		return false
	}
	commandRawActualCost, err := decimal.NewFromString(command.RawActualCost)
	if err != nil {
		return false
	}
	return record.AttemptID == command.AttemptID && record.ExecutionID == command.ExecutionID &&
		record.OrganizationID == command.OrganizationID && record.AttemptSequence == command.AttemptSequence &&
		record.AccountID.Int64 == command.AccountID && record.RouteSnapshotHash == command.RouteSnapshotHash &&
		record.PricingSnapshotHash == command.PricingSnapshotHash && record.ProviderRequestID == command.ProviderRequestID &&
		record.ProviderCorrelation == command.ProviderCorrelationID && record.EvidenceSource == command.Provenance &&
		record.TerminalOutcome == command.TerminalOutcome && record.ProvenanceHash == command.ProvenanceHash && record.EvidenceHash == command.EvidenceHash &&
		record.InputTokens == command.InputTokens && record.OutputTokens == command.OutputTokens &&
		record.CacheCreationTokens == command.CacheCreationTokens && record.CacheReadTokens == command.CacheReadTokens &&
		rawActualCost.Equal(commandRawActualCost) &&
		record.ProviderObservedAt.Equal(command.ProviderObservedAt.UTC().Truncate(time.Microsecond)) &&
		record.IngestedAt.Equal(command.IngestedAt.UTC().Truncate(time.Microsecond))
}

func terminalizationMatches(command service.OrganizationTerminalizationCommand, allocation *organizationAllocationRecord, reservation *organizationReservationRecord, execution *organizationExecutionRecord) bool {
	return allocation != nil && reservation != nil && execution != nil &&
		allocation.ID == command.AllocationID && allocation.OrganizationID == command.OrganizationID && allocation.UserSnapshot == command.UserID &&
		reservation.ID == command.ReservationID && reservation.ExecutionRefID == execution.ID && reservation.ExecutionIDSnapshot == command.ExecutionID &&
		reservation.OrganizationID == command.OrganizationID && reservation.AllocationIDSnapshot == command.AllocationID &&
		execution.ExecutionID == command.ExecutionID && execution.CorrelationID == command.CorrelationID && execution.OrganizationID == command.OrganizationID &&
		execution.UserIDSnapshot == command.UserID && execution.APIKeyIDSnapshot == command.APIKeyID &&
		execution.AllocationIDSnapshot.Valid && execution.AllocationIDSnapshot.Int64 == command.AllocationID
}

func sameTerminalization(final *organizationFinalEvidenceRecord, command service.OrganizationTerminalizationCommand) bool {
	if final == nil || final.TerminalOutcome != command.TerminalOutcome || final.BusinessLimited != command.BusinessLimited {
		return false
	}
	if final.ReconciledAt.Valid != (command.ReconciledAt != nil) || (command.ReconciledAt != nil && !final.ReconciledAt.Time.Equal(command.ReconciledAt.UTC())) {
		return false
	}
	if final.FinalAttemptID.Valid != (command.FinalAttemptID != nil) || final.FinalAttemptEvidenceID.Valid != (command.FinalAttemptEvidenceID != nil) {
		return false
	}
	if command.FinalAttemptID != nil && (final.FinalAttemptID.String != *command.FinalAttemptID || final.FinalAttemptEvidenceID.Int64 != *command.FinalAttemptEvidenceID) {
		return false
	}
	if command.FinalAttemptID == nil {
		if !final.ZeroCostProof.Valid {
			return false
		}
		var proof map[string]string
		if err := json.Unmarshal([]byte(final.ZeroCostProof.String), &proof); err != nil {
			return false
		}
		return proof["reason"] == command.ZeroProof && len(proof) == 1
	}
	return true
}

func parseExecutionMoney(raw string) (decimal.Decimal, error) {
	value, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil || value.IsNegative() || value.Truncate(domain.OrganizationMoneyScale).Cmp(value) != 0 {
		if err == nil {
			err = fmt.Errorf("invalid execution money %q", raw)
		}
		return decimal.Zero, err
	}
	return value, nil
}

func fixedExecutionMoney(value decimal.Decimal) string {
	return value.StringFixed(domain.OrganizationMoneyScale)
}

// settleOrganizationExecutionCost is the sole conversion from provider cost
// evidence to organization money: non-negative raw cost is half-up rounded to
// the USD scale and then capped by the already-reserved route ceiling.
func settleOrganizationExecutionCost(rawActualCost, reservedCeiling decimal.Decimal) decimal.Decimal {
	settled := rawActualCost.Round(domain.OrganizationMoneyScale)
	if settled.GreaterThan(reservedCeiling) {
		return reservedCeiling
	}
	return settled
}

func fixedExecutionString(raw string) string {
	value, err := decimal.NewFromString(raw)
	if err != nil {
		return raw
	}
	return fixedExecutionMoney(value)
}

func nullableExecutionString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableCurrentUser(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	id := value.Int64
	return &id
}

func nullableExecutionSQLInt64(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func executionLedgerHash(executionID, suffix string) string {
	sum := sha256.Sum256([]byte(executionID + ":" + suffix))
	return hex.EncodeToString(sum[:])
}

func scanExecutionOne(ctx context.Context, client *dbent.Client, query string, args []any, destinations ...any) error {
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := rows.Scan(destinations...); err != nil {
		return err
	}
	return rows.Err()
}

func requireSingleExecutionMutation(operation string, result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return executionStorageError(operation+" rows affected", err)
	}
	if rows != 1 {
		return service.ErrOrganizationExecutionConflict.WithCause(fmt.Errorf("%s affected %d rows", operation, rows))
	}
	return nil
}

func executionStorageError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return service.ErrOrganizationExecutionRepositoryUnavailable.WithCause(fmt.Errorf("%s: %w", operation, err))
}

var _ service.OrganizationExecutionRepository = (*organizationExecutionRepository)(nil)
