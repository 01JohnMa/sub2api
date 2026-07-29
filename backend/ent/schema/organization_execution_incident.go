package schema

import (
	"encoding/json"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrganizationExecutionIncident is an immutable operator-attention fact.
type OrganizationExecutionIncident struct {
	ent.Schema
}

func (OrganizationExecutionIncident) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_execution_incidents",
			Checks: map[string]string{
				"organization_execution_incidents_type_check":   "incident_type IN ('provenance_conflict', 'platform_overage')",
				"organization_execution_incidents_status_check": "status = 'open'",
			},
		},
	}
}

func (OrganizationExecutionIncident) Fields() []ent.Field {
	return []ent.Field{
		field.String("execution_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("execution_ref_id").Immutable(),
		field.Int64("organization_id").Immutable(),
		field.String("attempt_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Optional().Nillable().Immutable(),
		field.Int64("attempt_ref_id").Optional().Nillable().Immutable(),
		field.String("incident_type").MaxLen(64).Immutable(),
		field.String("public_reason").MaxLen(255).Immutable(),
		field.String("internal_reason").MaxLen(2000).Immutable(),
		field.JSON("details", json.RawMessage{}).Default(json.RawMessage(`{}`)).Immutable(),
		field.String("status").MaxLen(16).Default("open").Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (OrganizationExecutionIncident) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("execution_ref_id", "incident_type", "attempt_id_snapshot").Unique(),
		index.Fields("organization_id", "created_at"),
	}
}
