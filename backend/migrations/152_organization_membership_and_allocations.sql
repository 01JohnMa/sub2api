-- Current organization attribution remains nullable for all existing users and
-- API keys. No historical user, key, group, payment, or upstream data is used
-- to infer a membership.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS organization_id BIGINT,
    ADD COLUMN IF NOT EXISTS organization_role VARCHAR(32);

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS organization_id BIGINT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'users_organization_id_fkey'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_organization_id_fkey
            FOREIGN KEY (organization_id)
            REFERENCES organizations(id)
            ON DELETE RESTRICT;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'users_organization_assignment_check'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_organization_assignment_check
            CHECK (
                (organization_id IS NULL AND organization_role IS NULL)
                OR (
                    organization_id IS NOT NULL
                    AND organization_role IS NOT NULL
                    AND organization_role IN ('organization_admin', 'organization_member')
                )
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'api_keys_organization_id_fkey'
    ) THEN
        ALTER TABLE api_keys
            ADD CONSTRAINT api_keys_organization_id_fkey
            FOREIGN KEY (organization_id)
            REFERENCES organizations(id)
            ON DELETE RESTRICT;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS users_organization_id_idx
    ON users (organization_id);

CREATE INDEX IF NOT EXISTS api_keys_organization_id_idx
    ON api_keys (organization_id)
    WHERE deleted_at IS NULL;

-- Allocation rows retain a non-null membership snapshot. The live User
-- reference may be removed, but financial history is never cascade-deleted.
CREATE TABLE IF NOT EXISTS organization_allocations (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    user_id_snapshot BIGINT NOT NULL,
    user_ref_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    available DECIMAL(20,8) NOT NULL DEFAULT 0,
    reserved DECIMAL(20,8) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT organization_allocations_status_check
        CHECK (status IN ('active', 'closed')),
    CONSTRAINT organization_allocations_amounts_nonnegative_check
        CHECK (available >= 0 AND reserved >= 0),
    CONSTRAINT organization_allocations_closed_zero_check
        CHECK (status = 'active' OR (available = 0 AND reserved = 0)),
    CONSTRAINT organization_allocations_user_snapshot_check
        CHECK (user_ref_id IS NULL OR user_ref_id = user_id_snapshot),
    CONSTRAINT organization_allocations_active_user_ref_check
        CHECK (status <> 'active' OR user_ref_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS organization_allocations_organization_status_idx
    ON organization_allocations (organization_id, status);

CREATE INDEX IF NOT EXISTS organization_allocations_user_ref_id_idx
    ON organization_allocations (user_ref_id);

CREATE UNIQUE INDEX IF NOT EXISTS organization_allocations_active_user_snapshot_unique
    ON organization_allocations (user_id_snapshot)
    WHERE status = 'active';
