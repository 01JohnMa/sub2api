//go:build unit

package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type organizationAdminUserReaderStub struct {
	users []*service.User
	err   error
	calls int
}

func (s *organizationAdminUserReaderStub) GetByID(_ context.Context, _ int64) (*service.User, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(s.users) == 0 {
		return nil, service.ErrUserNotFound
	}
	index := s.calls
	if index >= len(s.users) {
		index = len(s.users) - 1
	}
	s.calls++
	return s.users[index], nil
}

type organizationReaderStub struct {
	organization *service.Organization
	err          error
}

func (s *organizationReaderStub) Get(_ context.Context, _ int64) (*service.Organization, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.organization, nil
}

func TestOrganizationAdminAuthUsesServerDerivedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	organizationID := int64(7)
	role := service.OrganizationRoleAdmin
	userReader := &organizationAdminUserReaderStub{users: []*service.User{{
		ID:               31,
		Status:           service.StatusActive,
		OrganizationID:   &organizationID,
		OrganizationRole: &role,
	}}}
	organizationReader := &organizationReaderStub{organization: &service.Organization{
		ID:     organizationID,
		Status: service.OrganizationStatusActive,
	}}

	router := organizationAuthTestRouter(userReader, organizationReader)
	request := httptest.NewRequest(http.MethodGet, "/organization?organization_id=99", nil)
	request.Header.Set("X-Organization-ID", "99")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, float64(organizationID), body["organization_id"])
	require.Equal(t, float64(31), body["user_id"])
}

func TestOrganizationAdminAuthDeniesInvalidCurrentAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	organizationID := int64(7)
	adminRole := service.OrganizationRoleAdmin
	memberRole := service.OrganizationRoleMember

	tests := []struct {
		name         string
		user         *service.User
		organization *service.Organization
		wantCode     string
	}{
		{name: "unorganized", user: &service.User{ID: 31, Status: service.StatusActive}, organization: &service.Organization{ID: organizationID, Status: service.OrganizationStatusActive}, wantCode: "ORGANIZATION_ADMIN_REQUIRED"},
		{name: "ordinary member", user: &service.User{ID: 31, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &memberRole}, organization: &service.Organization{ID: organizationID, Status: service.OrganizationStatusActive}, wantCode: "ORGANIZATION_ADMIN_REQUIRED"},
		{name: "disabled organization", user: &service.User{ID: 31, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &adminRole}, organization: &service.Organization{ID: organizationID, Status: service.OrganizationStatusDisabled}, wantCode: "ORGANIZATION_UNAVAILABLE"},
		{name: "archived organization", user: &service.User{ID: 31, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &adminRole}, organization: &service.Organization{ID: organizationID, Status: service.OrganizationStatusArchived}, wantCode: "ORGANIZATION_UNAVAILABLE"},
		{name: "mismatched organization", user: &service.User{ID: 31, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &adminRole}, organization: &service.Organization{ID: 8, Status: service.OrganizationStatusActive}, wantCode: "ORGANIZATION_UNAVAILABLE"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := organizationAuthTestRouter(
				&organizationAdminUserReaderStub{users: []*service.User{test.user}},
				&organizationReaderStub{organization: test.organization},
			)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/organization", nil))

			require.Equal(t, http.StatusForbidden, recorder.Code)
			var body ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, test.wantCode, body.Code)
		})
	}
}

func TestOrganizationAdminAuthReloadsRoleForEveryRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	organizationID := int64(7)
	adminRole := service.OrganizationRoleAdmin
	memberRole := service.OrganizationRoleMember
	userReader := &organizationAdminUserReaderStub{users: []*service.User{
		{ID: 31, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &adminRole},
		{ID: 31, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &memberRole},
	}}
	router := organizationAuthTestRouter(userReader, &organizationReaderStub{organization: &service.Organization{
		ID: organizationID, Status: service.OrganizationStatusActive,
	}})

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/organization", nil))
	require.Equal(t, http.StatusOK, first.Code)

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/organization", nil))
	require.Equal(t, http.StatusForbidden, second.Code)
	require.Equal(t, 2, userReader.calls)
}

func TestOrganizationAdminAuthFailsClosedOnReaderError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := organizationAuthTestRouter(
		&organizationAdminUserReaderStub{err: errors.New("database unavailable")},
		&organizationReaderStub{},
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/organization", nil))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestPersonalAccountAccessKeepsPlatformAdminsMembersAndUnorganizedUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	organizationID := int64(7)
	adminRole := service.OrganizationRoleAdmin
	memberRole := service.OrganizationRoleMember
	unknownRole := "unknown_role"
	activeOrganization := &service.Organization{ID: organizationID, Status: service.OrganizationStatusActive}

	tests := []struct {
		name     string
		user     *service.User
		want     int
		wantCode string
	}{
		{name: "platform admin", user: &service.User{ID: 31, Role: service.RoleAdmin, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &adminRole}, want: http.StatusOK},
		{name: "organization member", user: &service.User{ID: 31, Role: service.RoleUser, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &memberRole}, want: http.StatusOK},
		{name: "unorganized", user: &service.User{ID: 31, Role: service.RoleUser, Status: service.StatusActive}, want: http.StatusOK},
		{name: "active organization admin", user: &service.User{ID: 31, Role: service.RoleUser, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &adminRole}, want: http.StatusForbidden, wantCode: "ORGANIZATION_PERSONAL_ACCESS_FORBIDDEN"},
		{name: "disabled organization admin", user: &service.User{ID: 31, Role: service.RoleUser, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &adminRole}, want: http.StatusForbidden, wantCode: "ORGANIZATION_PERSONAL_ACCESS_FORBIDDEN"},
		{name: "unknown organization role", user: &service.User{ID: 31, Role: service.RoleUser, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &unknownRole}, want: http.StatusForbidden, wantCode: "ORGANIZATION_MEMBERSHIP_INCONSISTENT"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			organization := activeOrganization
			if test.name == "disabled organization admin" {
				organization = &service.Organization{ID: organizationID, Status: service.OrganizationStatusDisabled}
			}
			router := gin.New()
			router.GET(
				"/personal",
				func(c *gin.Context) {
					c.Set(string(ContextKeyUser), AuthSubject{UserID: 31})
					c.Next()
				},
				personalAccountAccess(
					&organizationAdminUserReaderStub{users: []*service.User{test.user}},
					&organizationReaderStub{organization: organization},
				),
				func(c *gin.Context) { c.Status(http.StatusOK) },
			)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/personal", nil))
			require.Equal(t, test.want, recorder.Code)
			if test.wantCode != "" {
				var body ErrorResponse
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
				require.Equal(t, test.wantCode, body.Code)
			}
		})
	}
}

func TestPersonalAccountAccessReloadsCurrentOrganizationRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	organizationID := int64(7)
	adminRole := service.OrganizationRoleAdmin
	memberRole := service.OrganizationRoleMember
	userReader := &organizationAdminUserReaderStub{users: []*service.User{
		{ID: 31, Role: service.RoleUser, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &adminRole},
		{ID: 31, Role: service.RoleUser, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &memberRole},
	}}
	organization := &organizationReaderStub{organization: &service.Organization{ID: organizationID, Status: service.OrganizationStatusActive}}
	router := gin.New()
	router.GET(
		"/personal",
		func(c *gin.Context) {
			c.Set(string(ContextKeyUser), AuthSubject{UserID: 31})
			c.Next()
		},
		personalAccountAccess(userReader, organization),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/personal", nil))
	require.Equal(t, http.StatusForbidden, first.Code)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/personal", nil))
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, 2, userReader.calls)
}

func organizationAuthTestRouter(userReader organizationAdminUserReader, organizationReader service.OrganizationReader) *gin.Engine {
	router := gin.New()
	router.GET(
		"/organization",
		func(c *gin.Context) {
			c.Set(string(ContextKeyUser), AuthSubject{UserID: 31})
			c.Next()
		},
		organizationAdminAuth(userReader, organizationReader),
		func(c *gin.Context) {
			scope, ok := GetOrganizationScopeFromContext(c)
			if !ok {
				c.Status(http.StatusInternalServerError)
				return
			}
			c.JSON(http.StatusOK, gin.H{"user_id": scope.UserID, "organization_id": scope.OrganizationID})
		},
	)
	return router
}
