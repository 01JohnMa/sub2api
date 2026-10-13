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
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// ChatCompletions handles OpenAI Chat Completions API endpoint for Anthropic platform groups.
// POST /v1/chat/completions
// This converts Chat Completions requests to Anthropic format (via Responses format chain),
// forwards to Anthropic upstream, and converts responses back to Chat Completions format.
func (h *GatewayHandler) ChatCompletions(c *gin.Context) {
	streamStarted := false

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.chat_completions",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	_, organizationRequest := middleware2.GetOrganizationKeyAuthorizationFromContext(c)

	// Read request body
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if organizationRequest && !h.recordOrganizationGatewayChatPreDispatchRejection(c, apiKey, nil, service.OrganizationRequestShapeHTTP, service.ErrOrganizationGatewayChatUnsupportedRequest, &streamStarted) {
			return
		}
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.chatCompletionsErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(body) == 0 {
		if organizationRequest && !h.recordOrganizationGatewayChatPreDispatchRejection(c, apiKey, body, service.OrganizationRequestShapeHTTP, service.ErrOrganizationGatewayChatUnsupportedRequest, &streamStarted) {
			return
		}
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	setOpsRequestContext(c, "", false)

	// Validate JSON
	if !gjson.ValidBytes(body) {
		if organizationRequest && !h.recordOrganizationGatewayChatPreDispatchRejection(c, apiKey, body, service.OrganizationRequestShapeHTTP, service.ErrOrganizationGatewayChatUnsupportedRequest, &streamStarted) {
			return
		}
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	// Extract model and stream
	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		if organizationRequest && !h.recordOrganizationGatewayChatPreDispatchRejection(c, apiKey, body, service.OrganizationRequestShapeHTTP, service.ErrOrganizationGatewayChatUnsupportedRequest, &streamStarted) {
			return
		}
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	reqStream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		if organizationRequest && !h.recordOrganizationGatewayChatPreDispatchRejection(c, apiKey, body, service.OrganizationRequestShapeHTTP, service.ErrOrganizationGatewayChatUnsupportedRequest, &streamStarted) {
			return
		}
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return
	}
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))
	if organizationRequest {
		h.handleOrganizationGatewayChatCompletions(c, apiKey, subject, body, reqModel, reqStream, reqLog, &streamStarted)
		return
	}

	// 解析渠道级模型映射
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)

	// Claude Code only restriction
	if apiKey.Group != nil && apiKey.Group.ClaudeCodeOnly {
		h.chatCompletionsErrorResponse(c, http.StatusForbidden, "permission_error",
			"This group is restricted to Claude Code clients (/v1/messages only)")
		return
	}

	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, reqModel, body); decision != nil && decision.Blocked {
		h.chatCompletionsErrorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		return
	}

	// Error passthrough binding
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	// 1. Acquire user concurrency slot
	maxWait := service.CalculateMaxWait(subject.Concurrency)
	canWait, err := h.concurrencyHelper.IncrementWaitCount(c.Request.Context(), subject.UserID, maxWait)
	waitCounted := false
	if err != nil {
		reqLog.Warn("gateway.cc.user_wait_counter_increment_failed", zap.Error(err))
	} else if !canWait {
		h.chatCompletionsErrorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later")
		return
	}
	if err == nil && canWait {
		waitCounted = true
	}
	defer func() {
		if waitCounted {
			h.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		}
	}()

	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted)
	if err != nil {
		reqLog.Warn("gateway.cc.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	if waitCounted {
		h.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		waitCounted = false
	}
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2. Re-check billing
	if err := checkGatewayBillingEligibility(c.Request.Context(), c, h.billingCacheService, apiKey, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("gateway.cc.billing_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.chatCompletionsErrorResponse(c, status, code, message)
		return
	}

	// Parse request for session hash
	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, _ := service.ParseGatewayRequest(bodyRef, "chat_completions")
	if parsedReq == nil {
		parsedReq = &service.ParsedRequest{Model: reqModel, Stream: reqStream, Body: bodyRef}
	}
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)
	groupPlatform := ""
	if apiKey.Group != nil {
		groupPlatform = apiKey.Group.Platform
	}
	selectionSessionHash := sessionHash
	if groupPlatform == service.PlatformGemini && selectionSessionHash != "" {
		selectionSessionHash = "gemini:" + selectionSessionHash
	}

	// 3. Account selection + failover loop
	fs := NewFailoverState(h.maxAccountSwitches, false)
	if groupPlatform == service.PlatformGemini {
		fs = NewFailoverState(h.maxAccountSwitchesGemini, false)
	}

	for {
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), apiKey.GroupID, selectionSessionHash, reqModel, fs.FailedAccountIDs, "", int64(0))
		if err != nil {
			if len(fs.FailedAccountIDs) == 0 {
				markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts: "+err.Error())
				return
			}
			action := fs.HandleSelectionExhausted(c.Request.Context())
			switch action {
			case FailoverContinue:
				continue
			case FailoverCanceled:
				return
			default:
				if fs.LastFailoverErr != nil {
					h.handleCCFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
				} else {
					h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "server_error", "All available accounts exhausted")
				}
				return
			}
		}
		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)

		// 4. Acquire account concurrency slot
		accountReleaseFunc := selection.ReleaseFunc
		if !selection.Acquired {
			if selection.WaitPlan == nil {
				markOpsRoutingCapacityLimited(c)
				h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
				return
			}
			accountReleaseFunc, err = h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
				c,
				account.ID,
				selection.WaitPlan.MaxConcurrency,
				selection.WaitPlan.Timeout,
				reqStream,
				&streamStarted,
			)
			if err != nil {
				reqLog.Warn("gateway.cc.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				h.handleConcurrencyError(c, err, "account", streamStarted)
				return
			}
		}
		accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

		if groupPlatform == service.PlatformGemini && account.Platform != service.PlatformGemini {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			fs.FailedAccountIDs[account.ID] = struct{}{}
			continue
		}

		// 5. Forward request
		writerSizeBeforeForward := c.Writer.Size()
		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		var result *service.ForwardResult
		if account.Platform == service.PlatformGemini {
			if h.geminiCompatService == nil {
				h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", "Gemini compatibility service is not configured")
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				return
			}
			result, err = h.geminiCompatService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody)
		} else {
			result, err = h.gatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, parsedReq)
		}

		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}

		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if c.Writer.Size() != writerSizeBeforeForward {
					h.handleCCFailoverExhausted(c, failoverErr, true)
					return
				}
				action := fs.HandleFailoverError(c.Request.Context(), h.gatewayService, account.ID, account.Platform, failoverErr)
				switch action {
				case FailoverContinue:
					continue
				case FailoverExhausted:
					h.handleCCFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
					return
				case FailoverCanceled:
					return
				}
			}
			upstreamErrorAlreadyCommunicated := gatewayForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
			wroteFallback := false
			if !upstreamErrorAlreadyCommunicated {
				wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
			}
			reqLog.Error("gateway.cc.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Bool("fallback_error_response_written", wroteFallback),
				zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
				zap.Error(err),
			)
			return
		}

		// 6. Record usage
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)

		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
		h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
			if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
				Result:             result,
				QuotaPlatform:      quotaPlatform,
				APIKey:             apiKey,
				User:               apiKey.User,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      h.apiKeyService,
				ChannelUsageFields: channelMapping.ToUsageFields(reqModel, result.UpstreamModel),
			}); err != nil {
				reqLog.Error("gateway.cc.record_usage_failed",
					zap.Int64("account_id", account.ID),
					zap.Error(err),
				)
			}
		})
		return
	}
}

// chatCompletionsErrorResponse writes an error in OpenAI Chat Completions format.
func (h *GatewayHandler) chatCompletionsErrorResponse(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

func (h *GatewayHandler) handleOrganizationGatewayChatCompletions(
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
	if h == nil || h.organizationExecution == nil {
		h.writeOrganizationGatewayChatError(c, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	if h.gatewayService == nil {
		if !h.recordOrganizationGatewayChatPreDispatchRejection(c, apiKey, body, requestShape, service.ErrOrganizationBillingUnavailable, streamStarted) {
			return
		}
		h.writeOrganizationGatewayChatError(c, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	if apiKey != nil && apiKey.Group != nil && apiKey.Group.ClaudeCodeOnly {
		rejection := infraerrors.Forbidden(
			"ORGANIZATION_GATEWAY_CHAT_CLAUDE_CODE_ONLY",
			"this group is restricted to Claude Code clients (/v1/messages only)",
		)
		if !h.recordOrganizationGatewayChatPreDispatchRejection(c, apiKey, body, requestShape, rejection, streamStarted) {
			return
		}
		h.chatCompletionsErrorResponse(c, http.StatusForbidden, "permission_error", "This group is restricted to Claude Code clients (/v1/messages only)")
		return
	}
	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, requestedModel, body); decision != nil && decision.Blocked {
		rejection := infraerrors.New(contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		if !h.recordOrganizationGatewayChatPreDispatchRejection(c, apiKey, body, requestShape, rejection, streamStarted) {
			return
		}
		h.chatCompletionsErrorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		return
	}
	if err := h.validateOrganizationGatewayChatPreDispatch(apiKey, requestShape); err != nil {
		reqLog.Info("gateway.organization_chat_completions_admission_rejected", zap.Error(err))
		if !h.recordOrganizationGatewayChatPreDispatchRejection(c, apiKey, body, requestShape, err, streamStarted) {
			return
		}
		h.writeOrganizationGatewayChatError(c, err, streamStarted)
		return
	}
	plan, err := h.gatewayService.PlanOrganizationGatewayChatCandidates(c.Request.Context(), service.OrganizationGatewayChatCandidatePlanInput{
		APIKey: apiKey, Body: body, RequestShape: requestShape, MaximumAttempts: h.organizationGatewayChatMaximumAttempts(apiKey.Group.Platform),
	})
	if err != nil {
		reqLog.Info("gateway.organization_chat_completions_plan_rejected", zap.Error(err))
		if !h.recordOrganizationGatewayChatPreDispatchRejection(c, apiKey, body, requestShape, err, streamStarted) {
			return
		}
		h.writeOrganizationGatewayChatError(c, err, streamStarted)
		return
	}
	reservationInput, err := plan.NewExecutionReservationInput(apiKey, organizationGatewayChatClientRequestID(c))
	if err != nil {
		h.writeOrganizationGatewayChatError(c, err, streamStarted)
		return
	}
	command, err := h.organizationExecution.NewReservationCommand(reservationInput)
	if err != nil {
		h.writeOrganizationGatewayChatError(c, err, streamStarted)
		return
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	adapter := &organizationGatewayChatAdapter{
		handler: h, c: c, apiKey: apiKey, subject: subject, subscription: subscription, plan: plan,
		requestModel: requestedModel, reqLog: reqLog, streamStarted: streamStarted,
	}
	defer adapter.releaseUserSlot()
	run, err := h.organizationExecution.ExecuteMultiAttempt(c.Request.Context(), *command, plan.MaximumAttempts, adapter)
	if err != nil {
		reqLog.Error("gateway.organization_chat_completions_execution_failed", zap.Error(err))
		if !c.Writer.Written() {
			h.writeOrganizationGatewayChatError(c, err, streamStarted)
		}
		return
	}
	if run == nil || run.PendingReconciliation || run.Final == nil {
		if !c.Writer.Written() {
			if run != nil && run.PendingReconciliation {
				h.writeOrganizationGatewayChatError(c, service.ErrOrganizationExecutionPendingReconciliation, streamStarted)
			} else {
				h.writeOrganizationGatewayChatError(c, service.ErrOrganizationExecutionInvariant, streamStarted)
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
		h.writeOrganizationGatewayChatError(c, service.ErrOrganizationGatewayChatCandidateConflict, streamStarted)
	case "user_concurrency_unavailable":
		h.chatCompletionsErrorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "User concurrency limit reached")
	case "account_concurrency_unavailable":
		h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "rate_limit_error", "No frozen account has available concurrency")
	case "organization_platform_admission_rejected":
		h.writeOrganizationGatewayChatError(c, adapter.lastErr, streamStarted)
	default:
		if adapter.lastFailover != nil {
			h.handleCCFailoverExhausted(c, adapter.lastFailover, streamStarted != nil && *streamStarted)
			return
		}
		h.ensureForwardErrorResponse(c, streamStarted != nil && *streamStarted)
	}
}

func (h *GatewayHandler) organizationGatewayChatMaximumAttempts(platform string) int {
	maxSwitches := h.maxAccountSwitches
	if platform == service.PlatformGemini {
		maxSwitches = h.maxAccountSwitchesGemini
	}
	return maxSwitches + 1
}

func (h *GatewayHandler) validateOrganizationGatewayChatPreDispatch(apiKey *service.APIKey, requestShape string) error {
	if h == nil || h.cfg == nil || h.cfg.RunMode == config.RunModeSimple {
		return service.ErrOrganizationBillingUnavailable
	}
	if apiKey == nil || apiKey.Group == nil || (apiKey.Group.Platform != service.PlatformAnthropic && apiKey.Group.Platform != service.PlatformGemini) {
		return service.ErrOrganizationGatewayChatUnsupportedRequest
	}
	return service.NewOrganizationSpendingCapabilityRegistry(h.cfg).Require(service.OrganizationEndpointFamilyChat, requestShape)
}

func (h *GatewayHandler) recordOrganizationGatewayChatPreDispatchRejection(c *gin.Context, apiKey *service.APIKey, body []byte, requestShape string, rejection error, streamStarted *bool) bool {
	if h == nil || h.organizationExecution == nil {
		return true
	}
	input, err := service.NewOrganizationChatPreDispatchRejectionInput(apiKey, body, requestShape, organizationGatewayChatClientRequestID(c), rejection)
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
	h.writeOrganizationGatewayChatError(c, err, streamStarted)
	return false
}

func (h *GatewayHandler) writeOrganizationGatewayChatError(c *gin.Context, err error, streamStarted *bool) {
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

func organizationGatewayChatClientRequestID(c *gin.Context) string {
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

func (h *GatewayHandler) recordOrganizationGatewayChatRateLimitUsage(apiKey *service.APIKey, final *service.OrganizationFinalExecutionEvidenceResult) {
	if h == nil || h.billingCacheService == nil || apiKey == nil || !apiKey.HasRateLimits() || final == nil || strings.TrimSpace(final.RawActualCost) == "" || final.RawActualCost == "0" {
		return
	}
	_ = h.billingCacheService.InvalidateAPIKeyRateLimit(context.Background(), apiKey.ID)
}

type organizationGatewayChatAdapter struct {
	handler       *GatewayHandler
	c             *gin.Context
	apiKey        *service.APIKey
	subject       middleware2.AuthSubject
	subscription  *service.UserSubscription
	plan          *service.OrganizationGatewayChatCandidatePlan
	requestModel  string
	reqLog        *zap.Logger
	streamStarted *bool
	nextCandidate int
	lastFailover  *service.UpstreamFailoverError
	lastErr       error
	zeroProof     string
	admissionDone bool
	userRelease   func()
}

func (a *organizationGatewayChatAdapter) Dispatch(ctx context.Context, request service.OrganizationExecutionDispatchRequest) (*service.OrganizationExecutionAttemptResult, error) {
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
		validated, err := a.handler.gatewayService.RevalidateOrganizationGatewayChatCandidate(ctx, a.apiKey, candidate, a.plan.Policy, a.plan.SourceBody)
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
		if err := a.handler.gatewayService.BindStickySession(ctx, a.apiKey.GroupID, a.sessionHash(candidate.ForwardBody), candidate.AccountID); err != nil {
			a.reqLog.Warn("gateway.organization_chat_completions_bind_sticky_failed", zap.Int64("account_id", candidate.AccountID), zap.Error(err))
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
	return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}, nil
}

func (a *organizationGatewayChatAdapter) ensureAdmission(ctx context.Context) *service.OrganizationExecutionAttemptResult {
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
	}
	if err != nil || !acquired {
		a.zeroProof = "user_concurrency_unavailable"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}
	}
	a.userRelease = release
	return nil
}

func (a *organizationGatewayChatAdapter) releaseUserSlot() {
	if a != nil && a.userRelease != nil {
		a.userRelease()
		a.userRelease = nil
	}
}

func (a *organizationGatewayChatAdapter) sessionHash(body []byte) string {
	parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(body), "chat_completions")
	if err == nil && parsed != nil {
		parsed.SessionContext = &service.SessionContext{ClientIP: ip.GetClientIP(a.c), UserAgent: a.c.GetHeader("User-Agent"), APIKeyID: a.apiKey.ID}
		return a.handler.gatewayService.GenerateSessionHash(parsed)
	}
	return ""
}

func (a *organizationGatewayChatAdapter) forward(ctx context.Context, candidate service.OrganizationGatewayChatCandidate) (*service.ForwardResult, error) {
	if candidate.Account == nil {
		return nil, service.ErrOrganizationGatewayChatCandidateConflict
	}
	if candidate.Route.Platform == service.PlatformGemini {
		if a.handler.geminiCompatService == nil {
			return nil, service.ErrOrganizationBillingUnavailable
		}
		return a.handler.geminiCompatService.ForwardAsChatCompletionsOrganizationAttempt(ctx, a.c, candidate.Account, candidate.ForwardBody)
	}
	return a.handler.gatewayService.ForwardAsChatCompletionsOrganizationAttempt(ctx, a.c, candidate.Account, candidate.ForwardBody, nil)
}

func (a *organizationGatewayChatAdapter) resultForForward(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationGatewayChatCandidate, result *service.ForwardResult, forwardErr error, writerSizeBefore int) (*service.OrganizationExecutionAttemptResult, error) {
	now := time.Now().UTC()
	writerSizeUnchanged := a.c == nil || a.c.Writer == nil || a.c.Writer.Size() == writerSizeBefore
	if result != nil {
		if a.handler == nil || a.handler.gatewayService == nil {
			pendingErr := forwardErr
			if pendingErr == nil {
				pendingErr = service.ErrOrganizationBillingUnavailable
			}
			a.lastErr = pendingErr
			return &service.OrganizationExecutionAttemptResult{
				Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
				Observation: a.pendingObservation(request, candidate),
			}, pendingErr
		}
		rawCost, tokens, err := a.handler.gatewayService.CalculateOrganizationGatewayChatActualCost(candidate, result)
		if errors.Is(err, service.ErrOrganizationExecutionPendingReconciliation) {
			a.lastErr = err
			return &service.OrganizationExecutionAttemptResult{Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, Observation: a.pendingObservation(request, candidate)}, err
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
		retryable := false
		if forwardErr != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(forwardErr, &failoverErr) && writerSizeUnchanged && !result.ClientDisconnect {
				a.lastFailover = failoverErr
				retryable = true
			}
		}
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: true, TerminalOutcome: terminalOutcome, Retryable: retryable,
			Observation: a.observation(request, candidate, result, rawCost, tokens, forwardErr, now),
		}, nil
	}
	var failoverErr *service.UpstreamFailoverError
	if errors.As(forwardErr, &failoverErr) && !failoverErr.NoHTTPResponse && failoverErr.ZeroCostProven && writerSizeUnchanged {
		a.lastFailover = failoverErr
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: true, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, Retryable: true,
			Observation: a.observation(request, candidate, nil, "0", service.UsageTokens{}, forwardErr, now),
		}, nil
	}
	var localErr *service.OpenAILocalPreDispatchError
	if errors.As(forwardErr, &localErr) {
		a.lastErr = forwardErr
		a.zeroProof = localErr.ZeroProof
		if a.zeroProof == "" {
			a.zeroProof = "local_policy_rejected"
		}
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof,
		}, nil
	}
	if errors.Is(forwardErr, service.ErrOrganizationBillingUnavailable) {
		a.lastErr = forwardErr
		a.zeroProof = "compatibility_adapter_unavailable"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}, nil
	}
	a.lastErr = forwardErr
	return &service.OrganizationExecutionAttemptResult{
		Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
		Observation: a.pendingObservation(request, candidate),
	}, forwardErr
}

func (a *organizationGatewayChatAdapter) observation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationGatewayChatCandidate, result *service.ForwardResult, rawCost string, tokens service.UsageTokens, forwardErr error, observedAt time.Time) *service.OrganizationExecutionAttemptObservation {
	evidence := map[string]any{
		"account_id": candidate.AccountID, "route": candidate.Route, "requested_model": a.requestModel,
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

func (a *organizationGatewayChatAdapter) pendingObservation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationGatewayChatCandidate) *service.OrganizationExecutionAttemptObservation {
	routeJSON, _ := json.Marshal(candidate.Route)
	return &service.OrganizationExecutionAttemptObservation{
		AccountID: candidate.AccountID, RouteSnapshot: routeJSON, ProviderRequestID: request.ProviderRequestID, ProviderCorrelationID: request.ProviderCorrelationID,
	}
}

// handleCCFailoverExhausted writes a failover-exhausted error in CC format.
func (h *GatewayHandler) handleCCFailoverExhausted(c *gin.Context, lastErr *service.UpstreamFailoverError, streamStarted bool) {
	if streamStarted {
		return
	}
	statusCode := http.StatusBadGateway
	if lastErr != nil && lastErr.StatusCode > 0 {
		statusCode = lastErr.StatusCode
	}
	if lastErr != nil && service.IsOpenAISilentRefusalErrorBody(lastErr.ResponseBody) {
		service.SetOpsUpstreamError(c, statusCode, service.OpenAISilentRefusalClientMessage(), "")
		h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", service.OpenAISilentRefusalClientMessage())
		return
	}
	h.chatCompletionsErrorResponse(c, statusCode, "server_error", "All available accounts exhausted")
}
