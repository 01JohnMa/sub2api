package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	OrganizationExecutionStateAccepted              = "accepted"
	OrganizationExecutionStateReserved              = "reserved"
	OrganizationExecutionStateDispatched            = "dispatched"
	OrganizationExecutionStateFinalUsageObserved    = "final_usage_observed"
	OrganizationExecutionStatePendingReconciliation = "pending_reconciliation"
	OrganizationExecutionStateSettled               = "settled"
	OrganizationExecutionStateReleased              = "released"

	OrganizationReservationStateReserved              = "reserved"
	OrganizationReservationStatePendingReconciliation = "pending_reconciliation"
	OrganizationReservationStateSettled               = "settled"
	OrganizationReservationStateReleased              = "released"

	OrganizationExecutionTerminalOutcomeSuccess = "success"
	OrganizationExecutionTerminalOutcomeError   = "error"

	OrganizationAttemptEvidenceProvenanceAdapterDirect     = "adapter_direct"
	OrganizationAttemptEvidenceProvenanceProviderQuery     = "provider_query"
	OrganizationAttemptEvidenceTerminalOutcomeUnknown      = "unknown"
	OrganizationAttemptAccountEffectStatePending           = "pending"
	OrganizationAttemptAccountEffectStateApplied           = "applied"
	OrganizationAttemptAccountEffectStateDeletedTargetNoop = "applied_noop_deleted_target"
	OrganizationAttemptAccountEffectStateOperatorAttention = "operator_attention"
	OrganizationExecutionIncidentProvenanceConflict        = "provenance_conflict"
	OrganizationExecutionIncidentPlatformOverage           = "platform_overage"

	OrganizationExecutionReservationDispositionReserved            = "reserved"
	OrganizationExecutionReservationDispositionPreDispatchRejected = "pre_dispatch_rejected"
	OrganizationExecutionReservationDispositionFinalized           = "finalized"
)

const (
	organizationExecutionSnapshotMaxBytes = 64 * 1024
	organizationExecutionMetadataMaxBytes = 512
	organizationExecutionIdentifierMax    = 512
	organizationExecutionReasonMaxBytes   = 2000
)

var (
	ErrOrganizationExecutionRepositoryUnavailable = infraerrors.ServiceUnavailable(
		"ORGANIZATION_EXECUTION_REPOSITORY_UNAVAILABLE",
		"organization execution storage is unavailable",
	)
	ErrOrganizationExecutionInvalid = infraerrors.BadRequest(
		"ORGANIZATION_EXECUTION_INVALID",
		"organization execution request is invalid",
	)
	ErrOrganizationExecutionInvariant = infraerrors.InternalServer(
		"ORGANIZATION_EXECUTION_INVARIANT_BROKEN",
		"organization execution invariant is invalid",
	)
	ErrOrganizationExecutionConflict = infraerrors.Conflict(
		"ORGANIZATION_EXECUTION_CONFLICT",
		"organization execution conflicts with an existing billing fingerprint",
	)
	ErrOrganizationExecutionAdapterUnavailable = infraerrors.ServiceUnavailable(
		"ORGANIZATION_EXECUTION_ADAPTER_UNAVAILABLE",
		"organization execution adapter is unavailable",
	)
	ErrOrganizationExecutionPendingReconciliation = infraerrors.Conflict(
		"ORGANIZATION_EXECUTION_PENDING_RECONCILIATION",
		"organization execution has an unknown upstream outcome and requires reconciliation",
	)
	ErrOrganizationExecutionReconciliationForbidden = infraerrors.Forbidden(
		"ORGANIZATION_EXECUTION_RECONCILIATION_FORBIDDEN",
		"an active platform administrator JWT is required for organization reconciliation",
	)
)

// OrganizationExecutionTrustedAttribution is the server-captured identity
// snapshot from the authoritative API Key resolver. The repository rechecks
// every current row in the reservation transaction; these values are never
// accepted from a client request.
type OrganizationExecutionTrustedAttribution struct {
	OrganizationID                     int64
	OrganizationStatus                 string
	OrganizationBalanceVersionSnapshot int64
	UserID                             int64
	UserOrganizationID                 int64
	APIKeyID                           int64
	APIKeyUserID                       int64
	APIKeyOrganizationID               int64
	AllocationID                       int64
	AllocationUserID                   int64
	AllocationOrganizationID           int64
	AllocationStatus                   string
	AllocationVersionSnapshot          int64
}

// OrganizationExecutionReservationInput contains the trusted, immutable
// request facts needed before an upstream dispatch. ClientRequestID is only
// diagnostic metadata; BillingFingerprint is a server-computed SHA-256 hash.
type OrganizationExecutionReservationInput struct {
	Attribution               OrganizationExecutionTrustedAttribution
	EndpointFamily            string
	RequestShape              string
	ClientRequestID           string
	CandidateSnapshot         json.RawMessage
	PricingSnapshot           json.RawMessage
	CandidateRouteMaximumCost string
	BillingFingerprint        string
	PreDispatchZeroProof      string
}

// OrganizationExecutionReservationCommand is the repository transaction
// contract. It carries canonical JSON and hashes so the persistence layer can
// enforce a stable candidate/pricing ceiling without reparsing gateway input.
type OrganizationExecutionReservationCommand struct {
	ExecutionID               string
	CorrelationID             string
	Attribution               OrganizationExecutionTrustedAttribution
	EndpointFamily            string
	RequestShape              string
	ClientRequestID           string
	CandidateSnapshotJSON     string
	CandidateSetHash          string
	PricingSnapshotJSON       string
	PricingSnapshotHash       string
	CandidateRouteMaximumCost string
	BillingFingerprint        string
	PreDispatchZeroProof      string
	AcceptedAt                time.Time
}

// OrganizationFinalExecutionEvidenceResult is the repository-owned final
// fact. OrganizationSettledCost is deliberately returned, never accepted by
// Terminalize callers: the transaction derives it from durable evidence and
// the reservation ceiling.
type OrganizationFinalExecutionEvidenceResult struct {
	FinalEvidenceID         int64
	ExecutionID             string
	CorrelationID           string
	OrganizationID          int64
	UserID                  int64
	APIKeyID                int64
	AllocationID            int64
	ReservationID           int64
	FinalAttemptID          *string
	FinalAttemptEvidenceID  *int64
	TerminalOutcome         string
	BusinessLimited         bool
	RawActualCost           string
	OrganizationSettledCost string
	ZeroProof               string
	ReservationState        string
	ConsumptionPostingID    *int64
	OccurredAt              time.Time
	ReconciledAt            *time.Time
	Replayed                bool
}

// OrganizationExecutionReservationResult distinguishes a successful durable
// reservation from a stale four-way attribution rejection. The latter must
// already contain the zero-cost final evidence and must not create a
// reservation or dispatch an upstream request.
type OrganizationExecutionReservationResult struct {
	ExecutionID    string
	CorrelationID  string
	OrganizationID int64
	UserID         int64
	APIKeyID       int64
	AllocationID   int64
	ReservationID  int64
	ReservedAmount string
	State          string
	Disposition    string
	FinalEvidence  *OrganizationFinalExecutionEvidenceResult
	Replayed       bool
}

// OrganizationExecutionDispatchRequest is supplied to a gateway adapter only
// after a durable reservation. The adapter must use the generated IDs rather
// than a client request ID for provider correlation and attempt evidence.
type OrganizationExecutionDispatchRequest struct {
	ExecutionID               string
	AttemptID                 string
	CorrelationID             string
	ProviderCorrelationID     string
	ProviderRequestID         string
	AttemptSequence           int
	ReservationID             int64
	OrganizationID            int64
	AllocationID              int64
	EndpointFamily            string
	RequestShape              string
	CandidateSnapshotJSON     string
	CandidateSetHash          string
	PricingSnapshotJSON       string
	PricingSnapshotHash       string
	CandidateRouteMaximumCost string
}

// OrganizationExecutionAttemptObservation is a trusted adapter observation.
// It is intentionally evidence-oriented and contains no facility to choose an
// organization settlement amount.
type OrganizationExecutionAttemptObservation struct {
	AccountID             int64
	RouteSnapshot         json.RawMessage
	ProviderRequestID     string
	ProviderCorrelationID string
	Evidence              json.RawMessage
	RawActualCost         string
	InputTokens           int64
	OutputTokens          int64
	CacheCreationTokens   int64
	CacheReadTokens       int64
	ProviderObservedAt    time.Time
}

// OrganizationExecutionAttemptResult models a single adapter dispatch. A
// non-dispatched result is only valid with a durable zero-cost proof, which
// lets the service terminalize a known pre-dispatch error. Unknown dispatch
// failures remain an adapter error and are not blindly released.
type OrganizationExecutionAttemptResult struct {
	Dispatched            bool
	Observation           *OrganizationExecutionAttemptObservation
	TerminalOutcome       string
	BusinessLimited       bool
	ZeroProof             string
	Retryable             bool
	PendingReconciliation bool
}

// OrganizationAttemptEvidenceCommand persists one immutable dispatch attempt
// before final execution settlement. AttemptSequence is positive and increases
// across failover attempts sharing the same parent reservation.
type OrganizationAttemptEvidenceCommand struct {
	ExecutionID           string
	AttemptID             string
	CorrelationID         string
	ReservationID         int64
	OrganizationID        int64
	UserID                int64
	APIKeyID              int64
	AllocationID          int64
	AttemptSequence       int
	AccountID             int64
	RouteSnapshotJSON     string
	RouteSnapshotHash     string
	PricingSnapshotJSON   string
	PricingSnapshotHash   string
	ProviderRequestID     string
	ProviderCorrelationID string
	Provenance            string
	TerminalOutcome       string
	ProvenanceHash        string
	EvidenceJSON          string
	EvidenceHash          string
	RawActualCost         string
	InputTokens           int64
	OutputTokens          int64
	CacheCreationTokens   int64
	CacheReadTokens       int64
	ProviderObservedAt    time.Time
	IngestedAt            time.Time
}

// OrganizationPendingAttemptCommand records a dispatch whose provider outcome
// is not yet known. It preserves the server-generated attempt/provider IDs so a
// later reconciler can ingest authoritative evidence without dispatching again.
type OrganizationPendingAttemptCommand struct {
	ExecutionID           string
	AttemptID             string
	CorrelationID         string
	ReservationID         int64
	OrganizationID        int64
	UserID                int64
	APIKeyID              int64
	AllocationID          int64
	AttemptSequence       int
	AccountID             int64
	RouteSnapshotJSON     string
	RouteSnapshotHash     string
	PricingSnapshotJSON   string
	PricingSnapshotHash   string
	ProviderRequestID     string
	ProviderCorrelationID string
	DispatchedAt          time.Time
}

type OrganizationAttemptEvidenceResult struct {
	AttemptEvidenceID int64
	ExecutionID       string
	AttemptID         string
	Replayed          bool
}

// OrganizationTerminalizationCommand cannot contain settled cost. A final
// attempt reference derives a billable or zero-usage final evidence; a nil
// final attempt requires a durable no-dispatch zero proof.
type OrganizationTerminalizationCommand struct {
	ExecutionID            string
	CorrelationID          string
	OrganizationID         int64
	UserID                 int64
	APIKeyID               int64
	AllocationID           int64
	ReservationID          int64
	FinalAttemptID         *string
	FinalAttemptEvidenceID *int64
	TerminalOutcome        string
	BusinessLimited        bool
	ZeroProof              string
	OccurredAt             time.Time
	ReconciledAt           *time.Time
}

// OrganizationExecutionRepository is intentionally small. Its methods own
// all financial SQL transactions, locks, idempotency and outbox writes.
type OrganizationExecutionRepository interface {
	Reserve(ctx context.Context, command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error)
	RecordAttemptEvidence(ctx context.Context, command OrganizationAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error)
	Terminalize(ctx context.Context, command OrganizationTerminalizationCommand) (*OrganizationFinalExecutionEvidenceResult, error)
}

// OrganizationExecutionRecoveryRepository is an optional extension of the
// Ticket 09 repository seam. Keeping it separate preserves existing fakes and
// callers while allowing Ticket 10 recovery workers to be introduced safely.
type OrganizationExecutionRecoveryRepository interface {
	RecordPendingAttempt(ctx context.Context, command OrganizationPendingAttemptCommand) error
	ReconcilePendingExecution(ctx context.Context, command OrganizationPendingReconciliationCommand) (*OrganizationFinalExecutionEvidenceResult, error)
	RedriveAttemptAccountEffect(ctx context.Context, attemptID string) (*OrganizationAttemptAccountEffectResult, error)
	RedrivePendingAttemptAccountEffects(ctx context.Context, limit int) ([]OrganizationAttemptAccountEffectResult, error)
	RedriveExecutionOutbox(ctx context.Context, outboxID int64) (*OrganizationExecutionOutboxApplyResult, error)
}

// OrganizationExecutionAutomaticEffectRepository is an optional runtime seam
// for applying the durable side-effect records created by attempt and final
// evidence. Failures leave those records pending for the recovery control plane.
type OrganizationExecutionAutomaticEffectRepository interface {
	RedrivePendingExecutionEffects(ctx context.Context, limit int) ([]OrganizationExecutionOutboxApplyResult, error)
}

type OrganizationPendingReconciliationCommand struct {
	ExecutionID          string
	OperationID          string
	CorrelationID        string
	IdempotencyKeyHash   string
	CanonicalPayloadHash string
	Actor                OrganizationReconciliationActor
	ReconciledAt         time.Time
}

type OrganizationReconciliationActor struct {
	UserID             int64
	DisplayName        string
	PlatformRole       string
	AuthorizationScope string
	Active             bool
}

type OrganizationAttemptAccountEffectResult struct {
	EffectID            int64
	AttemptID           string
	ExecutionID         string
	AccountID           int64
	Status              string
	PostingID           *int64
	RawActualCost       string
	PlatformOverageCost string
	DeletedTargetNoop   bool
	Replayed            bool
}

type OrganizationExecutionOutboxApplyResult struct {
	OutboxID          int64
	ExecutionID       string
	EventType         string
	Status            string
	DeletedTargetNoop bool
	Replayed          bool
}

type OrganizationExecutionAttemptRun struct {
	AttemptID       string
	AttemptSequence int
	EvidenceID      *int64
	TerminalOutcome string
	Retryable       bool
}

type OrganizationExecutionAttemptRunResult struct {
	ExecutionID           string
	CorrelationID         string
	Attempts              []OrganizationExecutionAttemptRun
	Final                 *OrganizationFinalExecutionEvidenceResult
	PendingReconciliation bool
}

// OrganizationSingleAttemptAdapter is a fake-adapter-compatible contract. It
// is not connected to any real endpoint by this ticket. An adapter that knows
// the upstream request was sent but lost the response must return a dispatched
// pending result together with its transport error so the service can persist
// the attempt and move the parent execution to reconciliation.
type OrganizationSingleAttemptAdapter interface {
	Dispatch(ctx context.Context, request OrganizationExecutionDispatchRequest) (*OrganizationExecutionAttemptResult, error)
}

type OrganizationExecutionManager interface {
	NewReservationCommand(input OrganizationExecutionReservationInput) (*OrganizationExecutionReservationCommand, error)
	Reserve(ctx context.Context, command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error)
	ExecuteSingleAttempt(ctx context.Context, command OrganizationExecutionReservationCommand, adapter OrganizationSingleAttemptAdapter) (*OrganizationFinalExecutionEvidenceResult, error)
}

type OrganizationExecutionService struct {
	repo OrganizationExecutionRepository
	now  func() time.Time
}

func NewOrganizationExecutionService(repo OrganizationExecutionRepository) *OrganizationExecutionService {
	return &OrganizationExecutionService{
		repo: repo,
		now:  func() time.Time { return time.Now().UTC() },
	}
}

// NewReservationCommand creates server-owned execution and correlation UUIDs.
// Replays must keep this command unchanged; creating a new command starts a
// different execution and is therefore not an idempotent retry.
func (s *OrganizationExecutionService) NewReservationCommand(input OrganizationExecutionReservationInput) (*OrganizationExecutionReservationCommand, error) {
	command, err := newOrganizationExecutionReservationCommand(input, s.clockNow())
	if err != nil {
		return nil, err
	}
	return &command, nil
}

func (s *OrganizationExecutionService) Reserve(ctx context.Context, command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := ValidateOrganizationExecutionReservationCommand(command); err != nil {
		return nil, err
	}
	result, err := s.repo.Reserve(ctx, command)
	if err != nil {
		return nil, err
	}
	if err := ValidateOrganizationExecutionReservationResult(command, result); err != nil {
		return nil, err
	}
	return result, nil
}

// ExecuteSingleAttempt preserves the Ticket 09 API while delegating to the
// Ticket 10 multi-attempt state machine with a one-attempt ceiling.
func (s *OrganizationExecutionService) ExecuteSingleAttempt(
	ctx context.Context,
	command OrganizationExecutionReservationCommand,
	adapter OrganizationSingleAttemptAdapter,
) (*OrganizationFinalExecutionEvidenceResult, error) {
	run, err := s.ExecuteMultiAttempt(ctx, command, 1, adapter)
	if err != nil {
		return nil, err
	}
	if run == nil || run.Final == nil {
		return nil, ErrOrganizationExecutionPendingReconciliation
	}
	return run.Final, nil
}

// ExecuteMultiAttempt reserves once and records one immutable evidence stream
// per dispatch. Retryable failed attempts remain platform cost only; the final
// known attempt settles the member once. An unknown post-dispatch outcome is
// durably moved to pending reconciliation and is never released by age.
func (s *OrganizationExecutionService) ExecuteMultiAttempt(
	ctx context.Context,
	command OrganizationExecutionReservationCommand,
	maxAttempts int,
	adapter OrganizationSingleAttemptAdapter,
) (*OrganizationExecutionAttemptRunResult, error) {
	if maxAttempts <= 0 {
		return nil, ErrOrganizationExecutionInvalid
	}
	reservation, err := s.Reserve(ctx, command)
	if err != nil {
		return nil, err
	}
	if reservation.FinalEvidence != nil {
		finalEvidence := *reservation.FinalEvidence
		finalEvidence.Replayed = finalEvidence.Replayed || reservation.Replayed
		s.redriveExecutionEffects(ctx)
		return &OrganizationExecutionAttemptRunResult{
			ExecutionID:   command.ExecutionID,
			CorrelationID: command.CorrelationID,
			Final:         &finalEvidence,
		}, nil
	}
	if adapter == nil {
		return nil, ErrOrganizationExecutionAdapterUnavailable
	}

	run := &OrganizationExecutionAttemptRunResult{
		ExecutionID:   command.ExecutionID,
		CorrelationID: command.CorrelationID,
		Attempts:      make([]OrganizationExecutionAttemptRun, 0, maxAttempts),
	}
	for sequence := 1; sequence <= maxAttempts; sequence++ {
		attemptID := uuid.NewString()
		providerCorrelationID := uuid.NewString()
		providerRequestID := uuid.NewString()
		dispatchResult, dispatchErr := adapter.Dispatch(ctx, OrganizationExecutionDispatchRequest{
			ExecutionID:               command.ExecutionID,
			AttemptID:                 attemptID,
			CorrelationID:             command.CorrelationID,
			ProviderCorrelationID:     providerCorrelationID,
			ProviderRequestID:         providerRequestID,
			AttemptSequence:           sequence,
			ReservationID:             reservation.ReservationID,
			OrganizationID:            command.Attribution.OrganizationID,
			AllocationID:              command.Attribution.AllocationID,
			EndpointFamily:            command.EndpointFamily,
			RequestShape:              command.RequestShape,
			CandidateSnapshotJSON:     command.CandidateSnapshotJSON,
			CandidateSetHash:          command.CandidateSetHash,
			PricingSnapshotJSON:       command.PricingSnapshotJSON,
			PricingSnapshotHash:       command.PricingSnapshotHash,
			CandidateRouteMaximumCost: command.CandidateRouteMaximumCost,
		})
		if dispatchErr != nil && (dispatchResult == nil || !dispatchResult.PendingReconciliation) {
			return nil, dispatchErr
		}
		if err := ValidateOrganizationExecutionAttemptResult(dispatchResult); err != nil {
			return nil, err
		}

		terminal := OrganizationTerminalizationCommand{
			ExecutionID:     command.ExecutionID,
			CorrelationID:   command.CorrelationID,
			OrganizationID:  command.Attribution.OrganizationID,
			UserID:          command.Attribution.UserID,
			APIKeyID:        command.Attribution.APIKeyID,
			AllocationID:    command.Attribution.AllocationID,
			ReservationID:   reservation.ReservationID,
			TerminalOutcome: dispatchResult.TerminalOutcome,
			BusinessLimited: dispatchResult.BusinessLimited,
			OccurredAt:      s.clockNow(),
		}
		if !dispatchResult.Dispatched {
			if dispatchResult.Retryable || dispatchResult.PendingReconciliation {
				return nil, ErrOrganizationExecutionInvalid
			}
			terminal.ZeroProof = strings.TrimSpace(dispatchResult.ZeroProof)
			final, err := s.terminalize(ctx, command, terminal)
			if err != nil {
				return nil, err
			}
			run.Final = final
			return run, nil
		}

		if dispatchResult.PendingReconciliation {
			recovery, ok := s.repo.(OrganizationExecutionRecoveryRepository)
			if !ok {
				return nil, ErrOrganizationExecutionRepositoryUnavailable
			}
			pendingCommand, err := newOrganizationPendingAttemptCommand(
				command, reservation, attemptID, providerRequestID, providerCorrelationID,
				sequence, dispatchResult.Observation, s.clockNow(),
			)
			if err != nil {
				return nil, err
			}
			if err := recovery.RecordPendingAttempt(ctx, pendingCommand); err != nil {
				return nil, err
			}
			run.Attempts = append(run.Attempts, OrganizationExecutionAttemptRun{
				AttemptID:       attemptID,
				AttemptSequence: sequence,
				TerminalOutcome: OrganizationAttemptEvidenceTerminalOutcomeUnknown,
			})
			run.PendingReconciliation = true
			return run, nil
		}

		attemptCommand, err := newOrganizationAttemptEvidenceCommand(
			command, reservation, attemptID, providerRequestID, providerCorrelationID,
			sequence, dispatchResult.TerminalOutcome, dispatchResult.Observation, s.clockNow(),
		)
		if err != nil {
			return nil, err
		}
		attemptEvidence, err := s.repo.RecordAttemptEvidence(ctx, attemptCommand)
		if err != nil {
			return nil, err
		}
		if err := ValidateOrganizationAttemptEvidenceResult(attemptCommand, attemptEvidence); err != nil {
			return nil, err
		}
		s.redriveAttemptAccountEffect(ctx, attemptCommand.AttemptID)
		evidenceID := attemptEvidence.AttemptEvidenceID
		run.Attempts = append(run.Attempts, OrganizationExecutionAttemptRun{
			AttemptID:       attemptCommand.AttemptID,
			AttemptSequence: sequence,
			EvidenceID:      &evidenceID,
			TerminalOutcome: dispatchResult.TerminalOutcome,
			Retryable:       dispatchResult.Retryable,
		})
		if dispatchResult.Retryable && sequence < maxAttempts {
			continue
		}
		terminal.FinalAttemptID = pointerToString(attemptCommand.AttemptID)
		terminal.FinalAttemptEvidenceID = pointerToInt64(attemptEvidence.AttemptEvidenceID)
		final, err := s.terminalize(ctx, command, terminal)
		if err != nil {
			return nil, err
		}
		run.Final = final
		return run, nil
	}
	return nil, ErrOrganizationExecutionInvariant
}

func (s *OrganizationExecutionService) terminalize(
	ctx context.Context,
	reservation OrganizationExecutionReservationCommand,
	command OrganizationTerminalizationCommand,
) (*OrganizationFinalExecutionEvidenceResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := ValidateOrganizationTerminalizationCommand(command); err != nil {
		return nil, err
	}
	result, err := s.repo.Terminalize(ctx, command)
	if err != nil {
		return nil, err
	}
	if err := ValidateOrganizationFinalExecutionEvidenceResult(reservation, command, result); err != nil {
		return nil, err
	}
	s.redriveExecutionEffects(ctx)
	return result, nil
}

func (s *OrganizationExecutionService) redriveAttemptAccountEffect(ctx context.Context, attemptID string) {
	if s == nil || s.repo == nil {
		return
	}
	recovery, ok := s.repo.(OrganizationExecutionRecoveryRepository)
	if !ok {
		return
	}
	_, _ = recovery.RedriveAttemptAccountEffect(ctx, attemptID)
	_, _ = recovery.RedrivePendingAttemptAccountEffects(ctx, 100)
}

func (s *OrganizationExecutionService) redriveExecutionEffects(ctx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	repository, ok := s.repo.(OrganizationExecutionAutomaticEffectRepository)
	if !ok {
		return
	}
	_, _ = repository.RedrivePendingExecutionEffects(ctx, 100)
}

// OrganizationExecutionFakeAdapter is a static, in-process adapter for the
// Ticket 10 multi-attempt contract tests. It never invokes an upstream endpoint.
type OrganizationExecutionFakeAdapter struct {
	Result OrganizationExecutionAttemptResult
	Err    error
}

func (a OrganizationExecutionFakeAdapter) Dispatch(_ context.Context, request OrganizationExecutionDispatchRequest) (*OrganizationExecutionAttemptResult, error) {
	if a.Err != nil {
		return nil, a.Err
	}
	result := a.Result
	if result.Observation != nil {
		observation := *result.Observation
		if observation.ProviderCorrelationID == "" {
			observation.ProviderCorrelationID = request.ProviderCorrelationID
		}
		if observation.ProviderRequestID == "" {
			observation.ProviderRequestID = request.ProviderRequestID
		}
		result.Observation = &observation
	}
	return &result, nil
}

func newOrganizationExecutionReservationCommand(input OrganizationExecutionReservationInput, now time.Time) (OrganizationExecutionReservationCommand, error) {
	if err := ValidateOrganizationExecutionTrustedAttribution(input.Attribution); err != nil {
		return OrganizationExecutionReservationCommand{}, err
	}
	candidateSnapshotJSON, candidateSetHash, err := canonicalOrganizationExecutionJSON(input.CandidateSnapshot)
	if err != nil {
		return OrganizationExecutionReservationCommand{}, err
	}
	pricingSnapshotJSON, pricingSnapshotHash, err := canonicalOrganizationExecutionJSON(input.PricingSnapshot)
	if err != nil {
		return OrganizationExecutionReservationCommand{}, err
	}
	preDispatchZeroProof, err := normalizeOrganizationExecutionIdentifier(input.PreDispatchZeroProof, organizationExecutionReasonMaxBytes, true)
	if err != nil {
		return OrganizationExecutionReservationCommand{}, err
	}
	ceiling, err := NormalizeOrganizationMoney(input.CandidateRouteMaximumCost, preDispatchZeroProof == "")
	if err != nil {
		return OrganizationExecutionReservationCommand{}, ErrOrganizationExecutionInvalid.WithCause(err)
	}
	endpointFamily, err := normalizeOrganizationExecutionIdentifier(input.EndpointFamily, 128, false)
	if err != nil {
		return OrganizationExecutionReservationCommand{}, err
	}
	requestShape, err := normalizeOrganizationExecutionIdentifier(input.RequestShape, 128, false)
	if err != nil {
		return OrganizationExecutionReservationCommand{}, err
	}
	clientRequestID, err := normalizeOrganizationExecutionIdentifier(input.ClientRequestID, organizationExecutionMetadataMaxBytes, true)
	if err != nil {
		return OrganizationExecutionReservationCommand{}, err
	}
	if preDispatchZeroProof != "" && ceiling != "0.00000000" {
		return OrganizationExecutionReservationCommand{}, ErrOrganizationExecutionInvalid
	}
	fingerprint := strings.ToLower(strings.TrimSpace(input.BillingFingerprint))
	if !validSHA256(fingerprint) {
		return OrganizationExecutionReservationCommand{}, ErrOrganizationExecutionInvalid
	}
	if now.IsZero() {
		return OrganizationExecutionReservationCommand{}, ErrOrganizationExecutionInvariant
	}
	return OrganizationExecutionReservationCommand{
		ExecutionID:               uuid.NewString(),
		CorrelationID:             uuid.NewString(),
		Attribution:               input.Attribution,
		EndpointFamily:            endpointFamily,
		RequestShape:              requestShape,
		ClientRequestID:           clientRequestID,
		CandidateSnapshotJSON:     candidateSnapshotJSON,
		CandidateSetHash:          candidateSetHash,
		PricingSnapshotJSON:       pricingSnapshotJSON,
		PricingSnapshotHash:       pricingSnapshotHash,
		CandidateRouteMaximumCost: ceiling,
		BillingFingerprint:        fingerprint,
		PreDispatchZeroProof:      preDispatchZeroProof,
		AcceptedAt:                now.UTC(),
	}, nil
}

func newOrganizationAttemptEvidenceCommand(
	reservation OrganizationExecutionReservationCommand,
	reserved *OrganizationExecutionReservationResult,
	attemptID, providerRequestID, providerCorrelationID string,
	attemptSequence int,
	terminalOutcome string,
	observation *OrganizationExecutionAttemptObservation,
	now time.Time,
) (OrganizationAttemptEvidenceCommand, error) {
	if reserved == nil || observation == nil || now.IsZero() {
		return OrganizationAttemptEvidenceCommand{}, ErrOrganizationExecutionInvariant
	}
	routeSnapshotJSON, routeSnapshotHash, err := canonicalOrganizationExecutionJSON(observation.RouteSnapshot)
	if err != nil {
		return OrganizationAttemptEvidenceCommand{}, err
	}
	evidenceJSON, evidenceHash, err := canonicalOrganizationExecutionJSON(observation.Evidence)
	if err != nil {
		return OrganizationAttemptEvidenceCommand{}, err
	}
	rawActualCost, err := NormalizeOrganizationRawActualCost(observation.RawActualCost)
	if err != nil {
		return OrganizationAttemptEvidenceCommand{}, err
	}
	observedProviderRequestID, err := normalizeOrganizationExecutionIdentifier(observation.ProviderRequestID, organizationExecutionIdentifierMax, false)
	if err != nil {
		return OrganizationAttemptEvidenceCommand{}, err
	}
	if observedProviderRequestID != providerRequestID {
		return OrganizationAttemptEvidenceCommand{}, ErrOrganizationExecutionInvariant
	}
	if observation.ProviderCorrelationID != providerCorrelationID {
		return OrganizationAttemptEvidenceCommand{}, ErrOrganizationExecutionInvariant
	}
	if attemptSequence <= 0 || observation.AccountID <= 0 || observation.InputTokens < 0 || observation.OutputTokens < 0 || observation.CacheCreationTokens < 0 || observation.CacheReadTokens < 0 || observation.ProviderObservedAt.IsZero() || !validOrganizationAttemptTerminalOutcome(terminalOutcome) {
		return OrganizationAttemptEvidenceCommand{}, ErrOrganizationExecutionInvalid
	}
	return OrganizationAttemptEvidenceCommand{
		ExecutionID:           reservation.ExecutionID,
		AttemptID:             attemptID,
		CorrelationID:         reservation.CorrelationID,
		ReservationID:         reserved.ReservationID,
		OrganizationID:        reservation.Attribution.OrganizationID,
		UserID:                reservation.Attribution.UserID,
		APIKeyID:              reservation.Attribution.APIKeyID,
		AllocationID:          reservation.Attribution.AllocationID,
		AttemptSequence:       attemptSequence,
		AccountID:             observation.AccountID,
		RouteSnapshotJSON:     routeSnapshotJSON,
		RouteSnapshotHash:     routeSnapshotHash,
		PricingSnapshotJSON:   reservation.PricingSnapshotJSON,
		PricingSnapshotHash:   reservation.PricingSnapshotHash,
		ProviderRequestID:     observedProviderRequestID,
		ProviderCorrelationID: providerCorrelationID,
		Provenance:            OrganizationAttemptEvidenceProvenanceAdapterDirect,
		TerminalOutcome:       terminalOutcome,
		ProvenanceHash:        evidenceHash,
		EvidenceJSON:          evidenceJSON,
		EvidenceHash:          evidenceHash,
		RawActualCost:         rawActualCost,
		InputTokens:           observation.InputTokens,
		OutputTokens:          observation.OutputTokens,
		CacheCreationTokens:   observation.CacheCreationTokens,
		CacheReadTokens:       observation.CacheReadTokens,
		ProviderObservedAt:    observation.ProviderObservedAt.UTC(),
		IngestedAt:            now.UTC(),
	}, nil
}

func newOrganizationPendingAttemptCommand(
	reservation OrganizationExecutionReservationCommand,
	reserved *OrganizationExecutionReservationResult,
	attemptID, providerRequestID, providerCorrelationID string,
	attemptSequence int,
	observation *OrganizationExecutionAttemptObservation,
	now time.Time,
) (OrganizationPendingAttemptCommand, error) {
	if reserved == nil || observation == nil || now.IsZero() || attemptSequence <= 0 || observation.AccountID <= 0 {
		return OrganizationPendingAttemptCommand{}, ErrOrganizationExecutionInvalid
	}
	routeSnapshotJSON, routeSnapshotHash, err := canonicalOrganizationExecutionJSON(observation.RouteSnapshot)
	if err != nil {
		return OrganizationPendingAttemptCommand{}, err
	}
	observedProviderRequestID, err := normalizeOrganizationExecutionIdentifier(observation.ProviderRequestID, organizationExecutionIdentifierMax, false)
	if err != nil {
		return OrganizationPendingAttemptCommand{}, err
	}
	if observedProviderRequestID != providerRequestID || observation.ProviderCorrelationID != providerCorrelationID {
		return OrganizationPendingAttemptCommand{}, ErrOrganizationExecutionInvariant
	}
	return OrganizationPendingAttemptCommand{
		ExecutionID:           reservation.ExecutionID,
		AttemptID:             attemptID,
		CorrelationID:         reservation.CorrelationID,
		ReservationID:         reserved.ReservationID,
		OrganizationID:        reservation.Attribution.OrganizationID,
		UserID:                reservation.Attribution.UserID,
		APIKeyID:              reservation.Attribution.APIKeyID,
		AllocationID:          reservation.Attribution.AllocationID,
		AttemptSequence:       attemptSequence,
		AccountID:             observation.AccountID,
		RouteSnapshotJSON:     routeSnapshotJSON,
		RouteSnapshotHash:     routeSnapshotHash,
		PricingSnapshotJSON:   reservation.PricingSnapshotJSON,
		PricingSnapshotHash:   reservation.PricingSnapshotHash,
		ProviderRequestID:     observedProviderRequestID,
		ProviderCorrelationID: providerCorrelationID,
		DispatchedAt:          now.UTC(),
	}, nil
}

func ValidateOrganizationExecutionTrustedAttribution(attribution OrganizationExecutionTrustedAttribution) error {
	if attribution.OrganizationID <= 0 || attribution.OrganizationStatus != OrganizationStatusActive || attribution.OrganizationBalanceVersionSnapshot < 0 ||
		attribution.UserID <= 0 || attribution.UserOrganizationID != attribution.OrganizationID ||
		attribution.APIKeyID <= 0 || attribution.APIKeyUserID != attribution.UserID || attribution.APIKeyOrganizationID != attribution.OrganizationID ||
		attribution.AllocationID <= 0 || attribution.AllocationUserID != attribution.UserID || attribution.AllocationOrganizationID != attribution.OrganizationID ||
		attribution.AllocationStatus != domain.OrganizationAllocationStatusActive || attribution.AllocationVersionSnapshot < 0 {
		return ErrOrganizationExecutionInvalid
	}
	return nil
}

func ValidateOrganizationExecutionReservationCommand(command OrganizationExecutionReservationCommand) error {
	if err := ValidateOrganizationExecutionTrustedAttribution(command.Attribution); err != nil {
		return err
	}
	if !validOrganizationExecutionUUID(command.ExecutionID) || !validOrganizationExecutionUUID(command.CorrelationID) || command.ExecutionID == command.CorrelationID || command.AcceptedAt.IsZero() {
		return ErrOrganizationExecutionInvalid
	}
	if _, err := normalizeOrganizationExecutionIdentifier(command.EndpointFamily, 128, false); err != nil {
		return err
	}
	if _, err := normalizeOrganizationExecutionIdentifier(command.RequestShape, 128, false); err != nil {
		return err
	}
	if _, err := normalizeOrganizationExecutionIdentifier(command.ClientRequestID, organizationExecutionMetadataMaxBytes, true); err != nil {
		return err
	}
	if err := validateCanonicalOrganizationExecutionJSON(command.CandidateSnapshotJSON, command.CandidateSetHash); err != nil {
		return err
	}
	if err := validateCanonicalOrganizationExecutionJSON(command.PricingSnapshotJSON, command.PricingSnapshotHash); err != nil {
		return err
	}
	proof, err := normalizeOrganizationExecutionIdentifier(command.PreDispatchZeroProof, organizationExecutionReasonMaxBytes, true)
	if err != nil || proof != command.PreDispatchZeroProof {
		return ErrOrganizationExecutionInvalid
	}
	if canonical, err := NormalizeOrganizationMoney(command.CandidateRouteMaximumCost, proof == ""); err != nil || canonical != command.CandidateRouteMaximumCost {
		return ErrOrganizationExecutionInvalid
	}
	if fingerprint := strings.ToLower(strings.TrimSpace(command.BillingFingerprint)); fingerprint != command.BillingFingerprint || !validSHA256(fingerprint) {
		return ErrOrganizationExecutionInvalid
	}
	if proof != "" && command.CandidateRouteMaximumCost != "0.00000000" {
		return ErrOrganizationExecutionInvalid
	}
	return nil
}

func ValidateOrganizationExecutionReservationResult(command OrganizationExecutionReservationCommand, result *OrganizationExecutionReservationResult) error {
	if result == nil || result.ExecutionID != command.ExecutionID || result.CorrelationID != command.CorrelationID ||
		result.OrganizationID != command.Attribution.OrganizationID || result.UserID != command.Attribution.UserID || result.APIKeyID != command.Attribution.APIKeyID || result.AllocationID != command.Attribution.AllocationID {
		return ErrOrganizationExecutionInvariant
	}
	switch result.Disposition {
	case OrganizationExecutionReservationDispositionReserved:
		if result.ReservationID <= 0 || result.State != OrganizationReservationStateReserved || result.FinalEvidence != nil {
			return ErrOrganizationExecutionInvariant
		}
		if canonical, err := NormalizeOrganizationMoney(result.ReservedAmount, true); err != nil || canonical != result.ReservedAmount || result.ReservedAmount != command.CandidateRouteMaximumCost {
			return ErrOrganizationExecutionInvariant
		}
		return nil
	case OrganizationExecutionReservationDispositionPreDispatchRejected:
		if result.ReservationID != 0 || result.State != "" || result.ReservedAmount != "" || result.FinalEvidence == nil {
			return ErrOrganizationExecutionInvariant
		}
		if strings.TrimSpace(result.FinalEvidence.ZeroProof) == "" {
			return ErrOrganizationExecutionInvariant
		}
		finalCommand := OrganizationTerminalizationCommand{
			ExecutionID:     command.ExecutionID,
			CorrelationID:   command.CorrelationID,
			OrganizationID:  command.Attribution.OrganizationID,
			UserID:          command.Attribution.UserID,
			APIKeyID:        command.Attribution.APIKeyID,
			AllocationID:    command.Attribution.AllocationID,
			TerminalOutcome: OrganizationExecutionTerminalOutcomeError,
			ZeroProof:       result.FinalEvidence.ZeroProof,
			OccurredAt:      result.FinalEvidence.OccurredAt,
		}
		return ValidateOrganizationFinalExecutionEvidenceResult(command, finalCommand, result.FinalEvidence)
	case OrganizationExecutionReservationDispositionFinalized:
		if result.ReservationID <= 0 || result.FinalEvidence == nil || result.State != result.FinalEvidence.ReservationState {
			return ErrOrganizationExecutionInvariant
		}
		if canonical, err := NormalizeOrganizationMoney(result.ReservedAmount, true); err != nil || canonical != result.ReservedAmount || result.ReservedAmount != command.CandidateRouteMaximumCost {
			return ErrOrganizationExecutionInvariant
		}
		finalCommand := OrganizationTerminalizationCommand{
			ExecutionID:            command.ExecutionID,
			CorrelationID:          command.CorrelationID,
			OrganizationID:         command.Attribution.OrganizationID,
			UserID:                 command.Attribution.UserID,
			APIKeyID:               command.Attribution.APIKeyID,
			AllocationID:           command.Attribution.AllocationID,
			ReservationID:          result.ReservationID,
			FinalAttemptID:         result.FinalEvidence.FinalAttemptID,
			FinalAttemptEvidenceID: result.FinalEvidence.FinalAttemptEvidenceID,
			TerminalOutcome:        result.FinalEvidence.TerminalOutcome,
			BusinessLimited:        result.FinalEvidence.BusinessLimited,
			OccurredAt:             result.FinalEvidence.OccurredAt,
			ReconciledAt:           result.FinalEvidence.ReconciledAt,
		}
		if finalCommand.FinalAttemptID == nil {
			finalCommand.ZeroProof = result.FinalEvidence.ZeroProof
		}
		return ValidateOrganizationFinalExecutionEvidenceResult(command, finalCommand, result.FinalEvidence)
	default:
		return ErrOrganizationExecutionInvariant
	}
}

func ValidateOrganizationExecutionAttemptResult(result *OrganizationExecutionAttemptResult) error {
	if result == nil {
		return ErrOrganizationExecutionInvalid
	}
	if result.PendingReconciliation {
		if !result.Dispatched || result.Observation == nil || result.TerminalOutcome != OrganizationAttemptEvidenceTerminalOutcomeUnknown || result.BusinessLimited || result.Retryable || strings.TrimSpace(result.ZeroProof) != "" {
			return ErrOrganizationExecutionInvalid
		}
		return nil
	}
	if !validOrganizationExecutionTerminalOutcome(result.TerminalOutcome) || (result.BusinessLimited && result.TerminalOutcome != OrganizationExecutionTerminalOutcomeError) {
		return ErrOrganizationExecutionInvalid
	}
	if result.Retryable && (!result.Dispatched || result.TerminalOutcome != OrganizationExecutionTerminalOutcomeError || result.BusinessLimited) {
		return ErrOrganizationExecutionInvalid
	}
	if result.Dispatched {
		if result.Observation == nil || strings.TrimSpace(result.ZeroProof) != "" {
			return ErrOrganizationExecutionInvalid
		}
		return nil
	}
	if result.Observation != nil || result.TerminalOutcome != OrganizationExecutionTerminalOutcomeError || strings.TrimSpace(result.ZeroProof) == "" || len(strings.TrimSpace(result.ZeroProof)) > organizationExecutionReasonMaxBytes {
		return ErrOrganizationExecutionInvalid
	}
	return nil
}

func ValidateOrganizationAttemptEvidenceCommand(command OrganizationAttemptEvidenceCommand) error {
	if !validOrganizationExecutionUUID(command.ExecutionID) || !validOrganizationExecutionUUID(command.AttemptID) || !validOrganizationExecutionUUID(command.CorrelationID) ||
		command.ReservationID <= 0 || command.OrganizationID <= 0 || command.UserID <= 0 || command.APIKeyID <= 0 || command.AllocationID <= 0 || command.AttemptSequence <= 0 || command.AccountID <= 0 ||
		!validOrganizationAttemptEvidenceProvenance(command.Provenance) || !validSHA256(command.ProvenanceHash) || !validOrganizationAttemptTerminalOutcome(command.TerminalOutcome) ||
		command.ProviderObservedAt.IsZero() || command.IngestedAt.IsZero() || command.InputTokens < 0 || command.OutputTokens < 0 || command.CacheCreationTokens < 0 || command.CacheReadTokens < 0 {
		return ErrOrganizationExecutionInvalid
	}
	if _, err := normalizeOrganizationExecutionIdentifier(command.ProviderRequestID, organizationExecutionIdentifierMax, false); err != nil {
		return err
	}
	if !validOrganizationExecutionUUID(command.ProviderCorrelationID) {
		return ErrOrganizationExecutionInvalid
	}
	if err := validateCanonicalOrganizationExecutionJSON(command.RouteSnapshotJSON, command.RouteSnapshotHash); err != nil {
		return err
	}
	if err := validateCanonicalOrganizationExecutionJSON(command.PricingSnapshotJSON, command.PricingSnapshotHash); err != nil {
		return err
	}
	if err := validateCanonicalOrganizationExecutionJSON(command.EvidenceJSON, command.EvidenceHash); err != nil {
		return err
	}
	if _, err := NormalizeOrganizationRawActualCost(command.RawActualCost); err != nil {
		return err
	}
	return nil
}

func ValidateOrganizationPendingAttemptCommand(command OrganizationPendingAttemptCommand) error {
	if !validOrganizationExecutionUUID(command.ExecutionID) || !validOrganizationExecutionUUID(command.AttemptID) || !validOrganizationExecutionUUID(command.CorrelationID) ||
		command.ReservationID <= 0 || command.OrganizationID <= 0 || command.UserID <= 0 || command.APIKeyID <= 0 || command.AllocationID <= 0 || command.AttemptSequence <= 0 || command.AccountID <= 0 || command.DispatchedAt.IsZero() {
		return ErrOrganizationExecutionInvalid
	}
	if _, err := normalizeOrganizationExecutionIdentifier(command.ProviderRequestID, organizationExecutionIdentifierMax, false); err != nil {
		return err
	}
	if !validOrganizationExecutionUUID(command.ProviderCorrelationID) {
		return ErrOrganizationExecutionInvalid
	}
	if err := validateCanonicalOrganizationExecutionJSON(command.RouteSnapshotJSON, command.RouteSnapshotHash); err != nil {
		return err
	}
	if err := validateCanonicalOrganizationExecutionJSON(command.PricingSnapshotJSON, command.PricingSnapshotHash); err != nil {
		return err
	}
	return nil
}

func ValidateOrganizationAttemptEvidenceResult(command OrganizationAttemptEvidenceCommand, result *OrganizationAttemptEvidenceResult) error {
	if result == nil || result.AttemptEvidenceID <= 0 || result.ExecutionID != command.ExecutionID || result.AttemptID != command.AttemptID {
		return ErrOrganizationExecutionInvariant
	}
	return nil
}

func ValidateOrganizationTerminalizationCommand(command OrganizationTerminalizationCommand) error {
	if !validOrganizationExecutionUUID(command.ExecutionID) || !validOrganizationExecutionUUID(command.CorrelationID) ||
		command.OrganizationID <= 0 || command.UserID <= 0 || command.APIKeyID <= 0 || command.AllocationID <= 0 || command.ReservationID <= 0 ||
		!validOrganizationExecutionTerminalOutcome(command.TerminalOutcome) || command.OccurredAt.IsZero() ||
		(command.BusinessLimited && command.TerminalOutcome != OrganizationExecutionTerminalOutcomeError) {
		return ErrOrganizationExecutionInvalid
	}
	if command.ReconciledAt != nil && (command.ReconciledAt.IsZero() || command.ReconciledAt.Before(command.OccurredAt)) {
		return ErrOrganizationExecutionInvalid
	}
	hasAttemptID := command.FinalAttemptID != nil
	hasEvidenceID := command.FinalAttemptEvidenceID != nil
	if hasAttemptID != hasEvidenceID {
		return ErrOrganizationExecutionInvalid
	}
	if hasAttemptID {
		if !validOrganizationExecutionUUID(*command.FinalAttemptID) || *command.FinalAttemptEvidenceID <= 0 || strings.TrimSpace(command.ZeroProof) != "" {
			return ErrOrganizationExecutionInvalid
		}
		return nil
	}
	if command.TerminalOutcome != OrganizationExecutionTerminalOutcomeError || strings.TrimSpace(command.ZeroProof) == "" || len(strings.TrimSpace(command.ZeroProof)) > organizationExecutionReasonMaxBytes {
		return ErrOrganizationExecutionInvalid
	}
	return nil
}

func ValidateOrganizationFinalExecutionEvidenceResult(
	reservation OrganizationExecutionReservationCommand,
	command OrganizationTerminalizationCommand,
	result *OrganizationFinalExecutionEvidenceResult,
) error {
	if result == nil || result.FinalEvidenceID <= 0 || result.ExecutionID != command.ExecutionID || result.CorrelationID != command.CorrelationID ||
		result.OrganizationID != command.OrganizationID || result.UserID != command.UserID || result.APIKeyID != command.APIKeyID || result.AllocationID != command.AllocationID ||
		result.ReservationID != command.ReservationID || result.TerminalOutcome != command.TerminalOutcome || result.BusinessLimited != command.BusinessLimited || result.OccurredAt.IsZero() {
		return ErrOrganizationExecutionInvariant
	}
	if result.FinalAttemptID == nil != (command.FinalAttemptID == nil) || result.FinalAttemptEvidenceID == nil != (command.FinalAttemptEvidenceID == nil) {
		return ErrOrganizationExecutionInvariant
	}
	if (result.ReconciledAt == nil) != (command.ReconciledAt == nil) || (command.ReconciledAt != nil && !result.ReconciledAt.Equal(command.ReconciledAt.UTC())) {
		return ErrOrganizationExecutionInvariant
	}
	if command.FinalAttemptID != nil && (*result.FinalAttemptID != *command.FinalAttemptID || *result.FinalAttemptEvidenceID != *command.FinalAttemptEvidenceID) {
		return ErrOrganizationExecutionInvariant
	}
	if command.FinalAttemptID == nil {
		if strings.TrimSpace(command.ZeroProof) == "" || result.ZeroProof != strings.TrimSpace(command.ZeroProof) {
			return ErrOrganizationExecutionInvariant
		}
	} else if result.ZeroProof != "" {
		return ErrOrganizationExecutionInvariant
	}
	rawActualCost, err := NormalizeOrganizationRawActualCost(result.RawActualCost)
	if err != nil || rawActualCost != result.RawActualCost {
		return ErrOrganizationExecutionInvariant
	}
	settledCost, err := NormalizeOrganizationMoney(result.OrganizationSettledCost, false)
	if err != nil || settledCost != result.OrganizationSettledCost {
		return ErrOrganizationExecutionInvariant
	}
	settledDecimal, _ := decimal.NewFromString(settledCost)
	reservedDecimal, _ := decimal.NewFromString(reservation.CandidateRouteMaximumCost)
	if settledDecimal.GreaterThan(reservedDecimal) {
		return ErrOrganizationExecutionInvariant
	}
	if result.ReservationID == 0 {
		if !settledDecimal.IsZero() || result.ReservationState != "" || result.ConsumptionPostingID != nil || result.FinalAttemptID != nil || result.FinalAttemptEvidenceID != nil {
			return ErrOrganizationExecutionInvariant
		}
		return nil
	}
	if settledDecimal.IsZero() {
		if result.ReservationState != OrganizationReservationStateReleased || result.ConsumptionPostingID != nil {
			return ErrOrganizationExecutionInvariant
		}
		return nil
	}
	if command.FinalAttemptID == nil || result.ReservationState != OrganizationReservationStateSettled || result.ConsumptionPostingID == nil || *result.ConsumptionPostingID <= 0 {
		return ErrOrganizationExecutionInvariant
	}
	return nil
}

// NormalizeOrganizationRawActualCost permits high-precision provider costs
// while retaining a bounded, canonical non-negative decimal representation.
// This value is evidence only; it is never directly used as settled funds.
func NormalizeOrganizationRawActualCost(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > organizationExecutionMetadataMaxBytes {
		return "", ErrOrganizationExecutionInvalid
	}
	for _, runeValue := range value {
		if (runeValue < '0' || runeValue > '9') && runeValue != '.' {
			return "", ErrOrganizationExecutionInvalid
		}
	}
	if strings.Count(value, ".") > 1 || value == "." {
		return "", ErrOrganizationExecutionInvalid
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil || parsed.IsNegative() {
		return "", ErrOrganizationExecutionInvalid
	}
	return parsed.String(), nil
}

func canonicalOrganizationExecutionJSON(raw json.RawMessage) (string, string, error) {
	if len(raw) == 0 || len(raw) > organizationExecutionSnapshotMaxBytes {
		return "", "", ErrOrganizationExecutionInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", "", ErrOrganizationExecutionInvalid.WithCause(err)
	}
	if _, ok := value.(map[string]any); !ok {
		if _, ok := value.([]any); !ok {
			return "", "", ErrOrganizationExecutionInvalid
		}
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", "", ErrOrganizationExecutionInvalid
	}
	canonical, err := json.Marshal(value)
	if err != nil || len(canonical) == 0 || len(canonical) > organizationExecutionSnapshotMaxBytes {
		return "", "", ErrOrganizationExecutionInvalid
	}
	sum := sha256.Sum256(canonical)
	return string(canonical), hex.EncodeToString(sum[:]), nil
}

func validateCanonicalOrganizationExecutionJSON(raw, expectedHash string) error {
	if raw == "" || len(raw) > organizationExecutionSnapshotMaxBytes || !validSHA256(expectedHash) {
		return ErrOrganizationExecutionInvalid
	}
	canonical, hash, err := canonicalOrganizationExecutionJSON(json.RawMessage(raw))
	if err != nil || canonical != raw || hash != expectedHash {
		return ErrOrganizationExecutionInvalid
	}
	return nil
}

func normalizeOrganizationExecutionIdentifier(value string, maxLen int, optional bool) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" && optional {
		return "", nil
	}
	if normalized == "" || len(normalized) > maxLen || strings.IndexByte(normalized, 0) >= 0 {
		return "", ErrOrganizationExecutionInvalid
	}
	return normalized, nil
}

func validOrganizationExecutionUUID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}

func validOrganizationExecutionTerminalOutcome(value string) bool {
	return value == OrganizationExecutionTerminalOutcomeSuccess || value == OrganizationExecutionTerminalOutcomeError
}

func validOrganizationAttemptTerminalOutcome(value string) bool {
	return validOrganizationExecutionTerminalOutcome(value) || value == OrganizationAttemptEvidenceTerminalOutcomeUnknown
}

func validOrganizationAttemptEvidenceProvenance(value string) bool {
	return value == OrganizationAttemptEvidenceProvenanceAdapterDirect || value == OrganizationAttemptEvidenceProvenanceProviderQuery
}

func pointerToString(value string) *string {
	return &value
}

func pointerToInt64(value int64) *int64 {
	return &value
}

func (s *OrganizationExecutionService) clockNow() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}
