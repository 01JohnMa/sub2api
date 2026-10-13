package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

func (h *GatewayHandler) handleOrganizationGeminiV1BetaModels(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware.AuthSubject,
	body []byte,
	requestedModel string,
	action string,
	endpointFamily string,
	reqLog *zap.Logger,
) {
	requestShape := service.OrganizationRequestShapeModelsAction
	maximumAttempts := h.maxAccountSwitchesGemini + 1
	if endpointFamily == service.OrganizationEndpointFamilyAntigravity {
		requestShape = service.OrganizationRequestShapeHTTP
		maximumAttempts = h.maxAccountSwitches + 1
		if action == "streamGenerateContent" {
			requestShape = service.OrganizationRequestShapeStreaming
		}
	}
	if h == nil || h.organizationExecution == nil || h.gatewayService == nil || h.billingCacheService == nil || h.cfg == nil {
		googleError(c, http.StatusServiceUnavailable, "Organization billing is temporarily unavailable")
		return
	}
	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolGemini, requestedModel, body); decision != nil && decision.Blocked {
		rejection := infraerrors.New(contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		if err := h.recordOrganizationGeminiPreDispatchRejectionForFamily(c, apiKey, body, endpointFamily, requestShape, action, requestedModel, rejection); err != nil {
			h.writeOrganizationGeminiError(c, err)
			return
		}
		googleError(c, contentModerationStatus(decision), decision.Message)
		return
	}
	if err := service.NewOrganizationSpendingCapabilityRegistry(h.cfg).Require(endpointFamily, requestShape); err != nil {
		if evidenceErr := h.recordOrganizationGeminiPreDispatchRejectionForFamily(c, apiKey, body, endpointFamily, requestShape, action, requestedModel, err); evidenceErr != nil {
			h.writeOrganizationGeminiError(c, evidenceErr)
			return
		}
		h.writeOrganizationGeminiError(c, err)
		return
	}
	planInput := service.OrganizationGeminiCandidatePlanInput{
		APIKey: apiKey, Body: body, EndpointFamily: endpointFamily,
		RequestShape: requestShape, Action: action, RequestedModel: requestedModel,
		MaximumAttempts: maximumAttempts,
	}
	planInput.PrepareCandidate = h.organizationAntigravityCandidatePreparer(c.Request.Context())
	plan, err := h.gatewayService.PlanOrganizationGeminiCandidates(c.Request.Context(), planInput)
	if err != nil {
		if evidenceErr := h.recordOrganizationGeminiPreDispatchRejectionForFamily(c, apiKey, body, endpointFamily, requestShape, action, requestedModel, err); evidenceErr != nil {
			h.writeOrganizationGeminiError(c, evidenceErr)
			return
		}
		h.writeOrganizationGeminiError(c, err)
		return
	}
	reservationInput, err := plan.NewExecutionReservationInput(apiKey, organizationGeminiClientRequestID(c))
	if err != nil {
		h.writeOrganizationGeminiError(c, err)
		return
	}
	command, err := h.organizationExecution.NewReservationCommand(reservationInput)
	if err != nil {
		h.writeOrganizationGeminiError(c, err)
		return
	}
	subscription, _ := middleware.GetSubscriptionFromContext(c)
	sessionHash := extractGeminiCLISessionHash(c, body)
	sessionKey := ""
	if sessionHash != "" {
		sessionKey = "gemini:" + sessionHash
	}
	adapter := &organizationGeminiV1BetaAdapter{
		handler: h, c: c, apiKey: apiKey, subject: subject, subscription: subscription,
		plan: plan, body: plan.ForwardBody, sessionKey: sessionKey, reqLog: reqLog,
	}
	defer adapter.releaseUserSlot()
	run, err := h.organizationExecution.ExecuteMultiAttempt(c.Request.Context(), *command, plan.MaximumAttempts, adapter)
	if err != nil {
		reqLog.Error("gemini.organization_execution_failed", zap.Error(err))
		if !c.Writer.Written() {
			h.writeOrganizationGeminiError(c, err)
		}
		return
	}
	if run == nil || run.Final == nil {
		if !c.Writer.Written() {
			h.writeOrganizationGeminiError(c, service.ErrOrganizationExecutionPendingReconciliation)
		}
		return
	}
	h.recordOrganizationGeminiRateLimitUsage(apiKey, run.Final)
	if run.Final.TerminalOutcome == service.OrganizationExecutionTerminalOutcomeError && !c.Writer.Written() {
		switch adapter.zeroProof {
		case "candidate_snapshot_conflict":
			h.writeOrganizationGeminiError(c, service.ErrOrganizationGeminiCandidateConflict)
		case "organization_platform_admission_rejected":
			h.writeOrganizationGeminiError(c, adapter.lastErr)
		case "user_concurrency_unavailable":
			googleError(c, http.StatusTooManyRequests, "User concurrency limit reached")
		case "account_concurrency_unavailable":
			googleError(c, http.StatusServiceUnavailable, "No frozen account has available concurrency")
		default:
			if adapter.lastFailover != nil {
				h.handleGeminiFailoverExhausted(c, adapter.lastFailover)
			} else {
				h.writeOrganizationGeminiError(c, adapter.lastErr)
			}
		}
	}
}

func (h *GatewayHandler) recordOrganizationGeminiV1BetaPreDispatchRejection(c *gin.Context, apiKey *service.APIKey, body []byte, endpointFamily, requestedModel, action string, rejection error) error {
	if h == nil || h.organizationExecution == nil {
		return service.ErrOrganizationBillingUnavailable
	}
	requestShape := service.OrganizationRequestShapeModelsAction
	if endpointFamily == service.OrganizationEndpointFamilyAntigravity {
		requestShape = service.OrganizationRequestShapeHTTP
		if action == "streamGenerateContent" {
			requestShape = service.OrganizationRequestShapeStreaming
		}
	}
	return h.recordOrganizationGeminiPreDispatchRejectionForFamily(c, apiKey, body, endpointFamily, requestShape, action, requestedModel, rejection)
}

func (h *GatewayHandler) recordOrganizationGeminiPreDispatchRejection(c *gin.Context, apiKey *service.APIKey, body []byte, requestShape, action, requestedModel string, rejection error) error {
	return h.recordOrganizationGeminiPreDispatchRejectionForFamily(c, apiKey, body, service.OrganizationEndpointFamilyGemini, requestShape, action, requestedModel, rejection)
}

func (h *GatewayHandler) recordOrganizationGeminiPreDispatchRejectionForFamily(c *gin.Context, apiKey *service.APIKey, body []byte, endpointFamily, requestShape, action, requestedModel string, rejection error) error {
	if h == nil || h.organizationExecution == nil {
		return service.ErrOrganizationBillingUnavailable
	}
	input, err := service.NewOrganizationGeminiPreDispatchRejectionInput(
		apiKey, body, endpointFamily, requestShape, action, requestedModel,
		organizationGeminiClientRequestID(c), rejection,
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
	return nil
}

func (h *GatewayHandler) handleOrganizationAntigravityMessages(c *gin.Context, apiKey *service.APIKey, subject middleware.AuthSubject, body []byte, requestedModel string, stream bool, reqLog *zap.Logger, streamStarted *bool) {
	requestShape := service.OrganizationRequestShapeHTTP
	if stream {
		requestShape = service.OrganizationRequestShapeStreaming
	}
	if h == nil || h.organizationExecution == nil || h.gatewayService == nil || h.billingCacheService == nil || h.cfg == nil {
		h.writeOrganizationAntigravityError(c, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolAnthropicMessages, requestedModel, body); decision != nil && decision.Blocked {
		rejection := infraerrors.New(contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		if err := h.recordOrganizationGeminiPreDispatchRejectionForFamily(c, apiKey, body, service.OrganizationEndpointFamilyAntigravity, requestShape, "messages", requestedModel, rejection); err != nil {
			h.writeOrganizationAntigravityError(c, err, streamStarted)
			return
		}
		h.handleStreamingAwareError(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message, streamStarted != nil && *streamStarted)
		return
	}
	if err := service.NewOrganizationSpendingCapabilityRegistry(h.cfg).Require(service.OrganizationEndpointFamilyAntigravity, requestShape); err != nil {
		if evidenceErr := h.recordOrganizationGeminiPreDispatchRejectionForFamily(c, apiKey, body, service.OrganizationEndpointFamilyAntigravity, requestShape, "messages", requestedModel, err); evidenceErr != nil {
			h.writeOrganizationAntigravityError(c, evidenceErr, streamStarted)
			return
		}
		h.writeOrganizationAntigravityError(c, err, streamStarted)
		return
	}
	planInput := service.OrganizationGeminiCandidatePlanInput{
		APIKey: apiKey, Body: body, EndpointFamily: service.OrganizationEndpointFamilyAntigravity,
		RequestShape: requestShape, Action: "messages", RequestedModel: requestedModel, MaximumAttempts: h.maxAccountSwitches + 1,
		PrepareCandidate: h.organizationAntigravityCandidatePreparer(c.Request.Context()),
	}
	plan, err := h.gatewayService.PlanOrganizationGeminiCandidates(c.Request.Context(), planInput)
	if err != nil {
		if evidenceErr := h.recordOrganizationGeminiPreDispatchRejectionForFamily(c, apiKey, body, service.OrganizationEndpointFamilyAntigravity, requestShape, "messages", requestedModel, err); evidenceErr != nil {
			h.writeOrganizationAntigravityError(c, evidenceErr, streamStarted)
			return
		}
		h.writeOrganizationAntigravityError(c, err, streamStarted)
		return
	}
	reservationInput, err := plan.NewExecutionReservationInput(apiKey, organizationGeminiClientRequestID(c))
	if err != nil {
		h.writeOrganizationAntigravityError(c, err, streamStarted)
		return
	}
	command, err := h.organizationExecution.NewReservationCommand(reservationInput)
	if err != nil {
		h.writeOrganizationAntigravityError(c, err, streamStarted)
		return
	}
	subscription, _ := middleware.GetSubscriptionFromContext(c)
	adapter := &organizationGeminiV1BetaAdapter{
		handler: h, c: c, apiKey: apiKey, subject: subject, subscription: subscription, plan: plan, body: plan.ForwardBody, reqLog: reqLog,
	}
	defer adapter.releaseUserSlot()
	run, err := h.organizationExecution.ExecuteMultiAttempt(c.Request.Context(), *command, plan.MaximumAttempts, adapter)
	if err != nil {
		if !c.Writer.Written() {
			h.writeOrganizationAntigravityError(c, err, streamStarted)
		}
		return
	}
	if run == nil || run.Final == nil {
		if !c.Writer.Written() {
			h.writeOrganizationAntigravityError(c, service.ErrOrganizationExecutionPendingReconciliation, streamStarted)
		}
		return
	}
	h.recordOrganizationGeminiRateLimitUsage(apiKey, run.Final)
	if run.Final.TerminalOutcome == service.OrganizationExecutionTerminalOutcomeError && !c.Writer.Written() {
		switch adapter.zeroProof {
		case "candidate_snapshot_conflict":
			h.writeOrganizationAntigravityError(c, service.ErrOrganizationGeminiCandidateConflict, streamStarted)
		case "organization_platform_admission_rejected":
			h.writeOrganizationAntigravityError(c, adapter.lastErr, streamStarted)
		case "user_concurrency_unavailable":
			h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "User concurrency limit reached", streamStarted != nil && *streamStarted)
		case "account_concurrency_unavailable":
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "rate_limit_error", "No frozen account has available concurrency", streamStarted != nil && *streamStarted)
		default:
			if adapter.lastFailover != nil {
				h.handleFailoverExhausted(c, adapter.lastFailover, service.PlatformAntigravity, streamStarted != nil && *streamStarted)
			} else {
				h.writeOrganizationAntigravityError(c, adapter.lastErr, streamStarted)
			}
		}
	}
}

func (h *GatewayHandler) writeOrganizationAntigravityError(c *gin.Context, err error, streamStarted *bool) {
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
		message = "organization billing is temporarily unavailable"
	}
	h.handleStreamingAwareError(c, status, code, message, streamStarted != nil && *streamStarted)
}

func (h *GatewayHandler) writeOrganizationGeminiError(c *gin.Context, err error) {
	status := infraerrors.Code(err)
	if status < http.StatusBadRequest || status > 599 {
		status = http.StatusInternalServerError
	}
	message := strings.TrimSpace(infraerrors.Message(err))
	if message == "" {
		message = "Organization billing is temporarily unavailable"
	}
	googleError(c, status, message)
}

func organizationGeminiClientRequestID(c *gin.Context) string {
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

func (h *GatewayHandler) recordOrganizationGeminiRateLimitUsage(apiKey *service.APIKey, final *service.OrganizationFinalExecutionEvidenceResult) {
	if h == nil || h.billingCacheService == nil || apiKey == nil || !apiKey.HasRateLimits() || final == nil {
		return
	}
	cost, err := decimal.NewFromString(final.RawActualCost)
	if err != nil || !cost.IsPositive() {
		return
	}
	_ = h.billingCacheService.InvalidateAPIKeyRateLimit(context.Background(), apiKey.ID)
}

type organizationGeminiV1BetaAdapter struct {
	handler                *GatewayHandler
	c                      *gin.Context
	apiKey                 *service.APIKey
	subject                middleware.AuthSubject
	subscription           *service.UserSubscription
	plan                   *service.OrganizationGeminiCandidatePlan
	body                   []byte
	sessionKey             string
	reqLog                 *zap.Logger
	nextCandidate          int
	lastFailover           *service.UpstreamFailoverError
	lastErr                error
	zeroProof              string
	admissionDone          bool
	userRelease            func()
	revalidate             func(context.Context, *service.APIKey, service.OrganizationGeminiCandidate, service.OrganizationGeminiRequestPolicy, []byte) (*service.OrganizationGeminiCandidate, error)
	forwardTest            func(context.Context, service.OrganizationGeminiCandidate) (*service.ForwardResult, error)
	acquireAccountSlotTest func(context.Context, service.OrganizationGeminiCandidate) (func(), bool, error)
	actualCostTest         func(service.OrganizationGeminiCandidate, *service.ForwardResult) (string, service.UsageTokens, error)
}

func (a *organizationGeminiV1BetaAdapter) Dispatch(ctx context.Context, request service.OrganizationExecutionDispatchRequest) (*service.OrganizationExecutionAttemptResult, error) {
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
		revalidate := a.handler.gatewayService.RevalidateOrganizationGeminiCandidate
		if a.revalidate != nil {
			revalidate = a.revalidate
		}
		validated, err := revalidate(ctx, a.apiKey, candidate, a.plan.Policy, a.body)
		if err != nil {
			sawConflict = true
			continue
		}
		candidate = *validated
		var release func()
		var acquired bool
		if a.acquireAccountSlotTest != nil {
			release, acquired, err = a.acquireAccountSlotTest(ctx, candidate)
		} else {
			release, acquired, err = a.handler.concurrencyHelper.TryAcquireAccountSlot(ctx, candidate.AccountID, candidate.Account.Concurrency)
		}
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
		if err := a.handler.gatewayService.BindStickySession(ctx, a.apiKey.GroupID, a.sessionKey, candidate.AccountID); err != nil && a.reqLog != nil {
			a.reqLog.Warn("gemini.organization_bind_sticky_failed", zap.Int64("account_id", candidate.AccountID), zap.Error(err))
		}
		setOpsSelectedAccount(a.c, candidate.AccountID, candidate.Account.Platform)
		writerSizeBefore := a.c.Writer.Size()
		previousProviderRequestID := a.c.Request.Header.Get("X-Client-Request-Id")
		a.c.Request.Header.Set("X-Client-Request-Id", request.ProviderRequestID)
		result, forwardErr := a.forward(ctx, candidate)
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

func (a *organizationGeminiV1BetaAdapter) ensureAdmission(ctx context.Context) *service.OrganizationExecutionAttemptResult {
	if a.admissionDone {
		return nil
	}
	a.admissionDone = true
	if err := checkGatewayBillingEligibility(ctx, a.c, a.handler.billingCacheService, a.apiKey, a.subscription, service.QuotaPlatform(ctx, a.apiKey)); err != nil {
		a.lastErr = err
		a.zeroProof = "organization_platform_admission_rejected"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}
	}
	release, acquired, err := a.handler.concurrencyHelper.TryAcquireUserSlot(ctx, a.subject.UserID, a.subject.Concurrency)
	if err != nil {
		a.lastErr = err
		a.zeroProof = "organization_platform_admission_rejected"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}
	}
	if !acquired {
		a.zeroProof = "user_concurrency_unavailable"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}
	}
	a.userRelease = release
	return nil
}

func (a *organizationGeminiV1BetaAdapter) releaseUserSlot() {
	if a == nil || a.userRelease == nil {
		return
	}
	a.userRelease()
	a.userRelease = nil
}

func (a *organizationGeminiV1BetaAdapter) forward(ctx context.Context, candidate service.OrganizationGeminiCandidate) (*service.ForwardResult, error) {
	if a.forwardTest != nil {
		return a.forwardTest(ctx, candidate)
	}
	if candidate.Account == nil {
		return nil, service.ErrOrganizationGeminiCandidateConflict
	}
	if candidate.Account.Platform == service.PlatformAntigravity && candidate.Account.Type != service.AccountTypeAPIKey && !candidate.PreparedPayload {
		return nil, service.ErrOrganizationGeminiCandidateConflict
	}
	if a.plan.Policy.EndpointFamily == service.OrganizationEndpointFamilyAntigravity {
		if a.plan.Policy.Action != "messages" {
			if candidate.Account.Platform == service.PlatformAntigravity && candidate.Account.Type != service.AccountTypeAPIKey {
				options := []service.ForwardGeminiOption{service.WithForwardGeminiSession(derefGroupID(a.apiKey.GroupID), a.sessionKey)}
				if candidate.PreparedPayload {
					options = append(options, service.WithPreparedAntigravityGeminiPayload(service.PreparedAntigravityGeminiPayload{Payload: candidate.ForwardBody, DispatchModel: candidate.UpstreamModel, UpstreamAction: "streamGenerateContent", IdentitySummary: candidate.IdentitySummary}))
				}
				return a.handler.antigravityGatewayService.ForwardGemini(
					ctx, a.c, candidate.Account, candidate.DispatchModel, a.plan.Policy.Action, a.plan.Policy.Stream, candidate.ForwardBody, a.sessionKey != "",
					options...,
				)
			}
			return a.handler.geminiCompatService.ForwardNative(ctx, a.c, candidate.Account, candidate.DispatchModel, a.plan.Policy.Action, a.plan.Policy.Stream, candidate.ForwardBody)
		}
		if candidate.Account.Platform == service.PlatformAntigravity && candidate.Account.Type != service.AccountTypeAPIKey {
			options := []service.ForwardOption{}
			body := candidate.ForwardBody
			if candidate.PreparedPayload {
				body = a.body
				options = append(options, service.WithPreparedAntigravityMessagesPayload(service.PreparedAntigravityMessagesPayload{Payload: candidate.ForwardBody, DispatchModel: candidate.UpstreamModel, IdentitySummary: candidate.IdentitySummary}))
			}
			return a.handler.antigravityGatewayService.Forward(ctx, a.c, candidate.Account, body, a.sessionKey != "", options...)
		}
		parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(candidate.ForwardBody), domain.PlatformAnthropic)
		if err != nil {
			return nil, err
		}
		parsed.GroupID = a.apiKey.GroupID
		return a.handler.gatewayService.Forward(ctx, a.c, candidate.Account, parsed)
	}
	if candidate.Account.Platform == service.PlatformAntigravity && candidate.Account.Type != service.AccountTypeAPIKey {
		options := []service.ForwardGeminiOption{service.WithForwardGeminiSession(derefGroupID(a.apiKey.GroupID), a.sessionKey)}
		if candidate.PreparedPayload {
			options = append(options, service.WithPreparedAntigravityGeminiPayload(service.PreparedAntigravityGeminiPayload{Payload: candidate.ForwardBody, DispatchModel: candidate.UpstreamModel, UpstreamAction: "streamGenerateContent", IdentitySummary: candidate.IdentitySummary}))
		}
		return a.handler.antigravityGatewayService.ForwardGemini(
			ctx, a.c, candidate.Account, candidate.DispatchModel, a.plan.Policy.Action, a.plan.Policy.Stream, candidate.ForwardBody, a.sessionKey != "",
			options...,
		)
	}
	return a.handler.geminiCompatService.ForwardNative(ctx, a.c, candidate.Account, candidate.DispatchModel, a.plan.Policy.Action, a.plan.Policy.Stream, candidate.ForwardBody)
}

func (h *GatewayHandler) organizationAntigravityCandidatePreparer(ctx context.Context) func(account *service.Account, dispatchModel, upstreamModel, action string, body []byte) ([]byte, string, error) {
	if h == nil || h.antigravityGatewayService == nil {
		return nil
	}
	return func(account *service.Account, dispatchModel, upstreamModel, action string, body []byte) ([]byte, string, error) {
		if action == "messages" {
			prepared, err := h.antigravityGatewayService.PrepareOrganizationMessagesPayload(ctx, account, body)
			if err != nil {
				return nil, "", err
			}
			if prepared.DispatchModel != upstreamModel {
				return nil, "", service.ErrOrganizationGeminiCandidateConflict
			}
			return prepared.Payload, prepared.IdentitySummary, nil
		}
		prepared, err := h.antigravityGatewayService.PrepareOrganizationGeminiPayload(account, dispatchModel, action, body)
		if err != nil {
			return nil, "", err
		}
		if prepared.DispatchModel != upstreamModel {
			return nil, "", service.ErrOrganizationGeminiCandidateConflict
		}
		return prepared.Payload, prepared.IdentitySummary, nil
	}
}

func (a *organizationGeminiV1BetaAdapter) resultForForward(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationGeminiCandidate, result *service.ForwardResult, forwardErr error, writerSizeBefore int) (*service.OrganizationExecutionAttemptResult, error) {
	now := time.Now().UTC()
	if result != nil {
		calculate := a.handler.gatewayService.CalculateOrganizationGeminiActualCost
		if a.actualCostTest != nil {
			calculate = a.actualCostTest
		}
		rawCost, tokens, err := calculate(candidate, result)
		if errors.Is(err, service.ErrOrganizationExecutionPendingReconciliation) {
			a.lastErr = service.ErrOrganizationExecutionPendingReconciliation
			return &service.OrganizationExecutionAttemptResult{Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, Observation: a.pendingObservation(request, candidate)}, a.lastErr
		}
		if err != nil {
			a.lastErr = err
			return &service.OrganizationExecutionAttemptResult{Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, Observation: a.pendingObservation(request, candidate)}, err
		}
		terminalOutcome := service.OrganizationExecutionTerminalOutcomeSuccess
		if forwardErr != nil || result.ClientDisconnect {
			terminalOutcome = service.OrganizationExecutionTerminalOutcomeError
		}
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: true, TerminalOutcome: terminalOutcome,
			Observation: a.observation(request, candidate, result, rawCost, tokens, forwardErr, now),
		}, nil
	}

	var failoverErr *service.UpstreamFailoverError
	writerSizeUnchanged := a.c.Writer == nil || a.c.Writer.Size() == writerSizeBefore
	if errors.As(forwardErr, &failoverErr) && !failoverErr.NoHTTPResponse && writerSizeUnchanged {
		a.lastFailover = failoverErr
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: true, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, Retryable: true,
			Observation: a.observation(request, candidate, nil, "0", service.UsageTokens{}, forwardErr, now),
		}, nil
	}
	a.lastErr = forwardErr
	return &service.OrganizationExecutionAttemptResult{
		Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
		Observation: a.pendingObservation(request, candidate),
	}, forwardErr
}

func (a *organizationGeminiV1BetaAdapter) observation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationGeminiCandidate, result *service.ForwardResult, rawCost string, tokens service.UsageTokens, forwardErr error, observedAt time.Time) *service.OrganizationExecutionAttemptObservation {
	evidence := map[string]any{
		"account_id": candidate.AccountID, "route": candidate.Route, "requested_model": candidate.RequestedModel,
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
	routeJSON, _ := json.Marshal(candidate.Route)
	return &service.OrganizationExecutionAttemptObservation{
		AccountID: candidate.AccountID, RouteSnapshot: routeJSON, ProviderRequestID: request.ProviderRequestID,
		ProviderCorrelationID: request.ProviderCorrelationID, Evidence: evidenceJSON, RawActualCost: rawCost,
		InputTokens: int64(tokens.InputTokens), OutputTokens: int64(tokens.OutputTokens), CacheCreationTokens: int64(tokens.CacheCreationTokens), CacheReadTokens: int64(tokens.CacheReadTokens),
		ProviderObservedAt: observedAt,
	}
}

func (a *organizationGeminiV1BetaAdapter) pendingObservation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationGeminiCandidate) *service.OrganizationExecutionAttemptObservation {
	routeJSON, _ := json.Marshal(candidate.Route)
	return &service.OrganizationExecutionAttemptObservation{
		AccountID: candidate.AccountID, RouteSnapshot: routeJSON, ProviderRequestID: request.ProviderRequestID, ProviderCorrelationID: request.ProviderCorrelationID,
	}
}
