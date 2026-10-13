//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOrganizationMigrationDefinesRetainedLifecycleSchema(t *testing.T) {
	tx := testTx(t)

	var organizationTable sql.NullString
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.organizations')").Scan(&organizationTable))
	require.True(t, organizationTable.Valid)
	requireColumn(t, tx, "organizations", "code", "character varying", 64, false)
	requireColumn(t, tx, "organizations", "created_by", "bigint", 0, false)
	requireColumn(t, tx, "organizations", "created_at", "timestamp with time zone", 0, false)
	requireColumn(t, tx, "organizations", "updated_at", "timestamp with time zone", 0, false)
	requireConstraintDefinitionContains(t, tx, "organizations", "organizations_status_check", "status", "'active'", "'disabled'", "'archived'")
	requireForeignKeyOnDelete(t, tx, "organizations", "created_by", "users", "RESTRICT")
	requireIndex(t, tx, "organizations", "organizations_code_unique")
}

func TestOrganizationMembershipMigrationDefinesNullableAttributionAndAllocationGuards(t *testing.T) {
	tx := testTx(t)

	requireColumn(t, tx, "users", "organization_id", "bigint", 0, true)
	requireColumn(t, tx, "users", "organization_role", "character varying", 32, true)
	requireConstraintDefinitionContains(
		t, tx, "users", "users_organization_assignment_check",
		"organization_id IS NULL", "organization_role IS NULL", "organization_admin", "organization_member",
	)
	requireForeignKeyOnDelete(t, tx, "users", "organization_id", "organizations", "RESTRICT")

	requireColumn(t, tx, "api_keys", "organization_id", "bigint", 0, true)
	requireForeignKeyOnDelete(t, tx, "api_keys", "organization_id", "organizations", "RESTRICT")

	var allocationTable sql.NullString
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.organization_allocations')").Scan(&allocationTable))
	require.True(t, allocationTable.Valid)
	requireColumn(t, tx, "organization_allocations", "organization_id", "bigint", 0, false)
	requireColumn(t, tx, "organization_allocations", "user_id_snapshot", "bigint", 0, false)
	requireColumn(t, tx, "organization_allocations", "user_ref_id", "bigint", 0, true)
	requireColumn(t, tx, "organization_allocations", "available", "numeric", 0, false)
	requireColumn(t, tx, "organization_allocations", "reserved", "numeric", 0, false)
	requireNumericPrecisionScale(t, tx, "organization_allocations", "available", 20, 8)
	requireNumericPrecisionScale(t, tx, "organization_allocations", "reserved", 20, 8)
	requireConstraintDefinitionContains(t, tx, "organization_allocations", "organization_allocations_status_check", "active", "closed")
	requireConstraintDefinitionContains(t, tx, "organization_allocations", "organization_allocations_amounts_nonnegative_check", "available >=", "reserved >=")
	requireConstraintDefinitionContains(t, tx, "organization_allocations", "organization_allocations_closed_zero_check", "available =", "reserved =")
	requireConstraintDefinitionContains(t, tx, "organization_allocations", "organization_allocations_active_user_ref_check", "status", "active", "user_ref_id IS NOT NULL")
	requireForeignKeyOnDelete(t, tx, "organization_allocations", "organization_id", "organizations", "RESTRICT")
	requireForeignKeyOnDelete(t, tx, "organization_allocations", "user_ref_id", "users", "SET NULL")
	requireIndex(t, tx, "organization_allocations", "organization_allocations_active_user_snapshot_unique")
}

func TestOrganizationMembershipCheckRejectsNullRoleWithOrganization(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()

	user := mustCreateUser(t, client, &service.User{})
	organization, err := client.Organization.Create().
		SetName("Null Role Guard").
		SetCode("null-role-guard").
		SetCreatedBy(user.ID).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.ExecContext(ctx, "UPDATE users SET organization_id = $1, organization_role = NULL WHERE id = $2", organization.ID, user.ID)
	require.Error(t, err)
}

func requireNumericPrecisionScale(t *testing.T, tx *sql.Tx, table, column string, precision, scale int) {
	t.Helper()
	var gotPrecision, gotScale sql.NullInt64
	err := tx.QueryRowContext(context.Background(), `
		SELECT numeric_precision, numeric_scale
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
	`, table, column).Scan(&gotPrecision, &gotScale)
	require.NoError(t, err)
	require.Equal(t, int64(precision), gotPrecision.Int64)
	require.Equal(t, int64(scale), gotScale.Int64)
}
