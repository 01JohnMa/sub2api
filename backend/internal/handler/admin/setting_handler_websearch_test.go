//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type missingWebSearchSettingRepoStub struct {
	*settingHandlerRepoStub
}

func (s *missingWebSearchSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if key == service.SettingKeyWebSearchEmulationConfig {
		return "", service.ErrSettingNotFound
	}
	return s.settingHandlerRepoStub.GetValue(ctx, key)
}

func TestSettingHandler_GetWebSearchEmulationConfig_MissingSettingReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &missingWebSearchSettingRepoStub{
		settingHandlerRepoStub: &settingHandlerRepoStub{},
	}
	svc := service.NewSettingService(repo, &config.Config{})
	handler := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/settings/web-search-emulation",
		nil,
	)

	handler.GetWebSearchEmulationConfig(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Code *int `json:"code"`
		Data struct {
			Enabled   *bool                              `json:"enabled"`
			Providers *[]service.WebSearchProviderConfig `json:"providers"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Code)
	require.Zero(t, *resp.Code)
	require.NotNil(t, resp.Data.Enabled)
	require.False(t, *resp.Data.Enabled)
	require.NotNil(t, resp.Data.Providers)
	require.Empty(t, *resp.Data.Providers)
}
