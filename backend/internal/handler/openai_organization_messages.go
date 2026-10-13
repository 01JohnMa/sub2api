package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

func (h *OpenAIGatewayHandler) handleOrganizationOpenAIMessages(c *gin.Context, apiKey *service.APIKey, subject middleware2.AuthSubject, body []byte, reqLog *zap.Logger, streamStarted *bool) {
	stream := gjson.GetBytes(body, "stream").Bool()
	requestShape := service.OrganizationRequestShapeHTTP
	if stream {
		requestShape = service.OrganizationRequestShapeStreaming
	}
	requestedModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	setOpsRequestContext(c, requestedModel, stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(stream, false)))
	if h == nil || h.organizationExecution == nil {
		h.writeOrganizationOpenAIMessagesError(c, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	if h.gatewayService == nil || apiKey == nil || apiKey.Group == nil {
		if err := h.recordOrganizationOpenAIMessagesPreDispatchRejection(c, apiKey, body, requestShape, service.ErrOrganizationBillingUnavailable); err != nil {
			h.writeOrganizationOpenAIMessagesError(c, err, streamStarted)
			return
		}
		h.writeOrganizationOpenAIMessagesError(c, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolAnthropicMessages, requestedModel, body); decision != nil && decision.Blocked {
		rejection := infraerrors.New(contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		if err := h.recordOrganizationOpenAIMessagesPreDispatchRejection(c, apiKey, body, requestShape, rejection); err != nil {
			h.writeOrganizationOpenAIMessagesError(c, err, streamStarted)
			return
		}
		h.anthropicErrorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		return
	}
	if err := h.validateOrganizationOpenAIMessagesPreDispatch(apiKey, requestShape); err != nil {
		reqLog.Info("openai_messages.organization_admission_rejected", zap.Error(err))
		if evidenceErr := h.recordOrganizationOpenAIMessagesPreDispatchRejection(c, apiKey, body, requestShape, err); evidenceErr != nil {
			h.writeOrganizationOpenAIMessagesError(c, evidenceErr, streamStarted)
			return
		}
		h.writeOrganizationOpenAIMessagesError(c, err, streamStarted)
		return
	}
	plan, err := h.gatewayService.PlanOrganizationOpenAIMessagesCandidate(c.Request.Context(), apiKey, body, requestShape)
	if err != nil {
		reqLog.Info("openai_messages.organization_plan_rejected", zap.Error(err))
		if evidenceErr := h.recordOrganizationOpenAIMessagesPreDispatchRejection(c, apiKey, body, requestShape, err); evidenceErr != nil {
			h.writeOrganizationOpenAIMessagesError(c, evidenceErr, streamStarted)
			return
		}
		h.writeOrganizationOpenAIMessagesError(c, err, streamStarted)
		return
	}
	reservationInput, err := plan.NewExecutionReservationInput(apiKey, organizationGatewayChatClientRequestID(c))
	if err != nil {
		h.writeOrganizationOpenAIMessagesError(c, err, streamStarted)
		return
	}
	command, err := h.organizationExecution.NewReservationCommand(reservationInput)
	if err != nil {
		h.writeOrganizationOpenAIMessagesError(c, err, streamStarted)
		return
	}
	adapter := &organizationOpenAIMessagesAdapter{
		handler: h, c: c, apiKey: apiKey, subject: subject, plan: plan,
		requestedModel: requestedModel, reqLog: reqLog, streamStarted: streamStarted,
	}
	defer adapter.releaseUserSlot()
	run, err := h.organizationExecution.ExecuteMultiAttempt(c.Request.Context(), *command, plan.MaximumAttempts, adapter)
	if err != nil {
		reqLog.Error("openai_messages.organization_execution_failed", zap.Error(err))
		if !c.Writer.Written() {
			h.writeOrganizationOpenAIMessagesError(c, err, streamStarted)
		}
		return
	}
	if run == nil || run.PendingReconciliation || run.Final == nil {
		if !c.Writer.Written() {
			if run != nil && run.PendingReconciliation {
				h.writeOrganizationOpenAIMessagesError(c, service.ErrOrganizationExecutionPendingReconciliation, streamStarted)
			} else {
				h.writeOrganizationOpenAIMessagesError(c, service.ErrOrganizationExecutionInvariant, streamStarted)
			}
		}
		return
	}
	if h.billingCacheService != nil && apiKey.HasRateLimits() && run.Final.RawActualCost != "" && run.Final.RawActualCost != "0" {
		_ = h.billingCacheService.InvalidateAPIKeyRateLimit(context.Background(), apiKey.ID)
	}
	if run.Final.TerminalOutcome != service.OrganizationExecutionTerminalOutcomeError || c.Writer.Written() {
		return
	}
	switch adapter.zeroProof {
	case "candidate_snapshot_conflict":
		h.writeOrganizationOpenAIMessagesError(c, service.ErrOrganizationMessagesCandidateConflict, streamStarted)
	case "user_concurrency_unavailable":
		h.anthropicErrorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "User concurrency limit reached")
	case "account_concurrency_unavailable":
		h.anthropicErrorResponse(c, http.StatusServiceUnavailable, "rate_limit_error", "No frozen account has available concurrency")
	case "organization_platform_admission_rejected":
		h.writeOrganizationOpenAIMessagesError(c, adapter.lastErr, streamStarted)
	default:
		if adapter.lastFailover != nil {
			h.handleAnthropicFailoverExhausted(c, adapter.lastFailover, streamStarted != nil && *streamStarted)
			return
		}
		h.ensureAnthropicErrorResponse(c, streamStarted != nil && *streamStarted)
	}
}

func (h *OpenAIGatewayHandler) validateOrganizationOpenAIMessagesPreDispatch(apiKey *service.APIKey, requestShape string) error {
	if h == nil || h.cfg == nil || h.cfg.RunMode == config.RunModeSimple || apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformOpenAI {
		return service.ErrOrganizationBillingUnavailable
	}
	return service.NewOrganizationSpendingCapabilityRegistry(h.cfg).Require(service.OrganizationEndpointFamilyMessages, requestShape)
}

func (h *OpenAIGatewayHandler) recordOrganizationOpenAIMessagesPreDispatchRejection(c *gin.Context, apiKey *service.APIKey, body []byte, requestShape string, rejection error) error {
	if h == nil || h.organizationExecution == nil {
		return service.ErrOrganizationBillingUnavailable
	}
	input, err := service.NewOrganizationMessagesPreDispatchRejectionInput(apiKey, body, requestShape, organizationGatewayChatClientRequestID(c), rejection)
	if err != nil {
		return err
	}
	command, err := h.organizationExecution.NewReservationCommand(input)
	if err != nil {
		return err
	}
	result, err := h.organizationExecution.Reserve(c.Request.Context(), *command)
	if err != nil {
		return err
	}
	if result == nil || result.Disposition != service.OrganizationExecutionReservationDispositionPreDispatchRejected || result.FinalEvidence == nil || result.FinalEvidence.ZeroProof != command.PreDispatchZeroProof {
		return service.ErrOrganizationExecutionInvariant
	}
	return nil
}

func (h *OpenAIGatewayHandler) writeOrganizationOpenAIMessagesError(c *gin.Context, err error, streamStarted *bool) {
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
	h.anthropicStreamingAwareError(c, status, code, message, streamStarted != nil && *streamStarted)
}

type organizationOpenAIMessagesAdapter struct {
	handler        *OpenAIGatewayHandler
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

func (a *organizationOpenAIMessagesAdapter) Dispatch(ctx context.Context, request service.OrganizationExecutionDispatchRequest) (*service.OrganizationExecutionAttemptResult, error) {
	if a == nil || a.handler == nil || a.handler.gatewayService == nil || a.plan == nil || len(a.plan.Candidates) != 1 || a.c == nil {
		return nil, service.ErrOrganizationExecutionAdapterUnavailable
	}
	if result := a.ensureAdmission(ctx); result != nil {
		return result, nil
	}
	candidate, err := a.handler.gatewayService.RevalidateOrganizationOpenAIMessagesCandidate(ctx, a.apiKey, a.plan.Candidates[0], a.plan.Policy, a.plan.SourceBody)
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
	promptCacheKey := a.handler.gatewayService.ExtractSessionID(a.c, candidate.ForwardBody)
	_, promptCacheKey = resolveOpenAIMessagesMetadataSession("", promptCacheKey, a.requestedModel, candidate.ForwardBody)
	result, forwardErr := a.handler.gatewayService.ForwardAsAnthropicOrganizationAttempt(ctx, a.c, candidate.Account, candidate.ForwardBody, promptCacheKey, candidate.DispatchModel)
	if previousProviderRequestID == "" {
		a.c.Request.Header.Del("X-Client-Request-ID")
	} else {
		a.c.Request.Header.Set("X-Client-Request-ID", previousProviderRequestID)
	}
	if proof := organizationMessagesLocalZeroProof(forwardErr); proof != "" {
		a.lastErr = forwardErr
		a.zeroProof = proof
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: proof}, nil
	}
	return a.resultForForward(request, *candidate, result, forwardErr, writerSizeBefore)
}

func (a *organizationOpenAIMessagesAdapter) ensureAdmission(ctx context.Context) *service.OrganizationExecutionAttemptResult {
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

func (a *organizationOpenAIMessagesAdapter) releaseUserSlot() {
	if a != nil && a.userRelease != nil {
		a.userRelease()
		a.userRelease = nil
	}
}

func (a *organizationOpenAIMessagesAdapter) resultForForward(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationMessagesCandidate, result *service.OpenAIForwardResult, forwardErr error, writerSizeBefore int) (*service.OrganizationExecutionAttemptResult, error) {
	now := time.Now().UTC()
	if result != nil {
		rawCost, tokens, err := a.handler.gatewayService.CalculateOrganizationOpenAIMessagesActualCost(candidate, result)
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

func (a *organizationOpenAIMessagesAdapter) observation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationMessagesCandidate, result *service.OpenAIForwardResult, rawCost string, tokens service.UsageTokens, forwardErr error, observedAt time.Time) *service.OrganizationExecutionAttemptObservation {
	evidence := map[string]any{
		"account_id": candidate.AccountID, "platform": service.PlatformOpenAI, "requested_model": a.requestedModel,
		"dispatch_model": candidate.DispatchModel, "upstream_model": candidate.UpstreamModel,
	}
	if result != nil {
		evidence["upstream_request_id"] = result.RequestID
		evidence["response_id"] = result.ResponseID
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

func (a *organizationOpenAIMessagesAdapter) pendingObservation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationMessagesCandidate) *service.OrganizationExecutionAttemptObservation {
	routeJSON, _ := json.Marshal(candidate)
	return &service.OrganizationExecutionAttemptObservation{
		AccountID: candidate.AccountID, RouteSnapshot: routeJSON,
		ProviderRequestID: request.ProviderRequestID, ProviderCorrelationID: request.ProviderCorrelationID,
	}
}
