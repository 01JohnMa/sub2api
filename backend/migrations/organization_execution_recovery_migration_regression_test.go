package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration157DefinesMultiAttemptEvidenceAndAccountRecovery(t *testing.T) {
	content, err := FS.ReadFile("157_organization_execution_recovery.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "CHECK (attempt_sequence > 0)")
	require.Contains(t, sql, "provider_query")
	require.Contains(t, sql, "organization_attempt_evidences_source_provenance_unique")
	require.Contains(t, sql, "ALTER COLUMN status TYPE VARCHAR(32)")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS organization_attempt_account_effects")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS organization_attempt_account_postings")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS organization_execution_incidents")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS organization_execution_reconciliation_audits")
	require.Contains(t, sql, "applied_noop_deleted_target")
	require.Contains(t, sql, "operator_attention")
	require.Contains(t, sql, "platform_overage_cost")
	require.Contains(t, sql, "idempotency_key_hash")
	require.Contains(t, sql, "canonical_payload_hash")
	require.Contains(t, sql, "reservation_status_before")
	require.Contains(t, sql, "reservation_status_after")
	require.Contains(t, sql, "organization_execution_reconciliation_audits_scoped_key_unique")
	require.Contains(t, sql, "organization_attempt_account_postings_attempt_snapshot_unique")
	require.Contains(t, sql, "organization_attempt_account_postings_immutable")
	require.Contains(t, sql, "organization_execution_reconciliation_audits_immutable")
}
