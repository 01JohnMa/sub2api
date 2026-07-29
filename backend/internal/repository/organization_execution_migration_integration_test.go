//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOrganizationExecutionMigrationDefinesDurableEvidenceSchema(t *testing.T) {
	tx := testTx(t)

	requireColumn(t, tx, "organization_allocations", "version", "bigint", 0, false)
	requireConstraintDefinitionContains(t, tx, "organization_allocations", "organization_allocations_version_nonnegative_check", "version >=")

	for _, table := range []string{
		"organization_executions",
		"organization_reservations",
		"organization_execution_attempts",
		"organization_attempt_evidences",
		"organization_final_execution_evidences",
		"organization_execution_outboxes",
		"organization_attempt_account_postings",
		"organization_attempt_account_effects",
		"organization_execution_incidents",
		"organization_execution_reconciliation_audits",
	} {
		var relation sql.NullString
		require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.' || $1)", table).Scan(&relation))
		require.True(t, relation.Valid, "expected table %s", table)
	}

	requireColumn(t, tx, "organization_executions", "execution_id", "uuid", 0, false)
	requireColumn(t, tx, "organization_executions", "correlation_id", "uuid", 0, false)
	requireColumn(t, tx, "organization_executions", "candidate_ceiling", "numeric", 0, false)
	requireNumericPrecisionScale(t, tx, "organization_executions", "candidate_ceiling", 20, 8)
	requireForeignKeyOnDelete(t, tx, "organization_executions", "organization_id", "organizations", "RESTRICT")
	requireForeignKeyOnDelete(t, tx, "organization_executions", "allocation_ref_id", "organization_allocations", "RESTRICT")
	requireForeignKeyOnDelete(t, tx, "organization_executions", "user_ref_id", "users", "SET NULL")
	requireForeignKeyOnDelete(t, tx, "organization_executions", "api_key_ref_id", "api_keys", "SET NULL")
	requireIndex(t, tx, "organization_executions", "organization_executions_execution_id_unique")

	requireColumn(t, tx, "organization_reservations", "reserved_amount", "numeric", 0, false)
	requireColumn(t, tx, "organization_reservations", "settled_amount", "numeric", 0, false)
	requireColumn(t, tx, "organization_reservations", "released_amount", "numeric", 0, false)
	requireNumericPrecisionScale(t, tx, "organization_reservations", "reserved_amount", 20, 8)
	requireConstraintDefinitionContains(t, tx, "organization_reservations", "organization_reservations_terminal_amount_check", "settled_amount", "released_amount", "reserved_amount")
	requireForeignKeyOnDelete(t, tx, "organization_reservations", "allocation_ref_id", "organization_allocations", "RESTRICT")

	requireColumn(t, tx, "organization_attempt_evidences", "raw_actual_cost", "numeric", 0, false)
	requireConstraintDefinitionContains(t, tx, "organization_execution_attempts", "organization_execution_attempts_sequence_check", "attempt_sequence > 0")
	var outboxStatusLength int
	require.NoError(t, tx.QueryRowContext(context.Background(), `
		SELECT character_maximum_length
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'organization_execution_outboxes' AND column_name = 'status'
	`).Scan(&outboxStatusLength))
	require.Equal(t, 32, outboxStatusLength)
	requireColumn(t, tx, "organization_execution_reconciliation_audits", "idempotency_key_hash", "character", 0, false)
	requireColumn(t, tx, "organization_execution_reconciliation_audits", "canonical_payload_hash", "character", 0, false)
	requireColumn(t, tx, "organization_execution_reconciliation_audits", "raw_actual_cost", "numeric", 0, false)
	requireIndex(t, tx, "organization_execution_reconciliation_audits", "organization_execution_reconciliation_audits_scoped_key_unique")
	requireColumn(t, tx, "organization_final_execution_evidences", "organization_settled_cost", "numeric", 0, false)
	requireNumericPrecisionScale(t, tx, "organization_final_execution_evidences", "organization_settled_cost", 20, 8)
	requireConstraintDefinitionContains(t, tx, "organization_final_execution_evidences", "organization_final_execution_evidences_zero_proof_check", "jsonb_typeof", "reason")
	requireConstraintDefinitionContains(t, tx, "organization_final_execution_evidences", "organization_final_execution_evidences_cost_proof_check", "zero_cost_proof", "final_attempt_evidence_ref_id")
	requireForeignKeyOnDelete(t, tx, "organization_final_execution_evidences", "final_attempt_evidence_ref_id", "organization_attempt_evidences", "SET NULL")
	requireIndex(t, tx, "organization_final_execution_evidences", "organization_final_execution_evidences_execution_ref_unique")
	requireIndex(t, tx, "organization_ledger_postings", "org_ledger_postings_final_evidence_unique")
	requireIndex(t, tx, "organization_ledger_postings", "org_ledger_postings_consumption_execution_unique")
}

func TestOrganizationExecutionMigrationInstallsImmutableAndDeferredGuards(t *testing.T) {
	tx := testTx(t)

	var triggerCount int
	err := tx.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM pg_trigger
		WHERE NOT tgisinternal
		  AND tgname IN (
			'organization_executions_immutable',
			'organization_reservations_immutable',
			'organization_execution_attempts_immutable',
			'organization_attempt_evidences_immutable',
			'organization_final_execution_evidences_immutable',
			'organization_execution_outboxes_immutable',
			'organization_attempt_account_postings_immutable',
			'organization_attempt_account_effects_apply_guard',
			'organization_execution_incidents_immutable',
			'organization_execution_reconciliation_audits_immutable',
			'organization_final_execution_evidences_consumption_check',
			'organization_ledger_postings_consumption_check'
		  )
	`).Scan(&triggerCount)
	require.NoError(t, err)
	require.Equal(t, 12, triggerCount)
}
