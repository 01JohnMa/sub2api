package schema

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/shopspring/decimal"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrganizationAttemptAccountEffect is the durable apply-state for one
// attempt's Account cost posting and optional live quota update.
type OrganizationAttemptAccountEffect struct {
	ent.Schema
}

func (OrganizationAttemptAccountEffect) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_attempt_account_effects",
			Checks: map[string]string{
				"organization_attempt_account_effects_status_check":       "status IN ('pending', 'applied', 'applied_noop_deleted_target', 'operator_attention')",
				"organization_attempt_account_effects_account_ref_check":  "account_ref_id IS NULL OR account_ref_id = account_id_snapshot",
				"organization_attempt_account_effects_evidence_ref_check": "selected_evidence_ref_id IS NULL OR selected_evidence_ref_id = selected_evidence_id_snapshot",
				"organization_attempt_account_effects_posting_ref_check":  "posting_ref_id IS NULL OR posting_ref_id = posting_id_snapshot",
			},
		},
	}
}

func (OrganizationAttemptAccountEffect) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (OrganizationAttemptAccountEffect) Fields() []ent.Field {
	return []ent.Field{
		field.String("attempt_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("attempt_ref_id").Immutable(),
		field.String("execution_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("execution_ref_id").Immutable(),
		field.Int64("organization_id").Immutable(),
		field.Int64("account_id_snapshot").Immutable(),
		field.Int64("account_ref_id").Optional().Nillable(),
		field.Int64("selected_evidence_id_snapshot").Optional().Nillable(),
		field.Int64("selected_evidence_ref_id").Optional().Nillable(),
		field.Int64("posting_id_snapshot").Optional().Nillable(),
		field.Int64("posting_ref_id").Optional().Nillable(),
		field.Other("raw_actual_cost", decimal.Decimal{}).SchemaType(map[string]string{dialect.Postgres: "numeric", dialect.SQLite: "numeric"}).Optional().Nillable(),
		field.Other("platform_overage_cost", decimal.Decimal{}).SchemaType(map[string]string{dialect.Postgres: "numeric", dialect.SQLite: "numeric"}).Optional().Nillable(),
		field.String("status").MaxLen(32).Default("pending").Validate(validateOrganizationAttemptAccountEffectStatus),
		field.String("audit_reason").MaxLen(2000).Optional().Nillable(),
		field.Time("applied_at").Optional().Nillable(),
	}
}

func (OrganizationAttemptAccountEffect) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("attempt_ref_id").Unique(),
		index.Fields("attempt_id_snapshot").Unique(),
		index.Fields("status", "created_at"),
	}
}

func validateOrganizationAttemptAccountEffectStatus(value string) error {
	switch value {
	case "pending", "applied", "applied_noop_deleted_target", "operator_attention":
		return nil
	default:
		return fmt.Errorf("invalid organization attempt Account effect status %q", value)
	}
}
