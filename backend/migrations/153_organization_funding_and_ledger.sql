-- Organization funding is USD-only and independent from personal balances,
-- subscriptions, payment orders, API key quota, RPM, and concurrency.
ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS unallocated_pool DECIMAL(20,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS balance_version BIGINT NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'organizations_unallocated_nonnegative_check'
    ) THEN
        ALTER TABLE organizations
            ADD CONSTRAINT organizations_unallocated_nonnegative_check
            CHECK (unallocated_pool >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'organizations_balance_version_nonnegative_check'
    ) THEN
        ALTER TABLE organizations
            ADD CONSTRAINT organizations_balance_version_nonnegative_check
            CHECK (balance_version >= 0);
    END IF;
END $$;

-- One immutable operation envelope owns actor, authorization, reason, and
-- durable idempotency snapshots. Raw idempotency keys are never persisted.
CREATE TABLE IF NOT EXISTS organization_ledger_operations (
    id BIGSERIAL PRIMARY KEY,
    operation_id UUID NOT NULL,
    correlation_id UUID NOT NULL,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    operation_type VARCHAR(64) NOT NULL,
    authorization_scope VARCHAR(64) NOT NULL,
    actor_id_snapshot BIGINT NOT NULL,
    actor_ref_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    actor_display_snapshot VARCHAR(255) NOT NULL,
    actor_role_snapshot VARCHAR(64) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    idempotency_key_hash CHAR(64) NOT NULL,
    canonical_payload_hash CHAR(64) NOT NULL,
    public_reason TEXT NULL,
    internal_reason TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT org_ledger_operations_operation_id_unique UNIQUE (operation_id),
    CONSTRAINT org_ledger_operations_idempotency_scope_unique UNIQUE (
        authorization_scope,
        organization_id,
        operation_type,
        idempotency_key_hash
    ),
    CONSTRAINT org_ledger_operations_currency_check
        CHECK (currency = 'USD'),
    CONSTRAINT org_ledger_operations_operation_type_check
        CHECK (operation_type IN (
            'organization_credit',
            'equal_allocation',
            'custom_allocation',
            'allocation_reclaim',
            'member_removal_reclaim',
            'reservation',
            'consumption',
            'release'
        )),
    CONSTRAINT org_ledger_operations_authorization_scope_check
        CHECK (authorization_scope IN (
            'platform_admin_jwt',
            'organization_admin_jwt',
            'system_execution'
        )),
    CONSTRAINT org_ledger_operations_actor_ref_check
        CHECK (actor_ref_id IS NULL OR actor_ref_id = actor_id_snapshot),
    CONSTRAINT org_ledger_operations_reason_check
        CHECK (trim(internal_reason) <> ''),
    CONSTRAINT org_ledger_operations_hash_check
        CHECK (
            idempotency_key_hash ~ '^[0-9a-f]{64}$'
            AND canonical_payload_hash ~ '^[0-9a-f]{64}$'
        )
);

CREATE INDEX IF NOT EXISTS org_ledger_operations_org_occurred_idx
    ON organization_ledger_operations (organization_id, occurred_at DESC, id DESC);

-- A posting records one exact bucket movement. Future allocation operations can
-- add a correlated pair without changing the operation/idempotency envelope.
CREATE TABLE IF NOT EXISTS organization_ledger_postings (
    id BIGSERIAL PRIMARY KEY,
    operation_ref_id BIGINT NOT NULL
        REFERENCES organization_ledger_operations(id) ON DELETE RESTRICT,
    operation_id_snapshot UUID NOT NULL,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    posting_sequence INTEGER NOT NULL,
    bucket_type VARCHAR(64) NOT NULL,
    bucket_id_snapshot BIGINT NOT NULL,
    allocation_id_snapshot BIGINT NULL,
    allocation_ref_id BIGINT NULL
        REFERENCES organization_allocations(id) ON DELETE SET NULL,
    target_user_id_snapshot BIGINT NULL,
    target_user_ref_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    before_amount DECIMAL(20,8) NOT NULL,
    delta_amount DECIMAL(20,8) NOT NULL,
    after_amount DECIMAL(20,8) NOT NULL,
    balance_version_before BIGINT NOT NULL,
    balance_version_after BIGINT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT org_ledger_postings_operation_sequence_unique
        UNIQUE (operation_ref_id, posting_sequence),
    CONSTRAINT org_ledger_postings_currency_check
        CHECK (currency = 'USD'),
    CONSTRAINT org_ledger_postings_sequence_check
        CHECK (posting_sequence > 0),
    CONSTRAINT org_ledger_postings_amounts_nonnegative_check
        CHECK (before_amount >= 0 AND after_amount >= 0),
    CONSTRAINT org_ledger_postings_delta_nonzero_check
        CHECK (delta_amount <> 0),
    CONSTRAINT org_ledger_postings_amount_equation_check
        CHECK (after_amount = before_amount + delta_amount),
    CONSTRAINT org_ledger_postings_version_equation_check
        CHECK (balance_version_after = balance_version_before + 1),
    CONSTRAINT org_ledger_postings_bucket_check
        CHECK (
            (
                bucket_type = 'organization_unallocated'
                AND bucket_id_snapshot = organization_id
                AND allocation_id_snapshot IS NULL
                AND allocation_ref_id IS NULL
            )
            OR (
                bucket_type IN ('allocation_available', 'allocation_reserved')
                AND allocation_id_snapshot IS NOT NULL
            )
        ),
    CONSTRAINT org_ledger_postings_allocation_ref_check
        CHECK (
            allocation_ref_id IS NULL
            OR allocation_ref_id = allocation_id_snapshot
        ),
    CONSTRAINT org_ledger_postings_target_user_ref_check
        CHECK (
            target_user_ref_id IS NULL
            OR target_user_ref_id = target_user_id_snapshot
        )
);

CREATE INDEX IF NOT EXISTS org_ledger_postings_org_occurred_idx
    ON organization_ledger_postings (organization_id, occurred_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS org_ledger_postings_org_bucket_idx
    ON organization_ledger_postings (organization_id, bucket_type, bucket_id_snapshot);

-- A posting is a durable snapshot of its operation. Keep the immutable
-- snapshots correlated at the database boundary, including writes that do not
-- pass through the Go repository.
CREATE OR REPLACE FUNCTION guard_org_ledger_posting_integrity()
RETURNS TRIGGER AS $$
DECLARE
    operation_organization_id BIGINT;
    operation_uuid UUID;
    operation_currency VARCHAR(3);
    operation_occurred_at TIMESTAMPTZ;
    allocation_organization_id BIGINT;
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

    -- Live allocation references must remain within the posting organization.
    -- When the referenced allocation is physically deleted, ON DELETE SET NULL
    -- reaches this trigger as an UPDATE and the immutable snapshot is retained.
    IF NEW.allocation_ref_id IS NOT NULL THEN
        SELECT organization_id
        INTO allocation_organization_id
        FROM organization_allocations
        WHERE id = NEW.allocation_ref_id;

        IF NOT FOUND
           OR NEW.allocation_id_snapshot IS NULL
           OR NEW.allocation_id_snapshot <> NEW.allocation_ref_id
           OR allocation_organization_id <> NEW.organization_id THEN
            RAISE EXCEPTION 'organization ledger allocation reference is inconsistent'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS organization_ledger_postings_integrity
    ON organization_ledger_postings;
CREATE TRIGGER organization_ledger_postings_integrity
    BEFORE INSERT OR UPDATE ON organization_ledger_postings
    FOR EACH ROW EXECUTE FUNCTION guard_org_ledger_posting_integrity();

-- Financial fields are immutable. The sole permitted update is an FK-driven
-- live-reference nulling; immutable ID/display snapshots remain unchanged.
CREATE OR REPLACE FUNCTION guard_org_ledger_operation_immutable()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.actor_ref_id IS NOT NULL
       AND NEW.actor_ref_id IS NULL
       AND (to_jsonb(NEW) - 'actor_ref_id') = (to_jsonb(OLD) - 'actor_ref_id') THEN
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'organization ledger operations are immutable'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_org_ledger_posting_immutable()
RETURNS TRIGGER AS $$
DECLARE
    allocation_ref_allowed BOOLEAN;
    target_user_ref_allowed BOOLEAN;
    reference_changed BOOLEAN;
BEGIN
    allocation_ref_allowed :=
        NEW.allocation_ref_id IS NOT DISTINCT FROM OLD.allocation_ref_id
        OR (OLD.allocation_ref_id IS NOT NULL AND NEW.allocation_ref_id IS NULL);
    target_user_ref_allowed :=
        NEW.target_user_ref_id IS NOT DISTINCT FROM OLD.target_user_ref_id
        OR (OLD.target_user_ref_id IS NOT NULL AND NEW.target_user_ref_id IS NULL);
    reference_changed :=
        NEW.allocation_ref_id IS DISTINCT FROM OLD.allocation_ref_id
        OR NEW.target_user_ref_id IS DISTINCT FROM OLD.target_user_ref_id;

    IF TG_OP = 'UPDATE'
       AND reference_changed
       AND allocation_ref_allowed
       AND target_user_ref_allowed
       AND (to_jsonb(NEW) - ARRAY['allocation_ref_id', 'target_user_ref_id'])
           = (to_jsonb(OLD) - ARRAY['allocation_ref_id', 'target_user_ref_id']) THEN
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'organization ledger postings are immutable'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS organization_ledger_operations_immutable
    ON organization_ledger_operations;
CREATE TRIGGER organization_ledger_operations_immutable
    BEFORE UPDATE OR DELETE ON organization_ledger_operations
    FOR EACH ROW EXECUTE FUNCTION guard_org_ledger_operation_immutable();

DROP TRIGGER IF EXISTS organization_ledger_postings_immutable
    ON organization_ledger_postings;
CREATE TRIGGER organization_ledger_postings_immutable
    BEFORE UPDATE OR DELETE ON organization_ledger_postings
    FOR EACH ROW EXECUTE FUNCTION guard_org_ledger_posting_immutable();
