package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"github.com/MACOS-DO/sub4api/internal/pkg/ip"
	"github.com/MACOS-DO/sub4api/internal/pkg/logger"
	"github.com/MACOS-DO/sub4api/internal/pkg/openai_compat"
	middleware2 "github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"

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

	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if organizationRequest && !h.recordOrganizationChatPreDispatchRejection(c, apiKey, nil, service.OrganizationRequestShapeHTTP, err, &streamStarted) {
			return
		}
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		logRequestBodyReadFailure(reqLog, c.Request, err)
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
		logRequestBodyParseFailure(reqLog, body, nil)
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
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !openAICompatibleTextTargetAllowed(c, apiKey, reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}
	if cappedBody, changed, err := applyOpenAIReasoningEffortPolicyForRequest(c, apiKey, body); err != nil {
		respondOpenAIReasoningEffortPolicyError(c, err, h.errorResponse)
		return
	} else if changed {
		body = cappedBody
	}
	reqStream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		if organizationRequest && !h.recordOrganizationChatPreDispatchRejection(c, apiKey, body, service.OrganizationRequestShapeHTTP, service.ErrOrganizationChatUnsupportedRequest, &streamStarted) {
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return
	}
	if _, err := service.ValidateOpenAIServiceTierField(body); err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if service.IsGPTImageGenerationModel(reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "This model is not supported on the Chat Completions endpoint")
		return
	}

	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))
	if organizationRequest {
		h.handleOrganizationChatCompletions(c, apiKey, subject, body, reqModel, reqStream, reqLog, &streamStarted)
		return
	}

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}
	if h.rejectIfCyberSessionBlocked(c, apiKey, body, reqModel, cyberBlockFormatChat) {
		return
	}

	// 解析渠道级模型映射
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
	forwardModel := openAIChannelForwardModel(channelMapping, reqModel)

	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestPlatform := openAICompatibleRequestPlatform(c.Request.Context(), apiKey)

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

	// 余额模式在途预留：防止并发请求在预检时看到同一份余额而集体透支。
	inflightRelease, err := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, tokenInflightEstimate(reqModel, body))
	if err != nil {
		reqLog.Info("openai_chat_completions.inflight_reservation_rejected", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}
	defer inflightRelease()

	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	promptCacheKey := h.gatewayService.ExtractSessionID(c, body)

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	profitVetoCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	var oauth429FailoverState service.OpenAIOAuth429FailoverState

	// 分组利润控制：chat completions 文本入口请求级装门并固定 pricingAt。
	ccPricingCtx, pricingAt := h.gatewayService.WithOpenAIRequestPricingContext(c.Request.Context(), apiKey.GroupID)
	c.Request = c.Request.WithContext(ccPricingCtx)

	for {
		if failoverClientGone(c) {
			return
		}
		reqLog.Debug("openai_chat_completions.account_selecting", zap.Int("excluded_account_count", len(failedAccountIDs)))
		selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(),
			apiKey.GroupID,
			"",
			sessionHash,
			forwardModel,
			failedAccountIDs,
			service.OpenAIUpstreamTransportAny,
			service.OpenAIEndpointCapabilityChatCompletions,
			false,
			false,
			true,
			requestPlatform,
		)
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("openai_chat_completions.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			reqLog.Warn("openai_chat_completions.account_select_failed",
				zap.Error(openAICompatibleSelectionErrorForLog(err, requestPlatform)),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
			)
			if len(failedAccountIDs) == 0 {
				cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel)
				cls = classifySelectionFailureError(err, cls)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
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
			cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
			return
		}
		account := selection.Account
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		reqLog.Debug("openai_chat_completions.account_selected", zap.Int64("account_id", account.ID), zap.String("account_name", account.Name))
		_ = scheduleDecision
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountReleaseFunc, slotResult := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, reqStream, &streamStarted, reqLog)
		if slotResult == openAISlotAcquireProfitVetoed {
			// 利润终检否决：排除该账号重新选号；否决次数达上限则按无可用账号终止。
			if !recordOpenAIProfitVeto(failedAccountIDs, account.ID, &profitVetoCount) {
				h.handleOpenAIProfitVetoExhausted(c, streamStarted, reqLog, profitVetoCount)
				return
			}
			continue
		}
		if slotResult != openAISlotAcquireOK {
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
		var cyberBlockBodyChat []byte
		if service.GetOpsCyberPolicy(c) != nil {
			cyberBlockBodyChat = body
		}
		h.recordCyberPolicyIfMarked(c, apiKey, account, subscription, reqModel, err != nil, cyberBlockBodyChat, clientRequestedUsageFields(c, channelMapping, reqModel, ""), service.HashUsageRequestPayload(body))

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
		// #5148 对齐：错误返回携带的部分 result（流中断前上游已计量的 usage）照常
		// 入账；failover 错误恒定 result=nil，不会重复计费。
		submitChatUsage := func(res *service.OpenAIForwardResult) {
			if res == nil {
				return
			}
			stampOpenAIRequestedReasoningEffort(res, c)
			userAgent := c.GetHeader("User-Agent")
			clientIP := ip.GetClientIP(c)
			inboundEndpoint := GetInboundEndpoint(c)
			upstreamEndpoint := resolveOpenAIUpstreamEndpoint(c, account, res)
			quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
			sessionID := service.ExtractClientSessionID(c)
			cyberBlocked := service.GetOpsCyberPolicy(c) != nil
			h.submitOpenAIUsageRecordTask(c.Request.Context(), res, func(ctx context.Context) {
				if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
					Result:             res,
					APIKey:             apiKey,
					User:               apiKey.User,
					Account:            account,
					Subscription:       subscription,
					InboundEndpoint:    inboundEndpoint,
					UpstreamEndpoint:   upstreamEndpoint,
					UserAgent:          userAgent,
					IPAddress:          clientIP,
					APIKeyService:      h.apiKeyService,
					QuotaPlatform:      quotaPlatform,
					SessionID:          sessionID,
					ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, res.UpstreamModel),
					PricingAt:          pricingAt,
					CyberBlocked:       cyberBlocked,
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
					if failoverClientGone(c) {
						reqLog.Info("openai_chat_completions.failover_aborted_client_disconnected",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						return
					}
					if c.Writer.Size() != writerSizeBeforeForward {
						h.gatewayService.ObserveOpenAIAccountHealthFailure(c.Request.Context(), account, err)
						h.handleFailoverExhausted(c, failoverErr, true)
						return
					}
					if failoverErr.ShouldReportAccountScheduleFailure() {
						h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, nil), false, nil, err)
					}
					if !failoverErr.ShouldRetryNextAccount() {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					// Pool mode: retry on the same account
					if failoverErr.RetryableOnSameAccount {
						retryLimit := effectiveSameAccountRetryLimit(failoverErr, account)
						if sameAccountRetryAllowed(failoverErr, sameAccountRetryCount[account.ID], retryLimit) {
							sameAccountRetryCount[account.ID]++
							retryDelay := sameAccountRetryDelayFor(failoverErr, sameAccountRetryCount[account.ID])
							reqLog.Warn("openai_chat_completions.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
								zap.Duration("retry_delay", retryDelay),
							)
							select {
							case <-c.Request.Context().Done():
								return
							case <-time.After(retryDelay):
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
					if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount, &oauth429FailoverState) {
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
				h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, nil), false, nil, err)
				upstreamErrorAlreadyCommunicated := openAIForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
				wroteFallback := false
				if !upstreamErrorAlreadyCommunicated {
					wroteFallback = h.ensureOpenAIStreamReadErrorResponse(c, err, streamStarted)
					if !wroteFallback {
						wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
					}
				}
				reqLog.Warn("openai_chat_completions.forward_failed",
					zap.Int64("account_id", account.ID),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
					zap.Error(err),
				)
				submitChatUsage(result)
				return
			}
		}
		if result != nil {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, result), true, result.FirstTokenMs)
		} else {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, result), true, nil)
		}

		submitChatUsage(result)
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
		// 组织执行不参与个人调度统计：candidate 仅持有账号快照，账号健康
		// 由组织执行服务自行管理（上游 5 参 Report 绑定完整 *Account）。
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: true, TerminalOutcome: terminalOutcome, Retryable: retryable,
			Observation: a.observation(request, candidate, result, rawCost, tokens, forwardErr, now),
		}, nil
	}

	var failoverErr *service.UpstreamFailoverError
	writerSizeUnchanged := a.c == nil || a.c.Writer == nil || a.c.Writer.Size() == writerSizeBefore
	if errors.As(forwardErr, &failoverErr) && failoverErr.ZeroCostProven && !failoverErr.NoHTTPResponse && writerSizeUnchanged {
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

// resolveOpenAIUpstreamEndpoint returns the actual upstream endpoint for an
// OpenAI-compatible account. A forwarding result is authoritative because a
// single inbound route may choose raw Chat or a Responses bridge at runtime.
// The account-based derivation remains as a fallback for existing callers and
// forwarding paths that do not report their endpoint yet.
func resolveOpenAIUpstreamEndpoint(c *gin.Context, account *service.Account, result *service.OpenAIForwardResult) string {
	if result != nil {
		if endpoint := strings.TrimSpace(result.UpstreamEndpoint); endpoint != "" {
			return endpoint
		}
	}
	if endpoint := service.GetActualOpenAIUpstreamEndpoint(c); endpoint != "" {
		return endpoint
	}
	if account != nil && account.Type == service.AccountTypeAPIKey &&
		!openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return EndpointChatCompletions
	}
	return GetUpstreamEndpoint(c, account.Platform)
}
