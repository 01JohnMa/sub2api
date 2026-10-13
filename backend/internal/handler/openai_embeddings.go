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
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Embeddings handles the OpenAI-compatible Embeddings API.
// POST /v1/embeddings
func (h *OpenAIGatewayHandler) Embeddings(c *gin.Context) {
	streamStarted := false
	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.embeddings",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	_, organizationRequest := middleware2.GetOrganizationKeyAuthorizationFromContext(c)
	if organizationRequest {
		h.handleOrganizationEmbeddings(c, apiKey, subject, reqLog, &streamStarted)
		return
	}
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	if !gjson.ValidBytes(body) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || strings.TrimSpace(modelResult.String()) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	reqLog = reqLog.With(zap.String("model", reqModel))
	setOpsRequestContext(c, reqModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))

	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	if err := checkGatewayBillingEligibility(c.Request.Context(), c, h.billingCacheService, apiKey, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("openai_embeddings.billing_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}

	failedAccountIDs := make(map[int64]struct{})
	var lastFailoverErr *service.UpstreamFailoverError
	switchCount := 0
	maxAccountSwitches := h.maxAccountSwitches
	if maxAccountSwitches <= 0 {
		maxAccountSwitches = 3
	}
	routingStart := time.Now()

	for {
		selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(),
			apiKey.GroupID,
			"",
			"",
			reqModel,
			failedAccountIDs,
			service.OpenAIUpstreamTransportHTTPSSE,
			service.OpenAIEndpointCapabilityEmbeddings,
			false,
		)
		if err != nil {
			reqLog.Warn("openai_embeddings.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
			)
			if len(failedAccountIDs) == 0 {
				markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Service temporarily unavailable")
				return
			}
			if lastFailoverErr != nil {
				h.handleFailoverExhausted(c, lastFailoverErr, false)
			} else {
				h.errorResponse(c, http.StatusBadGateway, "api_error", "Upstream request failed")
			}
			return
		}
		if selection == nil || selection.Account == nil {
			markOpsRoutingCapacityLimited(c)
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
			return
		}
		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountReleaseFunc, accountAcquired := h.acquireResponsesAccountSlot(c, apiKey.GroupID, "", selection, false, &streamStarted, reqLog)
		if !accountAcquired {
			return
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()

		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		writerSizeBeforeForward := c.Writer.Size()
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer func() {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
			}()
			return h.gatewayService.ForwardEmbeddings(c.Request.Context(), c, account, forwardBody, "")
		}()

		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)

		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if c.Writer.Size() != writerSizeBeforeForward {
					h.handleFailoverExhausted(c, failoverErr, true)
					return
				}
				h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
				h.gatewayService.RecordOpenAIAccountSwitch()
				failedAccountIDs[account.ID] = struct{}{}
				lastFailoverErr = failoverErr
				if switchCount >= maxAccountSwitches {
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				switchCount++
				reqLog.Warn("openai_embeddings.upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", switchCount),
					zap.Int("max_switches", maxAccountSwitches),
				)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
			if c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("openai_embeddings.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
			return
		}

		h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, nil)
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)

		h.submitOpenAIUsageRecordTask(c.Request.Context(), result, func(ctx context.Context) {
			if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
				Result:             result,
				APIKey:             apiKey,
				User:               apiKey.User,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				APIKeyService:      h.apiKeyService,
				ChannelUsageFields: channelMapping.ToUsageFields(reqModel, result.UpstreamModel),
			}); err != nil {
				logger.L().With(
					zap.String("component", "handler.openai_gateway.embeddings"),
					zap.Int64("user_id", subject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("group_id", apiKey.GroupID),
					zap.String("model", reqModel),
					zap.Int64("account_id", account.ID),
				).Error("openai_embeddings.record_usage_failed", zap.Error(err))
			}
		})
		reqLog.Debug("openai_embeddings.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

func (h *OpenAIGatewayHandler) handleOrganizationEmbeddings(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	reqLog *zap.Logger,
	streamStarted *bool,
) {
	const shape = service.OrganizationRequestShapeHTTP
	if h == nil || h.organizationExecution == nil {
		h.writeOrganizationEmbeddingsImagesError(c, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		h.handleOrganizationEmbeddingsPreDispatchRejection(c, apiKey, nil, "", shape, service.ErrOrganizationEmbeddingsImagesUnsupportedRequest, streamStarted)
		return
	}
	policy, err := service.BuildOrganizationEmbeddingsRequestPolicy(body)
	if err != nil {
		h.handleOrganizationEmbeddingsPreDispatchRejection(c, apiKey, body, "", shape, err, streamStarted)
		return
	}
	setOpsRequestContext(c, policy.RequestedModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	if err := h.validateOrganizationEmbeddingsImagesPreDispatch(apiKey, service.OrganizationEndpointFamilyEmbeddings, shape); err != nil {
		reqLog.Info("openai.organization_embeddings_admission_rejected", zap.Error(err))
		h.handleOrganizationEmbeddingsPreDispatchRejection(c, apiKey, body, policy.RequestedModel, shape, err, streamStarted)
		return
	}
	if h.gatewayService == nil {
		h.handleOrganizationEmbeddingsPreDispatchRejection(c, apiKey, body, policy.RequestedModel, shape, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	plan, err := h.gatewayService.PlanOrganizationEmbeddingsCandidates(c.Request.Context(), service.OrganizationEmbeddingsImagesCandidatePlanInput{
		APIKey: apiKey, Body: body, MaximumAttempts: h.maxAccountSwitches + 1,
	})
	if err != nil {
		reqLog.Info("openai.organization_embeddings_plan_rejected", zap.Error(err))
		h.handleOrganizationEmbeddingsPreDispatchRejection(c, apiKey, body, policy.RequestedModel, shape, err, streamStarted)
		return
	}
	input, err := plan.NewExecutionReservationInput(apiKey, organizationEmbeddingsImagesClientRequestID(c))
	if err != nil {
		h.writeOrganizationEmbeddingsImagesError(c, err, streamStarted)
		return
	}
	command, err := h.organizationExecution.NewReservationCommand(input)
	if err != nil {
		h.writeOrganizationEmbeddingsImagesError(c, err, streamStarted)
		return
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	adapter := &organizationEmbeddingsAdapter{
		handler: h, c: c, apiKey: apiKey, subject: subject, subscription: subscription, plan: plan, reqLog: reqLog, streamStarted: streamStarted,
	}
	defer adapter.releaseUserSlot()
	run, err := h.organizationExecution.ExecuteMultiAttempt(c.Request.Context(), *command, plan.MaximumAttempts, adapter)
	if err != nil {
		reqLog.Error("openai.organization_embeddings_execution_failed", zap.Error(err))
		if !c.Writer.Written() {
			h.writeOrganizationEmbeddingsImagesError(c, err, streamStarted)
		}
		return
	}
	if run == nil || run.Final == nil {
		if run != nil && run.PendingReconciliation {
			reqLog.Warn("openai.organization_embeddings_pending_reconciliation", zap.String("execution_id", run.ExecutionID))
			if !c.Writer.Written() {
				h.writeOrganizationEmbeddingsImagesError(c, service.ErrOrganizationExecutionPendingReconciliation, streamStarted)
			}
			return
		}
		if !c.Writer.Written() {
			h.writeOrganizationEmbeddingsImagesError(c, service.ErrOrganizationExecutionInvariant, streamStarted)
		}
		return
	}
	h.recordOrganizationResponsesRateLimitUsage(apiKey, run.Final)
	if run.Final.TerminalOutcome == service.OrganizationExecutionTerminalOutcomeError && !c.Writer.Written() {
		if adapter.lastFailover != nil {
			h.handleFailoverExhausted(c, adapter.lastFailover, false)
			return
		}
		if adapter.lastErr != nil {
			h.writeOrganizationEmbeddingsImagesError(c, adapter.lastErr, streamStarted)
			return
		}
		h.ensureForwardErrorResponse(c, false)
	}
}

func (h *OpenAIGatewayHandler) validateOrganizationEmbeddingsImagesPreDispatch(apiKey *service.APIKey, family, shape string) error {
	if h == nil || h.cfg == nil || h.cfg.RunMode == config.RunModeSimple {
		return service.ErrOrganizationBillingUnavailable
	}
	if apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformOpenAI {
		return infraerrors.BadRequest("ORGANIZATION_SPENDING_UNSUPPORTED_REQUEST", "organization spending does not support this endpoint route")
	}
	return service.NewOrganizationSpendingCapabilityRegistry(h.cfg).Require(family, shape)
}

func (h *OpenAIGatewayHandler) handleOrganizationEmbeddingsPreDispatchRejection(c *gin.Context, apiKey *service.APIKey, body []byte, model, shape string, rejection error, streamStarted *bool) {
	if err := h.recordOrganizationEmbeddingsImagesPreDispatchRejection(c, apiKey, body, service.OrganizationEndpointFamilyEmbeddings, shape, model, rejection); err != nil {
		h.writeOrganizationEmbeddingsImagesError(c, err, streamStarted)
		return
	}
	h.writeOrganizationEmbeddingsImagesError(c, rejection, streamStarted)
}

func (h *OpenAIGatewayHandler) recordOrganizationEmbeddingsImagesPreDispatchRejection(c *gin.Context, apiKey *service.APIKey, body []byte, family, shape, model string, rejection error) error {
	if h == nil || h.organizationExecution == nil {
		return service.ErrOrganizationBillingUnavailable
	}
	input, err := service.NewOrganizationEmbeddingsImagesPreDispatchRejectionInput(apiKey, body, family, shape, model, organizationEmbeddingsImagesClientRequestID(c), rejection)
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

func (h *OpenAIGatewayHandler) writeOrganizationEmbeddingsImagesError(c *gin.Context, err error, streamStarted *bool) {
	status := infraerrors.Code(err)
	if status < http.StatusBadRequest || status > 599 {
		status = http.StatusInternalServerError
	}
	code := strings.TrimSpace(infraerrors.Reason(err))
	if code == "" {
		code = "ORGANIZATION_BILLING_UNAVAILABLE"
	}
	message := strings.TrimSpace(infraerrors.Message(err))
	if message == "" {
		message = "organization billing is unavailable"
	}
	h.handleStreamingAwareError(c, status, code, message, streamStarted != nil && *streamStarted)
}

func organizationEmbeddingsImagesClientRequestID(c *gin.Context) string {
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

type organizationEmbeddingsAdapter struct {
	handler       *OpenAIGatewayHandler
	c             *gin.Context
	apiKey        *service.APIKey
	subject       middleware2.AuthSubject
	subscription  *service.UserSubscription
	plan          *service.OrganizationEmbeddingsImagesCandidatePlan
	reqLog        *zap.Logger
	streamStarted *bool
	nextCandidate int
	lastFailover  *service.UpstreamFailoverError
	lastErr       error
	zeroProof     string
	admissionDone bool
	userRelease   func()
}

func (a *organizationEmbeddingsAdapter) Dispatch(ctx context.Context, request service.OrganizationExecutionDispatchRequest) (*service.OrganizationExecutionAttemptResult, error) {
	if a == nil || a.handler == nil || a.handler.gatewayService == nil || a.plan == nil {
		return nil, service.ErrOrganizationExecutionAdapterUnavailable
	}
	if rejection := a.ensureAdmission(ctx); rejection != nil {
		return rejection, nil
	}
	for a.nextCandidate < len(a.plan.Candidates) {
		candidate := a.plan.Candidates[a.nextCandidate]
		a.nextCandidate++
		validated, err := a.handler.gatewayService.RevalidateOrganizationEmbeddingsImagesCandidate(ctx, a.apiKey, candidate)
		if err != nil {
			continue
		}
		release, acquired, err := a.handler.concurrencyHelper.TryAcquireAccountSlot(ctx, validated.AccountID, validated.Account.Concurrency)
		if err != nil || !acquired {
			if err != nil {
				a.lastErr = err
			}
			a.zeroProof = "account_concurrency_unavailable"
			continue
		}
		setOpsSelectedAccount(a.c, validated.AccountID, validated.Account.Platform)
		writerSizeBefore := a.c.Writer.Size()
		previousRequestID := a.c.Request.Header.Get("X-Client-Request-Id")
		a.c.Request.Header.Set("X-Client-Request-Id", request.ProviderRequestID)
		result, forwardErr := func() (*service.OpenAIForwardResult, error) {
			defer func() {
				if release != nil {
					release()
				}
			}()
			return a.handler.gatewayService.ForwardEmbeddings(ctx, a.c, validated.Account, validated.ForwardBody, "")
		}()
		if previousRequestID == "" {
			a.c.Request.Header.Del("X-Client-Request-Id")
		} else {
			a.c.Request.Header.Set("X-Client-Request-Id", previousRequestID)
		}
		return a.resultForForward(request, *validated, result, forwardErr, writerSizeBefore)
	}
	if a.zeroProof == "" {
		a.zeroProof = "candidate_snapshot_conflict"
	}
	return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}, nil
}

func (a *organizationEmbeddingsAdapter) ensureAdmission(ctx context.Context) *service.OrganizationExecutionAttemptResult {
	if a.admissionDone {
		return nil
	}
	a.admissionDone = true
	if err := checkGatewayBillingEligibility(ctx, a.c, a.handler.billingCacheService, a.apiKey, a.subscription, service.QuotaPlatform(ctx, a.apiKey)); err != nil {
		a.lastErr = err
		a.zeroProof = "organization_platform_admission_rejected"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}
	}
	release, acquired := a.handler.acquireResponsesUserSlot(a.c, a.subject.UserID, a.subject.Concurrency, false, a.streamStarted, a.reqLog)
	if !acquired {
		a.zeroProof = "user_concurrency_unavailable"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}
	}
	a.userRelease = release
	return nil
}

func (a *organizationEmbeddingsAdapter) releaseUserSlot() {
	if a != nil && a.userRelease != nil {
		a.userRelease()
		a.userRelease = nil
	}
}

func (a *organizationEmbeddingsAdapter) resultForForward(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationEmbeddingsImagesCandidate, result *service.OpenAIForwardResult, forwardErr error, writerSizeBefore int) (*service.OrganizationExecutionAttemptResult, error) {
	now := time.Now().UTC()
	if result != nil {
		if !result.UsageObserved {
			a.lastErr = service.ErrOrganizationExecutionPendingReconciliation
			return &service.OrganizationExecutionAttemptResult{Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, Observation: a.pendingObservation(request, candidate)}, a.lastErr
		}
		rawCost, tokens, err := a.handler.gatewayService.CalculateOrganizationEmbeddingsActualCost(candidate, result)
		if err != nil {
			a.lastErr = err
			return &service.OrganizationExecutionAttemptResult{Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, Observation: a.pendingObservation(request, candidate)}, err
		}
		outcome := service.OrganizationExecutionTerminalOutcomeSuccess
		if forwardErr != nil || result.ClientDisconnect {
			outcome = service.OrganizationExecutionTerminalOutcomeError
		}
		a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, outcome == service.OrganizationExecutionTerminalOutcomeSuccess, result.FirstTokenMs)
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{Dispatched: true, TerminalOutcome: outcome, Observation: a.observation(request, candidate, result, rawCost, tokens, forwardErr, now)}, nil
	}
	var failoverErr *service.UpstreamFailoverError
	if errors.As(forwardErr, &failoverErr) && !failoverErr.NoHTTPResponse && a.c.Writer.Size() == writerSizeBefore {
		a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, false, nil)
		a.handler.gatewayService.RecordOpenAIAccountSwitch()
		a.lastFailover = failoverErr
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{Dispatched: true, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, Retryable: true, Observation: a.observation(request, candidate, nil, "0", service.UsageTokens{}, forwardErr, now)}, nil
	}
	a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, false, nil)
	a.lastErr = forwardErr
	return &service.OrganizationExecutionAttemptResult{Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, Observation: a.pendingObservation(request, candidate)}, forwardErr
}

func (a *organizationEmbeddingsAdapter) observation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationEmbeddingsImagesCandidate, result *service.OpenAIForwardResult, rawCost string, tokens service.UsageTokens, forwardErr error, observedAt time.Time) *service.OrganizationExecutionAttemptObservation {
	evidence := map[string]any{"account_id": candidate.AccountID, "route": candidate.Route, "requested_model": candidate.RequestedModel, "upstream_model": candidate.UpstreamModel}
	if result != nil {
		evidence["upstream_request_id"] = result.RequestID
		evidence["client_disconnect"] = result.ClientDisconnect
	}
	if forwardErr != nil {
		evidence["error_class"] = "forward_error"
	}
	evidenceJSON, _ := json.Marshal(evidence)
	routeJSON, _ := json.Marshal(candidate)
	return &service.OrganizationExecutionAttemptObservation{AccountID: candidate.AccountID, RouteSnapshot: routeJSON, ProviderRequestID: request.ProviderRequestID, ProviderCorrelationID: request.ProviderCorrelationID, Evidence: evidenceJSON, RawActualCost: rawCost, InputTokens: int64(tokens.InputTokens), OutputTokens: int64(tokens.OutputTokens), CacheCreationTokens: int64(tokens.CacheCreationTokens), CacheReadTokens: int64(tokens.CacheReadTokens), ProviderObservedAt: observedAt}
}

func (a *organizationEmbeddingsAdapter) pendingObservation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationEmbeddingsImagesCandidate) *service.OrganizationExecutionAttemptObservation {
	routeJSON, _ := json.Marshal(candidate)
	return &service.OrganizationExecutionAttemptObservation{AccountID: candidate.AccountID, RouteSnapshot: routeJSON, ProviderRequestID: request.ProviderRequestID, ProviderCorrelationID: request.ProviderCorrelationID}
}
