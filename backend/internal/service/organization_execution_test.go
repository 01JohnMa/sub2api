package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type organizationExecutionRepositoryStub struct {
	reserveCalls    int
	attemptCalls    int
	terminalCalls   int
	reserveCommand  OrganizationExecutionReservationCommand
	attemptCommand  OrganizationAttemptEvidenceCommand
	terminalCommand OrganizationTerminalizationCommand
	reserve         func(OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error)
	attempt         func(OrganizationAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error)
	terminal        func(OrganizationTerminalizationCommand) (*OrganizationFinalExecutionEvidenceResult, error)
}

func (r *organizationExecutionRepositoryStub) Reserve(_ context.Context, command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
	r.reserveCalls++
	r.reserveCommand = command
	if r.reserve == nil {
		return nil, errors.New("unexpected reserve")
	}
	return r.reserve(command)
}

func (r *organizationExecutionRepositoryStub) RecordAttemptEvidence(_ context.Context, command OrganizationAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error) {
	r.attemptCalls++
	r.attemptCommand = command
	if r.attempt == nil {
		return nil, errors.New("unexpected attempt evidence")
	}
	return r.attempt(command)
}

func (r *organizationExecutionRepositoryStub) Terminalize(_ context.Context, command OrganizationTerminalizationCommand) (*OrganizationFinalExecutionEvidenceResult, error) {
	r.terminalCalls++
	r.terminalCommand = command
	if r.terminal == nil {
		return nil, errors.New("unexpected terminalization")
	}
	return r.terminal(command)
}

type organizationExecutionAdapterStub struct {
	calls   int
	request OrganizationExecutionDispatchRequest
	result  *OrganizationExecutionAttemptResult
	err     error
}

type organizationExecutionSequenceAdapterStub struct {
	requests []OrganizationExecutionDispatchRequest
	results  []*OrganizationExecutionAttemptResult
	errs     []error
	index    int
}

func (a *organizationExecutionSequenceAdapterStub) Dispatch(_ context.Context, request OrganizationExecutionDispatchRequest) (*OrganizationExecutionAttemptResult, error) {
	a.requests = append(a.requests, request)
	if a.index >= len(a.results) {
		return nil, errors.New("unexpected dispatch")
	}
	result := *a.results[a.index]
	var dispatchErr error
	if a.index < len(a.errs) {
		dispatchErr = a.errs[a.index]
	}
	a.index++
	if result.Observation != nil {
		observation := *result.Observation
		if observation.ProviderCorrelationID == "" {
			observation.ProviderCorrelationID = request.ProviderCorrelationID
		}
		if observation.ProviderRequestID == "" {
			observation.ProviderRequestID = request.ProviderRequestID
		}
		result.Observation = &observation
	}
	return &result, dispatchErr
}

type organizationExecutionRecoveryRepositoryStub struct {
	*organizationExecutionRepositoryStub
	pendingCommands  []OrganizationPendingAttemptCommand
	reconcileCommand OrganizationPendingReconciliationCommand
	reconcileResult  *OrganizationFinalExecutionEvidenceResult
}

type organizationExecutionAutomaticEffectsRepositoryStub struct {
	*organizationExecutionRecoveryRepositoryStub
	accountEffectAttemptIDs []string
	executionEffectLimits   []int
}

func (r *organizationExecutionAutomaticEffectsRepositoryStub) RedriveAttemptAccountEffect(_ context.Context, attemptID string) (*OrganizationAttemptAccountEffectResult, error) {
	r.accountEffectAttemptIDs = append(r.accountEffectAttemptIDs, attemptID)
	return &OrganizationAttemptAccountEffectResult{AttemptID: attemptID, Status: OrganizationAttemptAccountEffectStateApplied}, nil
}

func (r *organizationExecutionAutomaticEffectsRepositoryStub) RedrivePendingExecutionEffects(_ context.Context, limit int) ([]OrganizationExecutionOutboxApplyResult, error) {
	r.executionEffectLimits = append(r.executionEffectLimits, limit)
	return nil, nil
}

func (r *organizationExecutionAutomaticEffectsRepositoryStub) RedrivePendingAttemptAccountEffects(_ context.Context, limit int) ([]OrganizationAttemptAccountEffectResult, error) {
	return nil, nil
}

func (r *organizationExecutionRecoveryRepositoryStub) RecordPendingAttempt(_ context.Context, command OrganizationPendingAttemptCommand) error {
	r.pendingCommands = append(r.pendingCommands, command)
	return nil
}

func (r *organizationExecutionRecoveryRepositoryStub) ReconcilePendingExecution(_ context.Context, command OrganizationPendingReconciliationCommand) (*OrganizationFinalExecutionEvidenceResult, error) {
	r.reconcileCommand = command
	if r.reconcileResult == nil {
		return nil, errors.New("unexpected reconciliation")
	}
	return r.reconcileResult, nil
}

func (r *organizationExecutionRecoveryRepositoryStub) RedriveAttemptAccountEffect(context.Context, string) (*OrganizationAttemptAccountEffectResult, error) {
	return nil, errors.New("unexpected account effect redrive")
}

func (r *organizationExecutionRecoveryRepositoryStub) RedrivePendingAttemptAccountEffects(context.Context, int) ([]OrganizationAttemptAccountEffectResult, error) {
	return nil, errors.New("unexpected account effect batch redrive")
}

func (r *organizationExecutionRecoveryRepositoryStub) RedriveExecutionOutbox(context.Context, int64) (*OrganizationExecutionOutboxApplyResult, error) {
	return nil, errors.New("unexpected execution outbox redrive")
}

func (a *organizationExecutionAdapterStub) Dispatch(_ context.Context, request OrganizationExecutionDispatchRequest) (*OrganizationExecutionAttemptResult, error) {
	a.calls++
	a.request = request
	if a.result == nil || a.result.Observation == nil || a.result.Observation.ProviderCorrelationID != "" {
		return a.result, a.err
	}
	result := *a.result
	observation := *a.result.Observation
	observation.ProviderCorrelationID = request.ProviderCorrelationID
	observation.ProviderRequestID = request.ProviderRequestID
	result.Observation = &observation
	return &result, a.err
}

func organizationExecutionClock() time.Time {
	return time.Date(2026, time.July, 28, 9, 30, 0, 0, time.UTC)
}

func organizationExecutionInput() OrganizationExecutionReservationInput {
	return OrganizationExecutionReservationInput{
		Attribution: OrganizationExecutionTrustedAttribution{
			OrganizationID:                     7,
			OrganizationStatus:                 OrganizationStatusActive,
			OrganizationBalanceVersionSnapshot: 3,
			UserID:                             11,
			UserOrganizationID:                 7,
			APIKeyID:                           13,
			APIKeyUserID:                       11,
			APIKeyOrganizationID:               7,
			AllocationID:                       17,
			AllocationUserID:                   11,
			AllocationOrganizationID:           7,
			AllocationStatus:                   domain.OrganizationAllocationStatusActive,
			AllocationVersionSnapshot:          5,
		},
		EndpointFamily:            OrganizationEndpointFamilyMessages,
		RequestShape:              OrganizationRequestShapeHTTP,
		ClientRequestID:           "client-request-ignored-for-billing",
		CandidateSnapshot:         []byte(` { "routes" : [ { "id" : "route-a" } ] } `),
		PricingSnapshot:           []byte(`{"model":"claude-test","unit_cost":"0.000001"}`),
		CandidateRouteMaximumCost: "5",
		BillingFingerprint:        strings.Repeat("a", 64),
	}
}

func organizationExecutionReserved(command OrganizationExecutionReservationCommand) *OrganizationExecutionReservationResult {
	return &OrganizationExecutionReservationResult{
		ExecutionID:    command.ExecutionID,
		CorrelationID:  command.CorrelationID,
		OrganizationID: command.Attribution.OrganizationID,
		UserID:         command.Attribution.UserID,
		APIKeyID:       command.Attribution.APIKeyID,
		AllocationID:   command.Attribution.AllocationID,
		ReservationID:  301,
		ReservedAmount: command.CandidateRouteMaximumCost,
		State:          OrganizationReservationStateReserved,
		Disposition:    OrganizationExecutionReservationDispositionReserved,
	}
}

func organizationExecutionFinal(command OrganizationTerminalizationCommand, rawCost, settledCost string, reservationState string) *OrganizationFinalExecutionEvidenceResult {
	result := &OrganizationFinalExecutionEvidenceResult{
		FinalEvidenceID:         401,
		ExecutionID:             command.ExecutionID,
		CorrelationID:           command.CorrelationID,
		OrganizationID:          command.OrganizationID,
		UserID:                  command.UserID,
		APIKeyID:                command.APIKeyID,
		AllocationID:            command.AllocationID,
		ReservationID:           command.ReservationID,
		FinalAttemptID:          command.FinalAttemptID,
		FinalAttemptEvidenceID:  command.FinalAttemptEvidenceID,
		TerminalOutcome:         command.TerminalOutcome,
		BusinessLimited:         command.BusinessLimited,
		RawActualCost:           rawCost,
		OrganizationSettledCost: settledCost,
		ZeroProof:               command.ZeroProof,
		ReservationState:        reservationState,
		OccurredAt:              command.OccurredAt,
	}
	if settledCost != "0.00000000" {
		postingID := int64(501)
		result.ConsumptionPostingID = &postingID
	}
	if command.ReservationID == 0 {
		result.ReservationState = ""
	}
	return result
}

func organizationExecutionObservation() *OrganizationExecutionAttemptObservation {
	return &OrganizationExecutionAttemptObservation{
		AccountID:           23,
		RouteSnapshot:       []byte(`{"account_id":23,"route":"route-a"}`),
		ProviderRequestID:   "",
		Evidence:            []byte(`{"input_tokens":12,"output_tokens":7,"status":"success"}`),
		RawActualCost:       "1.2345678912300",
		InputTokens:         12,
		OutputTokens:        7,
		CacheCreationTokens: 2,
		CacheReadTokens:     3,
		ProviderObservedAt:  organizationExecutionClock(),
	}
}

func newOrganizationExecutionTestService(repo OrganizationExecutionRepository) *OrganizationExecutionService {
	service := NewOrganizationExecutionService(repo)
	service.now = organizationExecutionClock
	return service
}

func TestOrganizationExecutionServiceSingleAttemptSuccessPersistsEvidenceBeforeSettlement(t *testing.T) {
	repo := &organizationExecutionRepositoryStub{}
	repo.reserve = func(command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
		return organizationExecutionReserved(command), nil
	}
	repo.attempt = func(command OrganizationAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error) {
		return &OrganizationAttemptEvidenceResult{AttemptEvidenceID: 601, ExecutionID: command.ExecutionID, AttemptID: command.AttemptID}, nil
	}
	repo.terminal = func(command OrganizationTerminalizationCommand) (*OrganizationFinalExecutionEvidenceResult, error) {
		return organizationExecutionFinal(command, "1.23456789123", "1.23456789", OrganizationReservationStateSettled), nil
	}
	service := newOrganizationExecutionTestService(repo)
	command, err := service.NewReservationCommand(organizationExecutionInput())
	require.NoError(t, err)

	result, err := service.ExecuteSingleAttempt(context.Background(), *command, OrganizationExecutionFakeAdapter{Result: OrganizationExecutionAttemptResult{
		Dispatched:      true,
		TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess,
		Observation:     organizationExecutionObservation(),
	}})

	require.NoError(t, err)
	require.Equal(t, 1, repo.reserveCalls)
	require.Equal(t, 1, repo.attemptCalls)
	require.Equal(t, 1, repo.terminalCalls)
	require.NoError(t, ValidateOrganizationExecutionReservationCommand(repo.reserveCommand))
	require.NoError(t, uuid.Validate(repo.reserveCommand.ExecutionID))
	require.NoError(t, uuid.Validate(repo.reserveCommand.CorrelationID))
	require.NotEqual(t, repo.reserveCommand.ExecutionID, repo.reserveCommand.CorrelationID)
	require.Equal(t, `{"routes":[{"id":"route-a"}]}`, repo.reserveCommand.CandidateSnapshotJSON)
	require.Equal(t, "1.23456789123", repo.attemptCommand.RawActualCost)
	require.Equal(t, OrganizationAttemptEvidenceProvenanceAdapterDirect, repo.attemptCommand.Provenance)
	require.NoError(t, uuid.Validate(repo.attemptCommand.AttemptID))
	require.NoError(t, uuid.Validate(repo.attemptCommand.ProviderCorrelationID))
	require.Equal(t, repo.attemptCommand.AttemptID, *repo.terminalCommand.FinalAttemptID)
	require.EqualValues(t, 601, *repo.terminalCommand.FinalAttemptEvidenceID)
	require.Equal(t, "1.23456789", result.OrganizationSettledCost)
	require.Less(t, result.OrganizationSettledCost, repo.reserveCommand.CandidateRouteMaximumCost)
}

func TestOrganizationExecutionServiceZeroUsageReleasesReservationWithoutConsumption(t *testing.T) {
	repo := &organizationExecutionRepositoryStub{}
	repo.reserve = func(command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
		return organizationExecutionReserved(command), nil
	}
	repo.attempt = func(command OrganizationAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error) {
		return &OrganizationAttemptEvidenceResult{AttemptEvidenceID: 602, ExecutionID: command.ExecutionID, AttemptID: command.AttemptID}, nil
	}
	repo.terminal = func(command OrganizationTerminalizationCommand) (*OrganizationFinalExecutionEvidenceResult, error) {
		return organizationExecutionFinal(command, "0", "0.00000000", OrganizationReservationStateReleased), nil
	}
	service := newOrganizationExecutionTestService(repo)
	command, err := service.NewReservationCommand(organizationExecutionInput())
	require.NoError(t, err)
	observation := organizationExecutionObservation()
	observation.RawActualCost = "0.000000000000"

	result, err := service.ExecuteSingleAttempt(context.Background(), *command, OrganizationExecutionFakeAdapter{Result: OrganizationExecutionAttemptResult{
		Dispatched:      true,
		TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess,
		Observation:     observation,
	}})

	require.NoError(t, err)
	require.Equal(t, 1, repo.attemptCalls)
	require.Equal(t, 1, repo.terminalCalls)
	require.Equal(t, "0", repo.attemptCommand.RawActualCost)
	require.Nil(t, result.ConsumptionPostingID)
	require.Equal(t, OrganizationReservationStateReleased, result.ReservationState)
}

func TestOrganizationExecutionServiceMultiAttemptFailoverPersistsEveryAttemptAndSettlesOnlyFinalEvidence(t *testing.T) {
	repo := &organizationExecutionRepositoryStub{}
	repo.reserve = func(command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
		return organizationExecutionReserved(command), nil
	}
	var commands []OrganizationAttemptEvidenceCommand
	repo.attempt = func(command OrganizationAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error) {
		commands = append(commands, command)
		return &OrganizationAttemptEvidenceResult{AttemptEvidenceID: int64(600 + len(commands)), ExecutionID: command.ExecutionID, AttemptID: command.AttemptID}, nil
	}
	repo.terminal = func(command OrganizationTerminalizationCommand) (*OrganizationFinalExecutionEvidenceResult, error) {
		require.Equal(t, commands[1].AttemptID, *command.FinalAttemptID)
		return organizationExecutionFinal(command, commands[1].RawActualCost, "1.50000000", OrganizationReservationStateSettled), nil
	}
	service := newOrganizationExecutionTestService(repo)
	command, err := service.NewReservationCommand(organizationExecutionInput())
	require.NoError(t, err)
	failed := organizationExecutionObservation()
	failed.RawActualCost = "0.25000000"
	failed.Evidence = []byte(`{"status":"error","input_tokens":2}`)
	failed.InputTokens = 2
	failed.OutputTokens = 0
	final := organizationExecutionObservation()
	final.RawActualCost = "1.50000000"
	adapter := &organizationExecutionSequenceAdapterStub{results: []*OrganizationExecutionAttemptResult{
		{Dispatched: true, TerminalOutcome: OrganizationExecutionTerminalOutcomeError, Observation: failed, Retryable: true},
		{Dispatched: true, TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess, Observation: final},
	}}

	run, err := service.ExecuteMultiAttempt(context.Background(), *command, 2, adapter)

	require.NoError(t, err)
	require.Len(t, commands, 2)
	require.Equal(t, 1, commands[0].AttemptSequence)
	require.Equal(t, 2, commands[1].AttemptSequence)
	require.NotEqual(t, commands[0].AttemptID, commands[1].AttemptID)
	require.Equal(t, commands[0].AttemptID, run.Attempts[0].AttemptID)
	require.True(t, run.Attempts[0].Retryable)
	require.NotNil(t, run.Final)
	require.Equal(t, commands[1].AttemptID, *run.Final.FinalAttemptID)
	require.Equal(t, 1, repo.terminalCalls)
	require.Len(t, adapter.requests, 2)
	require.NotEqual(t, adapter.requests[0].ProviderRequestID, adapter.requests[1].ProviderRequestID)
}

func TestOrganizationExecutionServiceBestEffortAppliesDurableRuntimeEffects(t *testing.T) {
	base := &organizationExecutionRepositoryStub{}
	base.reserve = func(command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
		return organizationExecutionReserved(command), nil
	}
	base.attempt = func(command OrganizationAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error) {
		return &OrganizationAttemptEvidenceResult{AttemptEvidenceID: 811, ExecutionID: command.ExecutionID, AttemptID: command.AttemptID}, nil
	}
	base.terminal = func(command OrganizationTerminalizationCommand) (*OrganizationFinalExecutionEvidenceResult, error) {
		return organizationExecutionFinal(command, "1.25", "1.25000000", OrganizationReservationStateSettled), nil
	}
	repo := &organizationExecutionAutomaticEffectsRepositoryStub{
		organizationExecutionRecoveryRepositoryStub: &organizationExecutionRecoveryRepositoryStub{organizationExecutionRepositoryStub: base},
	}
	executionService := newOrganizationExecutionTestService(repo)
	command, err := executionService.NewReservationCommand(organizationExecutionInput())
	require.NoError(t, err)
	observation := organizationExecutionObservation()
	observation.RawActualCost = "1.25"

	run, err := executionService.ExecuteMultiAttempt(context.Background(), *command, 1, &organizationExecutionAdapterStub{result: &OrganizationExecutionAttemptResult{
		Dispatched: true, TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess, Observation: observation,
	}})

	require.NoError(t, err)
	require.Len(t, repo.accountEffectAttemptIDs, 1)
	require.Equal(t, run.Attempts[0].AttemptID, repo.accountEffectAttemptIDs[0])
	require.Equal(t, []int{100}, repo.executionEffectLimits)
}

func TestOrganizationExecutionServiceCandidateConflictAfterFailoverReleasesWithZeroProof(t *testing.T) {
	repo := &organizationExecutionRepositoryStub{}
	repo.reserve = func(command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
		return organizationExecutionReserved(command), nil
	}
	repo.attempt = func(command OrganizationAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error) {
		return &OrganizationAttemptEvidenceResult{AttemptEvidenceID: 701, ExecutionID: command.ExecutionID, AttemptID: command.AttemptID}, nil
	}
	repo.terminal = func(command OrganizationTerminalizationCommand) (*OrganizationFinalExecutionEvidenceResult, error) {
		require.Nil(t, command.FinalAttemptID)
		require.Equal(t, "candidate_snapshot_conflict", command.ZeroProof)
		return organizationExecutionFinal(command, "0", "0.00000000", OrganizationReservationStateReleased), nil
	}
	service := newOrganizationExecutionTestService(repo)
	command, err := service.NewReservationCommand(organizationExecutionInput())
	require.NoError(t, err)
	failed := organizationExecutionObservation()
	failed.RawActualCost = "0"
	adapter := &organizationExecutionSequenceAdapterStub{results: []*OrganizationExecutionAttemptResult{
		{Dispatched: true, TerminalOutcome: OrganizationExecutionTerminalOutcomeError, Observation: failed, Retryable: true},
		{Dispatched: false, TerminalOutcome: OrganizationExecutionTerminalOutcomeError, ZeroProof: "candidate_snapshot_conflict"},
	}}

	run, err := service.ExecuteMultiAttempt(context.Background(), *command, 2, adapter)

	require.NoError(t, err)
	require.Len(t, run.Attempts, 1)
	require.NotNil(t, run.Final)
	require.Equal(t, OrganizationReservationStateReleased, run.Final.ReservationState)
	require.Equal(t, 1, repo.attemptCalls)
	require.Equal(t, 1, repo.terminalCalls)
}

func TestOrganizationExecutionServiceUnknownDispatchKeepsReservationPending(t *testing.T) {
	base := &organizationExecutionRepositoryStub{}
	base.reserve = func(command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
		return organizationExecutionReserved(command), nil
	}
	repo := &organizationExecutionRecoveryRepositoryStub{organizationExecutionRepositoryStub: base}
	service := newOrganizationExecutionTestService(repo)
	command, err := service.NewReservationCommand(organizationExecutionInput())
	require.NoError(t, err)
	observation := organizationExecutionObservation()
	adapter := &organizationExecutionSequenceAdapterStub{results: []*OrganizationExecutionAttemptResult{{
		Dispatched:            true,
		TerminalOutcome:       OrganizationAttemptEvidenceTerminalOutcomeUnknown,
		Observation:           observation,
		PendingReconciliation: true,
	}}}

	run, err := service.ExecuteMultiAttempt(context.Background(), *command, 2, adapter)

	require.NoError(t, err)
	require.True(t, run.PendingReconciliation)
	require.Nil(t, run.Final)
	require.Len(t, repo.pendingCommands, 1)
	require.Equal(t, 0, base.attemptCalls)
	require.Equal(t, 0, base.terminalCalls)
	require.Equal(t, adapter.requests[0].ProviderRequestID, repo.pendingCommands[0].ProviderRequestID)
}

func TestOrganizationExecutionServicePostDispatchErrorPersistsPendingAttempt(t *testing.T) {
	base := &organizationExecutionRepositoryStub{}
	base.reserve = func(command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
		return organizationExecutionReserved(command), nil
	}
	repo := &organizationExecutionRecoveryRepositoryStub{organizationExecutionRepositoryStub: base}
	service := newOrganizationExecutionTestService(repo)
	command, err := service.NewReservationCommand(organizationExecutionInput())
	require.NoError(t, err)
	adapter := &organizationExecutionSequenceAdapterStub{
		results: []*OrganizationExecutionAttemptResult{{
			Dispatched:            true,
			TerminalOutcome:       OrganizationAttemptEvidenceTerminalOutcomeUnknown,
			Observation:           organizationExecutionObservation(),
			PendingReconciliation: true,
		}},
		errs: []error{errors.New("upstream response stream ended after dispatch")},
	}

	run, err := service.ExecuteMultiAttempt(context.Background(), *command, 1, adapter)

	require.NoError(t, err)
	require.True(t, run.PendingReconciliation)
	require.Len(t, repo.pendingCommands, 1)
	require.Equal(t, adapter.requests[0].AttemptID, repo.pendingCommands[0].AttemptID)
	require.Equal(t, adapter.requests[0].ProviderRequestID, repo.pendingCommands[0].ProviderRequestID)
	require.Equal(t, 0, base.terminalCalls)
}

func TestOrganizationExecutionAttemptResultRejectsRetryableSuccess(t *testing.T) {
	err := ValidateOrganizationExecutionAttemptResult(&OrganizationExecutionAttemptResult{
		Dispatched:      true,
		TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess,
		Observation:     organizationExecutionObservation(),
		Retryable:       true,
	})
	require.ErrorIs(t, err, ErrOrganizationExecutionInvalid)
}

func TestOrganizationExecutionRecoveryServiceRequiresInteractivePlatformAdminJWT(t *testing.T) {
	repo := &organizationExecutionRecoveryRepositoryStub{organizationExecutionRepositoryStub: &organizationExecutionRepositoryStub{}}
	recovery := NewOrganizationExecutionRecoveryService(repo)
	_, err := recovery.ReconcilePending(context.Background(), OrganizationReconciliationActor{
		UserID:             42,
		DisplayName:        "long-lived admin key",
		PlatformRole:       domain.RoleAdmin,
		AuthorizationScope: "admin_api_key",
		Active:             true,
	}, uuid.NewString(), "ticket10-reconcile-admin-api-key")
	require.ErrorIs(t, err, ErrOrganizationExecutionReconciliationForbidden)
}

func TestOrganizationExecutionRecoveryServiceBuildsScopedIdempotentCommand(t *testing.T) {
	repo := &organizationExecutionRecoveryRepositoryStub{
		organizationExecutionRepositoryStub: &organizationExecutionRepositoryStub{},
		reconcileResult: &OrganizationFinalExecutionEvidenceResult{
			ExecutionID: "ignored-by-service-validation",
		},
	}
	recovery := NewOrganizationExecutionRecoveryService(repo)
	executionID := uuid.NewString()
	_, err := recovery.ReconcilePending(context.Background(), OrganizationReconciliationActor{
		UserID:             42,
		DisplayName:        "platform admin",
		PlatformRole:       domain.RoleAdmin,
		AuthorizationScope: domain.OrganizationFundingAuthorizationPlatformAdminJWT,
		Active:             true,
	}, executionID, "ticket10-reconcile-key")
	require.NoError(t, err)
	require.Equal(t, executionID, repo.reconcileCommand.ExecutionID)
	require.NotEmpty(t, repo.reconcileCommand.OperationID)
	require.NotEmpty(t, repo.reconcileCommand.CorrelationID)
	require.Len(t, repo.reconcileCommand.IdempotencyKeyHash, 64)
	require.Len(t, repo.reconcileCommand.CanonicalPayloadHash, 64)
}

func TestOrganizationExecutionServiceStalePreDispatchEvidenceSkipsAdapterAndReservation(t *testing.T) {
	repo := &organizationExecutionRepositoryStub{}
	repo.reserve = func(command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
		terminal := OrganizationTerminalizationCommand{
			ExecutionID:     command.ExecutionID,
			CorrelationID:   command.CorrelationID,
			OrganizationID:  command.Attribution.OrganizationID,
			UserID:          command.Attribution.UserID,
			APIKeyID:        command.Attribution.APIKeyID,
			AllocationID:    command.Attribution.AllocationID,
			TerminalOutcome: OrganizationExecutionTerminalOutcomeError,
			ZeroProof:       "stale_authoritative_attribution",
			OccurredAt:      organizationExecutionClock(),
		}
		return &OrganizationExecutionReservationResult{
			ExecutionID:    command.ExecutionID,
			CorrelationID:  command.CorrelationID,
			OrganizationID: command.Attribution.OrganizationID,
			UserID:         command.Attribution.UserID,
			APIKeyID:       command.Attribution.APIKeyID,
			AllocationID:   command.Attribution.AllocationID,
			Disposition:    OrganizationExecutionReservationDispositionPreDispatchRejected,
			FinalEvidence:  organizationExecutionFinal(terminal, "0", "0.00000000", OrganizationReservationStateReleased),
		}, nil
	}
	service := newOrganizationExecutionTestService(repo)
	command, err := service.NewReservationCommand(organizationExecutionInput())
	require.NoError(t, err)

	result, err := service.ExecuteSingleAttempt(context.Background(), *command, nil)

	require.NoError(t, err)
	require.Equal(t, 0, repo.attemptCalls)
	require.Equal(t, 0, repo.terminalCalls)
	require.EqualValues(t, 0, result.ReservationID)
	require.Equal(t, OrganizationExecutionTerminalOutcomeError, result.TerminalOutcome)
	require.Equal(t, "0.00000000", result.OrganizationSettledCost)
}

func TestOrganizationExecutionServiceAcceptsPersistedInsufficientAllocationProof(t *testing.T) {
	repo := &organizationExecutionRepositoryStub{}
	repo.reserve = func(command OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
		terminal := OrganizationTerminalizationCommand{
			ExecutionID:     command.ExecutionID,
			CorrelationID:   command.CorrelationID,
			OrganizationID:  command.Attribution.OrganizationID,
			UserID:          command.Attribution.UserID,
			APIKeyID:        command.Attribution.APIKeyID,
			AllocationID:    command.Attribution.AllocationID,
			TerminalOutcome: OrganizationExecutionTerminalOutcomeError,
			ZeroProof:       "organization_allocation_insufficient",
			OccurredAt:      organizationExecutionClock(),
		}
		return &OrganizationExecutionReservationResult{
			ExecutionID:    command.ExecutionID,
			CorrelationID:  command.CorrelationID,
			OrganizationID: command.Attribution.OrganizationID,
			UserID:         command.Attribution.UserID,
			APIKeyID:       command.Attribution.APIKeyID,
			AllocationID:   command.Attribution.AllocationID,
			Disposition:    OrganizationExecutionReservationDispositionPreDispatchRejected,
			FinalEvidence:  organizationExecutionFinal(terminal, "0", "0.00000000", OrganizationReservationStateReleased),
		}, nil
	}
	service := newOrganizationExecutionTestService(repo)
	command, err := service.NewReservationCommand(organizationExecutionInput())
	require.NoError(t, err)

	result, err := service.ExecuteSingleAttempt(context.Background(), *command, nil)

	require.NoError(t, err)
	require.Equal(t, "organization_allocation_insufficient", result.ZeroProof)
	require.Equal(t, 0, repo.attemptCalls)
	require.Equal(t, 0, repo.terminalCalls)
}

func TestOrganizationExecutionServiceFinalizedReplayNeverDispatchesTwice(t *testing.T) {
	repo := &organizationExecutionRepositoryStub{}
	service := newOrganizationExecutionTestService(repo)
	command, err := service.NewReservationCommand(organizationExecutionInput())
	require.NoError(t, err)
	firstReservation := organizationExecutionReserved(*command)
	var final *OrganizationFinalExecutionEvidenceResult
	repo.reserve = func(received OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
		if final == nil {
			return firstReservation, nil
		}
		return &OrganizationExecutionReservationResult{
			ExecutionID:    received.ExecutionID,
			CorrelationID:  received.CorrelationID,
			OrganizationID: received.Attribution.OrganizationID,
			UserID:         received.Attribution.UserID,
			APIKeyID:       received.Attribution.APIKeyID,
			AllocationID:   received.Attribution.AllocationID,
			ReservationID:  firstReservation.ReservationID,
			ReservedAmount: received.CandidateRouteMaximumCost,
			State:          final.ReservationState,
			Disposition:    OrganizationExecutionReservationDispositionFinalized,
			FinalEvidence:  final,
			Replayed:       true,
		}, nil
	}
	repo.attempt = func(received OrganizationAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error) {
		return &OrganizationAttemptEvidenceResult{AttemptEvidenceID: 603, ExecutionID: received.ExecutionID, AttemptID: received.AttemptID}, nil
	}
	repo.terminal = func(received OrganizationTerminalizationCommand) (*OrganizationFinalExecutionEvidenceResult, error) {
		final = organizationExecutionFinal(received, "1", "1.00000000", OrganizationReservationStateSettled)
		return final, nil
	}
	adapter := &organizationExecutionAdapterStub{result: &OrganizationExecutionAttemptResult{
		Dispatched:      true,
		TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess,
		Observation:     organizationExecutionObservation(),
	}}

	first, err := service.ExecuteSingleAttempt(context.Background(), *command, adapter)
	require.NoError(t, err)
	second, err := service.ExecuteSingleAttempt(context.Background(), *command, adapter)

	require.NoError(t, err)
	require.NotSame(t, first, second)
	require.Equal(t, first.FinalEvidenceID, second.FinalEvidenceID)
	require.Equal(t, 2, repo.reserveCalls)
	require.Equal(t, 1, repo.attemptCalls)
	require.Equal(t, 1, repo.terminalCalls)
	require.Equal(t, 1, adapter.calls)
	require.True(t, second.Replayed)
}

func TestOrganizationExecutionServicePropagatesFingerprintConflictBeforeDispatch(t *testing.T) {
	repo := &organizationExecutionRepositoryStub{
		reserve: func(OrganizationExecutionReservationCommand) (*OrganizationExecutionReservationResult, error) {
			return nil, ErrOrganizationExecutionConflict
		},
	}
	service := newOrganizationExecutionTestService(repo)
	command, err := service.NewReservationCommand(organizationExecutionInput())
	require.NoError(t, err)
	adapter := &organizationExecutionAdapterStub{}

	_, err = service.ExecuteSingleAttempt(context.Background(), *command, adapter)

	require.ErrorIs(t, err, ErrOrganizationExecutionConflict)
	require.Equal(t, 1, repo.reserveCalls)
	require.Zero(t, adapter.calls)
	require.Zero(t, repo.attemptCalls)
	require.Zero(t, repo.terminalCalls)
}

func TestOrganizationExecutionReservationCommandRejectsNonCanonicalSnapshotAndRawCostStillAllowsHighPrecision(t *testing.T) {
	service := newOrganizationExecutionTestService(&organizationExecutionRepositoryStub{})
	input := organizationExecutionInput()
	command, err := service.NewReservationCommand(input)
	require.NoError(t, err)
	command.CandidateSnapshotJSON = ` {"routes":[{"id":"route-a"}]}`
	require.ErrorIs(t, ValidateOrganizationExecutionReservationCommand(*command), ErrOrganizationExecutionInvalid)

	raw, err := NormalizeOrganizationRawActualCost("0.000000000000000000123400")
	require.NoError(t, err)
	require.Equal(t, "0.0000000000000000001234", raw)
	_, err = NormalizeOrganizationRawActualCost("1e-9")
	require.ErrorIs(t, err, ErrOrganizationExecutionInvalid)
}
