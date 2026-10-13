package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration154BindsAllocationPostingSnapshotsWithoutRewritingDepartedUsers(t *testing.T) {
	content, err := FS.ReadFile("154_organization_allocation_ledger_integrity.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "CREATE OR REPLACE FUNCTION guard_org_ledger_posting_integrity()")
	require.Contains(t, sql, "NEW.bucket_type IN ('allocation_available', 'allocation_reserved')")
	require.Contains(t, sql, "NEW.allocation_id_snapshot IS NULL")
	require.Contains(t, sql, "NEW.allocation_ref_id IS NULL")
	require.Contains(t, sql, "NEW.target_user_id_snapshot IS NULL")
	require.Contains(t, sql, "NEW.target_user_ref_id IS NULL")
	require.Contains(t, sql, "allocation.organization_id")
	require.Contains(t, sql, "allocation.user_id_snapshot")
	require.Contains(t, sql, "NEW.target_user_id_snapshot <> NEW.target_user_ref_id")
	require.NotContains(t, sql, "target_user.organization_id")
	require.NotContains(t, sql, "UPDATE users")
	require.True(t, strings.Contains(sql, "IF TG_OP = 'INSERT'"), "live-reference checks must be insertion-only so ON DELETE SET NULL remains valid")
}

func TestMigration155AddsNoOpIdempotencySnapshotWithoutLedgerPostings(t *testing.T) {
	content, err := FS.ReadFile("155_organization_allocation_no_op_idempotency.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS no_op_result_snapshot JSONB NULL")
	require.Contains(t, sql, "org_ledger_operations_no_op_snapshot_check")
	require.Contains(t, sql, "no_op_result_snapshot->>'outcome' = 'no_op'")
	require.Contains(t, sql, "jsonb_array_length(no_op_result_snapshot->'postings') = 0")
	require.NotContains(t, sql, "organization_ledger_postings")
}
