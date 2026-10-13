package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration158PersistsZeroAllocationMemberRemovalIdempotency(t *testing.T) {
	content, err := FS.ReadFile("158_organization_member_removal_idempotency.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS org_ledger_operations_no_op_snapshot_check")
	require.Contains(t, sql, "operation_type = 'equal_allocation'")
	require.Contains(t, sql, "operation_type = 'member_removal_reclaim'")
	require.Contains(t, sql, "no_op_result_snapshot->>'outcome' = 'committed'")
	require.Contains(t, sql, "no_op_result_snapshot->>'reclaimed' = '0.00000000'")
	require.Contains(t, sql, "no_op_result_snapshot->>'pool_before' = no_op_result_snapshot->>'pool_after'")
	require.NotContains(t, sql, "INSERT INTO organization_ledger_postings")
}
