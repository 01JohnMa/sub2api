package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// Models and usage are local/non-billable organization observations. They are
// deliberately separate from the spend-capable endpoint families so callers
// cannot accidentally enter the reservation contract with a zero ceiling.
const (
	OrganizationEndpointFamilyModels = "models"
	OrganizationEndpointFamilyUsage  = "usage"

	organizationNonBillableDurabilityTimeout = 5 * time.Second

	OrganizationNonBillableExecutionDispositionStarted   = "started"
	OrganizationNonBillableExecutionDispositionFinalized = "finalized"

	organizationNonBillableZeroProofStaleAttribution = "stale_authoritative_attribution"
)

// OrganizationNonBillableExecutionTrustedAttribution contains the identity
// facts needed by a non-financial organization execution. Allocation identity
// remains mandatory for member attribution, but its available balance is not
// part of this contract and cannot block a non-billable request.
type OrganizationNonBillableExecutionTrustedAttribution struct {
	OrganizationID            int64
	OrganizationStatus        string
	UserID                    int64
	UserOrganizationID        int64
	APIKeyID                  int64
	APIKeyUserID              int64
	APIKeyOrganizationID      int64
	AllocationID              int64
	AllocationUserID          int64
	AllocationOrganizationID  int64
	AllocationStatus          string
	AllocationVersionSnapshot int64
}

type OrganizationNonBillableExecutionInput struct {
	Attribution        OrganizationNonBillableExecutionTrustedAttribution
	EndpointFamily     string
	RequestShape       string
	Model              string
	ClientRequestID    string
	CandidateSnapshot  json.RawMessage
	PricingSnapshot    json.RawMessage
	BillingFingerprint string
}

// OrganizationNonBillableExecutionCommand is immutable request identity. It
// has no candidate ceiling or reservation fields by design.
type OrganizationNonBillableExecutionCommand struct {
	ExecutionID           string
	CorrelationID         string
	Attribution           OrganizationNonBillableExecutionTrustedAttribution
	EndpointFamily        string
	RequestShape          string
	Model                 string
	ClientRequestID       string
	CandidateSnapshotJSON string
	CandidateSetHash      string
	PricingSnapshotJSON   string
	PricingSnapshotHash   string
	BillingFingerprint    string
	AcceptedAt            time.Time
}

type OrganizationNonBillableExecutionStartResult struct {
	ExecutionID    string
	CorrelationID  string
	OrganizationID int64
	UserID         int64
	APIKeyID       int64
	AllocationID   int64
	State          string
	Disposition    string
	FinalEvidence  *OrganizationNonBillableFinalExecutionEvidenceResult
	Replayed       bool
}

// OrganizationNonBillableDispatchRequest is sent to exactly one adapter
// dispatch. The IDs are generated before the adapter is called and must be
// echoed by its observation, making provider evidence linkable even on errors.
type OrganizationNonBillableDispatchRequest struct {
	ExecutionID           string
	AttemptID             string
	CorrelationID         string
	ProviderCorrelationID string
	ProviderRequestID     string
	AttemptSequence       int
	OrganizationID        int64
	UserID                int64
	APIKeyID              int64
	AllocationID          int64
	EndpointFamily        string
	RequestShape          string
	Model                 string
	CandidateSnapshotJSON string
	CandidateSetHash      string
	PricingSnapshotJSON   string
	PricingSnapshotHash   string
}

// OrganizationNonBillableDispatchResult is the adapter's single observation.
// A non-dispatched result must carry a durable zero proof. A dispatched result
// must carry an observation and is persisted as count_tokens evidence.
type OrganizationNonBillableDispatchResult struct {
	Dispatched            bool
	PendingReconciliation bool
	Observation           *OrganizationExecutionAttemptObservation
	TerminalOutcome       string
	BusinessLimited       bool
	ZeroProof             string
}

type OrganizationNonBillableExecutionAdapter interface {
	Dispatch(ctx context.Context, request OrganizationNonBillableDispatchRequest) (*OrganizationNonBillableDispatchResult, error)
}

type OrganizationNonBillableAttemptEvidenceCommand struct {
	ExecutionID           string
	AttemptID             string
	CorrelationID         string
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

type OrganizationNonBillablePendingAttemptCommand struct {
	ExecutionID           string
	AttemptID             string
	CorrelationID         string
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

type OrganizationNonBillableTerminalizationCommand struct {
	ExecutionID            string
	CorrelationID          string
	OrganizationID         int64
	UserID                 int64
	APIKeyID               int64
	AllocationID           int64
	FinalAttemptID         *string
	FinalAttemptEvidenceID *int64
	TerminalOutcome        string
	BusinessLimited        bool
	ZeroProof              string
	OccurredAt             time.Time
}

type OrganizationNonBillableFinalExecutionEvidenceResult struct {
	FinalEvidenceID         int64
	ExecutionID             string
	CorrelationID           string
	OrganizationID          int64
	UserID                  int64
	APIKeyID                int64
	AllocationID            int64
	FinalAttemptID          *string
	FinalAttemptEvidenceID  *int64
	TerminalOutcome         string
	BusinessLimited         bool
	InputTokens             int64
	OutputTokens            int64
	CacheCreationTokens     int64
	CacheReadTokens         int64
	RawActualCost           string
	OrganizationSettledCost string
	IsCountTokensProbe      bool
	ZeroProof               string
	ConsumptionPostingID    *int64
	OccurredAt              time.Time
	Replayed                bool
}

type OrganizationNonBillableExecutionRepository interface {
	BeginNonBillableExecution(ctx context.Context, command OrganizationNonBillableExecutionCommand) (*OrganizationNonBillableExecutionStartResult, error)
	RecordNonBillableAttemptEvidence(ctx context.Context, command OrganizationNonBillableAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error)
	RecordNonBillablePendingAttempt(ctx context.Context, command OrganizationNonBillablePendingAttemptCommand) error
	TerminalizeNonBillableExecution(ctx context.Context, command OrganizationNonBillableTerminalizationCommand) (*OrganizationNonBillableFinalExecutionEvidenceResult, error)
}

func (s *OrganizationExecutionService) NewNonBillableExecutionCommand(input OrganizationNonBillableExecutionInput) (*OrganizationNonBillableExecutionCommand, error) {
	if err := ValidateOrganizationNonBillableTrustedAttribution(input.Attribution); err != nil {
		return nil, err
	}
	candidateJSON, candidateHash, err := canonicalOrganizationExecutionJSON(input.CandidateSnapshot)
	if err != nil {
		return nil, err
	}
	pricingJSON, pricingHash, err := canonicalOrganizationExecutionJSON(input.PricingSnapshot)
	if err != nil {
		return nil, err
	}
	endpointFamily, err := normalizeOrganizationExecutionIdentifier(input.EndpointFamily, 128, false)
	if err != nil || !validOrganizationNonBillableEndpointFamily(endpointFamily) {
		return nil, ErrOrganizationExecutionInvalid
	}
	requestShape, err := normalizeOrganizationExecutionIdentifier(input.RequestShape, 128, false)
	if err != nil {
		return nil, err
	}
	model, err := normalizeOrganizationExecutionIdentifier(input.Model, 255, true)
	if err != nil {
		return nil, err
	}
	clientRequestID, err := normalizeOrganizationExecutionIdentifier(input.ClientRequestID, organizationExecutionMetadataMaxBytes, true)
	if err != nil {
		return nil, err
	}
	fingerprint := strings.ToLower(strings.TrimSpace(input.BillingFingerprint))
	if !validSHA256(fingerprint) {
		return nil, ErrOrganizationExecutionInvalid
	}
	now := s.clockNow()
	if now.IsZero() {
		return nil, ErrOrganizationExecutionInvariant
	}
	return &OrganizationNonBillableExecutionCommand{
		ExecutionID:           uuid.NewString(),
		CorrelationID:         uuid.NewString(),
		Attribution:           input.Attribution,
		EndpointFamily:        endpointFamily,
		RequestShape:          requestShape,
		Model:                 model,
		ClientRequestID:       clientRequestID,
		CandidateSnapshotJSON: candidateJSON,
		CandidateSetHash:      candidateHash,
		PricingSnapshotJSON:   pricingJSON,
		PricingSnapshotHash:   pricingHash,
		BillingFingerprint:    fingerprint,
		AcceptedAt:            now,
	}, nil
}

func (s *OrganizationExecutionService) BeginNonBillableExecution(ctx context.Context, command OrganizationNonBillableExecutionCommand) (*OrganizationNonBillableExecutionStartResult, error) {
	repo, err := organizationNonBillableRepository(s)
	if err != nil {
		return nil, err
	}
	if err := ValidateOrganizationNonBillableExecutionCommand(command); err != nil {
		return nil, err
	}
	result, err := repo.BeginNonBillableExecution(ctx, command)
	if err != nil {
		return result, err
	}
	if err := ValidateOrganizationNonBillableStartResult(command, result); err != nil {
		return nil, err
	}
	if organizationNonBillableStaleAttributionFinal(result.FinalEvidence) {
		return result, ErrOrganizationBillingUnavailable
	}
	return result, nil
}

func (s *OrganizationExecutionService) RecordNonBillableAttemptEvidence(ctx context.Context, execution OrganizationNonBillableExecutionCommand, command OrganizationNonBillableAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error) {
	repo, err := organizationNonBillableRepository(s)
	if err != nil {
		return nil, err
	}
	if err := ValidateOrganizationNonBillableExecutionCommand(execution); err != nil {
		return nil, err
	}
	if err := ValidateOrganizationNonBillableAttemptEvidenceCommand(execution, command); err != nil {
		return nil, err
	}
	result, err := repo.RecordNonBillableAttemptEvidence(ctx, command)
	if err != nil {
		return nil, err
	}
	if result == nil || result.ExecutionID != command.ExecutionID || result.AttemptID != command.AttemptID || result.AttemptEvidenceID <= 0 {
		return nil, ErrOrganizationExecutionInvariant
	}
	return result, nil
}

func (s *OrganizationExecutionService) RecordNonBillablePendingAttempt(ctx context.Context, execution OrganizationNonBillableExecutionCommand, command OrganizationNonBillablePendingAttemptCommand) error {
	repo, err := organizationNonBillableRepository(s)
	if err != nil {
		return err
	}
	if err := ValidateOrganizationNonBillableExecutionCommand(execution); err != nil {
		return err
	}
	if err := ValidateOrganizationNonBillablePendingAttemptCommand(execution, command); err != nil {
		return err
	}
	return repo.RecordNonBillablePendingAttempt(ctx, command)
}

func (s *OrganizationExecutionService) TerminalizeNonBillableExecution(ctx context.Context, execution OrganizationNonBillableExecutionCommand, command OrganizationNonBillableTerminalizationCommand) (*OrganizationNonBillableFinalExecutionEvidenceResult, error) {
	repo, err := organizationNonBillableRepository(s)
	if err != nil {
		return nil, err
	}
	if err := ValidateOrganizationNonBillableExecutionCommand(execution); err != nil {
		return nil, err
	}
	if err := ValidateOrganizationNonBillableTerminalizationCommand(execution, command); err != nil {
		return nil, err
	}
	result, err := repo.TerminalizeNonBillableExecution(ctx, command)
	if err != nil {
		return nil, err
	}
	if err := ValidateOrganizationNonBillableFinalExecutionEvidenceResult(execution, command, result); err != nil {
		return nil, err
	}
	return result, nil
}

// ExecuteNonBillable owns the common parent/optional-attempt/final sequence.
// Adapters must return a result even when a transport error happened after
// dispatch so the attempted request can still be finalized exactly once.
func (s *OrganizationExecutionService) ExecuteNonBillable(ctx context.Context, command OrganizationNonBillableExecutionCommand, adapter OrganizationNonBillableExecutionAdapter) (*OrganizationNonBillableFinalExecutionEvidenceResult, error) {
	start, err := s.BeginNonBillableExecution(ctx, command)
	if err != nil {
		if start != nil && organizationNonBillableStaleAttributionFinal(start.FinalEvidence) {
			return start.FinalEvidence, err
		}
		return nil, err
	}
	if start.FinalEvidence != nil {
		if organizationNonBillableStaleAttributionFinal(start.FinalEvidence) {
			return start.FinalEvidence, ErrOrganizationBillingUnavailable
		}
		return start.FinalEvidence, nil
	}
	if start.Disposition != OrganizationNonBillableExecutionDispositionStarted || adapter == nil {
		return nil, ErrOrganizationExecutionAdapterUnavailable
	}
	attemptID := uuid.NewString()
	providerCorrelationID := uuid.NewString()
	providerRequestID := uuid.NewString()
	dispatchResult, dispatchErr := adapter.Dispatch(ctx, OrganizationNonBillableDispatchRequest{
		ExecutionID: command.ExecutionID, AttemptID: attemptID, CorrelationID: command.CorrelationID,
		ProviderCorrelationID: providerCorrelationID, ProviderRequestID: providerRequestID, AttemptSequence: 1,
		OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID, APIKeyID: command.Attribution.APIKeyID, AllocationID: command.Attribution.AllocationID,
		EndpointFamily: command.EndpointFamily, RequestShape: command.RequestShape, Model: command.Model,
		CandidateSnapshotJSON: command.CandidateSnapshotJSON, CandidateSetHash: command.CandidateSetHash,
		PricingSnapshotJSON: command.PricingSnapshotJSON, PricingSnapshotHash: command.PricingSnapshotHash,
	})
	if dispatchResult == nil {
		if dispatchErr != nil {
			return nil, dispatchErr
		}
		return nil, ErrOrganizationExecutionInvariant
	}
	if err := ValidateOrganizationNonBillableDispatchResult(command, dispatchResult); err != nil {
		return nil, err
	}
	durabilityCtx, cancelDurability := organizationNonBillableDurabilityContext(ctx)
	defer cancelDurability()
	terminal := OrganizationNonBillableTerminalizationCommand{
		ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
		OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID, APIKeyID: command.Attribution.APIKeyID, AllocationID: command.Attribution.AllocationID,
		TerminalOutcome: dispatchResult.TerminalOutcome, BusinessLimited: dispatchResult.BusinessLimited, ZeroProof: strings.TrimSpace(dispatchResult.ZeroProof),
		OccurredAt: s.clockNow(),
	}
	if dispatchResult.Dispatched {
		observation := *dispatchResult.Observation
		if observation.ProviderRequestID == "" {
			observation.ProviderRequestID = providerRequestID
		}
		observedProviderRequestID := observation.ProviderRequestID
		if observation.ProviderCorrelationID == "" {
			observation.ProviderCorrelationID = providerCorrelationID
		}
		if observation.ProviderObservedAt.IsZero() {
			observation.ProviderObservedAt = terminal.OccurredAt
		}
		if dispatchResult.PendingReconciliation {
			pendingCommand, err := newOrganizationNonBillablePendingAttemptCommand(command, attemptID, observedProviderRequestID, providerCorrelationID, &observation, terminal.OccurredAt)
			if err != nil {
				return nil, err
			}
			if err := s.RecordNonBillablePendingAttempt(durabilityCtx, command, pendingCommand); err != nil {
				return nil, err
			}
			return nil, ErrOrganizationExecutionPendingReconciliation
		}
		attemptCommand, err := newOrganizationNonBillableAttemptEvidenceCommand(command, attemptID, observedProviderRequestID, providerCorrelationID, &observation, dispatchResult.TerminalOutcome, terminal.OccurredAt)
		if err != nil {
			return nil, err
		}
		attemptResult, err := s.RecordNonBillableAttemptEvidence(durabilityCtx, command, attemptCommand)
		if err != nil {
			return nil, err
		}
		terminal.FinalAttemptID = pointerToString(attemptCommand.AttemptID)
		terminal.FinalAttemptEvidenceID = pointerToInt64(attemptResult.AttemptEvidenceID)
		terminal.ZeroProof = ""
	}
	final, err := s.TerminalizeNonBillableExecution(durabilityCtx, command, terminal)
	if err != nil {
		return nil, err
	}
	if dispatchErr != nil {
		return final, dispatchErr
	}
	return final, nil
}

func organizationNonBillableDurabilityContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), organizationNonBillableDurabilityTimeout)
}

func organizationNonBillableStaleAttributionFinal(final *OrganizationNonBillableFinalExecutionEvidenceResult) bool {
	return final != nil && final.TerminalOutcome == OrganizationExecutionTerminalOutcomeError &&
		final.FinalAttemptID == nil && final.FinalAttemptEvidenceID == nil &&
		strings.TrimSpace(final.ZeroProof) == organizationNonBillableZeroProofStaleAttribution
}

func organizationNonBillableRepository(s *OrganizationExecutionService) (OrganizationNonBillableExecutionRepository, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationExecutionRepositoryUnavailable
	}
	repo, ok := s.repo.(OrganizationNonBillableExecutionRepository)
	if !ok || repo == nil {
		return nil, ErrOrganizationExecutionRepositoryUnavailable
	}
	return repo, nil
}

func ValidateOrganizationNonBillableTrustedAttribution(attribution OrganizationNonBillableExecutionTrustedAttribution) error {
	if attribution.OrganizationID <= 0 || attribution.OrganizationStatus != OrganizationStatusActive ||
		attribution.UserID <= 0 || attribution.UserOrganizationID != attribution.OrganizationID ||
		attribution.APIKeyID <= 0 || attribution.APIKeyUserID != attribution.UserID || attribution.APIKeyOrganizationID != attribution.OrganizationID ||
		attribution.AllocationID <= 0 || attribution.AllocationUserID != attribution.UserID || attribution.AllocationOrganizationID != attribution.OrganizationID ||
		attribution.AllocationStatus != domain.OrganizationAllocationStatusActive || attribution.AllocationVersionSnapshot < 0 {
		return ErrOrganizationExecutionInvalid
	}
	return nil
}

func ValidateOrganizationNonBillableExecutionCommand(command OrganizationNonBillableExecutionCommand) error {
	if err := ValidateOrganizationNonBillableTrustedAttribution(command.Attribution); err != nil {
		return err
	}
	if !validOrganizationExecutionUUID(command.ExecutionID) || !validOrganizationExecutionUUID(command.CorrelationID) || command.ExecutionID == command.CorrelationID || command.AcceptedAt.IsZero() {
		return ErrOrganizationExecutionInvalid
	}
	if !validOrganizationNonBillableEndpointFamily(command.EndpointFamily) {
		return ErrOrganizationExecutionInvalid
	}
	if _, err := normalizeOrganizationExecutionIdentifier(command.RequestShape, 128, false); err != nil {
		return err
	}
	if _, err := normalizeOrganizationExecutionIdentifier(command.Model, 255, true); err != nil {
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
	if fingerprint := strings.ToLower(strings.TrimSpace(command.BillingFingerprint)); fingerprint != command.BillingFingerprint || !validSHA256(fingerprint) {
		return ErrOrganizationExecutionInvalid
	}
	return nil
}

func ValidateOrganizationNonBillableStartResult(command OrganizationNonBillableExecutionCommand, result *OrganizationNonBillableExecutionStartResult) error {
	if result == nil || result.ExecutionID != command.ExecutionID || result.CorrelationID != command.CorrelationID ||
		result.OrganizationID != command.Attribution.OrganizationID || result.UserID != command.Attribution.UserID || result.APIKeyID != command.Attribution.APIKeyID || result.AllocationID != command.Attribution.AllocationID {
		return ErrOrganizationExecutionInvariant
	}
	switch result.Disposition {
	case OrganizationNonBillableExecutionDispositionStarted:
		if result.FinalEvidence != nil || result.State != OrganizationExecutionStateAccepted || result.Replayed {
			return ErrOrganizationExecutionInvariant
		}
	case OrganizationNonBillableExecutionDispositionFinalized:
		if result.FinalEvidence == nil || result.State != OrganizationExecutionStateReleased || result.FinalEvidence.Replayed != result.Replayed {
			return ErrOrganizationExecutionInvariant
		}
		if err := ValidateOrganizationNonBillableFinalExecutionEvidenceResult(command, OrganizationNonBillableTerminalizationCommand{
			ExecutionID: result.FinalEvidence.ExecutionID, CorrelationID: result.FinalEvidence.CorrelationID,
			OrganizationID: result.FinalEvidence.OrganizationID, UserID: result.FinalEvidence.UserID, APIKeyID: result.FinalEvidence.APIKeyID, AllocationID: result.FinalEvidence.AllocationID,
			FinalAttemptID: result.FinalEvidence.FinalAttemptID, FinalAttemptEvidenceID: result.FinalEvidence.FinalAttemptEvidenceID,
			TerminalOutcome: result.FinalEvidence.TerminalOutcome, BusinessLimited: result.FinalEvidence.BusinessLimited,
			ZeroProof: result.FinalEvidence.ZeroProof, OccurredAt: result.FinalEvidence.OccurredAt,
		}, result.FinalEvidence); err != nil {
			return err
		}
	default:
		return ErrOrganizationExecutionInvariant
	}
	return nil
}

func ValidateOrganizationNonBillableAttemptEvidenceCommand(execution OrganizationNonBillableExecutionCommand, command OrganizationNonBillableAttemptEvidenceCommand) error {
	if !organizationNonBillableDispatchFamily(execution.EndpointFamily) ||
		!validOrganizationExecutionUUID(command.ExecutionID) || command.ExecutionID != execution.ExecutionID ||
		!validOrganizationExecutionUUID(command.AttemptID) || !validOrganizationExecutionUUID(command.CorrelationID) || command.CorrelationID != execution.CorrelationID ||
		command.OrganizationID != execution.Attribution.OrganizationID || command.UserID != execution.Attribution.UserID || command.APIKeyID != execution.Attribution.APIKeyID || command.AllocationID != execution.Attribution.AllocationID ||
		command.PricingSnapshotHash != execution.PricingSnapshotHash || command.PricingSnapshotJSON != execution.PricingSnapshotJSON {
		return ErrOrganizationExecutionInvalid
	}
	return ValidateOrganizationNonBillableAttemptEvidenceRepositoryCommand(command)
}

func ValidateOrganizationNonBillablePendingAttemptCommand(execution OrganizationNonBillableExecutionCommand, command OrganizationNonBillablePendingAttemptCommand) error {
	if !organizationNonBillableDispatchFamily(execution.EndpointFamily) ||
		!validOrganizationExecutionUUID(command.ExecutionID) || command.ExecutionID != execution.ExecutionID ||
		!validOrganizationExecutionUUID(command.AttemptID) || !validOrganizationExecutionUUID(command.CorrelationID) || command.CorrelationID != execution.CorrelationID ||
		command.OrganizationID != execution.Attribution.OrganizationID || command.UserID != execution.Attribution.UserID || command.APIKeyID != execution.Attribution.APIKeyID || command.AllocationID != execution.Attribution.AllocationID ||
		command.AttemptSequence != 1 || command.AccountID <= 0 || command.DispatchedAt.IsZero() ||
		command.PricingSnapshotHash != execution.PricingSnapshotHash || command.PricingSnapshotJSON != execution.PricingSnapshotJSON {
		return ErrOrganizationExecutionInvalid
	}
	if _, err := normalizeOrganizationExecutionIdentifier(command.ProviderRequestID, organizationExecutionIdentifierMax, false); err != nil {
		return err
	}
	if !validOrganizationExecutionUUID(command.ProviderCorrelationID) {
		return ErrOrganizationExecutionInvalid
	}
	return validateCanonicalOrganizationExecutionJSON(command.RouteSnapshotJSON, command.RouteSnapshotHash)
}

func ValidateOrganizationNonBillableTerminalizationCommand(execution OrganizationNonBillableExecutionCommand, command OrganizationNonBillableTerminalizationCommand) error {
	if command.ExecutionID != execution.ExecutionID || command.CorrelationID != execution.CorrelationID ||
		command.OrganizationID != execution.Attribution.OrganizationID || command.UserID != execution.Attribution.UserID || command.APIKeyID != execution.Attribution.APIKeyID || command.AllocationID != execution.Attribution.AllocationID {
		return ErrOrganizationExecutionInvalid
	}
	if err := ValidateOrganizationNonBillableTerminalizationRepositoryCommand(command); err != nil {
		return err
	}
	hasAttemptID := command.FinalAttemptID != nil
	if hasAttemptID && !organizationNonBillableDispatchFamily(execution.EndpointFamily) {
		return ErrOrganizationExecutionInvalid
	}
	return nil
}

func ValidateOrganizationNonBillableAttemptEvidenceRepositoryCommand(command OrganizationNonBillableAttemptEvidenceCommand) error {
	if !validOrganizationExecutionUUID(command.ExecutionID) ||
		!validOrganizationExecutionUUID(command.AttemptID) || !validOrganizationExecutionUUID(command.CorrelationID) ||
		command.OrganizationID <= 0 || command.UserID <= 0 || command.APIKeyID <= 0 || command.AllocationID <= 0 ||
		command.AttemptSequence != 1 || command.AccountID <= 0 ||
		(command.Provenance != OrganizationAttemptEvidenceProvenanceAdapterDirect && command.Provenance != OrganizationAttemptEvidenceProvenanceProviderQuery) ||
		!validOrganizationExecutionTerminalOutcome(command.TerminalOutcome) || command.ProviderObservedAt.IsZero() || command.IngestedAt.IsZero() ||
		command.InputTokens < 0 || command.OutputTokens < 0 || command.CacheCreationTokens < 0 || command.CacheReadTokens < 0 || command.RawActualCost != "0" {
		return ErrOrganizationExecutionInvalid
	}
	if _, err := normalizeOrganizationExecutionIdentifier(command.ProviderRequestID, organizationExecutionIdentifierMax, false); err != nil {
		return err
	}
	if !validOrganizationExecutionUUID(command.ProviderCorrelationID) || !validSHA256(command.ProvenanceHash) {
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
	return nil
}

func ValidateOrganizationNonBillablePendingAttemptRepositoryCommand(command OrganizationNonBillablePendingAttemptCommand) error {
	if !validOrganizationExecutionUUID(command.ExecutionID) || !validOrganizationExecutionUUID(command.AttemptID) || !validOrganizationExecutionUUID(command.CorrelationID) ||
		command.OrganizationID <= 0 || command.UserID <= 0 || command.APIKeyID <= 0 || command.AllocationID <= 0 || command.AttemptSequence != 1 || command.AccountID <= 0 || command.DispatchedAt.IsZero() {
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
	return validateCanonicalOrganizationExecutionJSON(command.PricingSnapshotJSON, command.PricingSnapshotHash)
}

func ValidateOrganizationNonBillableTerminalizationRepositoryCommand(command OrganizationNonBillableTerminalizationCommand) error {
	if !validOrganizationExecutionUUID(command.ExecutionID) ||
		!validOrganizationExecutionUUID(command.CorrelationID) ||
		command.OrganizationID <= 0 || command.UserID <= 0 || command.APIKeyID <= 0 || command.AllocationID <= 0 ||
		!validOrganizationExecutionTerminalOutcome(command.TerminalOutcome) || command.OccurredAt.IsZero() ||
		(command.BusinessLimited && command.TerminalOutcome != OrganizationExecutionTerminalOutcomeError) {
		return ErrOrganizationExecutionInvalid
	}
	hasAttemptID := command.FinalAttemptID != nil
	if hasAttemptID != (command.FinalAttemptEvidenceID != nil) {
		return ErrOrganizationExecutionInvalid
	}
	if hasAttemptID {
		if !validOrganizationExecutionUUID(*command.FinalAttemptID) || *command.FinalAttemptEvidenceID <= 0 || strings.TrimSpace(command.ZeroProof) != "" {
			return ErrOrganizationExecutionInvalid
		}
		return nil
	}
	if strings.TrimSpace(command.ZeroProof) == "" || len(strings.TrimSpace(command.ZeroProof)) > organizationExecutionReasonMaxBytes {
		return ErrOrganizationExecutionInvalid
	}
	return nil
}

func ValidateOrganizationNonBillableFinalExecutionEvidenceResult(execution OrganizationNonBillableExecutionCommand, command OrganizationNonBillableTerminalizationCommand, result *OrganizationNonBillableFinalExecutionEvidenceResult) error {
	if result == nil || result.FinalEvidenceID <= 0 || result.ExecutionID != command.ExecutionID || result.CorrelationID != command.CorrelationID ||
		result.OrganizationID != command.OrganizationID || result.UserID != command.UserID || result.APIKeyID != command.APIKeyID || result.AllocationID != command.AllocationID ||
		result.TerminalOutcome != command.TerminalOutcome || result.BusinessLimited != command.BusinessLimited || result.OccurredAt.IsZero() ||
		result.OrganizationSettledCost != "0.00000000" || result.RawActualCost != "0" || result.ConsumptionPostingID != nil ||
		result.IsCountTokensProbe != (execution.EndpointFamily == OrganizationEndpointFamilyTokenCounting) {
		return ErrOrganizationExecutionInvariant
	}
	if !result.OccurredAt.UTC().Truncate(time.Microsecond).Equal(command.OccurredAt.UTC().Truncate(time.Microsecond)) {
		return ErrOrganizationExecutionInvariant
	}
	if result.FinalAttemptID == nil != (command.FinalAttemptID == nil) || result.FinalAttemptEvidenceID == nil != (command.FinalAttemptEvidenceID == nil) {
		return ErrOrganizationExecutionInvariant
	}
	if command.FinalAttemptID == nil {
		if execution.EndpointFamily == OrganizationEndpointFamilyTokenCounting && (result.InputTokens != 0 || result.OutputTokens != 0 || result.CacheCreationTokens != 0 || result.CacheReadTokens != 0) {
			return ErrOrganizationExecutionInvariant
		}
		if result.InputTokens != 0 || result.OutputTokens != 0 || result.CacheCreationTokens != 0 || result.CacheReadTokens != 0 || strings.TrimSpace(result.ZeroProof) != strings.TrimSpace(command.ZeroProof) {
			return ErrOrganizationExecutionInvariant
		}
	} else {
		if !organizationNonBillableDispatchFamily(execution.EndpointFamily) || result.ZeroProof != "" || *result.FinalAttemptID != *command.FinalAttemptID || *result.FinalAttemptEvidenceID != *command.FinalAttemptEvidenceID {
			return ErrOrganizationExecutionInvariant
		}
		if result.InputTokens < 0 || result.OutputTokens < 0 || result.CacheCreationTokens < 0 || result.CacheReadTokens < 0 {
			return ErrOrganizationExecutionInvariant
		}
	}
	return nil
}

func ValidateOrganizationNonBillableDispatchResult(execution OrganizationNonBillableExecutionCommand, result *OrganizationNonBillableDispatchResult) error {
	if result == nil {
		return ErrOrganizationExecutionInvalid
	}
	if result.PendingReconciliation {
		if !result.Dispatched || !organizationNonBillableDispatchFamily(execution.EndpointFamily) || result.Observation == nil || result.TerminalOutcome != OrganizationAttemptEvidenceTerminalOutcomeUnknown || result.BusinessLimited || strings.TrimSpace(result.ZeroProof) != "" {
			return ErrOrganizationExecutionInvalid
		}
		return validateOrganizationNonBillablePendingObservation(result.Observation)
	}
	if !validOrganizationExecutionTerminalOutcome(result.TerminalOutcome) || (result.BusinessLimited && result.TerminalOutcome != OrganizationExecutionTerminalOutcomeError) {
		return ErrOrganizationExecutionInvalid
	}
	if !result.Dispatched {
		if result.Observation != nil || strings.TrimSpace(result.ZeroProof) == "" || len(strings.TrimSpace(result.ZeroProof)) > organizationExecutionReasonMaxBytes {
			return ErrOrganizationExecutionInvalid
		}
		return nil
	}
	if !organizationNonBillableDispatchFamily(execution.EndpointFamily) || result.Observation == nil || strings.TrimSpace(result.ZeroProof) != "" {
		return ErrOrganizationExecutionInvalid
	}
	return validateOrganizationNonBillableDispatchObservation(result.Observation)
}

func newOrganizationNonBillableAttemptEvidenceCommand(execution OrganizationNonBillableExecutionCommand, attemptID, providerRequestID, providerCorrelationID string, observation *OrganizationExecutionAttemptObservation, terminalOutcome string, now time.Time) (OrganizationNonBillableAttemptEvidenceCommand, error) {
	if observation == nil || now.IsZero() {
		return OrganizationNonBillableAttemptEvidenceCommand{}, ErrOrganizationExecutionInvalid
	}
	if observation.ProviderRequestID != providerRequestID || observation.ProviderCorrelationID != providerCorrelationID {
		return OrganizationNonBillableAttemptEvidenceCommand{}, ErrOrganizationExecutionInvariant
	}
	routeJSON, routeHash, err := canonicalOrganizationExecutionJSON(observation.RouteSnapshot)
	if err != nil {
		return OrganizationNonBillableAttemptEvidenceCommand{}, err
	}
	evidenceJSON, evidenceHash, err := canonicalOrganizationExecutionJSON(observation.Evidence)
	if err != nil {
		return OrganizationNonBillableAttemptEvidenceCommand{}, err
	}
	if observation.RawActualCost == "" {
		observation.RawActualCost = "0"
	}
	if observation.RawActualCost != "0" {
		return OrganizationNonBillableAttemptEvidenceCommand{}, ErrOrganizationExecutionInvalid
	}
	return OrganizationNonBillableAttemptEvidenceCommand{
		ExecutionID: execution.ExecutionID, AttemptID: attemptID, CorrelationID: execution.CorrelationID,
		OrganizationID: execution.Attribution.OrganizationID, UserID: execution.Attribution.UserID, APIKeyID: execution.Attribution.APIKeyID, AllocationID: execution.Attribution.AllocationID,
		AttemptSequence: 1, AccountID: observation.AccountID, RouteSnapshotJSON: routeJSON, RouteSnapshotHash: routeHash,
		PricingSnapshotJSON: execution.PricingSnapshotJSON, PricingSnapshotHash: execution.PricingSnapshotHash,
		ProviderRequestID: providerRequestID, ProviderCorrelationID: providerCorrelationID,
		Provenance: OrganizationAttemptEvidenceProvenanceAdapterDirect, TerminalOutcome: terminalOutcome,
		ProvenanceHash: evidenceHash, EvidenceJSON: evidenceJSON, EvidenceHash: evidenceHash, RawActualCost: "0",
		InputTokens: observation.InputTokens, OutputTokens: observation.OutputTokens, CacheCreationTokens: observation.CacheCreationTokens,
		CacheReadTokens: observation.CacheReadTokens, ProviderObservedAt: observation.ProviderObservedAt.UTC(), IngestedAt: now.UTC(),
	}, nil
}

func newOrganizationNonBillablePendingAttemptCommand(execution OrganizationNonBillableExecutionCommand, attemptID, providerRequestID, providerCorrelationID string, observation *OrganizationExecutionAttemptObservation, now time.Time) (OrganizationNonBillablePendingAttemptCommand, error) {
	if observation == nil || now.IsZero() || observation.ProviderRequestID != providerRequestID || observation.ProviderCorrelationID != providerCorrelationID {
		return OrganizationNonBillablePendingAttemptCommand{}, ErrOrganizationExecutionInvariant
	}
	routeJSON, routeHash, err := canonicalOrganizationExecutionJSON(observation.RouteSnapshot)
	if err != nil {
		return OrganizationNonBillablePendingAttemptCommand{}, err
	}
	return OrganizationNonBillablePendingAttemptCommand{
		ExecutionID: execution.ExecutionID, AttemptID: attemptID, CorrelationID: execution.CorrelationID,
		OrganizationID: execution.Attribution.OrganizationID, UserID: execution.Attribution.UserID, APIKeyID: execution.Attribution.APIKeyID, AllocationID: execution.Attribution.AllocationID,
		AttemptSequence: 1, AccountID: observation.AccountID, RouteSnapshotJSON: routeJSON, RouteSnapshotHash: routeHash,
		PricingSnapshotJSON: execution.PricingSnapshotJSON, PricingSnapshotHash: execution.PricingSnapshotHash,
		ProviderRequestID: providerRequestID, ProviderCorrelationID: providerCorrelationID, DispatchedAt: now.UTC(),
	}, nil
}

func validateOrganizationNonBillableDispatchObservation(observation *OrganizationExecutionAttemptObservation) error {
	if observation == nil || observation.AccountID <= 0 || observation.InputTokens < 0 || observation.OutputTokens < 0 || observation.CacheCreationTokens < 0 || observation.CacheReadTokens < 0 || observation.RawActualCost != "0" {
		return ErrOrganizationExecutionInvalid
	}
	if _, _, err := canonicalOrganizationExecutionJSON(observation.RouteSnapshot); err != nil {
		return err
	}
	if len(observation.Evidence) > 0 {
		if _, _, err := canonicalOrganizationExecutionJSON(observation.Evidence); err != nil {
			return err
		}
	}
	return nil
}

func validateOrganizationNonBillablePendingObservation(observation *OrganizationExecutionAttemptObservation) error {
	if observation == nil || observation.AccountID <= 0 || observation.InputTokens != 0 || observation.OutputTokens != 0 || observation.CacheCreationTokens != 0 || observation.CacheReadTokens != 0 || observation.RawActualCost != "" {
		return ErrOrganizationExecutionInvalid
	}
	if _, _, err := canonicalOrganizationExecutionJSON(observation.RouteSnapshot); err != nil {
		return err
	}
	return nil
}

func validOrganizationNonBillableEndpointFamily(value string) bool {
	switch value {
	case OrganizationEndpointFamilyModels, OrganizationEndpointFamilyUsage, OrganizationEndpointFamilyTokenCounting:
		return true
	default:
		return false
	}
}

func organizationNonBillableDispatchFamily(value string) bool {
	return value == OrganizationEndpointFamilyModels || value == OrganizationEndpointFamilyTokenCounting
}
