-- A zero-allocation member removal still changes membership and Key attribution,
-- but it has no monetary delta and therefore no ledger postings. Persist the
-- committed result in the existing scoped idempotency envelope.
ALTER TABLE organization_ledger_operations
    DROP CONSTRAINT IF EXISTS org_ledger_operations_no_op_snapshot_check;

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
        OR (
            operation_type = 'member_removal_reclaim'
            AND currency = 'USD'
            AND no_op_result_snapshot->>'outcome' = 'committed'
            AND no_op_result_snapshot->>'organization_id' = organization_id::text
            AND no_op_result_snapshot->>'operation_type' = operation_type
            AND no_op_result_snapshot->>'currency' = currency
            AND no_op_result_snapshot->>'operation_id' = operation_id::text
            AND no_op_result_snapshot->>'correlation_id' = correlation_id::text
            AND no_op_result_snapshot->>'canonical_payload_hash' = canonical_payload_hash
            AND no_op_result_snapshot->>'reclaimed' = '0.00000000'
            AND no_op_result_snapshot->>'pool_before' = no_op_result_snapshot->>'pool_after'
        )
    );
