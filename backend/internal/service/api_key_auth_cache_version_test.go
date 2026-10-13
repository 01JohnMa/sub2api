package service

import (
	"context"
	"testing"
)

func TestAPIKeyService_RejectsV10AuthSnapshotWithoutModelsListConfig(t *testing.T) {
	groupID := int64(9)
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-models-list", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  10,
			APIKeyID: 1,
			UserID:   2,
			GroupID:  &groupID,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:          2,
				Status:      StatusActive,
				Role:        RoleUser,
				Balance:     10,
				Concurrency: 3,
			},
			Group: &APIKeyAuthGroupSnapshot{
				ID:               groupID,
				Name:             "openai",
				Platform:         PlatformOpenAI,
				Status:           StatusActive,
				SubscriptionType: SubscriptionTypeStandard,
				RateMultiplier:   1,
			},
		},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatalf("expected v10 auth snapshot to be rejected after models_list_config was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}

func TestAPIKeyService_RejectsV12AuthSnapshotWithoutOrganizationAttribution(t *testing.T) {
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-organization", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  12,
			APIKeyID: 1,
			UserID:   2,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:          2,
				Status:      StatusActive,
				Role:        RoleUser,
				Concurrency: 1,
			},
		},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok || apiKey != nil {
		t.Fatalf("expected v12 auth snapshot to be rejected after organization attribution was added")
	}
}

func TestAPIKeyService_AuthSnapshotRoundTripPreservesOrganizationAttribution(t *testing.T) {
	organizationID := int64(17)
	organizationRole := OrganizationRoleAdmin
	svc := &APIKeyService{}

	apiKey := &APIKey{
		ID:             1,
		UserID:         2,
		Key:            "k-org",
		Status:         StatusActive,
		OrganizationID: &organizationID,
		User: &User{
			ID:               2,
			Status:           StatusActive,
			Role:             RoleUser,
			OrganizationID:   &organizationID,
			OrganizationRole: &organizationRole,
		},
	}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	if snapshot.Version != apiKeyAuthSnapshotVersion {
		t.Fatalf("snapshot version = %d, want %d", snapshot.Version, apiKeyAuthSnapshotVersion)
	}
	rebuilt := svc.snapshotToAPIKey(apiKey.Key, snapshot)
	if rebuilt.OrganizationID == nil || *rebuilt.OrganizationID != organizationID {
		t.Fatalf("API Key organization attribution was not preserved: %#v", rebuilt.OrganizationID)
	}
	if rebuilt.User.OrganizationID == nil || *rebuilt.User.OrganizationID != organizationID {
		t.Fatalf("User organization attribution was not preserved: %#v", rebuilt.User.OrganizationID)
	}
	if rebuilt.User.OrganizationRole == nil || *rebuilt.User.OrganizationRole != organizationRole {
		t.Fatalf("User organization role was not preserved: %#v", rebuilt.User.OrganizationRole)
	}
}
