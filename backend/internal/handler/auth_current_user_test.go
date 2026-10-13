//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAuthHandlerGetCurrentUserReturnsProfileCompatibilityFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	verifiedAt := time.Date(2026, 4, 20, 8, 30, 0, 0, time.UTC)
	repo := &userHandlerRepoStub{
		user: &service.User{
			ID:           31,
			Email:        "me@example.com",
			Username:     "linuxdo-handle",
			Role:         service.RoleUser,
			Status:       service.StatusActive,
			AvatarURL:    "https://cdn.example.com/linuxdo.png",
			AvatarSource: "remote_url",
		},
		identities: []service.UserAuthIdentityRecord{
			{
				ProviderType:    "linuxdo",
				ProviderKey:     "linuxdo",
				ProviderSubject: "linuxdo-subject-31",
				VerifiedAt:      &verifiedAt,
				Metadata: map[string]any{
					"username":   "linuxdo-handle",
					"avatar_url": "https://cdn.example.com/linuxdo.png",
				},
			},
		},
	}

	handler := &AuthHandler{
		userService: service.NewUserService(repo, nil, nil, nil),
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 31})

	handler.GetCurrentUser(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, true, resp.Data["email_bound"])
	require.Equal(t, true, resp.Data["linuxdo_bound"])
	require.Equal(t, "https://cdn.example.com/linuxdo.png", resp.Data["avatar_url"])
	require.Contains(t, resp.Data, "balance")
	require.Contains(t, resp.Data, "concurrency")

	authBindings, ok := resp.Data["auth_bindings"].(map[string]any)
	require.True(t, ok)
	linuxdoBinding, ok := authBindings["linuxdo"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, linuxdoBinding["bound"])

	avatarSource, ok := resp.Data["avatar_source"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "linuxdo", avatarSource["provider"])
	require.Equal(t, "linuxdo", avatarSource["source"])

	profileSources, ok := resp.Data["profile_sources"].(map[string]any)
	require.True(t, ok)
	usernameSource, ok := profileSources["username"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "linuxdo", usernameSource["provider"])
	require.Equal(t, "linuxdo", usernameSource["source"])
	require.Nil(t, resp.Data["organization"])
	require.Nil(t, resp.Data["organization_role"])
}

type authOrganizationReaderStub struct {
	organization *service.Organization
}

func (s *authOrganizationReaderStub) Get(_ context.Context, _ int64) (*service.Organization, error) {
	return s.organization, nil
}

func TestAuthHandlerGetCurrentUserReturnsCurrentOrganizationIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	organizationID := int64(7)
	role := service.OrganizationRoleAdmin
	repo := &userHandlerRepoStub{user: &service.User{
		ID:               31,
		Email:            "admin@example.com",
		Username:         "org-admin",
		Role:             service.RoleUser,
		Balance:          19.75,
		Concurrency:      8,
		RPMLimit:         120,
		Status:           service.StatusActive,
		OrganizationID:   &organizationID,
		OrganizationRole: &role,
	}}
	handler := &AuthHandler{
		userService: service.NewUserService(repo, nil, nil, nil),
		organizationService: &authOrganizationReaderStub{organization: &service.Organization{
			ID:          organizationID,
			Name:        "Acme",
			Code:        "acme",
			Description: "Engineering",
			Status:      service.OrganizationStatusActive,
		}},
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 31})

	handler.GetCurrentUser(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "user", resp.Data["role"])
	require.Equal(t, "organization_admin", resp.Data["organization_role"])
	organization, ok := resp.Data["organization"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(organizationID), organization["id"])
	require.Equal(t, "Acme", organization["name"])
	require.Equal(t, "active", organization["status"])
	require.NotContains(t, organization, "created_by")
	for _, field := range []string{
		"balance",
		"concurrency",
		"allowed_groups",
		"balance_notify_enabled",
		"balance_notify_threshold_type",
		"balance_notify_threshold",
		"balance_notify_extra_emails",
		"total_recharged",
		"rpm_limit",
		"api_keys",
		"subscriptions",
	} {
		require.NotContains(t, resp.Data, field)
	}
}

func TestAuthHandlerLoginUserResponsePreservesPlatformRoleAndOrganizationIdentity(t *testing.T) {
	organizationID := int64(7)
	role := service.OrganizationRoleAdmin
	handler := &AuthHandler{
		cfg: &config.Config{RunMode: config.RunModeSimple},
		organizationService: &authOrganizationReaderStub{organization: &service.Organization{
			ID: organizationID, Name: "Acme", Code: "acme", Status: service.OrganizationStatusActive,
		}},
	}

	result, err := handler.authenticatedUserResponse(context.Background(), &service.User{
		ID:               31,
		Email:            "admin@example.com",
		Role:             service.RoleUser,
		Status:           service.StatusActive,
		OrganizationID:   &organizationID,
		OrganizationRole: &role,
	})

	require.NoError(t, err)
	require.Equal(t, service.RoleUser, result.Role)
	require.Equal(t, service.OrganizationRoleAdmin, *result.OrganizationRole)
	require.Equal(t, organizationID, result.Organization.ID)
	require.Equal(t, config.RunModeSimple, result.RunMode)
	require.Nil(t, result.AuthenticatedUserPersonalFields)

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(encoded, &payload))
	for _, field := range []string{
		"balance",
		"concurrency",
		"allowed_groups",
		"balance_notify_enabled",
		"balance_notify_threshold_type",
		"balance_notify_threshold",
		"balance_notify_extra_emails",
		"total_recharged",
		"rpm_limit",
		"api_keys",
		"subscriptions",
	} {
		require.NotContains(t, payload, field)
	}
}

func TestAuthHandlerLoginResponseOmitsPersonalFieldsForNonActiveOrganizationAdmin(t *testing.T) {
	organizationID := int64(7)
	role := service.OrganizationRoleAdmin

	for _, status := range []string{service.OrganizationStatusDisabled, service.OrganizationStatusArchived} {
		t.Run(status, func(t *testing.T) {
			handler := &AuthHandler{
				organizationService: &authOrganizationReaderStub{organization: &service.Organization{
					ID: organizationID, Name: "Acme", Code: "acme", Status: status,
				}},
			}

			result, err := handler.authenticatedUserResponse(context.Background(), &service.User{
				ID:               31,
				Email:            "admin@example.com",
				Role:             service.RoleUser,
				Balance:          19.75,
				Concurrency:      8,
				Status:           service.StatusActive,
				OrganizationID:   &organizationID,
				OrganizationRole: &role,
			})

			require.NoError(t, err)
			require.Equal(t, status, result.Organization.Status)
			require.Nil(t, result.AuthenticatedUserPersonalFields)
		})
	}
}

func TestAuthHandlerLoginResponseKeepsPersonalFieldsForPlatformAdmin(t *testing.T) {
	organizationID := int64(7)
	role := service.OrganizationRoleAdmin
	handler := &AuthHandler{
		organizationService: &authOrganizationReaderStub{organization: &service.Organization{
			ID: organizationID, Name: "Acme", Code: "acme", Status: service.OrganizationStatusActive,
		}},
	}

	result, err := handler.authenticatedUserResponse(context.Background(), &service.User{
		ID:               31,
		Email:            "platform-admin@example.com",
		Role:             service.RoleAdmin,
		Balance:          23.5,
		Concurrency:      9,
		Status:           service.StatusActive,
		OrganizationID:   &organizationID,
		OrganizationRole: &role,
	})

	require.NoError(t, err)
	require.NotNil(t, result.AuthenticatedUserPersonalFields)
	require.Equal(t, 23.5, result.Balance)
	require.Equal(t, 9, result.Concurrency)
}
