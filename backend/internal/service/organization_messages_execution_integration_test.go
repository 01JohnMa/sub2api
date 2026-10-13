package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestOrganizationMessagesPlanExecutesDurableSuccess(t *testing.T) {
	plan, apiKey := organizationMessagesExecutionPlan(t)
	input, err := plan.NewExecutionReservationInput(apiKey, "messages-client-request")
	require.NoError(t, err)
	require.Equal(t, OrganizationEndpointFamilyMessages, input.EndpointFamily)
	require.Equal(t, OrganizationRequestShapeHTTP, input.RequestShape)

	repo := &organizationExecutionRepositoryStub{}
	repo.reserve = func(command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
		require.Equal(t, OrganizationEndpointFamilyMessages, command.EndpointFamily)
		require.Equal(t, plan.CandidateRouteMaximumCost, command.CandidateRouteMaximumCost)
		return organizationExecutionReserved(command), nil
	}
	repo.attempt = func(command OrganizationAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error) {
		require.Equal(t, 1, command.AttemptSequence)
		require.Equal(t, repo.reserveCommand.PricingSnapshotHash, command.PricingSnapshotHash)
		return &OrganizationAttemptEvidenceResult{AttemptEvidenceID: 901, ExecutionID: command.ExecutionID, AttemptID: command.AttemptID}, nil
	}
	repo.terminal = func(command OrganizationTerminalizationCommand) (*OrganizationFinalExecutionEvidenceResult, error) {
		require.NotNil(t, command.FinalAttemptID)
		require.NotNil(t, command.FinalAttemptEvidenceID)
		return organizationExecutionFinal(command, repo.attemptCommand.RawActualCost, "1.25000000", OrganizationReservationStateSettled), nil
	}
	execution := newOrganizationExecutionTestService(repo)
	command, err := execution.NewReservationCommand(input)
	require.NoError(t, err)
	observation := organizationExecutionObservation()
	observation.RawActualCost = "1.25000000"

	run, err := execution.ExecuteMultiAttempt(context.Background(), *command, plan.MaximumAttempts, &organizationExecutionAdapterStub{result: &OrganizationExecutionAttemptResult{
		Dispatched: true, TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess, Observation: observation,
	}})

	require.NoError(t, err)
	require.False(t, run.PendingReconciliation)
	require.Len(t, run.Attempts, 1)
	require.NotNil(t, run.Final)
	require.Equal(t, "1.25000000", run.Final.OrganizationSettledCost)
	require.Equal(t, 1, repo.reserveCalls)
	require.Equal(t, 1, repo.attemptCalls)
	require.Equal(t, 1, repo.terminalCalls)
}

func TestOrganizationMessagesPlanPreservesUnknownDispatchForReconciliation(t *testing.T) {
	plan, apiKey := organizationMessagesExecutionPlan(t)
	input, err := plan.NewExecutionReservationInput(apiKey, "messages-pending-request")
	require.NoError(t, err)

	base := &organizationExecutionRepositoryStub{}
	base.reserve = func(command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
		return organizationExecutionReserved(command), nil
	}
	repo := &organizationExecutionRecoveryRepositoryStub{organizationExecutionRepositoryStub: base}
	execution := newOrganizationExecutionTestService(repo)
	command, err := execution.NewReservationCommand(input)
	require.NoError(t, err)
	observation := organizationExecutionObservation()
	observation.RawActualCost = ""
	observation.InputTokens = 0
	observation.OutputTokens = 0
	observation.CacheCreationTokens = 0
	observation.CacheReadTokens = 0

	run, err := execution.ExecuteMultiAttempt(context.Background(), *command, plan.MaximumAttempts, &organizationExecutionAdapterStub{result: &OrganizationExecutionAttemptResult{
		Dispatched: true, PendingReconciliation: true,
		TerminalOutcome: OrganizationAttemptEvidenceTerminalOutcomeUnknown,
		Observation:     observation,
	}})

	require.NoError(t, err)
	require.True(t, run.PendingReconciliation)
	require.Nil(t, run.Final)
	require.Len(t, repo.pendingCommands, 1)
	require.Equal(t, base.reserveCommand.ExecutionID, repo.pendingCommands[0].ExecutionID)
	require.Equal(t, int64(301), repo.pendingCommands[0].ReservationID)
	require.Equal(t, base.reserveCommand.PricingSnapshotHash, repo.pendingCommands[0].PricingSnapshotHash)
	require.Equal(t, 0, base.attemptCalls)
	require.Equal(t, 0, base.terminalCalls)
}

func organizationMessagesExecutionPlan(t *testing.T) (*OrganizationMessagesCandidatePlan, *APIKey) {
	t.Helper()
	policy := OrganizationMessagesRequestPolicy{
		RequestedModel: "claude-sonnet-4", RequestShape: OrganizationRequestShapeHTTP,
		InputTokenCeiling: 1024, OutputTokenCeiling: 64,
	}
	body := []byte(`{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
	candidate := OrganizationMessagesCandidate{
		AccountID: 23, RequestedModel: policy.RequestedModel, DispatchModel: policy.RequestedModel, UpstreamModel: policy.RequestedModel,
		Route: OrganizationMessagesRoute{AccountID: 23, Platform: PlatformAnthropic, AccountType: AccountTypeAPIKey, DispatchModel: policy.RequestedModel, UpstreamModel: policy.RequestedModel, AccountMultiplier: 1},
		Pricing: OrganizationResponsesCandidatePricing{
			Model: policy.RequestedModel, RateMultiplier: 1, AccountMultiplier: 1,
			InputTokenCeiling: policy.InputTokenCeiling, OutputTokenCeiling: policy.OutputTokenCeiling,
			MaximumCost: "2.00000000",
		},
		ForwardBodyHash: organizationMessagesBodyHash(body), PlanningBodyHash: organizationMessagesBodyHash(body),
	}
	plan, err := buildOrganizationMessagesPlan(policy, PlatformAnthropic, body, candidate)
	require.NoError(t, err)

	groupID := int64(42)
	organizationID := int64(88)
	role := OrganizationRoleMember
	apiKey := &APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID,
		User: &User{ID: 7, Status: StatusActive, OrganizationID: &organizationID, OrganizationRole: &role},
		OrganizationAuthorization: &OrganizationKeyAuthorization{
			APIKeyID: 9, UserID: 7, OrganizationID: organizationID, OrganizationStatus: OrganizationStatusActive,
			AllocationID: 77, AllocationUserID: 7, AllocationOrganizationID: organizationID,
			AllocationStatus: domain.OrganizationAllocationStatusActive, Available: "10.00000000",
		},
	}
	return plan, apiKey
}
