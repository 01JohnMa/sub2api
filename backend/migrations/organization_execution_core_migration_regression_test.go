package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration156DefinesDurableOrganizationExecutionCore(t *testing.T) {
	content, err := FS.ReadFile("156_organization_execution_core.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 0")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS organization_executions")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS organization_reservations")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS organization_execution_attempts")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS organization_attempt_evidences")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS organization_final_execution_evidences")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS organization_execution_outboxes")

	require.Contains(t, sql, "CONSTRAINT organization_executions_execution_id_unique UNIQUE (execution_id)")
	require.NotContains(t, sql, "UNIQUE (organization_id, billing_fingerprint)")
	require.Contains(t, sql, "candidate_snapshot JSONB NOT NULL")
	require.Contains(t, sql, "pricing_snapshot_hash CHAR(64) NOT NULL")
	require.Contains(t, sql, "raw_actual_cost NUMERIC NOT NULL")
	require.NotContains(t, sql, "raw_actual_cost DECIMAL(20,10)")
	require.Contains(t, sql, "organization_execution_attempts_sequence_check CHECK (attempt_sequence = 1)")
	require.Contains(t, sql, "organization_final_execution_evidences_zero_proof_check")
	require.Contains(t, sql, "jsonb_typeof(zero_cost_proof) = 'object'")
	require.Contains(t, sql, "zero_cost_proof = jsonb_build_object('reason', zero_cost_proof ->> 'reason')")
	require.Contains(t, sql, "attempt_evidence_raw_actual_cost <> NEW.raw_actual_cost")

	require.Contains(t, sql, "allocation_ref_id BIGINT NULL REFERENCES organization_allocations(id) ON DELETE RESTRICT")
	require.Contains(t, sql, "final_attempt_evidence_ref_id BIGINT NULL REFERENCES organization_attempt_evidences(id) ON DELETE SET NULL")
	require.Contains(t, sql, "org_ledger_postings_final_evidence_unique")
	require.Contains(t, sql, "org_ledger_postings_consumption_execution_unique")
	require.Contains(t, sql, "CREATE CONSTRAINT TRIGGER organization_final_execution_evidences_consumption_check")
	require.Contains(t, sql, "DEFERRABLE INITIALLY DEFERRED")
	require.Contains(t, sql, "organization positive final evidence is inconsistent with terminal reservation state")
	require.Contains(t, sql, "organization zero-cost final evidence is inconsistent with terminal reservation state")
	require.Contains(t, sql, "organization reservation-free final evidence is not a proven-zero terminal execution")
	require.Contains(t, sql, "evidence_occurred_at TIMESTAMPTZ")
	require.Contains(t, sql, "OR NEW.occurred_at <> evidence_occurred_at")
}

func TestMigration156KeepsFinalizationDurableAndDeletionSafe(t *testing.T) {
	content, err := FS.ReadFile("156_organization_execution_core.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "event_type IN ('usage_projection', 'error_projection', 'api_key_quota', 'user_platform_quota')")
	require.Contains(t, sql, "status IN ('pending', 'applied', 'applied_noop_deleted_target', 'operator_attention')")
	require.Contains(t, sql, "actor_execution_id_snapshot UUID NULL")
	require.Contains(t, sql, "authorization_scope = 'system_execution'")
	require.Contains(t, sql, "NEW.target_user_ref_id IS NULL AND operation_execution_uuid IS NULL")
	require.Contains(t, sql, "organization reservations are immutable outside terminal state")
	require.Contains(t, sql, "settled_amount", "terminalization must persist settled amount")
	require.Contains(t, sql, "released_amount", "terminalization must persist released amount")

	for _, forbidden := range []string{
		"update users set organization_id",
		"update api_keys set organization_id",
		"from payment_orders",
		"from user_subscriptions",
		"update payment_orders",
		"update user_subscriptions",
	} {
		require.False(t, strings.Contains(strings.ToLower(sql), forbidden), "migration must not infer or rewrite unrelated attribution: %s", forbidden)
	}
}
