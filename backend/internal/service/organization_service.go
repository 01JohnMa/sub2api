package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	OrganizationStatusActive   = domain.OrganizationStatusActive
	OrganizationStatusDisabled = domain.OrganizationStatusDisabled
	OrganizationStatusArchived = domain.OrganizationStatusArchived
)

var (
	ErrOrganizationNotFound    = infraerrors.NotFound("ORGANIZATION_NOT_FOUND", "organization not found")
	ErrOrganizationCodeExists  = infraerrors.Conflict("ORGANIZATION_CODE_EXISTS", "organization code already exists")
	ErrOrganizationArchived    = infraerrors.Conflict("ORGANIZATION_ARCHIVED", "archived organization cannot be edited")
	ErrInvalidOrganizationID   = infraerrors.BadRequest("INVALID_ORGANIZATION_ID", "invalid organization id")
	ErrInvalidOrganizationName = infraerrors.BadRequest("INVALID_ORGANIZATION_NAME", "organization name is required")
	ErrInvalidOrganizationCode = infraerrors.BadRequest("INVALID_ORGANIZATION_CODE", "organization code must contain only lowercase letters, numbers, hyphens, or underscores")
)

var organizationCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// OrganizationRepository owns only organization identity persistence. Later
// tickets add membership and financial repositories rather than overloading it.
type OrganizationRepository interface {
	Create(ctx context.Context, organization *Organization) error
	GetByID(ctx context.Context, id int64) (*Organization, error)
	List(ctx context.Context, params pagination.PaginationParams, filters OrganizationListFilters) ([]Organization, *pagination.PaginationResult, error)
	Update(ctx context.Context, id int64, input UpdateOrganizationInput) (*Organization, error)
}

// OrganizationReader is the read-only contract used by current-organization
// authorization and identity responses. It intentionally excludes platform
// lifecycle and write operations.
type OrganizationReader interface {
	Get(ctx context.Context, id int64) (*Organization, error)
}

// OrganizationManager is the narrow platform-admin contract used by HTTP.
type OrganizationManager interface {
	OrganizationReader
	Create(ctx context.Context, actorID int64, input CreateOrganizationInput) (*Organization, error)
	List(ctx context.Context, params pagination.PaginationParams, filters OrganizationListFilters) ([]Organization, *pagination.PaginationResult, error)
	Update(ctx context.Context, id int64, input UpdateOrganizationInput) (*Organization, error)
}

type CreateOrganizationInput struct {
	Name        string
	Code        string
	Description string
}

type UpdateOrganizationInput struct {
	Name        *string
	Code        *string
	Description *string
}

type OrganizationListFilters struct {
	Status string
	Search string
}

type OrganizationService struct {
	repo OrganizationRepository
}

func NewOrganizationService(repo OrganizationRepository) *OrganizationService {
	return &OrganizationService{repo: repo}
}

func (s *OrganizationService) Create(ctx context.Context, actorID int64, input CreateOrganizationInput) (*Organization, error) {
	if actorID <= 0 {
		return nil, infraerrors.Forbidden("ORGANIZATION_ADMIN_REQUIRED", "platform administrator is required")
	}

	name, err := normalizeOrganizationName(input.Name)
	if err != nil {
		return nil, err
	}
	code, err := normalizeOrganizationCode(input.Code)
	if err != nil {
		return nil, err
	}

	organization := &Organization{
		Name:        name,
		Code:        code,
		Description: strings.TrimSpace(input.Description),
		Status:      OrganizationStatusActive,
		CreatedBy:   actorID,
	}
	if err := s.repo.Create(ctx, organization); err != nil {
		return nil, fmt.Errorf("create organization: %w", err)
	}
	return organization, nil
}

func (s *OrganizationService) Get(ctx context.Context, id int64) (*Organization, error) {
	if id <= 0 {
		return nil, ErrInvalidOrganizationID
	}
	organization, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get organization: %w", err)
	}
	return organization, nil
}

func (s *OrganizationService) List(ctx context.Context, params pagination.PaginationParams, filters OrganizationListFilters) ([]Organization, *pagination.PaginationResult, error) {
	filters.Status = strings.TrimSpace(filters.Status)
	filters.Search = strings.TrimSpace(filters.Search)
	if filters.Status != "" && !isOrganizationStatus(filters.Status) {
		return nil, nil, infraerrors.BadRequest("INVALID_ORGANIZATION_STATUS", "invalid organization status")
	}
	searchRunes := []rune(filters.Search)
	if len(searchRunes) > 100 {
		filters.Search = string(searchRunes[:100])
	}
	params.SortBy = normalizeOrganizationSortBy(params.SortBy)
	params.SortOrder = params.NormalizedSortOrder(pagination.SortOrderDesc)

	organizations, page, err := s.repo.List(ctx, params, filters)
	if err != nil {
		return nil, nil, fmt.Errorf("list organizations: %w", err)
	}
	return organizations, page, nil
}

func isOrganizationStatus(status string) bool {
	switch status {
	case OrganizationStatusActive, OrganizationStatusDisabled, OrganizationStatusArchived:
		return true
	default:
		return false
	}
}

func normalizeOrganizationSortBy(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "name", "code", "status", "created_at":
		return normalized
	default:
		return "created_at"
	}
}

func (s *OrganizationService) Update(ctx context.Context, id int64, input UpdateOrganizationInput) (*Organization, error) {
	if id <= 0 {
		return nil, ErrInvalidOrganizationID
	}

	normalized := UpdateOrganizationInput{}
	if input.Name != nil {
		name, err := normalizeOrganizationName(*input.Name)
		if err != nil {
			return nil, err
		}
		normalized.Name = &name
	}
	if input.Code != nil {
		code, err := normalizeOrganizationCode(*input.Code)
		if err != nil {
			return nil, err
		}
		normalized.Code = &code
	}
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		normalized.Description = &description
	}

	if normalized.Name == nil && normalized.Code == nil && normalized.Description == nil {
		return s.Get(ctx, id)
	}
	organization, err := s.repo.Update(ctx, id, normalized)
	if err != nil {
		return nil, fmt.Errorf("update organization: %w", err)
	}
	return organization, nil
}

func normalizeOrganizationName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", ErrInvalidOrganizationName
	}
	return name, nil
}

func normalizeOrganizationCode(value string) (string, error) {
	code := strings.ToLower(strings.TrimSpace(value))
	if !organizationCodePattern.MatchString(code) {
		return "", ErrInvalidOrganizationCode
	}
	return code, nil
}
