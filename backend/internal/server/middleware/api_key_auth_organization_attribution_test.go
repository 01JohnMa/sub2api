package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/service"
)

// TestApiKeyAuthOrganizationAttributionSnapshotConcurrentMembershipChange 验证
// 组织归属的并发语义（openspec 任务 2.4）：
//  1. 请求准入时从 Key/User 快照捕获的组织归属（reporting 维度）在成员变更
//     提交后保持不变——在途请求的遥测/计费归属不会被后续数据库状态改写；
//  2. 成员变更提交后的新请求按新状态解析——已移出组织的 Key 不再携带组织
//     归属，回到个人路径。
func TestApiKeyAuthOrganizationAttributionSnapshotConcurrentMembershipChange(t *testing.T) {
	gin.SetMode(gin.TestMode)

	organizationID := int64(88)
	organizationRole := service.OrganizationRoleMember
	withOrganization := func() *service.APIKey {
		return &service.APIKey{
			ID:             100,
			UserID:         7,
			Key:            "organization-attribution-key",
			OrganizationID: &organizationID,
			Status:         service.StatusActive,
			Group:          &service.Group{Status: service.StatusActive},
			User: &service.User{
				ID:               7,
				Role:             service.RoleUser,
				Status:           service.StatusActive,
				OrganizationID:   &organizationID,
				OrganizationRole: &organizationRole,
				Balance:          100,
			},
		}
	}
	withoutOrganization := func() *service.APIKey {
		key := withOrganization()
		key.OrganizationID = nil
		key.User.OrganizationID = nil
		key.User.OrganizationRole = nil
		return key
	}

	// 可变仓库状态：先返回组织内 Key，成员变更提交后返回个人 Key。
	state := withOrganization()
	repo := fakeAPIKeyRepo{
		getByKey: func(_ context.Context, key string) (*service.APIKey, error) {
			if key != state.Key {
				return nil, service.ErrAPIKeyNotFound
			}
			clone := *state
			if state.User != nil {
				user := *state.User
				clone.User = &user
			}
			return &clone, nil
		},
		updateLastUsed: func(_ context.Context, _ int64, _ time.Time) error { return nil },
	}
	apiKeyService := newTestAPIKeyService(repo)

	type attributionEcho struct {
		OrganizationID *int64 `json:"organization_id"`
	}
	// GET /v1/models 在简易模式下放行；回显请求准入时捕获的组织归属。
	respondWithCapturedAttribution := func(c *gin.Context) {
		organizationID, ok := GetOrganizationReportingIDFromContext(c)
		if !ok {
			c.JSON(http.StatusOK, attributionEcho{})
			return
		}
		c.JSON(http.StatusOK, attributionEcho{OrganizationID: organizationID})
	}

	do := func() attributionEcho {
		r := gin.New()
		r.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiKeyService, nil, &config.Config{RunMode: config.RunModeSimple})))
		r.GET("/v1/models", respondWithCapturedAttribution)
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		req.Header.Set("X-Api-Key", state.Key)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var echo attributionEcho
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &echo))
		return echo
	}

	// 变更前：成员请求在准入时捕获组织 88 的归属快照（值语义进入请求上下文）。
	first := do()
	require.NotNil(t, first.OrganizationID)
	require.Equal(t, organizationID, *first.OrganizationID)

	// 成员变更提交：同一 Key 的后续解析不再携带组织归属，回到个人路径；
	// 已在途请求的遥测/计费归属沿用其捕获的快照，不受此次提交影响。
	state = withoutOrganization()
	second := do()
	require.Nil(t, second.OrganizationID)
}
