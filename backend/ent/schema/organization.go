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

// Organization is the platform tenancy boundary. It intentionally has no
// SoftDeleteMixin: archived is a retained, terminal lifecycle state.
type Organization struct {
	ent.Schema
}

func (Organization) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organizations",
			Checks: map[string]string{
				"organizations_unallocated_nonnegative_check":     "unallocated_pool >= 0",
				"organizations_balance_version_nonnegative_check": "balance_version >= 0",
			},
		},
	}
}

func (Organization) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (Organization) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			MaxLen(100).
			NotEmpty(),
		field.String("code").
			MaxLen(64).
			NotEmpty(),
		field.String("description").
			Default("").
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("status").
			MaxLen(20).
			Default(domain.OrganizationStatusActive).
			Validate(validateOrganizationStatus),
		field.Int64("created_by").
			Immutable(),
		field.Other("unallocated_pool", decimal.Decimal{}).
			SchemaType(map[string]string{
				dialect.Postgres: "decimal(20,8)",
				dialect.SQLite:   "numeric",
			}).
			Default(decimal.Zero),
		field.Int64("balance_version").
			Default(0),
	}
}

func (Organization) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code").Unique(),
		index.Fields("status"),
		index.Fields("created_by"),
	}
}

func (Organization) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("members", User.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("api_keys", APIKey.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("allocations", OrganizationAllocation.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("ledger_operations", OrganizationLedgerOperation.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("ledger_postings", OrganizationLedgerPosting.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("executions", OrganizationExecution.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("reservations", OrganizationReservation.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("execution_attempts", OrganizationExecutionAttempt.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("attempt_evidences", OrganizationAttemptEvidence.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("final_execution_evidences", OrganizationFinalExecutionEvidence.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("execution_outboxes", OrganizationExecutionOutbox.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
	}
}

func validateOrganizationStatus(status string) error {
	switch status {
	case domain.OrganizationStatusActive, domain.OrganizationStatusDisabled, domain.OrganizationStatusArchived:
		return nil
	default:
		return fmt.Errorf("invalid organization status %q", status)
	}
}
