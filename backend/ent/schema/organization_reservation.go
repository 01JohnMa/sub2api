package schema

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/MACOS-DO/sub4api/ent/schema/mixins"
	"github.com/shopspring/decimal"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrganizationReservation reserves a deterministic execution ceiling before
// dispatch. Its version snapshots are compared by the repository transaction.
type OrganizationReservation struct {
	ent.Schema
}

func (OrganizationReservation) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_reservations",
			Checks: map[string]string{
				"organization_reservations_status_check":          "status IN ('reserved', 'pending_reconciliation', 'settled', 'released')",
				"organization_reservations_amount_check":          "reserved_amount >= 0 AND settled_amount >= 0 AND released_amount >= 0 AND settled_amount + released_amount <= reserved_amount",
				"organization_reservations_version_check":         "allocation_version_snapshot >= 0 AND organization_balance_version_snapshot >= 0",
				"organization_reservations_allocation_ref_check":  "allocation_ref_id = allocation_id_snapshot",
				"organization_reservations_terminal_amount_check": "((status IN ('reserved', 'pending_reconciliation') AND settled_amount = 0 AND released_amount = 0) OR (status IN ('settled', 'released') AND settled_amount + released_amount = reserved_amount))",
			},
		},
	}
}

func (OrganizationReservation) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (OrganizationReservation) Fields() []ent.Field {
	return []ent.Field{
		field.String("execution_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("execution_ref_id").Immutable(),
		field.Int64("organization_id").Immutable(),
		field.Int64("allocation_id_snapshot").Immutable(),
		field.Int64("allocation_ref_id").Immutable(),
		field.Int64("allocation_version_snapshot").Immutable(),
		field.Int64("organization_balance_version_snapshot").Immutable(),
		field.String("billing_fingerprint").MaxLen(64).SchemaType(map[string]string{dialect.Postgres: "char(64)"}).Immutable(),
		field.JSON("candidate_snapshot", json.RawMessage{}).Immutable(),
		field.String("candidate_set_hash").MaxLen(64).SchemaType(map[string]string{dialect.Postgres: "char(64)"}).Immutable(),
		field.JSON("pricing_snapshot", json.RawMessage{}).Immutable(),
		field.String("pricing_snapshot_hash").MaxLen(64).SchemaType(map[string]string{dialect.Postgres: "char(64)"}).Immutable(),
		field.Other("reserved_amount", decimal.Decimal{}).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)", dialect.SQLite: "numeric"}).Immutable(),
		field.Other("settled_amount", decimal.Decimal{}).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)", dialect.SQLite: "numeric"}).Default(decimal.Zero),
		field.Other("released_amount", decimal.Decimal{}).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)", dialect.SQLite: "numeric"}).Default(decimal.Zero),
		field.String("status").MaxLen(32).Default("reserved").Validate(validateOrganizationReservationStatus),
		field.Time("reserved_at").Default(time.Now).Immutable(),
		field.Time("terminalized_at").Optional().Nillable(),
	}
}

func (OrganizationReservation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("execution_ref", OrganizationExecution.Type).Ref("reservation").Field("execution_ref_id").Unique().Required().Immutable(),
		edge.From("organization", Organization.Type).Ref("reservations").Field("organization_id").Unique().Required().Immutable(),
		edge.From("allocation_ref", OrganizationAllocation.Type).Ref("reservations").Field("allocation_ref_id").Unique().Required().Immutable().Annotations(entsql.OnDelete(entsql.Restrict)),
	}
}

func (OrganizationReservation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("execution_ref_id").Unique(),
		index.Fields("execution_id_snapshot").Unique(),
		index.Fields("allocation_id_snapshot", "status"),
		index.Fields("organization_id", "status", "reserved_at"),
	}
}

func validateOrganizationReservationStatus(value string) error {
	switch value {
	case "reserved", "pending_reconciliation", "settled", "released":
		return nil
	default:
		return fmt.Errorf("invalid organization reservation status %q", value)
	}
}
