package schema

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrganizationAttemptAccountPosting is the immutable platform cost fact for
// one dispatch attempt. It is distinct from member/organization settlement.
type OrganizationAttemptAccountPosting struct {
	ent.Schema
}

func (OrganizationAttemptAccountPosting) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_attempt_account_postings",
			Checks: map[string]string{
				"organization_attempt_account_postings_amount_check":         "raw_actual_cost >= 0 AND reservation_ceiling >= 0 AND platform_overage_cost >= 0",
				"organization_attempt_account_postings_classification_check": "classification IN ('normal', 'platform_overage')",
				"organization_attempt_account_postings_evidence_ref_check":   "evidence_ref_id IS NULL OR evidence_ref_id = evidence_id_snapshot",
				"organization_attempt_account_postings_account_ref_check":    "account_ref_id IS NULL OR account_ref_id = account_id_snapshot",
			},
		},
	}
}

func (OrganizationAttemptAccountPosting) Fields() []ent.Field {
	return []ent.Field{
		field.String("attempt_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("attempt_ref_id").Immutable(),
		field.Int64("evidence_id_snapshot").Immutable(),
		field.Int64("evidence_ref_id").Optional().Nillable().Immutable(),
		field.String("execution_id_snapshot").MaxLen(36).SchemaType(map[string]string{dialect.Postgres: "uuid", dialect.SQLite: "text"}).Immutable(),
		field.Int64("execution_ref_id").Immutable(),
		field.Int64("organization_id").Immutable(),
		field.Int64("account_id_snapshot").Immutable(),
		field.Int64("account_ref_id").Optional().Nillable().Immutable(),
		field.Other("raw_actual_cost", decimal.Decimal{}).SchemaType(map[string]string{dialect.Postgres: "numeric", dialect.SQLite: "numeric"}).Immutable(),
		field.Other("reservation_ceiling", decimal.Decimal{}).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)", dialect.SQLite: "numeric"}).Immutable(),
		field.Other("platform_overage_cost", decimal.Decimal{}).SchemaType(map[string]string{dialect.Postgres: "numeric", dialect.SQLite: "numeric"}).Default(decimal.Zero).Immutable(),
		field.String("classification").MaxLen(32).Default("normal").Validate(validateOrganizationAttemptAccountPostingClassification).Immutable(),
		field.Time("occurred_at").Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (OrganizationAttemptAccountPosting) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("attempt_ref_id").Unique(),
		index.Fields("attempt_id_snapshot").Unique(),
		index.Fields("evidence_id_snapshot").Unique(),
		index.Fields("organization_id", "occurred_at"),
	}
}

func validateOrganizationAttemptAccountPostingClassification(value string) error {
	switch value {
	case "normal", "platform_overage":
		return nil
	default:
		return fmt.Errorf("invalid organization attempt Account posting classification %q", value)
	}
}
