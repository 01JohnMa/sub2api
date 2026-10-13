package schema

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/MACOS-DO/sub4api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrganizationLedgerOperation is the immutable, auditable operation envelope
// for every organization monetary mutation. Posting rows carry the numeric
// before/delta/after values; this row carries actor and idempotency snapshots.
type OrganizationLedgerOperation struct {
	ent.Schema
}

func (OrganizationLedgerOperation) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_ledger_operations",
			Checks: map[string]string{
				"org_ledger_operations_currency_check":                 "currency = 'USD'",
				"org_ledger_operations_operation_type_check":           "operation_type IN ('organization_credit', 'equal_allocation', 'custom_allocation', 'allocation_reclaim', 'member_removal_reclaim', 'reservation', 'consumption', 'release')",
				"org_ledger_operations_authorization_scope_check":      "authorization_scope IN ('platform_admin_jwt', 'organization_admin_jwt', 'system_execution')",
				"org_ledger_operations_actor_ref_check":                "(actor_ref_id IS NULL OR actor_ref_id = actor_id_snapshot)",
				"org_ledger_operations_actor_execution_ref_check":      "(actor_execution_ref_id IS NULL OR actor_execution_id_snapshot IS NOT NULL)",
				"org_ledger_operations_actor_execution_identity_check": "((authorization_scope = 'system_execution' AND actor_id_snapshot = 0 AND actor_ref_id IS NULL AND actor_execution_id_snapshot IS NOT NULL) OR (authorization_scope IN ('platform_admin_jwt', 'organization_admin_jwt') AND actor_id_snapshot > 0 AND actor_execution_id_snapshot IS NULL AND actor_execution_ref_id IS NULL))",
				"org_ledger_operations_reason_check":                   "trim(internal_reason) <> ''",
			},
		},
	}
}

func (OrganizationLedgerOperation) Fields() []ent.Field {
	return []ent.Field{
		field.String("operation_id").
			MaxLen(36).
			SchemaType(map[string]string{
				dialect.Postgres: "uuid",
				dialect.SQLite:   "text",
			}).
			Immutable(),
		field.String("correlation_id").
			MaxLen(36).
			SchemaType(map[string]string{
				dialect.Postgres: "uuid",
				dialect.SQLite:   "text",
			}).
			Immutable(),
		field.Int64("organization_id").
			Immutable(),
		field.String("operation_type").
			MaxLen(64).
			Validate(validateOrganizationLedgerOperationType).
			Immutable(),
		field.String("authorization_scope").
			MaxLen(64).
			Immutable(),
		field.Int64("actor_id_snapshot").
			Immutable(),
		field.Int64("actor_ref_id").
			Optional().
			Nillable().
			Immutable(),
		field.String("actor_execution_id_snapshot").
			MaxLen(36).
			Optional().
			Nillable().
			SchemaType(map[string]string{
				dialect.Postgres: "uuid",
				dialect.SQLite:   "text",
			}).
			Immutable(),
		field.Int64("actor_execution_ref_id").
			Optional().
			Nillable().
			Immutable(),
		field.String("actor_display_snapshot").
			MaxLen(255).
			Immutable(),
		field.String("actor_role_snapshot").
			MaxLen(64).
			Immutable(),
		field.String("currency").
			MaxLen(3).
			Default(domain.OrganizationCurrencyUSD).
			Immutable(),
		field.String("idempotency_key_hash").
			MaxLen(64).
			SchemaType(map[string]string{dialect.Postgres: "char(64)"}).
			Immutable(),
		field.String("canonical_payload_hash").
			MaxLen(64).
			SchemaType(map[string]string{dialect.Postgres: "char(64)"}).
			Immutable(),
		field.String("public_reason").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Immutable(),
		field.String("internal_reason").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Immutable(),
		field.JSON("no_op_result_snapshot", json.RawMessage{}).
			Optional().
			Immutable(),
		field.Time("occurred_at").
			Immutable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

func (OrganizationLedgerOperation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("organization", Organization.Type).
			Ref("ledger_operations").
			Field("organization_id").
			Unique().
			Required().
			Immutable(),
		edge.To("actor_ref", User.Type).
			Field("actor_ref_id").
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("actor_execution_ref", OrganizationExecution.Type).
			Field("actor_execution_ref_id").
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("postings", OrganizationLedgerPosting.Type),
	}
}

func (OrganizationLedgerOperation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("operation_id").Unique(),
		index.Fields("authorization_scope", "organization_id", "operation_type", "idempotency_key_hash").Unique(),
		index.Fields("organization_id", "occurred_at"),
	}
}

func validateOrganizationLedgerOperationType(operationType string) error {
	switch operationType {
	case domain.OrganizationFundingOperationCredit,
		domain.OrganizationFundingOperationEqualAllocation,
		domain.OrganizationFundingOperationCustomAllocation,
		domain.OrganizationFundingOperationReclaim,
		domain.OrganizationFundingOperationMemberRemovalReclaim,
		domain.OrganizationFundingOperationReservation,
		domain.OrganizationFundingOperationConsumption,
		domain.OrganizationFundingOperationRelease:
		return nil
	default:
		return fmt.Errorf("invalid organization ledger operation type %q", operationType)
	}
}
