package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrOrganizationFundingForbidden = infraerrors.Forbidden(
		"ORGANIZATION_FUNDING_FORBIDDEN",
		"an active platform administrator JWT is required for organization funding",
	)
	ErrOrganizationFundingAmountInvalid = infraerrors.BadRequest(
		"ORGANIZATION_FUNDING_AMOUNT_INVALID",
		"organization amount must be a finite USD decimal with at most eight fractional digits",
	)
	ErrOrganizationFundingAmountRequired = infraerrors.BadRequest(
		"ORGANIZATION_FUNDING_AMOUNT_REQUIRED",
		"organization amount is required",
	)
	ErrOrganizationFundingAmountPositive = infraerrors.BadRequest(
		"ORGANIZATION_FUNDING_AMOUNT_NOT_POSITIVE",
		"organization credit must be greater than zero",
	)
	ErrOrganizationFundingAmountOverflow = infraerrors.BadRequest(
		"ORGANIZATION_FUNDING_AMOUNT_OVERFLOW",
		"organization amount exceeds DECIMAL(20,8) capacity",
	)
	ErrOrganizationFundingReasonRequired = infraerrors.BadRequest(
		"ORGANIZATION_FUNDING_REASON_REQUIRED",
		"an internal funding reason is required",
	)
	ErrOrganizationFundingRepositoryUnavailable = infraerrors.ServiceUnavailable(
		"ORGANIZATION_FUNDING_REPOSITORY_UNAVAILABLE",
		"organization funding storage is unavailable",
	)
	ErrOrganizationFundingInvariant = infraerrors.InternalServer(
		"ORGANIZATION_FUNDING_INVARIANT_BROKEN",
		"organization funding invariant is invalid",
	)
)

var organizationMoneyPattern = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]{1,8})?|\.[0-9]{1,8})$`)

var organizationMaxMoney = decimal.RequireFromString("999999999999.99999999")

// OrganizationFundingActor is the authorization snapshot captured by the
// interactive JWT boundary. The service rechecks these fields on every call,
// including an idempotent replay, before reading a historical result.
type OrganizationFundingActor struct {
	UserID             int64
	DisplayName        string
	PlatformRole       string
	AuthorizationScope string
	Active             bool
}

type OrganizationFundingReasons struct {
	PublicReason   string
	InternalReason string
}

type OrganizationFundingRepository interface {
	Credit(ctx context.Context, input OrganizationCreditCommand) (*OrganizationFundingResult, error)
	GetSummary(ctx context.Context, organizationID int64) (*OrganizationBalanceSummary, error)
	ListLedger(ctx context.Context, organizationID int64, params pagination.PaginationParams) ([]OrganizationLedgerEntry, *pagination.PaginationResult, error)
	GetReport(ctx context.Context, organizationID int64, reportRange OrganizationReportRange) (*OrganizationReport, error)
	ListReportMembers(ctx context.Context, organizationID int64, reportRange OrganizationReportRange, params pagination.PaginationParams) ([]OrganizationReportMemberUsage, *OrganizationReportMemberUsage, *pagination.PaginationResult, error)
}

type OrganizationFundingManager interface {
	Credit(ctx context.Context, actor OrganizationFundingActor, organizationID int64, amount string, reasons OrganizationFundingReasons, idempotencyKey string) (*OrganizationFundingResult, error)
	GetSummary(ctx context.Context, organizationID int64) (*OrganizationBalanceSummary, error)
	ListLedger(ctx context.Context, organizationID int64, params pagination.PaginationParams) ([]OrganizationLedgerEntry, *pagination.PaginationResult, error)
	GetReport(ctx context.Context, organizationID int64, reportRange OrganizationReportRange) (*OrganizationReport, error)
	ListReportMembers(ctx context.Context, organizationID int64, reportRange OrganizationReportRange, params pagination.PaginationParams) ([]OrganizationReportMemberUsage, *OrganizationReportMemberUsage, *pagination.PaginationResult, error)
}

type OrganizationFundingReader interface {
	GetSummary(ctx context.Context, organizationID int64) (*OrganizationBalanceSummary, error)
	ListLedger(ctx context.Context, organizationID int64, params pagination.PaginationParams) ([]OrganizationLedgerEntry, *pagination.PaginationResult, error)
	GetReport(ctx context.Context, organizationID int64, reportRange OrganizationReportRange) (*OrganizationReport, error)
	ListReportMembers(ctx context.Context, organizationID int64, reportRange OrganizationReportRange, params pagination.PaginationParams) ([]OrganizationReportMemberUsage, *OrganizationReportMemberUsage, *pagination.PaginationResult, error)
}

type OrganizationCreditCommand struct {
	OrganizationID       int64
	Amount               string
	Currency             string
	OperationType        string
	AuthorizationScope   string
	IdempotencyKeyHash   string
	CanonicalPayloadHash string
	Actor                OrganizationFundingActor
	PublicReason         *string
	InternalReason       string
	OperationID          string
	CorrelationID        string
	OccurredAt           time.Time
}

type OrganizationFundingResult struct {
	OperationID          string
	CorrelationID        string
	OrganizationID       int64
	Currency             string
	Amount               string
	PoolBefore           string
	PoolAfter            string
	BalanceVersionBefore int64
	BalanceVersionAfter  int64
	CanonicalPayloadHash string
	Ledger               OrganizationLedgerEntry
	Replayed             bool
}

type OrganizationBalanceSummary struct {
	OrganizationID int64
	Status         string
	Currency       string
	Unallocated    string
	Available      string
	Reserved       string
	Consumed       string
	TotalRemaining string
	BalanceVersion int64
}

type OrganizationLedgerEntry struct {
	PostingID            int64
	PostingSequence      int
	OperationID          string
	CorrelationID        string
	OrganizationID       int64
	OperationType        string
	AuthorizationScope   string
	ActorIDSnapshot      int64
	ActorDisplaySnapshot string
	ActorRoleSnapshot    string
	Currency             string
	IdempotencyKeyHash   string
	CanonicalPayloadHash string
	PublicReason         *string
	InternalReason       string
	BucketType           string
	BucketIDSnapshot     int64
	AllocationIDSnapshot *int64
	TargetUserIDSnapshot *int64
	BeforeAmount         string
	DeltaAmount          string
	AfterAmount          string
	BalanceVersionBefore int64
	BalanceVersionAfter  int64
	OccurredAt           time.Time
	CreatedAt            time.Time
}

type OrganizationFundingService struct {
	repo OrganizationFundingRepository
}

func NewOrganizationFundingService(repo OrganizationFundingRepository) *OrganizationFundingService {
	return &OrganizationFundingService{repo: repo}
}

// Credit adds positive platform credit to an active or disabled organization.
// It deliberately does not use the generic TTL idempotency coordinator: the
// committed operation and posting are the durable idempotency record.
func (s *OrganizationFundingService) Credit(
	ctx context.Context,
	actor OrganizationFundingActor,
	organizationID int64,
	amount string,
	reasons OrganizationFundingReasons,
	idempotencyKey string,
) (*OrganizationFundingResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationFundingRepositoryUnavailable
	}
	if err := validateOrganizationFundingActor(actor); err != nil {
		return nil, err
	}
	actor.DisplayName = strings.TrimSpace(actor.DisplayName)
	if organizationID <= 0 {
		return nil, ErrInvalidOrganizationID
	}
	canonicalAmount, err := NormalizeOrganizationMoney(amount, true)
	if err != nil {
		return nil, err
	}
	publicReason, internalReason, err := normalizeOrganizationFundingReasons(reasons)
	if err != nil {
		return nil, err
	}
	key, err := NormalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrIdempotencyKeyRequired
	}

	payload := organizationCreditPayload{
		OrganizationID: organizationID,
		Currency:       domain.OrganizationCurrencyUSD,
		OperationType:  domain.OrganizationFundingOperationCredit,
		Amount:         canonicalAmount,
		PublicReason:   publicReason,
		InternalReason: internalReason,
	}
	payloadHash, err := hashOrganizationFundingPayload(payload)
	if err != nil {
		return nil, ErrIdempotencyInvalidPayload.WithCause(err)
	}
	publicReasonPtr := optionalString(publicReason)
	command := OrganizationCreditCommand{
		OrganizationID:       organizationID,
		Amount:               canonicalAmount,
		Currency:             domain.OrganizationCurrencyUSD,
		OperationType:        domain.OrganizationFundingOperationCredit,
		AuthorizationScope:   domain.OrganizationFundingAuthorizationPlatformAdminJWT,
		IdempotencyKeyHash:   HashIdempotencyKey(key),
		CanonicalPayloadHash: payloadHash,
		Actor:                actor,
		PublicReason:         publicReasonPtr,
		InternalReason:       internalReason,
		OperationID:          uuid.NewString(),
		CorrelationID:        uuid.NewString(),
		OccurredAt:           time.Now().UTC(),
	}
	result, err := s.repo.Credit(ctx, command)
	if err != nil {
		return nil, err
	}
	if err := ValidateOrganizationFundingResult(result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *OrganizationFundingService) GetSummary(ctx context.Context, organizationID int64) (*OrganizationBalanceSummary, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationFundingRepositoryUnavailable
	}
	if organizationID <= 0 {
		return nil, ErrInvalidOrganizationID
	}
	return s.repo.GetSummary(ctx, organizationID)
}

func (s *OrganizationFundingService) ListLedger(ctx context.Context, organizationID int64, params pagination.PaginationParams) ([]OrganizationLedgerEntry, *pagination.PaginationResult, error) {
	if s == nil || s.repo == nil {
		return nil, nil, ErrOrganizationFundingRepositoryUnavailable
	}
	if organizationID <= 0 {
		return nil, nil, ErrInvalidOrganizationID
	}
	return s.repo.ListLedger(ctx, organizationID, params)
}

// NormalizeOrganizationMoney validates the wire representation and returns
// exactly eight fractional digits. It intentionally rejects exponent notation,
// signs, NaN/Inf, and values that cannot fit DECIMAL(20,8).
func NormalizeOrganizationMoney(raw string, requirePositive bool) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ErrOrganizationFundingAmountRequired
	}
	if !organizationMoneyPattern.MatchString(value) {
		return "", ErrOrganizationFundingAmountInvalid
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil {
		return "", ErrOrganizationFundingAmountInvalid.WithCause(err)
	}
	if parsed.IsNegative() {
		return "", ErrOrganizationFundingAmountInvalid
	}
	if parsed.GreaterThan(organizationMaxMoney) {
		return "", ErrOrganizationFundingAmountOverflow
	}
	if parsed.Truncate(domain.OrganizationMoneyScale).Cmp(parsed) != 0 {
		return "", ErrOrganizationFundingAmountInvalid
	}
	if requirePositive && parsed.IsZero() {
		return "", ErrOrganizationFundingAmountPositive
	}
	return parsed.StringFixed(domain.OrganizationMoneyScale), nil
}

func validateOrganizationFundingActor(actor OrganizationFundingActor) error {
	if actor.UserID <= 0 || !actor.Active || actor.PlatformRole != domain.RoleAdmin ||
		actor.AuthorizationScope != domain.OrganizationFundingAuthorizationPlatformAdminJWT {
		return ErrOrganizationFundingForbidden
	}
	display := strings.TrimSpace(actor.DisplayName)
	if display == "" || len(display) > 255 {
		return ErrOrganizationFundingForbidden
	}
	return nil
}

func normalizeOrganizationFundingReasons(reasons OrganizationFundingReasons) (string, string, error) {
	publicReason := strings.TrimSpace(reasons.PublicReason)
	internalReason := strings.TrimSpace(reasons.InternalReason)
	if internalReason == "" {
		return "", "", ErrOrganizationFundingReasonRequired
	}
	if len(publicReason) > 2000 || len(internalReason) > 10000 {
		return "", "", ErrOrganizationFundingReasonRequired
	}
	return publicReason, internalReason, nil
}

type organizationCreditPayload struct {
	OrganizationID int64  `json:"organization_id"`
	Currency       string `json:"currency"`
	OperationType  string `json:"operation_type"`
	Amount         string `json:"amount"`
	PublicReason   string `json:"public_reason,omitempty"`
	InternalReason string `json:"internal_reason"`
}

func hashOrganizationFundingPayload(payload organizationCreditPayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func validHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

// ValidateOrganizationFundingResult checks the repository boundary before a
// handler serializes it. It is intentionally decimal-only and catches a
// corrupt raw SQL row early.
func ValidateOrganizationFundingResult(result *OrganizationFundingResult) error {
	if result == nil {
		return ErrOrganizationFundingInvariant
	}
	values := []string{result.Amount, result.PoolBefore, result.PoolAfter, result.Ledger.BeforeAmount, result.Ledger.DeltaAmount, result.Ledger.AfterAmount}
	parsed := make([]decimal.Decimal, 0, len(values))
	for _, value := range values {
		canonical, err := NormalizeOrganizationMoney(value, false)
		if err != nil || canonical != value {
			if err == nil {
				err = fmt.Errorf("non-canonical organization amount %q", value)
			}
			return ErrOrganizationFundingInvariant.WithCause(err)
		}
		amount, err := decimal.NewFromString(value)
		if err != nil {
			return ErrOrganizationFundingInvariant.WithCause(err)
		}
		parsed = append(parsed, amount)
	}
	if result.Currency != domain.OrganizationCurrencyUSD || result.Ledger.Currency != domain.OrganizationCurrencyUSD ||
		result.Ledger.AuthorizationScope != domain.OrganizationFundingAuthorizationPlatformAdminJWT ||
		result.Ledger.ActorIDSnapshot <= 0 || strings.TrimSpace(result.Ledger.ActorDisplaySnapshot) == "" ||
		result.Ledger.ActorRoleSnapshot != domain.RoleAdmin || strings.TrimSpace(result.Ledger.InternalReason) == "" ||
		result.Ledger.OccurredAt.IsZero() || result.Ledger.CreatedAt.IsZero() ||
		!validHash(result.Ledger.IdempotencyKeyHash) {
		return ErrOrganizationFundingInvariant
	}
	if _, err := uuid.Parse(result.OperationID); err != nil {
		return ErrOrganizationFundingInvariant.WithCause(err)
	}
	if _, err := uuid.Parse(result.CorrelationID); err != nil {
		return ErrOrganizationFundingInvariant.WithCause(err)
	}
	if result.OrganizationID <= 0 || result.Ledger.OrganizationID != result.OrganizationID ||
		result.Ledger.OperationID != result.OperationID || result.Ledger.CorrelationID != result.CorrelationID ||
		result.Ledger.OperationType != domain.OrganizationFundingOperationCredit ||
		result.Ledger.BucketType != domain.OrganizationLedgerBucketUnallocated ||
		result.Ledger.BucketIDSnapshot != result.OrganizationID || result.Ledger.PostingSequence != 1 ||
		result.BalanceVersionAfter != result.BalanceVersionBefore+1 ||
		result.Ledger.BalanceVersionBefore != result.BalanceVersionBefore ||
		result.Ledger.BalanceVersionAfter != result.BalanceVersionAfter ||
		result.Amount != result.Ledger.DeltaAmount || result.PoolBefore != result.Ledger.BeforeAmount ||
		result.PoolAfter != result.Ledger.AfterAmount {
		return ErrOrganizationFundingInvariant
	}
	if !parsed[3].Add(parsed[4]).Equal(parsed[5]) || parsed[4].IsZero() || parsed[4].IsNegative() {
		return ErrOrganizationFundingInvariant
	}
	if !validHash(result.CanonicalPayloadHash) || result.CanonicalPayloadHash != result.Ledger.CanonicalPayloadHash {
		return ErrOrganizationFundingInvariant
	}
	return nil
}
