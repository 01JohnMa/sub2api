//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type countingAuthAPIKeyRepository struct {
	service.APIKeyRepository
	authCalls atomic.Int64
}

func (r *countingAuthAPIKeyRepository) GetByKeyForAuth(ctx context.Context, key string) (*service.APIKey, error) {
	r.authCalls.Add(1)
	return r.APIKeyRepository.GetByKeyForAuth(ctx, key)
}

func TestAPIKeyAuthCacheRedisPubSubInvalidatesOtherInstanceL1AfterMemberRemoval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := testEntClient(t)
	suffix := time.Now().UnixNano()
	actor := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("auth-cache-remove-actor-%d@example.com", suffix), Role: service.RoleAdmin})
	target := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("auth-cache-remove-target-%d@example.com", suffix)})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: target.ID, Key: fmt.Sprintf("sk-auth-cache-remove-%d", suffix), Name: "auth cache member"})
	organization := &service.Organization{Name: "Auth Cache Removal", Code: fmt.Sprintf("auth-cache-remove-%d", suffix), Status: service.OrganizationStatusActive, CreatedBy: actor.ID}
	require.NoError(t, NewOrganizationRepository(client).Create(ctx, organization))
	membershipRepo := NewOrganizationMembershipRepository(client)
	_, err := membershipRepo.Add(ctx, organization.ID, target.ID, service.OrganizationRoleMember)
	require.NoError(t, err)

	apiKeyRepo := &countingAuthAPIKeyRepository{APIKeyRepository: NewAPIKeyRepository(client, integrationDB)}
	authConfig := &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{L1Size: 1000, L1TTLSeconds: 60}}
	instanceA := service.NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, NewAPIKeyCache(testRedis(t)), authConfig)
	instanceB := service.NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, NewAPIKeyCache(testRedis(t)), authConfig)
	instanceB.StartAuthCacheInvalidationSubscriber(ctx)

	organizationKey, err := instanceB.GetByKey(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, &organization.ID, organizationKey.OrganizationID)
	require.Equal(t, int64(1), apiKeyRepo.authCalls.Load())
	warmDeadline := time.Now().Add(5 * time.Second)
	for {
		callsBefore := apiKeyRepo.authCalls.Load()
		cachedKey, authErr := instanceB.GetByKey(ctx, key.Key)
		require.NoError(t, authErr)
		require.Equal(t, &organization.ID, cachedKey.OrganizationID)
		if apiKeyRepo.authCalls.Load() == callsBefore {
			break
		}
		if time.Now().After(warmDeadline) {
			t.Fatal("instance B L1 cache did not warm")
		}
		time.Sleep(10 * time.Millisecond)
	}
	apiKeyRepo.authCalls.Store(0)

	manager := service.NewOrganizationMembershipService(membershipRepo, instanceA, nil)
	_, err = manager.Remove(ctx, platformMemberRemovalActor(actor.ID), organization.ID, target.ID, service.OrganizationMemberRemovalInput{}, fmt.Sprintf("auth-cache-removal-%d", suffix))
	require.NoError(t, err)

	deadline := time.Now().Add(5 * time.Second)
	for {
		personalKey, authErr := instanceB.GetByKey(ctx, key.Key)
		require.NoError(t, authErr)
		if personalKey.OrganizationID == nil && personalKey.User.OrganizationID == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("redis pub/sub did not invalidate instance B L1 cache")
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Equal(t, int64(1), apiKeyRepo.authCalls.Load())
}
