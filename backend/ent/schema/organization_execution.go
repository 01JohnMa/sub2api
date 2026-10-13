package schema

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/shopspring/decimal"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrganizationExecution is the server-generated parent identity for a trusted
// organization request. It is independent of client request identifiers.
type OrganizationExecution struct {
	ent.Schema
}

func (OrganizationExecution) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_executions",
			Checks: map[string]string{
				"organization_executions_status_check":               "status IN ('accepted', 'reserved', 'dispatched', 'final_usage_observed', 'pending_reconciliation', 'settled', 'released')",
				"organization_executions_amount_check":               "candidate_ceiling >= 0",
				"organization_executions_allocation_ref_check":       "allocation_ref_id IS NULL OR allocation_ref_id = allocation_id_snapshot",
				"organization_executions_user_ref_check":             "user_ref_id IS NULL OR user_ref_id = user_id_snapshot",
				"organization_executions_api_key_ref_check":          "api_key_ref_id IS NULL OR api_key_ref_id = api_key_id_snapshot",
				"organization_executions_non_billable_ceiling_check": "is_billable OR candidate_ceiling = 0",
			},
		},
	}
}

func (OrganizationExecution) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (OrganizationExecution) Fields() []ent.Field {
	return []ent.Field{
		field.String("execution_id").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.String("correlation_id").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("organization_id").Immutable(),
		field.Int64("allocation_id_snapshot").Optional().Nillable().Immutable(),
		field.Int64("allocation_ref_id").Optional().Nillable().Immutable(),
		field.Int64("user_id_snapshot").Immutable(),
		field.Int64("user_ref_id").Optional().Nillable().Immutable(),
		field.Int64("api_key_id_snapshot").Immutable(),
		field.Int64("api_key_ref_id").Optional().Nillable().Immutable(),
		field.String("endpoint_family").MaxLen(128).Immutable(),
		field.String("request_shape").MaxLen(128).Immutable(),
		field.String("model").MaxLen(255).Optional().Nillable().Immutable(),
		field.String("client_request_id").MaxLen(512).Optional().Nillable().Immutable(),
		field.String("billing_fingerprint").MaxLen(64).SchemaType(map[string]string{dialect.Postgres: "char(64)"}).Immutable(),
		field.JSON("candidate_snapshot", json.RawMessage{}).Immutable(),
		field.String("candidate_set_hash").MaxLen(64).SchemaType(map[string]string{dialect.Postgres: "char(64)"}).Immutable(),
		field.JSON("pricing_snapshot", json.RawMessage{}).Immutable(),
		field.String("pricing_snapshot_hash").MaxLen(64).SchemaType(map[string]string{dialect.Postgres: "char(64)"}).Immutable(),
		field.Other("candidate_ceiling", decimal.Decimal{}).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)", dialect.SQLite: "numeric"}).Immutable(),
		field.Bool("is_billable").Default(true).Immutable(),
		field.String("status").MaxLen(32).Default("accepted").Validate(validateOrganizationExecutionStatus),
		field.Time("accepted_at").Default(time.Now).Immutable(),
		field.Time("terminalized_at").Optional().Nillable(),
	}
}

func (OrganizationExecution) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("organization", Organization.Type).Ref("executions").Field("organization_id").Unique().Required().Immutable(),
		edge.From("allocation_ref", OrganizationAllocation.Type).Ref("executions").Field("allocation_ref_id").Unique().Immutable().Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.From("user_ref", User.Type).Ref("organization_executions").Field("user_ref_id").Unique().Immutable().Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.From("api_key_ref", APIKey.Type).Ref("organization_executions").Field("api_key_ref_id").Unique().Immutable().Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("reservation", OrganizationReservation.Type).Unique(),
		edge.To("attempts", OrganizationExecutionAttempt.Type),
		edge.To("attempt_evidences", OrganizationAttemptEvidence.Type),
		edge.To("final_evidence", OrganizationFinalExecutionEvidence.Type).Unique(),
		edge.To("outboxes", OrganizationExecutionOutbox.Type),
	}
}

func (OrganizationExecution) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("execution_id").Unique(),
		index.Fields("correlation_id").Unique(),
		index.Fields("organization_id", "status", "accepted_at"),
		index.Fields("allocation_id_snapshot", "accepted_at"),
	}
}

func validateOrganizationExecutionStatus(value string) error {
	switch value {
	case "accepted", "reserved", "dispatched", "final_usage_observed", "pending_reconciliation", "settled", "released":
		return nil
	default:
		return fmt.Errorf("invalid organization execution status %q", value)
	}
}
