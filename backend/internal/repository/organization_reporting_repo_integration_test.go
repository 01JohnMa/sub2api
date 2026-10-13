//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/organizationallocation"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestOrganizationReportPostgresUsesFinalEvidenceScopeTimeAndCurrentAllocation(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	suffix := fmt.Sprintf("report-%d", time.Now().UnixNano())
	fixtureA := createOrganizationAllocationIntegrationFixture(t, ctx, client, suffix+"-a", "0", "10")
	fixtureB := createOrganizationAllocationIntegrationFixture(t, ctx, client, suffix+"-b", "0", "10")
	keyA := createOrganizationReportKey(t, ctx, client, fixtureA.member.ID, fixtureA.organization.ID, suffix+"-a")
	keyB := createOrganizationReportKey(t, ctx, client, fixtureB.member.ID, fixtureB.organization.ID, suffix+"-b")
	account := mustCreateAccount(t, client, &service.Account{Name: "organization reporting account " + suffix})
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	// Old allocation contributes to the report period before the user is reallocated.
	terminalizeOrganizationReportExecution(t, ctx, client, fixtureA.organization.ID, fixtureA.member.ID, keyA.ID, account.ID, start, service.OrganizationEndpointFamilyChat, service.OrganizationExecutionTerminalOutcomeSuccess, false, "0.20000000", 4, 2)
	terminalizeOrganizationReportExecution(t, ctx, client, fixtureA.organization.ID, fixtureA.member.ID, keyA.ID, account.ID, start.Add(24*time.Hour), service.OrganizationEndpointFamilyChat, service.OrganizationExecutionTerminalOutcomeError, true, "0.30000000", 4, 2)
	terminalizeOrganizationReportExecution(t, ctx, client, fixtureA.organization.ID, fixtureA.member.ID, keyA.ID, account.ID, start.Add(48*time.Hour), service.OrganizationEndpointFamilyTokenCounting, service.OrganizationExecutionTerminalOutcomeError, false, "0", 8, 0)
	terminalizeOrganizationReportExecution(t, ctx, client, fixtureA.organization.ID, fixtureA.member.ID, keyA.ID, account.ID, start.Add(60*time.Hour), service.OrganizationEndpointFamilyChat, service.OrganizationExecutionTerminalOutcomeError, false, "0.05000000", 4, 2)
	// The end boundary is excluded even though it has a positive settled posting.
	terminalizeOrganizationReportExecution(t, ctx, client, fixtureA.organization.ID, fixtureA.member.ID, keyA.ID, account.ID, end, service.OrganizationEndpointFamilyChat, service.OrganizationExecutionTerminalOutcomeSuccess, false, "0.40000000", 4, 2)

	oldAllocation, err := client.OrganizationAllocation.Query().Where(
		organizationallocation.OrganizationIDEQ(fixtureA.organization.ID),
		organizationallocation.UserIDSnapshotEQ(fixtureA.member.ID),
		organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
	).Only(ctx)
	require.NoError(t, err)
	_, err = client.OrganizationAllocation.UpdateOneID(oldAllocation.ID).
		SetAvailable(decimal.Zero).SetStatus(domain.OrganizationAllocationStatusClosed).Save(ctx)
	require.NoError(t, err)
	_, err = client.OrganizationAllocation.Create().SetOrganizationID(fixtureA.organization.ID).
		SetUserIDSnapshot(fixtureA.member.ID).SetUserRefID(fixtureA.member.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).SetAvailable(decimal.RequireFromString("10.00000000")).SetReserved(decimal.Zero).Save(ctx)
	require.NoError(t, err)
	terminalizeOrganizationReportExecution(t, ctx, client, fixtureA.organization.ID, fixtureA.member.ID, keyA.ID, account.ID, start.Add(72*time.Hour), service.OrganizationEndpointFamilyChat, service.OrganizationExecutionTerminalOutcomeSuccess, false, "0.10000000", 4, 2)

	// A same-period foreign organization row must not affect A's totals or facets.
	terminalizeOrganizationReportExecution(t, ctx, client, fixtureB.organization.ID, fixtureB.member.ID, keyB.ID, account.ID, start.Add(24*time.Hour), service.OrganizationEndpointFamilyChat, service.OrganizationExecutionTerminalOutcomeSuccess, false, "0.90000000", 40, 20)

	funding := service.NewOrganizationFundingService(NewOrganizationFundingRepository(client))
	report, err := funding.GetReport(ctx, fixtureA.organization.ID, service.OrganizationReportRange{Start: start, End: end})
	require.NoError(t, err)
	require.Equal(t, int64(4), report.RequestCountTotal)
	require.Equal(t, int64(2), report.SuccessCount)
	require.Equal(t, int64(2), report.ErrorCountTotal)
	require.Equal(t, int64(1), report.BusinessLimitedCount)
	require.Equal(t, int64(1), report.ErrorCountSLA)
	require.Equal(t, 2.0/3.0, report.SuccessRate)
	require.Equal(t, 1.0/3.0, report.ErrorRate)
	require.Equal(t, "0.65000000", report.Cost)
	require.Equal(t, int64(24), report.InputTokens, "count_tokens tokens remain included")
	require.Equal(t, int64(15), report.CacheCreationTokens)
	require.Equal(t, int64(5), report.CacheReadTokens)
	require.Len(t, report.Models, 1)
	require.Equal(t, "report-model", report.Models[0].Model)
	require.Equal(t, int64(4), report.Models[0].RequestCount)

	current, departed, page, err := funding.ListReportMembers(ctx, fixtureA.organization.ID, service.OrganizationReportRange{Start: start, End: end}, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	var currentMember *service.OrganizationReportMemberUsage
	for index := range current {
		if current[index].UserID != nil && *current[index].UserID == fixtureA.member.ID {
			currentMember = &current[index]
		}
	}
	require.NotNil(t, currentMember)
	require.Equal(t, int64(1), currentMember.RequestCount, "only the new allocation is actionable")
	require.Equal(t, "0.10000000", currentMember.Cost)
	require.Equal(t, int64(3), currentMember.CacheCreationTokens)
	require.Equal(t, int64(1), currentMember.CacheReadTokens)
	require.Equal(t, int64(3), departed.RequestCount, "old allocation success/error are historical")
	require.Equal(t, "0.55000000", departed.Cost)
	require.Equal(t, int64(20), departed.InputTokens, "historical bucket retains count_tokens tokens")
	require.Equal(t, int64(12), departed.CacheCreationTokens)
	require.Equal(t, int64(4), departed.CacheReadTokens)

	var positiveEvidence, consumptionPostings int64
	require.NoError(t, scanOne(ctx, client, `SELECT COUNT(*) FROM organization_final_execution_evidences WHERE organization_id = $1 AND organization_settled_cost > 0`, []any{fixtureA.organization.ID}, &positiveEvidence))
	require.NoError(t, scanOne(ctx, client, `SELECT COUNT(*) FROM organization_ledger_postings p JOIN organization_ledger_operations op ON op.id = p.operation_ref_id WHERE p.organization_id = $1 AND op.operation_type = 'consumption'`, []any{fixtureA.organization.ID}, &consumptionPostings))
	require.Equal(t, positiveEvidence, consumptionPostings, "each positive final evidence has exactly one consumption posting")
	var mismatchedOccurredAt int64
	require.NoError(t, scanOne(ctx, client, `
		SELECT COUNT(*) FROM organization_ledger_postings p
		JOIN organization_final_execution_evidences e ON e.id = p.final_evidence_id_snapshot
		WHERE p.organization_id = $1 AND p.occurred_at <> e.occurred_at
	`, []any{fixtureA.organization.ID}, &mismatchedOccurredAt))
	require.Zero(t, mismatchedOccurredAt)
	ledger, ledgerPage, err := funding.ListLedger(ctx, fixtureA.organization.ID, pagination.PaginationParams{Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, ledger, 1)
	require.Greater(t, ledgerPage.Total, int64(1))
	require.Equal(t, fixtureA.organization.ID, ledger[0].OrganizationID)
}

func TestOrganizationReportPostgresEmptyOrganizationReturnsZeroes(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	_, organization := createFundingOrganization(t, ctx, tx.Client(), fmt.Sprintf("report-empty-%d", time.Now().UnixNano()), domain.OrganizationStatusActive)
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	funding := service.NewOrganizationFundingService(NewOrganizationFundingRepository(tx.Client()))
	report, err := funding.GetReport(ctx, organization.ID, service.OrganizationReportRange{Start: start, End: start.AddDate(0, 1, 0)})
	require.NoError(t, err)
	require.Zero(t, report.RequestCountTotal)
	require.Zero(t, report.InputTokens)
	require.Equal(t, "0.00000000", report.Cost)
	require.Empty(t, report.Models)
	require.Empty(t, report.Trends)
	items, departed, page, err := funding.ListReportMembers(ctx, organization.ID, service.OrganizationReportRange{Start: start, End: start.AddDate(0, 1, 0)}, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Empty(t, items)
	require.Zero(t, page.Total)
	require.Zero(t, departed.RequestCount)
	require.Equal(t, "0.00000000", departed.Cost)
}

func TestOrganizationReportPostgresUsesOccurredAtWhenReconciliationIsLate(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	suffix := fmt.Sprintf("report-late-%d", time.Now().UnixNano())
	fixture := createOrganizationAllocationIntegrationFixture(t, ctx, client, suffix, "0", "2")
	key := createOrganizationReportKey(t, ctx, client, fixture.member.ID, fixture.organization.ID, suffix)
	account := mustCreateAccount(t, client, &service.Account{Name: "organization late reporting account " + suffix})
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	reconciledAt := end.Add(48 * time.Hour)
	terminalizeOrganizationReportExecution(t, ctx, client, fixture.organization.ID, fixture.member.ID, key.ID, account.ID, start.Add(time.Hour), service.OrganizationEndpointFamilyChat, service.OrganizationExecutionTerminalOutcomeSuccess, false, "0.10000000", 4, 2, &reconciledAt)
	report, err := service.NewOrganizationFundingService(NewOrganizationFundingRepository(client)).GetReport(ctx, fixture.organization.ID, service.OrganizationReportRange{Start: start, End: end})
	require.NoError(t, err)
	require.Equal(t, int64(1), report.RequestCountTotal)
	require.Equal(t, "0.10000000", report.Cost)
}

func TestOrganizationReportPostgresScopesMemberFiltersAndSafeKeyMetadata(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	suffix := fmt.Sprintf("report-metadata-%d", time.Now().UnixNano())
	fixtureA := createOrganizationAllocationIntegrationFixture(t, ctx, client, suffix+"-a", "0", "2")
	fixtureB := createOrganizationAllocationIntegrationFixture(t, ctx, client, suffix+"-b", "0", "2")
	keyA := createOrganizationReportKey(t, ctx, client, fixtureA.member.ID, fixtureA.organization.ID, suffix+"-a")
	keyB := createOrganizationReportKey(t, ctx, client, fixtureB.member.ID, fixtureB.organization.ID, suffix+"-b")
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	funding := service.NewOrganizationFundingService(NewOrganizationFundingRepository(client))

	keys, page, err := funding.ListOrganizationAPIKeyMetadata(ctx, fixtureA.organization.ID, pagination.PaginationParams{Page: 1, PageSize: 20}, service.OrganizationReportMemberFilters{})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, keys, 1)
	require.Equal(t, keyA.ID, keys[0].ID)
	require.Equal(t, fixtureA.member.ID, keys[0].UserID)
	require.Equal(t, fixtureA.organization.ID, keys[0].OrganizationID)
	memberRows, departed, memberPage, err := funding.ListReportMembersFiltered(ctx, fixtureA.organization.ID, service.OrganizationReportRange{Start: start, End: start.AddDate(0, 1, 0)}, pagination.PaginationParams{Page: 1, PageSize: 20}, service.OrganizationReportMemberFilters{APIKeyID: &keyA.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), memberPage.Total)
	require.Len(t, memberRows, 1)
	require.Equal(t, fixtureA.member.ID, *memberRows[0].UserID)
	require.Zero(t, departed.RequestCount)

	_, _, err = funding.ListOrganizationAPIKeyMetadata(ctx, fixtureA.organization.ID, pagination.PaginationParams{Page: 1, PageSize: 20}, service.OrganizationReportMemberFilters{APIKeyID: &keyB.ID})
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	_, _, _, err = funding.ListReportMembersFiltered(ctx, fixtureA.organization.ID, service.OrganizationReportRange{Start: start, End: start.AddDate(0, 1, 0)}, pagination.PaginationParams{Page: 1, PageSize: 20}, service.OrganizationReportMemberFilters{UserID: &fixtureB.member.ID})
	require.ErrorIs(t, err, service.ErrOrganizationMemberNotFound)
	foreignAllocation, err := client.OrganizationAllocation.Query().Where(
		organizationallocation.OrganizationIDEQ(fixtureB.organization.ID),
		organizationallocation.UserIDSnapshotEQ(fixtureB.member.ID),
		organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
	).Only(ctx)
	require.NoError(t, err)
	_, _, _, err = funding.ListReportMembersFiltered(ctx, fixtureA.organization.ID, service.OrganizationReportRange{Start: start, End: start.AddDate(0, 1, 0)}, pagination.PaginationParams{Page: 1, PageSize: 20}, service.OrganizationReportMemberFilters{AllocationID: &foreignAllocation.ID})
	require.ErrorIs(t, err, service.ErrOrganizationAllocationTargetNotFound)
}

func createOrganizationReportKey(t *testing.T, ctx context.Context, client *dbent.Client, userID, organizationID int64, suffix string) *dbent.APIKey {
	t.Helper()
	key, err := client.APIKey.Create().SetUserID(userID).SetOrganizationID(organizationID).
		SetKey("organization-report-" + suffix + "-" + uuid.NewString()).SetName("organization reporting key").SetStatus(service.StatusAPIKeyActive).Save(ctx)
	require.NoError(t, err)
	return key
}

func terminalizeOrganizationReportExecution(t *testing.T, ctx context.Context, client *dbent.Client, organizationID, userID, keyID, accountID int64, occurredAt time.Time, endpoint, outcome string, businessLimited bool, rawCost string, inputTokens, outputTokens int64, reconciledAt ...*time.Time) {
	t.Helper()
	allocation, err := client.OrganizationAllocation.Query().Where(
		organizationallocation.OrganizationIDEQ(organizationID), organizationallocation.UserIDSnapshotEQ(userID), organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
	).Only(ctx)
	require.NoError(t, err)
	organization, err := client.Organization.Get(ctx, organizationID)
	require.NoError(t, err)
	command := repositoryReservationCommand(occurredAt)
	command.EndpointFamily = endpoint
	command.Model = "report-model"
	command.RequestShape = service.OrganizationRequestShapeHTTP
	if endpoint == service.OrganizationEndpointFamilyTokenCounting {
		command.RequestShape = service.OrganizationRequestShapeProbe
	}
	command.Attribution.OrganizationID = organizationID
	command.Attribution.OrganizationBalanceVersionSnapshot = organization.BalanceVersion
	command.Attribution.UserID, command.Attribution.UserOrganizationID = userID, organizationID
	command.Attribution.APIKeyID, command.Attribution.APIKeyUserID, command.Attribution.APIKeyOrganizationID = keyID, userID, organizationID
	command.Attribution.AllocationID, command.Attribution.AllocationUserID, command.Attribution.AllocationOrganizationID = allocation.ID, userID, organizationID
	command.Attribution.AllocationStatus, command.Attribution.AllocationVersionSnapshot = domain.OrganizationAllocationStatusActive, allocation.Version
	repository := NewOrganizationExecutionRepository(client)
	reserved, err := repository.Reserve(ctx, command)
	require.NoError(t, err)
	attemptID := uuid.NewString()
	evidenceJSON := fmt.Sprintf(`{"execution":"%s"}`, command.ExecutionID)
	evidenceHash := repositoryExecutionHash(evidenceJSON)
	attempt, err := repository.RecordAttemptEvidence(ctx, service.OrganizationAttemptEvidenceCommand{
		ExecutionID: command.ExecutionID, AttemptID: attemptID, CorrelationID: command.CorrelationID, ReservationID: reserved.ReservationID,
		OrganizationID: organizationID, UserID: userID, APIKeyID: keyID, AllocationID: allocation.ID, AttemptSequence: 1, AccountID: accountID,
		RouteSnapshotJSON: `{}`, RouteSnapshotHash: repositoryExecutionHash(`{}`), PricingSnapshotJSON: command.PricingSnapshotJSON, PricingSnapshotHash: command.PricingSnapshotHash,
		ProviderRequestID: "report-" + attemptID, ProviderCorrelationID: uuid.NewString(), Provenance: service.OrganizationAttemptEvidenceProvenanceAdapterDirect,
		TerminalOutcome: outcome, ProvenanceHash: evidenceHash, EvidenceJSON: evidenceJSON, EvidenceHash: evidenceHash, RawActualCost: rawCost,
		InputTokens: inputTokens, OutputTokens: outputTokens, CacheCreationTokens: 3, CacheReadTokens: 1, ProviderObservedAt: occurredAt, IngestedAt: occurredAt,
	})
	require.NoError(t, err)
	var reconciled *time.Time
	if len(reconciledAt) > 0 {
		reconciled = reconciledAt[0]
	}
	_, err = repository.Terminalize(ctx, service.OrganizationTerminalizationCommand{
		ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID, OrganizationID: organizationID, UserID: userID, APIKeyID: keyID, AllocationID: allocation.ID, ReservationID: reserved.ReservationID,
		FinalAttemptID: &attemptID, FinalAttemptEvidenceID: &attempt.AttemptEvidenceID, TerminalOutcome: outcome, BusinessLimited: businessLimited, OccurredAt: occurredAt, ReconciledAt: reconciled,
	})
	require.NoError(t, err)
}
