-- Ticket 10: multi-attempt evidence, Account cost side effects, provenance
-- incidents, and durable recovery. This is an additive forward migration; the
-- already-applied execution core migration remains immutable.

ALTER TABLE organization_execution_attempts
    DROP CONSTRAINT IF EXISTS organization_execution_attempts_sequence_check;
ALTER TABLE organization_execution_attempts
    ADD CONSTRAINT organization_execution_attempts_sequence_check
    CHECK (attempt_sequence > 0);

ALTER TABLE organization_attempt_evidences
    DROP CONSTRAINT IF EXISTS organization_attempt_evidences_attempt_ref_unique,
    DROP CONSTRAINT IF EXISTS organization_attempt_evidences_attempt_snapshot_unique,
    DROP CONSTRAINT IF EXISTS organization_attempt_evidences_attempt_provenance_unique,
    DROP CONSTRAINT IF EXISTS organization_attempt_evidences_source_check;

ALTER TABLE organization_attempt_evidences
    ADD COLUMN IF NOT EXISTS terminal_outcome VARCHAR(16) NOT NULL DEFAULT 'unknown';

ALTER TABLE organization_attempt_evidences
    ADD CONSTRAINT organization_attempt_evidences_source_check
        CHECK (evidence_source IN ('fake_adapter', 'adapter_direct', 'provider_query')),
    ADD CONSTRAINT organization_attempt_evidences_terminal_outcome_check
        CHECK (terminal_outcome IN ('success', 'error', 'unknown'));

ALTER TABLE organization_execution_outboxes
    ALTER COLUMN status TYPE VARCHAR(32);

CREATE UNIQUE INDEX IF NOT EXISTS organization_attempt_evidences_source_provenance_unique
    ON organization_attempt_evidences (attempt_id_snapshot, evidence_source, provenance_hash);
CREATE INDEX IF NOT EXISTS organization_attempt_evidences_attempt_idx
    ON organization_attempt_evidences (attempt_id_snapshot, ingested_at, id);

CREATE TABLE IF NOT EXISTS organization_attempt_account_postings (
    id BIGSERIAL PRIMARY KEY,
    attempt_id_snapshot UUID NOT NULL,
    attempt_ref_id BIGINT NOT NULL REFERENCES organization_execution_attempts(id) ON DELETE RESTRICT,
    evidence_id_snapshot BIGINT NOT NULL,
    evidence_ref_id BIGINT NULL REFERENCES organization_attempt_evidences(id) ON DELETE SET NULL,
    execution_id_snapshot UUID NOT NULL,
    execution_ref_id BIGINT NOT NULL REFERENCES organization_executions(id) ON DELETE RESTRICT,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    account_id_snapshot BIGINT NOT NULL,
    account_ref_id BIGINT NULL REFERENCES accounts(id) ON DELETE SET NULL,
    raw_actual_cost NUMERIC NOT NULL,
    reservation_ceiling DECIMAL(20,8) NOT NULL,
    platform_overage_cost NUMERIC NOT NULL DEFAULT 0,
    classification VARCHAR(32) NOT NULL DEFAULT 'normal',
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT organization_attempt_account_postings_attempt_unique UNIQUE (attempt_ref_id),
    CONSTRAINT organization_attempt_account_postings_attempt_snapshot_unique UNIQUE (attempt_id_snapshot),
    CONSTRAINT organization_attempt_account_postings_evidence_snapshot_unique UNIQUE (evidence_id_snapshot),
    CONSTRAINT organization_attempt_account_postings_amount_check
        CHECK (raw_actual_cost >= 0 AND reservation_ceiling >= 0 AND platform_overage_cost >= 0),
    CONSTRAINT organization_attempt_account_postings_classification_check
        CHECK (classification IN ('normal', 'platform_overage')),
    CONSTRAINT organization_attempt_account_postings_evidence_ref_check
        CHECK (evidence_ref_id IS NULL OR evidence_ref_id = evidence_id_snapshot),
    CONSTRAINT organization_attempt_account_postings_account_ref_check
        CHECK (account_ref_id IS NULL OR account_ref_id = account_id_snapshot)
);

CREATE INDEX IF NOT EXISTS organization_attempt_account_postings_org_occurred_idx
    ON organization_attempt_account_postings (organization_id, occurred_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS organization_attempt_account_effects (
    id BIGSERIAL PRIMARY KEY,
    attempt_id_snapshot UUID NOT NULL,
    attempt_ref_id BIGINT NOT NULL REFERENCES organization_execution_attempts(id) ON DELETE RESTRICT,
    execution_id_snapshot UUID NOT NULL,
    execution_ref_id BIGINT NOT NULL REFERENCES organization_executions(id) ON DELETE RESTRICT,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    account_id_snapshot BIGINT NOT NULL,
    account_ref_id BIGINT NULL REFERENCES accounts(id) ON DELETE SET NULL,
    selected_evidence_id_snapshot BIGINT NULL,
    selected_evidence_ref_id BIGINT NULL REFERENCES organization_attempt_evidences(id) ON DELETE SET NULL,
    posting_id_snapshot BIGINT NULL,
    posting_ref_id BIGINT NULL REFERENCES organization_attempt_account_postings(id) ON DELETE SET NULL,
    raw_actual_cost NUMERIC NULL,
    platform_overage_cost NUMERIC NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    audit_reason VARCHAR(2000) NULL,
    applied_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT organization_attempt_account_effects_attempt_unique UNIQUE (attempt_ref_id),
    CONSTRAINT organization_attempt_account_effects_attempt_snapshot_unique UNIQUE (attempt_id_snapshot),
    CONSTRAINT organization_attempt_account_effects_status_check
        CHECK (status IN ('pending', 'applied', 'applied_noop_deleted_target', 'operator_attention')),
    CONSTRAINT organization_attempt_account_effects_amount_check
        CHECK (
            (raw_actual_cost IS NULL AND platform_overage_cost IS NULL)
            OR (raw_actual_cost >= 0 AND platform_overage_cost >= 0)
        ),
    CONSTRAINT organization_attempt_account_effects_account_ref_check
        CHECK (account_ref_id IS NULL OR account_ref_id = account_id_snapshot),
    CONSTRAINT organization_attempt_account_effects_evidence_ref_check
        CHECK (selected_evidence_ref_id IS NULL OR selected_evidence_ref_id = selected_evidence_id_snapshot),
    CONSTRAINT organization_attempt_account_effects_posting_ref_check
        CHECK (posting_ref_id IS NULL OR posting_ref_id = posting_id_snapshot),
    CONSTRAINT organization_attempt_account_effects_terminal_check
        CHECK (
            (status = 'pending' AND applied_at IS NULL AND posting_id_snapshot IS NULL)
            OR (
                status IN ('applied', 'applied_noop_deleted_target')
                AND applied_at IS NOT NULL
                AND selected_evidence_id_snapshot IS NOT NULL
                AND posting_id_snapshot IS NOT NULL
                AND raw_actual_cost IS NOT NULL
                AND platform_overage_cost IS NOT NULL
            )
            OR (
                status = 'operator_attention'
                AND applied_at IS NOT NULL
                AND posting_id_snapshot IS NULL
                AND audit_reason IS NOT NULL
            )
        )
);

CREATE INDEX IF NOT EXISTS organization_attempt_account_effects_pending_idx
    ON organization_attempt_account_effects (created_at, id)
    WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS organization_execution_incidents (
    id BIGSERIAL PRIMARY KEY,
    execution_id_snapshot UUID NOT NULL,
    execution_ref_id BIGINT NOT NULL REFERENCES organization_executions(id) ON DELETE RESTRICT,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    attempt_id_snapshot UUID NULL,
    attempt_ref_id BIGINT NULL REFERENCES organization_execution_attempts(id) ON DELETE SET NULL,
    incident_type VARCHAR(64) NOT NULL,
    public_reason VARCHAR(255) NOT NULL,
    internal_reason VARCHAR(2000) NOT NULL,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(16) NOT NULL DEFAULT 'open',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT organization_execution_incidents_identity_unique
        UNIQUE (execution_ref_id, incident_type, attempt_id_snapshot),
    CONSTRAINT organization_execution_incidents_type_check
        CHECK (incident_type IN ('provenance_conflict', 'platform_overage')),
    CONSTRAINT organization_execution_incidents_status_check CHECK (status = 'open')
);

CREATE INDEX IF NOT EXISTS organization_execution_incidents_org_created_idx
    ON organization_execution_incidents (organization_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS organization_execution_reconciliation_audits (
    id BIGSERIAL PRIMARY KEY,
    operation_id UUID NOT NULL,
    correlation_id UUID NOT NULL,
    idempotency_key_hash CHAR(64) NOT NULL,
    canonical_payload_hash CHAR(64) NOT NULL,
    execution_id_snapshot UUID NOT NULL,
    execution_ref_id BIGINT NOT NULL REFERENCES organization_executions(id) ON DELETE RESTRICT,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    final_evidence_id_snapshot BIGINT NOT NULL,
    final_evidence_ref_id BIGINT NULL REFERENCES organization_final_execution_evidences(id) ON DELETE SET NULL,
    selected_attempt_id_snapshot UUID NOT NULL,
    selected_attempt_ref_id BIGINT NULL REFERENCES organization_execution_attempts(id) ON DELETE SET NULL,
    selected_evidence_id_snapshot BIGINT NOT NULL,
    selected_evidence_ref_id BIGINT NULL REFERENCES organization_attempt_evidences(id) ON DELETE SET NULL,
    actor_id_snapshot BIGINT NOT NULL,
    actor_ref_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    actor_display_snapshot VARCHAR(255) NOT NULL,
    actor_role_snapshot VARCHAR(64) NOT NULL,
    authorization_scope VARCHAR(64) NOT NULL,
    action VARCHAR(16) NOT NULL,
    raw_actual_cost NUMERIC NOT NULL,
    organization_settled_cost DECIMAL(20,8) NOT NULL,
    reservation_amount DECIMAL(20,8) NOT NULL,
    reservation_status_before VARCHAR(32) NOT NULL,
    reservation_status_after VARCHAR(32) NOT NULL,
    public_reason VARCHAR(255) NOT NULL,
    internal_reason VARCHAR(2000) NOT NULL,
    reconciled_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT organization_execution_reconciliation_audits_execution_unique UNIQUE (execution_ref_id),
    CONSTRAINT organization_execution_reconciliation_audits_operation_unique UNIQUE (operation_id),
    CONSTRAINT organization_execution_reconciliation_audits_scoped_key_unique
        UNIQUE (authorization_scope, organization_id, idempotency_key_hash),
    CONSTRAINT organization_execution_reconciliation_audits_action_check CHECK (action IN ('settled', 'released')),
    CONSTRAINT organization_execution_reconciliation_audits_scope_check CHECK (authorization_scope = 'platform_admin_jwt'),
    CONSTRAINT organization_execution_reconciliation_audits_hash_check
        CHECK (
            idempotency_key_hash ~ '^[0-9a-f]{64}$'
            AND canonical_payload_hash ~ '^[0-9a-f]{64}$'
        ),
    CONSTRAINT organization_execution_reconciliation_audits_amount_check
        CHECK (raw_actual_cost >= 0 AND organization_settled_cost >= 0 AND reservation_amount >= 0),
    CONSTRAINT organization_execution_reconciliation_audits_reservation_state_check
        CHECK (
            reservation_status_before = 'pending_reconciliation'
            AND reservation_status_after IN ('settled', 'released')
        ),
    CONSTRAINT organization_execution_reconciliation_audits_final_ref_check
        CHECK (final_evidence_ref_id IS NULL OR final_evidence_ref_id = final_evidence_id_snapshot),
    CONSTRAINT organization_execution_reconciliation_audits_evidence_ref_check
        CHECK (selected_evidence_ref_id IS NULL OR selected_evidence_ref_id = selected_evidence_id_snapshot),
    CONSTRAINT organization_execution_reconciliation_audits_actor_ref_check
        CHECK (actor_ref_id IS NULL OR actor_ref_id = actor_id_snapshot)
);

CREATE INDEX IF NOT EXISTS organization_execution_reconciliation_audits_org_created_idx
    ON organization_execution_reconciliation_audits (organization_id, reconciled_at DESC, id DESC);

CREATE OR REPLACE FUNCTION guard_organization_attempt_account_posting_immutable()
RETURNS TRIGGER AS $$
DECLARE
    refs_allowed BOOLEAN;
BEGIN
    refs_allowed :=
        (NEW.evidence_ref_id IS NOT DISTINCT FROM OLD.evidence_ref_id OR (OLD.evidence_ref_id IS NOT NULL AND NEW.evidence_ref_id IS NULL))
        AND (NEW.account_ref_id IS NOT DISTINCT FROM OLD.account_ref_id OR (OLD.account_ref_id IS NOT NULL AND NEW.account_ref_id IS NULL));
    IF TG_OP = 'UPDATE' AND refs_allowed
       AND (to_jsonb(NEW) - ARRAY['evidence_ref_id', 'account_ref_id'])
           = (to_jsonb(OLD) - ARRAY['evidence_ref_id', 'account_ref_id']) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'organization attempt Account postings are immutable'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_attempt_account_effect_apply()
RETURNS TRIGGER AS $$
DECLARE
    refs_allowed BOOLEAN;
    terminal_allowed BOOLEAN;
BEGIN
	terminal_allowed := OLD.status = 'pending'
		AND NEW.status IN ('applied', 'applied_noop_deleted_target', 'operator_attention')
		AND NEW.applied_at IS NOT NULL;
	refs_allowed :=
		terminal_allowed
		OR (
			(NEW.account_ref_id IS NOT DISTINCT FROM OLD.account_ref_id OR (OLD.account_ref_id IS NOT NULL AND NEW.account_ref_id IS NULL))
			AND (NEW.selected_evidence_ref_id IS NOT DISTINCT FROM OLD.selected_evidence_ref_id OR (OLD.selected_evidence_ref_id IS NOT NULL AND NEW.selected_evidence_ref_id IS NULL))
			AND (NEW.posting_ref_id IS NOT DISTINCT FROM OLD.posting_ref_id OR (OLD.posting_ref_id IS NOT NULL AND NEW.posting_ref_id IS NULL))
		);
    IF TG_OP = 'UPDATE' AND refs_allowed AND (
        (NEW.status = OLD.status
         AND (to_jsonb(NEW) - ARRAY['account_ref_id', 'selected_evidence_ref_id', 'posting_ref_id', 'updated_at'])
             = (to_jsonb(OLD) - ARRAY['account_ref_id', 'selected_evidence_ref_id', 'posting_ref_id', 'updated_at']))
        OR terminal_allowed
    ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'organization attempt Account effects are immutable outside apply state'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_execution_incident_immutable()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'organization execution incidents are immutable'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION guard_organization_execution_reconciliation_audit_immutable()
RETURNS TRIGGER AS $$
DECLARE
    refs_allowed BOOLEAN;
BEGIN
    refs_allowed :=
        (NEW.final_evidence_ref_id IS NOT DISTINCT FROM OLD.final_evidence_ref_id OR (OLD.final_evidence_ref_id IS NOT NULL AND NEW.final_evidence_ref_id IS NULL))
        AND (NEW.selected_attempt_ref_id IS NOT DISTINCT FROM OLD.selected_attempt_ref_id OR (OLD.selected_attempt_ref_id IS NOT NULL AND NEW.selected_attempt_ref_id IS NULL))
        AND (NEW.selected_evidence_ref_id IS NOT DISTINCT FROM OLD.selected_evidence_ref_id OR (OLD.selected_evidence_ref_id IS NOT NULL AND NEW.selected_evidence_ref_id IS NULL))
        AND (NEW.actor_ref_id IS NOT DISTINCT FROM OLD.actor_ref_id OR (OLD.actor_ref_id IS NOT NULL AND NEW.actor_ref_id IS NULL));
    IF TG_OP = 'UPDATE' AND refs_allowed
       AND (to_jsonb(NEW) - ARRAY['final_evidence_ref_id', 'selected_attempt_ref_id', 'selected_evidence_ref_id', 'actor_ref_id'])
           = (to_jsonb(OLD) - ARRAY['final_evidence_ref_id', 'selected_attempt_ref_id', 'selected_evidence_ref_id', 'actor_ref_id']) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'organization execution reconciliation audits are immutable'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS organization_attempt_account_postings_immutable
    ON organization_attempt_account_postings;
CREATE TRIGGER organization_attempt_account_postings_immutable
    BEFORE UPDATE OR DELETE ON organization_attempt_account_postings
    FOR EACH ROW EXECUTE FUNCTION guard_organization_attempt_account_posting_immutable();

DROP TRIGGER IF EXISTS organization_attempt_account_effects_apply_guard
    ON organization_attempt_account_effects;
CREATE TRIGGER organization_attempt_account_effects_apply_guard
    BEFORE UPDATE OR DELETE ON organization_attempt_account_effects
    FOR EACH ROW EXECUTE FUNCTION guard_organization_attempt_account_effect_apply();

DROP TRIGGER IF EXISTS organization_execution_incidents_immutable
    ON organization_execution_incidents;
CREATE TRIGGER organization_execution_incidents_immutable
    BEFORE UPDATE OR DELETE ON organization_execution_incidents
    FOR EACH ROW EXECUTE FUNCTION guard_organization_execution_incident_immutable();

DROP TRIGGER IF EXISTS organization_execution_reconciliation_audits_immutable
    ON organization_execution_reconciliation_audits;
CREATE TRIGGER organization_execution_reconciliation_audits_immutable
    BEFORE UPDATE OR DELETE ON organization_execution_reconciliation_audits
    FOR EACH ROW EXECUTE FUNCTION guard_organization_execution_reconciliation_audit_immutable();
