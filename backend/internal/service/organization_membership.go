package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/google/uuid"
)

const (
	OrganizationRoleAdmin  = domain.OrganizationRoleAdmin
	OrganizationRoleMember = domain.OrganizationRoleMember
)

var (
	ErrInvalidOrganizationRole = infraerrors.BadRequest(
		"INVALID_ORGANIZATION_ROLE",
		"organization role must be organization_admin or organization_member",
	)
	// This intentionally combines nonexistent, deleted, and already-organized
	// targets so the platform endpoint does not disclose another organization.
	ErrOrganizationMemberIneligible = infraerrors.Conflict(
		"ORGANIZATION_MEMBER_INELIGIBLE",
		"user cannot be added to this organization",
	)
	ErrInvalidOrganizationMemberTarget = infraerrors.BadRequest(
		"INVALID_ORGANIZATION_MEMBER_TARGET",
		"provide exactly one of email or user_id",
	)
	ErrOrganizationUnavailable = infraerrors.Forbidden(
		"ORGANIZATION_UNAVAILABLE",
		"Organization is unavailable",
	)
	ErrOrganizationMembershipUnavailable = infraerrors.ServiceUnavailable(
		"ORGANIZATION_MEMBERSHIP_UNAVAILABLE",
		"organization membership service is unavailable",
	)
	ErrOrganizationMemberNotFound = infraerrors.NotFound(
		"ORGANIZATION_MEMBER_NOT_FOUND",
		"organization member not found",
	)
	ErrOrganizationMembershipInconsistent = infraerrors.Conflict(
		"ORGANIZATION_MEMBERSHIP_INCONSISTENT",
		"organization membership is inconsistent",
	)
	// ErrOrganizationKeyAttributionInvalid is the request-path-facing form of
	// inconsistent organization attribution. Its cause retains compatibility for
	// existing errors.Is(err, ErrOrganizationMembershipInconsistent) callers.
	ErrOrganizationKeyAttributionInvalid = infraerrors.Forbidden(
		"ORGANIZATION_KEY_ATTRIBUTION_INVALID",
		"organization key authorization is invalid",
	)
	// ErrOrganizationKeyAuthorizationUnavailable is deliberately distinct from
	// an inconsistent membership: the resolver could not establish the trusted
	// attribution snapshot from persistent storage, so callers must fail closed.
	ErrOrganizationKeyAuthorizationUnavailable = infraerrors.ServiceUnavailable(
		"ORGANIZATION_KEY_AUTHORIZATION_UNAVAILABLE",
		"organization key authorization is unavailable",
	)
	ErrOrganizationMembershipMustBeRemoved = infraerrors.Conflict(
		"ORGANIZATION_MEMBERSHIP_MUST_BE_REMOVED",
		"remove the user from its organization before deletion",
	)
	ErrOrganizationMemberRemovalForbidden = infraerrors.Forbidden(
		"ORGANIZATION_MEMBER_REMOVAL_FORBIDDEN",
		"current authority cannot remove this organization member",
	)
	ErrOrganizationAllocationHasReservations = infraerrors.Conflict(
		"ALLOCATION_HAS_RESERVATIONS",
		"organization allocation has unresolved reservations",
	)
	ErrOrganizationMemberRemovalInvalid = infraerrors.BadRequest(
		"ORGANIZATION_MEMBER_REMOVAL_INVALID",
		"organization member removal request is invalid",
	)
	ErrOrganizationMemberRemovalInvariant = infraerrors.InternalServer(
		"ORGANIZATION_MEMBER_REMOVAL_INVARIANT_BROKEN",
		"organization member removal invariant is invalid",
	)
)

// NewOrganizationKeyAttributionError reports a deliberately redacted alert
// before returning the stable, fail-closed authorization error.
func NewOrganizationKeyAttributionError(reason string, auth *OrganizationKeyAuthorization) error {
	ReportOrganizationKeyAttributionAlert(reason, auth)
	return ErrOrganizationKeyAttributionInvalid.WithCause(ErrOrganizationMembershipInconsistent)
}

// OrganizationMember is the redacted platform organization membership view.
// It intentionally omits personal balance, concurrency, quotas, subscriptions,
// API key secrets, and platform-role mutation controls.
type OrganizationMember struct {
	UserID           int64
	Email            string
	Username         string
	Status           string
	OrganizationRole string
	Allocation       OrganizationMemberAllocation
}

type OrganizationMemberAllocation struct {
	ID        int64
	Status    string
	Available string
	Reserved  string
}

// OrganizationMemberTarget is the exact registered user selected by an
// organization administrator. It intentionally has no organization or role
// field because both values are derived by the server.
type OrganizationMemberTarget struct {
	UserID *int64
	Email  string
}

type OrganizationMembershipRepository interface {
	List(ctx context.Context, organizationID int64, params pagination.PaginationParams) ([]OrganizationMember, *pagination.PaginationResult, error)
	Add(ctx context.Context, organizationID, userID int64, role string) (*OrganizationMember, error)
	ChangeRole(ctx context.Context, organizationID, userID int64, role string) (*OrganizationMember, error)
}

type CurrentOrganizationMembershipRepository interface {
	AddCurrentMember(ctx context.Context, organizationID int64, target OrganizationMemberTarget) (*OrganizationMember, error)
}

type OrganizationMemberRemovalRepository interface {
	Remove(ctx context.Context, command OrganizationMemberRemovalCommand) (*OrganizationMemberRemovalResult, error)
}

type OrganizationMembershipManager interface {
	List(ctx context.Context, organizationID int64, params pagination.PaginationParams) ([]OrganizationMember, *pagination.PaginationResult, error)
	Add(ctx context.Context, organizationID, userID int64, role string) (*OrganizationMember, error)
	ChangeRole(ctx context.Context, organizationID, userID int64, role string) (*OrganizationMember, error)
}

type CurrentOrganizationMembershipManager interface {
	List(ctx context.Context, organizationID int64, params pagination.PaginationParams) ([]OrganizationMember, *pagination.PaginationResult, error)
	AddCurrentMember(ctx context.Context, organizationID int64, target OrganizationMemberTarget) (*OrganizationMember, error)
}

type OrganizationMemberRemovalManager interface {
	Remove(ctx context.Context, actor OrganizationMemberRemovalActor, organizationID, targetUserID int64, input OrganizationMemberRemovalInput, idempotencyKey string) (*OrganizationMemberRemovalResult, error)
}

type OrganizationMembershipBillingCacheInvalidator interface {
	InvalidateUserBalance(ctx context.Context, userID int64) error
	InvalidateSubscription(ctx context.Context, userID, groupID int64) error
	InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error
	InvalidateUserPlatformQuota(ctx context.Context, userID int64, platform string) error
}

// OrganizationMemberCacheTarget contains only stable IDs required by the
// shared post-commit auth and billing cache invalidation path.
type OrganizationMemberCacheTarget struct {
	UserID    int64
	APIKeyIDs []int64
	GroupIDs  []int64
}

type OrganizationMemberRemovalActor struct {
	UserID                int64
	DisplayName           string
	PlatformRole          string
	OrganizationRole      string
	AuthorizationScope    string
	CurrentOrganizationID int64
	Active                bool
}

type OrganizationMemberRemovalInput struct {
	PublicReason string `json:"public_reason"`
}

type OrganizationMemberRemovalCommand struct {
	Actor                OrganizationMemberRemovalActor
	OrganizationID       int64
	TargetUserID         int64
	OperationType        string
	AuthorizationScope   string
	IdempotencyKeyHash   string
	CanonicalPayloadHash string
	PublicReason         string
	InternalReason       string
	OperationID          string
	CorrelationID        string
	OccurredAt           time.Time
}

type OrganizationMemberRemovalResult struct {
	OperationID          string  `json:"operation_id"`
	CorrelationID        string  `json:"correlation_id"`
	OrganizationID       int64   `json:"organization_id"`
	UserID               int64   `json:"user_id"`
	AllocationID         int64   `json:"allocation_id"`
	OperationType        string  `json:"operation_type"`
	Currency             string  `json:"currency"`
	Outcome              string  `json:"outcome"`
	Reclaimed            string  `json:"reclaimed"`
	PoolBefore           string  `json:"pool_before"`
	PoolAfter            string  `json:"pool_after"`
	BalanceVersionBefore int64   `json:"balance_version_before"`
	BalanceVersionAfter  int64   `json:"balance_version_after"`
	CanonicalPayloadHash string  `json:"canonical_payload_hash"`
	Replayed             bool    `json:"replayed"`
	APIKeyIDs            []int64 `json:"-"`
	GroupIDs             []int64 `json:"-"`
}

type OrganizationMembershipService struct {
	repo               OrganizationMembershipRepository
	authInvalidator    APIKeyAuthCacheInvalidator
	billingInvalidator OrganizationMembershipBillingCacheInvalidator
}

func NewOrganizationMembershipService(
	repo OrganizationMembershipRepository,
	authInvalidator APIKeyAuthCacheInvalidator,
	billingInvalidator OrganizationMembershipBillingCacheInvalidator,
) *OrganizationMembershipService {
	return &OrganizationMembershipService{
		repo:               repo,
		authInvalidator:    authInvalidator,
		billingInvalidator: billingInvalidator,
	}
}

func (s *OrganizationMembershipService) List(ctx context.Context, organizationID int64, params pagination.PaginationParams) ([]OrganizationMember, *pagination.PaginationResult, error) {
	if organizationID <= 0 {
		return nil, nil, ErrInvalidOrganizationID
	}
	items, page, err := s.repo.List(ctx, organizationID, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list organization members: %w", err)
	}
	return items, page, nil
}

func (s *OrganizationMembershipService) Add(ctx context.Context, organizationID, userID int64, role string) (*OrganizationMember, error) {
	if organizationID <= 0 {
		return nil, ErrInvalidOrganizationID
	}
	if userID <= 0 {
		return nil, ErrOrganizationMemberIneligible
	}
	role, err := normalizeOrganizationRole(role)
	if err != nil {
		return nil, err
	}
	member, err := s.repo.Add(ctx, organizationID, userID, role)
	if err != nil {
		return nil, fmt.Errorf("add organization member: %w", err)
	}
	s.invalidateUser(ctx, userID)
	return member, nil
}

// AddCurrentMember adds a user selected by an organization administrator. The
// target organization comes from the JWT-derived scope and this method always
// assigns the ordinary member role.
func (s *OrganizationMembershipService) AddCurrentMember(ctx context.Context, organizationID int64, target OrganizationMemberTarget) (*OrganizationMember, error) {
	if organizationID <= 0 {
		return nil, ErrInvalidOrganizationID
	}
	if err := normalizeOrganizationMemberTarget(&target); err != nil {
		return nil, err
	}
	repo, ok := s.repo.(CurrentOrganizationMembershipRepository)
	if !ok {
		return nil, ErrOrganizationMembershipUnavailable
	}
	member, err := repo.AddCurrentMember(ctx, organizationID, target)
	if err != nil {
		return nil, fmt.Errorf("add current organization member: %w", err)
	}
	s.invalidateUser(ctx, member.UserID)
	return member, nil
}

func (s *OrganizationMembershipService) ChangeRole(ctx context.Context, organizationID, userID int64, role string) (*OrganizationMember, error) {
	if organizationID <= 0 {
		return nil, ErrInvalidOrganizationID
	}
	if userID <= 0 {
		return nil, ErrOrganizationMemberNotFound
	}
	role, err := normalizeOrganizationRole(role)
	if err != nil {
		return nil, err
	}
	member, err := s.repo.ChangeRole(ctx, organizationID, userID, role)
	if err != nil {
		return nil, fmt.Errorf("change organization member role: %w", err)
	}
	s.invalidateUser(ctx, userID)
	return member, nil
}

func (s *OrganizationMembershipService) Remove(
	ctx context.Context,
	actor OrganizationMemberRemovalActor,
	organizationID, targetUserID int64,
	input OrganizationMemberRemovalInput,
	idempotencyKey string,
) (*OrganizationMemberRemovalResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationMembershipUnavailable
	}
	if err := ValidateOrganizationMemberRemovalActor(actor, organizationID); err != nil {
		return nil, err
	}
	if organizationID <= 0 || targetUserID <= 0 {
		return nil, ErrOrganizationMemberRemovalInvalid
	}
	publicReason := strings.TrimSpace(input.PublicReason)
	if len(publicReason) > 2000 {
		return nil, ErrOrganizationMemberRemovalInvalid
	}
	key, err := NormalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	payloadHash, err := hashOrganizationMemberRemovalPayload(organizationMemberRemovalPayload{
		OrganizationID: organizationID,
		OperationType:  domain.OrganizationFundingOperationMemberRemovalReclaim,
		Currency:       domain.OrganizationCurrencyUSD,
		MoneyScale:     domain.OrganizationMoneyScale,
		TargetUserID:   targetUserID,
		PublicReason:   publicReason,
	})
	if err != nil {
		return nil, ErrOrganizationMemberRemovalInvariant.WithCause(err)
	}
	repo, ok := s.repo.(OrganizationMemberRemovalRepository)
	if !ok {
		return nil, ErrOrganizationMembershipUnavailable
	}
	result, err := repo.Remove(ctx, OrganizationMemberRemovalCommand{
		Actor:                actor,
		OrganizationID:       organizationID,
		TargetUserID:         targetUserID,
		OperationType:        domain.OrganizationFundingOperationMemberRemovalReclaim,
		AuthorizationScope:   actor.AuthorizationScope,
		IdempotencyKeyHash:   HashIdempotencyKey(key),
		CanonicalPayloadHash: payloadHash,
		PublicReason:         publicReason,
		InternalReason:       "organization membership removed",
		OperationID:          uuid.NewString(),
		CorrelationID:        uuid.NewString(),
		OccurredAt:           time.Now().UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("remove organization member: %w", err)
	}
	if err := ValidateOrganizationMemberRemovalResult(result); err != nil {
		return nil, err
	}
	s.invalidateRemovalCaches(ctx, result)
	return result, nil
}

func normalizeOrganizationRole(value string) (string, error) {
	role := strings.ToLower(strings.TrimSpace(value))
	switch role {
	case OrganizationRoleAdmin, OrganizationRoleMember:
		return role, nil
	default:
		return "", ErrInvalidOrganizationRole
	}
}

func normalizeOrganizationMemberTarget(target *OrganizationMemberTarget) error {
	if target == nil {
		return ErrInvalidOrganizationMemberTarget
	}
	target.Email = strings.ToLower(strings.TrimSpace(target.Email))
	if target.UserID != nil {
		if *target.UserID <= 0 || target.Email != "" {
			return ErrInvalidOrganizationMemberTarget
		}
		return nil
	}
	if target.Email == "" {
		return ErrInvalidOrganizationMemberTarget
	}
	return nil
}

func (s *OrganizationMembershipService) invalidateUser(ctx context.Context, userID int64) {
	if s != nil && s.authInvalidator != nil {
		s.authInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
}

func (s *OrganizationMembershipService) invalidateRemovalCaches(ctx context.Context, result *OrganizationMemberRemovalResult) {
	if result == nil || result.UserID <= 0 {
		return
	}
	InvalidateOrganizationMemberCaches(ctx, s.authInvalidator, s.billingInvalidator, []OrganizationMemberCacheTarget{{
		UserID: result.UserID, APIKeyIDs: result.APIKeyIDs, GroupIDs: result.GroupIDs,
	}})
}

// InvalidateOrganizationMemberCaches is shared by membership and lifecycle
// mutations. Cache failures are logged and ignored because authoritative DB
// reads and locked execution admission remain the correctness boundary.
func InvalidateOrganizationMemberCaches(
	ctx context.Context,
	authInvalidator APIKeyAuthCacheInvalidator,
	billingInvalidator OrganizationMembershipBillingCacheInvalidator,
	targets []OrganizationMemberCacheTarget,
) {
	if len(targets) == 0 {
		return
	}
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, target := range targets {
		if target.UserID <= 0 {
			continue
		}
		if authInvalidator != nil {
			authInvalidator.InvalidateAuthCacheByUserID(cacheCtx, target.UserID)
		}
		if billingInvalidator == nil {
			continue
		}
		if err := billingInvalidator.InvalidateUserBalance(cacheCtx, target.UserID); err != nil {
			slog.Error("invalidate organization balance cache failed", "user_id", target.UserID, "error", err)
		}
		for _, platform := range AllowedQuotaPlatforms {
			if err := billingInvalidator.InvalidateUserPlatformQuota(cacheCtx, target.UserID, platform); err != nil {
				slog.Error("invalidate organization platform quota cache failed", "user_id", target.UserID, "platform", platform, "error", err)
			}
		}
		seenGroups := make(map[int64]struct{}, len(target.GroupIDs))
		for _, groupID := range target.GroupIDs {
			if groupID <= 0 {
				continue
			}
			if _, exists := seenGroups[groupID]; exists {
				continue
			}
			seenGroups[groupID] = struct{}{}
			if err := billingInvalidator.InvalidateSubscription(cacheCtx, target.UserID, groupID); err != nil {
				slog.Error("invalidate organization subscription cache failed", "user_id", target.UserID, "group_id", groupID, "error", err)
			}
		}
		for _, keyID := range target.APIKeyIDs {
			if keyID <= 0 {
				continue
			}
			if err := billingInvalidator.InvalidateAPIKeyRateLimit(cacheCtx, keyID); err != nil {
				slog.Error("invalidate organization api key rate limit cache failed", "user_id", target.UserID, "api_key_id", keyID, "error", err)
			}
		}
	}
}

func ValidateOrganizationMemberRemovalActor(actor OrganizationMemberRemovalActor, organizationID int64) error {
	if actor.UserID <= 0 || !actor.Active || strings.TrimSpace(actor.DisplayName) == "" {
		return ErrOrganizationMemberRemovalForbidden
	}
	switch actor.AuthorizationScope {
	case domain.OrganizationFundingAuthorizationPlatformAdminJWT:
		if actor.PlatformRole != domain.RoleAdmin {
			return ErrOrganizationMemberRemovalForbidden
		}
	case domain.OrganizationFundingAuthorizationOrganizationJWT:
		if actor.OrganizationRole != domain.OrganizationRoleAdmin || actor.CurrentOrganizationID != organizationID {
			return ErrOrganizationMemberRemovalForbidden
		}
	default:
		return ErrOrganizationMemberRemovalForbidden
	}
	return nil
}

func ValidateOrganizationMemberRemovalResult(result *OrganizationMemberRemovalResult) error {
	if result == nil || result.OperationID == "" || result.CorrelationID == "" ||
		result.OrganizationID <= 0 || result.UserID <= 0 || result.AllocationID <= 0 ||
		result.OperationType != domain.OrganizationFundingOperationMemberRemovalReclaim ||
		result.Currency != domain.OrganizationCurrencyUSD || result.Outcome != OrganizationAllocationOutcomeCommitted ||
		result.BalanceVersionBefore < 0 ||
		result.BalanceVersionAfter <= result.BalanceVersionBefore || len(result.CanonicalPayloadHash) != 64 {
		return ErrOrganizationMemberRemovalInvariant
	}
	for _, amount := range []string{result.Reclaimed, result.PoolBefore, result.PoolAfter} {
		if _, err := NormalizeOrganizationMoney(amount, false); err != nil {
			return ErrOrganizationMemberRemovalInvariant.WithCause(err)
		}
	}
	return nil
}

type organizationMemberRemovalPayload struct {
	OrganizationID int64  `json:"organization_id"`
	OperationType  string `json:"operation_type"`
	Currency       string `json:"currency"`
	MoneyScale     int    `json:"money_scale"`
	TargetUserID   int64  `json:"target_user_id"`
	PublicReason   string `json:"public_reason,omitempty"`
}

func hashOrganizationMemberRemovalPayload(payload organizationMemberRemovalPayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
