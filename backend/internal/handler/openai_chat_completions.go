package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// ChatCompletions handles OpenAI Chat Completions API requests.
// POST /v1/chat/completions
func (h *OpenAIGatewayHandler) ChatCompletions(c *gin.Context) {
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)

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
		"handler.openai_gateway.chat_completions",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	_, organizationRequest := middleware2.GetOrganizationKeyAuthorizationFromContext(c)
	if !organizationRequest && !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if organizationRequest && !h.recordOrganizationChatPreDispatchRejection(c, apiKey, nil, service.OrganizationRequestShapeHTTP, err, &streamStarted) {
			return
		}
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		if organizationRequest && !h.recordOrganizationChatPreDispatchRejection(c, apiKey, body, service.OrganizationRequestShapeHTTP, service.ErrOrganizationChatUnsupportedRequest, &streamStarted) {
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	if !gjson.ValidBytes(body) {
		if organizationRequest && !h.recordOrganizationChatPreDispatchRejection(c, apiKey, body, service.OrganizationRequestShapeHTTP, service.ErrOrganizationChatUnsupportedRequest, &streamStarted) {
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		if organizationRequest && !h.recordOrganizationChatPreDispatchRejection(c, apiKey, body, service.OrganizationRequestShapeHTTP, service.ErrOrganizationChatUnsupportedRequest, &streamStarted) {
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	reqStream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		if organizationRequest && !h.recordOrganizationChatPreDispatchRejection(c, apiKey, body, service.OrganizationRequestShapeHTTP, service.ErrOrganizationChatUnsupportedRequest, &streamStarted) {
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return
	}

	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))
	if organizationRequest {
		h.handleOrganizationChatCompletions(c, apiKey, subject, body, reqModel, reqStream, reqLog, &streamStarted)
		return
	}

	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, reqModel, body); decision != nil && decision.Blocked {
		h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		return
	}

	// 解析渠道级模型映射
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)

	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	if err := checkGatewayBillingEligibility(c.Request.Context(), c, h.billingCacheService, apiKey, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("openai_chat_completions.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	promptCacheKey := h.gatewayService.ExtractSessionID(c, body)

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError

	for {
		reqLog.Debug("openai_chat_completions.account_selecting", zap.Int("excluded_account_count", len(failedAccountIDs)))
		selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(),
			apiKey.GroupID,
			"",
			sessionHash,
			reqModel,
			failedAccountIDs,
			service.OpenAIUpstreamTransportAny,
			service.OpenAIEndpointCapabilityChatCompletions,
			false,
		)
		if err != nil {
			reqLog.Warn("openai_chat_completions.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
			)
			if len(failedAccountIDs) == 0 {
				markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Service temporarily unavailable", streamStarted)
				return
			} else {
				if lastFailoverErr != nil {
					h.handleFailoverExhausted(c, lastFailoverErr, streamStarted)
				} else {
					h.handleStreamingAwareError(c, http.StatusBadGateway, "api_error", "Upstream request failed", streamStarted)
				}
				return
			}
		}
		if selection == nil || selection.Account == nil {
			markOpsRoutingCapacityLimited(c)
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", streamStarted)
			return
		}
		account := selection.Account
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		reqLog.Debug("openai_chat_completions.account_selected", zap.Int64("account_id", account.ID), zap.String("account_name", account.Name))
		_ = scheduleDecision
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountReleaseFunc, acquired := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, reqStream, &streamStarted, reqLog)
		if !acquired {
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
			return h.gatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, promptCacheKey, "")
		}()

		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
		if err == nil && result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
		if err != nil {
			if result != nil && result.ImageCount > 0 {
				reqLog.Warn("openai_chat_completions.forward_partial_error_with_image_result",
					zap.Int64("account_id", account.ID),
					zap.Int("image_count", result.ImageCount),
					zap.Error(err),
				)
			} else {
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					if c.Writer.Size() != writerSizeBeforeForward {
						h.handleFailoverExhausted(c, failoverErr, true)
						return
					}
					h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
					// Pool mode: retry on the same account
					if failoverErr.RetryableOnSameAccount {
						retryLimit := account.GetPoolModeRetryCount()
						if sameAccountRetryCount[account.ID] < retryLimit {
							sameAccountRetryCount[account.ID]++
							reqLog.Warn("openai_chat_completions.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
							)
							select {
							case <-c.Request.Context().Done():
								return
							case <-time.After(sameAccountRetryDelay):
							}
							continue
						}
					}
					h.gatewayService.RecordOpenAIAccountSwitch()
					failedAccountIDs[account.ID] = struct{}{}
					lastFailoverErr = failoverErr
					if switchCount >= maxAccountSwitches {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					switchCount++
					if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount) {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					reqLog.Warn("openai_chat_completions.upstream_failover_switching",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
						zap.Int("switch_count", switchCount),
						zap.Int("max_switches", maxAccountSwitches),
					)
					continue
				}
				h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
				upstreamErrorAlreadyCommunicated := openAIForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
				wroteFallback := false
				if !upstreamErrorAlreadyCommunicated {
					wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
				}
				reqLog.Warn("openai_chat_completions.forward_failed",
					zap.Int64("account_id", account.ID),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
					zap.Error(err),
				)
				return
			}
		}
		if result != nil {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, result.FirstTokenMs)
		} else {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, nil)
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := resolveRawCCUpstreamEndpoint(c, account)

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
					zap.String("component", "handler.openai_gateway.chat_completions"),
					zap.Int64("user_id", subject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("group_id", apiKey.GroupID),
					zap.String("model", reqModel),
					zap.Int64("account_id", account.ID),
				).Error("openai_chat_completions.record_usage_failed", zap.Error(err))
			}
		})
		reqLog.Debug("openai_chat_completions.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

func (h *OpenAIGatewayHandler) handleOrganizationChatCompletions(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	body []byte,
	requestedModel string,
	stream bool,
	reqLog *zap.Logger,
	streamStarted *bool,
) {
	shape := service.OrganizationRequestShapeHTTP
	if stream {
		shape = service.OrganizationRequestShapeStreaming
	}
	if h == nil || h.organizationExecution == nil || h.gatewayService == nil {
		h.writeOrganizationResponsesError(c, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, requestedModel, body); decision != nil && decision.Blocked {
		rejection := infraerrors.New(contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		if !h.recordOrganizationChatPreDispatchRejection(c, apiKey, body, shape, rejection, streamStarted) {
			return
		}
		h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		return
	}
	if err := h.validateOrganizationChatPreDispatch(apiKey, shape); err != nil {
		reqLog.Info("openai.organization_chat_completions_admission_rejected", zap.Error(err))
		if !h.recordOrganizationChatPreDispatchRejection(c, apiKey, body, shape, err, streamStarted) {
			return
		}
		h.writeOrganizationResponsesError(c, err, streamStarted)
		return
	}

	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, requestedModel)
	forwardBody := openAIModelMappedBody(body, channelMapping.Mapped, channelMapping.MappedModel, h.gatewayService.ReplaceModelInBody)
	plan, err := h.gatewayService.PlanOrganizationChatCandidates(c.Request.Context(), service.OrganizationChatCandidatePlanInput{
		APIKey: apiKey, Body: forwardBody, RequestShape: shape, MaximumAttempts: h.maxAccountSwitches + 1,
	})
	if err != nil {
		reqLog.Info("openai.organization_chat_completions_plan_rejected", zap.Error(err))
		if !h.recordOrganizationChatPreDispatchRejection(c, apiKey, forwardBody, shape, err, streamStarted) {
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
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	adapter := &organizationChatCompletionsAdapter{
		handler: h, c: c, apiKey: apiKey, subject: subject, subscription: subscription, plan: plan,
		requestedModel: requestedModel, sessionHash: h.gatewayService.GenerateSessionHash(c, body),
		promptCacheKey: h.gatewayService.ExtractSessionID(c, body), reqLog: reqLog, streamStarted: streamStarted,
	}
	defer adapter.releaseUserSlot()
	run, err := h.organizationExecution.ExecuteMultiAttempt(c.Request.Context(), *command, plan.MaximumAttempts, adapter)
	if err != nil {
		reqLog.Error("openai.organization_chat_completions_execution_failed", zap.Error(err))
		if !c.Writer.Written() {
			h.writeOrganizationResponsesError(c, err, streamStarted)
		}
		return
	}
	if run == nil || run.Final == nil {
		if run != nil && run.PendingReconciliation {
			reqLog.Warn("openai.organization_chat_completions_pending_reconciliation", zap.String("execution_id", run.ExecutionID))
			if !c.Writer.Written() {
				h.writeOrganizationResponsesError(c, service.ErrOrganizationExecutionPendingReconciliation, streamStarted)
			}
			return
		}
		if !c.Writer.Written() {
			h.writeOrganizationResponsesError(c, service.ErrOrganizationExecutionInvariant, streamStarted)
		}
		return
	}
	h.recordOrganizationResponsesRateLimitUsage(apiKey, run.Final)
	if run.Final.TerminalOutcome != service.OrganizationExecutionTerminalOutcomeError || c.Writer.Written() {
		return
	}
	switch adapter.zeroProof {
	case "candidate_snapshot_conflict":
		h.writeOrganizationResponsesError(c, service.ErrOrganizationChatCandidateConflict, streamStarted)
	case "account_concurrency_unavailable":
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "rate_limit_error", "No frozen account has available concurrency", streamStarted != nil && *streamStarted)
	case "organization_platform_admission_rejected":
		h.writeOrganizationResponsesAdmissionError(c, adapter.lastErr, streamStarted)
	case "user_concurrency_unavailable":
		h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "User concurrency limit reached", streamStarted != nil && *streamStarted)
	default:
		if adapter.lastFailover != nil {
			h.handleFailoverExhausted(c, adapter.lastFailover, streamStarted != nil && *streamStarted)
			return
		}
		h.ensureForwardErrorResponse(c, streamStarted != nil && *streamStarted)
	}
}

func (h *OpenAIGatewayHandler) validateOrganizationChatPreDispatch(apiKey *service.APIKey, shape string) error {
	if h == nil || h.cfg == nil || h.cfg.RunMode == config.RunModeSimple {
		return service.ErrOrganizationBillingUnavailable
	}
	if apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformOpenAI {
		return infraerrors.BadRequest("ORGANIZATION_SPENDING_UNSUPPORTED_REQUEST", "organization spending does not support this endpoint route")
	}
	return service.NewOrganizationSpendingCapabilityRegistry(h.cfg).Require(service.OrganizationEndpointFamilyChat, shape)
}

func (h *OpenAIGatewayHandler) recordOrganizationChatPreDispatchRejection(
	c *gin.Context,
	apiKey *service.APIKey,
	body []byte,
	shape string,
	rejection error,
	streamStarted *bool,
) bool {
	if h == nil || h.organizationExecution == nil {
		return true
	}
	input, err := service.NewOrganizationChatPreDispatchRejectionInput(apiKey, body, shape, organizationResponsesClientRequestID(c), rejection)
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
	h.writeOrganizationResponsesError(c, err, streamStarted)
	return false
}

type organizationChatCompletionsAdapter struct {
	handler        *OpenAIGatewayHandler
	c              *gin.Context
	apiKey         *service.APIKey
	subject        middleware2.AuthSubject
	subscription   *service.UserSubscription
	plan           *service.OrganizationChatCandidatePlan
	requestedModel string
	sessionHash    string
	promptCacheKey string
	reqLog         *zap.Logger
	streamStarted  *bool
	nextCandidate  int
	lastFailover   *service.UpstreamFailoverError
	lastErr        error
	zeroProof      string
	admissionDone  bool
	userRelease    func()
}

func (a *organizationChatCompletionsAdapter) Dispatch(ctx context.Context, request service.OrganizationExecutionDispatchRequest) (*service.OrganizationExecutionAttemptResult, error) {
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
		validated, err := a.handler.gatewayService.RevalidateOrganizationChatCandidate(ctx, a.apiKey, candidate)
		if err != nil {
			sawConflict = true
			continue
		}
		candidate = *validated
		release, acquired, err := a.handler.concurrencyHelper.TryAcquireAccountSlot(ctx, candidate.AccountID, candidate.Account.Concurrency)
		if err != nil || !acquired {
			if err != nil {
				a.lastErr = err
			}
			sawBusy = true
			continue
		}
		if release != nil {
			defer release()
		}
		if err := a.handler.gatewayService.BindStickySession(ctx, a.apiKey.GroupID, a.sessionHash, candidate.AccountID); err != nil {
			a.reqLog.Warn("openai.organization_chat_completions_bind_sticky_failed", zap.Int64("account_id", candidate.AccountID), zap.Error(err))
		}
		setOpsSelectedAccount(a.c, candidate.AccountID, candidate.Account.Platform)
		writerSizeBefore := a.c.Writer.Size()
		previousProviderRequestID := a.c.Request.Header.Get("X-Client-Request-Id")
		a.c.Request.Header.Set("X-Client-Request-Id", request.ProviderRequestID)
		dispatchCtx := service.WithOrganizationResponsesDispatchContext(ctx)
		dispatchCtx = service.WithOrganizationChatDispatchRoute(dispatchCtx, candidate.Route)
		result, forwardErr := a.handler.gatewayService.ForwardAsChatCompletions(
			dispatchCtx, a.c, candidate.Account, candidate.ForwardBody, a.promptCacheKey, "",
		)
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
	return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}, nil
}

func (a *organizationChatCompletionsAdapter) ensureAdmission(ctx context.Context) *service.OrganizationExecutionAttemptResult {
	if a.admissionDone {
		return nil
	}
	a.admissionDone = true
	if err := checkGatewayBillingEligibility(ctx, a.c, a.handler.billingCacheService, a.apiKey, a.subscription, service.QuotaPlatform(ctx, a.apiKey)); err != nil {
		a.lastErr = err
		a.zeroProof = "organization_platform_admission_rejected"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}
	}
	release, acquired := a.handler.acquireResponsesUserSlot(a.c, a.subject.UserID, a.subject.Concurrency, a.plan.Policy.Stream, a.streamStarted, a.reqLog)
	if !acquired {
		a.zeroProof = "user_concurrency_unavailable"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}
	}
	a.userRelease = release
	return nil
}

func (a *organizationChatCompletionsAdapter) releaseUserSlot() {
	if a != nil && a.userRelease != nil {
		a.userRelease()
		a.userRelease = nil
	}
}

func (a *organizationChatCompletionsAdapter) resultForForward(
	request service.OrganizationExecutionDispatchRequest,
	candidate service.OrganizationChatCandidate,
	result *service.OpenAIForwardResult,
	forwardErr error,
	writerSizeBefore int,
) (*service.OrganizationExecutionAttemptResult, error) {
	now := time.Now().UTC()
	if result != nil {
		if !result.UsageObserved {
			a.lastErr = service.ErrOrganizationExecutionPendingReconciliation
			return &service.OrganizationExecutionAttemptResult{Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, Observation: a.pendingObservation(request, candidate)}, a.lastErr
		}
		if candidate.Account.Type == service.AccountTypeOAuth {
			a.handler.gatewayService.UpdateCodexUsageSnapshotFromHeaders(a.c.Request.Context(), candidate.AccountID, result.ResponseHeaders)
		}
		rawCost, tokens, err := a.handler.gatewayService.CalculateOrganizationChatActualCost(candidate, result)
		if err != nil {
			return &service.OrganizationExecutionAttemptResult{Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, Observation: a.pendingObservation(request, candidate)}, err
		}
		terminalOutcome := service.OrganizationExecutionTerminalOutcomeSuccess
		if forwardErr != nil || result.ClientDisconnect {
			terminalOutcome = service.OrganizationExecutionTerminalOutcomeError
		}
		retryable := false
		var failoverErr *service.UpstreamFailoverError
		writerSizeUnchanged := a.c == nil || a.c.Writer == nil || a.c.Writer.Size() == writerSizeBefore
		if errors.As(forwardErr, &failoverErr) && !result.ClientDisconnect && writerSizeUnchanged {
			retryable = true
			a.lastFailover = failoverErr
			a.handler.gatewayService.RecordOpenAIAccountSwitch()
		}
		a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, terminalOutcome == service.OrganizationExecutionTerminalOutcomeSuccess, result.FirstTokenMs)
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: true, TerminalOutcome: terminalOutcome, Retryable: retryable,
			Observation: a.observation(request, candidate, result, rawCost, tokens, forwardErr, now),
		}, nil
	}

	var failoverErr *service.UpstreamFailoverError
	writerSizeUnchanged := a.c == nil || a.c.Writer == nil || a.c.Writer.Size() == writerSizeBefore
	if errors.As(forwardErr, &failoverErr) && failoverErr.ZeroCostProven && !failoverErr.NoHTTPResponse && writerSizeUnchanged {
		a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, false, nil)
		a.handler.gatewayService.RecordOpenAIAccountSwitch()
		a.lastFailover = failoverErr
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: true, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, Retryable: true,
			Observation: a.observation(request, candidate, nil, "0", service.UsageTokens{}, forwardErr, now),
		}, nil
	}
	if zeroProof, proven := organizationResponsesProvenLocalZeroProof(forwardErr); proven {
		a.lastErr = forwardErr
		a.zeroProof = zeroProof
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: zeroProof}, nil
	}
	a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, false, nil)
	a.lastErr = forwardErr
	return &service.OrganizationExecutionAttemptResult{Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, Observation: a.pendingObservation(request, candidate)}, forwardErr
}

func (a *organizationChatCompletionsAdapter) observation(
	request service.OrganizationExecutionDispatchRequest,
	candidate service.OrganizationChatCandidate,
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

func (a *organizationChatCompletionsAdapter) pendingObservation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationChatCandidate) *service.OrganizationExecutionAttemptObservation {
	routeJSON, _ := json.Marshal(candidate)
	return &service.OrganizationExecutionAttemptObservation{
		AccountID: candidate.AccountID, RouteSnapshot: routeJSON, ProviderRequestID: request.ProviderRequestID, ProviderCorrelationID: request.ProviderCorrelationID,
	}
}

// resolveRawCCUpstreamEndpoint returns the actual upstream endpoint for
// OpenAI Chat Completions requests. For APIKey accounts whose upstream
// is forced or probed to not support the Responses API, the request is
// forwarded directly to /v1/chat/completions — not through the default
// CC→Responses conversion path.
func resolveRawCCUpstreamEndpoint(c *gin.Context, account *service.Account) string {
	if account != nil && account.Type == service.AccountTypeAPIKey &&
		!openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return "/v1/chat/completions"
	}
	return GetUpstreamEndpoint(c, account.Platform)
}
