package schema

import (
	"encoding/json"
	"fmt"

	"github.com/MACOS-DO/sub4api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrganizationExecutionAttempt identifies one server-side upstream dispatch.
type OrganizationExecutionAttempt struct {
	ent.Schema
}

func (OrganizationExecutionAttempt) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_execution_attempts",
			Checks: map[string]string{
				"organization_execution_attempts_sequence_check":    "attempt_sequence > 0",
				"organization_execution_attempts_status_check":      "status IN ('created', 'dispatched', 'final_usage_observed')",
				"organization_execution_attempts_account_ref_check": "account_ref_id IS NULL OR account_ref_id = account_id_snapshot",
			},
		},
	}
}

func (OrganizationExecutionAttempt) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (OrganizationExecutionAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.String("attempt_id").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.String("execution_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("execution_ref_id").Immutable(),
		field.Int64("organization_id").Immutable(),
		field.Int("attempt_sequence").Immutable(),
		field.Int64("account_id_snapshot").Optional().Nillable().Immutable(),
		field.Int64("account_ref_id").Optional().Nillable().Immutable(),
		field.JSON("route_snapshot", json.RawMessage{}).Immutable(),
		field.String("route_snapshot_hash").MaxLen(64).SchemaType(map[string]string{dialect.Postgres: "char(64)"}).Immutable(),
		field.JSON("pricing_snapshot", json.RawMessage{}).Immutable(),
		field.String("pricing_snapshot_hash").MaxLen(64).SchemaType(map[string]string{dialect.Postgres: "char(64)"}).Immutable(),
		field.String("provider_correlation_id").MaxLen(255).Immutable(),
		field.String("provider_request_id").MaxLen(255).Immutable(),
		field.String("status").MaxLen(32).Default("created").Validate(validateOrganizationExecutionAttemptStatus),
		field.Time("dispatched_at").Optional().Nillable(),
	}
}

func (OrganizationExecutionAttempt) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("execution_ref", OrganizationExecution.Type).Ref("attempts").Field("execution_ref_id").Unique().Required().Immutable(),
		edge.From("organization", Organization.Type).Ref("execution_attempts").Field("organization_id").Unique().Required().Immutable(),
		edge.To("account_ref", Account.Type).Field("account_ref_id").Unique().Immutable().Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("evidence", OrganizationAttemptEvidence.Type),
		edge.To("final_execution_evidence", OrganizationFinalExecutionEvidence.Type).Unique(),
	}
}

func (OrganizationExecutionAttempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("attempt_id").Unique(),
		index.Fields("execution_ref_id", "attempt_sequence").Unique(),
		index.Fields("execution_id_snapshot", "attempt_sequence").Unique(),
		index.Fields("organization_id", "created_at"),
	}
}

func validateOrganizationExecutionAttemptStatus(value string) error {
	switch value {
	case "created", "dispatched", "final_usage_observed":
		return nil
	default:
		return fmt.Errorf("invalid organization execution attempt status %q", value)
	}
}
