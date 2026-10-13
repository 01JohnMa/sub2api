package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration159AllowsOnlyReservationFreeNonBillableProbeOrModelsEvidence(t *testing.T) {
	content, err := FS.ReadFile("159_organization_nonbillable_execution_evidence.sql")
	require.NoError(t, err)
	sql := string(content)
	lower := strings.ToLower(sql)

	require.Contains(t, lower, "create or replace function guard_organization_execution_immutable")
	require.Contains(t, lower, "non_billable and old.status = 'accepted'")
	require.Contains(t, lower, "create or replace function enforce_organization_final_evidence_consumption")
	require.Contains(t, lower, "execution_is_billable = false")
	require.Contains(t, lower, "execution_endpoint_family = 'count_tokens'")
	require.Contains(t, lower, "execution_endpoint_family = 'models'")
	require.Contains(t, lower, "evidence_is_count_tokens_probe = true")
	require.Contains(t, lower, "evidence_raw_actual_cost = 0")
	require.Contains(t, lower, "evidence_zero_proof is null")
	require.Contains(t, lower, "posting_count <> 0")
	require.Contains(t, lower, "organization reservation-free attempt evidence must be non-billable")

	// The migration must not weaken the existing positive-cost reservation
	// branch or introduce a reservation/ledger write of its own.
	require.Contains(t, lower, "reservation_status_value <> 'settled'")
	require.Contains(t, lower, "settled_cost_value > 0")
	require.NotContains(t, lower, "insert into organization_reservations")
	require.NotContains(t, lower, "insert into organization_ledger_postings")
}
