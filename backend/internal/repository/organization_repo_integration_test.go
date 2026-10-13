//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOrganizationRepositoryRetainsCodeAndRejectsDuplicates(t *testing.T) {
	tx := testEntTx(t)
	repo := NewOrganizationRepository(tx.Client())
	creator := mustCreateUser(t, tx.Client(), &service.User{Email: "organization-creator@example.com", Role: service.RoleAdmin})

	created := &service.Organization{
		Name: "Example", Code: "example", Status: service.OrganizationStatusActive, CreatedBy: creator.ID,
	}
	require.NoError(t, repo.Create(context.Background(), created))
	require.NotZero(t, created.ID)

	duplicate := &service.Organization{
		Name: "Duplicate", Code: "example", Status: service.OrganizationStatusActive, CreatedBy: creator.ID,
	}
	err := repo.Create(context.Background(), duplicate)
	require.Error(t, err)
	require.True(t, errors.IsConflict(err))
}

func TestOrganizationRepositoryListFiltersAndWhitelistedSort(t *testing.T) {
	tx := testEntTx(t)
	repo := NewOrganizationRepository(tx.Client())
	creator := mustCreateUser(t, tx.Client(), &service.User{Email: "organization-list-creator@example.com", Role: service.RoleAdmin})

	require.NoError(t, repo.Create(context.Background(), &service.Organization{
		Name: "Zulu", Code: "zulu", Status: service.OrganizationStatusActive, CreatedBy: creator.ID,
	}))
	require.NoError(t, repo.Create(context.Background(), &service.Organization{
		Name: "Alpha", Code: "alpha", Status: service.OrganizationStatusDisabled, CreatedBy: creator.ID,
	}))

	items, page, err := repo.List(context.Background(), pagination.PaginationParams{
		Page: 1, PageSize: 20, SortBy: "name", SortOrder: pagination.SortOrderAsc,
	}, service.OrganizationListFilters{Status: service.OrganizationStatusActive, Search: "zul"})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, items, 1)
	require.Equal(t, "zulu", items[0].Code)
}

func TestOrganizationRepositoryPartialUpdatePreservesOtherFieldsAndRejectsArchived(t *testing.T) {
	tx := testEntTx(t)
	repo := NewOrganizationRepository(tx.Client())
	creator := mustCreateUser(t, tx.Client(), &service.User{Email: "organization-update-creator@example.com", Role: service.RoleAdmin})
	created := &service.Organization{
		Name: "Original", Code: "original", Description: "Keep me", Status: service.OrganizationStatusActive, CreatedBy: creator.ID,
	}
	require.NoError(t, repo.Create(context.Background(), created))

	name := "Renamed"
	updated, err := repo.Update(context.Background(), created.ID, service.UpdateOrganizationInput{Name: &name})
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)
	require.Equal(t, "original", updated.Code)
	require.Equal(t, "Keep me", updated.Description)
	require.Equal(t, service.OrganizationStatusActive, updated.Status)

	_, err = tx.Client().Organization.UpdateOneID(created.ID).
		SetStatus(service.OrganizationStatusArchived).
		Save(context.Background())
	require.NoError(t, err)
	description := "must not change"
	_, err = repo.Update(context.Background(), created.ID, service.UpdateOrganizationInput{Description: &description})
	require.ErrorIs(t, err, service.ErrOrganizationArchived)
}
