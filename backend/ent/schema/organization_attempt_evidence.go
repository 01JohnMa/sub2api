package schema

import (
	"encoding/json"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/shopspring/decimal"
)

// OrganizationAttemptEvidence is immutable evidence captured before any
// asynchronous worker can be lost. The first adapter uses fake_adapter only.
type OrganizationAttemptEvidence struct {
	ent.Schema
}

func (OrganizationAttemptEvidence) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_attempt_evidences",
			Checks: map[string]string{
				"organization_attempt_evidences_source_check":           "evidence_source IN ('fake_adapter', 'adapter_direct', 'provider_query')",
				"organization_attempt_evidences_terminal_outcome_check": "terminal_outcome IN ('success', 'error', 'unknown')",
				"organization_attempt_evidences_counts_cost_check":      "input_tokens >= 0 AND output_tokens >= 0 AND cache_creation_tokens >= 0 AND cache_read_tokens >= 0 AND raw_actual_cost >= 0",
				"organization_attempt_evidences_account_ref_check":      "account_ref_id IS NULL OR account_ref_id = account_id_snapshot",
			},
		},
	}
}

func (OrganizationAttemptEvidence) Fields() []ent.Field {
	return []ent.Field{
		field.String("attempt_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("attempt_ref_id").Immutable(),
		field.String("execution_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("execution_ref_id").Immutable(),
		field.Int64("organization_id").Immutable(),
		field.Int64("account_id_snapshot").Optional().Nillable().Immutable(),
		field.Int64("account_ref_id").Optional().Nillable().Immutable(),
		field.JSON("route_snapshot", json.RawMessage{}).Immutable(),
		field.String("route_snapshot_hash").MaxLen(64).SchemaType(map[string]string{dialect.Postgres: "char(64)"}).Immutable(),
		field.JSON("pricing_snapshot", json.RawMessage{}).Immutable(),
		field.String("pricing_snapshot_hash").MaxLen(64).SchemaType(map[string]string{dialect.Postgres: "char(64)"}).Immutable(),
		field.String("provider_correlation_id").MaxLen(255).Immutable(),
		field.String("provider_request_id").MaxLen(255).Immutable(),
		field.String("evidence_source").MaxLen(32).Default("fake_adapter").Immutable(),
		field.String("terminal_outcome").MaxLen(16).Default("unknown").Immutable(),
		field.String("provenance_hash").MaxLen(64).SchemaType(map[string]string{dialect.Postgres: "char(64)"}).Immutable(),
		field.JSON("evidence", json.RawMessage{}).Immutable(),
		field.String("evidence_hash").MaxLen(64).SchemaType(map[string]string{dialect.Postgres: "char(64)"}).Immutable(),
		field.Int64("input_tokens").Default(0).Immutable(),
		field.Int64("output_tokens").Default(0).Immutable(),
		field.Int64("cache_creation_tokens").Default(0).Immutable(),
		field.Int64("cache_read_tokens").Default(0).Immutable(),
		field.Other("raw_actual_cost", decimal.Decimal{}).SchemaType(map[string]string{dialect.Postgres: "numeric", dialect.SQLite: "numeric"}).Default(decimal.Zero).Immutable(),
		field.Time("provider_observed_at").Immutable(),
		field.Time("ingested_at").Default(time.Now).Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (OrganizationAttemptEvidence) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("attempt_ref", OrganizationExecutionAttempt.Type).Ref("evidence").Field("attempt_ref_id").Unique().Required().Immutable(),
		edge.From("execution_ref", OrganizationExecution.Type).Ref("attempt_evidences").Field("execution_ref_id").Unique().Required().Immutable(),
		edge.From("organization", Organization.Type).Ref("attempt_evidences").Field("organization_id").Unique().Required().Immutable(),
		edge.To("account_ref", Account.Type).Field("account_ref_id").Unique().Immutable().Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("final_execution_evidence", OrganizationFinalExecutionEvidence.Type).Unique(),
	}
}

func (OrganizationAttemptEvidence) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("attempt_ref_id"),
		index.Fields("attempt_id_snapshot", "evidence_source", "provenance_hash").Unique(),
		index.Fields("organization_id", "provider_observed_at"),
	}
}
