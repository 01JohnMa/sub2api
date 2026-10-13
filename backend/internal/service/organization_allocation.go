package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const organizationAllocationPreviewTTL = 5 * time.Minute

const (
	OrganizationAllocationOutcomeCommitted = "committed"
	OrganizationAllocationOutcomeNoOp      = "no_op"

	OrganizationAllocationNoOpNoEligible     = "no_eligible_members"
	OrganizationAllocationNoOpZeroPool       = "zero_pool"
	OrganizationAllocationNoOpBelowPrecision = "below_precision"

	OrganizationAllocationExclusionSelf         = "caller"
	OrganizationAllocationExclusionAdmin        = "organization_admin"
	OrganizationAllocationExclusionInactive     = "inactive_user"
	OrganizationAllocationExclusionNoAllocation = "missing_allocation"
)

var (
	ErrOrganizationAllocationForbidden = infraerrors.Forbidden(
		"ORGANIZATION_ALLOCATION_FORBIDDEN",
		"the current authority cannot allocate or reclaim organization funds",
	)
	ErrOrganizationAllocationInvalid = infraerrors.BadRequest(
		"ORGANIZATION_ALLOCATION_INVALID",
		"organization allocation request is invalid",
	)
	ErrOrganizationAllocationTargetRequired = infraerrors.BadRequest(
		"ORGANIZATION_ALLOCATION_TARGET_REQUIRED",
		"at least one allocation target is required",
	)
	ErrOrganizationAllocationDuplicateTarget = infraerrors.BadRequest(
		"ORGANIZATION_ALLOCATION_DUPLICATE_TARGET",
		"an allocation target may appear only once",
	)
	ErrOrganizationAllocationTargetNotFound = infraerrors.NotFound(
		"ORGANIZATION_ALLOCATION_TARGET_NOT_FOUND",
		"organization allocation target not found",
	)
	ErrOrganizationAllocationInsufficientPool = infraerrors.Conflict(
		"ORGANIZATION_ALLOCATION_INSUFFICIENT_POOL",
		"organization unallocated funds are insufficient",
	)
	ErrOrganizationAllocationInsufficientAvailable = infraerrors.Conflict(
		"ORGANIZATION_ALLOCATION_INSUFFICIENT_AVAILABLE",
		"member available organization funds are insufficient",
	)
	ErrOrganizationAllocationAmountOverflow = infraerrors.BadRequest(
		"ORGANIZATION_ALLOCATION_AMOUNT_OVERFLOW",
		"organization allocation exceeds DECIMAL(20,8) capacity",
	)
	ErrOrganizationAllocationStalePreview = infraerrors.Conflict(
		"ORGANIZATION_ALLOCATION_STALE_PREVIEW",
		"the allocation preview is stale and must be confirmed again",
	)
	ErrOrganizationAllocationPreviewInvalid = infraerrors.Conflict(
		"ORGANIZATION_ALLOCATION_PREVIEW_INVALID",
		"the allocation preview token is invalid or expired",
	)
	ErrOrganizationAllocationRepositoryUnavailable = infraerrors.ServiceUnavailable(
		"ORGANIZATION_ALLOCATION_REPOSITORY_UNAVAILABLE",
		"organization allocation storage is unavailable",
	)
	ErrOrganizationAllocationInvariant = infraerrors.InternalServer(
		"ORGANIZATION_ALLOCATION_INVARIANT_BROKEN",
		"organization allocation invariant is invalid",
	)
	ErrOrganizationAllocationPreviewUnavailable = infraerrors.ServiceUnavailable(
		"ORGANIZATION_ALLOCATION_PREVIEW_UNAVAILABLE",
		"organization allocation preview signing is unavailable",
	)
)

// OrganizationAllocationActor is a server-captured authorization snapshot.
// The repository revalidates the actor against persisted state before a write;
// these fields also bind a preview to the authorization boundary.
type OrganizationAllocationActor struct {
	UserID                int64
	DisplayName           string
	PlatformRole          string
	OrganizationRole      string
	AuthorizationScope    string
	CurrentOrganizationID int64
	Active                bool
}

type OrganizationAllocationTargetInput struct {
	UserID int64  `json:"user_id"`
	Amount string `json:"amount"`
}

type OrganizationAllocationPreviewInput struct {
	Targets      []OrganizationAllocationTargetInput `json:"targets,omitempty"`
	PublicReason string                              `json:"public_reason,omitempty"`
}

type OrganizationAllocationMemberSnapshot struct {
	UserID           int64
	UserStatus       string
	OrganizationRole string
	AllocationID     int64
	AllocationStatus string
	Available        string
	Reserved         string
}

type OrganizationAllocationSnapshot struct {
	OrganizationID int64
	Status         string
	OrganizationAt time.Time
	BalanceVersion int64
	Unallocated    string
	Members        []OrganizationAllocationMemberSnapshot
}

type OrganizationAllocationMemberPreview struct {
	UserID           int64  `json:"user_id"`
	UserStatus       string `json:"user_status"`
	OrganizationRole string `json:"organization_role"`
	AllocationID     int64  `json:"allocation_id"`
	AllocationStatus string `json:"allocation_status"`
	Available        string `json:"available"`
	Reserved         string `json:"reserved"`
	ExcludedReason   string `json:"excluded_reason,omitempty"`
}

type OrganizationAllocationTargetPreview struct {
	UserID       int64  `json:"user_id"`
	AllocationID int64  `json:"allocation_id"`
	Before       string `json:"before"`
	Delta        string `json:"delta"`
	After        string `json:"after"`
}

// OrganizationAllocationConfirmation is the exact, canonical plan returned
// by preview and echoed by execute. It intentionally contains no secrets.
type OrganizationAllocationConfirmation struct {
	OrganizationID     int64                                 `json:"organization_id"`
	AuthorizationScope string                                `json:"authorization_scope"`
	OperationType      string                                `json:"operation_type"`
	Currency           string                                `json:"currency"`
	MoneyScale         int                                   `json:"money_scale"`
	OrganizationAt     string                                `json:"organization_at"`
	BalanceVersion     int64                                 `json:"balance_version"`
	SnapshotHash       string                                `json:"snapshot_hash"`
	PoolBefore         string                                `json:"pool_before"`
	PoolAfter          string                                `json:"pool_after"`
	TotalDelta         string                                `json:"total_delta"`
	RoundingRemainder  string                                `json:"rounding_remainder"`
	PublicReason       string                                `json:"public_reason,omitempty"`
	NoOpReason         string                                `json:"no_op_reason,omitempty"`
	Targets            []OrganizationAllocationTargetPreview `json:"targets"`
}

type OrganizationAllocationPreview struct {
	Confirmation         OrganizationAllocationConfirmation    `json:"confirmation"`
	EligibleMembers      []OrganizationAllocationMemberPreview `json:"eligible_members"`
	ExcludedMembers      []OrganizationAllocationMemberPreview `json:"excluded_members"`
	CanonicalPayloadHash string                                `json:"canonical_payload_hash"`
	PreviewToken         string                                `json:"preview_token"`
	ExpiresAt            time.Time                             `json:"expires_at"`
}

type OrganizationAllocationExecuteInput struct {
	PreviewToken string                             `json:"preview_token"`
	Confirmation OrganizationAllocationConfirmation `json:"confirmation"`
}

type OrganizationAllocationCommittedQuery struct {
	Actor                OrganizationAllocationActor
	OrganizationID       int64
	OperationType        string
	IdempotencyKeyHash   string
	CanonicalPayloadHash string
}

type OrganizationAllocationExecuteCommand struct {
	Actor                OrganizationAllocationActor
	Confirmation         OrganizationAllocationConfirmation
	PreviewToken         string
	IdempotencyKeyHash   string
	CanonicalPayloadHash string
	OperationID          string
	CorrelationID        string
	OccurredAt           time.Time
	InternalReason       string
}

type OrganizationAllocationResult struct {
	OperationID          string                    `json:"operation_id"`
	CorrelationID        string                    `json:"correlation_id"`
	OrganizationID       int64                     `json:"organization_id"`
	OperationType        string                    `json:"operation_type"`
	Currency             string                    `json:"currency"`
	Outcome              string                    `json:"outcome"`
	NoOpReason           string                    `json:"no_op_reason,omitempty"`
	PoolBefore           string                    `json:"pool_before"`
	PoolAfter            string                    `json:"pool_after"`
	TotalDelta           string                    `json:"total_delta"`
	BalanceVersionBefore int64                     `json:"balance_version_before"`
	BalanceVersionAfter  int64                     `json:"balance_version_after"`
	CanonicalPayloadHash string                    `json:"canonical_payload_hash"`
	Postings             []OrganizationLedgerEntry `json:"postings"`
	Replayed             bool                      `json:"replayed"`
}

type OrganizationAllocationPlan struct {
	Confirmation    OrganizationAllocationConfirmation
	EligibleMembers []OrganizationAllocationMemberPreview
	ExcludedMembers []OrganizationAllocationMemberPreview
}

type OrganizationAllocationRepository interface {
	LoadPreviewSnapshot(ctx context.Context, actor OrganizationAllocationActor, organizationID int64, operationType string) (*OrganizationAllocationSnapshot, error)
	FindCommitted(ctx context.Context, query OrganizationAllocationCommittedQuery) (*OrganizationAllocationResult, error)
	Execute(ctx context.Context, command OrganizationAllocationExecuteCommand) (*OrganizationAllocationResult, error)
}

type OrganizationAllocationManager interface {
	Preview(ctx context.Context, actor OrganizationAllocationActor, organizationID int64, operationType string, input OrganizationAllocationPreviewInput) (*OrganizationAllocationPreview, error)
	Execute(ctx context.Context, actor OrganizationAllocationActor, organizationID int64, operationType string, input OrganizationAllocationExecuteInput, idempotencyKey string) (*OrganizationAllocationResult, error)
}

type OrganizationAllocationService struct {
	repo       OrganizationAllocationRepository
	previewKey []byte
	now        func() time.Time
	previewTTL time.Duration
}

func NewOrganizationAllocationService(repo OrganizationAllocationRepository, previewSecret string) (*OrganizationAllocationService, error) {
	if repo == nil {
		return nil, ErrOrganizationAllocationRepositoryUnavailable
	}
	secret := []byte(previewSecret)
	if len(secret) < 32 {
		return nil, ErrOrganizationAllocationPreviewUnavailable
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("sub2api.organization-allocation-preview.v1"))
	return &OrganizationAllocationService{
		repo:       repo,
		previewKey: mac.Sum(nil),
		now:        func() time.Time { return time.Now().UTC() },
		previewTTL: organizationAllocationPreviewTTL,
	}, nil
}

func (s *OrganizationAllocationService) Preview(ctx context.Context, actor OrganizationAllocationActor, organizationID int64, operationType string, input OrganizationAllocationPreviewInput) (*OrganizationAllocationPreview, error) {
	if s == nil || s.repo == nil || len(s.previewKey) == 0 {
		return nil, ErrOrganizationAllocationPreviewUnavailable
	}
	if err := ValidateOrganizationAllocationActor(actor, organizationID, operationType); err != nil {
		return nil, err
	}
	snapshot, err := s.repo.LoadPreviewSnapshot(ctx, actor, organizationID, operationType)
	if err != nil {
		return nil, err
	}
	plan, err := BuildOrganizationAllocationPlan(snapshot, actor, organizationID, operationType, input)
	if err != nil {
		return nil, err
	}
	now := s.clockNow()
	expiresAt := now.Add(s.previewTTL)
	confirmationHash, err := hashAllocationConfirmation(plan.Confirmation)
	if err != nil {
		return nil, ErrOrganizationAllocationInvariant.WithCause(err)
	}
	token, err := s.signPreviewToken(allocationPreviewClaims{
		Version:            1,
		OrganizationID:     organizationID,
		AuthorizationScope: actor.AuthorizationScope,
		OperationType:      operationType,
		ConfirmationHash:   confirmationHash,
		IssuedAt:           now.Unix(),
		ExpiresAt:          expiresAt.Unix(),
	})
	if err != nil {
		return nil, err
	}
	payloadHash, err := hashAllocationExecutionPayload(token, plan.Confirmation)
	if err != nil {
		return nil, ErrOrganizationAllocationInvariant.WithCause(err)
	}
	return &OrganizationAllocationPreview{
		Confirmation:         plan.Confirmation,
		EligibleMembers:      cloneMemberPreviews(plan.EligibleMembers),
		ExcludedMembers:      cloneMemberPreviews(plan.ExcludedMembers),
		CanonicalPayloadHash: payloadHash,
		PreviewToken:         token,
		ExpiresAt:            time.Unix(expiresAt.Unix(), 0).UTC(),
	}, nil
}

func (s *OrganizationAllocationService) Execute(ctx context.Context, actor OrganizationAllocationActor, organizationID int64, operationType string, input OrganizationAllocationExecuteInput, idempotencyKey string) (*OrganizationAllocationResult, error) {
	if s == nil || s.repo == nil || len(s.previewKey) == 0 {
		return nil, ErrOrganizationAllocationPreviewUnavailable
	}
	if err := ValidateOrganizationAllocationActor(actor, organizationID, operationType); err != nil {
		return nil, err
	}
	key, err := NormalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	payloadHash, err := hashAllocationExecutionPayload(input.PreviewToken, input.Confirmation)
	if err != nil {
		return nil, ErrOrganizationAllocationInvariant.WithCause(err)
	}
	lookup := OrganizationAllocationCommittedQuery{
		Actor:                actor,
		OrganizationID:       organizationID,
		OperationType:        operationType,
		IdempotencyKeyHash:   HashIdempotencyKey(key),
		CanonicalPayloadHash: payloadHash,
	}
	committed, err := s.repo.FindCommitted(ctx, lookup)
	if err != nil {
		return nil, err
	}
	if committed != nil {
		committed.Replayed = true
		return committed, ValidateOrganizationAllocationResult(committed)
	}

	claims, err := s.verifyPreviewToken(input.PreviewToken)
	if err != nil {
		// A lost response may race the first commit. Re-check the durable record
		// before exposing a stale-token error.
		if replay, replayErr := s.repo.FindCommitted(ctx, lookup); replayErr == nil && replay != nil {
			replay.Replayed = true
			return replay, ValidateOrganizationAllocationResult(replay)
		}
		return nil, err
	}
	if claims.OrganizationID != organizationID || claims.OperationType != operationType || claims.AuthorizationScope != actor.AuthorizationScope {
		return nil, ErrOrganizationAllocationStalePreview
	}
	confirmationHash, err := hashAllocationConfirmation(input.Confirmation)
	if err != nil || confirmationHash != claims.ConfirmationHash {
		return nil, ErrOrganizationAllocationStalePreview
	}
	if input.Confirmation.OrganizationID != organizationID || input.Confirmation.OperationType != operationType || input.Confirmation.AuthorizationScope != actor.AuthorizationScope {
		return nil, ErrOrganizationAllocationStalePreview
	}
	now := s.clockNow()
	if now.Unix() >= claims.ExpiresAt {
		return nil, ErrOrganizationAllocationPreviewInvalid
	}
	command := OrganizationAllocationExecuteCommand{
		Actor:                actor,
		Confirmation:         input.Confirmation,
		PreviewToken:         input.PreviewToken,
		IdempotencyKeyHash:   HashIdempotencyKey(key),
		CanonicalPayloadHash: payloadHash,
		OperationID:          uuid.NewString(),
		CorrelationID:        uuid.NewString(),
		OccurredAt:           now,
		InternalReason:       allocationInternalReason(operationType),
	}
	result, err := s.repo.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	if err := ValidateOrganizationAllocationResult(result); err != nil {
		return nil, err
	}
	return result, nil
}

func ValidateOrganizationAllocationActor(actor OrganizationAllocationActor, organizationID int64, operationType string) error {
	if actor.UserID <= 0 || organizationID <= 0 || !actor.Active || strings.TrimSpace(actor.DisplayName) == "" || len(strings.TrimSpace(actor.DisplayName)) > 255 {
		return ErrOrganizationAllocationForbidden
	}
	if actor.AuthorizationScope != domain.OrganizationFundingAuthorizationOrganizationJWT && actor.AuthorizationScope != domain.OrganizationFundingAuthorizationPlatformAdminJWT {
		return ErrOrganizationAllocationForbidden
	}
	if actor.AuthorizationScope == domain.OrganizationFundingAuthorizationPlatformAdminJWT {
		if actor.PlatformRole != domain.RoleAdmin || operationType == domain.OrganizationFundingOperationEqualAllocation {
			return ErrOrganizationAllocationForbidden
		}
	} else {
		if actor.OrganizationRole != domain.OrganizationRoleAdmin || actor.CurrentOrganizationID != organizationID {
			return ErrOrganizationAllocationForbidden
		}
	}
	switch operationType {
	case domain.OrganizationFundingOperationEqualAllocation,
		domain.OrganizationFundingOperationCustomAllocation,
		domain.OrganizationFundingOperationReclaim:
		return nil
	default:
		return ErrOrganizationAllocationInvalid
	}
}

func BuildOrganizationAllocationPlan(snapshot *OrganizationAllocationSnapshot, actor OrganizationAllocationActor, organizationID int64, operationType string, input OrganizationAllocationPreviewInput) (*OrganizationAllocationPlan, error) {
	if snapshot == nil || snapshot.OrganizationID != organizationID || snapshot.OrganizationAt.IsZero() {
		return nil, ErrOrganizationAllocationInvariant
	}
	if err := ValidateOrganizationAllocationActor(actor, organizationID, operationType); err != nil {
		return nil, err
	}
	if err := ValidateOrganizationAllocationLifecycle(snapshot.Status, actor.AuthorizationScope, operationType); err != nil {
		return nil, err
	}
	publicReason := strings.TrimSpace(input.PublicReason)
	if len(publicReason) > 2000 {
		return nil, ErrOrganizationAllocationInvalid
	}
	members := append([]OrganizationAllocationMemberSnapshot(nil), snapshot.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i].UserID < members[j].UserID })
	for _, member := range members {
		if member.UserID <= 0 || member.AllocationID <= 0 || member.AllocationStatus != domain.OrganizationAllocationStatusActive {
			return nil, ErrOrganizationAllocationInvariant
		}
		if _, err := canonicalStoredMoney(member.Available); err != nil {
			return nil, ErrOrganizationAllocationInvariant.WithCause(err)
		}
		if _, err := canonicalStoredMoney(member.Reserved); err != nil {
			return nil, ErrOrganizationAllocationInvariant.WithCause(err)
		}
	}
	pool, err := canonicalStoredMoney(snapshot.Unallocated)
	if err != nil {
		return nil, ErrOrganizationAllocationInvariant.WithCause(err)
	}
	snapshotHash, err := HashOrganizationAllocationSnapshot(snapshot)
	if err != nil {
		return nil, ErrOrganizationAllocationInvariant.WithCause(err)
	}

	eligible := make([]OrganizationAllocationMemberPreview, 0, len(members))
	excluded := make([]OrganizationAllocationMemberPreview, 0, len(members))
	eligibleByID := make(map[int64]OrganizationAllocationMemberPreview, len(members))
	allMembersByID := make(map[int64]OrganizationAllocationMemberPreview, len(members))
	for _, member := range members {
		preview := memberPreviewFromSnapshot(member)
		allMembersByID[member.UserID] = preview
		reason := allocationExclusionReason(member, actor)
		if reason == "" {
			eligible = append(eligible, preview)
			eligibleByID[member.UserID] = preview
		} else {
			preview.ExcludedReason = reason
			excluded = append(excluded, preview)
		}
	}

	if operationType == domain.OrganizationFundingOperationEqualAllocation && len(input.Targets) != 0 {
		return nil, ErrOrganizationAllocationInvalid
	}
	if operationType != domain.OrganizationFundingOperationEqualAllocation && len(input.Targets) == 0 {
		return nil, ErrOrganizationAllocationTargetRequired
	}
	if operationType == domain.OrganizationFundingOperationEqualAllocation {
		plan, err := buildEqualPlan(snapshot, actor, operationType, publicReason, snapshotHash, pool, eligible)
		if err != nil {
			return nil, err
		}
		plan.ExcludedMembers = excluded
		return plan, nil
	}
	plan, err := buildExplicitPlan(snapshot, actor, operationType, publicReason, snapshotHash, pool, input.Targets, eligibleByID, allMembersByID)
	if err != nil {
		return nil, err
	}
	plan.ExcludedMembers = excluded
	return plan, nil
}

func ValidateOrganizationAllocationConfirmation(snapshot *OrganizationAllocationSnapshot, actor OrganizationAllocationActor, confirmation OrganizationAllocationConfirmation) error {
	input := OrganizationAllocationPreviewInput{PublicReason: confirmation.PublicReason}
	if confirmation.OperationType != domain.OrganizationFundingOperationEqualAllocation {
		for _, target := range confirmation.Targets {
			amount := target.Delta
			if confirmation.OperationType == domain.OrganizationFundingOperationReclaim {
				parsed, err := decimal.NewFromString(amount)
				if err != nil {
					return ErrOrganizationAllocationStalePreview
				}
				amount = parsed.Abs().StringFixed(domain.OrganizationMoneyScale)
			}
			input.Targets = append(input.Targets, OrganizationAllocationTargetInput{UserID: target.UserID, Amount: amount})
		}
	}
	plan, err := BuildOrganizationAllocationPlan(snapshot, actor, confirmation.OrganizationID, confirmation.OperationType, input)
	if err != nil {
		return ErrOrganizationAllocationStalePreview
	}
	if !reflect.DeepEqual(plan.Confirmation, confirmation) {
		return ErrOrganizationAllocationStalePreview
	}
	return nil
}

func HashOrganizationAllocationSnapshot(snapshot *OrganizationAllocationSnapshot) (string, error) {
	if snapshot == nil {
		return "", ErrOrganizationAllocationInvariant
	}
	type memberHash struct {
		UserID           int64  `json:"user_id"`
		UserStatus       string `json:"user_status"`
		OrganizationRole string `json:"organization_role"`
		AllocationID     int64  `json:"allocation_id"`
		AllocationStatus string `json:"allocation_status"`
		Available        string `json:"available"`
		Reserved         string `json:"reserved"`
	}
	members := make([]memberHash, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		members = append(members, memberHash{
			UserID: member.UserID, UserStatus: member.UserStatus, OrganizationRole: member.OrganizationRole,
			AllocationID: member.AllocationID, AllocationStatus: member.AllocationStatus,
			Available: member.Available, Reserved: member.Reserved,
		})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].UserID < members[j].UserID })
	payload := struct {
		OrganizationID int64        `json:"organization_id"`
		Status         string       `json:"status"`
		OrganizationAt string       `json:"organization_at"`
		BalanceVersion int64        `json:"balance_version"`
		Unallocated    string       `json:"unallocated"`
		Members        []memberHash `json:"members"`
	}{
		OrganizationID: snapshot.OrganizationID,
		Status:         snapshot.Status,
		OrganizationAt: snapshot.OrganizationAt.UTC().Format(time.RFC3339Nano),
		BalanceVersion: snapshot.BalanceVersion,
		Unallocated:    snapshot.Unallocated,
		Members:        members,
	}
	return hashJSON(payload)
}

func ValidateOrganizationAllocationResult(result *OrganizationAllocationResult) error {
	if result == nil || result.OrganizationID <= 0 || result.Currency != domain.OrganizationCurrencyUSD || !validSHA256(result.CanonicalPayloadHash) {
		return ErrOrganizationAllocationInvariant
	}
	switch result.OperationType {
	case domain.OrganizationFundingOperationEqualAllocation,
		domain.OrganizationFundingOperationCustomAllocation,
		domain.OrganizationFundingOperationReclaim:
	default:
		return ErrOrganizationAllocationInvariant
	}
	if result.Outcome != OrganizationAllocationOutcomeCommitted && result.Outcome != OrganizationAllocationOutcomeNoOp {
		return ErrOrganizationAllocationInvariant
	}
	poolBefore, err := parseCanonicalAllocationResultMoney(result.PoolBefore, false)
	if err != nil {
		return ErrOrganizationAllocationInvariant.WithCause(err)
	}
	poolAfter, err := parseCanonicalAllocationResultMoney(result.PoolAfter, false)
	if err != nil {
		return ErrOrganizationAllocationInvariant.WithCause(err)
	}
	totalDelta, err := parseCanonicalAllocationResultMoney(result.TotalDelta, true)
	if err != nil {
		return ErrOrganizationAllocationInvariant.WithCause(err)
	}
	if !poolBefore.Sub(totalDelta).Equal(poolAfter) || result.BalanceVersionBefore < 0 || result.BalanceVersionAfter < 0 {
		return ErrOrganizationAllocationInvariant
	}
	if _, err := uuid.Parse(result.OperationID); err != nil {
		return ErrOrganizationAllocationInvariant.WithCause(err)
	}
	if _, err := uuid.Parse(result.CorrelationID); err != nil {
		return ErrOrganizationAllocationInvariant.WithCause(err)
	}
	if result.Outcome == OrganizationAllocationOutcomeCommitted {
		if result.NoOpReason != "" || len(result.Postings) == 0 || result.BalanceVersionAfter != result.BalanceVersionBefore+1 {
			return ErrOrganizationAllocationInvariant
		}
		return nil
	}
	if len(result.Postings) != 0 || !totalDelta.IsZero() || !poolBefore.Equal(poolAfter) || result.BalanceVersionAfter != result.BalanceVersionBefore {
		return ErrOrganizationAllocationInvariant
	}
	switch result.NoOpReason {
	case OrganizationAllocationNoOpNoEligible,
		OrganizationAllocationNoOpZeroPool,
		OrganizationAllocationNoOpBelowPrecision:
		return nil
	default:
		return ErrOrganizationAllocationInvariant
	}
}

func parseCanonicalAllocationResultMoney(value string, allowNegative bool) (decimal.Decimal, error) {
	if strings.TrimSpace(value) != value || value == "" {
		return decimal.Zero, fmt.Errorf("non-canonical organization allocation amount %q", value)
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil || parsed.Truncate(domain.OrganizationMoneyScale).Cmp(parsed) != 0 || parsed.Abs().GreaterThan(organizationMaxMoney) {
		if err == nil {
			err = fmt.Errorf("invalid organization allocation amount %q", value)
		}
		return decimal.Zero, err
	}
	if !allowNegative && parsed.IsNegative() {
		return decimal.Zero, fmt.Errorf("negative organization allocation balance %q", value)
	}
	if parsed.StringFixed(domain.OrganizationMoneyScale) != value {
		return decimal.Zero, fmt.Errorf("non-canonical organization allocation amount %q", value)
	}
	return parsed, nil
}

func (s *OrganizationAllocationService) clockNow() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

type allocationPreviewClaims struct {
	Version            int    `json:"v"`
	OrganizationID     int64  `json:"organization_id"`
	AuthorizationScope string `json:"authorization_scope"`
	OperationType      string `json:"operation_type"`
	ConfirmationHash   string `json:"confirmation_hash"`
	IssuedAt           int64  `json:"issued_at"`
	ExpiresAt          int64  `json:"expires_at"`
}

func (s *OrganizationAllocationService) signPreviewToken(claims allocationPreviewClaims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", ErrOrganizationAllocationInvariant.WithCause(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.previewKey)
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + signature, nil
}

func (s *OrganizationAllocationService) verifyPreviewToken(token string) (allocationPreviewClaims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return allocationPreviewClaims{}, ErrOrganizationAllocationPreviewInvalid
	}
	mac := hmac.New(sha256.New, s.previewKey)
	_, _ = mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return allocationPreviewClaims{}, ErrOrganizationAllocationPreviewInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return allocationPreviewClaims{}, ErrOrganizationAllocationPreviewInvalid
	}
	var claims allocationPreviewClaims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Version != 1 || claims.OrganizationID <= 0 || claims.ExpiresAt <= 0 || claims.ConfirmationHash == "" {
		return allocationPreviewClaims{}, ErrOrganizationAllocationPreviewInvalid
	}
	if !validSHA256(claims.ConfirmationHash) {
		return allocationPreviewClaims{}, ErrOrganizationAllocationPreviewInvalid
	}
	if s.clockNow().Unix() >= claims.ExpiresAt {
		return allocationPreviewClaims{}, ErrOrganizationAllocationPreviewInvalid
	}
	return claims, nil
}

func buildEqualPlan(snapshot *OrganizationAllocationSnapshot, actor OrganizationAllocationActor, operationType, publicReason, snapshotHash string, pool decimal.Decimal, eligible []OrganizationAllocationMemberPreview) (*OrganizationAllocationPlan, error) {
	count := decimal.NewFromInt(int64(len(eligible)))
	share := decimal.Zero
	if !count.IsZero() {
		share = pool.Div(count).Truncate(domain.OrganizationMoneyScale)
	}
	noOpReason := ""
	if len(eligible) == 0 {
		noOpReason = OrganizationAllocationNoOpNoEligible
	} else if pool.IsZero() {
		noOpReason = OrganizationAllocationNoOpZeroPool
	} else if share.IsZero() {
		noOpReason = OrganizationAllocationNoOpBelowPrecision
	}
	targets := make([]OrganizationAllocationTargetPreview, 0, len(eligible))
	total := decimal.Zero
	for _, member := range eligible {
		before, _ := decimal.NewFromString(member.Available)
		after := before.Add(share)
		targets = append(targets, OrganizationAllocationTargetPreview{
			UserID: member.UserID, AllocationID: member.AllocationID,
			Before: before.StringFixed(domain.OrganizationMoneyScale),
			Delta:  share.StringFixed(domain.OrganizationMoneyScale),
			After:  after.StringFixed(domain.OrganizationMoneyScale),
		})
		total = total.Add(share)
		if after.GreaterThan(organizationMaxMoney) {
			return nil, ErrOrganizationAllocationAmountOverflow
		}
	}
	poolAfter := pool.Sub(total)
	if poolAfter.IsNegative() {
		return nil, ErrOrganizationAllocationInvariant
	}
	return &OrganizationAllocationPlan{
		EligibleMembers: eligible,
		Confirmation: OrganizationAllocationConfirmation{
			OrganizationID: organizationIDFromSnapshot(snapshot), AuthorizationScope: actor.AuthorizationScope,
			OperationType: operationType, Currency: domain.OrganizationCurrencyUSD, MoneyScale: domain.OrganizationMoneyScale,
			OrganizationAt: snapshot.OrganizationAt.UTC().Format(time.RFC3339Nano), BalanceVersion: snapshot.BalanceVersion,
			SnapshotHash: snapshotHash, PoolBefore: fixedMoney(pool), PoolAfter: fixedMoney(poolAfter),
			TotalDelta: fixedMoney(total), RoundingRemainder: fixedMoney(poolAfter), PublicReason: publicReason,
			NoOpReason: noOpReason, Targets: targets,
		},
	}, nil
}

func buildExplicitPlan(snapshot *OrganizationAllocationSnapshot, actor OrganizationAllocationActor, operationType, publicReason, snapshotHash string, pool decimal.Decimal, inputs []OrganizationAllocationTargetInput, eligible, allMembers map[int64]OrganizationAllocationMemberPreview) (*OrganizationAllocationPlan, error) {
	seen := make(map[int64]struct{}, len(inputs))
	targets := make([]OrganizationAllocationTargetPreview, 0, len(inputs))
	total := decimal.Zero
	for _, input := range inputs {
		if input.UserID <= 0 {
			return nil, ErrOrganizationAllocationTargetNotFound
		}
		if _, ok := seen[input.UserID]; ok {
			return nil, ErrOrganizationAllocationDuplicateTarget
		}
		seen[input.UserID] = struct{}{}
		member, ok := eligible[input.UserID]
		if !ok {
			if _, current := allMembers[input.UserID]; current {
				return nil, ErrOrganizationAllocationForbidden
			}
			return nil, ErrOrganizationAllocationTargetNotFound
		}
		amountText, err := NormalizeOrganizationMoney(input.Amount, true)
		if err != nil {
			return nil, err
		}
		amount, _ := decimal.NewFromString(amountText)
		before, _ := decimal.NewFromString(member.Available)
		delta := amount
		if operationType == domain.OrganizationFundingOperationReclaim {
			if amount.GreaterThan(before) {
				return nil, ErrOrganizationAllocationInsufficientAvailable
			}
			delta = amount.Neg()
		}
		after := before.Add(delta)
		if after.IsNegative() || after.GreaterThan(organizationMaxMoney) {
			return nil, ErrOrganizationAllocationAmountOverflow
		}
		targets = append(targets, OrganizationAllocationTargetPreview{
			UserID: member.UserID, AllocationID: member.AllocationID,
			Before: before.StringFixed(domain.OrganizationMoneyScale), Delta: delta.StringFixed(domain.OrganizationMoneyScale),
			After: after.StringFixed(domain.OrganizationMoneyScale),
		})
		total = total.Add(delta)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].UserID < targets[j].UserID })
	if operationType == domain.OrganizationFundingOperationCustomAllocation && total.GreaterThan(pool) {
		return nil, ErrOrganizationAllocationInsufficientPool
	}
	poolAfter := pool.Sub(total)
	if poolAfter.IsNegative() || poolAfter.GreaterThan(organizationMaxMoney) {
		return nil, ErrOrganizationAllocationInsufficientPool
	}
	return &OrganizationAllocationPlan{
		EligibleMembers: memberPreviewsFromMap(eligible),
		Confirmation: OrganizationAllocationConfirmation{
			OrganizationID: organizationIDFromSnapshot(snapshot), AuthorizationScope: actor.AuthorizationScope,
			OperationType: operationType, Currency: domain.OrganizationCurrencyUSD, MoneyScale: domain.OrganizationMoneyScale,
			OrganizationAt: snapshot.OrganizationAt.UTC().Format(time.RFC3339Nano), BalanceVersion: snapshot.BalanceVersion,
			SnapshotHash: snapshotHash, PoolBefore: fixedMoney(pool), PoolAfter: fixedMoney(poolAfter),
			TotalDelta: fixedMoney(total), RoundingRemainder: "0.00000000", PublicReason: publicReason, Targets: targets,
		},
	}, nil
}

func ValidateOrganizationAllocationLifecycle(status, scope, operationType string) error {
	if status == domain.OrganizationStatusArchived {
		return ErrOrganizationArchived
	}
	if scope == domain.OrganizationFundingAuthorizationOrganizationJWT && status != domain.OrganizationStatusActive {
		return ErrOrganizationUnavailable
	}
	if scope == domain.OrganizationFundingAuthorizationPlatformAdminJWT {
		if operationType == domain.OrganizationFundingOperationCustomAllocation && status != domain.OrganizationStatusActive {
			return ErrOrganizationAllocationForbidden
		}
		if operationType == domain.OrganizationFundingOperationReclaim && status != domain.OrganizationStatusActive && status != domain.OrganizationStatusDisabled {
			return ErrOrganizationAllocationForbidden
		}
	}
	return nil
}

func allocationExclusionReason(member OrganizationAllocationMemberSnapshot, actor OrganizationAllocationActor) string {
	if member.AllocationID <= 0 || member.AllocationStatus != domain.OrganizationAllocationStatusActive {
		return OrganizationAllocationExclusionNoAllocation
	}
	if actor.AuthorizationScope == domain.OrganizationFundingAuthorizationOrganizationJWT {
		if member.UserID == actor.UserID {
			return OrganizationAllocationExclusionSelf
		}
		if member.OrganizationRole == domain.OrganizationRoleAdmin {
			return OrganizationAllocationExclusionAdmin
		}
		if member.OrganizationRole != domain.OrganizationRoleMember || member.UserStatus != domain.StatusActive {
			return OrganizationAllocationExclusionInactive
		}
	}
	return ""
}

func memberPreviewFromSnapshot(member OrganizationAllocationMemberSnapshot) OrganizationAllocationMemberPreview {
	return OrganizationAllocationMemberPreview{
		UserID: member.UserID, UserStatus: member.UserStatus, OrganizationRole: member.OrganizationRole,
		AllocationID: member.AllocationID, AllocationStatus: member.AllocationStatus,
		Available: member.Available, Reserved: member.Reserved,
	}
}

func memberPreviewsFromMap(input map[int64]OrganizationAllocationMemberPreview) []OrganizationAllocationMemberPreview {
	result := make([]OrganizationAllocationMemberPreview, 0, len(input))
	for _, member := range input {
		result = append(result, member)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UserID < result[j].UserID })
	return result
}

func cloneMemberPreviews(input []OrganizationAllocationMemberPreview) []OrganizationAllocationMemberPreview {
	return append([]OrganizationAllocationMemberPreview(nil), input...)
}

func organizationIDFromSnapshot(snapshot *OrganizationAllocationSnapshot) int64 {
	if snapshot == nil {
		return 0
	}
	return snapshot.OrganizationID
}

func allocationInternalReason(operationType string) string {
	switch operationType {
	case domain.OrganizationFundingOperationEqualAllocation:
		return "organization equal allocation"
	case domain.OrganizationFundingOperationCustomAllocation:
		return "organization custom allocation"
	case domain.OrganizationFundingOperationReclaim:
		return "organization allocation reclaim"
	default:
		return "organization allocation mutation"
	}
}

func canonicalStoredMoney(raw string) (decimal.Decimal, error) {
	value := strings.TrimSpace(raw)
	parsed, err := decimal.NewFromString(value)
	if err != nil || parsed.IsNegative() || parsed.Truncate(domain.OrganizationMoneyScale).Cmp(parsed) != 0 || parsed.GreaterThan(organizationMaxMoney) {
		if err == nil {
			err = fmt.Errorf("invalid stored organization amount %q", raw)
		}
		return decimal.Zero, err
	}
	return parsed, nil
}

func fixedMoney(value decimal.Decimal) string {
	return value.StringFixed(domain.OrganizationMoneyScale)
}

func hashJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func hashAllocationConfirmation(confirmation OrganizationAllocationConfirmation) (string, error) {
	return hashJSON(confirmation)
}

func hashAllocationExecutionPayload(token string, confirmation OrganizationAllocationConfirmation) (string, error) {
	return hashJSON(struct {
		PreviewToken string                             `json:"preview_token"`
		Confirmation OrganizationAllocationConfirmation `json:"confirmation"`
	}{PreviewToken: token, Confirmation: confirmation})
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
