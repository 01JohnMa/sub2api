package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrganizationExecutionReconciliationAudit is the immutable platform-admin
// audit fact created in the same transaction as pending execution resolution.
type OrganizationExecutionReconciliationAudit struct {
	ent.Schema
}

func (OrganizationExecutionReconciliationAudit) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_execution_reconciliation_audits",
			Checks: map[string]string{
				"organization_execution_reconciliation_audits_action_check":            "action IN ('settled', 'released')",
				"organization_execution_reconciliation_audits_scope_check":             "authorization_scope = 'platform_admin_jwt'",
				"organization_execution_reconciliation_audits_hash_check":              "length(idempotency_key_hash) = 64 AND length(canonical_payload_hash) = 64",
				"organization_execution_reconciliation_audits_amount_check":            "raw_actual_cost >= 0 AND organization_settled_cost >= 0 AND reservation_amount >= 0",
				"organization_execution_reconciliation_audits_reservation_state_check": "reservation_status_before = 'pending_reconciliation' AND reservation_status_after IN ('settled', 'released')",
				"organization_execution_reconciliation_audits_final_ref_check":         "final_evidence_ref_id IS NULL OR final_evidence_ref_id = final_evidence_id_snapshot",
				"organization_execution_reconciliation_audits_evidence_ref_check":      "selected_evidence_ref_id IS NULL OR selected_evidence_ref_id = selected_evidence_id_snapshot",
				"organization_execution_reconciliation_audits_actor_ref_check":         "actor_ref_id IS NULL OR actor_ref_id = actor_id_snapshot",
			},
		},
	}
}

func (OrganizationExecutionReconciliationAudit) Fields() []ent.Field {
	return []ent.Field{
		field.String("operation_id").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.String("correlation_id").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.String("idempotency_key_hash").MaxLen(64).Immutable(),
		field.String("canonical_payload_hash").MaxLen(64).Immutable(),
		field.String("execution_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("execution_ref_id").Immutable(),
		field.Int64("organization_id").Immutable(),
		field.Int64("final_evidence_id_snapshot").Immutable(),
		field.Int64("final_evidence_ref_id").Optional().Nillable().Immutable(),
		field.String("selected_attempt_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("selected_attempt_ref_id").Optional().Nillable().Immutable(),
		field.Int64("selected_evidence_id_snapshot").Immutable(),
		field.Int64("selected_evidence_ref_id").Optional().Nillable().Immutable(),
		field.Int64("actor_id_snapshot").Immutable(),
		field.Int64("actor_ref_id").Optional().Nillable().Immutable(),
		field.String("actor_display_snapshot").MaxLen(255).Immutable(),
		field.String("actor_role_snapshot").MaxLen(64).Immutable(),
		field.String("authorization_scope").MaxLen(64).Immutable(),
		field.String("action").MaxLen(16).Immutable(),
		field.String("raw_actual_cost").SchemaType(map[string]string{dialect.Postgres: "numeric", dialect.SQLite: "text"}).Immutable(),
		field.String("organization_settled_cost").SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)", dialect.SQLite: "text"}).Immutable(),
		field.String("reservation_amount").SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)", dialect.SQLite: "text"}).Immutable(),
		field.String("reservation_status_before").MaxLen(32).Immutable(),
		field.String("reservation_status_after").MaxLen(32).Immutable(),
		field.String("public_reason").MaxLen(255).Immutable(),
		field.String("internal_reason").MaxLen(2000).Immutable(),
		field.Time("reconciled_at").Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (OrganizationExecutionReconciliationAudit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("execution_ref_id").Unique(),
		index.Fields("operation_id").Unique(),
		index.Fields("authorization_scope", "organization_id", "idempotency_key_hash").Unique(),
		index.Fields("organization_id", "reconciled_at"),
	}
}
