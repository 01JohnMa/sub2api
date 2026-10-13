package schema

import (
	"fmt"
	"time"

	"github.com/MACOS-DO/sub4api/internal/domain"
	"github.com/shopspring/decimal"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrganizationLedgerPosting is an immutable balance movement. A posting is
// deliberately separate from its operation envelope so one operation can later
// contain a correlated pair of pool/allocation postings.
type OrganizationLedgerPosting struct {
	ent.Schema
}

func (OrganizationLedgerPosting) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "organization_ledger_postings",
			Checks: map[string]string{
				"org_ledger_postings_operation_snapshot_check":  "trim(operation_id_snapshot) <> ''",
				"org_ledger_postings_currency_check":            "currency = 'USD'",
				"org_ledger_postings_sequence_check":            "posting_sequence > 0",
				"org_ledger_postings_amounts_nonnegative_check": "before_amount >= 0 AND after_amount >= 0",
				"org_ledger_postings_delta_nonzero_check":       "delta_amount <> 0",
				"org_ledger_postings_amount_equation_check":     "after_amount = before_amount + delta_amount",
				"org_ledger_postings_version_equation_check":    "balance_version_after = balance_version_before + 1",
				"org_ledger_postings_bucket_check":              "((bucket_type = 'organization_unallocated' AND bucket_id_snapshot = organization_id AND allocation_id_snapshot IS NULL) OR (bucket_type IN ('allocation_available', 'allocation_reserved') AND allocation_id_snapshot IS NOT NULL))",
				"org_ledger_postings_allocation_ref_check":      "(allocation_ref_id IS NULL OR allocation_ref_id = allocation_id_snapshot)",
				"org_ledger_postings_target_user_ref_check":     "(target_user_ref_id IS NULL OR target_user_ref_id = target_user_id_snapshot)",
				"org_ledger_postings_execution_ref_check":       "(execution_ref_id IS NULL OR execution_id_snapshot IS NOT NULL)",
				"org_ledger_postings_final_evidence_ref_check":  "(final_evidence_ref_id IS NULL OR final_evidence_id_snapshot IS NOT NULL)",
			},
		},
	}
}

func (OrganizationLedgerPosting) Fields() []ent.Field {
	amount := func(name string) ent.Field {
		return field.Other(name, decimal.Decimal{}).
			SchemaType(map[string]string{
				dialect.Postgres: "decimal(20,8)",
				dialect.SQLite:   "numeric",
			}).
			Immutable()
	}
	return []ent.Field{
		field.Int64("operation_ref_id").
			Immutable(),
		field.String("operation_id_snapshot").
			MaxLen(36).
			SchemaType(map[string]string{
				dialect.Postgres: "uuid",
				dialect.SQLite:   "text",
			}).
			Immutable(),
		field.Int64("organization_id").
			Immutable(),
		field.Int("posting_sequence").
			SchemaType(map[string]string{dialect.Postgres: "integer"}).
			Immutable(),
		field.String("bucket_type").
			MaxLen(64).
			Validate(validateOrganizationLedgerBucket).
			Immutable(),
		field.Int64("bucket_id_snapshot").
			Immutable(),
		field.Int64("allocation_id_snapshot").
			Optional().
			Nillable().
			Immutable(),
		field.Int64("allocation_ref_id").
			Optional().
			Nillable().
			Immutable(),
		field.Int64("target_user_id_snapshot").
			Optional().
			Nillable().
			Immutable(),
		field.Int64("target_user_ref_id").
			Optional().
			Nillable().
			Immutable(),
		field.String("execution_id_snapshot").
			MaxLen(36).
			Optional().
			Nillable().
			SchemaType(map[string]string{
				dialect.Postgres: "uuid",
				dialect.SQLite:   "text",
			}).
			Immutable(),
		field.Int64("execution_ref_id").
			Optional().
			Nillable().
			Immutable(),
		field.Int64("final_evidence_id_snapshot").
			Optional().
			Nillable().
			Immutable(),
		field.Int64("final_evidence_ref_id").
			Optional().
			Nillable().
			Immutable(),
		field.String("currency").
			MaxLen(3).
			Default(domain.OrganizationCurrencyUSD).
			Immutable(),
		amount("before_amount"),
		amount("delta_amount"),
		amount("after_amount"),
		field.Int64("balance_version_before").
			Immutable(),
		field.Int64("balance_version_after").
			Immutable(),
		field.Time("occurred_at").
			Immutable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

func (OrganizationLedgerPosting) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("operation", OrganizationLedgerOperation.Type).
			Ref("postings").
			Field("operation_ref_id").
			Unique().
			Required().
			Immutable(),
		edge.From("organization", Organization.Type).
			Ref("ledger_postings").
			Field("organization_id").
			Unique().
			Required().
			Immutable(),
		edge.To("allocation_ref", OrganizationAllocation.Type).
			Field("allocation_ref_id").
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("target_user_ref", User.Type).
			Field("target_user_ref_id").
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("execution_ref", OrganizationExecution.Type).
			Field("execution_ref_id").
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("final_evidence_ref", OrganizationFinalExecutionEvidence.Type).
			Field("final_evidence_ref_id").
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (OrganizationLedgerPosting) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("operation_ref_id", "posting_sequence").Unique(),
		index.Fields("organization_id", "occurred_at"),
		index.Fields("organization_id", "bucket_type", "bucket_id_snapshot"),
	}
}

func validateOrganizationLedgerBucket(bucket string) error {
	switch bucket {
	case domain.OrganizationLedgerBucketUnallocated,
		domain.OrganizationLedgerBucketAllocationAvailable,
		domain.OrganizationLedgerBucketAllocationReserved:
		return nil
	default:
		return fmt.Errorf("invalid organization ledger bucket %q", bucket)
	}
}
