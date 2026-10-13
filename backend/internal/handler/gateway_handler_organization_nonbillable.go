package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/gemini"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const organizationNonBillableResponseCaptureLimit = 1 << 20

type organizationNonBillableAdapterFunc func(context.Context, service.OrganizationNonBillableDispatchRequest) (*service.OrganizationNonBillableDispatchResult, error)

func (f organizationNonBillableAdapterFunc) Dispatch(ctx context.Context, request service.OrganizationNonBillableDispatchRequest) (*service.OrganizationNonBillableDispatchResult, error) {
	return f(ctx, request)
}

func (h *GatewayHandler) requireOrganizationNonBillable(c *gin.Context, apiKey *service.APIKey, family, shape, platform string) error {
	if h == nil || h.organizationExecution == nil || h.billingCacheService == nil || h.cfg == nil {
		return service.ErrOrganizationBillingUnavailable
	}
	if err := service.NewOrganizationSpendingCapabilityRegistry(h.cfg).RequireNonBillable(family, shape); err != nil {
		return err
	}
	if apiKey == nil || apiKey.User == nil {
		return service.ErrOrganizationBillingUnavailable
	}
	return h.billingCacheService.CheckOrganizationNonBillablePlatformEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, platform)
}

func organizationNonBillableAttribution(apiKey *service.APIKey) (service.OrganizationNonBillableExecutionTrustedAttribution, error) {
	if apiKey == nil || apiKey.User == nil || apiKey.OrganizationID == nil || apiKey.OrganizationAuthorization == nil || apiKey.User.OrganizationID == nil {
		return service.OrganizationNonBillableExecutionTrustedAttribution{}, service.ErrOrganizationBillingUnavailable
	}
	auth := apiKey.OrganizationAuthorization
	return service.OrganizationNonBillableExecutionTrustedAttribution{
		OrganizationID:            auth.OrganizationID,
		OrganizationStatus:        auth.OrganizationStatus,
		UserID:                    apiKey.User.ID,
		UserOrganizationID:        dereferenceOrganizationID(apiKey.User.OrganizationID),
		APIKeyID:                  apiKey.ID,
		APIKeyUserID:              apiKey.UserID,
		APIKeyOrganizationID:      dereferenceOrganizationID(apiKey.OrganizationID),
		AllocationID:              auth.AllocationID,
		AllocationUserID:          auth.AllocationUserID,
		AllocationOrganizationID:  auth.AllocationOrganizationID,
		AllocationStatus:          auth.AllocationStatus,
		AllocationVersionSnapshot: auth.AllocationVersion,
	}, nil
}

func dereferenceOrganizationID(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func organizationNonBillableRequestID(c *gin.Context) string {
	if c != nil {
		for _, header := range []string{"X-Request-ID", "X-Client-Request-ID"} {
			if value := strings.TrimSpace(c.GetHeader(header)); value != "" {
				return value
			}
		}
	}
	return uuid.NewString()
}

func (h *GatewayHandler) newOrganizationNonBillableCommand(c *gin.Context, apiKey *service.APIKey, family, shape, model string, body []byte) (*service.OrganizationNonBillableExecutionCommand, error) {
	if h == nil || h.organizationExecution == nil {
		return nil, service.ErrOrganizationBillingUnavailable
	}
	attribution, err := organizationNonBillableAttribution(apiKey)
	if err != nil {
		return nil, err
	}
	path := ""
	method := ""
	if c != nil && c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
		method = c.Request.Method
	}
	bodyDigest := sha256.Sum256(body)
	snapshot, err := json.Marshal(map[string]any{
		"method":                      method,
		"path":                        path,
		"family":                      family,
		"shape":                       shape,
		"model":                       strings.TrimSpace(model),
		"body_sha256":                 hex.EncodeToString(bodyDigest[:]),
		"api_key_id":                  apiKey.ID,
		"organization_id":             attribution.OrganizationID,
		"allocation_id":               attribution.AllocationID,
		"allocation_status":           attribution.AllocationStatus,
		"allocation_version_snapshot": attribution.AllocationVersionSnapshot,
	})
	if err != nil {
		return nil, err
	}
	pricing := []byte(`{"organization_settled_cost":"0.00000000","raw_actual_cost":"0"}`)
	requestID := organizationNonBillableRequestID(c)
	fingerprintInput := bytes.Join([][]byte{snapshot, []byte{0}, pricing, []byte{0}, []byte(requestID)}, nil)
	fingerprint := sha256.Sum256(fingerprintInput)
	return h.organizationExecution.NewNonBillableExecutionCommand(service.OrganizationNonBillableExecutionInput{
		Attribution:        attribution,
		EndpointFamily:     family,
		RequestShape:       shape,
		Model:              strings.TrimSpace(model),
		ClientRequestID:    requestID,
		CandidateSnapshot:  snapshot,
		PricingSnapshot:    pricing,
		BillingFingerprint: hex.EncodeToString(fingerprint[:]),
	})
}

func (h *GatewayHandler) executeOrganizationLocalNonBillable(c *gin.Context, apiKey *service.APIKey, family, shape, model, proof string, body []byte) error {
	return h.executeOrganizationLocalNonBillableOutcome(c, apiKey, family, shape, model, proof, body, service.OrganizationExecutionTerminalOutcomeSuccess, false)
}

func (h *GatewayHandler) executeOrganizationLocalNonBillableOutcome(c *gin.Context, apiKey *service.APIKey, family, shape, model, proof string, body []byte, outcome string, businessLimited bool) error {
	command, err := h.newOrganizationNonBillableCommand(c, apiKey, family, shape, model, body)
	if err != nil {
		return err
	}
	_, err = h.organizationExecution.ExecuteNonBillable(c.Request.Context(), *command, organizationNonBillableAdapterFunc(func(context.Context, service.OrganizationNonBillableDispatchRequest) (*service.OrganizationNonBillableDispatchResult, error) {
		return &service.OrganizationNonBillableDispatchResult{
			TerminalOutcome: outcome,
			BusinessLimited: businessLimited,
			ZeroProof:       proof,
		}, nil
	}))
	return err
}

func (h *GatewayHandler) organizationLocalAdmission(c *gin.Context, apiKey *service.APIKey, family, shape, model, proof string, body []byte, platform string) error {
	if err := h.requireOrganizationNonBillable(c, apiKey, family, shape, platform); err != nil {
		return err
	}
	return h.executeOrganizationLocalNonBillable(c, apiKey, family, shape, model, proof, body)
}

func (h *GatewayHandler) recordOrganizationCountTokensPreDispatchRejection(c *gin.Context, apiKey *service.APIKey, model string, body []byte, proof string) error {
	if err := h.requireOrganizationNonBillable(c, apiKey, service.OrganizationEndpointFamilyTokenCounting, service.OrganizationRequestShapeProbe, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		return err
	}
	return h.executeOrganizationLocalNonBillableOutcome(
		c,
		apiKey,
		service.OrganizationEndpointFamilyTokenCounting,
		service.OrganizationRequestShapeProbe,
		model,
		proof,
		body,
		service.OrganizationExecutionTerminalOutcomeError,
		true,
	)
}

func (h *GatewayHandler) usageOrganization(c *gin.Context, apiKey *service.APIKey, usageData gin.H, dailyUsage any, modelStats any) {
	resp := gin.H{
		"mode":    "organization",
		"isValid": apiKey.Status == service.StatusAPIKeyActive,
		"status":  apiKey.Status,
	}
	if apiKey.Quota > 0 {
		remaining := apiKey.GetQuotaRemaining()
		resp["quota"] = gin.H{"limit": apiKey.Quota, "used": apiKey.QuotaUsed, "remaining": remaining, "unit": "USD"}
		resp["remaining"] = remaining
		resp["unit"] = "USD"
	}
	if usageData != nil {
		resp["usage"] = usageData
	}
	if dailyUsage != nil {
		resp["daily_usage"] = dailyUsage
	}
	if modelStats != nil {
		resp["model_stats"] = modelStats
	}
	c.JSON(http.StatusOK, resp)
}

func (h *GatewayHandler) handleOrganizationCountTokens(c *gin.Context, apiKey *service.APIKey, parsed *service.ParsedRequest, reqLog *zap.Logger) {
	if parsed == nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	body := parsed.Body.Bytes()
	platform := service.QuotaPlatform(c.Request.Context(), apiKey)
	if err := h.requireOrganizationNonBillable(c, apiKey, service.OrganizationEndpointFamilyTokenCounting, service.OrganizationRequestShapeProbe, platform); err != nil {
		h.writeOrganizationNonBillableError(c, err, false)
		return
	}
	if platform == service.PlatformOpenAI {
		if err := h.executeOrganizationLocalNonBillableOutcome(c, apiKey, service.OrganizationEndpointFamilyTokenCounting, service.OrganizationRequestShapeProbe, parsed.Model, "openai_count_tokens_unsupported", body, service.OrganizationExecutionTerminalOutcomeError, true); err != nil {
			h.writeOrganizationNonBillableError(c, err, false)
			return
		}
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{"type": "error", "error": gin.H{"type": "not_found_error", "message": "Token counting is not supported for this platform"}})
		return
	}
	command, err := h.newOrganizationNonBillableCommand(c, apiKey, service.OrganizationEndpointFamilyTokenCounting, service.OrganizationRequestShapeProbe, parsed.Model, body)
	if err != nil {
		h.writeOrganizationNonBillableError(c, err, false)
		return
	}
	var selected *service.Account
	var capture *organizationNonBillableResponseCapture
	forwardErr := error(nil)
	final, err := h.organizationExecution.ExecuteNonBillable(c.Request.Context(), *command, organizationNonBillableAdapterFunc(func(ctx context.Context, request service.OrganizationNonBillableDispatchRequest) (*service.OrganizationNonBillableDispatchResult, error) {
		selected, forwardErr = h.gatewayService.SelectAccountForModel(ctx, apiKey.GroupID, h.gatewayService.GenerateSessionHash(parsed), parsed.Model)
		if forwardErr != nil || selected == nil {
			if forwardErr == nil {
				forwardErr = service.ErrOrganizationBillingUnavailable
			}
			return &service.OrganizationNonBillableDispatchResult{TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: "count_tokens_account_selection_rejected", BusinessLimited: true}, forwardErr
		}
		setOrganizationNonBillableSelectedAccount(c, selected)
		capture = newOrganizationNonBillableResponseCapture(c.Writer)
		original := c.Writer
		c.Writer = capture
		defer func() { c.Writer = original }()
		if selected.Platform == service.PlatformAntigravity || selected.IsBedrock() {
			h.errorResponse(c, http.StatusNotFound, "not_found_error", "count_tokens endpoint is not supported for this platform")
			return &service.OrganizationNonBillableDispatchResult{
				TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError,
				ZeroProof:       "count_tokens_platform_unsupported",
			}, nil
		}
		forwardCtx := service.WithOrganizationNonBillableProviderRequestID(service.WithOrganizationNonBillableCountTokens(ctx), request.ProviderRequestID)
		forwardErr = h.gatewayService.ForwardCountTokens(forwardCtx, c, selected, parsed)
		status := capture.Status()
		inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens := parseOrganizationCountTokens(capture.body.Bytes())
		if service.IsUpstreamPreDispatchError(forwardErr) {
			return &service.OrganizationNonBillableDispatchResult{
				TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError,
				ZeroProof:       "count_tokens_pre_dispatch_error",
			}, forwardErr
		}
		if organizationCountTokensOutcomeUnknown(forwardErr) {
			observation := organizationNonBillablePendingObservation(request, selected, status, capture.body.Bytes())
			organizationNonBillableApplyProviderRequestID(observation, capture.Header().Get("x-request-id"))
			return &service.OrganizationNonBillableDispatchResult{
				Dispatched:            true,
				PendingReconciliation: true,
				Observation:           observation,
				TerminalOutcome:       service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
			}, forwardErr
		}
		terminal := service.OrganizationExecutionTerminalOutcomeSuccess
		if status >= http.StatusBadRequest || forwardErr != nil {
			terminal = service.OrganizationExecutionTerminalOutcomeError
		}
		observation := organizationNonBillableObservation(request, selected, status, capture.body.Bytes(), inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens)
		organizationNonBillableApplyProviderRequestID(observation, capture.Header().Get("x-request-id"))
		return &service.OrganizationNonBillableDispatchResult{Dispatched: true, Observation: observation, TerminalOutcome: terminal}, forwardErr
	}))
	if commitErr := commitOrganizationNonBillableResponse(final, capture); commitErr != nil {
		err = commitErr
	}
	if err != nil && !c.Writer.Written() {
		if reqLog != nil {
			reqLog.Error("organization.count_tokens_failed", zap.Error(err))
		}
		h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Request failed")
	}
}

func (h *GatewayHandler) handleOrganizationGeminiCountTokens(c *gin.Context, apiKey *service.APIKey, model string, body []byte, reqLog *zap.Logger) {
	if err := h.requireOrganizationNonBillable(c, apiKey, service.OrganizationEndpointFamilyTokenCounting, service.OrganizationRequestShapeProbe, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		h.writeOrganizationNonBillableError(c, err, true)
		return
	}
	command, err := h.newOrganizationNonBillableCommand(c, apiKey, service.OrganizationEndpointFamilyTokenCounting, service.OrganizationRequestShapeProbe, model, body)
	if err != nil {
		h.writeOrganizationNonBillableError(c, err, true)
		return
	}
	var selected *service.Account
	var capture *organizationNonBillableResponseCapture
	final, err := h.organizationExecution.ExecuteNonBillable(c.Request.Context(), *command, organizationNonBillableAdapterFunc(func(ctx context.Context, request service.OrganizationNonBillableDispatchRequest) (*service.OrganizationNonBillableDispatchResult, error) {
		if h.geminiCompatService == nil {
			err = service.ErrOrganizationBillingUnavailable
			return &service.OrganizationNonBillableDispatchResult{TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: "gemini_count_tokens_service_unavailable", BusinessLimited: true}, err
		}
		selected, err = h.geminiCompatService.SelectAccountForAIStudioEndpoints(ctx, apiKey.GroupID)
		if err != nil || selected == nil {
			if err == nil {
				err = service.ErrOrganizationBillingUnavailable
			}
			return &service.OrganizationNonBillableDispatchResult{TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: "gemini_count_tokens_account_selection_rejected", BusinessLimited: true}, err
		}
		setOrganizationNonBillableSelectedAccount(c, selected)
		original := c.Writer
		capture = newOrganizationNonBillableResponseCapture(original)
		c.Writer = capture
		defer func() { c.Writer = original }()
		var forwardResult *service.ForwardResult
		if selected.Platform == service.PlatformAntigravity && selected.Type != service.AccountTypeAPIKey {
			googleError(c, http.StatusNotFound, "countTokens endpoint is not supported for this platform")
			return &service.OrganizationNonBillableDispatchResult{
				TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError,
				ZeroProof:       "antigravity_count_tokens_unsupported",
			}, nil
		} else {
			forwardCtx := service.WithOrganizationNonBillableProviderRequestID(service.WithOrganizationNonBillableCountTokens(ctx), request.ProviderRequestID)
			forwardResult, err = h.geminiCompatService.ForwardNative(forwardCtx, c, selected, model, "countTokens", false, body)
		}
		inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens := parseOrganizationCountTokens(capture.body.Bytes())
		if forwardResult != nil {
			if inputTokens == 0 {
				inputTokens = int64(forwardResult.Usage.InputTokens)
			}
			if outputTokens == 0 {
				outputTokens = int64(forwardResult.Usage.OutputTokens)
			}
			if cacheCreationTokens == 0 {
				cacheCreationTokens = int64(forwardResult.Usage.CacheCreationInputTokens)
			}
			if cacheReadTokens == 0 {
				cacheReadTokens = int64(forwardResult.Usage.CacheReadInputTokens)
			}
		}
		var failoverErr *service.UpstreamFailoverError
		if errors.As(err, &failoverErr) && !failoverErr.NoHTTPResponse && !capture.Written() {
			writeOrganizationNonBillableFailoverResponse(capture, failoverErr)
		}
		status := capture.Status()
		if service.IsUpstreamPreDispatchError(err) {
			return &service.OrganizationNonBillableDispatchResult{
				TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError,
				ZeroProof:       "gemini_count_tokens_pre_dispatch_error",
			}, err
		}
		if organizationCountTokensOutcomeUnknown(err) || (errors.As(err, &failoverErr) && failoverErr.NoHTTPResponse) {
			observation := organizationNonBillablePendingObservation(request, selected, status, capture.body.Bytes())
			organizationNonBillableApplyProviderRequestID(observation, capture.Header().Get("x-request-id"))
			return &service.OrganizationNonBillableDispatchResult{
				Dispatched:            true,
				PendingReconciliation: true,
				Observation:           observation,
				TerminalOutcome:       service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
			}, err
		}
		terminal := service.OrganizationExecutionTerminalOutcomeSuccess
		if status >= http.StatusBadRequest || err != nil {
			terminal = service.OrganizationExecutionTerminalOutcomeError
		}
		observation := organizationNonBillableObservation(request, selected, status, capture.body.Bytes(), inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens)
		organizationNonBillableApplyProviderRequestID(observation, capture.Header().Get("x-request-id"))
		if forwardResult != nil {
			organizationNonBillableApplyProviderRequestID(observation, forwardResult.RequestID)
		}
		return &service.OrganizationNonBillableDispatchResult{
			Dispatched:      true,
			Observation:     observation,
			TerminalOutcome: terminal,
		}, err
	}))
	if commitErr := commitOrganizationNonBillableResponse(final, capture); commitErr != nil {
		err = commitErr
	}
	if err != nil && !c.Writer.Written() {
		if reqLog != nil {
			reqLog.Error("organization.gemini_count_tokens_failed", zap.Error(err))
		}
		googleError(c, http.StatusBadGateway, "Request failed")
	}
}

func (h *GatewayHandler) handleOrganizationGeminiModelsRead(c *gin.Context, apiKey *service.APIKey, model string) {
	if err := h.requireOrganizationNonBillable(c, apiKey, service.OrganizationEndpointFamilyModels, service.OrganizationRequestShapeHTTP, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		h.writeOrganizationNonBillableError(c, err, true)
		return
	}
	forcePlatform, forced := middleware.GetForcePlatformFromContext(c)
	if forced && forcePlatform == service.PlatformAntigravity {
		proof := "local_antigravity_models_response"
		if err := h.executeOrganizationLocalNonBillable(c, apiKey, service.OrganizationEndpointFamilyModels, service.OrganizationRequestShapeHTTP, model, proof, nil); err != nil {
			h.writeOrganizationNonBillableError(c, err, true)
			return
		}
		if model == "" {
			c.JSON(http.StatusOK, antigravity.FallbackGeminiModelsList())
		} else {
			c.JSON(http.StatusOK, antigravity.FallbackGeminiModel(model))
		}
		return
	}
	if h.geminiCompatService == nil {
		if err := h.executeOrganizationLocalNonBillableOutcome(c, apiKey, service.OrganizationEndpointFamilyModels, service.OrganizationRequestShapeHTTP, model, "gemini_models_service_unavailable", nil, service.OrganizationExecutionTerminalOutcomeError, true); err != nil {
			h.writeOrganizationNonBillableError(c, err, true)
			return
		}
		googleError(c, http.StatusServiceUnavailable, "Organization billing is temporarily unavailable")
		return
	}
	command, err := h.newOrganizationNonBillableCommand(c, apiKey, service.OrganizationEndpointFamilyModels, service.OrganizationRequestShapeHTTP, model, nil)
	if err != nil {
		h.writeOrganizationNonBillableError(c, err, true)
		return
	}
	var account *service.Account
	var upstream *service.UpstreamHTTPResult
	var forwardErr error
	_, err = h.organizationExecution.ExecuteNonBillable(c.Request.Context(), *command, organizationNonBillableAdapterFunc(func(ctx context.Context, request service.OrganizationNonBillableDispatchRequest) (*service.OrganizationNonBillableDispatchResult, error) {
		account, forwardErr = h.geminiCompatService.SelectAccountForAIStudioEndpoints(ctx, apiKey.GroupID)
		if forwardErr != nil || account == nil {
			if forwardErr == nil {
				forwardErr = service.ErrOrganizationBillingUnavailable
			}
			return &service.OrganizationNonBillableDispatchResult{TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError, ZeroProof: "gemini_models_account_selection_rejected", BusinessLimited: true}, forwardErr
		}
		setOrganizationNonBillableSelectedAccount(c, account)
		path := "/v1beta/models"
		if model != "" {
			path += "/" + model
		}
		forwardCtx := service.WithOrganizationNonBillableProviderRequestID(ctx, request.ProviderRequestID)
		upstream, forwardErr = h.geminiCompatService.ForwardAIStudioGET(forwardCtx, account, path)
		if forwardErr != nil && upstream == nil {
			if service.IsUpstreamNoHTTPResponse(forwardErr) || service.IsUpstreamOutcomeUnknown(forwardErr) {
				return &service.OrganizationNonBillableDispatchResult{
					Dispatched:            true,
					PendingReconciliation: true,
					Observation:           organizationNonBillablePendingObservation(request, account, http.StatusBadGateway, nil),
					TerminalOutcome:       service.OrganizationAttemptEvidenceTerminalOutcomeUnknown,
				}, forwardErr
			}
			return &service.OrganizationNonBillableDispatchResult{
				TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError,
				ZeroProof:       "gemini_models_pre_dispatch_error",
			}, forwardErr
		}
		status := http.StatusBadGateway
		var responseBody []byte
		if upstream != nil {
			status = upstream.StatusCode
			responseBody = upstream.Body
		}
		terminal := service.OrganizationExecutionTerminalOutcomeSuccess
		if forwardErr != nil || (status >= http.StatusBadRequest && !((model == "" && shouldFallbackGeminiModels(upstream)) || (model != "" && shouldFallbackGeminiModel(model, upstream)))) {
			terminal = service.OrganizationExecutionTerminalOutcomeError
		}
		observation := organizationNonBillableObservation(request, account, status, responseBody, 0, 0, 0, 0)
		if upstream != nil {
			organizationNonBillableApplyProviderRequestID(observation, upstream.RequestID)
		}
		return &service.OrganizationNonBillableDispatchResult{
			Dispatched:      true,
			Observation:     observation,
			TerminalOutcome: terminal,
		}, forwardErr
	}))
	if err != nil {
		if !c.Writer.Written() {
			googleError(c, http.StatusBadGateway, "Request failed")
		}
		return
	}
	if model == "" && shouldFallbackGeminiModels(upstream) {
		c.JSON(http.StatusOK, gemini.FallbackModelsList())
		return
	}
	if model != "" && shouldFallbackGeminiModel(model, upstream) {
		c.JSON(http.StatusOK, gemini.FallbackModel(model))
		return
	}
	writeUpstreamResponse(c, upstream)
}

func organizationCountTokensOutcomeUnknown(err error) bool {
	if err == nil {
		return false
	}
	if service.IsUpstreamOutcomeUnknown(err) {
		return true
	}
	if service.IsUpstreamNoHTTPResponse(err) {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "upstream request failed")
}

func writeOrganizationNonBillableFailoverResponse(capture *organizationNonBillableResponseCapture, failoverErr *service.UpstreamFailoverError) {
	if capture == nil || failoverErr == nil {
		return
	}
	for key, values := range failoverErr.ResponseHeaders {
		capture.Header()[key] = append([]string(nil), values...)
	}
	status := failoverErr.StatusCode
	if status <= 0 {
		status = http.StatusBadGateway
	}
	capture.WriteHeader(status)
	if len(failoverErr.ResponseBody) > 0 {
		_, _ = capture.Write(failoverErr.ResponseBody)
	} else {
		capture.WriteHeaderNow()
	}
}

func (h *GatewayHandler) writeOrganizationNonBillableError(c *gin.Context, err error, google bool) {
	if err == nil {
		return
	}
	status, code, message, retryAfter := billingErrorDetails(err)
	if retryAfter > 0 {
		c.Header("Retry-After", retryAfterString(retryAfter))
	}
	if google {
		googleError(c, status, message)
		return
	}
	h.errorResponse(c, status, code, message)
}

func retryAfterString(seconds int) string {
	return strconv.Itoa(seconds)
}

type organizationNonBillableResponseCapture struct {
	gin.ResponseWriter
	body      bytes.Buffer
	response  bytes.Buffer
	header    http.Header
	status    int
	size      int
	committed bool
}

func newOrganizationNonBillableResponseCapture(writer gin.ResponseWriter) *organizationNonBillableResponseCapture {
	header := make(http.Header)
	if writer != nil {
		header = writer.Header().Clone()
	}
	return &organizationNonBillableResponseCapture{ResponseWriter: writer, header: header, status: http.StatusOK, size: -1}
}

func (w *organizationNonBillableResponseCapture) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *organizationNonBillableResponseCapture) WriteHeader(code int) {
	if code > 0 && !w.Written() {
		w.status = code
	}
}

func (w *organizationNonBillableResponseCapture) WriteHeaderNow() {
	if !w.Written() {
		w.size = 0
	}
}

func (w *organizationNonBillableResponseCapture) Write(data []byte) (int, error) {
	original := data
	w.WriteHeaderNow()
	if w.body.Len() < organizationNonBillableResponseCaptureLimit {
		remaining := organizationNonBillableResponseCaptureLimit - w.body.Len()
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = w.body.Write(data)
	}
	_, _ = w.response.Write(original)
	w.size += len(original)
	return len(original), nil
}

func (w *organizationNonBillableResponseCapture) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *organizationNonBillableResponseCapture) Status() int {
	return w.status
}

func (w *organizationNonBillableResponseCapture) Size() int {
	return w.size
}

func (w *organizationNonBillableResponseCapture) Written() bool {
	return w.size >= 0
}

func (w *organizationNonBillableResponseCapture) Flush() {
	w.WriteHeaderNow()
}

func (w *organizationNonBillableResponseCapture) commit() error {
	if w == nil || w.ResponseWriter == nil || w.committed || !w.Written() {
		return nil
	}
	destination := w.ResponseWriter.Header()
	for key := range destination {
		delete(destination, key)
	}
	for key, values := range w.Header() {
		destination[key] = append([]string(nil), values...)
	}
	w.ResponseWriter.WriteHeader(w.status)
	if w.response.Len() == 0 {
		w.ResponseWriter.WriteHeaderNow()
		w.committed = true
		return nil
	}
	_, err := w.ResponseWriter.Write(w.response.Bytes())
	if err == nil {
		w.committed = true
	}
	return err
}

func commitOrganizationNonBillableResponse(final *service.OrganizationNonBillableFinalExecutionEvidenceResult, capture *organizationNonBillableResponseCapture) error {
	if final == nil || capture == nil || !capture.Written() {
		return nil
	}
	return capture.commit()
}

func organizationNonBillableObservation(request service.OrganizationNonBillableDispatchRequest, account *service.Account, status int, body []byte, inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int64) *service.OrganizationExecutionAttemptObservation {
	if account == nil {
		return nil
	}
	digest := sha256.Sum256(body)
	route, _ := json.Marshal(map[string]any{
		"account_id":      account.ID,
		"platform":        account.Platform,
		"account_type":    account.Type,
		"endpoint_family": request.EndpointFamily,
		"request_shape":   request.RequestShape,
	})
	evidence, _ := json.Marshal(map[string]any{
		"status_code":         status,
		"body_sha256":         hex.EncodeToString(digest[:]),
		"body_bytes_captured": len(body),
		"provider_request_id": request.ProviderRequestID,
	})
	return &service.OrganizationExecutionAttemptObservation{
		AccountID:             account.ID,
		RouteSnapshot:         route,
		ProviderRequestID:     request.ProviderRequestID,
		ProviderCorrelationID: request.ProviderCorrelationID,
		Evidence:              evidence,
		RawActualCost:         "0",
		InputTokens:           inputTokens,
		OutputTokens:          outputTokens,
		CacheCreationTokens:   cacheCreationTokens,
		CacheReadTokens:       cacheReadTokens,
		ProviderObservedAt:    time.Now().UTC(),
	}
}

func organizationNonBillableApplyProviderRequestID(observation *service.OrganizationExecutionAttemptObservation, providerRequestID string) {
	providerRequestID = strings.TrimSpace(providerRequestID)
	if observation == nil || providerRequestID == "" {
		return
	}
	observation.ProviderRequestID = providerRequestID
	var evidence map[string]any
	if json.Unmarshal(observation.Evidence, &evidence) != nil {
		evidence = map[string]any{}
	}
	evidence["provider_request_id"] = providerRequestID
	if encoded, err := json.Marshal(evidence); err == nil {
		observation.Evidence = encoded
	}
}

func organizationNonBillablePendingObservation(request service.OrganizationNonBillableDispatchRequest, account *service.Account, status int, body []byte) *service.OrganizationExecutionAttemptObservation {
	observation := organizationNonBillableObservation(request, account, status, body, 0, 0, 0, 0)
	if observation != nil {
		observation.RawActualCost = ""
	}
	return observation
}

func parseOrganizationCountTokens(body []byte) (inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int64) {
	var payload struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		TotalTokens              int64 `json:"totalTokens"`
		PromptTokenCount         int64 `json:"promptTokenCount"`
		CachedContentTokenCount  int64 `json:"cachedContentTokenCount"`
		Usage                    struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return 0, 0, 0, 0
	}
	inputTokens = payload.InputTokens
	if inputTokens == 0 {
		inputTokens = payload.Usage.InputTokens
	}
	outputTokens = payload.OutputTokens
	if outputTokens == 0 {
		outputTokens = payload.Usage.OutputTokens
	}
	cacheCreationTokens = payload.CacheCreationInputTokens
	if cacheCreationTokens == 0 {
		cacheCreationTokens = payload.Usage.CacheCreationInputTokens
	}
	cacheReadTokens = payload.CacheReadInputTokens
	if cacheReadTokens == 0 {
		cacheReadTokens = payload.Usage.CacheReadInputTokens
	}
	if cacheReadTokens == 0 {
		cacheReadTokens = payload.CachedContentTokenCount
	}
	if inputTokens == 0 {
		inputTokens = payload.PromptTokenCount
	}
	if inputTokens == 0 && payload.TotalTokens > 0 {
		inputTokens = payload.TotalTokens - cacheReadTokens
		if inputTokens < 0 {
			inputTokens = 0
		}
	}
	return inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens
}

func setOrganizationNonBillableSelectedAccount(c *gin.Context, account *service.Account) {
	if c != nil && account != nil {
		setOpsSelectedAccount(c, account.ID, account.Platform)
	}
}

func organizationNonBillableAuthorization(c *gin.Context) bool {
	_, ok := middleware.GetOrganizationKeyAuthorizationFromContext(c)
	return ok
}
