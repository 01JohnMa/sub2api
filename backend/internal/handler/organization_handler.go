package handler

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// OrganizationHandler exposes the JWT-only current-organization boundary.
// Financial, membership, reporting, and API Key endpoints are added by later
// tickets and must continue deriving scope from the middleware context.
type OrganizationHandler struct {
	membershipService service.CurrentOrganizationMembershipManager
	fundingReader     service.OrganizationFundingReader
	allocationService service.OrganizationAllocationManager
	recoveryService   *service.OrganizationExecutionRecoveryService
}

func (h *OrganizationHandler) SetRecoveryService(recoveryService *service.OrganizationExecutionRecoveryService) {
	if h != nil {
		h.recoveryService = recoveryService
	}
}

func NewOrganizationHandler(
	membershipService service.CurrentOrganizationMembershipManager,
	fundingReader service.OrganizationFundingReader,
	allocationService service.OrganizationAllocationManager,
) *OrganizationHandler {
	return &OrganizationHandler{
		membershipService: membershipService,
		fundingReader:     fundingReader,
		allocationService: allocationService,
	}
}

type addCurrentOrganizationMemberRequest struct {
	UserID *int64 `json:"user_id"`
	Email  string `json:"email"`
}

type removeCurrentOrganizationMemberRequest struct {
	PublicReason string `json:"public_reason"`
}

// GetCurrent returns only the server-derived current organization identity.
func (h *OrganizationHandler) GetCurrent(c *gin.Context) {
	scope, ok := middleware.GetOrganizationScopeFromContext(c)
	if !ok {
		response.ErrorFrom(c, infraerrors.Forbidden(
			"ORGANIZATION_ADMIN_REQUIRED",
			"Organization administrator access required",
		))
		return
	}
	response.Success(c, dto.OrganizationSummaryFromService(&scope.Organization))
}

// AddCurrentMember adds an exact registered user to the server-derived active
// organization. The request has no organization or role controls.
func (h *OrganizationHandler) AddCurrentMember(c *gin.Context) {
	scope, ok := middleware.GetOrganizationScopeFromContext(c)
	if !ok {
		response.ErrorFrom(c, infraerrors.Forbidden(
			"ORGANIZATION_ADMIN_REQUIRED",
			"Organization administrator access required",
		))
		return
	}
	if h == nil || h.membershipService == nil {
		response.ErrorFrom(c, service.ErrOrganizationMembershipUnavailable)
		return
	}

	var request addCurrentOrganizationMemberRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	request.Email = strings.TrimSpace(request.Email)
	if (request.UserID == nil) == (request.Email == "") || (request.UserID != nil && *request.UserID <= 0) {
		response.ErrorFrom(c, service.ErrInvalidOrganizationMemberTarget)
		return
	}
	target := service.OrganizationMemberTarget{
		UserID: request.UserID,
		Email:  request.Email,
	}
	member, err := h.membershipService.AddCurrentMember(c.Request.Context(), scope.OrganizationID, target)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.OrganizationMemberFromService(member))
}

func (h *OrganizationHandler) ListCurrentMembers(c *gin.Context) {
	scope, ok := currentOrganizationScope(c)
	if !ok {
		return
	}
	if h == nil || h.membershipService == nil {
		response.ErrorFrom(c, service.ErrOrganizationMembershipUnavailable)
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, pageResult, err := h.membershipService.List(c.Request.Context(), scope.OrganizationID, pagination.PaginationParams{
		Page: page, PageSize: pageSize,
		SortBy: c.DefaultQuery("sort_by", "created_at"), SortOrder: c.DefaultQuery("sort_order", "desc"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result := make([]dto.OrganizationMember, 0, len(items))
	for i := range items {
		result = append(result, *dto.OrganizationMemberFromService(&items[i]))
	}
	response.PaginatedWithResult(c, result, &response.PaginationResult{
		Total: pageResult.Total, Page: pageResult.Page, PageSize: pageResult.PageSize, Pages: pageResult.Pages,
	})
}

func (h *OrganizationHandler) RemoveCurrentMember(c *gin.Context) {
	scope, ok := currentOrganizationScope(c)
	if !ok {
		return
	}
	remover, ok := h.membershipService.(service.OrganizationMemberRemovalManager)
	if !ok {
		response.ErrorFrom(c, service.ErrOrganizationMembershipUnavailable)
		return
	}
	targetUserID, err := strconv.ParseInt(strings.TrimSpace(c.Param("user_id")), 10, 64)
	if err != nil || targetUserID <= 0 {
		response.ErrorFrom(c, service.ErrOrganizationMemberRemovalInvalid)
		return
	}
	var request removeCurrentOrganizationMemberRequest
	if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	result, err := remover.Remove(
		c.Request.Context(),
		organizationMemberRemovalActor(scope),
		scope.OrganizationID,
		targetUserID,
		service.OrganizationMemberRemovalInput{PublicReason: strings.TrimSpace(request.PublicReason)},
		c.GetHeader("Idempotency-Key"),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Success(c, dto.OrganizationMemberRemovalResultFromService(result))
}

func (h *OrganizationHandler) GetCurrentBalance(c *gin.Context) {
	scope, ok := currentOrganizationScope(c)
	if !ok {
		return
	}
	if h == nil || h.fundingReader == nil {
		response.ErrorFrom(c, service.ErrOrganizationFundingRepositoryUnavailable)
		return
	}
	summary, err := h.fundingReader.GetSummary(c.Request.Context(), scope.OrganizationID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationBalanceSummaryFromService(summary))
}

func (h *OrganizationHandler) GetPendingReconciliationSummary(c *gin.Context) {
	scope, ok := currentOrganizationScope(c)
	if !ok {
		return
	}
	if h == nil || h.recoveryService == nil {
		response.ErrorFrom(c, service.ErrOrganizationExecutionRepositoryUnavailable)
		return
	}
	summary, err := h.recoveryService.PendingSummary(c.Request.Context(), scope.OrganizationID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationPendingReconciliationSummaryFromService(summary))
}

func (h *OrganizationHandler) GetCurrentReport(c *gin.Context) {
	scope, ok := currentOrganizationScope(c)
	if !ok {
		return
	}
	reportRange, ok := organizationReportRangeFromQuery(c)
	if !ok {
		return
	}
	if h == nil || h.fundingReader == nil {
		response.ErrorFrom(c, service.ErrOrganizationFundingRepositoryUnavailable)
		return
	}
	report, err := h.fundingReader.GetReport(c.Request.Context(), scope.OrganizationID, reportRange)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationReportFromService(report))
}

func (h *OrganizationHandler) ListCurrentReportMembers(c *gin.Context) {
	scope, ok := currentOrganizationScope(c)
	if !ok {
		return
	}
	reportRange, ok := organizationReportRangeFromQuery(c)
	if !ok {
		return
	}
	if h == nil || h.fundingReader == nil {
		response.ErrorFrom(c, service.ErrOrganizationFundingRepositoryUnavailable)
		return
	}
	filters, hasFilters, ok := organizationReportMemberFiltersFromQuery(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	var items []service.OrganizationReportMemberUsage
	var departed *service.OrganizationReportMemberUsage
	var pageResult *pagination.PaginationResult
	var err error
	if hasFilters {
		reader, supported := h.fundingReader.(service.OrganizationReportMemberFilterReader)
		if !supported {
			response.ErrorFrom(c, service.ErrOrganizationFundingRepositoryUnavailable)
			return
		}
		items, departed, pageResult, err = reader.ListReportMembersFiltered(c.Request.Context(), scope.OrganizationID, reportRange, params, filters)
	} else {
		items, departed, pageResult, err = h.fundingReader.ListReportMembers(c.Request.Context(), scope.OrganizationID, reportRange, params)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result := make([]dto.OrganizationReportMemberUsage, 0, len(items))
	for i := range items {
		result = append(result, *dto.OrganizationReportMemberUsageFromService(&items[i]))
	}
	response.Success(c, struct {
		Items    []dto.OrganizationReportMemberUsage `json:"items"`
		Departed *dto.OrganizationReportMemberUsage  `json:"departed"`
		Total    int64                               `json:"total"`
		Page     int                                 `json:"page"`
		PageSize int                                 `json:"page_size"`
		Pages    int                                 `json:"pages"`
	}{Items: result, Departed: dto.OrganizationReportMemberUsageFromService(departed), Total: pageResult.Total, Page: pageResult.Page, PageSize: pageResult.PageSize, Pages: pageResult.Pages})
}

func (h *OrganizationHandler) ListCurrentOrganizationAPIKeys(c *gin.Context) {
	scope, ok := currentOrganizationScope(c)
	if !ok {
		return
	}
	if h == nil || h.fundingReader == nil {
		response.ErrorFrom(c, service.ErrOrganizationFundingRepositoryUnavailable)
		return
	}
	filters, _, ok := organizationReportMemberFiltersFromQuery(c)
	if !ok {
		return
	}
	reader, supported := h.fundingReader.(service.OrganizationReportMemberFilterReader)
	if !supported {
		response.ErrorFrom(c, service.ErrOrganizationFundingRepositoryUnavailable)
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, pageResult, err := reader.ListOrganizationAPIKeyMetadata(c.Request.Context(), scope.OrganizationID, pagination.PaginationParams{Page: page, PageSize: pageSize}, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result := make([]dto.OrganizationAPIKeyMetadata, 0, len(items))
	for i := range items {
		result = append(result, *dto.OrganizationAPIKeyMetadataFromService(&items[i]))
	}
	response.PaginatedWithResult(c, result, &response.PaginationResult{Total: pageResult.Total, Page: pageResult.Page, PageSize: pageResult.PageSize, Pages: pageResult.Pages})
}

func (h *OrganizationHandler) ListCurrentPublicLedger(c *gin.Context) {
	scope, ok := currentOrganizationScope(c)
	if !ok {
		return
	}
	if h == nil || h.fundingReader == nil {
		response.ErrorFrom(c, service.ErrOrganizationFundingRepositoryUnavailable)
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, pageResult, err := h.fundingReader.ListLedger(c.Request.Context(), scope.OrganizationID, pagination.PaginationParams{Page: page, PageSize: pageSize, SortOrder: c.DefaultQuery("sort_order", "desc")})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result := make([]dto.OrganizationPublicLedgerEntry, 0, len(items))
	for i := range items {
		result = append(result, *dto.OrganizationPublicLedgerEntryFromService(&items[i]))
	}
	response.PaginatedWithResult(c, result, &response.PaginationResult{Total: pageResult.Total, Page: pageResult.Page, PageSize: pageResult.PageSize, Pages: pageResult.Pages})
}

func (h *OrganizationHandler) PreviewEqualAllocation(c *gin.Context) {
	h.previewAllocation(c, domain.OrganizationFundingOperationEqualAllocation)
}

func (h *OrganizationHandler) PreviewCustomAllocation(c *gin.Context) {
	h.previewAllocation(c, domain.OrganizationFundingOperationCustomAllocation)
}

func (h *OrganizationHandler) PreviewReclaimAllocation(c *gin.Context) {
	h.previewAllocation(c, domain.OrganizationFundingOperationReclaim)
}

func (h *OrganizationHandler) ExecuteEqualAllocation(c *gin.Context) {
	h.executeAllocation(c, domain.OrganizationFundingOperationEqualAllocation)
}

func (h *OrganizationHandler) ExecuteCustomAllocation(c *gin.Context) {
	h.executeAllocation(c, domain.OrganizationFundingOperationCustomAllocation)
}

func (h *OrganizationHandler) ExecuteReclaimAllocation(c *gin.Context) {
	h.executeAllocation(c, domain.OrganizationFundingOperationReclaim)
}

func (h *OrganizationHandler) previewAllocation(c *gin.Context, operationType string) {
	scope, ok := currentOrganizationScope(c)
	if !ok {
		return
	}
	if h == nil || h.allocationService == nil {
		response.ErrorFrom(c, service.ErrOrganizationAllocationRepositoryUnavailable)
		return
	}
	var input service.OrganizationAllocationPreviewInput
	if err := c.ShouldBindJSON(&input); err != nil && !errors.Is(err, io.EOF) {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	actor := organizationAllocationActor(scope)
	preview, err := h.allocationService.Preview(c.Request.Context(), actor, scope.OrganizationID, operationType, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationAllocationPreviewFromService(preview))
}

func (h *OrganizationHandler) executeAllocation(c *gin.Context, operationType string) {
	scope, ok := currentOrganizationScope(c)
	if !ok {
		return
	}
	if h == nil || h.allocationService == nil {
		response.ErrorFrom(c, service.ErrOrganizationAllocationRepositoryUnavailable)
		return
	}
	var input service.OrganizationAllocationExecuteInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	result, err := h.allocationService.Execute(
		c.Request.Context(), organizationAllocationActor(scope), scope.OrganizationID, operationType, input, c.GetHeader("Idempotency-Key"),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Success(c, dto.OrganizationAllocationResultFromService(result))
}

func currentOrganizationScope(c *gin.Context) (middleware.OrganizationScope, bool) {
	if c == nil || c.GetString("auth_method") != "jwt" {
		response.ErrorFrom(c, service.ErrOrganizationAllocationForbidden)
		return middleware.OrganizationScope{}, false
	}
	scope, ok := middleware.GetOrganizationScopeFromContext(c)
	if !ok || scope.OrganizationID <= 0 || scope.OrganizationRole != service.OrganizationRoleAdmin {
		response.ErrorFrom(c, infraerrors.Forbidden("ORGANIZATION_ADMIN_REQUIRED", "Organization administrator access required"))
		return middleware.OrganizationScope{}, false
	}
	return scope, true
}

func organizationReportRangeFromQuery(c *gin.Context) (service.OrganizationReportRange, bool) {
	start, startErr := time.Parse(time.RFC3339, strings.TrimSpace(c.Query("start")))
	end, endErr := time.Parse(time.RFC3339, strings.TrimSpace(c.Query("end")))
	reportRange := service.OrganizationReportRange{Start: start, End: end}
	if startErr != nil || endErr != nil || !reportRange.Valid() {
		response.ErrorFrom(c, service.ErrOrganizationReportRangeInvalid)
		return service.OrganizationReportRange{}, false
	}
	return reportRange, true
}

func organizationReportMemberFiltersFromQuery(c *gin.Context) (service.OrganizationReportMemberFilters, bool, bool) {
	filters := service.OrganizationReportMemberFilters{}
	for _, field := range []struct {
		name   string
		target **int64
	}{
		{name: "member_id", target: &filters.UserID}, {name: "allocation_id", target: &filters.AllocationID}, {name: "api_key_id", target: &filters.APIKeyID},
	} {
		raw, present := c.GetQuery(field.name)
		if !present {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || value <= 0 {
			response.ErrorFrom(c, infraerrors.BadRequest("ORGANIZATION_REPORT_FILTER_INVALID", "organization report filters must be positive integer IDs"))
			return service.OrganizationReportMemberFilters{}, false, false
		}
		*field.target = &value
	}
	return filters, filters.UserID != nil || filters.AllocationID != nil || filters.APIKeyID != nil, true
}

func organizationAllocationActor(scope middleware.OrganizationScope) service.OrganizationAllocationActor {
	return service.OrganizationAllocationActor{
		UserID:                scope.UserID,
		DisplayName:           fmt.Sprintf("organization-admin-%d", scope.UserID),
		OrganizationRole:      scope.OrganizationRole,
		AuthorizationScope:    domain.OrganizationFundingAuthorizationOrganizationJWT,
		CurrentOrganizationID: scope.OrganizationID,
		Active:                true,
	}
}

func organizationMemberRemovalActor(scope middleware.OrganizationScope) service.OrganizationMemberRemovalActor {
	return service.OrganizationMemberRemovalActor{
		UserID:                scope.UserID,
		DisplayName:           fmt.Sprintf("organization-admin-%d", scope.UserID),
		OrganizationRole:      scope.OrganizationRole,
		AuthorizationScope:    domain.OrganizationFundingAuthorizationOrganizationJWT,
		CurrentOrganizationID: scope.OrganizationID,
		Active:                true,
	}
}
