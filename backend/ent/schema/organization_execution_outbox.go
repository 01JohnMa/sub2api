package schema

import (
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrganizationExecutionOutbox keeps final projection and quota work durable.
// It contains no Account-side effect worker state.
type OrganizationExecutionOutbox struct {
	ent.Schema
}

func (OrganizationExecutionOutbox) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_execution_outboxes",
			Checks: map[string]string{
				"organization_execution_outboxes_event_type_check":   "event_type IN ('usage_projection', 'error_projection', 'api_key_quota', 'user_platform_quota')",
				"organization_execution_outboxes_status_check":       "status IN ('pending', 'applied', 'applied_noop_deleted_target', 'operator_attention')",
				"organization_execution_outboxes_evidence_ref_check": "final_evidence_ref_id IS NULL OR final_evidence_ref_id = final_evidence_id_snapshot",
			},
		},
	}
}

func (OrganizationExecutionOutbox) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (OrganizationExecutionOutbox) Fields() []ent.Field {
	return []ent.Field{
		field.String("execution_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("execution_ref_id").Immutable(),
		field.Int64("final_evidence_id_snapshot").Immutable(),
		field.Int64("final_evidence_ref_id").Optional().Nillable().Immutable(),
		field.Int64("organization_id").Immutable(),
		field.String("event_type").MaxLen(32).Validate(validateOrganizationExecutionOutboxEventType).Immutable(),
		field.JSON("payload", json.RawMessage{}).Immutable(),
		field.String("status").MaxLen(32).Default("pending").Validate(validateOrganizationExecutionOutboxStatus),
		field.Time("occurred_at").Immutable(),
		field.Time("applied_at").Optional().Nillable(),
	}
}

func (OrganizationExecutionOutbox) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("execution_ref", OrganizationExecution.Type).Ref("outboxes").Field("execution_ref_id").Unique().Required().Immutable(),
		edge.From("final_evidence_ref", OrganizationFinalExecutionEvidence.Type).Ref("outboxes").Field("final_evidence_ref_id").Unique().Immutable().Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.From("organization", Organization.Type).Ref("execution_outboxes").Field("organization_id").Unique().Required().Immutable(),
	}
}

func (OrganizationExecutionOutbox) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("execution_ref_id", "event_type").Unique(),
		index.Fields("execution_id_snapshot", "event_type").Unique(),
		index.Fields("status", "created_at"),
	}
}

func validateOrganizationExecutionOutboxEventType(value string) error {
	switch value {
	case "usage_projection", "error_projection", "api_key_quota", "user_platform_quota":
		return nil
	default:
		return fmt.Errorf("invalid organization execution outbox event type %q", value)
	}
}

func validateOrganizationExecutionOutboxStatus(value string) error {
	switch value {
	case "pending", "applied", "applied_noop_deleted_target", "operator_attention":
		return nil
	default:
		return fmt.Errorf("invalid organization execution outbox status %q", value)
	}
}
