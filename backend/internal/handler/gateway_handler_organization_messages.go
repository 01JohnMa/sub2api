package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *GatewayHandler) handleOrganizationGatewayMessages(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	body []byte,
	requestedModel string,
	stream bool,
	reqLog *zap.Logger,
	streamStarted *bool,
) {
	requestShape := service.OrganizationRequestShapeHTTP
	if stream {
		requestShape = service.OrganizationRequestShapeStreaming
	}
	setOpsRequestContext(c, requestedModel, stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(stream, false)))
	if h == nil || h.organizationExecution == nil {
		h.writeOrganizationGatewayMessagesError(c, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	if h.gatewayService == nil || apiKey == nil || apiKey.Group == nil {
		if !h.recordOrganizationGatewayMessagesPreDispatchRejection(c, apiKey, body, requestShape, service.ErrOrganizationBillingUnavailable, streamStarted) {
			return
		}
		h.writeOrganizationGatewayMessagesError(c, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolAnthropicMessages, requestedModel, body); decision != nil && decision.Blocked {
		rejection := infraerrors.New(contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		if !h.recordOrganizationGatewayMessagesPreDispatchRejection(c, apiKey, body, requestShape, rejection, streamStarted) {
			return
		}
		h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		return
	}
	if err := h.validateOrganizationGatewayMessagesPreDispatch(apiKey, requestShape); err != nil {
		reqLog.Info("gateway.organization_messages_admission_rejected", zap.Error(err))
		if !h.recordOrganizationGatewayMessagesPreDispatchRejection(c, apiKey, body, requestShape, err, streamStarted) {
			return
		}
		h.writeOrganizationGatewayMessagesError(c, err, streamStarted)
		return
	}
	plan, err := h.gatewayService.PlanOrganizationGatewayMessagesCandidate(c.Request.Context(), apiKey, body, requestShape)
	if err != nil {
		reqLog.Info("gateway.organization_messages_plan_rejected", zap.Error(err))
		if !h.recordOrganizationGatewayMessagesPreDispatchRejection(c, apiKey, body, requestShape, err, streamStarted) {
			return
		}
		h.writeOrganizationGatewayMessagesError(c, err, streamStarted)
		return
	}
	reservationInput, err := plan.NewExecutionReservationInput(apiKey, organizationGatewayChatClientRequestID(c))
	if err != nil {
		h.writeOrganizationGatewayMessagesError(c, err, streamStarted)
		return
	}
	command, err := h.organizationExecution.NewReservationCommand(reservationInput)
	if err != nil {
		h.writeOrganizationGatewayMessagesError(c, err, streamStarted)
		return
	}
	adapter := &organizationGatewayMessagesAdapter{
		handler: h, c: c, apiKey: apiKey, subject: subject, plan: plan,
		requestedModel: requestedModel, reqLog: reqLog, streamStarted: streamStarted,
	}
	defer adapter.releaseUserSlot()
	run, err := h.organizationExecution.ExecuteMultiAttempt(c.Request.Context(), *command, plan.MaximumAttempts, adapter)
	if err != nil {
		reqLog.Error("gateway.organization_messages_execution_failed", zap.Error(err))
		if !c.Writer.Written() {
			h.writeOrganizationGatewayMessagesError(c, err, streamStarted)
		}
		return
	}
	if run == nil || run.PendingReconciliation || run.Final == nil {
		if !c.Writer.Written() {
			if run != nil && run.PendingReconciliation {
				h.writeOrganizationGatewayMessagesError(c, service.ErrOrganizationExecutionPendingReconciliation, streamStarted)
			} else {
				h.writeOrganizationGatewayMessagesError(c, service.ErrOrganizationExecutionInvariant, streamStarted)
			}
		}
		return
	}
	h.recordOrganizationGatewayChatRateLimitUsage(apiKey, run.Final)
	if run.Final.TerminalOutcome != service.OrganizationExecutionTerminalOutcomeError || c.Writer.Written() {
		return
	}
	switch adapter.zeroProof {
	case "candidate_snapshot_conflict":
		h.writeOrganizationGatewayMessagesError(c, service.ErrOrganizationMessagesCandidateConflict, streamStarted)
	case "user_concurrency_unavailable":
		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "User concurrency limit reached")
	case "account_concurrency_unavailable":
		h.errorResponse(c, http.StatusServiceUnavailable, "rate_limit_error", "No frozen account has available concurrency")
	case "organization_platform_admission_rejected":
		h.writeOrganizationGatewayMessagesError(c, adapter.lastErr, streamStarted)
	default:
		if adapter.lastFailover != nil {
			h.handleFailoverExhausted(c, adapter.lastFailover, apiKey.Group.Platform, streamStarted != nil && *streamStarted)
			return
		}
		h.ensureForwardErrorResponse(c, streamStarted != nil && *streamStarted)
	}
}

func (h *GatewayHandler) validateOrganizationGatewayMessagesPreDispatch(apiKey *service.APIKey, requestShape string) error {
	if h == nil || h.cfg == nil || h.cfg.RunMode == config.RunModeSimple || apiKey == nil || apiKey.Group == nil {
		return service.ErrOrganizationBillingUnavailable
	}
	if apiKey.Group.Platform != service.PlatformAnthropic && apiKey.Group.Platform != service.PlatformGemini {
		return service.ErrOrganizationMessagesUnsupportedRequest
	}
	return service.NewOrganizationSpendingCapabilityRegistry(h.cfg).Require(service.OrganizationEndpointFamilyMessages, requestShape)
}

func (h *GatewayHandler) recordOrganizationGatewayMessagesPreDispatchRejection(c *gin.Context, apiKey *service.APIKey, body []byte, requestShape string, rejection error, streamStarted *bool) bool {
	if h == nil || h.organizationExecution == nil {
		return true
	}
	input, err := service.NewOrganizationMessagesPreDispatchRejectionInput(apiKey, body, requestShape, organizationGatewayChatClientRequestID(c), rejection)
	if err == nil {
		command, commandErr := h.organizationExecution.NewReservationCommand(input)
		if commandErr == nil {
			result, reserveErr := h.organizationExecution.Reserve(c.Request.Context(), *command)
			if reserveErr == nil && result != nil && result.Disposition == service.OrganizationExecutionReservationDispositionPreDispatchRejected && result.FinalEvidence != nil && result.FinalEvidence.ZeroProof == command.PreDispatchZeroProof {
				return true
			}
			err = reserveErr
			if err == nil {
				err = service.ErrOrganizationExecutionInvariant
			}
		} else {
			err = commandErr
		}
	}
	h.writeOrganizationGatewayMessagesError(c, err, streamStarted)
	return false
}

func (h *GatewayHandler) writeOrganizationGatewayMessagesError(c *gin.Context, err error, streamStarted *bool) {
	status := infraerrors.Code(err)
	if status < http.StatusBadRequest || status > 599 {
		status = http.StatusInternalServerError
	}
	code := strings.ToLower(strings.TrimSpace(infraerrors.Reason(err)))
	if code == "" {
		code = "organization_billing_unavailable"
	}
	message := strings.TrimSpace(infraerrors.Message(err))
	if message == "" {
		message = "Organization billing is temporarily unavailable"
	}
	h.handleStreamingAwareError(c, status, code, message, streamStarted != nil && *streamStarted)
}

type organizationGatewayMessagesAdapter struct {
	handler        *GatewayHandler
	c              *gin.Context
	apiKey         *service.APIKey
	subject        middleware2.AuthSubject
	plan           *service.OrganizationMessagesCandidatePlan
	requestedModel string
	reqLog         *zap.Logger
	streamStarted  *bool
	lastFailover   *service.UpstreamFailoverError
	lastErr        error
	zeroProof      string
	admissionDone  bool
	userRelease    func()
}

func (a *organizationGatewayMessagesAdapter) Dispatch(ctx context.Context, request service.OrganizationExecutionDispatchRequest) (*service.OrganizationExecutionAttemptResult, error) {
	if a == nil || a.handler == nil || a.handler.gatewayService == nil || a.plan == nil || len(a.plan.Candidates) != 1 || a.c == nil {
		return nil, service.ErrOrganizationExecutionAdapterUnavailable
	}
	if result := a.ensureAdmission(ctx); result != nil {
		return result, nil
	}
	candidate, err := a.handler.gatewayService.RevalidateOrganizationGatewayMessagesCandidate(ctx, a.apiKey, a.plan.Candidates[0], a.plan.Policy, a.plan.SourceBody)
	if err != nil {
		a.zeroProof = "candidate_snapshot_conflict"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}, nil
	}
	release, acquired, err := a.handler.concurrencyHelper.TryAcquireAccountSlot(ctx, candidate.AccountID, candidate.Account.Concurrency)
	if err != nil || !acquired {
		a.lastErr = err
		a.zeroProof = "account_concurrency_unavailable"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}, nil
	}
	if release != nil {
		defer release()
	}
	setOpsSelectedAccount(a.c, candidate.AccountID, candidate.Account.Platform)
	writerSizeBefore := a.c.Writer.Size()
	previousProviderRequestID := a.c.Request.Header.Get("X-Client-Request-ID")
	a.c.Request.Header.Set("X-Client-Request-ID", request.ProviderRequestID)
	result, forwardErr, localZeroProof := a.forward(ctx, *candidate)
	if previousProviderRequestID == "" {
		a.c.Request.Header.Del("X-Client-Request-ID")
	} else {
		a.c.Request.Header.Set("X-Client-Request-ID", previousProviderRequestID)
	}
	if localZeroProof != "" {
		a.lastErr = forwardErr
		a.zeroProof = localZeroProof
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: localZeroProof}, nil
	}
	return a.resultForForward(request, *candidate, result, forwardErr, writerSizeBefore)
}

func (a *organizationGatewayMessagesAdapter) ensureAdmission(ctx context.Context) *service.OrganizationExecutionAttemptResult {
	if a.admissionDone {
		return nil
	}
	a.admissionDone = true
	if err := checkGatewayBillingEligibility(ctx, a.c, a.handler.billingCacheService, a.apiKey, nil, service.QuotaPlatform(ctx, a.apiKey)); err != nil {
		a.lastErr = err
		a.zeroProof = "organization_platform_admission_rejected"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}
	}
	release, acquired, err := a.handler.concurrencyHelper.TryAcquireUserSlot(ctx, a.subject.UserID, a.subject.Concurrency)
	if err != nil {
		a.lastErr = err
	}
	if err != nil || !acquired {
		a.zeroProof = "user_concurrency_unavailable"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}
	}
	a.userRelease = release
	return nil
}

func (a *organizationGatewayMessagesAdapter) releaseUserSlot() {
	if a != nil && a.userRelease != nil {
		a.userRelease()
		a.userRelease = nil
	}
}

func (a *organizationGatewayMessagesAdapter) forward(ctx context.Context, candidate service.OrganizationMessagesCandidate) (*service.ForwardResult, error, string) {
	if candidate.Account == nil {
		return nil, service.ErrOrganizationMessagesCandidateConflict, "candidate_snapshot_conflict"
	}
	if candidate.Route.Platform == service.PlatformGemini {
		if a.handler.geminiCompatService == nil {
			return nil, service.ErrOrganizationBillingUnavailable, "local_policy_rejected"
		}
		result, err := a.handler.geminiCompatService.ForwardOrganizationMessagesAttempt(ctx, a.c, candidate.Account, candidate.ForwardBody)
		return result, err, organizationMessagesLocalZeroProof(err)
	}
	parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(candidate.ForwardBody), domain.PlatformAnthropic)
	if err != nil {
		return nil, err, "local_policy_rejected"
	}
	parsed.GroupID = a.apiKey.GroupID
	result, err := a.handler.gatewayService.ForwardOrganizationMessagesAttempt(ctx, a.c, candidate.Account, parsed)
	return result, err, organizationMessagesLocalZeroProof(err)
}

func (a *organizationGatewayMessagesAdapter) resultForForward(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationMessagesCandidate, result *service.ForwardResult, forwardErr error, writerSizeBefore int) (*service.OrganizationExecutionAttemptResult, error) {
	now := time.Now().UTC()
	if result != nil {
		rawCost, tokens, err := a.handler.gatewayService.CalculateOrganizationGatewayMessagesActualCost(candidate, result)
		if err != nil {
			a.lastErr = err
			return &service.OrganizationExecutionAttemptResult{Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, Observation: a.pendingObservation(request, candidate)}, err
		}
		outcome := service.OrganizationExecutionTerminalOutcomeSuccess
		if forwardErr != nil || result.ClientDisconnect {
			outcome = service.OrganizationExecutionTerminalOutcomeError
		}
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: true, TerminalOutcome: outcome,
			Observation: a.observation(request, candidate, result, rawCost, tokens, forwardErr, now),
		}, nil
	}
	var failoverErr *service.UpstreamFailoverError
	writerSizeUnchanged := a.c == nil || a.c.Writer == nil || a.c.Writer.Size() == writerSizeBefore
	if errors.As(forwardErr, &failoverErr) && failoverErr.ZeroCostProven && !failoverErr.NoHTTPResponse && writerSizeUnchanged {
		a.lastFailover = failoverErr
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: true, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError,
			Observation: a.observation(request, candidate, nil, "0", service.UsageTokens{}, forwardErr, now),
		}, nil
	}
	a.lastErr = forwardErr
	return &service.OrganizationExecutionAttemptResult{
		Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
		Observation: a.pendingObservation(request, candidate),
	}, forwardErr
}

func (a *organizationGatewayMessagesAdapter) observation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationMessagesCandidate, result *service.ForwardResult, rawCost string, tokens service.UsageTokens, forwardErr error, observedAt time.Time) *service.OrganizationExecutionAttemptObservation {
	evidence := map[string]any{
		"account_id": candidate.AccountID, "platform": candidate.Route.Platform, "requested_model": a.requestedModel,
		"dispatch_model": candidate.DispatchModel, "upstream_model": candidate.UpstreamModel,
	}
	if result != nil {
		evidence["upstream_request_id"] = result.RequestID
		evidence["client_disconnect"] = result.ClientDisconnect
	}
	if forwardErr != nil {
		evidence["error_class"] = "forward_error"
		var failoverErr *service.UpstreamFailoverError
		if errors.As(forwardErr, &failoverErr) {
			evidence["upstream_status"] = failoverErr.StatusCode
		}
	}
	evidenceJSON, _ := json.Marshal(evidence)
	routeJSON, _ := json.Marshal(candidate)
	return &service.OrganizationExecutionAttemptObservation{
		AccountID: candidate.AccountID, RouteSnapshot: routeJSON,
		ProviderRequestID: request.ProviderRequestID, ProviderCorrelationID: request.ProviderCorrelationID,
		Evidence: evidenceJSON, RawActualCost: rawCost,
		InputTokens: int64(tokens.InputTokens), OutputTokens: int64(tokens.OutputTokens),
		CacheCreationTokens: int64(tokens.CacheCreationTokens), CacheReadTokens: int64(tokens.CacheReadTokens), ProviderObservedAt: observedAt,
	}
}

func (a *organizationGatewayMessagesAdapter) pendingObservation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationMessagesCandidate) *service.OrganizationExecutionAttemptObservation {
	routeJSON, _ := json.Marshal(candidate)
	return &service.OrganizationExecutionAttemptObservation{
		AccountID: candidate.AccountID, RouteSnapshot: routeJSON,
		ProviderRequestID: request.ProviderRequestID, ProviderCorrelationID: request.ProviderCorrelationID,
	}
}

func organizationMessagesLocalZeroProof(err error) string {
	var localErr *service.OpenAILocalPreDispatchError
	if !errors.As(err, &localErr) {
		return ""
	}
	if proof := strings.TrimSpace(localErr.ZeroProof); proof != "" {
		return proof
	}
	return "local_policy_rejected"
}
