package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

const organizationNonBillableStaleZeroProof = "stale_authoritative_attribution"

// BeginNonBillableExecution creates the durable parent without touching any
// allocation or reservation. The identity locks are retained so a concurrent
// organization/user/key change cannot race admission.
func (r *organizationExecutionRepository) BeginNonBillableExecution(ctx context.Context, command service.OrganizationNonBillableExecutionCommand) (*service.OrganizationNonBillableExecutionStartResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := service.ValidateOrganizationNonBillableExecutionCommand(command); err != nil {
		return nil, err
	}
	return withExecutionTransaction(r, ctx, "begin non-billable organization execution", func(txCtx context.Context, client *dbent.Client) (*service.OrganizationNonBillableExecutionStartResult, error) {
		return beginNonBillableOrganizationExecution(txCtx, client, command)
	})
}

func (r *organizationExecutionRepository) RecordNonBillableAttemptEvidence(ctx context.Context, command service.OrganizationNonBillableAttemptEvidenceCommand) (*service.OrganizationAttemptEvidenceResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := service.ValidateOrganizationNonBillableAttemptEvidenceRepositoryCommand(command); err != nil {
		return nil, err
	}
	return withExecutionTransaction(r, ctx, "record non-billable attempt evidence", func(txCtx context.Context, client *dbent.Client) (*service.OrganizationAttemptEvidenceResult, error) {
		return recordNonBillableOrganizationAttemptEvidence(txCtx, client, command)
	})
}

func (r *organizationExecutionRepository) RecordNonBillableRecoveryEvidence(ctx context.Context, command service.OrganizationNonBillableRecoveryEvidenceCommand) (*service.OrganizationAttemptEvidenceResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := service.ValidateOrganizationNonBillableRecoveryEvidenceCommand(command); err != nil {
		return nil, err
	}
	return withExecutionTransaction(r, ctx, "record non-billable recovery evidence", func(txCtx context.Context, client *dbent.Client) (*service.OrganizationAttemptEvidenceResult, error) {
		return recordNonBillableRecoveryEvidence(txCtx, client, command)
	})
}

func (r *organizationExecutionRepository) RecordNonBillablePendingAttempt(ctx context.Context, command service.OrganizationNonBillablePendingAttemptCommand) error {
	if r == nil || r.client == nil {
		return service.ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := service.ValidateOrganizationNonBillablePendingAttemptRepositoryCommand(command); err != nil {
		return err
	}
	return withExecutionTransactionNoResult(r, ctx, "record non-billable pending attempt", func(txCtx context.Context, client *dbent.Client) error {
		return recordNonBillablePendingAttempt(txCtx, client, command)
	})
}

func (r *organizationExecutionRepository) TerminalizeNonBillableExecution(ctx context.Context, command service.OrganizationNonBillableTerminalizationCommand) (*service.OrganizationNonBillableFinalExecutionEvidenceResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := service.ValidateOrganizationNonBillableTerminalizationRepositoryCommand(command); err != nil {
		return nil, err
	}
	return withExecutionTransaction(r, ctx, "terminalize non-billable organization execution", func(txCtx context.Context, client *dbent.Client) (*service.OrganizationNonBillableFinalExecutionEvidenceResult, error) {
		return terminalizeNonBillableOrganizationExecution(txCtx, client, command)
	})
}

type organizationNonBillableExecutionRecord struct {
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
	Model                sql.NullString
	ClientRequestID      sql.NullString
	BillingFingerprint   string
	CandidateSnapshot    string
	CandidateSetHash     string
	PricingSnapshot      string
	PricingSnapshotHash  string
	CandidateCeiling     string
	IsBillable           bool
	Status               string
	AcceptedAt           time.Time
	TerminalizedAt       sql.NullTime
}

func withExecutionTransactionNoResult(r *organizationExecutionRepository, ctx context.Context, operation string, fn func(context.Context, *dbent.Client) error) error {
	_, err := withExecutionTransaction(r, ctx, operation, func(txCtx context.Context, client *dbent.Client) (struct{}, error) {
		return struct{}{}, fn(txCtx, client)
	})
	return err
}

type organizationNonBillableFinalEvidenceRecord struct {
	ID                   int64
	ExecutionRefID       int64
	ExecutionID          string
	OrganizationID       int64
	AllocationIDSnapshot sql.NullInt64
	AllocationRefID      sql.NullInt64
	UserIDSnapshot       int64
	APIKeyIDSnapshot     int64
	FinalAttemptID       sql.NullString
	FinalAttemptEvidence sql.NullInt64
	TerminalOutcome      string
	BusinessLimited      bool
	InputTokens          int64
	OutputTokens         int64
	CacheCreationTokens  int64
	CacheReadTokens      int64
	RawActualCost        string
	SettledCost          string
	IsCountTokensProbe   bool
	ZeroCostProof        sql.NullString
	OccurredAt           time.Time
	ReconciledAt         sql.NullTime
}

func beginNonBillableOrganizationExecution(ctx context.Context, client *dbent.Client, command service.OrganizationNonBillableExecutionCommand) (*service.OrganizationNonBillableExecutionStartResult, error) {
	organizationStatus, _, err := lockExecutionOrganization(ctx, client, command.Attribution.OrganizationID)
	if err != nil {
		return nil, executionStorageError("lock organization for non-billable execution", err)
	}
	userCurrent, userOrganizationID, userDeleted, err := lockExecutionUser(ctx, client, command.Attribution.UserID)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("lock non-billable user", err)
	}
	keyCurrent, keyUserID, keyOrganizationID, keyStatus, keyDeleted, keyExpired, err := lockExecutionAPIKey(ctx, client, command.Attribution.APIKeyID)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("lock non-billable API key", err)
	}
	allocation, err := lockExecutionAllocation(ctx, client, command.Attribution.AllocationID)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("lock non-billable allocation", err)
	}
	// Lock attribution rows before the execution row, matching the billable
	// terminalization/recovery protocol and making concurrent begins replay-safe.
	existing, err := loadNonBillableExecutionByUUID(ctx, client, command.ExecutionID, true)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("load non-billable execution replay", err)
	}

	userRefID := (*int64)(nil)
	if userCurrent && !userDeleted && userOrganizationID.Valid && userOrganizationID.Int64 == command.Attribution.OrganizationID {
		userRefID = &command.Attribution.UserID
	}
	keyRefID := (*int64)(nil)
	if keyCurrent && !keyDeleted && keyUserID == command.Attribution.UserID && keyOrganizationID.Valid && keyOrganizationID.Int64 == command.Attribution.OrganizationID && keyStatus == service.StatusAPIKeyActive && !keyExpired {
		keyRefID = &command.Attribution.APIKeyID
	}
	allocationRefID := (*int64)(nil)
	if allocation != nil && allocation.ID == command.Attribution.AllocationID && allocation.OrganizationID == command.Attribution.OrganizationID && allocation.UserSnapshot == command.Attribution.UserID && allocation.Status == domain.OrganizationAllocationStatusActive && allocation.Version == command.Attribution.AllocationVersionSnapshot {
		allocationRefID = &command.Attribution.AllocationID
	}
	stale := organizationStatus != service.OrganizationStatusActive ||
		!userCurrent || userDeleted || !userOrganizationID.Valid || userOrganizationID.Int64 != command.Attribution.OrganizationID ||
		!keyCurrent || keyDeleted || keyExpired || keyUserID != command.Attribution.UserID || !keyOrganizationID.Valid || keyOrganizationID.Int64 != command.Attribution.OrganizationID || keyStatus != service.StatusAPIKeyActive ||
		allocation == nil || allocation.OrganizationID != command.Attribution.OrganizationID || allocation.UserSnapshot != command.Attribution.UserID || allocation.Status != domain.OrganizationAllocationStatusActive || allocation.Version != command.Attribution.AllocationVersionSnapshot
	if existing != nil {
		if stale {
			return nil, service.ErrOrganizationBillingUnavailable
		}
		return replayNonBillableExecution(ctx, client, command, existing)
	}

	_, err = insertNonBillableOrganizationExecution(ctx, client, command, allocationRefID, userRefID, keyRefID)
	if err != nil {
		return nil, err
	}
	if stale {
		terminal := service.OrganizationNonBillableTerminalizationCommand{
			ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
			OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID, APIKeyID: command.Attribution.APIKeyID, AllocationID: command.Attribution.AllocationID,
			TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: organizationNonBillableStaleZeroProof, OccurredAt: command.AcceptedAt.UTC(),
		}
		final, err := terminalizeNonBillableOrganizationExecution(ctx, client, terminal)
		if err != nil {
			return nil, err
		}
		return &service.OrganizationNonBillableExecutionStartResult{
			ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
			OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID, APIKeyID: command.Attribution.APIKeyID, AllocationID: command.Attribution.AllocationID,
			State: service.OrganizationExecutionStateReleased, Disposition: service.OrganizationNonBillableExecutionDispositionFinalized, FinalEvidence: final,
		}, nil
	}
	return &service.OrganizationNonBillableExecutionStartResult{
		ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
		OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID, APIKeyID: command.Attribution.APIKeyID, AllocationID: command.Attribution.AllocationID,
		State: service.OrganizationExecutionStateAccepted, Disposition: service.OrganizationNonBillableExecutionDispositionStarted,
	}, nil
}

func replayNonBillableExecution(ctx context.Context, client *dbent.Client, command service.OrganizationNonBillableExecutionCommand, execution *organizationNonBillableExecutionRecord) (*service.OrganizationNonBillableExecutionStartResult, error) {
	if !sameNonBillableExecutionCommand(execution, command) {
		return nil, service.ErrOrganizationExecutionConflict
	}
	final, err := loadNonBillableFinalEvidenceByExecution(ctx, client, execution.ID, true)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("load non-billable final replay", err)
	}
	if final != nil {
		result, err := nonBillableFinalEvidenceResult(execution, final, true)
		if err != nil {
			return nil, err
		}
		return &service.OrganizationNonBillableExecutionStartResult{
			ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
			OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID, APIKeyID: command.Attribution.APIKeyID, AllocationID: command.Attribution.AllocationID,
			State: service.OrganizationExecutionStateReleased, Disposition: service.OrganizationNonBillableExecutionDispositionFinalized, FinalEvidence: result, Replayed: true,
		}, nil
	}
	// A parent without final evidence is intentionally not dispatched again.
	// The caller must surface the conflict and let a recovery/control-plane
	// workflow decide whether the provider was ever reached.
	return nil, service.ErrOrganizationExecutionConflict
}

func insertNonBillableOrganizationExecution(ctx context.Context, client *dbent.Client, command service.OrganizationNonBillableExecutionCommand, allocationRefID, userRefID, keyRefID *int64) (*organizationNonBillableExecutionRecord, error) {
	var id int64
	err := scanExecutionOne(ctx, client, `
		INSERT INTO organization_executions (
			execution_id, correlation_id, organization_id,
			allocation_id_snapshot, allocation_ref_id, user_id_snapshot, user_ref_id,
			api_key_id_snapshot, api_key_ref_id, endpoint_family, request_shape, model,
			client_request_id, billing_fingerprint, candidate_snapshot,
			candidate_set_hash, pricing_snapshot, pricing_snapshot_hash,
			candidate_ceiling, is_billable, status, accepted_at
		) VALUES (
			$1, $2, $3,
			$4, $5, $6, $7,
			$8, $9, $10, $11,
			$12, $13, $14::jsonb,
			$15, $16::jsonb, $17,
			0, FALSE, $18, $19
		)
		RETURNING id
	`, []any{
		command.ExecutionID, command.CorrelationID, command.Attribution.OrganizationID,
		command.Attribution.AllocationID, allocationRefID, command.Attribution.UserID, userRefID, command.Attribution.APIKeyID, keyRefID,
		command.EndpointFamily, command.RequestShape, nullableExecutionString(command.Model), nullableExecutionString(command.ClientRequestID),
		command.BillingFingerprint, command.CandidateSnapshotJSON, command.CandidateSetHash, command.PricingSnapshotJSON, command.PricingSnapshotHash,
		service.OrganizationExecutionStateAccepted, command.AcceptedAt.UTC(),
	}, &id)
	if err != nil {
		return nil, executionStorageError("insert non-billable execution", err)
	}
	return loadNonBillableExecutionByUUID(ctx, client, command.ExecutionID, false)
}

func recordNonBillableOrganizationAttemptEvidence(ctx context.Context, client *dbent.Client, command service.OrganizationNonBillableAttemptEvidenceCommand) (*service.OrganizationAttemptEvidenceResult, error) {
	execution, err := loadNonBillableExecutionByUUID(ctx, client, command.ExecutionID, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return nil, executionStorageError("load non-billable attempt execution", err)
	}
	if execution.IsBillable || !nonBillableDispatchFamily(execution.EndpointFamily) ||
		execution.OrganizationID != command.OrganizationID || execution.UserIDSnapshot != command.UserID || execution.APIKeyIDSnapshot != command.APIKeyID || !execution.AllocationIDSnapshot.Valid || execution.AllocationIDSnapshot.Int64 != command.AllocationID ||
		execution.PricingSnapshotHash != command.PricingSnapshotHash || execution.PricingSnapshot != command.PricingSnapshotJSON {
		return nil, service.ErrOrganizationExecutionConflict
	}
	existing, err := loadAttemptEvidenceByIdentity(ctx, client, command.AttemptID, command.Provenance, command.ProvenanceHash, true)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("load non-billable attempt replay", err)
	}
	if existing != nil {
		if !sameNonBillableAttemptEvidence(existing, command) || existing.ExecutionRefID != execution.ID {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return &service.OrganizationAttemptEvidenceResult{AttemptEvidenceID: existing.ID, ExecutionID: command.ExecutionID, AttemptID: command.AttemptID, Replayed: true}, nil
	}
	anyEvidence, err := loadAttemptEvidenceForReplay(ctx, client, command.AttemptID, true)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("load non-billable attempt evidence identity", err)
	}
	if anyEvidence != nil {
		return nil, service.ErrOrganizationExecutionConflict
	}
	if execution.Status != service.OrganizationExecutionStateAccepted && execution.Status != service.OrganizationExecutionStateDispatched && execution.Status != service.OrganizationExecutionStateFinalUsageObserved && execution.Status != service.OrganizationExecutionStatePendingReconciliation {
		return nil, service.ErrOrganizationExecutionConflict
	}
	if (execution.Status == service.OrganizationExecutionStatePendingReconciliation) != (command.Provenance == service.OrganizationAttemptEvidenceProvenanceProviderQuery) {
		return nil, service.ErrOrganizationExecutionConflict
	}
	accountRefID, err := currentExecutionAccountRef(ctx, client, command.AccountID)
	if err != nil {
		return nil, err
	}
	attempt, err := loadExecutionAttemptByUUID(ctx, client, command.AttemptID, true)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("load non-billable attempt", err)
	}
	if attempt == nil {
		var attemptCount int64
		if err := scanExecutionOne(ctx, client, `SELECT COUNT(*) FROM organization_execution_attempts WHERE execution_ref_id = $1`, []any{execution.ID}, &attemptCount); err != nil {
			return nil, executionStorageError("count non-billable execution attempts", err)
		}
		if attemptCount != 0 {
			return nil, service.ErrOrganizationExecutionConflict
		}
		attempt, err = insertOrganizationExecutionAttempt(ctx, client, organizationExecutionAttemptInsert{
			AttemptID: command.AttemptID, ExecutionID: command.ExecutionID, ExecutionRefID: execution.ID, OrganizationID: command.OrganizationID,
			AttemptSequence: command.AttemptSequence, AccountID: command.AccountID, AccountRefID: accountRefID,
			RouteSnapshotJSON: command.RouteSnapshotJSON, RouteSnapshotHash: command.RouteSnapshotHash,
			PricingSnapshotJSON: command.PricingSnapshotJSON, PricingSnapshotHash: command.PricingSnapshotHash,
			ProviderCorrelationID: command.ProviderCorrelationID, ProviderRequestID: command.ProviderRequestID,
			Status: service.OrganizationExecutionStateFinalUsageObserved, DispatchedAt: command.IngestedAt,
		})
		if err != nil {
			return nil, err
		}
	} else if !attemptMatchesNonBillableEvidence(attempt, command, execution.ID) {
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
		command.AttemptID, attempt.ID, command.ExecutionID, execution.ID, command.OrganizationID, command.AccountID, accountRefID,
		command.RouteSnapshotJSON, command.RouteSnapshotHash, command.PricingSnapshotJSON, command.PricingSnapshotHash,
		command.ProviderCorrelationID, command.ProviderRequestID, command.Provenance, command.TerminalOutcome, command.ProvenanceHash,
		command.EvidenceJSON, command.EvidenceHash, command.InputTokens, command.OutputTokens, command.CacheCreationTokens,
		command.CacheReadTokens, command.RawActualCost, command.ProviderObservedAt.UTC(), command.IngestedAt.UTC(),
	}, &evidenceID)
	if err != nil {
		return nil, executionStorageError("insert non-billable attempt evidence", err)
	}
	if execution.Status == service.OrganizationExecutionStateAccepted {
		result, err := client.ExecContext(ctx, `UPDATE organization_executions SET status = $1, updated_at = NOW() WHERE id = $2 AND status = $3`, service.OrganizationExecutionStateDispatched, execution.ID, service.OrganizationExecutionStateAccepted)
		if err != nil {
			return nil, executionStorageError("mark non-billable execution dispatched", err)
		}
		if err := requireSingleExecutionMutation("mark non-billable execution dispatched", result); err != nil {
			return nil, err
		}
	}
	if execution.Status == service.OrganizationExecutionStateAccepted || execution.Status == service.OrganizationExecutionStateDispatched {
		result, err := client.ExecContext(ctx, `UPDATE organization_executions SET status = $1, updated_at = NOW() WHERE id = $2 AND status IN ($3, $4)`, service.OrganizationExecutionStateFinalUsageObserved, execution.ID, service.OrganizationExecutionStateAccepted, service.OrganizationExecutionStateDispatched)
		if err != nil {
			return nil, executionStorageError("mark non-billable final usage observed", err)
		}
		if err := requireSingleExecutionMutation("mark non-billable final usage observed", result); err != nil {
			return nil, err
		}
	}
	return &service.OrganizationAttemptEvidenceResult{AttemptEvidenceID: evidenceID, ExecutionID: command.ExecutionID, AttemptID: command.AttemptID}, nil
}

func recordNonBillableRecoveryEvidence(ctx context.Context, client *dbent.Client, command service.OrganizationNonBillableRecoveryEvidenceCommand) (*service.OrganizationAttemptEvidenceResult, error) {
	execution, err := loadNonBillableExecutionByUUID(ctx, client, command.ExecutionID, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return nil, executionStorageError("load non-billable recovery execution", err)
	}
	if execution.IsBillable || !nonBillableDispatchFamily(execution.EndpointFamily) || execution.Status != service.OrganizationExecutionStatePendingReconciliation || execution.OrganizationID != command.OrganizationID || !execution.AllocationIDSnapshot.Valid {
		return nil, service.ErrOrganizationExecutionConflict
	}
	attempt, err := loadExecutionAttemptByUUID(ctx, client, command.AttemptID, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return nil, executionStorageError("load non-billable recovery attempt", err)
	}
	if attempt.ExecutionRefID != execution.ID || attempt.ExecutionID != execution.ExecutionID || attempt.OrganizationID != execution.OrganizationID || attempt.AttemptSequence != 1 || !attempt.AccountID.Valid || attempt.Status != service.OrganizationExecutionStateDispatched || attempt.PricingSnapshotJSON != execution.PricingSnapshot || attempt.PricingSnapshotHash != execution.PricingSnapshotHash || attempt.ProviderCorrelationID == "" || attempt.ProviderRequestID == "" {
		return nil, service.ErrOrganizationExecutionConflict
	}

	// Reuse the normal evidence writer so account references, immutable
	// identity checks, and evidence uniqueness stay in one transaction.
	return recordNonBillableOrganizationAttemptEvidence(ctx, client, service.OrganizationNonBillableAttemptEvidenceCommand{
		ExecutionID:           execution.ExecutionID,
		AttemptID:             attempt.AttemptID,
		CorrelationID:         execution.CorrelationID,
		OrganizationID:        execution.OrganizationID,
		UserID:                execution.UserIDSnapshot,
		APIKeyID:              execution.APIKeyIDSnapshot,
		AllocationID:          execution.AllocationIDSnapshot.Int64,
		AttemptSequence:       attempt.AttemptSequence,
		AccountID:             attempt.AccountID.Int64,
		RouteSnapshotJSON:     attempt.RouteSnapshotJSON,
		RouteSnapshotHash:     attempt.RouteSnapshotHash,
		PricingSnapshotJSON:   attempt.PricingSnapshotJSON,
		PricingSnapshotHash:   attempt.PricingSnapshotHash,
		ProviderRequestID:     attempt.ProviderRequestID,
		ProviderCorrelationID: attempt.ProviderCorrelationID,
		Provenance:            service.OrganizationAttemptEvidenceProvenanceProviderQuery,
		TerminalOutcome:       command.TerminalOutcome,
		ProvenanceHash:        command.EvidenceHash,
		EvidenceJSON:          command.EvidenceJSON,
		EvidenceHash:          command.EvidenceHash,
		RawActualCost:         "0",
		InputTokens:           command.InputTokens,
		OutputTokens:          command.OutputTokens,
		CacheCreationTokens:   command.CacheCreationTokens,
		CacheReadTokens:       command.CacheReadTokens,
		ProviderObservedAt:    command.ProviderObservedAt,
		IngestedAt:            command.IngestedAt,
	})
}

func recordNonBillablePendingAttempt(ctx context.Context, client *dbent.Client, command service.OrganizationNonBillablePendingAttemptCommand) error {
	execution, err := loadNonBillableExecutionByUUID(ctx, client, command.ExecutionID, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return service.ErrOrganizationExecutionConflict
		}
		return executionStorageError("load non-billable pending execution", err)
	}
	if execution.IsBillable || !nonBillableDispatchFamily(execution.EndpointFamily) || execution.CorrelationID != command.CorrelationID || execution.OrganizationID != command.OrganizationID || execution.UserIDSnapshot != command.UserID || execution.APIKeyIDSnapshot != command.APIKeyID || !execution.AllocationIDSnapshot.Valid || execution.AllocationIDSnapshot.Int64 != command.AllocationID || execution.PricingSnapshotHash != command.PricingSnapshotHash || execution.PricingSnapshot != command.PricingSnapshotJSON {
		return service.ErrOrganizationExecutionConflict
	}
	if execution.Status != service.OrganizationExecutionStateAccepted && execution.Status != service.OrganizationExecutionStateDispatched && execution.Status != service.OrganizationExecutionStateFinalUsageObserved && execution.Status != service.OrganizationExecutionStatePendingReconciliation {
		return service.ErrOrganizationExecutionConflict
	}
	attempt, err := loadExecutionAttemptByUUID(ctx, client, command.AttemptID, true)
	if err != nil && err != sql.ErrNoRows {
		return executionStorageError("load non-billable pending attempt", err)
	}
	evidence, err := loadAttemptEvidenceForReplay(ctx, client, command.AttemptID, true)
	if err != nil && err != sql.ErrNoRows {
		return executionStorageError("load non-billable pending attempt evidence", err)
	}
	if evidence != nil {
		return service.ErrOrganizationExecutionConflict
	}
	if execution.Status == service.OrganizationExecutionStatePendingReconciliation {
		matches, err := exactNonBillablePendingAttempt(ctx, client, attempt, command, execution.ID)
		if err != nil {
			return err
		}
		if !matches {
			return service.ErrOrganizationExecutionConflict
		}
		return nil
	}
	accountRefID, err := currentExecutionAccountRef(ctx, client, command.AccountID)
	if err != nil {
		return err
	}
	if attempt == nil {
		var attemptCount int64
		if err := scanExecutionOne(ctx, client, `SELECT COUNT(*) FROM organization_execution_attempts WHERE execution_ref_id = $1`, []any{execution.ID}, &attemptCount); err != nil {
			return executionStorageError("count non-billable pending attempts", err)
		}
		if attemptCount != 0 {
			return service.ErrOrganizationExecutionConflict
		}
		_, err = insertOrganizationExecutionAttempt(ctx, client, organizationExecutionAttemptInsert{
			AttemptID: command.AttemptID, ExecutionID: command.ExecutionID, ExecutionRefID: execution.ID, OrganizationID: command.OrganizationID,
			AttemptSequence: command.AttemptSequence, AccountID: command.AccountID, AccountRefID: accountRefID,
			RouteSnapshotJSON: command.RouteSnapshotJSON, RouteSnapshotHash: command.RouteSnapshotHash,
			PricingSnapshotJSON: command.PricingSnapshotJSON, PricingSnapshotHash: command.PricingSnapshotHash,
			ProviderCorrelationID: command.ProviderCorrelationID, ProviderRequestID: command.ProviderRequestID,
			Status: service.OrganizationExecutionStateDispatched, DispatchedAt: command.DispatchedAt,
		})
		if err != nil {
			return err
		}
	} else {
		matches, err := exactNonBillablePendingAttempt(ctx, client, attempt, command, execution.ID)
		if err != nil {
			return err
		}
		if !matches {
			return service.ErrOrganizationExecutionConflict
		}
	}
	reservationlessStatus, err := client.ExecContext(ctx, `UPDATE organization_executions SET status = $1, updated_at = NOW() WHERE id = $2 AND status IN ($3, $4, $5)`, service.OrganizationExecutionStatePendingReconciliation, execution.ID, service.OrganizationExecutionStateAccepted, service.OrganizationExecutionStateDispatched, service.OrganizationExecutionStateFinalUsageObserved)
	if err != nil {
		return executionStorageError("mark non-billable execution pending reconciliation", err)
	}
	if err := requireSingleExecutionMutation("mark non-billable execution pending reconciliation", reservationlessStatus); err != nil {
		return err
	}
	return nil
}

func terminalizeNonBillableOrganizationExecution(ctx context.Context, client *dbent.Client, command service.OrganizationNonBillableTerminalizationCommand) (*service.OrganizationNonBillableFinalExecutionEvidenceResult, error) {
	return terminalizeNonBillableOrganizationExecutionAt(ctx, client, command, nil)
}

func terminalizeNonBillableOrganizationExecutionAt(ctx context.Context, client *dbent.Client, command service.OrganizationNonBillableTerminalizationCommand, reconciledAt *time.Time) (*service.OrganizationNonBillableFinalExecutionEvidenceResult, error) {
	allowStaleFinal := command.FinalAttemptID == nil && strings.TrimSpace(command.ZeroProof) == organizationNonBillableStaleZeroProof
	allocation, allocationErr := lockExecutionAllocation(ctx, client, command.AllocationID)
	if allocationErr != nil {
		if !allowStaleFinal || allocationErr != sql.ErrNoRows {
			if allocationErr == sql.ErrNoRows {
				return nil, service.ErrOrganizationExecutionConflict
			}
			return nil, executionStorageError("lock non-billable terminal allocation", allocationErr)
		}
	}
	execution, err := loadNonBillableExecutionByUUID(ctx, client, command.ExecutionID, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return nil, executionStorageError("load non-billable terminal execution", err)
	}
	if execution.IsBillable || execution.CorrelationID != command.CorrelationID || execution.OrganizationID != command.OrganizationID || execution.UserIDSnapshot != command.UserID || execution.APIKeyIDSnapshot != command.APIKeyID || !execution.AllocationIDSnapshot.Valid || execution.AllocationIDSnapshot.Int64 != command.AllocationID {
		return nil, service.ErrOrganizationExecutionConflict
	}
	final, err := loadNonBillableFinalEvidenceByExecution(ctx, client, execution.ID, true)
	if err != nil && err != sql.ErrNoRows {
		return nil, executionStorageError("load non-billable terminal replay", err)
	}
	if final != nil {
		if !sameNonBillableTerminalization(final, command) {
			return nil, service.ErrOrganizationExecutionConflict
		}
		return nonBillableFinalEvidenceResult(execution, final, true)
	}
	var allocationRefID any
	if !execution.AllocationRefID.Valid {
		if !allowStaleFinal {
			return nil, service.ErrOrganizationExecutionConflict
		}
	} else if allocationErr == nil && allocation != nil && allocation.OrganizationID == command.OrganizationID && allocation.UserSnapshot == command.UserID {
		allocationRefID = execution.AllocationRefID.Int64
	} else if !allowStaleFinal {
		return nil, service.ErrOrganizationExecutionConflict
	}
	if execution.Status != service.OrganizationExecutionStateAccepted && execution.Status != service.OrganizationExecutionStateFinalUsageObserved && !(reconciledAt != nil && execution.Status == service.OrganizationExecutionStatePendingReconciliation) {
		return nil, service.ErrOrganizationExecutionConflict
	}

	var evidence *organizationAttemptEvidenceRecord
	if command.FinalAttemptID != nil {
		if !nonBillableDispatchFamily(execution.EndpointFamily) {
			return nil, service.ErrOrganizationExecutionConflict
		}
		evidence, err = loadFinalAttemptEvidence(ctx, client, *command.FinalAttemptID, *command.FinalAttemptEvidenceID, true)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, service.ErrOrganizationExecutionConflict
			}
			return nil, executionStorageError("load non-billable final attempt evidence", err)
		}
		if evidence.ExecutionRefID != execution.ID || evidence.OrganizationID != command.OrganizationID || evidence.AttemptID != *command.FinalAttemptID || evidence.TerminalOutcome != command.TerminalOutcome || evidence.RawActualCost != "0" {
			return nil, service.ErrOrganizationExecutionConflict
		}
		var attemptCount, evidenceCount int64
		if err := scanExecutionOne(ctx, client, `
			SELECT COUNT(DISTINCT attempt.id), COUNT(evidence.id)
			FROM organization_execution_attempts AS attempt
			LEFT JOIN organization_attempt_evidences AS evidence ON evidence.attempt_ref_id = attempt.id
			WHERE attempt.execution_ref_id = $1
		`, []any{execution.ID}, &attemptCount, &evidenceCount); err != nil {
			return nil, executionStorageError("count non-billable final attempt evidence", err)
		}
		if attemptCount != 1 || evidenceCount != 1 {
			return nil, service.ErrOrganizationExecutionConflict
		}
	} else {
		if strings.TrimSpace(command.ZeroProof) == "" {
			return nil, service.ErrOrganizationExecutionInvalid
		}
		var attemptCount int64
		if err := scanExecutionOne(ctx, client, `SELECT COUNT(*) FROM organization_execution_attempts WHERE execution_ref_id = $1`, []any{execution.ID}, &attemptCount); err != nil {
			return nil, executionStorageError("count non-billable execution attempts", err)
		}
		if attemptCount != 0 {
			return nil, service.ErrOrganizationExecutionConflict
		}
	}

	userRefID, err := currentExecutionUserRef(ctx, client, command.UserID)
	if err != nil {
		return nil, err
	}
	apiKeyRefID, err := currentExecutionAPIKeyRef(ctx, client, command.APIKeyID)
	if err != nil {
		return nil, err
	}
	inputTokens, outputTokens, cacheCreation, cacheRead := int64(0), int64(0), int64(0), int64(0)
	var finalAttemptID any
	var finalAttemptRefID any
	var finalAttemptEvidenceID any
	var finalAttemptEvidenceRefID any
	var accountIDSnapshot any
	var accountRefID any
	if evidence != nil {
		inputTokens, outputTokens = evidence.InputTokens, evidence.OutputTokens
		cacheCreation, cacheRead = evidence.CacheCreation, evidence.CacheRead
		finalAttemptID, finalAttemptRefID = evidence.AttemptID, evidence.AttemptRefID
		finalAttemptEvidenceID, finalAttemptEvidenceRefID = evidence.ID, evidence.ID
		if evidence.AccountID.Valid {
			accountIDSnapshot = evidence.AccountID.Int64
			ref, err := currentExecutionAccountRef(ctx, client, evidence.AccountID.Int64)
			if err != nil {
				return nil, err
			}
			accountRefID = ref
		}
	}
	var zeroProof any
	if evidence == nil {
		proofJSON, err := json.Marshal(map[string]string{"reason": strings.TrimSpace(command.ZeroProof)})
		if err != nil {
			return nil, service.ErrOrganizationExecutionInvariant.WithCause(err)
		}
		zeroProof = string(proofJSON)
	}
	var reconciledAtValue any
	if reconciledAt != nil {
		reconciledAtValue = reconciledAt.UTC()
	}
	var finalID int64
	isCountTokensProbe := execution.EndpointFamily == service.OrganizationEndpointFamilyTokenCounting
	err = scanExecutionOne(ctx, client, `
		INSERT INTO organization_final_execution_evidences (
			execution_id_snapshot, execution_ref_id, organization_id,
			allocation_id_snapshot, allocation_ref_id,
			user_id_snapshot, user_ref_id, api_key_id_snapshot, api_key_ref_id,
			final_attempt_id_snapshot, final_attempt_ref_id,
			final_attempt_evidence_id_snapshot, final_attempt_evidence_ref_id,
			account_id_snapshot, account_ref_id, endpoint_family, request_shape, model,
			input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
			raw_actual_cost, organization_settled_cost, terminal_outcome,
			is_business_limited, is_count_tokens_probe, zero_cost_proof, occurred_at, reconciled_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12, $13,
			$14, $15, $16, $17, $18,
			$19, $20, $21, $22, 0, 0, $23,
			$24, $25, $26::jsonb, $27, $28
		)
		RETURNING id
	`, []any{
		execution.ExecutionID, execution.ID, command.OrganizationID, execution.AllocationIDSnapshot.Int64, allocationRefID,
		command.UserID, userRefID, command.APIKeyID, apiKeyRefID,
		finalAttemptID, finalAttemptRefID, finalAttemptEvidenceID, finalAttemptEvidenceRefID,
		accountIDSnapshot, accountRefID, execution.EndpointFamily, execution.RequestShape, nullableExecutionString(execution.Model.String),
		inputTokens, outputTokens, cacheCreation, cacheRead, command.TerminalOutcome, command.BusinessLimited, isCountTokensProbe, zeroProof, command.OccurredAt.UTC(), reconciledAtValue,
	}, &finalID)
	if err != nil {
		return nil, executionStorageError("insert non-billable final evidence", err)
	}
	var updateResult sql.Result
	if reconciledAt != nil {
		updateResult, err = client.ExecContext(ctx, `UPDATE organization_executions SET status = $1, terminalized_at = $2, updated_at = NOW() WHERE id = $3 AND status IN ($4, $5, $6)`, service.OrganizationExecutionStateReleased, command.OccurredAt.UTC(), execution.ID, service.OrganizationExecutionStateAccepted, service.OrganizationExecutionStateFinalUsageObserved, service.OrganizationExecutionStatePendingReconciliation)
	} else {
		updateResult, err = client.ExecContext(ctx, `UPDATE organization_executions SET status = $1, terminalized_at = $2, updated_at = NOW() WHERE id = $3 AND status IN ($4, $5)`, service.OrganizationExecutionStateReleased, command.OccurredAt.UTC(), execution.ID, service.OrganizationExecutionStateAccepted, service.OrganizationExecutionStateFinalUsageObserved)
	}
	if err != nil {
		return nil, executionStorageError("terminalize non-billable execution", err)
	}
	if err := requireSingleExecutionMutation("terminalize non-billable execution", updateResult); err != nil {
		return nil, err
	}
	if err := insertNonBillableExecutionOutbox(ctx, client, execution, finalID, command.TerminalOutcome, command.OccurredAt, inputTokens, outputTokens, cacheCreation, cacheRead, isCountTokensProbe); err != nil {
		return nil, err
	}
	record := &organizationNonBillableFinalEvidenceRecord{
		ID: finalID, ExecutionRefID: execution.ID, ExecutionID: execution.ExecutionID, OrganizationID: execution.OrganizationID,
		AllocationIDSnapshot: execution.AllocationIDSnapshot, UserIDSnapshot: command.UserID, APIKeyIDSnapshot: command.APIKeyID, TerminalOutcome: command.TerminalOutcome, BusinessLimited: command.BusinessLimited,
		InputTokens: inputTokens, OutputTokens: outputTokens, CacheCreationTokens: cacheCreation, CacheReadTokens: cacheRead,
		RawActualCost: "0", SettledCost: "0.00000000", IsCountTokensProbe: isCountTokensProbe, OccurredAt: command.OccurredAt.UTC(),
	}
	if reconciledAt != nil {
		record.ReconciledAt = sql.NullTime{Time: reconciledAt.UTC(), Valid: true}
	}
	if evidence != nil {
		record.FinalAttemptID = sql.NullString{String: evidence.AttemptID, Valid: true}
		record.FinalAttemptEvidence = sql.NullInt64{Int64: evidence.ID, Valid: true}
	} else {
		proofJSON, _ := json.Marshal(map[string]string{"reason": strings.TrimSpace(command.ZeroProof)})
		record.ZeroCostProof = sql.NullString{String: string(proofJSON), Valid: true}
	}
	return nonBillableFinalEvidenceResult(execution, record, false)
}

func insertNonBillableExecutionOutbox(ctx context.Context, client *dbent.Client, execution *organizationNonBillableExecutionRecord, finalEvidenceID int64, terminalOutcome string, occurredAt time.Time, inputTokens, outputTokens, cacheCreation, cacheRead int64, isCountTokensProbe bool) error {
	eventType := "usage_projection"
	if terminalOutcome == service.OrganizationExecutionTerminalOutcomeError {
		eventType = "error_projection"
	}
	payload, err := json.Marshal(map[string]any{
		"execution_id": execution.ExecutionID, "final_evidence_id": finalEvidenceID, "organization_id": execution.OrganizationID,
		"terminal_outcome": terminalOutcome, "input_tokens": inputTokens, "output_tokens": outputTokens,
		"cache_creation_tokens": cacheCreation, "cache_read_tokens": cacheRead, "is_count_tokens_probe": isCountTokensProbe,
	})
	if err != nil {
		return service.ErrOrganizationExecutionInvariant.WithCause(err)
	}
	if _, err := client.ExecContext(ctx, `
		INSERT INTO organization_execution_outboxes (
			execution_id_snapshot, execution_ref_id, final_evidence_id_snapshot,
			final_evidence_ref_id, organization_id, event_type, payload, status, occurred_at
		) VALUES ($1, $2, $3, $3, $4, $5, $6::jsonb, 'pending', $7)
	`, execution.ExecutionID, execution.ID, finalEvidenceID, execution.OrganizationID, eventType, string(payload), occurredAt.UTC()); err != nil {
		return executionStorageError("insert non-billable execution outbox", err)
	}
	return nil
}

func loadNonBillableExecutionByUUID(ctx context.Context, client *dbent.Client, executionID string, forUpdate bool) (*organizationNonBillableExecutionRecord, error) {
	query := `
		SELECT id, execution_id, correlation_id, organization_id,
			allocation_id_snapshot, allocation_ref_id, user_id_snapshot, user_ref_id, api_key_id_snapshot, api_key_ref_id,
			endpoint_family, request_shape, model, client_request_id,
			billing_fingerprint, candidate_snapshot::text, candidate_set_hash,
			pricing_snapshot::text, pricing_snapshot_hash, candidate_ceiling::text,
			is_billable, status, accepted_at, terminalized_at
		FROM organization_executions WHERE execution_id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	record := &organizationNonBillableExecutionRecord{}
	err := scanExecutionOne(ctx, client, query, []any{executionID},
		&record.ID, &record.ExecutionID, &record.CorrelationID, &record.OrganizationID,
		&record.AllocationIDSnapshot, &record.AllocationRefID, &record.UserIDSnapshot, &record.UserRefID, &record.APIKeyIDSnapshot, &record.APIKeyRefID,
		&record.EndpointFamily, &record.RequestShape, &record.Model, &record.ClientRequestID,
		&record.BillingFingerprint, &record.CandidateSnapshot, &record.CandidateSetHash,
		&record.PricingSnapshot, &record.PricingSnapshotHash, &record.CandidateCeiling,
		&record.IsBillable, &record.Status, &record.AcceptedAt, &record.TerminalizedAt,
	)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func loadNonBillableFinalEvidenceByExecution(ctx context.Context, client *dbent.Client, executionRefID int64, forUpdate bool) (*organizationNonBillableFinalEvidenceRecord, error) {
	query := `
		SELECT id, execution_ref_id, execution_id_snapshot, organization_id,
			allocation_id_snapshot, allocation_ref_id, user_id_snapshot, api_key_id_snapshot, final_attempt_id_snapshot,
			final_attempt_evidence_id_snapshot, terminal_outcome, is_business_limited,
			input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
			raw_actual_cost::text, organization_settled_cost::text, is_count_tokens_probe,
			zero_cost_proof::text, occurred_at, reconciled_at
		FROM organization_final_execution_evidences WHERE execution_ref_id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	record := &organizationNonBillableFinalEvidenceRecord{}
	err := scanExecutionOne(ctx, client, query, []any{executionRefID},
		&record.ID, &record.ExecutionRefID, &record.ExecutionID, &record.OrganizationID,
		&record.AllocationIDSnapshot, &record.AllocationRefID, &record.UserIDSnapshot, &record.APIKeyIDSnapshot, &record.FinalAttemptID,
		&record.FinalAttemptEvidence, &record.TerminalOutcome, &record.BusinessLimited,
		&record.InputTokens, &record.OutputTokens, &record.CacheCreationTokens, &record.CacheReadTokens,
		&record.RawActualCost, &record.SettledCost, &record.IsCountTokensProbe, &record.ZeroCostProof, &record.OccurredAt, &record.ReconciledAt,
	)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func sameNonBillableExecutionCommand(execution *organizationNonBillableExecutionRecord, command service.OrganizationNonBillableExecutionCommand) bool {
	if execution == nil || execution.IsBillable || execution.CandidateCeiling != "0" && execution.CandidateCeiling != "0.00000000" {
		return false
	}
	return execution.ExecutionID == command.ExecutionID && execution.CorrelationID == command.CorrelationID && execution.OrganizationID == command.Attribution.OrganizationID && execution.AllocationIDSnapshot.Valid && execution.AllocationIDSnapshot.Int64 == command.Attribution.AllocationID &&
		execution.UserIDSnapshot == command.Attribution.UserID && execution.APIKeyIDSnapshot == command.Attribution.APIKeyID && execution.EndpointFamily == command.EndpointFamily && execution.RequestShape == command.RequestShape &&
		execution.BillingFingerprint == command.BillingFingerprint && execution.CandidateSnapshot == command.CandidateSnapshotJSON && execution.CandidateSetHash == command.CandidateSetHash &&
		execution.PricingSnapshot == command.PricingSnapshotJSON && execution.PricingSnapshotHash == command.PricingSnapshotHash && nullableStringValue(execution.Model) == command.Model && nullableStringValue(execution.ClientRequestID) == command.ClientRequestID
}

func sameNonBillableAttemptEvidence(record *organizationAttemptEvidenceReplayRecord, command service.OrganizationNonBillableAttemptEvidenceCommand) bool {
	if record == nil || !record.AccountID.Valid {
		return false
	}
	recordCost, err := decimal.NewFromString(record.RawActualCost)
	if err != nil {
		return false
	}
	commandCost, err := decimal.NewFromString(command.RawActualCost)
	if err != nil {
		return false
	}
	ingestedAtMatches := record.IngestedAt.Equal(command.IngestedAt.UTC().Truncate(time.Microsecond)) || command.Provenance == service.OrganizationAttemptEvidenceProvenanceProviderQuery
	return record.AttemptID == command.AttemptID && record.ExecutionID == command.ExecutionID && record.OrganizationID == command.OrganizationID && record.AttemptSequence == command.AttemptSequence &&
		record.AccountID.Int64 == command.AccountID && record.RouteSnapshotHash == command.RouteSnapshotHash && record.PricingSnapshotHash == command.PricingSnapshotHash &&
		record.ProviderRequestID == command.ProviderRequestID && record.ProviderCorrelation == command.ProviderCorrelationID && record.EvidenceSource == command.Provenance &&
		record.TerminalOutcome == command.TerminalOutcome && record.ProvenanceHash == command.ProvenanceHash && record.EvidenceHash == command.EvidenceHash &&
		record.InputTokens == command.InputTokens && record.OutputTokens == command.OutputTokens && record.CacheCreationTokens == command.CacheCreationTokens && record.CacheReadTokens == command.CacheReadTokens &&
		recordCost.Equal(commandCost) && record.ProviderObservedAt.Equal(command.ProviderObservedAt.UTC().Truncate(time.Microsecond)) && ingestedAtMatches
}

func attemptMatchesNonBillableEvidence(attempt *organizationExecutionAttemptRecord, command service.OrganizationNonBillableAttemptEvidenceCommand, executionRefID int64) bool {
	return attempt != nil && attempt.ExecutionRefID == executionRefID && attempt.ExecutionID == command.ExecutionID && attempt.OrganizationID == command.OrganizationID && attempt.AttemptSequence == command.AttemptSequence &&
		attempt.AccountID.Valid && attempt.AccountID.Int64 == command.AccountID && attempt.RouteSnapshotHash == command.RouteSnapshotHash && attempt.PricingSnapshotHash == command.PricingSnapshotHash &&
		attempt.ProviderRequestID == command.ProviderRequestID && attempt.ProviderCorrelationID == command.ProviderCorrelationID
}

func attemptMatchesNonBillablePending(attempt *organizationExecutionAttemptRecord, command service.OrganizationNonBillablePendingAttemptCommand, executionRefID int64) bool {
	return attempt != nil && attempt.ExecutionRefID == executionRefID && attempt.ExecutionID == command.ExecutionID && attempt.OrganizationID == command.OrganizationID && attempt.AttemptSequence == command.AttemptSequence &&
		attempt.AccountID.Valid && attempt.AccountID.Int64 == command.AccountID && attempt.RouteSnapshotHash == command.RouteSnapshotHash && attempt.PricingSnapshotHash == command.PricingSnapshotHash &&
		attempt.ProviderRequestID == command.ProviderRequestID && attempt.ProviderCorrelationID == command.ProviderCorrelationID
}

func exactNonBillablePendingAttempt(ctx context.Context, client *dbent.Client, attempt *organizationExecutionAttemptRecord, command service.OrganizationNonBillablePendingAttemptCommand, executionRefID int64) (bool, error) {
	if !attemptMatchesNonBillablePending(attempt, command, executionRefID) {
		return false, nil
	}
	var dispatchedAt time.Time
	if err := scanExecutionOne(ctx, client, `SELECT dispatched_at FROM organization_execution_attempts WHERE id = $1`, []any{attempt.ID}, &dispatchedAt); err != nil {
		return false, executionStorageError("load non-billable pending dispatch timestamp", err)
	}
	return dispatchedAt.UTC().Truncate(time.Microsecond).Equal(command.DispatchedAt.UTC().Truncate(time.Microsecond)), nil
}

func sameNonBillableTerminalization(final *organizationNonBillableFinalEvidenceRecord, command service.OrganizationNonBillableTerminalizationCommand) bool {
	if final == nil || final.TerminalOutcome != command.TerminalOutcome || final.BusinessLimited != command.BusinessLimited || final.FinalAttemptID.Valid != (command.FinalAttemptID != nil) || final.FinalAttemptEvidence.Valid != (command.FinalAttemptEvidenceID != nil) {
		return false
	}
	if !final.OccurredAt.UTC().Truncate(time.Microsecond).Equal(command.OccurredAt.UTC().Truncate(time.Microsecond)) {
		return false
	}
	if command.FinalAttemptID != nil {
		return final.FinalAttemptID.String == *command.FinalAttemptID && final.FinalAttemptEvidence.Int64 == *command.FinalAttemptEvidenceID
	}
	if !final.ZeroCostProof.Valid {
		return false
	}
	var proof map[string]string
	if err := json.Unmarshal([]byte(final.ZeroCostProof.String), &proof); err != nil {
		return false
	}
	return len(proof) == 1 && strings.TrimSpace(proof["reason"]) == strings.TrimSpace(command.ZeroProof)
}

func nonBillableFinalEvidenceResult(execution *organizationNonBillableExecutionRecord, final *organizationNonBillableFinalEvidenceRecord, replayed bool) (*service.OrganizationNonBillableFinalExecutionEvidenceResult, error) {
	if execution == nil || final == nil || !execution.AllocationIDSnapshot.Valid || !final.AllocationIDSnapshot.Valid || final.AllocationIDSnapshot.Int64 != execution.AllocationIDSnapshot.Int64 || final.ExecutionRefID != execution.ID || final.ExecutionID != execution.ExecutionID || final.OrganizationID != execution.OrganizationID || final.UserIDSnapshot != execution.UserIDSnapshot || final.APIKeyIDSnapshot != execution.APIKeyIDSnapshot {
		return nil, service.ErrOrganizationExecutionInvariant
	}
	result := &service.OrganizationNonBillableFinalExecutionEvidenceResult{
		FinalEvidenceID: final.ID, ExecutionID: final.ExecutionID, CorrelationID: execution.CorrelationID, OrganizationID: final.OrganizationID,
		AllocationID: final.AllocationIDSnapshot.Int64, UserID: final.UserIDSnapshot, APIKeyID: final.APIKeyIDSnapshot, TerminalOutcome: final.TerminalOutcome, BusinessLimited: final.BusinessLimited,
		InputTokens: final.InputTokens, OutputTokens: final.OutputTokens, CacheCreationTokens: final.CacheCreationTokens, CacheReadTokens: final.CacheReadTokens,
		RawActualCost: final.RawActualCost, OrganizationSettledCost: fixedExecutionString(final.SettledCost), IsCountTokensProbe: final.IsCountTokensProbe,
		OccurredAt: final.OccurredAt, Replayed: replayed,
	}
	if final.FinalAttemptID.Valid {
		value := final.FinalAttemptID.String
		result.FinalAttemptID = &value
	}
	if final.FinalAttemptEvidence.Valid {
		value := final.FinalAttemptEvidence.Int64
		result.FinalAttemptEvidenceID = &value
	}
	if final.ZeroCostProof.Valid {
		var proof map[string]string
		if err := json.Unmarshal([]byte(final.ZeroCostProof.String), &proof); err != nil || len(proof) != 1 || strings.TrimSpace(proof["reason"]) == "" {
			return nil, service.ErrOrganizationExecutionInvariant
		}
		result.ZeroProof = strings.TrimSpace(proof["reason"])
	}
	return result, nil
}

func nullableStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func nonBillableDispatchFamily(value string) bool {
	return value == service.OrganizationEndpointFamilyModels || value == service.OrganizationEndpointFamilyTokenCounting
}

var _ service.OrganizationNonBillableExecutionRepository = (*organizationExecutionRepository)(nil)
var _ service.OrganizationNonBillableRecoveryEvidenceRepository = (*organizationExecutionRepository)(nil)
