package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

func (h *OpenAIGatewayHandler) handleOrganizationResponsesRequestBody(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	body []byte,
	reqLog *zap.Logger,
	streamStarted *bool,
) {
	requestedModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	stream, streamOK := parseOpenAICompatibleStream(body)
	if !streamOK {
		stream = false
	}
	forwardBody := body
	if h.gatewayService != nil && requestedModel != "" {
		channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, requestedModel)
		forwardBody = openAIModelMappedBody(body, channelMapping.Mapped, channelMapping.MappedModel, h.gatewayService.ReplaceModelInBody)
	}
	if requestedModel != "" {
		reqLog = reqLog.With(zap.String("model", requestedModel), zap.Bool("stream", stream))
		setOpsRequestContext(c, requestedModel, stream)
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	h.handleOrganizationResponses(c, apiKey, subject, subscription, forwardBody, requestedModel, stream, body, reqLog, streamStarted)
}

func (h *OpenAIGatewayHandler) handleOrganizationResponses(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	subscription *service.UserSubscription,
	forwardBody []byte,
	requestedModel string,
	stream bool,
	sessionHashBody []byte,
	reqLog *zap.Logger,
	streamStarted *bool,
) {
	shape := service.OrganizationRequestShapeHTTP
	if stream {
		shape = service.OrganizationRequestShapeStreaming
	}
	if h == nil || h.organizationExecution == nil {
		h.writeOrganizationResponsesError(c, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	if h.gatewayService == nil {
		rejection := service.ErrOrganizationBillingUnavailable
		if evidenceErr := h.recordOrganizationResponsesPreDispatchRejection(c, apiKey, forwardBody, shape, rejection); evidenceErr != nil {
			h.writeOrganizationResponsesError(c, evidenceErr, streamStarted)
			return
		}
		h.writeOrganizationResponsesError(c, rejection, streamStarted)
		return
	}
	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, requestedModel, forwardBody); decision != nil && decision.Blocked {
		rejection := infraerrors.New(contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		if evidenceErr := h.recordOrganizationResponsesPreDispatchRejection(c, apiKey, forwardBody, shape, rejection); evidenceErr != nil {
			h.writeOrganizationResponsesError(c, evidenceErr, streamStarted)
			return
		}
		h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		return
	}
	if message := organizationResponsesFunctionCallOutputValidationMessage(forwardBody); message != "" {
		rejection := infraerrors.BadRequest("ORGANIZATION_RESPONSES_UNSUPPORTED_REQUEST", message)
		if evidenceErr := h.recordOrganizationResponsesPreDispatchRejection(c, apiKey, forwardBody, shape, rejection); evidenceErr != nil {
			h.writeOrganizationResponsesError(c, evidenceErr, streamStarted)
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", message)
		return
	}
	if err := h.validateOrganizationResponsesPreDispatch(apiKey, shape); err != nil {
		reqLog.Info("openai.organization_responses_admission_rejected", zap.Error(err))
		if evidenceErr := h.recordOrganizationResponsesPreDispatchRejection(c, apiKey, forwardBody, shape, err); evidenceErr != nil {
			reqLog.Error("openai.organization_responses_admission_rejection_evidence_failed", zap.Error(evidenceErr))
			h.writeOrganizationResponsesError(c, evidenceErr, streamStarted)
			return
		}
		h.writeOrganizationResponsesError(c, err, streamStarted)
		return
	}
	plan, err := h.gatewayService.PlanOrganizationResponsesCandidates(c.Request.Context(), service.OrganizationResponsesCandidatePlanInput{
		APIKey: apiKey, Body: forwardBody, RequestShape: shape, RequestPath: c.Request.URL.Path,
		GroupID: apiKey.GroupID, MaximumAttempts: h.maxAccountSwitches + 1,
	})
	if err != nil {
		reqLog.Info("openai.organization_responses_plan_rejected", zap.Error(err))
		if evidenceErr := h.recordOrganizationResponsesPreDispatchRejection(c, apiKey, forwardBody, shape, err); evidenceErr != nil {
			reqLog.Error("openai.organization_responses_plan_rejection_evidence_failed", zap.Error(evidenceErr))
			h.writeOrganizationResponsesError(c, evidenceErr, streamStarted)
			return
		}
		h.writeOrganizationResponsesError(c, err, streamStarted)
		return
	}
	reservationInput, err := plan.NewExecutionReservationInput(apiKey, organizationResponsesClientRequestID(c))
	if err != nil {
		h.writeOrganizationResponsesError(c, err, streamStarted)
		return
	}
	command, err := h.organizationExecution.NewReservationCommand(reservationInput)
	if err != nil {
		h.writeOrganizationResponsesError(c, err, streamStarted)
		return
	}
	sessionHash := h.gatewayService.GenerateSessionHash(c, sessionHashBody)
	adapter := &organizationResponsesHTTPAdapter{
		handler: h, c: c, apiKey: apiKey, subject: subject, subscription: subscription, plan: plan,
		requestedModel: requestedModel, sessionHash: sessionHash, reqLog: reqLog, streamStarted: streamStarted,
	}
	defer adapter.releaseUserSlot()
	run, err := h.organizationExecution.ExecuteMultiAttempt(c.Request.Context(), *command, plan.MaximumAttempts, adapter)
	if err != nil {
		reqLog.Error("openai.organization_responses_execution_failed", zap.Error(err))
		if !c.Writer.Written() {
			h.writeOrganizationResponsesError(c, err, streamStarted)
		}
		return
	}
	if run == nil {
		if !c.Writer.Written() {
			h.writeOrganizationResponsesError(c, service.ErrOrganizationExecutionInvariant, streamStarted)
		}
		return
	}
	if run.PendingReconciliation {
		reqLog.Warn("openai.organization_responses_pending_reconciliation", zap.String("execution_id", run.ExecutionID))
		if !c.Writer.Written() {
			h.writeOrganizationResponsesError(c, service.ErrOrganizationExecutionPendingReconciliation, streamStarted)
		}
		return
	}
	if run.Final == nil {
		if !c.Writer.Written() {
			h.writeOrganizationResponsesError(c, service.ErrOrganizationExecutionInvariant, streamStarted)
		}
		return
	}
	h.recordOrganizationResponsesRateLimitUsage(apiKey, run.Final)
	if run.Final.TerminalOutcome == service.OrganizationExecutionTerminalOutcomeError && !c.Writer.Written() {
		switch adapter.zeroProof {
		case "candidate_snapshot_conflict":
			h.writeOrganizationResponsesError(c, service.ErrOrganizationResponsesCandidateConflict, streamStarted)
		case "account_concurrency_unavailable":
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "rate_limit_error", "No frozen account has available concurrency", streamStarted != nil && *streamStarted)
		case "organization_platform_admission_rejected":
			h.writeOrganizationResponsesAdmissionError(c, adapter.lastErr, streamStarted)
		case "user_concurrency_unavailable":
			h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "User concurrency limit reached", streamStarted != nil && *streamStarted)
		default:
			if adapter.lastFailover != nil {
				h.handleFailoverExhausted(c, adapter.lastFailover, streamStarted != nil && *streamStarted)
			} else {
				h.ensureForwardErrorResponse(c, streamStarted != nil && *streamStarted)
			}
		}
	}
}

func organizationResponsesFunctionCallOutputValidationMessage(body []byte) string {
	if !gjson.GetBytes(body, `input.#(type=="function_call_output")`).Exists() {
		return ""
	}
	validation := service.ValidateFunctionCallOutputContextBytes(body)
	if !validation.HasFunctionCallOutput || strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()) != "" || validation.HasToolCallContext || validation.HasItemReferenceForAllCallIDs {
		return ""
	}
	if validation.HasFunctionCallOutputMissingCallID {
		return "function_call_output requires call_id on HTTP requests; continuation via previous_response_id is only supported on Responses WebSocket v2"
	}
	return "function_call_output requires item_reference ids matching each call_id on HTTP requests; continuation via previous_response_id is only supported on Responses WebSocket v2"
}

func (h *OpenAIGatewayHandler) validateOrganizationResponsesPreDispatch(apiKey *service.APIKey, shape string) error {
	if h == nil || h.cfg == nil || h.cfg.RunMode == config.RunModeSimple {
		return service.ErrOrganizationBillingUnavailable
	}
	if apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformOpenAI {
		return infraerrors.BadRequest(
			"ORGANIZATION_SPENDING_UNSUPPORTED_REQUEST",
			"organization spending does not support this endpoint route",
		)
	}
	return service.NewOrganizationSpendingCapabilityRegistry(h.cfg).Require(service.OrganizationEndpointFamilyResponses, shape)
}

func (h *OpenAIGatewayHandler) recordOrganizationResponsesPreDispatchRejection(c *gin.Context, apiKey *service.APIKey, body []byte, requestShape string, rejection error) error {
	input, err := service.NewOrganizationResponsesPreDispatchRejectionInput(
		apiKey, body, requestShape, c.Request.URL.Path, organizationResponsesClientRequestID(c), rejection,
	)
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
	if result == nil || result.Disposition != service.OrganizationExecutionReservationDispositionPreDispatchRejected || result.FinalEvidence == nil {
		return service.ErrOrganizationExecutionInvariant
	}
	if result.FinalEvidence.ZeroProof != command.PreDispatchZeroProof {
		return service.ErrOrganizationResponsesCandidateConflict
	}
	return nil
}

func (h *OpenAIGatewayHandler) writeOrganizationResponsesAdmissionError(c *gin.Context, err error, streamStarted *bool) {
	status, code, message, retryAfter := billingErrorDetails(err)
	if retryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(retryAfter))
	}
	h.handleStreamingAwareError(c, status, code, message, streamStarted != nil && *streamStarted)
}

func (h *OpenAIGatewayHandler) recordOrganizationResponsesRateLimitUsage(apiKey *service.APIKey, final *service.OrganizationFinalExecutionEvidenceResult) {
	if h == nil || h.billingCacheService == nil || apiKey == nil || !apiKey.HasRateLimits() || final == nil {
		return
	}
	cost, err := decimal.NewFromString(final.RawActualCost)
	if err != nil || !cost.IsPositive() {
		return
	}
	// The execution repository applies quota and rate usage together through
	// the durable API-key outbox. Invalidate the cache instead of incrementing
	// it here so replayed final evidence cannot double-count the same execution.
	_ = h.billingCacheService.InvalidateAPIKeyRateLimit(context.Background(), apiKey.ID)
}

func (h *OpenAIGatewayHandler) writeOrganizationResponsesError(c *gin.Context, err error, streamStarted *bool) {
	status := infraerrors.Code(err)
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	code := infraerrors.Reason(err)
	if strings.TrimSpace(code) == "" {
		code = "ORGANIZATION_BILLING_UNAVAILABLE"
	}
	message := infraerrors.Message(err)
	if strings.TrimSpace(message) == "" {
		message = "organization billing is unavailable"
	}
	h.handleStreamingAwareError(c, status, code, message, streamStarted != nil && *streamStarted)
}

func organizationResponsesClientRequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	for _, header := range []string{"X-Request-ID", "X-Client-Request-ID"} {
		if value := strings.TrimSpace(c.GetHeader(header)); value != "" {
			return value
		}
	}
	return ""
}

type organizationResponsesHTTPAdapter struct {
	handler        *OpenAIGatewayHandler
	c              *gin.Context
	apiKey         *service.APIKey
	subject        middleware2.AuthSubject
	subscription   *service.UserSubscription
	plan           *service.OrganizationResponsesCandidatePlan
	requestedModel string
	sessionHash    string
	reqLog         *zap.Logger
	streamStarted  *bool
	nextCandidate  int
	dispatchCount  int
	lastFailover   *service.UpstreamFailoverError
	lastErr        error
	zeroProof      string
	admissionDone  bool
	userRelease    func()
}

func (a *organizationResponsesHTTPAdapter) Dispatch(ctx context.Context, request service.OrganizationExecutionDispatchRequest) (*service.OrganizationExecutionAttemptResult, error) {
	if a == nil || a.handler == nil || a.handler.gatewayService == nil || a.plan == nil || a.c == nil {
		return nil, service.ErrOrganizationExecutionAdapterUnavailable
	}
	if result := a.ensureAdmission(ctx); result != nil {
		return result, nil
	}
	sawConflict := false
	sawBusy := false
	for a.nextCandidate < len(a.plan.Candidates) {
		candidate := a.plan.Candidates[a.nextCandidate]
		a.nextCandidate++
		validated, err := a.handler.gatewayService.RevalidateOrganizationResponsesCandidate(ctx, a.apiKey, candidate)
		if err != nil {
			sawConflict = true
			continue
		}
		candidate = *validated
		release, acquired, err := a.handler.concurrencyHelper.TryAcquireAccountSlot(ctx, candidate.AccountID, candidate.Account.Concurrency)
		if err != nil {
			a.lastErr = err
			sawBusy = true
			continue
		}
		if !acquired {
			sawBusy = true
			continue
		}
		if release != nil {
			defer release()
		}
		if err := a.handler.gatewayService.BindStickySession(ctx, a.apiKey.GroupID, a.sessionHash, candidate.AccountID); err != nil {
			a.reqLog.Warn("openai.organization_responses_bind_sticky_failed", zap.Int64("account_id", candidate.AccountID), zap.Error(err))
		}
		setOpsSelectedAccount(a.c, candidate.AccountID, candidate.Account.Platform)
		a.dispatchCount++
		writerSizeBefore := a.c.Writer.Size()
		previousProviderRequestID := a.c.Request.Header.Get("X-Client-Request-Id")
		a.c.Request.Header.Set("X-Client-Request-Id", request.ProviderRequestID)
		dispatchCtx := service.WithOrganizationResponsesDispatchContext(ctx)
		result, forwardErr := a.handler.gatewayService.Forward(dispatchCtx, a.c, candidate.Account, candidate.ForwardBody)
		if previousProviderRequestID == "" {
			a.c.Request.Header.Del("X-Client-Request-Id")
		} else {
			a.c.Request.Header.Set("X-Client-Request-Id", previousProviderRequestID)
		}
		return a.resultForForward(request, candidate, result, forwardErr, writerSizeBefore)
	}
	if sawConflict {
		a.zeroProof = "candidate_snapshot_conflict"
	} else if sawBusy {
		a.zeroProof = "account_concurrency_unavailable"
	} else {
		a.zeroProof = "candidate_snapshot_conflict"
	}
	return &service.OrganizationExecutionAttemptResult{
		Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof,
	}, nil
}

func (a *organizationResponsesHTTPAdapter) ensureAdmission(ctx context.Context) *service.OrganizationExecutionAttemptResult {
	if a.admissionDone {
		return nil
	}
	a.admissionDone = true
	if err := checkGatewayBillingEligibility(ctx, a.c, a.handler.billingCacheService, a.apiKey, a.subscription, service.QuotaPlatform(ctx, a.apiKey)); err != nil {
		a.lastErr = err
		a.zeroProof = "organization_platform_admission_rejected"
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof,
		}
	}
	release, acquired := a.handler.acquireResponsesUserSlot(a.c, a.subject.UserID, a.subject.Concurrency, a.plan.Policy.Stream, a.streamStarted, a.reqLog)
	if !acquired {
		a.zeroProof = "user_concurrency_unavailable"
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof,
		}
	}
	a.userRelease = release
	return nil
}

func (a *organizationResponsesHTTPAdapter) releaseUserSlot() {
	if a == nil || a.userRelease == nil {
		return
	}
	a.userRelease()
	a.userRelease = nil
}

func (a *organizationResponsesHTTPAdapter) resultForForward(
	request service.OrganizationExecutionDispatchRequest,
	candidate service.OrganizationResponsesCandidate,
	result *service.OpenAIForwardResult,
	forwardErr error,
	writerSizeBefore int,
) (*service.OrganizationExecutionAttemptResult, error) {
	now := time.Now().UTC()
	if result != nil {
		if !result.UsageObserved {
			a.lastErr = service.ErrOrganizationExecutionPendingReconciliation
			return &service.OrganizationExecutionAttemptResult{
				Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
				Observation: a.pendingObservation(request, candidate),
			}, a.lastErr
		}
		if candidate.Account.Type == service.AccountTypeOAuth {
			a.handler.gatewayService.UpdateCodexUsageSnapshotFromHeaders(a.c.Request.Context(), candidate.AccountID, result.ResponseHeaders)
		}
		rawCost, tokens, err := a.handler.gatewayService.CalculateOrganizationResponsesActualCost(candidate, result)
		if err != nil {
			return &service.OrganizationExecutionAttemptResult{
				Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
				Observation: a.pendingObservation(request, candidate),
			}, err
		}
		terminalOutcome := service.OrganizationExecutionTerminalOutcomeSuccess
		if forwardErr != nil || result.ClientDisconnect {
			terminalOutcome = service.OrganizationExecutionTerminalOutcomeError
		}
		a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, terminalOutcome == service.OrganizationExecutionTerminalOutcomeSuccess, result.FirstTokenMs)
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: true, TerminalOutcome: terminalOutcome,
			Observation: a.observation(request, candidate, result, rawCost, tokens, forwardErr, now),
		}, nil
	}

	var failoverErr *service.UpstreamFailoverError
	if errors.As(forwardErr, &failoverErr) && !failoverErr.NoHTTPResponse && a.c.Writer.Size() == writerSizeBefore {
		a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, false, nil)
		a.handler.gatewayService.RecordOpenAIAccountSwitch()
		a.lastFailover = failoverErr
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: true, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, Retryable: true,
			Observation: a.observation(request, candidate, nil, "0", service.UsageTokens{}, forwardErr, now),
		}, nil
	}
	if isOrganizationResponsesProvenLocalRejection(forwardErr) {
		a.lastErr = forwardErr
		a.zeroProof = "local_policy_rejected"
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof,
		}, nil
	}
	a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, false, nil)
	a.lastErr = forwardErr
	return &service.OrganizationExecutionAttemptResult{
		Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
		Observation: a.pendingObservation(request, candidate),
	}, forwardErr
}

func (a *organizationResponsesHTTPAdapter) observation(
	request service.OrganizationExecutionDispatchRequest,
	candidate service.OrganizationResponsesCandidate,
	result *service.OpenAIForwardResult,
	rawCost string,
	tokens service.UsageTokens,
	forwardErr error,
	observedAt time.Time,
) *service.OrganizationExecutionAttemptObservation {
	evidence := map[string]any{
		"account_id": candidate.AccountID, "route": candidate.Route, "requested_model": a.requestedModel,
		"upstream_model": candidate.UpstreamModel, "service_tier": candidate.ServiceTier,
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
		AccountID: candidate.AccountID, RouteSnapshot: routeJSON, ProviderRequestID: request.ProviderRequestID,
		ProviderCorrelationID: request.ProviderCorrelationID, Evidence: evidenceJSON, RawActualCost: rawCost,
		InputTokens: int64(tokens.InputTokens), OutputTokens: int64(tokens.OutputTokens),
		CacheCreationTokens: int64(tokens.CacheCreationTokens), CacheReadTokens: int64(tokens.CacheReadTokens), ProviderObservedAt: observedAt,
	}
}

func (a *organizationResponsesHTTPAdapter) pendingObservation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationResponsesCandidate) *service.OrganizationExecutionAttemptObservation {
	routeJSON, _ := json.Marshal(candidate)
	return &service.OrganizationExecutionAttemptObservation{
		AccountID: candidate.AccountID, RouteSnapshot: routeJSON,
		ProviderRequestID: request.ProviderRequestID, ProviderCorrelationID: request.ProviderCorrelationID,
	}
}

func isOrganizationResponsesProvenLocalRejection(err error) bool {
	if err == nil {
		return false
	}
	var betaBlocked *service.BetaBlockedError
	if errors.As(err, &betaBlocked) {
		return true
	}
	var fastBlocked *service.OpenAIFastBlockedError
	if errors.As(err, &fastBlocked) {
		return true
	}
	var codexRestricted *service.OpenAICodexClientRestrictionError
	if errors.As(err, &codexRestricted) {
		return true
	}
	var localPreDispatch *service.OpenAILocalPreDispatchError
	return errors.As(err, &localPreDispatch)
}
