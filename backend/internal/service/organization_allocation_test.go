package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestBuildOrganizationAllocationPlanIncludesExcludedMembers(t *testing.T) {
	actor := OrganizationAllocationActor{
		UserID:                10,
		DisplayName:           "organization-admin",
		OrganizationRole:      domain.OrganizationRoleAdmin,
		AuthorizationScope:    domain.OrganizationFundingAuthorizationOrganizationJWT,
		CurrentOrganizationID: 7,
		Active:                true,
	}
	snapshot := &OrganizationAllocationSnapshot{
		OrganizationID: 7,
		Status:         domain.OrganizationStatusActive,
		OrganizationAt: time.Date(2026, time.July, 28, 8, 0, 0, 0, time.UTC),
		BalanceVersion: 3,
		Unallocated:    "30.00000000",
		Members: []OrganizationAllocationMemberSnapshot{
			{UserID: 40, UserStatus: domain.StatusDisabled, OrganizationRole: domain.OrganizationRoleMember, AllocationID: 140, AllocationStatus: domain.OrganizationAllocationStatusActive, Available: "0.00000000", Reserved: "0.00000000"},
			{UserID: 20, UserStatus: domain.StatusActive, OrganizationRole: domain.OrganizationRoleAdmin, AllocationID: 120, AllocationStatus: domain.OrganizationAllocationStatusActive, Available: "0.00000000", Reserved: "0.00000000"},
			{UserID: 30, UserStatus: domain.StatusActive, OrganizationRole: domain.OrganizationRoleMember, AllocationID: 130, AllocationStatus: domain.OrganizationAllocationStatusActive, Available: "5.00000000", Reserved: "0.00000000"},
			{UserID: 10, UserStatus: domain.StatusActive, OrganizationRole: domain.OrganizationRoleAdmin, AllocationID: 110, AllocationStatus: domain.OrganizationAllocationStatusActive, Available: "0.00000000", Reserved: "0.00000000"},
		},
	}

	tests := []struct {
		name          string
		operationType string
		input         OrganizationAllocationPreviewInput
	}{
		{name: "equal", operationType: domain.OrganizationFundingOperationEqualAllocation},
		{
			name:          "custom",
			operationType: domain.OrganizationFundingOperationCustomAllocation,
			input: OrganizationAllocationPreviewInput{Targets: []OrganizationAllocationTargetInput{
				{UserID: 30, Amount: "1"},
			}},
		},
	}
	wantEligible := []OrganizationAllocationMemberPreview{
		{UserID: 30, UserStatus: domain.StatusActive, OrganizationRole: domain.OrganizationRoleMember, AllocationID: 130, AllocationStatus: domain.OrganizationAllocationStatusActive, Available: "5.00000000", Reserved: "0.00000000"},
	}
	wantExcluded := []OrganizationAllocationMemberPreview{
		{UserID: 10, UserStatus: domain.StatusActive, OrganizationRole: domain.OrganizationRoleAdmin, AllocationID: 110, AllocationStatus: domain.OrganizationAllocationStatusActive, Available: "0.00000000", Reserved: "0.00000000", ExcludedReason: OrganizationAllocationExclusionSelf},
		{UserID: 20, UserStatus: domain.StatusActive, OrganizationRole: domain.OrganizationRoleAdmin, AllocationID: 120, AllocationStatus: domain.OrganizationAllocationStatusActive, Available: "0.00000000", Reserved: "0.00000000", ExcludedReason: OrganizationAllocationExclusionAdmin},
		{UserID: 40, UserStatus: domain.StatusDisabled, OrganizationRole: domain.OrganizationRoleMember, AllocationID: 140, AllocationStatus: domain.OrganizationAllocationStatusActive, Available: "0.00000000", Reserved: "0.00000000", ExcludedReason: OrganizationAllocationExclusionInactive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := BuildOrganizationAllocationPlan(snapshot, actor, 7, tt.operationType, tt.input)
			require.NoError(t, err)
			require.Equal(t, wantEligible, plan.EligibleMembers)
			require.Equal(t, wantExcluded, plan.ExcludedMembers)
		})
	}
}

func TestBuildOrganizationAllocationPlanEqualAllocationRoundsDownAndAppends(t *testing.T) {
	actor := organizationAllocationTestActor()
	snapshot := organizationAllocationTestSnapshot("100.00000000",
		organizationAllocationTestMember(20, 120, "1.00000000"),
		organizationAllocationTestMember(30, 130, "2.00000000"),
		organizationAllocationTestMember(40, 140, "3.00000000"),
	)

	plan, err := BuildOrganizationAllocationPlan(
		snapshot,
		actor,
		7,
		domain.OrganizationFundingOperationEqualAllocation,
		OrganizationAllocationPreviewInput{},
	)
	require.NoError(t, err)
	require.Equal(t, "100.00000000", plan.Confirmation.PoolBefore)
	require.Equal(t, "0.00000001", plan.Confirmation.PoolAfter)
	require.Equal(t, "99.99999999", plan.Confirmation.TotalDelta)
	require.Equal(t, "0.00000001", plan.Confirmation.RoundingRemainder)
	require.Empty(t, plan.Confirmation.NoOpReason)
	require.Equal(t, []OrganizationAllocationTargetPreview{
		{UserID: 20, AllocationID: 120, Before: "1.00000000", Delta: "33.33333333", After: "34.33333333"},
		{UserID: 30, AllocationID: 130, Before: "2.00000000", Delta: "33.33333333", After: "35.33333333"},
		{UserID: 40, AllocationID: 140, Before: "3.00000000", Delta: "33.33333333", After: "36.33333333"},
	}, plan.Confirmation.Targets)
}

func TestBuildOrganizationAllocationPlanEqualAllocationNoOpStates(t *testing.T) {
	actor := organizationAllocationTestActor()
	tests := []struct {
		name       string
		pool       string
		members    []OrganizationAllocationMemberSnapshot
		wantReason string
	}{
		{
			name: "no eligible members", pool: "10.00000000",
			members: []OrganizationAllocationMemberSnapshot{
				organizationAllocationTestAdminMember(actor.UserID, 110),
			},
			wantReason: OrganizationAllocationNoOpNoEligible,
		},
		{
			name: "zero pool", pool: "0.00000000",
			members:    []OrganizationAllocationMemberSnapshot{organizationAllocationTestMember(20, 120, "1.00000000")},
			wantReason: OrganizationAllocationNoOpZeroPool,
		},
		{
			name: "below precision", pool: "0.00000001",
			members: []OrganizationAllocationMemberSnapshot{
				organizationAllocationTestMember(20, 120, "1.00000000"),
				organizationAllocationTestMember(30, 130, "2.00000000"),
			},
			wantReason: OrganizationAllocationNoOpBelowPrecision,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := BuildOrganizationAllocationPlan(
				organizationAllocationTestSnapshot(tt.pool, tt.members...),
				actor,
				7,
				domain.OrganizationFundingOperationEqualAllocation,
				OrganizationAllocationPreviewInput{},
			)
			require.NoError(t, err)
			require.Equal(t, tt.wantReason, plan.Confirmation.NoOpReason)
			require.Equal(t, tt.pool, plan.Confirmation.PoolBefore)
			require.Equal(t, tt.pool, plan.Confirmation.PoolAfter)
			require.Equal(t, "0.00000000", plan.Confirmation.TotalDelta)
		})
	}
}

func TestBuildOrganizationAllocationPlanValidatesExplicitAmountsAtomically(t *testing.T) {
	actor := organizationAllocationTestActor()
	snapshot := organizationAllocationTestSnapshot("10.00000000",
		organizationAllocationTestMember(20, 120, "4.00000000"),
		organizationAllocationTestMember(30, 130, "2.00000000"),
	)
	tests := []struct {
		name          string
		operationType string
		targets       []OrganizationAllocationTargetInput
		wantErr       error
	}{
		{name: "duplicate", operationType: domain.OrganizationFundingOperationCustomAllocation, targets: []OrganizationAllocationTargetInput{{UserID: 20, Amount: "1"}, {UserID: 20, Amount: "2"}}, wantErr: ErrOrganizationAllocationDuplicateTarget},
		{name: "zero", operationType: domain.OrganizationFundingOperationCustomAllocation, targets: []OrganizationAllocationTargetInput{{UserID: 20, Amount: "0"}}, wantErr: ErrOrganizationFundingAmountPositive},
		{name: "negative", operationType: domain.OrganizationFundingOperationCustomAllocation, targets: []OrganizationAllocationTargetInput{{UserID: 20, Amount: "-1"}}, wantErr: ErrOrganizationFundingAmountInvalid},
		{name: "over scale", operationType: domain.OrganizationFundingOperationCustomAllocation, targets: []OrganizationAllocationTargetInput{{UserID: 20, Amount: "0.000000001"}}, wantErr: ErrOrganizationFundingAmountInvalid},
		{name: "overflow", operationType: domain.OrganizationFundingOperationCustomAllocation, targets: []OrganizationAllocationTargetInput{{UserID: 20, Amount: "1000000000000"}}, wantErr: ErrOrganizationFundingAmountOverflow},
		{name: "over pool", operationType: domain.OrganizationFundingOperationCustomAllocation, targets: []OrganizationAllocationTargetInput{{UserID: 20, Amount: "6"}, {UserID: 30, Amount: "5"}}, wantErr: ErrOrganizationAllocationInsufficientPool},
		{name: "over available", operationType: domain.OrganizationFundingOperationReclaim, targets: []OrganizationAllocationTargetInput{{UserID: 20, Amount: "4.00000001"}}, wantErr: ErrOrganizationAllocationInsufficientAvailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildOrganizationAllocationPlan(
				snapshot,
				actor,
				7,
				tt.operationType,
				OrganizationAllocationPreviewInput{Targets: tt.targets},
			)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestBuildOrganizationAllocationPlanCanonicalizesCustomAndReclaimAmounts(t *testing.T) {
	actor := organizationAllocationTestActor()
	snapshot := organizationAllocationTestSnapshot("10.00000000",
		organizationAllocationTestMember(20, 120, "4.00000000"),
	)

	custom, err := BuildOrganizationAllocationPlan(
		snapshot,
		actor,
		7,
		domain.OrganizationFundingOperationCustomAllocation,
		OrganizationAllocationPreviewInput{Targets: []OrganizationAllocationTargetInput{{UserID: 20, Amount: ".1"}}},
	)
	require.NoError(t, err)
	require.Equal(t, "0.10000000", custom.Confirmation.Targets[0].Delta)
	require.Equal(t, "4.10000000", custom.Confirmation.Targets[0].After)
	require.Equal(t, "9.90000000", custom.Confirmation.PoolAfter)

	reclaim, err := BuildOrganizationAllocationPlan(
		snapshot,
		actor,
		7,
		domain.OrganizationFundingOperationReclaim,
		OrganizationAllocationPreviewInput{Targets: []OrganizationAllocationTargetInput{{UserID: 20, Amount: ".1"}}},
	)
	require.NoError(t, err)
	require.Equal(t, "-0.10000000", reclaim.Confirmation.Targets[0].Delta)
	require.Equal(t, "3.90000000", reclaim.Confirmation.Targets[0].After)
	require.Equal(t, "10.10000000", reclaim.Confirmation.PoolAfter)
}

func TestValidateOrganizationAllocationConfirmationMapsSnapshotChangesToStale(t *testing.T) {
	actor := organizationAllocationTestActor()
	customSnapshot := organizationAllocationTestSnapshot("10.00000000", organizationAllocationTestMember(20, 120, "4.00000000"))
	customPlan, err := BuildOrganizationAllocationPlan(customSnapshot, actor, 7, domain.OrganizationFundingOperationCustomAllocation,
		OrganizationAllocationPreviewInput{Targets: []OrganizationAllocationTargetInput{{UserID: 20, Amount: "8"}}})
	require.NoError(t, err)
	reclaimPlan, err := BuildOrganizationAllocationPlan(customSnapshot, actor, 7, domain.OrganizationFundingOperationReclaim,
		OrganizationAllocationPreviewInput{Targets: []OrganizationAllocationTargetInput{{UserID: 20, Amount: "4"}}})
	require.NoError(t, err)

	tests := []struct {
		name         string
		snapshot     *OrganizationAllocationSnapshot
		confirmation OrganizationAllocationConfirmation
	}{
		{
			name: "pool decreased",
			snapshot: func() *OrganizationAllocationSnapshot {
				changed := cloneOrganizationAllocationTestSnapshot(customSnapshot)
				changed.Unallocated = "5.00000000"
				return changed
			}(),
			confirmation: customPlan.Confirmation,
		},
		{
			name: "member role changed",
			snapshot: func() *OrganizationAllocationSnapshot {
				changed := cloneOrganizationAllocationTestSnapshot(customSnapshot)
				changed.Members[0].OrganizationRole = domain.OrganizationRoleAdmin
				return changed
			}(),
			confirmation: customPlan.Confirmation,
		},
		{
			name:         "member departed",
			snapshot:     organizationAllocationTestSnapshot("10.00000000"),
			confirmation: customPlan.Confirmation,
		},
		{
			name: "available decreased",
			snapshot: func() *OrganizationAllocationSnapshot {
				changed := cloneOrganizationAllocationTestSnapshot(customSnapshot)
				changed.Members[0].Available = "3.00000000"
				return changed
			}(),
			confirmation: reclaimPlan.Confirmation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, ValidateOrganizationAllocationConfirmation(tt.snapshot, actor, tt.confirmation), ErrOrganizationAllocationStalePreview)
		})
	}
}

func TestOrganizationAllocationServiceReplaysCommittedNoOpAfterPreviewExpiry(t *testing.T) {
	repo := &organizationAllocationServiceRepositoryStub{
		snapshot: organizationAllocationTestSnapshot("0.00000000", organizationAllocationTestMember(20, 120, "1.00000000")),
	}
	manager, err := NewOrganizationAllocationService(repo, strings.Repeat("preview-secret", 3))
	require.NoError(t, err)
	now := time.Date(2026, time.July, 28, 8, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	actor := organizationAllocationTestActor()
	preview, err := manager.Preview(context.Background(), actor, 7, domain.OrganizationFundingOperationEqualAllocation, OrganizationAllocationPreviewInput{})
	require.NoError(t, err)
	input := OrganizationAllocationExecuteInput{PreviewToken: preview.PreviewToken, Confirmation: preview.Confirmation}

	first, err := manager.Execute(context.Background(), actor, 7, domain.OrganizationFundingOperationEqualAllocation, input, "no-op-expiry")
	require.NoError(t, err)
	require.False(t, first.Replayed)
	require.Equal(t, OrganizationAllocationOutcomeNoOp, first.Outcome)
	require.Equal(t, 1, repo.executeCalls)

	now = preview.ExpiresAt.Add(time.Second)
	require.ErrorIs(t, func() error {
		_, verifyErr := manager.verifyPreviewToken(preview.PreviewToken)
		return verifyErr
	}(), ErrOrganizationAllocationPreviewInvalid)
	repo.snapshot.Unallocated = "5.00000000"
	repo.snapshot.BalanceVersion++

	replayed, err := manager.Execute(context.Background(), actor, 7, domain.OrganizationFundingOperationEqualAllocation, input, "no-op-expiry")
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, first.OperationID, replayed.OperationID)
	require.Equal(t, first.CorrelationID, replayed.CorrelationID)
	require.Equal(t, first.PoolBefore, replayed.PoolBefore)
	require.Equal(t, 1, repo.executeCalls)
}

func TestOrganizationAllocationServiceRejectsExpiredUncommittedPreview(t *testing.T) {
	repo := &organizationAllocationServiceRepositoryStub{
		snapshot: organizationAllocationTestSnapshot("0.00000000", organizationAllocationTestMember(20, 120, "1.00000000")),
	}
	manager, err := NewOrganizationAllocationService(repo, strings.Repeat("preview-secret", 3))
	require.NoError(t, err)
	now := time.Date(2026, time.July, 28, 8, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	actor := organizationAllocationTestActor()
	preview, err := manager.Preview(context.Background(), actor, 7, domain.OrganizationFundingOperationEqualAllocation, OrganizationAllocationPreviewInput{})
	require.NoError(t, err)
	now = preview.ExpiresAt.Add(time.Second)

	_, err = manager.Execute(context.Background(), actor, 7, domain.OrganizationFundingOperationEqualAllocation,
		OrganizationAllocationExecuteInput{PreviewToken: preview.PreviewToken, Confirmation: preview.Confirmation}, "expired-uncommitted")
	require.ErrorIs(t, err, ErrOrganizationAllocationPreviewInvalid)
	require.Zero(t, repo.executeCalls)
}

func TestValidateOrganizationAllocationResultRejectsMalformedNoOpSnapshot(t *testing.T) {
	valid := func() *OrganizationAllocationResult {
		return &OrganizationAllocationResult{
			OperationID:          "00000000-0000-4000-8000-000000000001",
			CorrelationID:        "00000000-0000-4000-8000-000000000002",
			OrganizationID:       7,
			OperationType:        domain.OrganizationFundingOperationEqualAllocation,
			Currency:             domain.OrganizationCurrencyUSD,
			Outcome:              OrganizationAllocationOutcomeNoOp,
			NoOpReason:           OrganizationAllocationNoOpZeroPool,
			PoolBefore:           "0.00000000",
			PoolAfter:            "0.00000000",
			TotalDelta:           "0.00000000",
			BalanceVersionBefore: 3,
			BalanceVersionAfter:  3,
			CanonicalPayloadHash: strings.Repeat("a", 64),
			Postings:             []OrganizationLedgerEntry{},
		}
	}
	tests := []struct {
		name   string
		mutate func(*OrganizationAllocationResult)
	}{
		{name: "missing operation id", mutate: func(result *OrganizationAllocationResult) { result.OperationID = "" }},
		{name: "invalid reason", mutate: func(result *OrganizationAllocationResult) { result.NoOpReason = "other" }},
		{name: "posting present", mutate: func(result *OrganizationAllocationResult) {
			result.Postings = []OrganizationLedgerEntry{{PostingID: 1}}
		}},
		{name: "nonzero total", mutate: func(result *OrganizationAllocationResult) { result.TotalDelta = "1.00000000" }},
		{name: "pool changed", mutate: func(result *OrganizationAllocationResult) { result.PoolAfter = "1.00000000" }},
		{name: "version changed", mutate: func(result *OrganizationAllocationResult) { result.BalanceVersionAfter++ }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := valid()
			tt.mutate(result)
			require.ErrorIs(t, ValidateOrganizationAllocationResult(result), ErrOrganizationAllocationInvariant)
		})
	}
}

type organizationAllocationServiceRepositoryStub struct {
	snapshot     *OrganizationAllocationSnapshot
	committed    *OrganizationAllocationResult
	executeCalls int
}

func (r *organizationAllocationServiceRepositoryStub) LoadPreviewSnapshot(context.Context, OrganizationAllocationActor, int64, string) (*OrganizationAllocationSnapshot, error) {
	return cloneOrganizationAllocationTestSnapshot(r.snapshot), nil
}

func (r *organizationAllocationServiceRepositoryStub) FindCommitted(_ context.Context, query OrganizationAllocationCommittedQuery) (*OrganizationAllocationResult, error) {
	if r.committed == nil || r.committed.CanonicalPayloadHash != query.CanonicalPayloadHash {
		return nil, nil
	}
	copyResult := *r.committed
	copyResult.Postings = append([]OrganizationLedgerEntry(nil), r.committed.Postings...)
	return &copyResult, nil
}

func (r *organizationAllocationServiceRepositoryStub) Execute(_ context.Context, command OrganizationAllocationExecuteCommand) (*OrganizationAllocationResult, error) {
	r.executeCalls++
	result := &OrganizationAllocationResult{
		OperationID:          command.OperationID,
		CorrelationID:        command.CorrelationID,
		OrganizationID:       command.Confirmation.OrganizationID,
		OperationType:        command.Confirmation.OperationType,
		Currency:             command.Confirmation.Currency,
		Outcome:              OrganizationAllocationOutcomeNoOp,
		NoOpReason:           command.Confirmation.NoOpReason,
		PoolBefore:           command.Confirmation.PoolBefore,
		PoolAfter:            command.Confirmation.PoolAfter,
		TotalDelta:           command.Confirmation.TotalDelta,
		BalanceVersionBefore: command.Confirmation.BalanceVersion,
		BalanceVersionAfter:  command.Confirmation.BalanceVersion,
		CanonicalPayloadHash: command.CanonicalPayloadHash,
		Postings:             []OrganizationLedgerEntry{},
	}
	r.committed = result
	return result, nil
}

func organizationAllocationTestActor() OrganizationAllocationActor {
	return OrganizationAllocationActor{
		UserID:                10,
		DisplayName:           "organization-admin",
		OrganizationRole:      domain.OrganizationRoleAdmin,
		AuthorizationScope:    domain.OrganizationFundingAuthorizationOrganizationJWT,
		CurrentOrganizationID: 7,
		Active:                true,
	}
}

func organizationAllocationTestSnapshot(pool string, members ...OrganizationAllocationMemberSnapshot) *OrganizationAllocationSnapshot {
	return &OrganizationAllocationSnapshot{
		OrganizationID: 7,
		Status:         domain.OrganizationStatusActive,
		OrganizationAt: time.Date(2026, time.July, 28, 8, 0, 0, 0, time.UTC),
		BalanceVersion: 3,
		Unallocated:    pool,
		Members:        members,
	}
}

func cloneOrganizationAllocationTestSnapshot(snapshot *OrganizationAllocationSnapshot) *OrganizationAllocationSnapshot {
	if snapshot == nil {
		return nil
	}
	clone := *snapshot
	clone.Members = append([]OrganizationAllocationMemberSnapshot(nil), snapshot.Members...)
	return &clone
}

func organizationAllocationTestMember(userID, allocationID int64, available string) OrganizationAllocationMemberSnapshot {
	return OrganizationAllocationMemberSnapshot{
		UserID:           userID,
		UserStatus:       domain.StatusActive,
		OrganizationRole: domain.OrganizationRoleMember,
		AllocationID:     allocationID,
		AllocationStatus: domain.OrganizationAllocationStatusActive,
		Available:        available,
		Reserved:         "0.00000000",
	}
}

func organizationAllocationTestAdminMember(userID, allocationID int64) OrganizationAllocationMemberSnapshot {
	member := organizationAllocationTestMember(userID, allocationID, "0.00000000")
	member.OrganizationRole = domain.OrganizationRoleAdmin
	return member
}
