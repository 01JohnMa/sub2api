package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

// OrganizationExecutionRecoveryService is the platform-only control plane for
// pending execution recovery and durable side-effect redrive. Reconciliation
// accepts no caller-provided settlement amount. The non-billable evidence path
// accepts a provider observation only and binds it to immutable attempt facts in
// the repository before the existing reconciliation flow can terminalize it.
type OrganizationExecutionRecoveryService struct {
	repo OrganizationExecutionRecoveryRepository
	now  func() time.Time
}

type OrganizationNonBillableRecoveryEvidenceInput struct {
	AttemptID           string
	TerminalOutcome     string
	Evidence            json.RawMessage
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	ProviderObservedAt  time.Time
}

type OrganizationNonBillableRecoveryEvidenceCommand struct {
	ExecutionID         string
	OrganizationID      int64
	AttemptID           string
	TerminalOutcome     string
	EvidenceJSON        string
	EvidenceHash        string
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	ProviderObservedAt  time.Time
	IngestedAt          time.Time
}

// OrganizationNonBillableRecoveryEvidenceRepository is optional so existing
// recovery fakes and billable-only implementations keep their current contract.
type OrganizationNonBillableRecoveryEvidenceRepository interface {
	RecordNonBillableRecoveryEvidence(ctx context.Context, command OrganizationNonBillableRecoveryEvidenceCommand) (*OrganizationAttemptEvidenceResult, error)
}

type OrganizationPendingReconciliationSummary struct {
	Count  int64
	Status string
}

func NewOrganizationExecutionRecoveryService(repo OrganizationExecutionRecoveryRepository) *OrganizationExecutionRecoveryService {
	return &OrganizationExecutionRecoveryService{
		repo: repo,
		now:  func() time.Time { return time.Now().UTC() },
	}
}

func (s *OrganizationExecutionRecoveryService) RecordNonBillableRecoveryEvidenceInOrganization(
	ctx context.Context,
	actor OrganizationReconciliationActor,
	organizationID int64,
	executionID string,
	input OrganizationNonBillableRecoveryEvidenceInput,
) (*OrganizationAttemptEvidenceResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := validateOrganizationReconciliationActor(actor); err != nil {
		return nil, err
	}
	repo, ok := s.repo.(OrganizationNonBillableRecoveryEvidenceRepository)
	if !ok || repo == nil {
		return nil, ErrOrganizationExecutionRepositoryUnavailable
	}
	evidenceJSON, evidenceHash, err := canonicalOrganizationExecutionJSON(input.Evidence)
	if err != nil {
		return nil, err
	}
	command := OrganizationNonBillableRecoveryEvidenceCommand{
		ExecutionID:         strings.TrimSpace(executionID),
		OrganizationID:      organizationID,
		AttemptID:           strings.TrimSpace(input.AttemptID),
		TerminalOutcome:     strings.TrimSpace(input.TerminalOutcome),
		EvidenceJSON:        evidenceJSON,
		EvidenceHash:        evidenceHash,
		InputTokens:         input.InputTokens,
		OutputTokens:        input.OutputTokens,
		CacheCreationTokens: input.CacheCreationTokens,
		CacheReadTokens:     input.CacheReadTokens,
		ProviderObservedAt:  input.ProviderObservedAt.UTC(),
		IngestedAt:          s.now().UTC(),
	}
	if err := ValidateOrganizationNonBillableRecoveryEvidenceCommand(command); err != nil {
		return nil, err
	}
	result, err := repo.RecordNonBillableRecoveryEvidence(ctx, command)
	if err != nil {
		return nil, err
	}
	if result == nil || result.AttemptEvidenceID <= 0 || result.ExecutionID != command.ExecutionID || result.AttemptID != command.AttemptID {
		return nil, ErrOrganizationExecutionInvariant
	}
	return result, nil
}

func ValidateOrganizationNonBillableRecoveryEvidenceCommand(command OrganizationNonBillableRecoveryEvidenceCommand) error {
	if command.OrganizationID <= 0 || !validOrganizationExecutionUUID(command.ExecutionID) || !validOrganizationExecutionUUID(command.AttemptID) ||
		!validOrganizationExecutionTerminalOutcome(command.TerminalOutcome) || command.ProviderObservedAt.IsZero() || command.IngestedAt.IsZero() ||
		command.InputTokens < 0 || command.OutputTokens < 0 || command.CacheCreationTokens < 0 || command.CacheReadTokens < 0 {
		return ErrOrganizationExecutionInvalid
	}
	return validateCanonicalOrganizationExecutionJSON(command.EvidenceJSON, command.EvidenceHash)
}

func (s *OrganizationExecutionRecoveryService) ReconcilePending(
	ctx context.Context,
	actor OrganizationReconciliationActor,
	executionID string,
	idempotencyKey string,
) (*OrganizationFinalExecutionEvidenceResult, error) {
	return s.reconcilePending(ctx, actor, 0, executionID, idempotencyKey)
}

func (s *OrganizationExecutionRecoveryService) ReconcilePendingInOrganization(
	ctx context.Context,
	actor OrganizationReconciliationActor,
	organizationID int64,
	executionID string,
	idempotencyKey string,
) (*OrganizationFinalExecutionEvidenceResult, error) {
	if organizationID <= 0 {
		return nil, ErrOrganizationExecutionInvalid
	}
	return s.reconcilePending(ctx, actor, organizationID, executionID, idempotencyKey)
}

func (s *OrganizationExecutionRecoveryService) reconcilePending(
	ctx context.Context,
	actor OrganizationReconciliationActor,
	organizationID int64,
	executionID string,
	idempotencyKey string,
) (*OrganizationFinalExecutionEvidenceResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := validateOrganizationReconciliationActor(actor); err != nil {
		return nil, err
	}
	if !validOrganizationExecutionUUID(strings.TrimSpace(executionID)) {
		return nil, ErrOrganizationExecutionInvalid
	}
	key, err := NormalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	payload, err := json.Marshal(struct {
		ExecutionID string `json:"execution_id"`
		Action      string `json:"action"`
	}{
		ExecutionID: strings.TrimSpace(executionID),
		Action:      "rule_computed_reconcile",
	})
	if err != nil {
		return nil, ErrIdempotencyInvalidPayload.WithCause(err)
	}
	_, payloadHash, err := canonicalOrganizationExecutionJSON(payload)
	if err != nil {
		return nil, ErrIdempotencyInvalidPayload.WithCause(err)
	}
	result, err := s.repo.ReconcilePendingExecution(ctx, OrganizationPendingReconciliationCommand{
		ExecutionID:          strings.TrimSpace(executionID),
		OrganizationID:       organizationID,
		OperationID:          uuid.NewString(),
		CorrelationID:        uuid.NewString(),
		IdempotencyKeyHash:   HashIdempotencyKey(key),
		CanonicalPayloadHash: payloadHash,
		Actor:                actor,
		ReconciledAt:         s.now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *OrganizationExecutionRecoveryService) ListPending(
	ctx context.Context,
	organizationID int64,
	params OrganizationPendingReconciliationListParams,
) ([]OrganizationPendingReconciliation, *OrganizationPendingReconciliationPage, error) {
	if s == nil || s.repo == nil {
		return nil, nil, ErrOrganizationExecutionRepositoryUnavailable
	}
	if organizationID <= 0 || params.Page <= 0 || params.PageSize <= 0 || params.PageSize > 100 {
		return nil, nil, ErrOrganizationExecutionInvalid
	}
	return s.repo.ListPendingReconciliations(ctx, organizationID, params)
}

func (s *OrganizationExecutionRecoveryService) GetPending(
	ctx context.Context,
	organizationID int64,
	executionID string,
) (*OrganizationPendingReconciliation, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationExecutionRepositoryUnavailable
	}
	if organizationID <= 0 || !validOrganizationExecutionUUID(strings.TrimSpace(executionID)) {
		return nil, ErrOrganizationExecutionInvalid
	}
	return s.repo.GetPendingReconciliation(ctx, organizationID, strings.TrimSpace(executionID))
}

func (s *OrganizationExecutionRecoveryService) PendingSummary(ctx context.Context, organizationID int64) (*OrganizationPendingReconciliationSummary, error) {
	_, page, err := s.ListPending(ctx, organizationID, OrganizationPendingReconciliationListParams{Page: 1, PageSize: 1})
	if err != nil {
		return nil, err
	}
	status := "clear"
	if page.Total > 0 {
		status = "pending"
	}
	return &OrganizationPendingReconciliationSummary{Count: page.Total, Status: status}, nil
}

func (s *OrganizationExecutionRecoveryService) RedriveAttemptAccountEffect(ctx context.Context, attemptID string) (*OrganizationAttemptAccountEffectResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationExecutionRepositoryUnavailable
	}
	if !validOrganizationExecutionUUID(strings.TrimSpace(attemptID)) {
		return nil, ErrOrganizationExecutionInvalid
	}
	return s.repo.RedriveAttemptAccountEffect(ctx, strings.TrimSpace(attemptID))
}

func (s *OrganizationExecutionRecoveryService) RedriveExecutionOutbox(ctx context.Context, actor OrganizationReconciliationActor, outboxID int64) (*OrganizationExecutionOutboxApplyResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationExecutionRepositoryUnavailable
	}
	if err := validateOrganizationReconciliationActor(actor); err != nil {
		return nil, err
	}
	if outboxID <= 0 {
		return nil, ErrOrganizationExecutionInvalid
	}
	return s.repo.RedriveExecutionOutbox(ctx, outboxID)
}

func validateOrganizationReconciliationActor(actor OrganizationReconciliationActor) error {
	if actor.UserID <= 0 || !actor.Active || actor.AuthorizationScope != domain.OrganizationFundingAuthorizationPlatformAdminJWT || strings.TrimSpace(actor.PlatformRole) != domain.RoleAdmin {
		return ErrOrganizationExecutionReconciliationForbidden
	}
	if strings.TrimSpace(actor.DisplayName) == "" {
		return infraerrors.BadRequest("ORGANIZATION_EXECUTION_ACTOR_INVALID", "reconciliation actor display name is required")
	}
	return nil
}
