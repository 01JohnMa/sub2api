package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOrganizationTelemetryAttributionMigrationIsAdditiveAndPartitionCompatible(t *testing.T) {
	content, err := FS.ReadFile("160_organization_telemetry_attribution.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "alter table usage_logs")
	require.Contains(t, sql, "alter table ops_error_logs")
	require.Equal(t, 2, strings.Count(sql, "add column if not exists organization_id bigint"))
	require.Equal(t, 2, strings.Count(sql, "add column if not exists execution_id uuid"))
	require.NotContains(t, sql, "organization_id bigint not null")
	require.NotContains(t, sql, "execution_id uuid not null")
	require.NotContains(t, sql, "update usage_logs")
	require.NotContains(t, sql, "update ops_error_logs")

	require.Equal(t, 4, strings.Count(sql, "create index if not exists"))
	require.Contains(t, sql, "on usage_logs (organization_id, created_at desc)")
	require.Contains(t, sql, "on usage_logs (execution_id)")
	require.Contains(t, sql, "on ops_error_logs (organization_id, created_at desc)")
	require.Contains(t, sql, "on ops_error_logs (execution_id)")
	require.Equal(t, 4, strings.Count(sql, "where organization_id is not null")+strings.Count(sql, "where execution_id is not null"))
	require.NotContains(t, sql, "create unique index")
	require.NotContains(t, sql, "concurrently")
}
