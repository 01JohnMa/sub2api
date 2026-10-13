package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type organizationFundingRepositoryStub struct {
	creditCalls  int
	creditInput  OrganizationCreditCommand
	creditResult *OrganizationFundingResult
	creditErr    error
}

func (r *organizationFundingRepositoryStub) Credit(_ context.Context, input OrganizationCreditCommand) (*OrganizationFundingResult, error) {
	r.creditCalls++
	r.creditInput = input
	return r.creditResult, r.creditErr
}

func (*organizationFundingRepositoryStub) GetSummary(context.Context, int64) (*OrganizationBalanceSummary, error) {
	return nil, nil
}

func (*organizationFundingRepositoryStub) ListLedger(context.Context, int64, pagination.PaginationParams) ([]OrganizationLedgerEntry, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (*organizationFundingRepositoryStub) GetReport(context.Context, int64, OrganizationReportRange) (*OrganizationReport, error) {
	return nil, nil
}

func (*organizationFundingRepositoryStub) ListReportMembers(context.Context, int64, OrganizationReportRange, pagination.PaginationParams) ([]OrganizationReportMemberUsage, *OrganizationReportMemberUsage, *pagination.PaginationResult, error) {
	return nil, nil, nil, nil
}

func validOrganizationFundingResult() *OrganizationFundingResult {
	now := time.Date(2026, time.July, 28, 8, 0, 0, 0, time.UTC)
	operationID := uuid.NewString()
	correlationID := uuid.NewString()
	canonicalPayloadHash := strings.Repeat("a", 64)
	idempotencyKeyHash := strings.Repeat("b", 64)
	return &OrganizationFundingResult{
		OperationID:          operationID,
		CorrelationID:        correlationID,
		OrganizationID:       7,
		Currency:             domain.OrganizationCurrencyUSD,
		Amount:               "1.25000000",
		PoolBefore:           "10.00000000",
		PoolAfter:            "11.25000000",
		BalanceVersionBefore: 4,
		BalanceVersionAfter:  5,
		CanonicalPayloadHash: canonicalPayloadHash,
		Ledger: OrganizationLedgerEntry{
			PostingID:            101,
			PostingSequence:      1,
			OperationID:          operationID,
			CorrelationID:        correlationID,
			OrganizationID:       7,
			OperationType:        domain.OrganizationFundingOperationCredit,
			AuthorizationScope:   domain.OrganizationFundingAuthorizationPlatformAdminJWT,
			ActorIDSnapshot:      42,
			ActorDisplaySnapshot: "platform-admin",
			ActorRoleSnapshot:    domain.RoleAdmin,
			Currency:             domain.OrganizationCurrencyUSD,
			IdempotencyKeyHash:   idempotencyKeyHash,
			CanonicalPayloadHash: canonicalPayloadHash,
			InternalReason:       "approved by finance",
			BucketType:           domain.OrganizationLedgerBucketUnallocated,
			BucketIDSnapshot:     7,
			BeforeAmount:         "10.00000000",
			DeltaAmount:          "1.25000000",
			AfterAmount:          "11.25000000",
			BalanceVersionBefore: 4,
			BalanceVersionAfter:  5,
			OccurredAt:           now,
			CreatedAt:            now,
		},
	}
}

func platformFundingActor() OrganizationFundingActor {
	return OrganizationFundingActor{
		UserID:             42,
		DisplayName:        " platform-admin ",
		PlatformRole:       domain.RoleAdmin,
		AuthorizationScope: domain.OrganizationFundingAuthorizationPlatformAdminJWT,
		Active:             true,
	}
}

func TestOrganizationFundingServiceCreditCanonicalizesInputAndBuildsDurableCommand(t *testing.T) {
	result := validOrganizationFundingResult()
	repo := &organizationFundingRepositoryStub{creditResult: result}
	service := NewOrganizationFundingService(repo)

	got, err := service.Credit(
		context.Background(),
		platformFundingActor(),
		7,
		".1",
		OrganizationFundingReasons{PublicReason: " public reason ", InternalReason: " approved by finance "},
		" credit-2026-07 ",
	)
	require.NoError(t, err)
	require.Same(t, result, got)
	require.Equal(t, 1, repo.creditCalls)
	require.Equal(t, "0.10000000", repo.creditInput.Amount)
	require.Equal(t, domain.OrganizationCurrencyUSD, repo.creditInput.Currency)
	require.Equal(t, domain.OrganizationFundingOperationCredit, repo.creditInput.OperationType)
	require.Equal(t, domain.OrganizationFundingAuthorizationPlatformAdminJWT, repo.creditInput.AuthorizationScope)
	require.Equal(t, "platform-admin", repo.creditInput.Actor.DisplayName)
	require.Equal(t, HashIdempotencyKey("credit-2026-07"), repo.creditInput.IdempotencyKeyHash)
	require.NotNil(t, repo.creditInput.PublicReason)
	require.Equal(t, "public reason", *repo.creditInput.PublicReason)
	require.Equal(t, "approved by finance", repo.creditInput.InternalReason)
	require.NotEmpty(t, repo.creditInput.CanonicalPayloadHash)
	_, err = uuid.Parse(repo.creditInput.OperationID)
	require.NoError(t, err)
	_, err = uuid.Parse(repo.creditInput.CorrelationID)
	require.NoError(t, err)
}

func TestOrganizationFundingServiceCreditRejectsInvalidInputBeforeRepository(t *testing.T) {
	tests := []struct {
		name   string
		actor  OrganizationFundingActor
		amount string
		reason OrganizationFundingReasons
		key    string
		want   error
	}{
		{name: "non platform actor", actor: OrganizationFundingActor{}, amount: "1", reason: OrganizationFundingReasons{InternalReason: "reason"}, key: "key", want: ErrOrganizationFundingForbidden},
		{name: "over scale", actor: platformFundingActor(), amount: "0.000000001", reason: OrganizationFundingReasons{InternalReason: "reason"}, key: "key", want: ErrOrganizationFundingAmountInvalid},
		{name: "overflow", actor: platformFundingActor(), amount: "1000000000000", reason: OrganizationFundingReasons{InternalReason: "reason"}, key: "key", want: ErrOrganizationFundingAmountOverflow},
		{name: "zero", actor: platformFundingActor(), amount: "0", reason: OrganizationFundingReasons{InternalReason: "reason"}, key: "key", want: ErrOrganizationFundingAmountPositive},
		{name: "missing reason", actor: platformFundingActor(), amount: "1", reason: OrganizationFundingReasons{}, key: "key", want: ErrOrganizationFundingReasonRequired},
		{name: "missing key", actor: platformFundingActor(), amount: "1", reason: OrganizationFundingReasons{InternalReason: "reason"}, key: "  ", want: ErrIdempotencyKeyRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &organizationFundingRepositoryStub{creditResult: validOrganizationFundingResult()}
			_, err := NewOrganizationFundingService(repo).Credit(context.Background(), tt.actor, 7, tt.amount, tt.reason, tt.key)
			require.ErrorIs(t, err, tt.want)
			require.Zero(t, repo.creditCalls)
		})
	}
}

func TestOrganizationFundingServiceCreditRejectsCorruptRepositoryResult(t *testing.T) {
	result := validOrganizationFundingResult()
	result.Ledger.IdempotencyKeyHash = "not-a-sha256"
	repo := &organizationFundingRepositoryStub{creditResult: result}

	_, err := NewOrganizationFundingService(repo).Credit(
		context.Background(),
		platformFundingActor(),
		7,
		"1.25",
		OrganizationFundingReasons{InternalReason: "approved by finance"},
		"credit-result-invariant",
	)
	require.ErrorIs(t, err, ErrOrganizationFundingInvariant)
}

func TestNormalizeOrganizationMoney(t *testing.T) {
	tests := []struct {
		input           string
		requirePositive bool
		want            string
		wantErr         error
	}{
		{input: "0.10000000", requirePositive: true, want: "0.10000000"},
		{input: ".1", requirePositive: true, want: "0.10000000"},
		{input: "999999999999.99999999", requirePositive: true, want: "999999999999.99999999"},
		{input: "0", requirePositive: true, wantErr: ErrOrganizationFundingAmountPositive},
		{input: "-1", requirePositive: true, wantErr: ErrOrganizationFundingAmountInvalid},
		{input: "1e2", requirePositive: true, wantErr: ErrOrganizationFundingAmountInvalid},
		{input: "NaN", requirePositive: true, wantErr: ErrOrganizationFundingAmountInvalid},
		{input: "0.000000001", requirePositive: true, wantErr: ErrOrganizationFundingAmountInvalid},
		{input: "1000000000000", requirePositive: true, wantErr: ErrOrganizationFundingAmountOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := NormalizeOrganizationMoney(tt.input, tt.requirePositive)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestValidateOrganizationFundingResultRejectsBrokenLedgerInvariants(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*OrganizationFundingResult)
	}{
		{name: "non uuid operation", mutate: func(result *OrganizationFundingResult) {
			result.OperationID = "not-a-uuid"
			result.Ledger.OperationID = result.OperationID
		}},
		{name: "invalid authorization scope", mutate: func(result *OrganizationFundingResult) { result.Ledger.AuthorizationScope = "organization_admin_jwt" }},
		{name: "zero actor", mutate: func(result *OrganizationFundingResult) { result.Ledger.ActorIDSnapshot = 0 }},
		{name: "unexpected actor role", mutate: func(result *OrganizationFundingResult) { result.Ledger.ActorRoleSnapshot = domain.RoleUser }},
		{name: "empty internal reason", mutate: func(result *OrganizationFundingResult) { result.Ledger.InternalReason = " " }},
		{name: "bad amount equation", mutate: func(result *OrganizationFundingResult) {
			result.Ledger.AfterAmount = "11.26000000"
			result.PoolAfter = result.Ledger.AfterAmount
		}},
		{name: "unrelated bucket", mutate: func(result *OrganizationFundingResult) {
			result.Ledger.BucketType = domain.OrganizationLedgerBucketAllocationAvailable
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validOrganizationFundingResult()
			tt.mutate(result)
			require.ErrorIs(t, ValidateOrganizationFundingResult(result), ErrOrganizationFundingInvariant)
		})
	}
}
