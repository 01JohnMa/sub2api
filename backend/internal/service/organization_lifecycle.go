package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrOrganizationLifecycleConflict = infraerrors.Conflict(
		"ORGANIZATION_LIFECYCLE_CONFLICT",
		"organization lifecycle transition is not allowed",
	)
	ErrOrganizationBalanceNotSettled = infraerrors.Conflict(
		"ORGANIZATION_BALANCE_NOT_SETTLED",
		"organization member balances or reservations are not settled",
	)
	ErrOrganizationArchiveConfirmationRequired = infraerrors.BadRequest(
		"ORGANIZATION_ARCHIVE_CONFIRMATION_REQUIRED",
		"archive confirmation is required",
	)
	ErrOrganizationArchiveVersionConflict = infraerrors.Conflict(
		"ORGANIZATION_ARCHIVE_VERSION_CONFLICT",
		"organization state changed; refresh the archive confirmation",
	)
	ErrOrganizationLifecycleUnavailable = infraerrors.ServiceUnavailable(
		"ORGANIZATION_LIFECYCLE_REPOSITORY_UNAVAILABLE",
		"organization lifecycle storage is unavailable",
	)
)

// OrganizationArchivePreview is the non-mutating state bound to an archive
// confirmation. UpdatedAt detects lifecycle/identity changes and
// BalanceVersion detects all funding and allocation changes.
type OrganizationArchivePreview struct {
	OrganizationID int64
	Status         string
	Unallocated    string
	BalanceVersion int64
	UpdatedAt      time.Time
}

type OrganizationLifecycleCommand struct {
	OrganizationID         int64
	TargetStatus           string
	ExpectedBalanceVersion int64
	ExpectedUpdatedAt      time.Time
	ConfirmArchive         bool
}

type OrganizationLifecycleResult struct {
	Organization    *Organization
	CacheTargets    []OrganizationMemberCacheTarget
	AffectedUserIDs []int64
}

type OrganizationLifecycleRepository interface {
	GetArchivePreview(ctx context.Context, organizationID int64) (*OrganizationArchivePreview, error)
	Transition(ctx context.Context, command OrganizationLifecycleCommand) (*OrganizationLifecycleResult, error)
}

// OrganizationLifecycleManager is intentionally separate from identity
// management so lifecycle locks and archive confirmation cannot be bypassed
// through an ordinary organization update.
type OrganizationLifecycleManager interface {
	GetArchivePreview(ctx context.Context, organizationID int64) (*OrganizationArchivePreview, error)
	Disable(ctx context.Context, organizationID int64) (*Organization, error)
	Reactivate(ctx context.Context, organizationID int64) (*Organization, error)
	Archive(ctx context.Context, command OrganizationLifecycleCommand) (*Organization, error)
}

type OrganizationLifecycleService struct {
	repo               OrganizationLifecycleRepository
	authInvalidator    APIKeyAuthCacheInvalidator
	billingInvalidator OrganizationMembershipBillingCacheInvalidator
}

func NewOrganizationLifecycleService(
	repo OrganizationLifecycleRepository,
	authInvalidator APIKeyAuthCacheInvalidator,
	billingInvalidator OrganizationMembershipBillingCacheInvalidator,
) *OrganizationLifecycleService {
	return &OrganizationLifecycleService{
		repo: repo, authInvalidator: authInvalidator, billingInvalidator: billingInvalidator,
	}
}

func (s *OrganizationLifecycleService) GetArchivePreview(ctx context.Context, organizationID int64) (*OrganizationArchivePreview, error) {
	if organizationID <= 0 {
		return nil, ErrInvalidOrganizationID
	}
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationLifecycleUnavailable
	}
	return s.repo.GetArchivePreview(ctx, organizationID)
}

func (s *OrganizationLifecycleService) Disable(ctx context.Context, organizationID int64) (*Organization, error) {
	return s.transition(ctx, OrganizationLifecycleCommand{OrganizationID: organizationID, TargetStatus: OrganizationStatusDisabled})
}

func (s *OrganizationLifecycleService) Reactivate(ctx context.Context, organizationID int64) (*Organization, error) {
	return s.transition(ctx, OrganizationLifecycleCommand{OrganizationID: organizationID, TargetStatus: OrganizationStatusActive})
}

func (s *OrganizationLifecycleService) Archive(ctx context.Context, command OrganizationLifecycleCommand) (*Organization, error) {
	command.TargetStatus = OrganizationStatusArchived
	if !command.ConfirmArchive {
		return nil, ErrOrganizationArchiveConfirmationRequired
	}
	if command.ExpectedBalanceVersion < 0 || command.ExpectedUpdatedAt.IsZero() {
		return nil, ErrOrganizationArchiveConfirmationRequired
	}
	return s.transition(ctx, command)
}

func (s *OrganizationLifecycleService) transition(ctx context.Context, command OrganizationLifecycleCommand) (*Organization, error) {
	if command.OrganizationID <= 0 {
		return nil, ErrInvalidOrganizationID
	}
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationLifecycleUnavailable
	}
	result, err := s.repo.Transition(ctx, command)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Organization == nil {
		return nil, ErrOrganizationLifecycleUnavailable
	}
	// Cache invalidation is best-effort. Cached organization-key snapshots
	// deliberately fail closed, while reservation rechecks the locked state.
	targets := result.CacheTargets
	if len(targets) == 0 {
		for _, userID := range result.AffectedUserIDs {
			targets = append(targets, OrganizationMemberCacheTarget{UserID: userID})
		}
	}
	InvalidateOrganizationMemberCaches(ctx, s.authInvalidator, s.billingInvalidator, targets)
	return result.Organization, nil
}
