package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type recoveryEvidenceRepositoryStub struct {
	command OrganizationNonBillableRecoveryEvidenceCommand
	calls   int
}

func (*recoveryEvidenceRepositoryStub) RecordPendingAttempt(context.Context, OrganizationPendingAttemptCommand) error {
	return nil
}

func (*recoveryEvidenceRepositoryStub) ReconcilePendingExecution(context.Context, OrganizationPendingReconciliationCommand) (*OrganizationFinalExecutionEvidenceResult, error) {
	return nil, nil
}

func (*recoveryEvidenceRepositoryStub) ListPendingReconciliations(context.Context, int64, OrganizationPendingReconciliationListParams) ([]OrganizationPendingReconciliation, *OrganizationPendingReconciliationPage, error) {
	return nil, nil, nil
}

func (*recoveryEvidenceRepositoryStub) GetPendingReconciliation(context.Context, int64, string) (*OrganizationPendingReconciliation, error) {
	return nil, nil
}

func (*recoveryEvidenceRepositoryStub) RedriveAttemptAccountEffect(context.Context, string) (*OrganizationAttemptAccountEffectResult, error) {
	return nil, nil
}

func (*recoveryEvidenceRepositoryStub) RedrivePendingAttemptAccountEffects(context.Context, int) ([]OrganizationAttemptAccountEffectResult, error) {
	return nil, nil
}

func (*recoveryEvidenceRepositoryStub) RedriveExecutionOutbox(context.Context, int64) (*OrganizationExecutionOutboxApplyResult, error) {
	return nil, nil
}

func (s *recoveryEvidenceRepositoryStub) RecordNonBillableRecoveryEvidence(_ context.Context, command OrganizationNonBillableRecoveryEvidenceCommand) (*OrganizationAttemptEvidenceResult, error) {
	s.calls++
	s.command = command
	return &OrganizationAttemptEvidenceResult{AttemptEvidenceID: 19, ExecutionID: command.ExecutionID, AttemptID: command.AttemptID}, nil
}

func recoveryEvidenceActor() OrganizationReconciliationActor {
	return OrganizationReconciliationActor{
		UserID: 42, DisplayName: "platform-admin-42", PlatformRole: domain.RoleAdmin,
		AuthorizationScope: domain.OrganizationFundingAuthorizationPlatformAdminJWT, Active: true,
	}
}

func TestOrganizationExecutionRecoveryServiceRecordsScopedNonBillableEvidence(t *testing.T) {
	repository := &recoveryEvidenceRepositoryStub{}
	recovery := NewOrganizationExecutionRecoveryService(repository)
	now := time.Date(2026, time.July, 31, 3, 0, 0, 0, time.UTC)
	recovery.now = func() time.Time { return now }
	executionID := uuid.NewString()
	attemptID := uuid.NewString()
	observedAt := now.Add(-time.Second)

	result, err := recovery.RecordNonBillableRecoveryEvidenceInOrganization(context.Background(), recoveryEvidenceActor(), 7, executionID, OrganizationNonBillableRecoveryEvidenceInput{
		AttemptID: attemptID, TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess,
		Evidence:    json.RawMessage(`{"provider":"query","usage":{"input":4}}`),
		InputTokens: 4, OutputTokens: 2, CacheCreationTokens: 1, CacheReadTokens: 0,
		ProviderObservedAt: observedAt,
	})
	require.NoError(t, err)
	require.Equal(t, int64(19), result.AttemptEvidenceID)
	require.Equal(t, 1, repository.calls)
	require.Equal(t, executionID, repository.command.ExecutionID)
	require.Equal(t, attemptID, repository.command.AttemptID)
	require.Equal(t, int64(7), repository.command.OrganizationID)
	require.Equal(t, `{"provider":"query","usage":{"input":4}}`, repository.command.EvidenceJSON)
	require.NotEmpty(t, repository.command.EvidenceHash)
	require.Equal(t, now, repository.command.IngestedAt)
	require.Equal(t, observedAt, repository.command.ProviderObservedAt)
}

func TestOrganizationExecutionRecoveryServiceEvidenceRequiresPlatformJWT(t *testing.T) {
	repository := &recoveryEvidenceRepositoryStub{}
	recovery := NewOrganizationExecutionRecoveryService(repository)
	input := OrganizationNonBillableRecoveryEvidenceInput{
		AttemptID: uuid.NewString(), TerminalOutcome: OrganizationExecutionTerminalOutcomeError,
		Evidence: json.RawMessage(`{"status":"error"}`), ProviderObservedAt: time.Now().UTC(),
	}
	actor := recoveryEvidenceActor()
	actor.AuthorizationScope = "admin_api_key"

	_, err := recovery.RecordNonBillableRecoveryEvidenceInOrganization(context.Background(), actor, 7, uuid.NewString(), input)
	require.ErrorIs(t, err, ErrOrganizationExecutionReconciliationForbidden)
	require.Zero(t, repository.calls)
}

func TestOrganizationExecutionRecoveryServiceEvidenceRejectsInvalidInputBeforeRepository(t *testing.T) {
	repository := &recoveryEvidenceRepositoryStub{}
	recovery := NewOrganizationExecutionRecoveryService(repository)
	valid := OrganizationNonBillableRecoveryEvidenceInput{
		AttemptID: uuid.NewString(), TerminalOutcome: OrganizationExecutionTerminalOutcomeSuccess,
		Evidence: json.RawMessage(`{"status":"ok"}`), ProviderObservedAt: time.Now().UTC(),
	}
	cases := []struct {
		name   string
		mutate func(*OrganizationNonBillableRecoveryEvidenceInput)
	}{
		{name: "organization", mutate: func(input *OrganizationNonBillableRecoveryEvidenceInput) {}},
		{name: "attempt", mutate: func(input *OrganizationNonBillableRecoveryEvidenceInput) { input.AttemptID = "bad" }},
		{name: "outcome", mutate: func(input *OrganizationNonBillableRecoveryEvidenceInput) {
			input.TerminalOutcome = OrganizationAttemptEvidenceTerminalOutcomeUnknown
		}},
		{name: "tokens", mutate: func(input *OrganizationNonBillableRecoveryEvidenceInput) { input.InputTokens = -1 }},
		{name: "observed", mutate: func(input *OrganizationNonBillableRecoveryEvidenceInput) { input.ProviderObservedAt = time.Time{} }},
		{name: "evidence", mutate: func(input *OrganizationNonBillableRecoveryEvidenceInput) { input.Evidence = json.RawMessage(`null`) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			organizationID := int64(7)
			if tc.name == "organization" {
				organizationID = 0
			}
			tc.mutate(&input)
			_, err := recovery.RecordNonBillableRecoveryEvidenceInOrganization(context.Background(), recoveryEvidenceActor(), organizationID, uuid.NewString(), input)
			require.ErrorIs(t, err, ErrOrganizationExecutionInvalid)
		})
	}
	require.Zero(t, repository.calls)
}
