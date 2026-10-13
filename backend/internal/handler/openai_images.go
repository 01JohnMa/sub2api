package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Images handles OpenAI Images API requests.
// POST /v1/images/generations
// POST /v1/images/edits
func (h *OpenAIGatewayHandler) Images(c *gin.Context) {
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
		"handler.openai_gateway.images",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	_, organizationRequest := middleware2.GetOrganizationKeyAuthorizationFromContext(c)
	if organizationRequest {
		h.handleOrganizationImages(c, apiKey, subject, reqLog, &streamStarted)
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

	if isMultipartImagesContentType(c.GetHeader("Content-Type")) {
		setOpsRequestContext(c, "", false)
	} else {
		setOpsRequestContext(c, "", false)
	}

	parsed, err := h.gatewayService.ParseOpenAIImagesRequest(c, body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	requestModel := parsed.Model

	reqLog = reqLog.With(
		zap.String("model", requestModel),
		zap.Bool("stream", parsed.Stream),
		zap.Bool("multipart", parsed.Multipart),
		zap.String("capability", string(parsed.RequiredCapability)),
	)

	if !service.GroupAllowsImageGeneration(apiKey.Group) {
		h.errorResponse(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return
	}
	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, requestModel, parsed.ModerationBody()); decision != nil && decision.Blocked {
		h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		return
	}
	imageReleaseFunc, acquired := h.acquireImageGenerationSlot(c, streamStarted, true)
	if !acquired {
		return
	}
	if imageReleaseFunc != nil {
		defer imageReleaseFunc()
	}

	if parsed.Multipart {
		setOpsRequestContext(c, requestModel, parsed.Stream)
	} else {
		setOpsRequestContext(c, requestModel, parsed.Stream)
	}
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(parsed.Stream, false)))

	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, requestModel)

	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, parsed.Stream, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	if err := checkGatewayBillingEligibility(c.Request.Context(), c, h.billingCacheService, apiKey, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("openai.images.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	sessionHash := h.gatewayService.GenerateExplicitSessionHash(c, body)
	requestCtx := service.WithOpenAIImageGenerationIntent(c.Request.Context())

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError

	for {
		reqLog.Debug("openai.images.account_selecting", zap.Int("excluded_account_count", len(failedAccountIDs)))
		selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForImages(
			requestCtx,
			apiKey.GroupID,
			sessionHash,
			requestModel,
			failedAccountIDs,
			parsed.RequiredCapability,
		)
		if err != nil {
			reqLog.Warn("openai.images.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
			)
			if len(failedAccountIDs) == 0 {
				markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available compatible accounts", streamStarted)
				return
			}
			if lastFailoverErr != nil {
				h.handleFailoverExhausted(c, lastFailoverErr, streamStarted)
			} else {
				h.handleFailoverExhaustedSimple(c, 502, streamStarted)
			}
			return
		}
		if selection == nil || selection.Account == nil {
			markOpsRoutingCapacityLimited(c)
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available compatible accounts", streamStarted)
			return
		}

		reqLog.Debug("openai.images.account_schedule_decision",
			zap.String("layer", scheduleDecision.Layer),
			zap.Bool("sticky_session_hit", scheduleDecision.StickySessionHit),
			zap.Int("candidate_count", scheduleDecision.CandidateCount),
			zap.Int("top_k", scheduleDecision.TopK),
			zap.Int64("latency_ms", scheduleDecision.LatencyMs),
			zap.Float64("load_skew", scheduleDecision.LoadSkew),
		)

		account := selection.Account
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		reqLog.Debug("openai.images.account_selected", zap.Int64("account_id", account.ID), zap.String("account_name", account.Name))
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountReleaseFunc, acquired := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, parsed.Stream, &streamStarted, reqLog)
		if !acquired {
			return
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()
		writerSizeBeforeForward := c.Writer.Size()
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer func() {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
			}()
			return h.gatewayService.ForwardImages(requestCtx, c, account, body, parsed, channelMapping.MappedModel)
		}()
		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
		if result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
		if err != nil {
			if result != nil && result.ImageCount > 0 {
				reqLog.Warn("openai.images.forward_partial_error_with_image_result",
					zap.Int64("account_id", account.ID),
					zap.Int("image_count", result.ImageCount),
					zap.Error(err),
				)
			} else {
				var imageUpstreamErr *service.OpenAIImagesUpstreamError
				if errors.As(err, &imageUpstreamErr) {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, nil)
					reqLog.Warn("openai.images.upstream_user_error",
						zap.Int64("account_id", account.ID),
						zap.Int("status_code", imageUpstreamErr.StatusCode),
						zap.String("error_type", imageUpstreamErr.ErrorType),
						zap.String("error_code", imageUpstreamErr.Code),
						zap.Error(err),
					)
					return
				}
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
					if failoverErr.RetryableOnSameAccount {
						retryLimit := account.GetPoolModeRetryCount()
						if sameAccountRetryCount[account.ID] < retryLimit {
							sameAccountRetryCount[account.ID]++
							reqLog.Warn("openai.images.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
							)
							select {
							case <-requestCtx.Done():
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
					reqLog.Warn("openai.images.upstream_failover_switching",
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
				fields := []zap.Field{
					zap.Int64("account_id", account.ID),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
					zap.Error(err),
				}
				if shouldLogOpenAIForwardFailureAsWarn(c, wroteFallback) {
					reqLog.Warn("openai.images.forward_failed", fields...)
					return
				}
				reqLog.Error("openai.images.forward_failed", fields...)
				return
			}
		}
		if result != nil {
			if account.Type == service.AccountTypeOAuth {
				h.gatewayService.UpdateCodexUsageSnapshotFromHeaders(c.Request.Context(), account.ID, result.ResponseHeaders)
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, result.FirstTokenMs)
		} else {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, nil)
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		if parsed.Multipart {
			requestPayloadHash = service.HashUsageRequestPayload([]byte(parsed.StickySessionSeed()))
		}
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)

		upstreamModel := ""
		if result != nil {
			upstreamModel = result.UpstreamModel
		}
		h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
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
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      h.apiKeyService,
				ChannelUsageFields: channelMapping.ToUsageFields(requestModel, upstreamModel),
			}); err != nil {
				logger.L().With(
					zap.String("component", "handler.openai_gateway.images"),
					zap.Int64("user_id", subject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("group_id", apiKey.GroupID),
					zap.String("model", requestModel),
					zap.Int64("account_id", account.ID),
				).Error("openai.images.record_usage_failed", zap.Error(err))
			}
		})

		reqLog.Debug("openai.images.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

func (h *OpenAIGatewayHandler) handleOrganizationImages(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	reqLog *zap.Logger,
	streamStarted *bool,
) {
	if h == nil || h.organizationExecution == nil {
		h.writeOrganizationEmbeddingsImagesError(c, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		h.handleOrganizationImagesPreDispatchRejection(c, apiKey, nil, nil, service.ErrOrganizationEmbeddingsImagesUnsupportedRequest, streamStarted)
		return
	}
	if h.gatewayService == nil {
		h.handleOrganizationImagesPreDispatchRejection(c, apiKey, body, nil, service.ErrOrganizationBillingUnavailable, streamStarted)
		return
	}
	parsed, err := h.gatewayService.ParseOpenAIImagesRequest(c, body)
	if err != nil {
		h.handleOrganizationImagesPreDispatchRejection(c, apiKey, body, nil, service.ErrOrganizationEmbeddingsImagesUnsupportedRequest, streamStarted)
		return
	}
	shape := service.OrganizationRequestShapeGenerations
	if parsed.IsEdits() {
		shape = service.OrganizationRequestShapeEdits
	}
	setOpsRequestContext(c, parsed.Model, parsed.Stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(parsed.Stream, false)))
	if !service.GroupAllowsImageGeneration(apiKey.Group) {
		h.handleOrganizationImagesPreDispatchRejection(c, apiKey, body, parsed, infraerrors.Forbidden("ORGANIZATION_IMAGES_PERMISSION_DENIED", service.ImageGenerationPermissionMessage()), streamStarted)
		return
	}
	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, parsed.Model, parsed.ModerationBody()); decision != nil && decision.Blocked {
		rejection := infraerrors.New(contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		h.handleOrganizationImagesPreDispatchRejection(c, apiKey, body, parsed, rejection, streamStarted)
		return
	}
	if err := h.validateOrganizationEmbeddingsImagesPreDispatch(apiKey, service.OrganizationEndpointFamilyImages, shape); err != nil {
		reqLog.Info("openai.organization_images_admission_rejected", zap.Error(err))
		h.handleOrganizationImagesPreDispatchRejection(c, apiKey, body, parsed, err, streamStarted)
		return
	}
	plan, err := h.gatewayService.PlanOrganizationImagesCandidates(c.Request.Context(), service.OrganizationEmbeddingsImagesCandidatePlanInput{
		APIKey: apiKey, Body: body, ParsedImages: parsed, MaximumAttempts: h.maxAccountSwitches + 1,
	})
	if err != nil {
		reqLog.Info("openai.organization_images_plan_rejected", zap.Error(err))
		h.handleOrganizationImagesPreDispatchRejection(c, apiKey, body, parsed, err, streamStarted)
		return
	}
	imageReleaseFunc, acquired := h.acquireImageGenerationSlot(c, parsed.Stream, false)
	if !acquired {
		rejection := infraerrors.New(http.StatusTooManyRequests, "rate_limit_error", "Image generation concurrency limit exceeded, please retry later")
		h.handleOrganizationImagesPreDispatchRejection(c, apiKey, body, parsed, rejection, streamStarted)
		return
	}
	if imageReleaseFunc != nil {
		defer imageReleaseFunc()
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
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	adapter := &organizationImagesAdapter{
		handler: h, c: c, apiKey: apiKey, subject: subject, subscription: subscription, plan: plan, reqLog: reqLog, streamStarted: streamStarted,
	}
	defer adapter.releaseUserSlot()
	run, err := h.organizationExecution.ExecuteMultiAttempt(c.Request.Context(), *command, plan.MaximumAttempts, adapter)
	if err != nil {
		reqLog.Error("openai.organization_images_execution_failed", zap.Error(err))
		if !c.Writer.Written() {
			h.writeOrganizationEmbeddingsImagesError(c, err, streamStarted)
		}
		return
	}
	if run == nil || run.Final == nil {
		if run != nil && run.PendingReconciliation {
			reqLog.Warn("openai.organization_images_pending_reconciliation", zap.String("execution_id", run.ExecutionID))
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
			h.handleFailoverExhausted(c, adapter.lastFailover, streamStarted != nil && *streamStarted)
			return
		}
		if adapter.lastErr != nil {
			h.writeOrganizationEmbeddingsImagesError(c, adapter.lastErr, streamStarted)
			return
		}
		h.ensureForwardErrorResponse(c, streamStarted != nil && *streamStarted)
	}
}

func (h *OpenAIGatewayHandler) handleOrganizationImagesPreDispatchRejection(c *gin.Context, apiKey *service.APIKey, body []byte, parsed *service.OpenAIImagesRequest, rejection error, streamStarted *bool) {
	shape := service.OrganizationRequestShapeGenerations
	model := ""
	if parsed != nil {
		model = parsed.Model
		if parsed.IsEdits() {
			shape = service.OrganizationRequestShapeEdits
		}
	} else if strings.Contains(c.Request.URL.Path, "/images/edits") {
		shape = service.OrganizationRequestShapeEdits
	}
	if err := h.recordOrganizationEmbeddingsImagesPreDispatchRejection(c, apiKey, body, service.OrganizationEndpointFamilyImages, shape, model, rejection); err != nil {
		h.writeOrganizationEmbeddingsImagesError(c, err, streamStarted)
		return
	}
	h.writeOrganizationEmbeddingsImagesError(c, rejection, streamStarted)
}

type organizationImagesAdapter struct {
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

func (a *organizationImagesAdapter) Dispatch(ctx context.Context, request service.OrganizationExecutionDispatchRequest) (*service.OrganizationExecutionAttemptResult, error) {
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
		sessionHash := a.handler.gatewayService.GenerateExplicitSessionHash(a.c, validated.ForwardBody)
		release, acquired, err := a.handler.concurrencyHelper.TryAcquireAccountSlot(ctx, validated.AccountID, validated.Account.Concurrency)
		if err != nil || !acquired {
			if err != nil {
				a.lastErr = err
			}
			a.zeroProof = "account_concurrency_unavailable"
			continue
		}
		if err := a.handler.gatewayService.BindStickySession(ctx, a.apiKey.GroupID, sessionHash, validated.AccountID); err != nil {
			a.reqLog.Warn("openai.organization_images_bind_sticky_failed", zap.Int64("account_id", validated.AccountID), zap.Error(err))
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
			return a.handler.gatewayService.ForwardImages(service.WithOpenAIImageGenerationIntent(ctx), a.c, validated.Account, validated.ForwardBody, validated.ParsedImages, validated.ChannelMappedModel)
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

func (a *organizationImagesAdapter) ensureAdmission(ctx context.Context) *service.OrganizationExecutionAttemptResult {
	if a.admissionDone {
		return nil
	}
	a.admissionDone = true
	stream := a.plan.ImagesPolicy != nil && a.plan.ImagesPolicy.RequestShape != "" && a.plan.Candidates[0].ParsedImages != nil && a.plan.Candidates[0].ParsedImages.Stream
	if err := checkGatewayBillingEligibility(ctx, a.c, a.handler.billingCacheService, a.apiKey, a.subscription, service.QuotaPlatform(ctx, a.apiKey)); err != nil {
		a.lastErr = err
		a.zeroProof = "organization_platform_admission_rejected"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}
	}
	release, acquired := a.handler.acquireResponsesUserSlot(a.c, a.subject.UserID, a.subject.Concurrency, stream, a.streamStarted, a.reqLog)
	if !acquired {
		a.zeroProof = "user_concurrency_unavailable"
		return &service.OrganizationExecutionAttemptResult{Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: a.zeroProof}
	}
	a.userRelease = release
	return nil
}

func (a *organizationImagesAdapter) releaseUserSlot() {
	if a != nil && a.userRelease != nil {
		a.userRelease()
		a.userRelease = nil
	}
}

func (a *organizationImagesAdapter) resultForForward(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationEmbeddingsImagesCandidate, result *service.OpenAIForwardResult, forwardErr error, writerSizeBefore int) (*service.OrganizationExecutionAttemptResult, error) {
	now := time.Now().UTC()
	if result != nil {
		if result.ImageCount <= 0 {
			a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, forwardErr == nil, result.FirstTokenMs)
			a.lastErr = forwardErr
			if a.lastErr == nil {
				a.lastErr = service.ErrOrganizationExecutionPendingReconciliation
			}
			return &service.OrganizationExecutionAttemptResult{
				Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
				Observation: a.pendingObservation(request, candidate, result, forwardErr, now),
			}, a.lastErr
		}
		rawCost, err := a.handler.gatewayService.CalculateOrganizationImagesActualCost(candidate, result)
		if err != nil {
			a.lastErr = err
			return &service.OrganizationExecutionAttemptResult{
				Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
				Observation: a.pendingObservation(request, candidate, result, forwardErr, now),
			}, err
		}
		outcome := service.OrganizationExecutionTerminalOutcomeSuccess
		if forwardErr != nil || result.ClientDisconnect {
			outcome = service.OrganizationExecutionTerminalOutcomeError
		}
		a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, outcome == service.OrganizationExecutionTerminalOutcomeSuccess, result.FirstTokenMs)
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{Dispatched: true, TerminalOutcome: outcome, Observation: a.observation(request, candidate, result, rawCost, forwardErr, now)}, nil
	}
	if zeroProof, proven := organizationResponsesProvenLocalZeroProof(forwardErr); proven {
		a.lastErr = forwardErr
		a.zeroProof = zeroProof
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: false, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: zeroProof,
		}, nil
	}
	var failoverErr *service.UpstreamFailoverError
	writerSizeUnchanged := a.c == nil || a.c.Writer == nil || a.c.Writer.Size() == writerSizeBefore
	if errors.As(forwardErr, &failoverErr) && !failoverErr.NoHTTPResponse && writerSizeUnchanged {
		a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, false, nil)
		a.handler.gatewayService.RecordOpenAIAccountSwitch()
		a.lastFailover = failoverErr
		a.lastErr = forwardErr
		return &service.OrganizationExecutionAttemptResult{
			Dispatched: true, TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, Retryable: true,
			Observation: a.observation(request, candidate, nil, "0", forwardErr, now),
		}, nil
	}
	if errors.As(forwardErr, &failoverErr) {
		a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, false, nil)
	} else {
		var upstreamErr *service.OpenAIImagesUpstreamError
		if errors.As(forwardErr, &upstreamErr) {
			a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, true, nil)
		} else {
			a.handler.gatewayService.ReportOpenAIAccountScheduleResult(candidate.AccountID, false, nil)
		}
	}
	a.lastErr = forwardErr
	if a.lastErr == nil {
		a.lastErr = service.ErrOrganizationExecutionPendingReconciliation
	}
	return &service.OrganizationExecutionAttemptResult{
		Dispatched: true, PendingReconciliation: true, TerminalOutcome: service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
		Observation: a.pendingObservation(request, candidate, nil, forwardErr, now),
	}, a.lastErr
}

func (a *organizationImagesAdapter) observation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationEmbeddingsImagesCandidate, result *service.OpenAIForwardResult, rawCost string, forwardErr error, observedAt time.Time) *service.OrganizationExecutionAttemptObservation {
	evidence := map[string]any{"account_id": candidate.AccountID, "route": candidate.Route, "requested_model": candidate.RequestedModel, "upstream_model": candidate.UpstreamModel}
	if result != nil {
		evidence["upstream_request_id"] = result.RequestID
		evidence["image_count"] = result.ImageCount
		evidence["image_size"] = result.ImageSize
		evidence["image_output_sizes"] = result.ImageOutputSizes
		evidence["client_disconnect"] = result.ClientDisconnect
	}
	if forwardErr != nil {
		evidence["error_class"] = "forward_error"
		var upstreamErr *service.OpenAIImagesUpstreamError
		if errors.As(forwardErr, &upstreamErr) {
			evidence["upstream_status"] = upstreamErr.StatusCode
		}
		var failoverErr *service.UpstreamFailoverError
		if errors.As(forwardErr, &failoverErr) {
			evidence["upstream_status"] = failoverErr.StatusCode
		}
	}
	evidenceJSON, _ := json.Marshal(evidence)
	routeJSON, _ := json.Marshal(candidate)
	tokens := service.UsageTokens{}
	if result != nil {
		tokens = service.UsageTokens{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, CacheCreationTokens: result.Usage.CacheCreationInputTokens, CacheReadTokens: result.Usage.CacheReadInputTokens}
	}
	return &service.OrganizationExecutionAttemptObservation{AccountID: candidate.AccountID, RouteSnapshot: routeJSON, ProviderRequestID: request.ProviderRequestID, ProviderCorrelationID: request.ProviderCorrelationID, Evidence: evidenceJSON, RawActualCost: rawCost, InputTokens: int64(tokens.InputTokens), OutputTokens: int64(tokens.OutputTokens), CacheCreationTokens: int64(tokens.CacheCreationTokens), CacheReadTokens: int64(tokens.CacheReadTokens), ProviderObservedAt: observedAt}
}

func (a *organizationImagesAdapter) pendingObservation(request service.OrganizationExecutionDispatchRequest, candidate service.OrganizationEmbeddingsImagesCandidate, result *service.OpenAIForwardResult, forwardErr error, observedAt time.Time) *service.OrganizationExecutionAttemptObservation {
	return a.observation(request, candidate, result, "", forwardErr, observedAt)
}

func isMultipartImagesContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "multipart/form-data")
}
