package schema

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrganizationFinalExecutionEvidence is the one immutable organization
// settlement and reporting fact for a terminal execution.
type OrganizationFinalExecutionEvidence struct {
	ent.Schema
}

func (OrganizationFinalExecutionEvidence) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_final_execution_evidences",
			Checks: map[string]string{
				"organization_final_execution_evidences_outcome_check":              "terminal_outcome IN ('success', 'error')",
				"organization_final_execution_evidences_counts_cost_check":          "input_tokens >= 0 AND output_tokens >= 0 AND cache_creation_tokens >= 0 AND cache_read_tokens >= 0 AND raw_actual_cost >= 0 AND organization_settled_cost >= 0",
				"organization_final_execution_evidences_allocation_ref_check":       "allocation_ref_id IS NULL OR allocation_ref_id = allocation_id_snapshot",
				"organization_final_execution_evidences_user_ref_check":             "user_ref_id IS NULL OR user_ref_id = user_id_snapshot",
				"organization_final_execution_evidences_api_key_ref_check":          "api_key_ref_id IS NULL OR api_key_ref_id = api_key_id_snapshot",
				"organization_final_execution_evidences_attempt_ref_check":          "((final_attempt_id_snapshot IS NULL AND final_attempt_ref_id IS NULL) OR (final_attempt_id_snapshot IS NOT NULL AND final_attempt_ref_id IS NOT NULL))",
				"organization_final_execution_evidences_attempt_evidence_ref_check": "((final_attempt_evidence_id_snapshot IS NULL AND final_attempt_evidence_ref_id IS NULL) OR (final_attempt_evidence_id_snapshot IS NOT NULL AND final_attempt_evidence_ref_id IS NOT NULL))",
				"organization_final_execution_evidences_account_ref_check":          "account_ref_id IS NULL OR account_ref_id = account_id_snapshot",
				"organization_final_execution_evidences_cost_proof_check":           "((organization_settled_cost > 0 AND allocation_id_snapshot IS NOT NULL AND allocation_ref_id IS NOT NULL AND final_attempt_id_snapshot IS NOT NULL AND final_attempt_ref_id IS NOT NULL AND final_attempt_evidence_id_snapshot IS NOT NULL AND final_attempt_evidence_ref_id IS NOT NULL AND zero_cost_proof IS NULL) OR (organization_settled_cost = 0 AND ((final_attempt_id_snapshot IS NOT NULL AND final_attempt_ref_id IS NOT NULL AND final_attempt_evidence_id_snapshot IS NOT NULL AND final_attempt_evidence_ref_id IS NOT NULL AND zero_cost_proof IS NULL) OR (final_attempt_id_snapshot IS NULL AND final_attempt_ref_id IS NULL AND final_attempt_evidence_id_snapshot IS NULL AND final_attempt_evidence_ref_id IS NULL AND account_id_snapshot IS NULL AND account_ref_id IS NULL AND input_tokens = 0 AND output_tokens = 0 AND cache_creation_tokens = 0 AND cache_read_tokens = 0 AND raw_actual_cost = 0 AND zero_cost_proof IS NOT NULL))))",
			},
		},
	}
}

func (OrganizationFinalExecutionEvidence) Fields() []ent.Field {
	return []ent.Field{
		field.String("execution_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("execution_ref_id").Immutable(),
		field.Int64("organization_id").Immutable(),
		field.Int64("allocation_id_snapshot").Optional().Nillable().Immutable(),
		field.Int64("allocation_ref_id").Optional().Nillable().Immutable(),
		field.Int64("user_id_snapshot").Immutable(),
		field.Int64("user_ref_id").Optional().Nillable().Immutable(),
		field.Int64("api_key_id_snapshot").Immutable(),
		field.Int64("api_key_ref_id").Optional().Nillable().Immutable(),
		field.String("final_attempt_id_snapshot").MaxLen(36).Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("final_attempt_ref_id").Optional().Nillable().Immutable(),
		field.Int64("final_attempt_evidence_id_snapshot").Optional().Nillable().Immutable(),
		field.Int64("final_attempt_evidence_ref_id").Optional().Nillable().Immutable(),
		field.Int64("account_id_snapshot").Optional().Nillable().Immutable(),
		field.Int64("account_ref_id").Optional().Nillable().Immutable(),
		field.String("endpoint_family").MaxLen(128).Immutable(),
		field.String("request_shape").MaxLen(128).Immutable(),
		field.String("model").MaxLen(255).Optional().Nillable().Immutable(),
		field.Int64("input_tokens").Default(0).Immutable(),
		field.Int64("output_tokens").Default(0).Immutable(),
		field.Int64("cache_creation_tokens").Default(0).Immutable(),
		field.Int64("cache_read_tokens").Default(0).Immutable(),
		field.Other("raw_actual_cost", decimal.Decimal{}).SchemaType(map[string]string{dialect.Postgres: "numeric", dialect.SQLite: "numeric"}).Default(decimal.Zero).Immutable(),
		field.Other("organization_settled_cost", decimal.Decimal{}).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)", dialect.SQLite: "numeric"}).Default(decimal.Zero).Immutable(),
		field.String("terminal_outcome").MaxLen(16).Validate(validateOrganizationTerminalOutcome).Immutable(),
		field.Bool("is_business_limited").Default(false).Immutable(),
		field.Bool("is_count_tokens_probe").Default(false).Immutable(),
		field.JSON("zero_cost_proof", json.RawMessage{}).Optional().Immutable(),
		field.Time("occurred_at").Immutable(),
		field.Time("reconciled_at").Optional().Nillable().Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (OrganizationFinalExecutionEvidence) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("execution_ref", OrganizationExecution.Type).Ref("final_evidence").Field("execution_ref_id").Unique().Required().Immutable(),
		edge.From("organization", Organization.Type).Ref("final_execution_evidences").Field("organization_id").Unique().Required().Immutable(),
		edge.From("allocation_ref", OrganizationAllocation.Type).Ref("final_execution_evidences").Field("allocation_ref_id").Unique().Immutable().Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.From("user_ref", User.Type).Ref("organization_final_execution_evidences").Field("user_ref_id").Unique().Immutable().Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.From("api_key_ref", APIKey.Type).Ref("organization_final_execution_evidences").Field("api_key_ref_id").Unique().Immutable().Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.From("final_attempt_ref", OrganizationExecutionAttempt.Type).Ref("final_execution_evidence").Field("final_attempt_ref_id").Unique().Immutable().Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.From("final_attempt_evidence_ref", OrganizationAttemptEvidence.Type).Ref("final_execution_evidence").Field("final_attempt_evidence_ref_id").Unique().Immutable().Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("account_ref", Account.Type).Field("account_ref_id").Unique().Immutable().Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("outboxes", OrganizationExecutionOutbox.Type),
	}
}

func (OrganizationFinalExecutionEvidence) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("execution_ref_id").Unique(),
		index.Fields("execution_id_snapshot").Unique(),
		index.Fields("organization_id", "occurred_at"),
		index.Fields("allocation_id_snapshot", "occurred_at"),
	}
}

func validateOrganizationTerminalOutcome(value string) error {
	switch value {
	case "success", "error":
		return nil
	default:
		return fmt.Errorf("invalid organization terminal outcome %q", value)
	}
}
