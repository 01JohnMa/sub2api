-- Confirmed equal-allocation no-ops need a durable scoped idempotency result,
-- but they are not monetary ledger movements and therefore have no postings.
ALTER TABLE organization_ledger_operations
    ADD COLUMN IF NOT EXISTS no_op_result_snapshot JSONB NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'org_ledger_operations_no_op_snapshot_check'
    ) THEN
        ALTER TABLE organization_ledger_operations
            ADD CONSTRAINT org_ledger_operations_no_op_snapshot_check
            CHECK (
                no_op_result_snapshot IS NULL
                OR (
                    operation_type = 'equal_allocation'
                    AND currency = 'USD'
                    AND no_op_result_snapshot->>'outcome' = 'no_op'
                    AND no_op_result_snapshot->>'organization_id' = organization_id::text
                    AND no_op_result_snapshot->>'operation_type' = operation_type
                    AND no_op_result_snapshot->>'currency' = currency
                    AND jsonb_typeof(no_op_result_snapshot->'postings') = 'array'
                    AND jsonb_array_length(no_op_result_snapshot->'postings') = 0
                )
            );
    END IF;
END $$;
