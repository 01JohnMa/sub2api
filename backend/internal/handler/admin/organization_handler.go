package admin

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

// OrganizationHandler exposes platform-only organization identity management.
// Organization-member and financial mutations deliberately live in later tickets.
type OrganizationHandler struct {
	organizationService service.OrganizationManager
	membershipService   service.OrganizationMembershipManager
	fundingService      *service.OrganizationFundingService
	allocationService   service.OrganizationAllocationManager
	recoveryService     *service.OrganizationExecutionRecoveryService
	lifecycleService    service.OrganizationLifecycleManager
}

func NewOrganizationHandler(
	organizationService service.OrganizationManager,
	membershipService service.OrganizationMembershipManager,
	fundingService *service.OrganizationFundingService,
	allocationService service.OrganizationAllocationManager,
) *OrganizationHandler {
	return &OrganizationHandler{
		organizationService: organizationService,
		membershipService:   membershipService,
		fundingService:      fundingService,
		allocationService:   allocationService,
	}
}

func (h *OrganizationHandler) SetRecoveryService(recoveryService *service.OrganizationExecutionRecoveryService) {
	if h != nil {
		h.recoveryService = recoveryService
	}
}

func (h *OrganizationHandler) SetLifecycleService(lifecycleService service.OrganizationLifecycleManager) {
	if h != nil {
		h.lifecycleService = lifecycleService
	}
}

func (h *OrganizationHandler) ListPendingReconciliations(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	if h == nil || h.recoveryService == nil {
		response.ErrorFrom(c, service.ErrOrganizationExecutionRepositoryUnavailable)
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, pageResult, err := h.recoveryService.ListPending(c.Request.Context(), organizationID, service.OrganizationPendingReconciliationListParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result := make([]dto.OrganizationPendingReconciliation, 0, len(items))
	for i := range items {
		result = append(result, *dto.OrganizationPendingReconciliationFromService(&items[i]))
	}
	response.PaginatedWithResult(c, result, &response.PaginationResult{Total: pageResult.Total, Page: pageResult.Page, PageSize: pageResult.PageSize, Pages: pageResult.Pages})
}

func (h *OrganizationHandler) GetPendingReconciliation(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	if h == nil || h.recoveryService == nil {
		response.ErrorFrom(c, service.ErrOrganizationExecutionRepositoryUnavailable)
		return
	}
	item, err := h.recoveryService.GetPending(c.Request.Context(), organizationID, c.Param("execution_id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationPendingReconciliationFromService(item))
}

func (h *OrganizationHandler) ResolvePendingReconciliation(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	if h == nil || h.recoveryService == nil {
		response.ErrorFrom(c, service.ErrOrganizationExecutionRepositoryUnavailable)
		return
	}
	actor, ok := organizationReconciliationActorFromContext(c)
	if !ok {
		response.ErrorFrom(c, service.ErrOrganizationExecutionReconciliationForbidden)
		return
	}
	result, err := h.recoveryService.ReconcilePendingInOrganization(c.Request.Context(), actor, organizationID, c.Param("execution_id"), c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Success(c, dto.OrganizationPendingReconciliationResolutionFromService(result))
}

type createOrganizationRequest struct {
	Name        string `json:"name" binding:"required,max=100"`
	Code        string `json:"code" binding:"required,max=64"`
	Description string `json:"description"`
}

type updateOrganizationRequest struct {
	Name        *string `json:"name" binding:"omitempty,max=100"`
	Code        *string `json:"code" binding:"omitempty,max=64"`
	Description *string `json:"description"`
}

type organizationMemberRequest struct {
	UserID           int64  `json:"user_id" binding:"required,gt=0"`
	OrganizationRole string `json:"organization_role" binding:"required"`
}

type updateOrganizationMemberRequest struct {
	OrganizationRole string `json:"organization_role" binding:"required"`
}

type removeOrganizationMemberRequest struct {
	PublicReason string `json:"public_reason"`
}

type creditOrganizationRequest struct {
	Amount         string `json:"amount" binding:"required"`
	PublicReason   string `json:"public_reason"`
	InternalReason string `json:"internal_reason" binding:"required"`
}

type archiveOrganizationRequest struct {
	BalanceVersion int64  `json:"balance_version" binding:"required,gte=0"`
	UpdatedAt      string `json:"updated_at" binding:"required"`
	ConfirmArchive bool   `json:"confirm_archive"`
}

func (h *OrganizationHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, pageResult, err := h.organizationService.List(c.Request.Context(), pagination.PaginationParams{
		Page: page, PageSize: pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}, service.OrganizationListFilters{
		Status: strings.TrimSpace(c.Query("status")),
		Search: strings.TrimSpace(c.Query("search")),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result := make([]dto.Organization, 0, len(items))
	for i := range items {
		result = append(result, *dto.OrganizationFromService(&items[i]))
	}
	response.PaginatedWithResult(c, result, &response.PaginationResult{
		Total: pageResult.Total, Page: pageResult.Page, PageSize: pageResult.PageSize, Pages: pageResult.Pages,
	})
}

func (h *OrganizationHandler) Get(c *gin.Context) {
	id, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	organization, err := h.organizationService.Get(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationFromService(organization))
}

func (h *OrganizationHandler) Create(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.ErrorFrom(c, infraerrors.Forbidden("ORGANIZATION_ADMIN_REQUIRED", "platform administrator is required"))
		return
	}
	var request createOrganizationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	organization, err := h.organizationService.Create(c.Request.Context(), subject.UserID, service.CreateOrganizationInput{
		Name: request.Name, Code: request.Code, Description: request.Description,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.OrganizationFromService(organization))
}

func (h *OrganizationHandler) Update(c *gin.Context) {
	id, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	var request updateOrganizationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	organization, err := h.organizationService.Update(c.Request.Context(), id, service.UpdateOrganizationInput{
		Name: request.Name, Code: request.Code, Description: request.Description,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationFromService(organization))
}

func (h *OrganizationHandler) Disable(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	if h == nil || h.lifecycleService == nil {
		response.ErrorFrom(c, service.ErrOrganizationLifecycleUnavailable)
		return
	}
	organization, err := h.lifecycleService.Disable(c.Request.Context(), organizationID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationFromService(organization))
}

func (h *OrganizationHandler) Reactivate(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	if h == nil || h.lifecycleService == nil {
		response.ErrorFrom(c, service.ErrOrganizationLifecycleUnavailable)
		return
	}
	organization, err := h.lifecycleService.Reactivate(c.Request.Context(), organizationID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationFromService(organization))
}

func (h *OrganizationHandler) GetArchivePreview(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	if h == nil || h.lifecycleService == nil {
		response.ErrorFrom(c, service.ErrOrganizationLifecycleUnavailable)
		return
	}
	preview, err := h.lifecycleService.GetArchivePreview(c.Request.Context(), organizationID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationArchivePreviewFromService(preview))
}

func (h *OrganizationHandler) Archive(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	if h == nil || h.lifecycleService == nil {
		response.ErrorFrom(c, service.ErrOrganizationLifecycleUnavailable)
		return
	}
	var request archiveOrganizationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, request.UpdatedAt)
	if err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", "updated_at must be RFC3339"))
		return
	}
	organization, err := h.lifecycleService.Archive(c.Request.Context(), service.OrganizationLifecycleCommand{
		OrganizationID:         organizationID,
		ExpectedBalanceVersion: request.BalanceVersion,
		ExpectedUpdatedAt:      updatedAt,
		ConfirmArchive:         request.ConfirmArchive,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationFromService(organization))
}

func (h *OrganizationHandler) ListMembers(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, pageResult, err := h.membershipService.List(c.Request.Context(), organizationID, pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
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

func (h *OrganizationHandler) AddMember(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	var request organizationMemberRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	member, err := h.membershipService.Add(c.Request.Context(), organizationID, request.UserID, request.OrganizationRole)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.OrganizationMemberFromService(member))
}

func (h *OrganizationHandler) UpdateMember(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	userID, ok := organizationMemberIDFromParam(c)
	if !ok {
		return
	}
	var request updateOrganizationMemberRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	member, err := h.membershipService.ChangeRole(c.Request.Context(), organizationID, userID, request.OrganizationRole)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationMemberFromService(member))
}

func (h *OrganizationHandler) RemoveMember(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	targetUserID, ok := organizationMemberIDFromParam(c)
	if !ok {
		return
	}
	actor, ok := organizationMemberRemovalActorFromContext(c)
	if !ok {
		response.ErrorFrom(c, service.ErrOrganizationMemberRemovalForbidden)
		return
	}
	remover, ok := h.membershipService.(service.OrganizationMemberRemovalManager)
	if !ok {
		response.ErrorFrom(c, service.ErrOrganizationMembershipUnavailable)
		return
	}
	var request removeOrganizationMemberRequest
	if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	result, err := remover.Remove(
		c.Request.Context(), actor, organizationID, targetUserID,
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

func (h *OrganizationHandler) Credit(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	actor, ok := organizationFundingActorFromContext(c)
	if !ok {
		response.ErrorFrom(c, service.ErrOrganizationFundingForbidden)
		return
	}
	var request creditOrganizationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	if h == nil || h.fundingService == nil {
		response.ErrorFrom(c, service.ErrOrganizationFundingRepositoryUnavailable)
		return
	}
	result, err := h.fundingService.Credit(
		c.Request.Context(),
		actor,
		organizationID,
		request.Amount,
		service.OrganizationFundingReasons{
			PublicReason:   request.PublicReason,
			InternalReason: request.InternalReason,
		},
		c.GetHeader("Idempotency-Key"),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Success(c, dto.OrganizationFundingResultFromService(result))
}

func (h *OrganizationHandler) GetBalance(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	if h == nil || h.fundingService == nil {
		response.ErrorFrom(c, service.ErrOrganizationFundingRepositoryUnavailable)
		return
	}
	summary, err := h.fundingService.GetSummary(c.Request.Context(), organizationID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationBalanceSummaryFromService(summary))
}

func (h *OrganizationHandler) GetReport(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	reportRange, ok := organizationReportRangeFromQuery(c)
	if !ok {
		return
	}
	if h == nil || h.fundingService == nil {
		response.ErrorFrom(c, service.ErrOrganizationFundingRepositoryUnavailable)
		return
	}
	report, err := h.fundingService.GetReport(c.Request.Context(), organizationID, reportRange)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationReportFromService(report))
}

func (h *OrganizationHandler) ListReportMembers(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	reportRange, ok := organizationReportRangeFromQuery(c)
	if !ok {
		return
	}
	if h == nil || h.fundingService == nil {
		response.ErrorFrom(c, service.ErrOrganizationFundingRepositoryUnavailable)
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, departed, pageResult, err := h.fundingService.ListReportMembers(c.Request.Context(), organizationID, reportRange, pagination.PaginationParams{Page: page, PageSize: pageSize})
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

func (h *OrganizationHandler) ListLedger(c *gin.Context) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	if h == nil || h.fundingService == nil {
		response.ErrorFrom(c, service.ErrOrganizationFundingRepositoryUnavailable)
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, pageResult, err := h.fundingService.ListLedger(c.Request.Context(), organizationID, pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result := make([]dto.OrganizationLedgerEntry, 0, len(items))
	for i := range items {
		result = append(result, *dto.OrganizationLedgerEntryFromService(&items[i]))
	}
	response.PaginatedWithResult(c, result, &response.PaginationResult{
		Total: pageResult.Total, Page: pageResult.Page, PageSize: pageResult.PageSize, Pages: pageResult.Pages,
	})
}

func (h *OrganizationHandler) PreviewCustomAllocation(c *gin.Context) {
	h.previewAllocation(c, domain.OrganizationFundingOperationCustomAllocation)
}

func (h *OrganizationHandler) ExecuteCustomAllocation(c *gin.Context) {
	h.executeAllocation(c, domain.OrganizationFundingOperationCustomAllocation)
}

func (h *OrganizationHandler) PreviewReclaimAllocation(c *gin.Context) {
	h.previewAllocation(c, domain.OrganizationFundingOperationReclaim)
}

func (h *OrganizationHandler) ExecuteReclaimAllocation(c *gin.Context) {
	h.executeAllocation(c, domain.OrganizationFundingOperationReclaim)
}

func (h *OrganizationHandler) previewAllocation(c *gin.Context, operationType string) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	actor, ok := organizationAllocationActorFromContext(c)
	if !ok {
		response.ErrorFrom(c, service.ErrOrganizationAllocationForbidden)
		return
	}
	if h == nil || h.allocationService == nil {
		response.ErrorFrom(c, service.ErrOrganizationAllocationRepositoryUnavailable)
		return
	}
	var input service.OrganizationAllocationPreviewInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	preview, err := h.allocationService.Preview(c.Request.Context(), actor, organizationID, operationType, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.OrganizationAllocationPreviewFromService(preview))
}

func (h *OrganizationHandler) executeAllocation(c *gin.Context, operationType string) {
	organizationID, ok := organizationIDFromParam(c)
	if !ok {
		return
	}
	actor, ok := organizationAllocationActorFromContext(c)
	if !ok {
		response.ErrorFrom(c, service.ErrOrganizationAllocationForbidden)
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
		c.Request.Context(), actor, organizationID, operationType, input, c.GetHeader("Idempotency-Key"),
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

func organizationFundingActorFromContext(c *gin.Context) (service.OrganizationFundingActor, bool) {
	if c == nil || c.GetString("auth_method") != "jwt" {
		return service.OrganizationFundingActor{}, false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		return service.OrganizationFundingActor{}, false
	}
	role, ok := middleware.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		return service.OrganizationFundingActor{}, false
	}
	return service.OrganizationFundingActor{
		UserID:             subject.UserID,
		DisplayName:        fmt.Sprintf("platform-admin-%d", subject.UserID),
		PlatformRole:       role,
		AuthorizationScope: domain.OrganizationFundingAuthorizationPlatformAdminJWT,
		Active:             true,
	}, true
}

func organizationAllocationActorFromContext(c *gin.Context) (service.OrganizationAllocationActor, bool) {
	if c == nil || c.GetString("auth_method") != "jwt" {
		return service.OrganizationAllocationActor{}, false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		return service.OrganizationAllocationActor{}, false
	}
	role, ok := middleware.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		return service.OrganizationAllocationActor{}, false
	}
	return service.OrganizationAllocationActor{
		UserID:             subject.UserID,
		DisplayName:        fmt.Sprintf("platform-admin-%d", subject.UserID),
		PlatformRole:       role,
		AuthorizationScope: domain.OrganizationFundingAuthorizationPlatformAdminJWT,
		Active:             true,
	}, true
}

func organizationMemberRemovalActorFromContext(c *gin.Context) (service.OrganizationMemberRemovalActor, bool) {
	if c == nil || c.GetString("auth_method") != "jwt" {
		return service.OrganizationMemberRemovalActor{}, false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		return service.OrganizationMemberRemovalActor{}, false
	}
	role, ok := middleware.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		return service.OrganizationMemberRemovalActor{}, false
	}
	return service.OrganizationMemberRemovalActor{
		UserID:             subject.UserID,
		DisplayName:        fmt.Sprintf("platform-admin-%d", subject.UserID),
		PlatformRole:       role,
		AuthorizationScope: domain.OrganizationFundingAuthorizationPlatformAdminJWT,
		Active:             true,
	}, true
}

func organizationReconciliationActorFromContext(c *gin.Context) (service.OrganizationReconciliationActor, bool) {
	actor, ok := organizationFundingActorFromContext(c)
	if !ok {
		return service.OrganizationReconciliationActor{}, false
	}
	return service.OrganizationReconciliationActor{
		UserID: actor.UserID, DisplayName: actor.DisplayName, PlatformRole: actor.PlatformRole,
		AuthorizationScope: actor.AuthorizationScope, Active: actor.Active,
	}, true
}

func organizationIDFromParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, service.ErrInvalidOrganizationID)
		return 0, false
	}
	return id, true
}

func organizationMemberIDFromParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, service.ErrOrganizationMemberNotFound)
		return 0, false
	}
	return id, true
}
