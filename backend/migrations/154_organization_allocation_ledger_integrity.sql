-- Allocation postings must be bound to the current allocation/member identity
-- when first written. Historical snapshots remain valid after live references
-- are cleared by their ON DELETE SET NULL foreign keys.
CREATE OR REPLACE FUNCTION guard_org_ledger_posting_integrity()
RETURNS TRIGGER AS $$
DECLARE
    operation_organization_id BIGINT;
    operation_uuid UUID;
    operation_currency VARCHAR(3);
    operation_occurred_at TIMESTAMPTZ;
    allocation_organization_id BIGINT;
    allocation_user_id_snapshot BIGINT;
BEGIN
    SELECT operation.organization_id, operation.operation_id, operation.currency, operation.occurred_at
    INTO operation_organization_id, operation_uuid, operation_currency, operation_occurred_at
    FROM organization_ledger_operations AS operation
    WHERE operation.id = NEW.operation_ref_id;

    IF NOT FOUND
       OR NEW.organization_id <> operation_organization_id
       OR NEW.operation_id_snapshot <> operation_uuid
       OR NEW.currency <> operation_currency
       OR NEW.occurred_at <> operation_occurred_at THEN
        RAISE EXCEPTION 'organization ledger posting does not match its operation'
            USING ERRCODE = '23514';
    END IF;

    IF TG_OP = 'INSERT'
       AND NEW.bucket_type IN ('allocation_available', 'allocation_reserved') THEN
        IF NEW.allocation_id_snapshot IS NULL
           OR NEW.allocation_ref_id IS NULL
           OR NEW.target_user_id_snapshot IS NULL
           OR NEW.target_user_ref_id IS NULL THEN
            RAISE EXCEPTION 'organization allocation posting requires live allocation and target references'
                USING ERRCODE = '23514';
        END IF;

        SELECT allocation.organization_id, allocation.user_id_snapshot
        INTO allocation_organization_id, allocation_user_id_snapshot
        FROM organization_allocations AS allocation
        WHERE allocation.id = NEW.allocation_ref_id;

        IF NOT FOUND
           OR NEW.allocation_id_snapshot <> NEW.allocation_ref_id
           OR allocation_organization_id <> NEW.organization_id
           OR NEW.target_user_id_snapshot <> allocation_user_id_snapshot THEN
            RAISE EXCEPTION 'organization ledger allocation reference is inconsistent'
                USING ERRCODE = '23514';
        END IF;

        IF NEW.target_user_id_snapshot <> NEW.target_user_ref_id THEN
            RAISE EXCEPTION 'organization ledger target user reference is inconsistent'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
