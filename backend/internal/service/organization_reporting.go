package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var ErrOrganizationReportRangeInvalid = infraerrors.BadRequest(
	"ORGANIZATION_REPORT_RANGE_INVALID",
	"organization report requires a valid half-open start and end range",
)

// OrganizationReportRange is deliberately explicit so reporting never falls
// back to a server-local rolling window or a projection insertion timestamp.
type OrganizationReportRange struct {
	Start time.Time
	End   time.Time
}

func (r OrganizationReportRange) Valid() bool {
	return !r.Start.IsZero() && !r.End.IsZero() && r.Start.Before(r.End)
}

type OrganizationReport struct {
	OrganizationID       int64
	Start                time.Time
	End                  time.Time
	CurrentMemberCount   int64
	ActiveMemberCount    int64
	APIKeyCount          int64
	RequestCountTotal    int64
	SuccessCount         int64
	ErrorCountTotal      int64
	BusinessLimitedCount int64
	ErrorCountSLA        int64
	SuccessRate          float64
	ErrorRate            float64
	InputTokens          int64
	OutputTokens         int64
	CacheCreationTokens  int64
	CacheReadTokens      int64
	Cost                 string
	Funds                OrganizationBalanceSummary
	Models               []OrganizationReportModel
	Trends               []OrganizationReportTrend
}

type OrganizationReportModel struct {
	Model               string
	RequestCount        int64
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	Cost                string
}

type OrganizationReportTrend struct {
	OccurredOn          string
	RequestCount        int64
	SuccessCount        int64
	ErrorCountTotal     int64
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	Cost                string
}

// OrganizationReportMemberUsage contains no personal balance, API key, or
// provider data. A nil UserID denotes the single non-actionable departed bucket.
type OrganizationReportMemberUsage struct {
	UserID              *int64
	DisplayName         string
	RequestCount        int64
	SuccessCount        int64
	ErrorCountTotal     int64
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	Cost                string
}

// OrganizationReportMemberFilters restricts a member-usage read to current
// organization resources. Every non-nil ID is exact and must be validated by
// the repository before aggregation so a foreign ID is never silently ignored.
type OrganizationReportMemberFilters struct {
	UserID       *int64
	AllocationID *int64
	APIKeyID     *int64
}

// OrganizationAPIKeyMetadata is intentionally a narrow organization-admin
// read model. In particular it has no raw key, ACL, credentials, or rate-window
// fields that could be used to recover an owner secret or alter key behavior.
type OrganizationAPIKeyMetadata struct {
	ID             int64
	Name           string
	Status         string
	UserID         int64
	MemberName     string
	OrganizationID int64
	CreatedAt      time.Time
	LastUsedAt     *time.Time
	ExpiresAt      *time.Time
	Quota          float64
	QuotaUsed      float64
}

type OrganizationReportMemberFilterReader interface {
	ListReportMembersFiltered(context.Context, int64, OrganizationReportRange, pagination.PaginationParams, OrganizationReportMemberFilters) ([]OrganizationReportMemberUsage, *OrganizationReportMemberUsage, *pagination.PaginationResult, error)
	ListOrganizationAPIKeyMetadata(context.Context, int64, pagination.PaginationParams, OrganizationReportMemberFilters) ([]OrganizationAPIKeyMetadata, *pagination.PaginationResult, error)
}

// OrganizationReportingDimensionReader is the minimal reporting seam for the
// first organization release. It reads existing usage/error projections and
// must not depend on organization funding or execution evidence.
type OrganizationReportingDimensionReader interface {
	GetReportingDimensionReport(context.Context, int64, OrganizationReportRange) (*OrganizationReport, error)
}

func (s *OrganizationFundingService) GetReport(ctx context.Context, organizationID int64, reportRange OrganizationReportRange) (*OrganizationReport, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOrganizationFundingRepositoryUnavailable
	}
	if organizationID <= 0 {
		return nil, ErrInvalidOrganizationID
	}
	if !reportRange.Valid() {
		return nil, ErrOrganizationReportRangeInvalid
	}
	if reader, ok := s.repo.(OrganizationReportingDimensionReader); ok {
		return reader.GetReportingDimensionReport(ctx, organizationID, reportRange)
	}
	return s.repo.GetReport(ctx, organizationID, reportRange)
}

func (s *OrganizationFundingService) ListReportMembers(ctx context.Context, organizationID int64, reportRange OrganizationReportRange, params pagination.PaginationParams) ([]OrganizationReportMemberUsage, *OrganizationReportMemberUsage, *pagination.PaginationResult, error) {
	if s == nil || s.repo == nil {
		return nil, nil, nil, ErrOrganizationFundingRepositoryUnavailable
	}
	if organizationID <= 0 {
		return nil, nil, nil, ErrInvalidOrganizationID
	}
	if !reportRange.Valid() {
		return nil, nil, nil, ErrOrganizationReportRangeInvalid
	}
	return s.repo.ListReportMembers(ctx, organizationID, reportRange, params)
}

func (s *OrganizationFundingService) ListReportMembersFiltered(ctx context.Context, organizationID int64, reportRange OrganizationReportRange, params pagination.PaginationParams, filters OrganizationReportMemberFilters) ([]OrganizationReportMemberUsage, *OrganizationReportMemberUsage, *pagination.PaginationResult, error) {
	if s == nil || s.repo == nil {
		return nil, nil, nil, ErrOrganizationFundingRepositoryUnavailable
	}
	if organizationID <= 0 {
		return nil, nil, nil, ErrInvalidOrganizationID
	}
	if !reportRange.Valid() {
		return nil, nil, nil, ErrOrganizationReportRangeInvalid
	}
	reader, ok := s.repo.(OrganizationReportMemberFilterReader)
	if !ok {
		return nil, nil, nil, ErrOrganizationFundingRepositoryUnavailable
	}
	return reader.ListReportMembersFiltered(ctx, organizationID, reportRange, params, filters)
}

func (s *OrganizationFundingService) ListOrganizationAPIKeyMetadata(ctx context.Context, organizationID int64, params pagination.PaginationParams, filters OrganizationReportMemberFilters) ([]OrganizationAPIKeyMetadata, *pagination.PaginationResult, error) {
	if s == nil || s.repo == nil {
		return nil, nil, ErrOrganizationFundingRepositoryUnavailable
	}
	if organizationID <= 0 {
		return nil, nil, ErrInvalidOrganizationID
	}
	reader, ok := s.repo.(OrganizationReportMemberFilterReader)
	if !ok {
		return nil, nil, ErrOrganizationFundingRepositoryUnavailable
	}
	return reader.ListOrganizationAPIKeyMetadata(ctx, organizationID, params, filters)
}
