-- Durable organization execution settlement core. This migration is additive:
-- existing personal requests remain unorganized and no existing balance, Group,
-- payment, Account, or upstream OAuth data is inferred or rewritten.

-- Allocation versions are captured by admission and compared in the reservation
-- transaction. They advance with every available/reserved bucket mutation.
ALTER TABLE organization_allocations
    ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'organization_allocations_version_nonnegative_check'
    ) THEN
        ALTER TABLE organization_allocations
            ADD CONSTRAINT organization_allocations_version_nonnegative_check
            CHECK (version >= 0);
    END IF;
END $$;

-- A server-generated execution is the durable parent identity. Client request
-- identifiers are metadata only; retry identity is the trusted fingerprint.
CREATE TABLE IF NOT EXISTS organization_executions (
    id BIGSERIAL PRIMARY KEY,
    execution_id UUID NOT NULL,
    correlation_id UUID NOT NULL,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    allocation_id_snapshot BIGINT NULL,
    allocation_ref_id BIGINT NULL REFERENCES organization_allocations(id) ON DELETE RESTRICT,
    user_id_snapshot BIGINT NOT NULL,
    user_ref_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    api_key_id_snapshot BIGINT NOT NULL,
    api_key_ref_id BIGINT NULL REFERENCES api_keys(id) ON DELETE SET NULL,
    endpoint_family VARCHAR(128) NOT NULL,
    request_shape VARCHAR(128) NOT NULL,
    model VARCHAR(255) NULL,
    client_request_id VARCHAR(512) NULL,
    billing_fingerprint CHAR(64) NOT NULL,
    candidate_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    candidate_set_hash CHAR(64) NOT NULL,
    pricing_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    pricing_snapshot_hash CHAR(64) NOT NULL,
    candidate_ceiling DECIMAL(20,8) NOT NULL,
    is_billable BOOLEAN NOT NULL DEFAULT TRUE,
    status VARCHAR(32) NOT NULL DEFAULT 'accepted',
    accepted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    terminalized_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT organization_executions_execution_id_unique UNIQUE (execution_id),
    CONSTRAINT organization_executions_correlation_id_unique UNIQUE (correlation_id),
    CONSTRAINT organization_executions_status_check
        CHECK (status IN ('accepted', 'reserved', 'dispatched', 'final_usage_observed', 'pending_reconciliation', 'settled', 'released')),
    CONSTRAINT organization_executions_amount_check CHECK (candidate_ceiling >= 0),
    CONSTRAINT organization_executions_hash_check
        CHECK (billing_fingerprint ~ '^[0-9a-f]{64}$' AND candidate_set_hash ~ '^[0-9a-f]{64}$' AND pricing_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT organization_executions_allocation_ref_check
        CHECK (allocation_ref_id IS NULL OR allocation_ref_id = allocation_id_snapshot),
    CONSTRAINT organization_executions_user_ref_check
        CHECK (user_ref_id IS NULL OR user_ref_id = user_id_snapshot),
    CONSTRAINT organization_executions_api_key_ref_check
        CHECK (api_key_ref_id IS NULL OR api_key_ref_id = api_key_id_snapshot),
    CONSTRAINT organization_executions_non_billable_ceiling_check
        CHECK (is_billable OR candidate_ceiling = 0)
);

CREATE INDEX IF NOT EXISTS organization_executions_org_status_accepted_idx
    ON organization_executions (organization_id, status, accepted_at DESC);
CREATE INDEX IF NOT EXISTS organization_executions_allocation_snapshot_idx
    ON organization_executions (allocation_id_snapshot, accepted_at DESC)
    WHERE allocation_id_snapshot IS NOT NULL;

-- A billable execution has at most one parent reservation. The snapshot fields
-- are the values revalidated by the atomic reservation transaction.
CREATE TABLE IF NOT EXISTS organization_reservations (
    id BIGSERIAL PRIMARY KEY,
    execution_id_snapshot UUID NOT NULL,
    execution_ref_id BIGINT NOT NULL REFERENCES organization_executions(id) ON DELETE RESTRICT,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    allocation_id_snapshot BIGINT NOT NULL,
    allocation_ref_id BIGINT NOT NULL REFERENCES organization_allocations(id) ON DELETE RESTRICT,
    allocation_version_snapshot BIGINT NOT NULL,
    organization_balance_version_snapshot BIGINT NOT NULL,
    billing_fingerprint CHAR(64) NOT NULL,
    candidate_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    candidate_set_hash CHAR(64) NOT NULL,
    pricing_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    pricing_snapshot_hash CHAR(64) NOT NULL,
    reserved_amount DECIMAL(20,8) NOT NULL,
    settled_amount DECIMAL(20,8) NOT NULL DEFAULT 0,
    released_amount DECIMAL(20,8) NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'reserved',
    reserved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    terminalized_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT organization_reservations_execution_ref_unique UNIQUE (execution_ref_id),
    CONSTRAINT organization_reservations_execution_snapshot_unique UNIQUE (execution_id_snapshot),
    CONSTRAINT organization_reservations_status_check
        CHECK (status IN ('reserved', 'pending_reconciliation', 'settled', 'released')),
    CONSTRAINT organization_reservations_amount_check
        CHECK (reserved_amount >= 0 AND settled_amount >= 0 AND released_amount >= 0 AND settled_amount + released_amount <= reserved_amount),
    CONSTRAINT organization_reservations_version_check
        CHECK (allocation_version_snapshot >= 0 AND organization_balance_version_snapshot >= 0),
    CONSTRAINT organization_reservations_hash_check
        CHECK (billing_fingerprint ~ '^[0-9a-f]{64}$' AND candidate_set_hash ~ '^[0-9a-f]{64}$' AND pricing_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT organization_reservations_execution_ref_check
        CHECK (execution_ref_id IS NOT NULL),
    CONSTRAINT organization_reservations_allocation_ref_check
        CHECK (allocation_ref_id = allocation_id_snapshot),
    CONSTRAINT organization_reservations_terminal_amount_check
        CHECK (
            (status IN ('reserved', 'pending_reconciliation') AND settled_amount = 0 AND released_amount = 0)
            OR (status IN ('settled', 'released') AND settled_amount + released_amount = reserved_amount)
        )
);

CREATE INDEX IF NOT EXISTS organization_reservations_allocation_status_idx
    ON organization_reservations (allocation_id_snapshot, status)
    WHERE status IN ('reserved', 'pending_reconciliation');
CREATE INDEX IF NOT EXISTS organization_reservations_org_status_idx
    ON organization_reservations (organization_id, status, reserved_at DESC);

-- An attempt identifies one server-side dispatch. The first implementation
-- uses exactly one attempt per execution; sequence uniqueness leaves evidence
-- history unambiguous without enabling failover behavior yet.
CREATE TABLE IF NOT EXISTS organization_execution_attempts (
    id BIGSERIAL PRIMARY KEY,
    attempt_id UUID NOT NULL,
    execution_id_snapshot UUID NOT NULL,
    execution_ref_id BIGINT NOT NULL REFERENCES organization_executions(id) ON DELETE RESTRICT,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    attempt_sequence INTEGER NOT NULL,
    account_id_snapshot BIGINT NULL,
    account_ref_id BIGINT NULL REFERENCES accounts(id) ON DELETE SET NULL,
    route_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    route_snapshot_hash CHAR(64) NOT NULL,
    pricing_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    pricing_snapshot_hash CHAR(64) NOT NULL,
    provider_correlation_id VARCHAR(255) NOT NULL,
    provider_request_id VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'created',
    dispatched_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT organization_execution_attempts_attempt_id_unique UNIQUE (attempt_id),
    CONSTRAINT organization_execution_attempts_execution_sequence_unique UNIQUE (execution_ref_id, attempt_sequence),
    CONSTRAINT organization_execution_attempts_execution_snapshot_sequence_unique UNIQUE (execution_id_snapshot, attempt_sequence),
    CONSTRAINT organization_execution_attempts_sequence_check CHECK (attempt_sequence = 1),
    CONSTRAINT organization_execution_attempts_status_check
        CHECK (status IN ('created', 'dispatched', 'final_usage_observed')),
    CONSTRAINT organization_execution_attempts_hash_check
        CHECK (route_snapshot_hash ~ '^[0-9a-f]{64}$' AND pricing_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT organization_execution_attempts_account_ref_check
        CHECK (account_ref_id IS NULL OR account_ref_id = account_id_snapshot)
);

CREATE INDEX IF NOT EXISTS organization_execution_attempts_org_created_idx
    ON organization_execution_attempts (organization_id, created_at DESC);

-- Provider/adapter evidence is append-only and is written before any
-- discardable worker. The ticket's fake adapter uses the fixed fake_adapter
-- provenance; no source-selection or reconciliation behavior is introduced.
CREATE TABLE IF NOT EXISTS organization_attempt_evidences (
    id BIGSERIAL PRIMARY KEY,
    attempt_id_snapshot UUID NOT NULL,
    attempt_ref_id BIGINT NOT NULL REFERENCES organization_execution_attempts(id) ON DELETE RESTRICT,
    execution_id_snapshot UUID NOT NULL,
    execution_ref_id BIGINT NOT NULL REFERENCES organization_executions(id) ON DELETE RESTRICT,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    account_id_snapshot BIGINT NULL,
    account_ref_id BIGINT NULL REFERENCES accounts(id) ON DELETE SET NULL,
    route_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    route_snapshot_hash CHAR(64) NOT NULL,
    pricing_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    pricing_snapshot_hash CHAR(64) NOT NULL,
    provider_correlation_id VARCHAR(255) NOT NULL,
    provider_request_id VARCHAR(255) NOT NULL,
    evidence_source VARCHAR(32) NOT NULL DEFAULT 'fake_adapter',
    provenance_hash CHAR(64) NOT NULL,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    evidence_hash CHAR(64) NOT NULL,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    raw_actual_cost NUMERIC NOT NULL DEFAULT 0,
    provider_observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT organization_attempt_evidences_attempt_ref_unique UNIQUE (attempt_ref_id),
    CONSTRAINT organization_attempt_evidences_attempt_snapshot_unique UNIQUE (attempt_id_snapshot),
    CONSTRAINT organization_attempt_evidences_attempt_provenance_unique UNIQUE (attempt_id_snapshot, provenance_hash),
    CONSTRAINT organization_attempt_evidences_source_check
        CHECK (evidence_source IN ('fake_adapter', 'adapter_direct')),
    CONSTRAINT organization_attempt_evidences_provenance_hash_check
        CHECK (provenance_hash ~ '^[0-9a-f]{64}$' AND evidence_hash ~ '^[0-9a-f]{64}$' AND route_snapshot_hash ~ '^[0-9a-f]{64}$' AND pricing_snapshot_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT organization_attempt_evidences_counts_cost_check
        CHECK (
            input_tokens >= 0 AND output_tokens >= 0
            AND cache_creation_tokens >= 0 AND cache_read_tokens >= 0
            AND raw_actual_cost >= 0
        ),
    CONSTRAINT organization_attempt_evidences_account_ref_check
        CHECK (account_ref_id IS NULL OR account_ref_id = account_id_snapshot)
);

CREATE INDEX IF NOT EXISTS organization_attempt_evidences_org_observed_idx
    ON organization_attempt_evidences (organization_id, provider_observed_at DESC);

-- One immutable final evidence row is the organization reporting and member
-- settlement fact. It may be based on an immutable attempt or a durable zero
-- proof for non-billable/pre-dispatch requests.
CREATE TABLE IF NOT EXISTS organization_final_execution_evidences (
    id BIGSERIAL PRIMARY KEY,
    execution_id_snapshot UUID NOT NULL,
    execution_ref_id BIGINT NOT NULL REFERENCES organization_executions(id) ON DELETE RESTRICT,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    allocation_id_snapshot BIGINT NULL,
    allocation_ref_id BIGINT NULL REFERENCES organization_allocations(id) ON DELETE RESTRICT,
    user_id_snapshot BIGINT NOT NULL,
    user_ref_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    api_key_id_snapshot BIGINT NOT NULL,
    api_key_ref_id BIGINT NULL REFERENCES api_keys(id) ON DELETE SET NULL,
    final_attempt_id_snapshot UUID NULL,
    final_attempt_ref_id BIGINT NULL REFERENCES organization_execution_attempts(id) ON DELETE SET NULL,
    final_attempt_evidence_id_snapshot BIGINT NULL,
    final_attempt_evidence_ref_id BIGINT NULL REFERENCES organization_attempt_evidences(id) ON DELETE SET NULL,
    account_id_snapshot BIGINT NULL,
    account_ref_id BIGINT NULL REFERENCES accounts(id) ON DELETE SET NULL,
    endpoint_family VARCHAR(128) NOT NULL,
    request_shape VARCHAR(128) NOT NULL,
    model VARCHAR(255) NULL,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    raw_actual_cost NUMERIC NOT NULL DEFAULT 0,
    organization_settled_cost DECIMAL(20,8) NOT NULL DEFAULT 0,
    terminal_outcome VARCHAR(16) NOT NULL,
    is_business_limited BOOLEAN NOT NULL DEFAULT FALSE,
    is_count_tokens_probe BOOLEAN NOT NULL DEFAULT FALSE,
    zero_cost_proof JSONB NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    reconciled_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT organization_final_execution_evidences_execution_ref_unique UNIQUE (execution_ref_id),
    CONSTRAINT organization_final_execution_evidences_execution_snapshot_unique UNIQUE (execution_id_snapshot),
    CONSTRAINT organization_final_execution_evidences_outcome_check
        CHECK (terminal_outcome IN ('success', 'error')),
    CONSTRAINT organization_final_execution_evidences_counts_cost_check
        CHECK (
            input_tokens >= 0 AND output_tokens >= 0
            AND cache_creation_tokens >= 0 AND cache_read_tokens >= 0
            AND raw_actual_cost >= 0 AND organization_settled_cost >= 0
        ),
    CONSTRAINT organization_final_execution_evidences_allocation_ref_check
        CHECK (allocation_ref_id IS NULL OR allocation_ref_id = allocation_id_snapshot),
    CONSTRAINT organization_final_execution_evidences_user_ref_check
        CHECK (user_ref_id IS NULL OR user_ref_id = user_id_snapshot),
    CONSTRAINT organization_final_execution_evidences_api_key_ref_check
        CHECK (api_key_ref_id IS NULL OR api_key_ref_id = api_key_id_snapshot),
    CONSTRAINT organization_final_execution_evidences_attempt_ref_check
        CHECK (
            (final_attempt_id_snapshot IS NULL AND final_attempt_ref_id IS NULL)
            OR (final_attempt_id_snapshot IS NOT NULL AND final_attempt_ref_id IS NOT NULL)
        ),
    CONSTRAINT organization_final_execution_evidences_attempt_evidence_ref_check
        CHECK (
            (final_attempt_evidence_id_snapshot IS NULL AND final_attempt_evidence_ref_id IS NULL)
            OR (final_attempt_evidence_id_snapshot IS NOT NULL AND final_attempt_evidence_ref_id IS NOT NULL)
        ),
    CONSTRAINT organization_final_execution_evidences_account_ref_check
        CHECK (account_ref_id IS NULL OR account_ref_id = account_id_snapshot),
    CONSTRAINT organization_final_execution_evidences_zero_proof_check
        CHECK (
            zero_cost_proof IS NULL
            OR (
                jsonb_typeof(zero_cost_proof) = 'object'
                AND zero_cost_proof ? 'reason'
                AND jsonb_typeof(zero_cost_proof -> 'reason') = 'string'
                AND LENGTH(BTRIM(zero_cost_proof ->> 'reason')) BETWEEN 1 AND 2000
                AND zero_cost_proof = jsonb_build_object('reason', zero_cost_proof ->> 'reason')
            )
        ),
    CONSTRAINT organization_final_execution_evidences_cost_proof_check
        CHECK (
            (
                organization_settled_cost > 0
                AND allocation_id_snapshot IS NOT NULL
                AND allocation_ref_id IS NOT NULL
                AND final_attempt_id_snapshot IS NOT NULL
                AND final_attempt_ref_id IS NOT NULL
                AND final_attempt_evidence_id_snapshot IS NOT NULL
                AND final_attempt_evidence_ref_id IS NOT NULL
                AND zero_cost_proof IS NULL
            )
            OR (
                organization_settled_cost = 0
                AND (
                    (
                        final_attempt_id_snapshot IS NOT NULL
                        AND final_attempt_ref_id IS NOT NULL
                        AND final_attempt_evidence_id_snapshot IS NOT NULL
                        AND final_attempt_evidence_ref_id IS NOT NULL
                        AND zero_cost_proof IS NULL
                    )
                    OR (
                        final_attempt_id_snapshot IS NULL
                        AND final_attempt_ref_id IS NULL
                        AND final_attempt_evidence_id_snapshot IS NULL
                        AND final_attempt_evidence_ref_id IS NULL
                        AND account_id_snapshot IS NULL
                        AND account_ref_id IS NULL
                        AND input_tokens = 0
                        AND output_tokens = 0
                        AND cache_creation_tokens = 0
                        AND cache_read_tokens = 0
                        AND raw_actual_cost = 0
                        AND zero_cost_proof IS NOT NULL
                    )
                )
            )
        )
);

CREATE INDEX IF NOT EXISTS organization_final_execution_evidences_org_occurred_idx
    ON organization_final_execution_evidences (organization_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS organization_final_execution_evidences_allocation_occurred_idx
    ON organization_final_execution_evidences (allocation_id_snapshot, occurred_at DESC)
    WHERE allocation_id_snapshot IS NOT NULL;

-- Terminalization emits durable projection/quota work in the same transaction.
-- This table records intent only; workers and Account-side effects are deferred.
CREATE TABLE IF NOT EXISTS organization_execution_outboxes (
    id BIGSERIAL PRIMARY KEY,
    execution_id_snapshot UUID NOT NULL,
    execution_ref_id BIGINT NOT NULL REFERENCES organization_executions(id) ON DELETE RESTRICT,
    final_evidence_id_snapshot BIGINT NOT NULL,
    final_evidence_ref_id BIGINT NULL REFERENCES organization_final_execution_evidences(id) ON DELETE SET NULL,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    event_type VARCHAR(32) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    occurred_at TIMESTAMPTZ NOT NULL,
    applied_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT organization_execution_outboxes_execution_event_unique UNIQUE (execution_ref_id, event_type),
    CONSTRAINT organization_execution_outboxes_execution_snapshot_event_unique UNIQUE (execution_id_snapshot, event_type),
    CONSTRAINT organization_execution_outboxes_event_type_check
        CHECK (event_type IN ('usage_projection', 'error_projection', 'api_key_quota', 'user_platform_quota')),
    CONSTRAINT organization_execution_outboxes_status_check CHECK (status IN ('pending', 'applied', 'applied_noop_deleted_target', 'operator_attention')),
    CONSTRAINT organization_execution_outboxes_evidence_ref_check
        CHECK (final_evidence_ref_id IS NULL OR final_evidence_ref_id = final_evidence_id_snapshot)
);

CREATE INDEX IF NOT EXISTS organization_execution_outboxes_pending_idx
    ON organization_execution_outboxes (created_at, id)
    WHERE status = 'pending';

-- System-ledger entries retain their execution identity without changing the
-- existing non-null actor snapshot type used by current readers and DTOs.
ALTER TABLE organization_ledger_operations
    ADD COLUMN IF NOT EXISTS actor_execution_id_snapshot UUID NULL,
    ADD COLUMN IF NOT EXISTS actor_execution_ref_id BIGINT NULL
        REFERENCES organization_executions(id) ON DELETE SET NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'org_ledger_operations_actor_execution_ref_check'
    ) THEN
        ALTER TABLE organization_ledger_operations
            ADD CONSTRAINT org_ledger_operations_actor_execution_ref_check
            CHECK (
                actor_execution_ref_id IS NULL
                OR actor_execution_id_snapshot IS NOT NULL
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'org_ledger_operations_actor_execution_identity_check'
    ) THEN
        ALTER TABLE organization_ledger_operations
            ADD CONSTRAINT org_ledger_operations_actor_execution_identity_check
            CHECK (
                (
                    authorization_scope = 'system_execution'
                    AND actor_id_snapshot = 0
                    AND actor_ref_id IS NULL
                    AND actor_execution_id_snapshot IS NOT NULL
                )
                OR (
                    authorization_scope IN ('platform_admin_jwt', 'organization_admin_jwt')
                    AND actor_id_snapshot > 0
                    AND actor_execution_id_snapshot IS NULL
                    AND actor_execution_ref_id IS NULL
                )
            );
    END IF;
END $$;

ALTER TABLE organization_ledger_postings
    ADD COLUMN IF NOT EXISTS execution_id_snapshot UUID NULL,
    ADD COLUMN IF NOT EXISTS execution_ref_id BIGINT NULL
        REFERENCES organization_executions(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS final_evidence_id_snapshot BIGINT NULL,
    ADD COLUMN IF NOT EXISTS final_evidence_ref_id BIGINT NULL
        REFERENCES organization_final_execution_evidences(id) ON DELETE SET NULL;

-- Allocation is a financial history anchor. Earlier posting migrations used a
-- nullable live reference for generic user deletion; execution settlement now
-- relies on allocation identity remaining physically retained.
ALTER TABLE organization_ledger_postings
    DROP CONSTRAINT IF EXISTS organization_ledger_postings_allocation_ref_id_fkey;
ALTER TABLE organization_ledger_postings
    ADD CONSTRAINT organization_ledger_postings_allocation_ref_id_fkey
    FOREIGN KEY (allocation_ref_id)
    REFERENCES organization_allocations(id) ON DELETE RESTRICT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'org_ledger_postings_execution_ref_check'
    ) THEN
        ALTER TABLE organization_ledger_postings
            ADD CONSTRAINT org_ledger_postings_execution_ref_check
            CHECK (execution_ref_id IS NULL OR execution_id_snapshot IS NOT NULL);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'org_ledger_postings_final_evidence_ref_check'
    ) THEN
        ALTER TABLE organization_ledger_postings
            ADD CONSTRAINT org_ledger_postings_final_evidence_ref_check
            CHECK (final_evidence_ref_id IS NULL OR final_evidence_id_snapshot IS NOT NULL);
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS org_ledger_postings_final_evidence_unique
    ON organization_ledger_postings (final_evidence_id_snapshot)
    WHERE final_evidence_id_snapshot IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS org_ledger_postings_consumption_execution_unique
    ON organization_ledger_postings (execution_id_snapshot)
    WHERE final_evidence_id_snapshot IS NOT NULL;

-- Cross-row integrity checks are insertion-only for mutable live references:
-- later ON DELETE SET NULL updates retain the immutable snapshots.
CREATE OR REPLACE FUNCTION guard_organization_execution_integrity()
RETURNS TRIGGER AS $$
DECLARE
    allocation_organization_id BIGINT;
    allocation_user_id_snapshot BIGINT;
BEGIN
    IF TG_OP = 'INSERT' AND NEW.allocation_ref_id IS NOT NULL THEN
        SELECT organization_id, user_id_snapshot
        INTO allocation_organization_id, allocation_user_id_snapshot
        FROM organization_allocations WHERE id = NEW.allocation_ref_id;
        IF NOT FOUND
           OR allocation_organization_id <> NEW.organization_id
           OR allocation_user_id_snapshot <> NEW.user_id_snapshot THEN
            RAISE EXCEPTION 'organization execution allocation identity is inconsistent'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_reservation_integrity()
RETURNS TRIGGER AS $$
DECLARE
    execution_uuid UUID;
    execution_organization_id BIGINT;
    execution_allocation_id BIGINT;
    execution_billing_fingerprint CHAR(64);
    execution_candidate_snapshot JSONB;
    execution_candidate_set_hash CHAR(64);
    execution_pricing_snapshot JSONB;
    execution_pricing_snapshot_hash CHAR(64);
    allocation_organization_id BIGINT;
BEGIN
    SELECT execution_id, organization_id, allocation_id_snapshot, billing_fingerprint,
           candidate_snapshot, candidate_set_hash, pricing_snapshot, pricing_snapshot_hash
    INTO execution_uuid, execution_organization_id, execution_allocation_id,
         execution_billing_fingerprint, execution_candidate_snapshot,
         execution_candidate_set_hash, execution_pricing_snapshot,
         execution_pricing_snapshot_hash
    FROM organization_executions WHERE id = NEW.execution_ref_id;
    IF NOT FOUND
       OR execution_uuid <> NEW.execution_id_snapshot
       OR execution_organization_id <> NEW.organization_id
       OR execution_allocation_id IS DISTINCT FROM NEW.allocation_id_snapshot
       OR execution_billing_fingerprint <> NEW.billing_fingerprint
       OR execution_candidate_snapshot IS DISTINCT FROM NEW.candidate_snapshot
       OR execution_candidate_set_hash <> NEW.candidate_set_hash
       OR execution_pricing_snapshot IS DISTINCT FROM NEW.pricing_snapshot
       OR execution_pricing_snapshot_hash <> NEW.pricing_snapshot_hash THEN
        RAISE EXCEPTION 'organization reservation execution identity is inconsistent'
            USING ERRCODE = '23514';
    END IF;

    IF TG_OP = 'INSERT' AND NEW.allocation_ref_id IS NOT NULL THEN
        SELECT organization_id INTO allocation_organization_id
        FROM organization_allocations WHERE id = NEW.allocation_ref_id;
        IF NOT FOUND OR allocation_organization_id <> NEW.organization_id THEN
            RAISE EXCEPTION 'organization reservation allocation reference is inconsistent'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_attempt_integrity()
RETURNS TRIGGER AS $$
DECLARE
    execution_uuid UUID;
    execution_organization_id BIGINT;
    execution_pricing_snapshot_hash CHAR(64);
BEGIN
    SELECT execution_id, organization_id, pricing_snapshot_hash
    INTO execution_uuid, execution_organization_id,
         execution_pricing_snapshot_hash
    FROM organization_executions WHERE id = NEW.execution_ref_id;
    IF NOT FOUND
       OR execution_uuid <> NEW.execution_id_snapshot
       OR execution_organization_id <> NEW.organization_id
       OR execution_pricing_snapshot_hash <> NEW.pricing_snapshot_hash THEN
        RAISE EXCEPTION 'organization attempt execution identity is inconsistent'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_attempt_evidence_integrity()
RETURNS TRIGGER AS $$
DECLARE
    attempt_uuid UUID;
    attempt_execution_uuid UUID;
    attempt_execution_ref_id BIGINT;
    attempt_organization_id BIGINT;
    attempt_account_id BIGINT;
    attempt_route_snapshot_hash CHAR(64);
    attempt_pricing_snapshot_hash CHAR(64);
    attempt_provider_correlation_id VARCHAR(255);
    attempt_provider_request_id VARCHAR(255);
BEGIN
    SELECT attempt_id, execution_id_snapshot, execution_ref_id, organization_id, account_id_snapshot,
           route_snapshot_hash, pricing_snapshot_hash, provider_correlation_id, provider_request_id
    INTO attempt_uuid, attempt_execution_uuid, attempt_execution_ref_id,
         attempt_organization_id, attempt_account_id, attempt_route_snapshot_hash,
         attempt_pricing_snapshot_hash, attempt_provider_correlation_id,
         attempt_provider_request_id
    FROM organization_execution_attempts WHERE id = NEW.attempt_ref_id;
    IF NOT FOUND
       OR attempt_uuid <> NEW.attempt_id_snapshot
       OR attempt_execution_uuid <> NEW.execution_id_snapshot
       OR attempt_execution_ref_id <> NEW.execution_ref_id
       OR attempt_organization_id <> NEW.organization_id
       OR attempt_account_id IS DISTINCT FROM NEW.account_id_snapshot
       OR attempt_route_snapshot_hash <> NEW.route_snapshot_hash
       OR attempt_pricing_snapshot_hash <> NEW.pricing_snapshot_hash
       OR attempt_provider_correlation_id <> NEW.provider_correlation_id
       OR attempt_provider_request_id <> NEW.provider_request_id THEN
        RAISE EXCEPTION 'organization attempt evidence identity is inconsistent'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_final_evidence_integrity()
RETURNS TRIGGER AS $$
DECLARE
    execution_uuid UUID;
    execution_organization_id BIGINT;
    execution_allocation_id BIGINT;
    execution_user_id BIGINT;
    execution_api_key_id BIGINT;
    execution_endpoint_family VARCHAR(128);
    execution_request_shape VARCHAR(128);
    attempt_uuid UUID;
    attempt_execution_ref_id BIGINT;
    attempt_organization_id BIGINT;
    attempt_account_id BIGINT;
    attempt_evidence_attempt_ref_id BIGINT;
    attempt_evidence_execution_ref_id BIGINT;
    attempt_evidence_organization_id BIGINT;
    attempt_evidence_account_id BIGINT;
    attempt_evidence_input_tokens BIGINT;
    attempt_evidence_output_tokens BIGINT;
    attempt_evidence_cache_creation_tokens BIGINT;
    attempt_evidence_cache_read_tokens BIGINT;
    attempt_evidence_raw_actual_cost NUMERIC;
BEGIN
    SELECT execution_id, organization_id, allocation_id_snapshot, user_id_snapshot, api_key_id_snapshot,
           endpoint_family, request_shape
    INTO execution_uuid, execution_organization_id, execution_allocation_id, execution_user_id, execution_api_key_id,
         execution_endpoint_family, execution_request_shape
    FROM organization_executions WHERE id = NEW.execution_ref_id;
    IF NOT FOUND
       OR execution_uuid <> NEW.execution_id_snapshot
       OR execution_organization_id <> NEW.organization_id
       OR execution_allocation_id IS DISTINCT FROM NEW.allocation_id_snapshot
       OR execution_user_id <> NEW.user_id_snapshot
       OR execution_api_key_id <> NEW.api_key_id_snapshot
       OR execution_endpoint_family <> NEW.endpoint_family
       OR execution_request_shape <> NEW.request_shape THEN
        RAISE EXCEPTION 'organization final evidence execution identity is inconsistent'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.final_attempt_ref_id IS NOT NULL THEN
        SELECT attempt_id, execution_ref_id, organization_id, account_id_snapshot
        INTO attempt_uuid, attempt_execution_ref_id, attempt_organization_id, attempt_account_id
        FROM organization_execution_attempts WHERE id = NEW.final_attempt_ref_id;
        IF NOT FOUND
           OR attempt_uuid <> NEW.final_attempt_id_snapshot
           OR attempt_execution_ref_id <> NEW.execution_ref_id
           OR attempt_organization_id <> NEW.organization_id
           OR attempt_account_id IS DISTINCT FROM NEW.account_id_snapshot THEN
            RAISE EXCEPTION 'organization final evidence attempt identity is inconsistent'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    IF NEW.final_attempt_evidence_ref_id IS NOT NULL THEN
        SELECT attempt_ref_id, execution_ref_id, organization_id, account_id_snapshot,
               input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
               raw_actual_cost
        INTO attempt_evidence_attempt_ref_id, attempt_evidence_execution_ref_id,
             attempt_evidence_organization_id, attempt_evidence_account_id,
             attempt_evidence_input_tokens, attempt_evidence_output_tokens,
             attempt_evidence_cache_creation_tokens, attempt_evidence_cache_read_tokens,
             attempt_evidence_raw_actual_cost
        FROM organization_attempt_evidences WHERE id = NEW.final_attempt_evidence_ref_id;
        IF NOT FOUND
           OR NEW.final_attempt_evidence_id_snapshot <> NEW.final_attempt_evidence_ref_id
           OR NEW.final_attempt_ref_id IS NULL
           OR attempt_evidence_attempt_ref_id <> NEW.final_attempt_ref_id
           OR attempt_evidence_execution_ref_id <> NEW.execution_ref_id
           OR attempt_evidence_organization_id <> NEW.organization_id
           OR attempt_evidence_account_id IS DISTINCT FROM NEW.account_id_snapshot
           OR attempt_evidence_input_tokens <> NEW.input_tokens
           OR attempt_evidence_output_tokens <> NEW.output_tokens
           OR attempt_evidence_cache_creation_tokens <> NEW.cache_creation_tokens
           OR attempt_evidence_cache_read_tokens <> NEW.cache_read_tokens
           OR attempt_evidence_raw_actual_cost <> NEW.raw_actual_cost THEN
            RAISE EXCEPTION 'organization final evidence attempt evidence is inconsistent'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_execution_outbox_integrity()
RETURNS TRIGGER AS $$
DECLARE
    execution_uuid UUID;
    execution_organization_id BIGINT;
    evidence_execution_ref_id BIGINT;
    evidence_execution_uuid UUID;
    evidence_organization_id BIGINT;
BEGIN
    SELECT execution_id, organization_id
    INTO execution_uuid, execution_organization_id
    FROM organization_executions WHERE id = NEW.execution_ref_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'organization execution outbox execution is missing'
            USING ERRCODE = '23514';
    END IF;
    SELECT execution_ref_id, execution_id_snapshot, organization_id
    INTO evidence_execution_ref_id, evidence_execution_uuid, evidence_organization_id
    FROM organization_final_execution_evidences WHERE id = NEW.final_evidence_id_snapshot;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'organization execution outbox final evidence is missing'
            USING ERRCODE = '23514';
    END IF;
    IF execution_uuid <> NEW.execution_id_snapshot
       OR execution_organization_id <> NEW.organization_id
       OR evidence_execution_ref_id <> NEW.execution_ref_id
       OR evidence_execution_uuid <> NEW.execution_id_snapshot
       OR evidence_organization_id <> NEW.organization_id THEN
        RAISE EXCEPTION 'organization execution outbox identity is inconsistent'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_org_ledger_operation_execution_integrity()
RETURNS TRIGGER AS $$
DECLARE
    execution_uuid UUID;
    execution_organization_id BIGINT;
BEGIN
    IF NEW.authorization_scope <> 'system_execution' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND NEW.actor_execution_ref_id IS NULL THEN
        RAISE EXCEPTION 'system organization ledger operation requires a live execution reference'
            USING ERRCODE = '23514';
    END IF;
    SELECT execution_id, organization_id
    INTO execution_uuid, execution_organization_id
    FROM organization_executions
    WHERE execution_id = NEW.actor_execution_id_snapshot;
    IF NOT FOUND
       OR execution_organization_id <> NEW.organization_id
       OR (NEW.actor_execution_ref_id IS NOT NULL AND NEW.actor_execution_ref_id <> (
            SELECT id FROM organization_executions WHERE execution_id = NEW.actor_execution_id_snapshot
       )) THEN
        RAISE EXCEPTION 'system organization ledger execution identity is inconsistent'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Extend the prior posting integrity guard with its final execution/evidence
-- correlation. A positive member consumption is one reserved-bucket debit and
-- cannot be inferred from request IDs or current membership.
CREATE OR REPLACE FUNCTION guard_org_ledger_posting_integrity()
RETURNS TRIGGER AS $$
DECLARE
    operation_organization_id BIGINT;
    operation_uuid UUID;
    operation_currency VARCHAR(3);
    operation_occurred_at TIMESTAMPTZ;
    operation_type_value VARCHAR(64);
    operation_execution_uuid UUID;
    allocation_organization_id BIGINT;
    allocation_user_id_snapshot BIGINT;
    execution_uuid UUID;
    execution_organization_id BIGINT;
    evidence_execution_ref_id BIGINT;
    evidence_execution_uuid UUID;
    evidence_organization_id BIGINT;
    evidence_allocation_id BIGINT;
    evidence_settled_cost DECIMAL(20,8);
    evidence_occurred_at TIMESTAMPTZ;
BEGIN
    SELECT operation.organization_id, operation.operation_id, operation.currency,
           operation.occurred_at, operation.operation_type, operation.actor_execution_id_snapshot
    INTO operation_organization_id, operation_uuid, operation_currency,
         operation_occurred_at, operation_type_value, operation_execution_uuid
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
           OR (NEW.target_user_ref_id IS NULL AND operation_execution_uuid IS NULL) THEN
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
           OR NEW.target_user_id_snapshot <> allocation_user_id_snapshot
           OR (NEW.target_user_ref_id IS NOT NULL AND NEW.target_user_id_snapshot <> NEW.target_user_ref_id) THEN
            RAISE EXCEPTION 'organization ledger allocation reference is inconsistent'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    IF NEW.final_evidence_id_snapshot IS NOT NULL THEN
        SELECT execution_ref_id, execution_id_snapshot, organization_id,
               allocation_id_snapshot, organization_settled_cost, occurred_at
        INTO evidence_execution_ref_id, evidence_execution_uuid, evidence_organization_id,
             evidence_allocation_id, evidence_settled_cost, evidence_occurred_at
        FROM organization_final_execution_evidences
        WHERE id = NEW.final_evidence_id_snapshot;
        IF NOT FOUND
           OR operation_type_value <> 'consumption'
           OR operation_execution_uuid IS NULL
           OR NEW.execution_id_snapshot IS NULL
           OR NEW.execution_ref_id IS NULL
           OR (TG_OP = 'INSERT' AND NEW.final_evidence_ref_id IS NULL)
           OR (NEW.final_evidence_ref_id IS NOT NULL AND NEW.final_evidence_ref_id <> NEW.final_evidence_id_snapshot)
           OR NEW.execution_ref_id <> evidence_execution_ref_id
           OR NEW.execution_id_snapshot <> evidence_execution_uuid
           OR NEW.execution_id_snapshot <> operation_execution_uuid
           OR NEW.organization_id <> evidence_organization_id
           OR NEW.allocation_id_snapshot IS DISTINCT FROM evidence_allocation_id
           OR NEW.bucket_type <> 'allocation_reserved'
           OR NEW.delta_amount <> -evidence_settled_cost
           OR NEW.occurred_at <> evidence_occurred_at
           OR evidence_settled_cost <= 0 THEN
            RAISE EXCEPTION 'organization consumption posting evidence is inconsistent'
                USING ERRCODE = '23514';
        END IF;
    ELSIF operation_type_value = 'consumption' THEN
        RAISE EXCEPTION 'organization consumption posting requires final evidence'
            USING ERRCODE = '23514';
    END IF;

    IF TG_OP = 'INSERT' AND NEW.execution_ref_id IS NOT NULL THEN
        SELECT execution_id, organization_id
        INTO execution_uuid, execution_organization_id
        FROM organization_executions WHERE id = NEW.execution_ref_id;
        IF NOT FOUND
           OR NEW.execution_id_snapshot <> execution_uuid
           OR NEW.organization_id <> execution_organization_id THEN
            RAISE EXCEPTION 'organization ledger execution reference is inconsistent'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- All evidence fields are immutable. The only accepted updates are terminal
-- state transitions or FK-driven live-reference nulling; snapshots never move.
CREATE OR REPLACE FUNCTION guard_organization_execution_immutable()
RETURNS TRIGGER AS $$
DECLARE
    refs_allowed BOOLEAN;
    state_allowed BOOLEAN;
    terminal_allowed BOOLEAN;
BEGIN
    refs_allowed :=
        NEW.allocation_ref_id IS NOT DISTINCT FROM OLD.allocation_ref_id
        AND (NEW.user_ref_id IS NOT DISTINCT FROM OLD.user_ref_id OR (OLD.user_ref_id IS NOT NULL AND NEW.user_ref_id IS NULL))
        AND (NEW.api_key_ref_id IS NOT DISTINCT FROM OLD.api_key_ref_id OR (OLD.api_key_ref_id IS NOT NULL AND NEW.api_key_ref_id IS NULL));
    state_allowed := NEW.status = OLD.status OR (
        (OLD.status = 'accepted' AND NEW.status IN ('reserved', 'settled', 'released'))
        OR (OLD.status = 'reserved' AND NEW.status IN ('dispatched', 'pending_reconciliation', 'settled', 'released'))
        OR (OLD.status = 'dispatched' AND NEW.status IN ('final_usage_observed', 'pending_reconciliation', 'settled', 'released'))
        OR (OLD.status = 'final_usage_observed' AND NEW.status IN ('pending_reconciliation', 'settled', 'released'))
        OR (OLD.status = 'pending_reconciliation' AND NEW.status IN ('settled', 'released'))
    );
    terminal_allowed := NEW.terminalized_at IS NOT DISTINCT FROM OLD.terminalized_at
        OR (
            OLD.terminalized_at IS NULL
            AND NEW.terminalized_at IS NOT NULL
            AND NEW.terminalized_at >= OLD.accepted_at
            AND NEW.status IN ('settled', 'released')
        );
    IF TG_OP = 'UPDATE' AND refs_allowed AND state_allowed
       AND terminal_allowed
       AND (to_jsonb(NEW) - ARRAY['allocation_ref_id', 'user_ref_id', 'api_key_ref_id', 'status', 'terminalized_at', 'updated_at'])
           = (to_jsonb(OLD) - ARRAY['allocation_ref_id', 'user_ref_id', 'api_key_ref_id', 'status', 'terminalized_at', 'updated_at']) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'organization executions are immutable outside terminal state'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_reservation_immutable()
RETURNS TRIGGER AS $$
DECLARE
    ref_allowed BOOLEAN;
    state_allowed BOOLEAN;
    terminal_allowed BOOLEAN;
    amounts_allowed BOOLEAN;
BEGIN
    ref_allowed := NEW.allocation_ref_id IS NOT DISTINCT FROM OLD.allocation_ref_id;
    state_allowed := NEW.status = OLD.status OR (
        OLD.status = 'reserved' AND NEW.status IN ('pending_reconciliation', 'settled', 'released')
    ) OR (
        OLD.status = 'pending_reconciliation' AND NEW.status IN ('settled', 'released')
    );
    terminal_allowed := NEW.terminalized_at IS NOT DISTINCT FROM OLD.terminalized_at
        OR (
            OLD.terminalized_at IS NULL
            AND NEW.terminalized_at IS NOT NULL
            AND NEW.status IN ('settled', 'released')
        );
    amounts_allowed := (
        NEW.settled_amount IS NOT DISTINCT FROM OLD.settled_amount
        AND NEW.released_amount IS NOT DISTINCT FROM OLD.released_amount
    ) OR (
        OLD.status IN ('reserved', 'pending_reconciliation')
        AND NEW.status IN ('settled', 'released')
    );
    IF TG_OP = 'UPDATE' AND ref_allowed AND state_allowed
       AND terminal_allowed
       AND amounts_allowed
       AND (to_jsonb(NEW) - ARRAY['allocation_ref_id', 'status', 'settled_amount', 'released_amount', 'terminalized_at', 'updated_at'])
           = (to_jsonb(OLD) - ARRAY['allocation_ref_id', 'status', 'settled_amount', 'released_amount', 'terminalized_at', 'updated_at']) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'organization reservations are immutable outside terminal state'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_attempt_immutable()
RETURNS TRIGGER AS $$
DECLARE
    ref_allowed BOOLEAN;
    state_allowed BOOLEAN;
    dispatched_at_allowed BOOLEAN;
BEGIN
    ref_allowed := NEW.account_ref_id IS NOT DISTINCT FROM OLD.account_ref_id
        OR (OLD.account_ref_id IS NOT NULL AND NEW.account_ref_id IS NULL);
    state_allowed := NEW.status = OLD.status OR
        (OLD.status = 'created' AND NEW.status = 'dispatched') OR
        (OLD.status = 'dispatched' AND NEW.status = 'final_usage_observed');
    dispatched_at_allowed := NEW.dispatched_at IS NOT DISTINCT FROM OLD.dispatched_at
        OR (
            OLD.status = 'created'
            AND NEW.status = 'dispatched'
            AND OLD.dispatched_at IS NULL
            AND NEW.dispatched_at IS NOT NULL
        );
    IF TG_OP = 'UPDATE' AND ref_allowed AND state_allowed
       AND dispatched_at_allowed
       AND (to_jsonb(NEW) - ARRAY['account_ref_id', 'status', 'dispatched_at', 'updated_at'])
           = (to_jsonb(OLD) - ARRAY['account_ref_id', 'status', 'dispatched_at', 'updated_at']) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'organization execution attempts are immutable outside dispatch state'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_attempt_evidence_immutable()
RETURNS TRIGGER AS $$
DECLARE
    ref_allowed BOOLEAN;
BEGIN
    ref_allowed := NEW.account_ref_id IS NOT DISTINCT FROM OLD.account_ref_id
        OR (OLD.account_ref_id IS NOT NULL AND NEW.account_ref_id IS NULL);
    IF TG_OP = 'UPDATE' AND ref_allowed
       AND (to_jsonb(NEW) - 'account_ref_id') = (to_jsonb(OLD) - 'account_ref_id') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'organization attempt evidences are immutable'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_final_evidence_immutable()
RETURNS TRIGGER AS $$
DECLARE
    refs_allowed BOOLEAN;
BEGIN
    refs_allowed :=
        NEW.allocation_ref_id IS NOT DISTINCT FROM OLD.allocation_ref_id
        AND (NEW.user_ref_id IS NOT DISTINCT FROM OLD.user_ref_id OR (OLD.user_ref_id IS NOT NULL AND NEW.user_ref_id IS NULL))
        AND (NEW.api_key_ref_id IS NOT DISTINCT FROM OLD.api_key_ref_id OR (OLD.api_key_ref_id IS NOT NULL AND NEW.api_key_ref_id IS NULL))
        AND (NEW.final_attempt_ref_id IS NOT DISTINCT FROM OLD.final_attempt_ref_id OR (OLD.final_attempt_ref_id IS NOT NULL AND NEW.final_attempt_ref_id IS NULL))
        AND (NEW.final_attempt_evidence_ref_id IS NOT DISTINCT FROM OLD.final_attempt_evidence_ref_id OR (OLD.final_attempt_evidence_ref_id IS NOT NULL AND NEW.final_attempt_evidence_ref_id IS NULL))
        AND (NEW.account_ref_id IS NOT DISTINCT FROM OLD.account_ref_id OR (OLD.account_ref_id IS NOT NULL AND NEW.account_ref_id IS NULL));
    IF TG_OP = 'UPDATE' AND refs_allowed
       AND (to_jsonb(NEW) - ARRAY['allocation_ref_id', 'user_ref_id', 'api_key_ref_id', 'final_attempt_ref_id', 'final_attempt_evidence_ref_id', 'account_ref_id'])
           = (to_jsonb(OLD) - ARRAY['allocation_ref_id', 'user_ref_id', 'api_key_ref_id', 'final_attempt_ref_id', 'final_attempt_evidence_ref_id', 'account_ref_id']) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'organization final execution evidences are immutable'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_execution_outbox_immutable()
RETURNS TRIGGER AS $$
DECLARE
    ref_allowed BOOLEAN;
    applied_at_allowed BOOLEAN;
BEGIN
    ref_allowed := NEW.final_evidence_ref_id IS NOT DISTINCT FROM OLD.final_evidence_ref_id
        OR (OLD.final_evidence_ref_id IS NOT NULL AND NEW.final_evidence_ref_id IS NULL);
    applied_at_allowed := NEW.applied_at IS NOT DISTINCT FROM OLD.applied_at
        OR (
            OLD.status = 'pending'
            AND NEW.status IN ('applied', 'applied_noop_deleted_target', 'operator_attention')
            AND OLD.applied_at IS NULL
        );
    IF TG_OP = 'UPDATE' AND ref_allowed
       AND (NEW.status = OLD.status OR (OLD.status = 'pending' AND NEW.status IN ('applied', 'applied_noop_deleted_target', 'operator_attention')))
       AND applied_at_allowed
       AND (to_jsonb(NEW) - ARRAY['final_evidence_ref_id', 'status', 'applied_at', 'updated_at'])
           = (to_jsonb(OLD) - ARRAY['final_evidence_ref_id', 'status', 'applied_at', 'updated_at']) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'organization execution outboxes are immutable outside apply state'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_org_ledger_operation_immutable()
RETURNS TRIGGER AS $$
DECLARE
    actor_ref_allowed BOOLEAN;
    execution_ref_allowed BOOLEAN;
BEGIN
    actor_ref_allowed := NEW.actor_ref_id IS NOT DISTINCT FROM OLD.actor_ref_id
        OR (OLD.actor_ref_id IS NOT NULL AND NEW.actor_ref_id IS NULL);
    execution_ref_allowed := NEW.actor_execution_ref_id IS NOT DISTINCT FROM OLD.actor_execution_ref_id
        OR (OLD.actor_execution_ref_id IS NOT NULL AND NEW.actor_execution_ref_id IS NULL);
    IF TG_OP = 'UPDATE' AND actor_ref_allowed AND execution_ref_allowed
       AND (to_jsonb(NEW) - ARRAY['actor_ref_id', 'actor_execution_ref_id'])
           = (to_jsonb(OLD) - ARRAY['actor_ref_id', 'actor_execution_ref_id']) THEN
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
    execution_ref_allowed BOOLEAN;
    final_evidence_ref_allowed BOOLEAN;
BEGIN
    allocation_ref_allowed := NEW.allocation_ref_id IS NOT DISTINCT FROM OLD.allocation_ref_id
        OR (OLD.allocation_ref_id IS NOT NULL AND NEW.allocation_ref_id IS NULL);
    target_user_ref_allowed := NEW.target_user_ref_id IS NOT DISTINCT FROM OLD.target_user_ref_id
        OR (OLD.target_user_ref_id IS NOT NULL AND NEW.target_user_ref_id IS NULL);
    execution_ref_allowed := NEW.execution_ref_id IS NOT DISTINCT FROM OLD.execution_ref_id
        OR (OLD.execution_ref_id IS NOT NULL AND NEW.execution_ref_id IS NULL);
    final_evidence_ref_allowed := NEW.final_evidence_ref_id IS NOT DISTINCT FROM OLD.final_evidence_ref_id
        OR (OLD.final_evidence_ref_id IS NOT NULL AND NEW.final_evidence_ref_id IS NULL);
    IF TG_OP = 'UPDATE'
       AND allocation_ref_allowed AND target_user_ref_allowed AND execution_ref_allowed AND final_evidence_ref_allowed
       AND (to_jsonb(NEW) - ARRAY['allocation_ref_id', 'target_user_ref_id', 'execution_ref_id', 'final_evidence_ref_id'])
           = (to_jsonb(OLD) - ARRAY['allocation_ref_id', 'target_user_ref_id', 'execution_ref_id', 'final_evidence_ref_id']) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'organization ledger postings are immutable'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

-- Deferred one-to-one reconciliation makes a positive final evidence row and
-- its consumption posting atomic at transaction commit. Zero-cost evidence is
-- deliberately forbidden from creating a consumption posting.
CREATE OR REPLACE FUNCTION enforce_organization_final_evidence_consumption()
RETURNS TRIGGER AS $$
DECLARE
    evidence_id_value BIGINT;
    evidence_execution_ref_id BIGINT;
    settled_cost_value DECIMAL(20,8);
    evidence_attempt_id BIGINT;
    evidence_zero_proof JSONB;
    execution_status_value VARCHAR(32);
    reservation_exists BOOLEAN;
    reservation_status_value VARCHAR(32);
    reservation_reserved_amount DECIMAL(20,8);
    reservation_settled_amount DECIMAL(20,8);
    reservation_released_amount DECIMAL(20,8);
    posting_count INTEGER;
BEGIN
    IF TG_TABLE_NAME = 'organization_final_execution_evidences' THEN
        evidence_id_value := COALESCE(NEW.id, OLD.id);
    ELSE
        evidence_id_value := COALESCE(NEW.final_evidence_id_snapshot, OLD.final_evidence_id_snapshot);
    END IF;
    IF evidence_id_value IS NULL THEN
        RETURN NULL;
    END IF;
    SELECT execution_ref_id, organization_settled_cost,
           final_attempt_evidence_id_snapshot, zero_cost_proof
    INTO evidence_execution_ref_id, settled_cost_value,
         evidence_attempt_id, evidence_zero_proof
    FROM organization_final_execution_evidences WHERE id = evidence_id_value;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;

    SELECT status INTO execution_status_value
    FROM organization_executions WHERE id = evidence_execution_ref_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'organization final evidence execution is missing at terminal reconciliation'
            USING ERRCODE = '23514';
    END IF;

    SELECT status, reserved_amount, settled_amount, released_amount
    INTO reservation_status_value, reservation_reserved_amount,
         reservation_settled_amount, reservation_released_amount
    FROM organization_reservations WHERE execution_ref_id = evidence_execution_ref_id;
    reservation_exists := FOUND;

    SELECT COUNT(*) INTO posting_count
    FROM organization_ledger_postings
    WHERE final_evidence_id_snapshot = evidence_id_value;
    IF (settled_cost_value > 0 AND posting_count <> 1)
       OR (settled_cost_value = 0 AND posting_count <> 0) THEN
        RAISE EXCEPTION 'organization final evidence consumption reconciliation failed'
            USING ERRCODE = '23514';
    END IF;

    IF settled_cost_value > 0 THEN
        IF NOT reservation_exists
           OR reservation_status_value <> 'settled'
           OR reservation_settled_amount <> settled_cost_value
           OR reservation_settled_amount + reservation_released_amount <> reservation_reserved_amount
           OR execution_status_value <> 'settled' THEN
            RAISE EXCEPTION 'organization positive final evidence is inconsistent with terminal reservation state'
                USING ERRCODE = '23514';
        END IF;
    ELSIF reservation_exists THEN
        IF reservation_status_value <> 'released'
           OR reservation_settled_amount <> 0
           OR reservation_released_amount <> reservation_reserved_amount
           OR execution_status_value <> 'released' THEN
            RAISE EXCEPTION 'organization zero-cost final evidence is inconsistent with terminal reservation state'
                USING ERRCODE = '23514';
        END IF;
    ELSIF execution_status_value <> 'released'
       OR evidence_attempt_id IS NOT NULL
       OR evidence_zero_proof IS NULL THEN
        RAISE EXCEPTION 'organization reservation-free final evidence is not a proven-zero terminal execution'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS organization_executions_integrity ON organization_executions;
CREATE TRIGGER organization_executions_integrity
    BEFORE INSERT OR UPDATE ON organization_executions
    FOR EACH ROW EXECUTE FUNCTION guard_organization_execution_integrity();
DROP TRIGGER IF EXISTS organization_executions_immutable ON organization_executions;
CREATE TRIGGER organization_executions_immutable
    BEFORE UPDATE OR DELETE ON organization_executions
    FOR EACH ROW EXECUTE FUNCTION guard_organization_execution_immutable();

DROP TRIGGER IF EXISTS organization_reservations_integrity ON organization_reservations;
CREATE TRIGGER organization_reservations_integrity
    BEFORE INSERT OR UPDATE ON organization_reservations
    FOR EACH ROW EXECUTE FUNCTION guard_organization_reservation_integrity();
DROP TRIGGER IF EXISTS organization_reservations_immutable ON organization_reservations;
CREATE TRIGGER organization_reservations_immutable
    BEFORE UPDATE OR DELETE ON organization_reservations
    FOR EACH ROW EXECUTE FUNCTION guard_organization_reservation_immutable();

DROP TRIGGER IF EXISTS organization_execution_attempts_integrity ON organization_execution_attempts;
CREATE TRIGGER organization_execution_attempts_integrity
    BEFORE INSERT OR UPDATE ON organization_execution_attempts
    FOR EACH ROW EXECUTE FUNCTION guard_organization_attempt_integrity();
DROP TRIGGER IF EXISTS organization_execution_attempts_immutable ON organization_execution_attempts;
CREATE TRIGGER organization_execution_attempts_immutable
    BEFORE UPDATE OR DELETE ON organization_execution_attempts
    FOR EACH ROW EXECUTE FUNCTION guard_organization_attempt_immutable();

DROP TRIGGER IF EXISTS organization_attempt_evidences_integrity ON organization_attempt_evidences;
CREATE TRIGGER organization_attempt_evidences_integrity
    BEFORE INSERT OR UPDATE ON organization_attempt_evidences
    FOR EACH ROW EXECUTE FUNCTION guard_organization_attempt_evidence_integrity();
DROP TRIGGER IF EXISTS organization_attempt_evidences_immutable ON organization_attempt_evidences;
CREATE TRIGGER organization_attempt_evidences_immutable
    BEFORE UPDATE OR DELETE ON organization_attempt_evidences
    FOR EACH ROW EXECUTE FUNCTION guard_organization_attempt_evidence_immutable();

DROP TRIGGER IF EXISTS organization_final_execution_evidences_integrity ON organization_final_execution_evidences;
CREATE TRIGGER organization_final_execution_evidences_integrity
    BEFORE INSERT OR UPDATE ON organization_final_execution_evidences
    FOR EACH ROW EXECUTE FUNCTION guard_organization_final_evidence_integrity();
DROP TRIGGER IF EXISTS organization_final_execution_evidences_immutable ON organization_final_execution_evidences;
CREATE TRIGGER organization_final_execution_evidences_immutable
    BEFORE UPDATE OR DELETE ON organization_final_execution_evidences
    FOR EACH ROW EXECUTE FUNCTION guard_organization_final_evidence_immutable();

DROP TRIGGER IF EXISTS organization_execution_outboxes_integrity ON organization_execution_outboxes;
CREATE TRIGGER organization_execution_outboxes_integrity
    BEFORE INSERT OR UPDATE ON organization_execution_outboxes
    FOR EACH ROW EXECUTE FUNCTION guard_organization_execution_outbox_integrity();
DROP TRIGGER IF EXISTS organization_execution_outboxes_immutable ON organization_execution_outboxes;
CREATE TRIGGER organization_execution_outboxes_immutable
    BEFORE UPDATE OR DELETE ON organization_execution_outboxes
    FOR EACH ROW EXECUTE FUNCTION guard_organization_execution_outbox_immutable();

DROP TRIGGER IF EXISTS organization_ledger_operations_immutable ON organization_ledger_operations;
CREATE TRIGGER organization_ledger_operations_immutable
    BEFORE UPDATE OR DELETE ON organization_ledger_operations
    FOR EACH ROW EXECUTE FUNCTION guard_org_ledger_operation_immutable();
DROP TRIGGER IF EXISTS organization_ledger_operations_execution_integrity ON organization_ledger_operations;
CREATE TRIGGER organization_ledger_operations_execution_integrity
    BEFORE INSERT OR UPDATE ON organization_ledger_operations
    FOR EACH ROW EXECUTE FUNCTION guard_org_ledger_operation_execution_integrity();
DROP TRIGGER IF EXISTS organization_ledger_postings_integrity ON organization_ledger_postings;
CREATE TRIGGER organization_ledger_postings_integrity
    BEFORE INSERT OR UPDATE ON organization_ledger_postings
    FOR EACH ROW EXECUTE FUNCTION guard_org_ledger_posting_integrity();
DROP TRIGGER IF EXISTS organization_ledger_postings_immutable ON organization_ledger_postings;
CREATE TRIGGER organization_ledger_postings_immutable
    BEFORE UPDATE OR DELETE ON organization_ledger_postings
    FOR EACH ROW EXECUTE FUNCTION guard_org_ledger_posting_immutable();

DROP TRIGGER IF EXISTS organization_final_execution_evidences_consumption_check
    ON organization_final_execution_evidences;
CREATE CONSTRAINT TRIGGER organization_final_execution_evidences_consumption_check
    AFTER INSERT OR UPDATE OR DELETE ON organization_final_execution_evidences
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION enforce_organization_final_evidence_consumption();
DROP TRIGGER IF EXISTS organization_ledger_postings_consumption_check ON organization_ledger_postings;
CREATE CONSTRAINT TRIGGER organization_ledger_postings_consumption_check
    AFTER INSERT OR UPDATE OR DELETE ON organization_ledger_postings
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION enforce_organization_final_evidence_consumption();
