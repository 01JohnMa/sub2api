package handler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

// organizationResponsesWSTurnCoordinator deliberately owns only the billing
// lifetime of a response.create. The upstream WebSocket stays connected for
// protocol continuity, but no authorization, platform admission, reservation,
// or candidate snapshot is shared between turns.
type organizationResponsesWSTurnCoordinator struct {
	handler         *OpenAIGatewayHandler
	rawAPIKey       string
	activeAccount   *service.Account
	requestLog      *zap.Logger
	baseContext     context.Context
	requestPath     string
	clientRequestID string
	mu              sync.Mutex
	turns           map[int]*organizationResponsesWSTurn
	accountMu       sync.Mutex
	accountChanged  chan struct{}
	retryRequests   chan organizationResponsesWSRetryRequest
	turnFinished    chan int
}

type organizationResponsesWSTurn struct {
	readyDone     chan struct{}
	readyErr      error
	readyOnce     sync.Once
	done          chan struct{}
	attemptMu     sync.Mutex
	attemptResult chan organizationResponsesWSTurnOutcome
	retryMu       sync.Mutex
	retryReady    chan error
	executionErr  error
	payload       []byte
	originalModel string
	lastAccountID int64
}

func (s *organizationResponsesWSTurn) startAttempt() <-chan organizationResponsesWSTurnOutcome {
	if s == nil {
		return nil
	}
	s.attemptMu.Lock()
	defer s.attemptMu.Unlock()
	s.attemptResult = make(chan organizationResponsesWSTurnOutcome, 1)
	return s.attemptResult
}

func (s *organizationResponsesWSTurn) completeAttempt(outcome organizationResponsesWSTurnOutcome) {
	if s == nil {
		return
	}
	s.attemptMu.Lock()
	result := s.attemptResult
	s.attemptResult = nil
	s.attemptMu.Unlock()
	if result != nil {
		result <- outcome
	}
}

func (s *organizationResponsesWSTurn) prepareRetry() <-chan error {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	s.retryReady = make(chan error, 1)
	return s.retryReady
}

func (s *organizationResponsesWSTurn) signalRetryReady(err error) {
	s.retryMu.Lock()
	ready := s.retryReady
	s.retryReady = nil
	s.retryMu.Unlock()
	if ready != nil {
		ready <- err
	}
}

func (s *organizationResponsesWSTurn) signalReady(err error) {
	if s == nil {
		return
	}
	s.readyOnce.Do(func() {
		s.readyErr = err
		close(s.readyDone)
	})
}

type organizationResponsesWSTurnOutcome struct {
	result *service.OpenAIForwardResult
	err    error
}

type organizationResponsesWSRetryRequest struct {
	turn          int
	payload       []byte
	originalModel string
	nextAccountID int64
	ready         <-chan error
}

func newOrganizationResponsesWSTurnCoordinator(
	h *OpenAIGatewayHandler,
	c *gin.Context,
	rawAPIKey string,
	account *service.Account,
	requestLog *zap.Logger,
) *organizationResponsesWSTurnCoordinator {
	baseContext := context.Background()
	requestPath := ""
	clientRequestID := ""
	if c != nil && c.Request != nil {
		baseContext = c.Request.Context()
		if c.Request.URL != nil {
			requestPath = c.Request.URL.Path
		}
		clientRequestID = organizationResponsesClientRequestID(c)
	}
	return &organizationResponsesWSTurnCoordinator{
		handler: h, rawAPIKey: strings.TrimSpace(rawAPIKey), activeAccount: account,
		requestLog: requestLog, baseContext: baseContext, requestPath: requestPath, clientRequestID: clientRequestID,
		turns: make(map[int]*organizationResponsesWSTurn), accountChanged: make(chan struct{}),
		retryRequests: make(chan organizationResponsesWSRetryRequest, 8), turnFinished: make(chan int, 8),
	}
}

func (o *organizationResponsesWSTurnCoordinator) SetActiveAccount(account *service.Account) {
	if o == nil || account == nil {
		return
	}
	o.accountMu.Lock()
	o.activeAccount = account
	close(o.accountChanged)
	o.accountChanged = make(chan struct{})
	o.accountMu.Unlock()
}

func (o *organizationResponsesWSTurnCoordinator) activeAccountID() int64 {
	if o == nil {
		return 0
	}
	o.accountMu.Lock()
	defer o.accountMu.Unlock()
	if o.activeAccount == nil {
		return 0
	}
	return o.activeAccount.ID
}

func (o *organizationResponsesWSTurnCoordinator) waitForActiveAccount(ctx context.Context, previousAccountID int64) (*service.Account, error) {
	for {
		o.accountMu.Lock()
		account := o.activeAccount
		changed := o.accountChanged
		o.accountMu.Unlock()
		if account != nil && account.ID > 0 && account.ID != previousAccountID {
			return account, nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (o *organizationResponsesWSTurnCoordinator) WaitRetry(ctx context.Context) (organizationResponsesWSRetryRequest, error) {
	if o == nil {
		return organizationResponsesWSRetryRequest{}, service.ErrOrganizationResponsesWSFailoverUnsupported
	}
	for {
		select {
		case <-o.turnFinished:
		default:
			goto drained
		}
	}
drained:
	o.mu.Lock()
	unfinished := 0
	for _, state := range o.turns {
		select {
		case <-state.done:
		default:
			unfinished++
		}
	}
	o.mu.Unlock()
	if unfinished != 1 {
		return organizationResponsesWSRetryRequest{}, service.ErrOrganizationResponsesWSFailoverUnsupported
	}
	select {
	case retry := <-o.retryRequests:
		return retry, nil
	case <-o.turnFinished:
		return organizationResponsesWSRetryRequest{}, service.ErrOrganizationResponsesWSFailoverUnsupported
	case <-ctx.Done():
		return organizationResponsesWSRetryRequest{}, ctx.Err()
	}
}

func (o *organizationResponsesWSTurnCoordinator) BeforeRequest(ctx context.Context, turn int, payload []byte, originalModel string) error {
	if o == nil || o.handler == nil || turn <= 0 {
		return service.NewOpenAIWSClientCloseError(websocket.StatusInternalError, "organization websocket billing is unavailable", service.ErrOrganizationBillingUnavailable)
	}
	o.mu.Lock()
	if existing := o.turns[turn]; existing != nil {
		o.mu.Unlock()
		select {
		case <-existing.readyDone:
			return existing.readyErr
		case <-existing.done:
			return existing.executionErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	state := &organizationResponsesWSTurn{
		readyDone: make(chan struct{}), done: make(chan struct{}),
		payload: append([]byte(nil), payload...), originalModel: strings.TrimSpace(originalModel),
	}
	o.turns[turn] = state
	o.mu.Unlock()

	go o.executeTurn(ctx, turn, payload, originalModel, state)
	select {
	case <-state.readyDone:
		return state.readyErr
	case <-state.done:
		return state.executionErr
	case <-ctx.Done():
		o.complete(state, organizationResponsesWSTurnOutcome{err: ctx.Err()})
		return ctx.Err()
	}
}

func (o *organizationResponsesWSTurnCoordinator) executeTurn(ctx context.Context, turn int, payload []byte, originalModel string, state *organizationResponsesWSTurn) {
	defer func() {
		close(state.done)
		select {
		case o.turnFinished <- turn:
		default:
		}
	}()
	if o.baseContext != nil {
		ctx = o.baseContext
	}
	freshAPIKey, err := o.authoritativeAPIKey(ctx)
	if err != nil {
		state.executionErr = err
		state.signalReady(wsOrganizationTurnCloseError(err))
		return
	}
	planningPayload, err := organizationWSPlanningPayload(payload, originalModel)
	if err != nil {
		state.executionErr = err
		state.signalReady(wsOrganizationTurnCloseError(err))
		return
	}
	plan, err := o.handler.gatewayService.PlanOrganizationResponsesCandidates(ctx, service.OrganizationResponsesCandidatePlanInput{
		APIKey: freshAPIKey, Body: planningPayload, RequestShape: service.OrganizationRequestShapeWebSocketResponseCreate,
		RequestPath: o.requestPath, GroupID: freshAPIKey.GroupID, MaximumAttempts: o.handler.maxAccountSwitches + 1,
		PreferredAccountID: o.activeAccountID(),
	})
	if err != nil {
		state.executionErr = err
		state.signalReady(wsOrganizationTurnCloseError(err))
		return
	}
	reservationInput, err := plan.NewExecutionReservationInput(freshAPIKey, o.clientRequestID)
	if err != nil {
		state.executionErr = err
		state.signalReady(wsOrganizationTurnCloseError(err))
		return
	}
	command, err := o.handler.organizationExecution.NewReservationCommand(reservationInput)
	if err != nil {
		state.executionErr = err
		state.signalReady(wsOrganizationTurnCloseError(err))
		return
	}
	adapter := &organizationResponsesWSTurnAdapter{
		coordinator: o, turn: turn, state: state, apiKey: freshAPIKey, plan: plan,
	}
	defer adapter.releaseParentAdmission()
	run, err := o.handler.organizationExecution.ExecuteMultiAttempt(ctx, *command, plan.MaximumAttempts, adapter)
	if err != nil && !errors.Is(err, service.ErrOrganizationExecutionPendingReconciliation) {
		state.executionErr = err
		state.signalReady(wsOrganizationTurnCloseError(err))
		return
	}
	if run != nil && run.Final != nil {
		o.handler.recordOrganizationResponsesRateLimitUsage(freshAPIKey, run.Final)
	}
}

func (o *organizationResponsesWSTurnCoordinator) AfterTurn(turn int, result *service.OpenAIForwardResult, turnErr error) {
	if o == nil {
		return
	}
	o.mu.Lock()
	state := o.turns[turn]
	o.mu.Unlock()
	if state == nil {
		return
	}
	o.complete(state, organizationResponsesWSTurnOutcome{result: result, err: turnErr})
	var failoverErr *service.UpstreamFailoverError
	if errors.As(turnErr, &failoverErr) {
		return
	}
	select {
	case <-state.done:
	case <-time.After(10 * time.Second):
	}
	o.removeTurn(turn, state)
}

func (o *organizationResponsesWSTurnCoordinator) Close(proxyErr error) {
	if o == nil {
		return
	}
	o.mu.Lock()
	states := make([]*organizationResponsesWSTurn, 0, len(o.turns))
	for _, state := range o.turns {
		states = append(states, state)
	}
	o.mu.Unlock()
	for _, state := range states {
		o.complete(state, organizationResponsesWSTurnOutcome{err: proxyErr})
		select {
		case <-state.done:
		case <-time.After(10 * time.Second):
		}
	}
	o.mu.Lock()
	for turn, state := range o.turns {
		if state != nil {
			delete(o.turns, turn)
		}
	}
	o.mu.Unlock()
}

func (o *organizationResponsesWSTurnCoordinator) removeTurn(turn int, state *organizationResponsesWSTurn) {
	if o == nil {
		return
	}
	o.mu.Lock()
	if current := o.turns[turn]; current == state {
		delete(o.turns, turn)
	}
	o.mu.Unlock()
}

func (o *organizationResponsesWSTurnCoordinator) complete(state *organizationResponsesWSTurn, outcome organizationResponsesWSTurnOutcome) {
	if state == nil {
		return
	}
	state.completeAttempt(outcome)
}

func (o *organizationResponsesWSTurnCoordinator) requestRetry(turn int, state *organizationResponsesWSTurn, nextAccountID int64) {
	if o == nil || state == nil {
		return
	}
	retry := organizationResponsesWSRetryRequest{
		turn: turn, payload: append([]byte(nil), state.payload...), originalModel: state.originalModel,
		nextAccountID: nextAccountID, ready: state.prepareRetry(),
	}
	select {
	case o.retryRequests <- retry:
	default:
		state.signalRetryReady(service.ErrOrganizationResponsesWSFailoverUnsupported)
	}
}

func (o *organizationResponsesWSTurnCoordinator) authoritativeAPIKey(ctx context.Context) (*service.APIKey, error) {
	if o.handler == nil || o.handler.apiKeyService == nil || o.handler.gatewayService == nil || o.handler.organizationExecution == nil || o.handler.billingCacheService == nil || o.handler.cfg == nil || o.handler.cfg.RunMode == config.RunModeSimple {
		return nil, service.ErrOrganizationBillingUnavailable
	}
	apiKey, err := o.handler.apiKeyService.GetByKeyAuthoritative(ctx, o.rawAPIKey)
	if err != nil {
		return nil, service.ErrOrganizationBillingUnavailable
	}
	if !isCurrentOrganizationResponsesWSKey(apiKey) {
		return nil, service.ErrOrganizationBillingUnavailable
	}
	if err := service.NewOrganizationSpendingCapabilityRegistry(o.handler.cfg).Require(service.OrganizationEndpointFamilyResponses, service.OrganizationRequestShapeWebSocketResponseCreate); err != nil {
		return nil, err
	}
	return apiKey, nil
}

func isCurrentOrganizationResponsesWSKey(apiKey *service.APIKey) bool {
	if apiKey == nil || !apiKey.IsActive() || apiKey.IsExpired() || apiKey.IsQuotaExhausted() || apiKey.User == nil || !apiKey.User.IsActive() || apiKey.Group == nil || !apiKey.Group.IsActive() || !apiKey.User.CanBindGroup(apiKey.Group.ID, apiKey.Group.IsExclusive) || apiKey.Group.Platform != service.PlatformOpenAI || apiKey.OrganizationID == nil || apiKey.User.OrganizationID == nil || apiKey.User.OrganizationRole == nil || apiKey.OrganizationAuthorization == nil {
		return false
	}
	auth := apiKey.OrganizationAuthorization
	if auth.APIKeyID != apiKey.ID || auth.UserID != apiKey.UserID || auth.OrganizationID != *apiKey.OrganizationID || auth.OrganizationID != *apiKey.User.OrganizationID || auth.AllocationID <= 0 || auth.AllocationUserID != apiKey.UserID || auth.AllocationOrganizationID != auth.OrganizationID || auth.OrganizationStatus != service.OrganizationStatusActive || auth.AllocationStatus != "active" {
		return false
	}
	return *apiKey.User.OrganizationRole == service.OrganizationRoleAdmin || *apiKey.User.OrganizationRole == service.OrganizationRoleMember
}

func organizationWSPlanningPayload(payload []byte, originalModel string) ([]byte, error) {
	if !gjson.ValidBytes(payload) {
		return nil, service.ErrOrganizationResponsesUnsupportedRequest
	}
	if strings.TrimSpace(gjson.GetBytes(payload, "type").String()) != "response.create" {
		return nil, service.ErrOrganizationResponsesUnsupportedRequest
	}
	if strings.TrimSpace(gjson.GetBytes(payload, "model").String()) != "" {
		return payload, nil
	}
	model := strings.TrimSpace(originalModel)
	if model == "" {
		return nil, service.ErrOrganizationResponsesUnsupportedRequest
	}
	return sjson.SetBytes(payload, "model", model)
}

func wsOrganizationTurnCloseError(err error) error {
	if err == nil {
		return nil
	}
	return service.NewOpenAIWSClientCloseError(websocket.StatusPolicyViolation, "organization websocket turn rejected", err)
}

type organizationResponsesWSTurnAdapter struct {
	coordinator        *organizationResponsesWSTurnCoordinator
	turn               int
	state              *organizationResponsesWSTurn
	apiKey             *service.APIKey
	plan               *service.OrganizationResponsesCandidatePlan
	admissionOnce      sync.Once
	admissionErr       error
	admissionZeroProof string
	userRelease        func()
	userReleaseOnce    sync.Once
}

func (a *organizationResponsesWSTurnAdapter) Dispatch(ctx context.Context, request service.OrganizationExecutionDispatchRequest) (*service.OrganizationExecutionAttemptResult, error) {
	if a == nil || a.coordinator == nil || a.coordinator.handler == nil || a.plan == nil {
		return nil, service.ErrOrganizationExecutionAdapterUnavailable
	}
	candidate, err := a.currentCandidate(ctx, request.AttemptSequence)
	if err != nil {
		a.signalDispatchReady(request.AttemptSequence, wsOrganizationTurnCloseError(err))
		return &service.OrganizationExecutionAttemptResult{TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: "candidate_snapshot_conflict"}, nil
	}
	if zeroProof, err := a.ensureParentAdmission(ctx); err != nil {
		a.signalDispatchReady(request.AttemptSequence, wsOrganizationTurnCloseError(err))
		return &service.OrganizationExecutionAttemptResult{TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: zeroProof}, nil
	}
	accountRelease, accountAcquired, err := a.coordinator.handler.concurrencyHelper.TryAcquireAccountSlot(ctx, candidate.AccountID, candidate.Account.Concurrency)
	if err != nil || !accountAcquired {
		if err == nil {
			err = service.ErrOrganizationBillingUnavailable
		}
		a.signalDispatchReady(request.AttemptSequence, wsOrganizationTurnCloseError(err))
		return &service.OrganizationExecutionAttemptResult{TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: "account_concurrency_unavailable"}, nil
	}
	defer accountRelease()
	attemptResult := a.state.startAttempt()
	a.signalDispatchReady(request.AttemptSequence, nil)
	select {
	case outcome := <-attemptResult:
		httpAdapter := &organizationResponsesHTTPAdapter{handler: a.coordinator.handler, apiKey: a.apiKey, requestedModel: a.plan.Policy.RequestedModel, reqLog: a.coordinator.requestLog}
		result, resultErr := httpAdapter.resultForForward(request, candidate, outcome.result, outcome.err, -1)
		if result != nil && result.Retryable && request.AttemptSequence < a.plan.MaximumAttempts {
			nextCandidateIndex := request.AttemptSequence
			if nextCandidateIndex >= 0 && nextCandidateIndex < len(a.plan.Candidates) {
				a.coordinator.requestRetry(a.turn, a.state, a.plan.Candidates[nextCandidateIndex].AccountID)
			}
		}
		return result, resultErr
	case <-ctx.Done():
		return &service.OrganizationExecutionAttemptResult{Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, Observation: (&organizationResponsesHTTPAdapter{handler: a.coordinator.handler, apiKey: a.apiKey, requestedModel: a.plan.Policy.RequestedModel, reqLog: a.coordinator.requestLog}).pendingObservation(request, candidate)}, ctx.Err()
	}
}

func (a *organizationResponsesWSTurnAdapter) ensureParentAdmission(ctx context.Context) (string, error) {
	if a == nil || a.coordinator == nil || a.coordinator.handler == nil || a.apiKey == nil || a.apiKey.User == nil {
		return "organization_platform_admission_rejected", service.ErrOrganizationBillingUnavailable
	}
	a.admissionOnce.Do(func() {
		handler := a.coordinator.handler
		if handler.billingCacheService == nil || handler.concurrencyHelper == nil {
			a.admissionZeroProof = "organization_platform_admission_rejected"
			a.admissionErr = service.ErrOrganizationBillingUnavailable
			return
		}
		if err := handler.billingCacheService.CheckOrganizationPlatformEligibility(ctx, a.apiKey.User, a.apiKey, a.apiKey.Group, service.QuotaPlatform(ctx, a.apiKey)); err != nil {
			a.admissionZeroProof = "organization_platform_admission_rejected"
			a.admissionErr = err
			return
		}
		release, acquired, err := handler.concurrencyHelper.TryAcquireUserSlot(ctx, a.apiKey.User.ID, a.apiKey.User.Concurrency)
		if err != nil || !acquired {
			if err == nil {
				err = service.ErrOrganizationBillingUnavailable
			}
			a.admissionZeroProof = "user_concurrency_unavailable"
			a.admissionErr = err
			return
		}
		a.userRelease = release
	})
	return a.admissionZeroProof, a.admissionErr
}

func (a *organizationResponsesWSTurnAdapter) releaseParentAdmission() {
	if a == nil {
		return
	}
	a.userReleaseOnce.Do(func() {
		if a.userRelease != nil {
			a.userRelease()
			a.userRelease = nil
		}
	})
}

func (a *organizationResponsesWSTurnAdapter) signalDispatchReady(sequence int, err error) {
	if a == nil || a.state == nil {
		return
	}
	if sequence <= 1 {
		a.state.signalReady(err)
		return
	}
	a.state.signalRetryReady(err)
}

func (a *organizationResponsesWSTurnAdapter) currentCandidate(ctx context.Context, attemptSequence int) (service.OrganizationResponsesCandidate, error) {
	candidateIndex := attemptSequence - 1
	if candidateIndex < 0 || candidateIndex >= len(a.plan.Candidates) {
		return service.OrganizationResponsesCandidate{}, service.ErrOrganizationResponsesCandidateConflict
	}
	expectedCandidate := a.plan.Candidates[candidateIndex]
	account, err := a.coordinator.waitForActiveAccount(ctx, a.state.lastAccountID)
	if err != nil {
		return service.OrganizationResponsesCandidate{}, err
	}
	if expectedCandidate.AccountID != account.ID {
		return service.OrganizationResponsesCandidate{}, service.ErrOrganizationResponsesCandidateConflict
	}
	validated, err := a.coordinator.handler.gatewayService.RevalidateOrganizationResponsesCandidate(ctx, a.apiKey, expectedCandidate)
	if err != nil {
		return service.OrganizationResponsesCandidate{}, err
	}
	a.state.lastAccountID = account.ID
	return *validated, nil
}
