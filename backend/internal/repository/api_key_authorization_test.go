package repository

import (
	"context"
	"errors"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepository_GetByKeyForAuth_ResolvesOrganizationAuthorization(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "organization-authorization-owner@test.com")
	organization, err := client.Organization.Create().
		SetName("Organization Authorization").
		SetCode("organization-authorization").
		SetStatus(domain.OrganizationStatusActive).
		SetCreatedBy(owner.ID).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.User.UpdateOneID(owner.ID).
		SetOrganizationID(organization.ID).
		SetOrganizationRole(domain.OrganizationRoleMember).
		Save(ctx)
	require.NoError(t, err)

	allocation, err := client.OrganizationAllocation.Create().
		SetOrganizationID(organization.ID).
		SetUserIDSnapshot(owner.ID).
		SetUserRefID(owner.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.RequireFromString("12.34000000")).
		SetReserved(decimal.RequireFromString("0.56000000")).
		Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{
		UserID: owner.ID,
		Key:    "sk-organization-authorization",
		Name:   "organization authorization",
		Status: service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	got, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, got.OrganizationAuthorization)
	require.Equal(t, &organization.ID, got.OrganizationID)
	require.Equal(t, &organization.ID, got.User.OrganizationID)
	require.Equal(t, domain.OrganizationRoleMember, *got.User.OrganizationRole)
	require.Equal(t, service.OrganizationKeyAuthorization{
		APIKeyID:                   key.ID,
		UserID:                     owner.ID,
		OrganizationID:             organization.ID,
		OrganizationStatus:         domain.OrganizationStatusActive,
		OrganizationBalanceVersion: organization.BalanceVersion,
		AllocationID:               allocation.ID,
		AllocationUserID:           owner.ID,
		AllocationOrganizationID:   organization.ID,
		AllocationStatus:           domain.OrganizationAllocationStatusActive,
		AllocationVersion:          allocation.Version,
		Available:                  "12.34000000",
		Reserved:                   "0.56000000",
	}, *got.OrganizationAuthorization)

	_, err = client.Organization.UpdateOneID(organization.ID).
		SetStatus(domain.OrganizationStatusDisabled).
		Save(ctx)
	require.NoError(t, err)

	reloaded, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, reloaded.OrganizationAuthorization)
	require.Equal(t, domain.OrganizationStatusDisabled, reloaded.OrganizationAuthorization.OrganizationStatus)
}

func TestAPIKeyRepository_GetByKeyForAuth_AllowsVerifiedPersonalKeyWithClosedAllocation(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "personal-key-closed-allocation@test.com")
	organization, err := client.Organization.Create().
		SetName("Former Organization").
		SetCode("former-organization").
		SetStatus(domain.OrganizationStatusArchived).
		SetCreatedBy(owner.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.OrganizationAllocation.Create().
		SetOrganizationID(organization.ID).
		SetUserIDSnapshot(owner.ID).
		SetUserRefID(owner.ID).
		SetStatus(domain.OrganizationAllocationStatusClosed).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{
		UserID: owner.ID,
		Key:    "sk-personal-closed-allocation",
		Name:   "personal key after removal",
		Status: service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	got, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Nil(t, got.OrganizationID)
	require.NotNil(t, got.User)
	require.Nil(t, got.User.OrganizationID)
	require.Nil(t, got.User.OrganizationRole)
	require.Nil(t, got.OrganizationAuthorization)
}

func TestAPIKeyRepository_GetByKeyForAuth_RejectsPersonalKeyWithActiveAllocation(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "personal-key-active-allocation@test.com")
	organization, err := client.Organization.Create().
		SetName("Unexpected Organization Allocation").
		SetCode("unexpected-organization-allocation").
		SetStatus(domain.OrganizationStatusActive).
		SetCreatedBy(owner.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.OrganizationAllocation.Create().
		SetOrganizationID(organization.ID).
		SetUserIDSnapshot(owner.ID).
		SetUserRefID(owner.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)
	key, err := client.APIKey.Create().
		SetUserID(owner.ID).
		SetKey("sk-personal-active-allocation").
		SetName("personal key with unexpected allocation").
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	_, err = repo.GetByKeyForAuth(ctx, key.Key)
	require.ErrorIs(t, err, service.ErrOrganizationKeyAttributionInvalid)
	require.True(t, errors.Is(err, service.ErrOrganizationMembershipInconsistent))
}

func TestAPIKeyRepository_GetByKeyForAuth_RejectsInvalidOrganizationAttribution(t *testing.T) {
	tests := []struct {
		name             string
		createAllocation func(t *testing.T, client *dbent.Client, ctx context.Context, organizationID, foreignOrganizationID, userID int64)
	}{
		{
			name: "missing active allocation",
			createAllocation: func(t *testing.T, _ *dbent.Client, _ context.Context, _, _, _ int64) {
				t.Helper()
			},
		},
		{
			name: "closed allocation",
			createAllocation: func(t *testing.T, client *dbent.Client, ctx context.Context, organizationID, _ int64, userID int64) {
				t.Helper()
				_, err := client.OrganizationAllocation.Create().
					SetOrganizationID(organizationID).
					SetUserIDSnapshot(userID).
					SetUserRefID(userID).
					SetStatus(domain.OrganizationAllocationStatusClosed).
					SetAvailable(decimal.Zero).
					SetReserved(decimal.Zero).
					Save(ctx)
				require.NoError(t, err)
			},
		},
		{
			name: "foreign active allocation",
			createAllocation: func(t *testing.T, client *dbent.Client, ctx context.Context, _ int64, foreignOrganizationID, userID int64) {
				t.Helper()
				_, err := client.OrganizationAllocation.Create().
					SetOrganizationID(foreignOrganizationID).
					SetUserIDSnapshot(userID).
					SetUserRefID(userID).
					SetStatus(domain.OrganizationAllocationStatusActive).
					SetAvailable(decimal.Zero).
					SetReserved(decimal.Zero).
					Save(ctx)
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, client := newAPIKeyRepoSQLite(t)
			ctx := context.Background()
			owner := mustCreateAPIKeyRepoUser(t, ctx, client, "invalid-organization-authorization-"+tt.name+"@test.com")
			organization, err := client.Organization.Create().
				SetName("Expected Organization").
				SetCode("expected-organization-" + sanitizeAuthorizationTestCode(tt.name)).
				SetStatus(domain.OrganizationStatusActive).
				SetCreatedBy(owner.ID).
				Save(ctx)
			require.NoError(t, err)
			foreignOrganization, err := client.Organization.Create().
				SetName("Foreign Organization").
				SetCode("foreign-organization-" + sanitizeAuthorizationTestCode(tt.name)).
				SetStatus(domain.OrganizationStatusActive).
				SetCreatedBy(owner.ID).
				Save(ctx)
			require.NoError(t, err)
			_, err = client.User.UpdateOneID(owner.ID).
				SetOrganizationID(organization.ID).
				SetOrganizationRole(domain.OrganizationRoleMember).
				Save(ctx)
			require.NoError(t, err)

			tt.createAllocation(t, client, ctx, organization.ID, foreignOrganization.ID, owner.ID)
			key, err := client.APIKey.Create().
				SetUserID(owner.ID).
				SetOrganizationID(organization.ID).
				SetKey("sk-invalid-organization-authorization-" + sanitizeAuthorizationTestCode(tt.name)).
				SetName("invalid organization authorization").
				SetStatus(service.StatusActive).
				Save(ctx)
			require.NoError(t, err)

			_, err = repo.GetByKeyForAuth(ctx, key.Key)
			require.ErrorIs(t, err, service.ErrOrganizationKeyAttributionInvalid)
			require.True(t, errors.Is(err, service.ErrOrganizationMembershipInconsistent))
		})
	}
}

func TestAPIKeyRepository_GetByKeyForAuth_MapsStorageFailuresToResolverUnavailable(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	require.NoError(t, client.Close())

	_, err := repo.GetByKeyForAuth(context.Background(), "sk-storage-error")
	require.ErrorIs(t, err, service.ErrOrganizationKeyAuthorizationUnavailable)
}

func TestResolveAPIKeyOrganizationAuthorization_RejectsInvalidOrganizationRoleBeforeStorageLookup(t *testing.T) {
	organizationID := int64(23)
	invalidRole := "unexpected_role"
	key := &dbent.APIKey{
		ID:             17,
		UserID:         19,
		OrganizationID: &organizationID,
		Edges: dbent.APIKeyEdges{
			User: &dbent.User{
				ID:               19,
				OrganizationID:   &organizationID,
				OrganizationRole: &invalidRole,
			},
		},
	}

	_, err := resolveAPIKeyOrganizationAuthorization(context.Background(), nil, key)
	require.ErrorIs(t, err, service.ErrOrganizationKeyAttributionInvalid)
	require.True(t, errors.Is(err, service.ErrOrganizationMembershipInconsistent))
}

func sanitizeAuthorizationTestCode(value string) string {
	result := make([]rune, 0, len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			result = append(result, r)
			continue
		}
		result = append(result, '-')
	}
	return string(result)
}
