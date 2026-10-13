package schema

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/shopspring/decimal"
)

// OrganizationAllocation is the distinct financial identity created for each
// organization membership. It deliberately retains a user ID snapshot after a
// user reference is cleared.
type OrganizationAllocation struct {
	ent.Schema
}

func (OrganizationAllocation) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_allocations",
			Checks: map[string]string{
				"organization_allocations_status_check":              "status IN ('active', 'closed')",
				"organization_allocations_amounts_nonnegative_check": "available >= 0 AND reserved >= 0",
				"organization_allocations_version_nonnegative_check": "version >= 0",
				"organization_allocations_closed_zero_check":         "(status = 'active' OR (available = 0 AND reserved = 0))",
				"organization_allocations_user_snapshot_check":       "(user_ref_id IS NULL OR user_ref_id = user_id_snapshot)",
				"organization_allocations_active_user_ref_check":     "(status <> 'active' OR user_ref_id IS NOT NULL)",
			},
		},
	}
}

func (OrganizationAllocation) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (OrganizationAllocation) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("organization_id").
			Immutable(),
		field.Int64("user_id_snapshot").
			Immutable(),
		field.Int64("user_ref_id").
			Optional().
			Nillable().
			Immutable(),
		field.String("status").
			MaxLen(20).
			Default(domain.OrganizationAllocationStatusActive).
			Validate(validateOrganizationAllocationStatus),
		field.Other("available", decimal.Decimal{}).
			SchemaType(map[string]string{
				dialect.Postgres: "decimal(20,8)",
				dialect.SQLite:   "numeric",
			}).
			Default(decimal.Zero),
		field.Other("reserved", decimal.Decimal{}).
			SchemaType(map[string]string{
				dialect.Postgres: "decimal(20,8)",
				dialect.SQLite:   "numeric",
			}).
			Default(decimal.Zero),
		field.Int64("version").
			Default(0),
	}
}

func (OrganizationAllocation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("organization", Organization.Type).
			Ref("allocations").
			Field("organization_id").
			Unique().
			Required().
			Immutable(),
		edge.From("user_ref", User.Type).
			Ref("organization_allocations").
			Field("user_ref_id").
			Unique().
			Immutable(),
		edge.To("executions", OrganizationExecution.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("reservations", OrganizationReservation.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("final_execution_evidences", OrganizationFinalExecutionEvidence.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
	}
}

func (OrganizationAllocation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("organization_id", "status"),
		index.Fields("user_ref_id"),
		index.Fields("user_id_snapshot").
			Unique().
			Annotations(entsql.IndexWhere("status = 'active'")),
	}
}

func validateOrganizationAllocationStatus(status string) error {
	switch status {
	case domain.OrganizationAllocationStatusActive, domain.OrganizationAllocationStatusClosed:
		return nil
	default:
		return fmt.Errorf("invalid organization allocation status %q", status)
	}
}
