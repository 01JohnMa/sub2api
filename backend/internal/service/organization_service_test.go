package service

import (
	"context"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type organizationRepositoryStub struct {
	created     *Organization
	updated     *Organization
	items       []Organization
	get         *Organization
	err         error
	params      pagination.PaginationParams
	filters     OrganizationListFilters
	updateInput UpdateOrganizationInput
}

func (s *organizationRepositoryStub) Create(_ context.Context, organization *Organization) error {
	s.created = organization
	organization.ID = 1
	return s.err
}

func (s *organizationRepositoryStub) GetByID(context.Context, int64) (*Organization, error) {
	return s.get, s.err
}

func (s *organizationRepositoryStub) List(_ context.Context, params pagination.PaginationParams, filters OrganizationListFilters) ([]Organization, *pagination.PaginationResult, error) {
	s.params = params
	s.filters = filters
	return s.items, &pagination.PaginationResult{Total: int64(len(s.items)), Page: 1, PageSize: 20, Pages: 1}, s.err
}

func TestOrganizationServiceListNormalizesFiltersAndWhitelistsSort(t *testing.T) {
	repo := &organizationRepositoryStub{}
	svc := NewOrganizationService(repo)

	_, _, err := svc.List(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20, SortBy: "untrusted", SortOrder: "ASC"}, OrganizationListFilters{
		Status: " active ", Search: "  example  ",
	})

	require.NoError(t, err)
	require.Equal(t, "active", repo.filters.Status)
	require.Equal(t, "example", repo.filters.Search)
	require.Equal(t, "created_at", repo.params.SortBy)
	require.Equal(t, pagination.SortOrderAsc, repo.params.SortOrder)
}

func TestOrganizationServiceListRejectsUnknownStatus(t *testing.T) {
	repo := &organizationRepositoryStub{}
	svc := NewOrganizationService(repo)

	_, _, err := svc.List(context.Background(), pagination.DefaultPagination(), OrganizationListFilters{Status: "deleted"})

	require.Error(t, err)
	require.Equal(t, "INVALID_ORGANIZATION_STATUS", infraerrors.Reason(err))
}

func TestOrganizationServiceListAllowsStatusSort(t *testing.T) {
	repo := &organizationRepositoryStub{}
	svc := NewOrganizationService(repo)

	_, _, err := svc.List(context.Background(), pagination.PaginationParams{SortBy: " STATUS ", SortOrder: "asc"}, OrganizationListFilters{})

	require.NoError(t, err)
	require.Equal(t, "status", repo.params.SortBy)
	require.Equal(t, pagination.SortOrderAsc, repo.params.SortOrder)
}

func (s *organizationRepositoryStub) Update(_ context.Context, id int64, input UpdateOrganizationInput) (*Organization, error) {
	s.updateInput = input
	if s.get == nil {
		return nil, s.err
	}
	updated := *s.get
	updated.ID = id
	if input.Name != nil {
		updated.Name = *input.Name
	}
	if input.Code != nil {
		updated.Code = *input.Code
	}
	if input.Description != nil {
		updated.Description = *input.Description
	}
	s.updated = &updated
	return &updated, s.err
}

func TestOrganizationServiceCreateNormalizesCodeAndDefaultsActive(t *testing.T) {
	repo := &organizationRepositoryStub{}
	svc := NewOrganizationService(repo)

	organization, err := svc.Create(context.Background(), 9, CreateOrganizationInput{
		Name:        "  Example Team  ",
		Code:        "  Example_Team  ",
		Description: "  Internal testing  ",
	})

	require.NoError(t, err)
	require.Equal(t, int64(1), organization.ID)
	require.Equal(t, "Example Team", organization.Name)
	require.Equal(t, "example_team", organization.Code)
	require.Equal(t, "Internal testing", organization.Description)
	require.Equal(t, OrganizationStatusActive, organization.Status)
	require.Equal(t, int64(9), organization.CreatedBy)
	require.Same(t, organization, repo.created)
}

func TestOrganizationServiceCreateRejectsInvalidCodeBeforePersistence(t *testing.T) {
	repo := &organizationRepositoryStub{}
	svc := NewOrganizationService(repo)

	_, err := svc.Create(context.Background(), 9, CreateOrganizationInput{Name: "Example", Code: "bad code"})

	require.Error(t, err)
	require.True(t, infraerrors.IsBadRequest(err))
	require.Equal(t, "INVALID_ORGANIZATION_CODE", infraerrors.Reason(err))
	require.Nil(t, repo.created)
}

func TestOrganizationServiceUpdateNormalizesCodeAndPreservesImmutableFields(t *testing.T) {
	createdAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	repo := &organizationRepositoryStub{get: &Organization{
		ID:          7,
		Name:        "Old name",
		Code:        "old-code",
		Description: "Old description",
		Status:      OrganizationStatusActive,
		CreatedBy:   3,
		CreatedAt:   createdAt,
	}}
	svc := NewOrganizationService(repo)
	name := " New name "
	code := " New_Code "
	description := ""

	organization, err := svc.Update(context.Background(), 7, UpdateOrganizationInput{
		Name:        &name,
		Code:        &code,
		Description: &description,
	})

	require.NoError(t, err)
	require.Equal(t, "New name", organization.Name)
	require.Equal(t, "new_code", organization.Code)
	require.Equal(t, "", organization.Description)
	require.Equal(t, OrganizationStatusActive, organization.Status)
	require.Equal(t, int64(3), organization.CreatedBy)
	require.Equal(t, createdAt, organization.CreatedAt)
	require.Equal(t, "new_code", *repo.updateInput.Code)
	require.Equal(t, "", *repo.updateInput.Description)
	require.Equal(t, "New name", *repo.updateInput.Name)
	require.Equal(t, organization, repo.updated)
}

func TestOrganizationServiceListTruncatesUnicodeSearchByRune(t *testing.T) {
	repo := &organizationRepositoryStub{}
	svc := NewOrganizationService(repo)
	search := strings.Repeat("组", 101)

	_, _, err := svc.List(context.Background(), pagination.DefaultPagination(), OrganizationListFilters{Search: search})

	require.NoError(t, err)
	require.Equal(t, strings.Repeat("组", 100), repo.filters.Search)
}
