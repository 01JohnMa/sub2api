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

type organizationNonBillableRepositoryStub struct {
	*organizationExecutionRepositoryStub
	beginCalls    int
	attemptCalls  int
	pendingCalls  int
	terminalCalls int
	pendingCtxErr error
	begin         func(OrganizationNonBillableExecutionCommand) (*OrganizationNonBillableExecutionStartResult, error)
	attempt       func(OrganizationNonBillableAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error)
	pending       func(OrganizationNonBillablePendingAttemptCommand) error
	terminal      func(OrganizationNonBillableTerminalizationCommand) (*OrganizationNonBillableFinalExecutionEvidenceResult, error)
}

func (r *organizationNonBillableRepositoryStub) BeginNonBillableExecution(_ context.Context, command OrganizationNonBillableExecutionCommand) (*OrganizationNonBillableExecutionStartResult, error) {
	r.beginCalls++
	if r.begin == nil {
		return nil, errors.New("unexpected non-billable begin")
	}
	return r.begin(command)
}

func (r *organizationNonBillableRepositoryStub) RecordNonBillableAttemptEvidence(_ context.Context, command OrganizationNonBillableAttemptEvidenceCommand) (*OrganizationAttemptEvidenceResult, error) {
	r.attemptCalls++
	if r.attempt == nil {
		return nil, errors.New("unexpected non-billable attempt")
	}
	return r.attempt(command)
}

func (r *organizationNonBillableRepositoryStub) RecordNonBillablePendingAttempt(ctx context.Context, command OrganizationNonBillablePendingAttemptCommand) error {
	r.pendingCalls++
	r.pendingCtxErr = ctx.Err()
	if r.pending == nil {
		return errors.New("unexpected non-billable pending attempt")
	}
	return r.pending(command)
}

func (r *organizationNonBillableRepositoryStub) TerminalizeNonBillableExecution(_ context.Context, command OrganizationNonBillableTerminalizationCommand) (*OrganizationNonBillableFinalExecutionEvidenceResult, error) {
	r.terminalCalls++
	if r.terminal == nil {
		return nil, errors.New("unexpected non-billable terminalization")
	}
	return r.terminal(command)
}

func organizationNonBillableInput(family string) OrganizationNonBillableExecutionInput {
	shape := OrganizationRequestShapeHTTP
	if family == OrganizationEndpointFamilyTokenCounting {
		shape = OrganizationRequestShapeProbe
	}
	return OrganizationNonBillableExecutionInput{
		Attribution: OrganizationNonBillableExecutionTrustedAttribution{
			OrganizationID:            7,
			OrganizationStatus:        OrganizationStatusActive,
			UserID:                    11,
			UserOrganizationID:        7,
			APIKeyID:                  13,
			APIKeyUserID:              11,
			APIKeyOrganizationID:      7,
			AllocationID:              17,
			AllocationUserID:          11,
			AllocationOrganizationID:  7,
			AllocationStatus:          domain.OrganizationAllocationStatusActive,
			AllocationVersionSnapshot: 5,
		},
		EndpointFamily:     family,
		RequestShape:       shape,
		ClientRequestID:    "non-billable-client-request",
		CandidateSnapshot:  []byte(` { "route" : "local" } `),
		PricingSnapshot:    []byte(` { "cost" : 0 } `),
		BillingFingerprint: strings.Repeat("a", 64),
	}
}

func newOrganizationNonBillableTestService(repo OrganizationExecutionRepository) *OrganizationExecutionService {
	service := NewOrganizationExecutionService(repo)
	service.now = organizationExecutionClock
	return service
}

func TestNewOrganizationNonBillableExecutionCommandKeepsAllocationIdentityWithoutFundsContract(t *testing.T) {
	repo := &organizationNonBillableRepositoryStub{organizationExecutionRepositoryStub: &organizationExecutionRepositoryStub{}}
	service := newOrganizationNonBillableTestService(repo)

	command, err := service.NewNonBillableExecutionCommand(organizationNonBillableInput(OrganizationEndpointFamilyModels))
	require.NoError(t, err)
	require.True(t, uuid.Validate(command.ExecutionID) == nil)
	require.True(t, uuid.Validate(command.CorrelationID) == nil)
	require.NotEqual(t, command.ExecutionID, command.CorrelationID)
	require.Equal(t, `{"route":"local"}`, command.CandidateSnapshotJSON)
	require.Equal(t, `{"cost":0}`, command.PricingSnapshotJSON)
	require.Equal(t, organizationExecutionClock(), command.AcceptedAt)
	require.NoError(t, ValidateOrganizationNonBillableExecutionCommand(*command))
}

func TestOrganizationNonBillableExecutionRejectsUnsupportedBillableFamily(t *testing.T) {
	service := newOrganizationNonBillableTestService(&organizationNonBillableRepositoryStub{organizationExecutionRepositoryStub: &organizationExecutionRepositoryStub{}})
	_, err := service.NewNonBillableExecutionCommand(organizationNonBillableInput(OrganizationEndpointFamilyMessages))
	require.ErrorIs(t, err, ErrOrganizationExecutionInvalid)
}

func TestOrganizationNonBillableModelsFinalRequiresZeroTokenProof(t *testing.T) {
	command := organizationNonBillableCommandForTest(t, OrganizationEndpointFamilyModels)
	terminal := OrganizationNonBillableTerminalizationCommand{
		ExecutionID:     command.ExecutionID,
		CorrelationID:   command.CorrelationID,
		OrganizationID:  command.Attribution.OrganizationID,
		UserID:          command.Attribution.UserID,
		APIKeyID:        command.Attribution.APIKeyID,
		AllocationID:    command.Attribution.AllocationID,
		TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess,
		ZeroProof:       "local_models_response",
		OccurredAt:      organizationExecutionClock(),
	}
	require.NoError(t, ValidateOrganizationNonBillableTerminalizationCommand(command, terminal))

	result := organizationNonBillableFinalForTest(command, terminal)
	require.NoError(t, ValidateOrganizationNonBillableFinalExecutionEvidenceResult(command, terminal, result))

	result.InputTokens = 1
	require.ErrorIs(t, ValidateOrganizationNonBillableFinalExecutionEvidenceResult(command, terminal, result), ErrOrganizationExecutionInvariant)
}

func TestOrganizationNonBillableCountTokensFinalKeepsAttemptTokensWithoutConsumption(t *testing.T) {
	command := organizationNonBillableCommandForTest(t, OrganizationEndpointFamilyTokenCounting)
	attemptID := uuid.NewString()
	evidenceID := int64(91)
	terminal := OrganizationNonBillableTerminalizationCommand{
		ExecutionID:            command.ExecutionID,
		CorrelationID:          command.CorrelationID,
		OrganizationID:         command.Attribution.OrganizationID,
		UserID:                 command.Attribution.UserID,
		APIKeyID:               command.Attribution.APIKeyID,
		AllocationID:           command.Attribution.AllocationID,
		FinalAttemptID:         &attemptID,
		FinalAttemptEvidenceID: &evidenceID,
		TerminalOutcome:        OrganizationExecutionTerminalOutcomeSuccess,
		OccurredAt:             organizationExecutionClock(),
	}
	require.NoError(t, ValidateOrganizationNonBillableTerminalizationCommand(command, terminal))

	result := organizationNonBillableFinalForTest(command, terminal)
	result.InputTokens = 37
	result.OutputTokens = 2
	result.CacheCreationTokens = 3
	result.CacheReadTokens = 5
	result.IsCountTokensProbe = true
	require.NoError(t, ValidateOrganizationNonBillableFinalExecutionEvidenceResult(command, terminal, result))
	require.Nil(t, result.ConsumptionPostingID)

	postingID := int64(99)
	result.ConsumptionPostingID = &postingID
	require.ErrorIs(t, ValidateOrganizationNonBillableFinalExecutionEvidenceResult(command, terminal, result), ErrOrganizationExecutionInvariant)
}

func TestOrganizationNonBillableModelsMayUseAttemptWithoutBecomingProbe(t *testing.T) {
	command := organizationNonBillableCommandForTest(t, OrganizationEndpointFamilyModels)
	attemptID := uuid.NewString()
	evidenceID := int64(92)
	terminal := OrganizationNonBillableTerminalizationCommand{
		ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
		OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID, APIKeyID: command.Attribution.APIKeyID,
		AllocationID:   command.Attribution.AllocationID,
		FinalAttemptID: &attemptID, FinalAttemptEvidenceID: &evidenceID,
		TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess, OccurredAt: organizationExecutionClock(),
	}
	require.NoError(t, ValidateOrganizationNonBillableTerminalizationCommand(command, terminal))
	result := organizationNonBillableFinalForTest(command, terminal)
	result.IsCountTokensProbe = false
	require.NoError(t, ValidateOrganizationNonBillableFinalExecutionEvidenceResult(command, terminal, result))
}

func TestOrganizationNonBillableFinalRejectsDifferentOccurredAt(t *testing.T) {
	command := organizationNonBillableCommandForTest(t, OrganizationEndpointFamilyUsage)
	terminal := OrganizationNonBillableTerminalizationCommand{
		ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
		OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID, APIKeyID: command.Attribution.APIKeyID,
		AllocationID:    command.Attribution.AllocationID,
		TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess, ZeroProof: "local_usage_response", OccurredAt: organizationExecutionClock(),
	}
	result := organizationNonBillableFinalForTest(command, terminal)
	result.OccurredAt = result.OccurredAt.Add(time.Second)
	require.ErrorIs(t, ValidateOrganizationNonBillableFinalExecutionEvidenceResult(command, terminal, result), ErrOrganizationExecutionInvariant)
}

func TestOrganizationNonBillableRepositoryValidatorsRequireOneZeroCostAttempt(t *testing.T) {
	command := organizationNonBillableCommandForTest(t, OrganizationEndpointFamilyTokenCounting)
	attempt := OrganizationNonBillableAttemptEvidenceCommand{
		ExecutionID: command.ExecutionID, AttemptID: uuid.NewString(), CorrelationID: command.CorrelationID,
		OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID, APIKeyID: command.Attribution.APIKeyID,
		AllocationID:    command.Attribution.AllocationID,
		AttemptSequence: 2, AccountID: 23, RouteSnapshotJSON: command.CandidateSnapshotJSON, RouteSnapshotHash: command.CandidateSetHash,
		PricingSnapshotJSON: command.PricingSnapshotJSON, PricingSnapshotHash: command.PricingSnapshotHash,
		ProviderRequestID: "provider-request", ProviderCorrelationID: uuid.NewString(), Provenance: OrganizationAttemptEvidenceProvenanceAdapterDirect,
		TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess, ProvenanceHash: command.CandidateSetHash,
		EvidenceJSON: command.CandidateSnapshotJSON, EvidenceHash: command.CandidateSetHash, RawActualCost: "0",
		ProviderObservedAt: organizationExecutionClock(), IngestedAt: organizationExecutionClock(),
	}
	require.ErrorIs(t, ValidateOrganizationNonBillableAttemptEvidenceRepositoryCommand(attempt), ErrOrganizationExecutionInvalid)
	attempt.AttemptSequence = 1
	attempt.RawActualCost = "0.1"
	require.ErrorIs(t, ValidateOrganizationNonBillableAttemptEvidenceRepositoryCommand(attempt), ErrOrganizationExecutionInvalid)
}

func TestOrganizationNonBillableServiceDelegatesExactlyOnceCommands(t *testing.T) {
	command := organizationNonBillableCommandForTest(t, OrganizationEndpointFamilyUsage)
	terminal := OrganizationNonBillableTerminalizationCommand{
		ExecutionID:     command.ExecutionID,
		CorrelationID:   command.CorrelationID,
		OrganizationID:  command.Attribution.OrganizationID,
		UserID:          command.Attribution.UserID,
		APIKeyID:        command.Attribution.APIKeyID,
		AllocationID:    command.Attribution.AllocationID,
		TerminalOutcome: OrganizationExecutionTerminalOutcomeError,
		ZeroProof:       "local_usage_error",
		OccurredAt:      organizationExecutionClock(),
	}
	repo := &organizationNonBillableRepositoryStub{organizationExecutionRepositoryStub: &organizationExecutionRepositoryStub{}}
	repo.begin = func(got OrganizationNonBillableExecutionCommand) (*OrganizationNonBillableExecutionStartResult, error) {
		require.Equal(t, command, got)
		return &OrganizationNonBillableExecutionStartResult{
			ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
			OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID,
			APIKeyID: command.Attribution.APIKeyID, AllocationID: command.Attribution.AllocationID, State: OrganizationExecutionStateAccepted,
			Disposition: OrganizationNonBillableExecutionDispositionStarted,
		}, nil
	}
	repo.terminal = func(got OrganizationNonBillableTerminalizationCommand) (*OrganizationNonBillableFinalExecutionEvidenceResult, error) {
		require.Equal(t, terminal, got)
		return organizationNonBillableFinalForTest(command, terminal), nil
	}
	service := newOrganizationNonBillableTestService(repo)

	start, err := service.BeginNonBillableExecution(context.Background(), command)
	require.NoError(t, err)
	require.Equal(t, OrganizationNonBillableExecutionDispositionStarted, start.Disposition)
	final, err := service.TerminalizeNonBillableExecution(context.Background(), command, terminal)
	require.NoError(t, err)
	require.Equal(t, "local_usage_error", final.ZeroProof)
	require.Equal(t, 1, repo.beginCalls)
	require.Equal(t, 1, repo.terminalCalls)
}

func TestOrganizationNonBillableStaleAttributionReturnsAdmissionErrorOnInitialAndReplay(t *testing.T) {
	command := organizationNonBillableCommandForTest(t, OrganizationEndpointFamilyUsage)
	repo := &organizationNonBillableRepositoryStub{organizationExecutionRepositoryStub: &organizationExecutionRepositoryStub{}}
	repo.begin = func(got OrganizationNonBillableExecutionCommand) (*OrganizationNonBillableExecutionStartResult, error) {
		require.Equal(t, command, got)
		replayed := repo.beginCalls > 1
		terminal := OrganizationNonBillableTerminalizationCommand{
			ExecutionID: got.ExecutionID, CorrelationID: got.CorrelationID,
			OrganizationID: got.Attribution.OrganizationID, UserID: got.Attribution.UserID, APIKeyID: got.Attribution.APIKeyID,
			AllocationID: got.Attribution.AllocationID, TerminalOutcome: OrganizationExecutionTerminalOutcomeError,
			ZeroProof: organizationNonBillableZeroProofStaleAttribution, OccurredAt: got.AcceptedAt,
		}
		final := organizationNonBillableFinalForTest(got, terminal)
		final.Replayed = replayed
		result := &OrganizationNonBillableExecutionStartResult{
			ExecutionID: got.ExecutionID, CorrelationID: got.CorrelationID,
			OrganizationID: got.Attribution.OrganizationID, UserID: got.Attribution.UserID, APIKeyID: got.Attribution.APIKeyID,
			AllocationID: got.Attribution.AllocationID, State: OrganizationExecutionStateReleased,
			Disposition: OrganizationNonBillableExecutionDispositionFinalized, FinalEvidence: final, Replayed: replayed,
		}
		if replayed {
			return result, ErrOrganizationBillingUnavailable
		}
		return result, nil
	}
	service := newOrganizationNonBillableTestService(repo)

	start, err := service.BeginNonBillableExecution(context.Background(), command)
	require.ErrorIs(t, err, ErrOrganizationBillingUnavailable)
	require.NotNil(t, start)
	require.NotNil(t, start.FinalEvidence)
	require.False(t, start.Replayed)

	final, err := service.ExecuteNonBillable(context.Background(), command, nil)
	require.ErrorIs(t, err, ErrOrganizationBillingUnavailable)
	require.NotNil(t, final)
	require.True(t, final.Replayed)
	require.Equal(t, organizationNonBillableZeroProofStaleAttribution, final.ZeroProof)
	require.Equal(t, 2, repo.beginCalls)
	require.Zero(t, repo.attemptCalls)
	require.Zero(t, repo.pendingCalls)
	require.Zero(t, repo.terminalCalls)
}

func TestOrganizationNonBillableBusinessZeroProofRemainsSuccessfulFinal(t *testing.T) {
	command := organizationNonBillableCommandForTest(t, OrganizationEndpointFamilyTokenCounting)
	terminal := OrganizationNonBillableTerminalizationCommand{
		ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
		OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID, APIKeyID: command.Attribution.APIKeyID,
		AllocationID: command.Attribution.AllocationID, TerminalOutcome: OrganizationExecutionTerminalOutcomeError,
		BusinessLimited: true, ZeroProof: "openai_count_tokens_unsupported", OccurredAt: command.AcceptedAt,
	}
	final := organizationNonBillableFinalForTest(command, terminal)
	repo := &organizationNonBillableRepositoryStub{organizationExecutionRepositoryStub: &organizationExecutionRepositoryStub{}}
	repo.begin = func(got OrganizationNonBillableExecutionCommand) (*OrganizationNonBillableExecutionStartResult, error) {
		require.Equal(t, command, got)
		return &OrganizationNonBillableExecutionStartResult{
			ExecutionID: got.ExecutionID, CorrelationID: got.CorrelationID,
			OrganizationID: got.Attribution.OrganizationID, UserID: got.Attribution.UserID, APIKeyID: got.Attribution.APIKeyID,
			AllocationID: got.Attribution.AllocationID, State: OrganizationExecutionStateReleased,
			Disposition: OrganizationNonBillableExecutionDispositionFinalized, FinalEvidence: final,
		}, nil
	}
	service := newOrganizationNonBillableTestService(repo)

	result, err := service.ExecuteNonBillable(context.Background(), command, nil)
	require.NoError(t, err)
	require.Equal(t, final, result)
	require.Equal(t, 1, repo.beginCalls)
	require.Zero(t, repo.attemptCalls)
	require.Zero(t, repo.pendingCalls)
	require.Zero(t, repo.terminalCalls)
}

func TestExecuteNonBillableRejectsMismatchedProviderIdentityBeforeEvidence(t *testing.T) {
	command := organizationNonBillableCommandForTest(t, OrganizationEndpointFamilyTokenCounting)
	repo := organizationNonBillableStartedRepository(t, command)
	service := newOrganizationNonBillableTestService(repo)
	adapter := organizationNonBillableAdapterFunc(func(_ context.Context, request OrganizationNonBillableDispatchRequest) (*OrganizationNonBillableDispatchResult, error) {
		return &OrganizationNonBillableDispatchResult{
			Dispatched: true, TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess,
			Observation: &OrganizationExecutionAttemptObservation{
				AccountID: 23, RouteSnapshot: []byte(`{"account_id":23}`), Evidence: []byte(`{"input_tokens":9}`), RawActualCost: "0",
				ProviderRequestID: request.ProviderRequestID, ProviderCorrelationID: uuid.NewString(), ProviderObservedAt: organizationExecutionClock(), InputTokens: 9,
			},
		}, nil
	})

	_, err := service.ExecuteNonBillable(context.Background(), command, adapter)
	require.ErrorIs(t, err, ErrOrganizationExecutionInvariant)
	require.Zero(t, repo.attemptCalls)
	require.Zero(t, repo.terminalCalls)
}

func TestExecuteNonBillablePersistsPendingAttemptWithoutFinalizingUnknownOutcome(t *testing.T) {
	command := organizationNonBillableCommandForTest(t, OrganizationEndpointFamilyTokenCounting)
	repo := organizationNonBillableStartedRepository(t, command)
	repo.pending = func(got OrganizationNonBillablePendingAttemptCommand) error {
		require.Equal(t, command.ExecutionID, got.ExecutionID)
		require.Equal(t, 1, got.AttemptSequence)
		require.Equal(t, int64(23), got.AccountID)
		return nil
	}
	service := newOrganizationNonBillableTestService(repo)
	transportErr := errors.New("unknown post-dispatch outcome")
	adapter := organizationNonBillableAdapterFunc(func(_ context.Context, _ OrganizationNonBillableDispatchRequest) (*OrganizationNonBillableDispatchResult, error) {
		return &OrganizationNonBillableDispatchResult{
			Dispatched: true, PendingReconciliation: true, TerminalOutcome: OrganizationAttemptEvidenceTerminalOutcomeUnknown,
			Observation: &OrganizationExecutionAttemptObservation{AccountID: 23, RouteSnapshot: []byte(`{"account_id":23}`)},
		}, transportErr
	})

	final, err := service.ExecuteNonBillable(context.Background(), command, adapter)
	require.Nil(t, final)
	require.ErrorIs(t, err, ErrOrganizationExecutionPendingReconciliation)
	require.Equal(t, 1, repo.pendingCalls)
	require.Zero(t, repo.attemptCalls)
	require.Zero(t, repo.terminalCalls)
}

func TestExecuteNonBillablePersistsPendingWithDetachedContextAfterClientCancellation(t *testing.T) {
	command := organizationNonBillableCommandForTest(t, OrganizationEndpointFamilyTokenCounting)
	repo := organizationNonBillableStartedRepository(t, command)
	repo.pending = func(OrganizationNonBillablePendingAttemptCommand) error { return nil }
	service := newOrganizationNonBillableTestService(repo)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	adapter := organizationNonBillableAdapterFunc(func(_ context.Context, request OrganizationNonBillableDispatchRequest) (*OrganizationNonBillableDispatchResult, error) {
		cancelRequest()
		return &OrganizationNonBillableDispatchResult{
			Dispatched: true, PendingReconciliation: true, TerminalOutcome: OrganizationAttemptEvidenceTerminalOutcomeUnknown,
			Observation: &OrganizationExecutionAttemptObservation{
				AccountID: 23, RouteSnapshot: []byte(`{"account_id":23}`), ProviderRequestID: request.ProviderRequestID, ProviderCorrelationID: request.ProviderCorrelationID,
			},
		}, context.Canceled
	})

	final, err := service.ExecuteNonBillable(requestCtx, command, adapter)
	require.Nil(t, final)
	require.ErrorIs(t, err, ErrOrganizationExecutionPendingReconciliation)
	require.NoError(t, repo.pendingCtxErr)
	require.Equal(t, 1, repo.pendingCalls)
}

func organizationNonBillableCommandForTest(t *testing.T, family string) OrganizationNonBillableExecutionCommand {
	t.Helper()
	repo := &organizationNonBillableRepositoryStub{organizationExecutionRepositoryStub: &organizationExecutionRepositoryStub{}}
	command, err := newOrganizationNonBillableTestService(repo).NewNonBillableExecutionCommand(organizationNonBillableInput(family))
	require.NoError(t, err)
	return *command
}

func organizationNonBillableFinalForTest(command OrganizationNonBillableExecutionCommand, terminal OrganizationNonBillableTerminalizationCommand) *OrganizationNonBillableFinalExecutionEvidenceResult {
	return &OrganizationNonBillableFinalExecutionEvidenceResult{
		FinalEvidenceID:         71,
		ExecutionID:             command.ExecutionID,
		CorrelationID:           command.CorrelationID,
		OrganizationID:          command.Attribution.OrganizationID,
		UserID:                  command.Attribution.UserID,
		APIKeyID:                command.Attribution.APIKeyID,
		AllocationID:            command.Attribution.AllocationID,
		FinalAttemptID:          terminal.FinalAttemptID,
		FinalAttemptEvidenceID:  terminal.FinalAttemptEvidenceID,
		TerminalOutcome:         terminal.TerminalOutcome,
		BusinessLimited:         terminal.BusinessLimited,
		RawActualCost:           "0",
		OrganizationSettledCost: "0.00000000",
		ZeroProof:               terminal.ZeroProof,
		IsCountTokensProbe:      command.EndpointFamily == OrganizationEndpointFamilyTokenCounting,
		OccurredAt:              terminal.OccurredAt,
	}
}

func organizationNonBillableStartedRepository(t *testing.T, command OrganizationNonBillableExecutionCommand) *organizationNonBillableRepositoryStub {
	t.Helper()
	repo := &organizationNonBillableRepositoryStub{organizationExecutionRepositoryStub: &organizationExecutionRepositoryStub{}}
	repo.begin = func(got OrganizationNonBillableExecutionCommand) (*OrganizationNonBillableExecutionStartResult, error) {
		require.Equal(t, command, got)
		return &OrganizationNonBillableExecutionStartResult{
			ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
			OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID, APIKeyID: command.Attribution.APIKeyID,
			AllocationID: command.Attribution.AllocationID,
			State:        OrganizationExecutionStateAccepted, Disposition: OrganizationNonBillableExecutionDispositionStarted,
		}, nil
	}
	return repo
}

type organizationNonBillableAdapterFunc func(context.Context, OrganizationNonBillableDispatchRequest) (*OrganizationNonBillableDispatchResult, error)

func (f organizationNonBillableAdapterFunc) Dispatch(ctx context.Context, request OrganizationNonBillableDispatchRequest) (*OrganizationNonBillableDispatchResult, error) {
	return f(ctx, request)
}

var _ OrganizationNonBillableExecutionRepository = (*organizationNonBillableRepositoryStub)(nil)
var _ OrganizationNonBillableExecutionAdapter = organizationNonBillableAdapterFunc(nil)
