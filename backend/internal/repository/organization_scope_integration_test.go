//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCurrentOrganizationScopeIgnoresForeignClientScopeAndReloadsRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	userRepo := newUserRepositoryWithSQL(tx.Client(), tx)
	organizationRepo := NewOrganizationRepository(tx.Client())
	membershipRepo := NewOrganizationMembershipRepository(tx.Client())
	organizationService := service.NewOrganizationService(organizationRepo)
	userService := service.NewUserService(userRepo, nil, nil, nil)

	creator := mustCreateUser(t, tx.Client(), &service.User{Email: "scope-creator@example.com", Role: service.RoleAdmin})
	first := &service.Organization{Name: "First", Code: "scope-first", Status: service.OrganizationStatusActive, CreatedBy: creator.ID}
	second := &service.Organization{Name: "Second", Code: "scope-second", Status: service.OrganizationStatusActive, CreatedBy: creator.ID}
	require.NoError(t, organizationRepo.Create(ctx, first))
	require.NoError(t, organizationRepo.Create(ctx, second))

	currentUser := mustCreateUser(t, tx.Client(), &service.User{Email: "scope-admin@example.com", Role: service.RoleUser})
	foreignUser := mustCreateUser(t, tx.Client(), &service.User{Email: "scope-foreign@example.com", Role: service.RoleUser})
	_, err := membershipRepo.Add(ctx, first.ID, currentUser.ID, service.OrganizationRoleAdmin)
	require.NoError(t, err)
	_, err = membershipRepo.Add(ctx, second.ID, foreignUser.ID, service.OrganizationRoleAdmin)
	require.NoError(t, err)

	auth := middleware.NewOrganizationAdminAuthMiddleware(userService, organizationService)
	router := gin.New()
	router.GET(
		"/api/v1/organization",
		func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: currentUser.ID})
			c.Next()
		},
		auth.Require,
		func(c *gin.Context) {
			scope, ok := middleware.GetOrganizationScopeFromContext(c)
			if !ok {
				c.Status(http.StatusInternalServerError)
				return
			}
			c.JSON(http.StatusOK, gin.H{"organization_id": scope.OrganizationID})
		},
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/organization?organization_id="+second.Code, nil)
	request.Header.Set("X-Organization-ID", second.Code)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, float64(first.ID), body["organization_id"])

	_, err = membershipRepo.ChangeRole(ctx, first.ID, currentUser.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	revoked := httptest.NewRecorder()
	router.ServeHTTP(revoked, httptest.NewRequest(http.MethodGet, "/api/v1/organization", nil))
	require.Equal(t, http.StatusForbidden, revoked.Code)

	_, err = membershipRepo.ChangeRole(ctx, first.ID, currentUser.ID, service.OrganizationRoleAdmin)
	require.NoError(t, err)
	_, err = tx.Client().Organization.UpdateOneID(first.ID).SetStatus(domain.OrganizationStatusDisabled).Save(ctx)
	require.NoError(t, err)
	disabled := httptest.NewRecorder()
	router.ServeHTTP(disabled, httptest.NewRequest(http.MethodGet, "/api/v1/organization", nil))
	require.Equal(t, http.StatusForbidden, disabled.Code)
}

func TestCurrentOrganizationRepositoriesScopeMembersAndBalanceTotals(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	organizationRepo := NewOrganizationRepository(tx.Client())
	membershipRepo := NewOrganizationMembershipRepository(tx.Client())

	creatorA := mustCreateUser(t, tx.Client(), &service.User{Email: "scope-data-creator-a@example.com", Role: service.RoleAdmin})
	creatorB := mustCreateUser(t, tx.Client(), &service.User{Email: "scope-data-creator-b@example.com", Role: service.RoleAdmin})
	organizationA := &service.Organization{Name: "Scope Data A", Code: "scope-data-a", Status: service.OrganizationStatusActive, CreatedBy: creatorA.ID}
	organizationB := &service.Organization{Name: "Scope Data B", Code: "scope-data-b", Status: service.OrganizationStatusActive, CreatedBy: creatorB.ID}
	require.NoError(t, organizationRepo.Create(ctx, organizationA))
	require.NoError(t, organizationRepo.Create(ctx, organizationB))

	memberA1 := mustCreateUser(t, tx.Client(), &service.User{Email: "scope-data-a1@example.com", Role: service.RoleUser})
	memberA2 := mustCreateUser(t, tx.Client(), &service.User{Email: "scope-data-a2@example.com", Role: service.RoleUser})
	memberB := mustCreateUser(t, tx.Client(), &service.User{Email: "scope-data-b@example.com", Role: service.RoleUser})
	_, err := membershipRepo.Add(ctx, organizationA.ID, memberA1.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	_, err = membershipRepo.Add(ctx, organizationA.ID, memberA2.ID, service.OrganizationRoleMember)
	require.NoError(t, err)
	_, err = membershipRepo.Add(ctx, organizationB.ID, memberB.ID, service.OrganizationRoleMember)
	require.NoError(t, err)

	members, page, err := membershipRepo.List(ctx, organizationA.ID, pagination.PaginationParams{Page: 2, PageSize: 1, SortBy: "email", SortOrder: pagination.SortOrderAsc})
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.Len(t, members, 1)
	require.NotEqual(t, memberB.ID, members[0].UserID)

	funding := service.NewOrganizationFundingService(NewOrganizationFundingRepository(tx.Client()))
	_, err = funding.Credit(ctx, fundingActorForUser(creatorA), organizationA.ID, "3.00000000", service.OrganizationFundingReasons{InternalReason: "scope data a"}, "scope-data-a-credit")
	require.NoError(t, err)
	_, err = funding.Credit(ctx, fundingActorForUser(creatorB), organizationB.ID, "7.00000000", service.OrganizationFundingReasons{InternalReason: "scope data b"}, "scope-data-b-credit")
	require.NoError(t, err)

	summary, err := funding.GetSummary(ctx, organizationA.ID)
	require.NoError(t, err)
	require.Equal(t, "3.00000000", summary.Unallocated)
	require.Equal(t, "3.00000000", summary.TotalRemaining)
}
