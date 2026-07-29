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
// pending execution recovery and durable side-effect redrive. It accepts no
// caller-provided evidence or settlement amount; the repository derives both
// from immutable attempt/provider evidence and the reservation ceiling.
type OrganizationExecutionRecoveryService struct {
	repo OrganizationExecutionRecoveryRepository
	now  func() time.Time
}

func NewOrganizationExecutionRecoveryService(repo OrganizationExecutionRecoveryRepository) *OrganizationExecutionRecoveryService {
	return &OrganizationExecutionRecoveryService{
		repo: repo,
		now:  func() time.Time { return time.Now().UTC() },
	}
}

func (s *OrganizationExecutionRecoveryService) ReconcilePending(
	ctx context.Context,
	actor OrganizationReconciliationActor,
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
