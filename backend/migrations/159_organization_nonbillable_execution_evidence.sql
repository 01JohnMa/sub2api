-- Ticket 17: reservation-free, non-billable execution evidence.
-- The existing billable execution/reservation contract remains unchanged.

CREATE OR REPLACE FUNCTION guard_organization_execution_immutable()
RETURNS TRIGGER AS $$
DECLARE
    refs_allowed BOOLEAN;
    state_allowed BOOLEAN;
    terminal_allowed BOOLEAN;
    non_billable BOOLEAN;
BEGIN
    refs_allowed :=
        NEW.allocation_ref_id IS NOT DISTINCT FROM OLD.allocation_ref_id
        AND (NEW.user_ref_id IS NOT DISTINCT FROM OLD.user_ref_id OR (OLD.user_ref_id IS NOT NULL AND NEW.user_ref_id IS NULL))
        AND (NEW.api_key_ref_id IS NOT DISTINCT FROM OLD.api_key_ref_id OR (OLD.api_key_ref_id IS NOT NULL AND NEW.api_key_ref_id IS NULL));
    non_billable := OLD.is_billable = FALSE;
    state_allowed := NEW.status = OLD.status OR (
        (OLD.status = 'accepted' AND NEW.status IN ('reserved', 'settled', 'released'))
        OR (OLD.status = 'reserved' AND NEW.status IN ('dispatched', 'pending_reconciliation', 'settled', 'released'))
        OR (OLD.status = 'dispatched' AND NEW.status IN ('final_usage_observed', 'pending_reconciliation', 'settled', 'released'))
        OR (OLD.status = 'final_usage_observed' AND NEW.status IN ('pending_reconciliation', 'settled', 'released'))
        OR (OLD.status = 'pending_reconciliation' AND NEW.status IN ('settled', 'released'))
        OR (non_billable AND OLD.status = 'accepted' AND NEW.status IN ('dispatched', 'final_usage_observed', 'pending_reconciliation', 'released'))
        OR (non_billable AND OLD.status = 'dispatched' AND NEW.status IN ('final_usage_observed', 'pending_reconciliation', 'released'))
        OR (non_billable AND OLD.status = 'final_usage_observed' AND NEW.status IN ('pending_reconciliation', 'released'))
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

CREATE OR REPLACE FUNCTION enforce_organization_final_evidence_consumption()
RETURNS TRIGGER AS $$
DECLARE
    evidence_id_value BIGINT;
    evidence_execution_ref_id BIGINT;
    settled_cost_value DECIMAL(20,8);
    evidence_attempt_id BIGINT;
    evidence_zero_proof JSONB;
    evidence_raw_actual_cost NUMERIC;
    evidence_is_count_tokens_probe BOOLEAN;
    execution_status_value VARCHAR(32);
    execution_is_billable BOOLEAN;
    execution_endpoint_family VARCHAR(128);
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
           final_attempt_evidence_id_snapshot, zero_cost_proof,
           raw_actual_cost, is_count_tokens_probe
    INTO evidence_execution_ref_id, settled_cost_value,
         evidence_attempt_id, evidence_zero_proof,
         evidence_raw_actual_cost, evidence_is_count_tokens_probe
    FROM organization_final_execution_evidences WHERE id = evidence_id_value;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;

    SELECT status, is_billable, endpoint_family
    INTO execution_status_value, execution_is_billable, execution_endpoint_family
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
    ELSIF execution_status_value <> 'released' THEN
        RAISE EXCEPTION 'organization reservation-free final evidence is not a released terminal execution'
            USING ERRCODE = '23514';
    ELSIF evidence_attempt_id IS NULL THEN
        IF evidence_zero_proof IS NULL THEN
            RAISE EXCEPTION 'organization reservation-free final evidence is not a proven-zero terminal execution'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NOT (
        execution_is_billable = FALSE
        AND evidence_zero_proof IS NULL
        AND evidence_raw_actual_cost = 0
        AND (
            (execution_endpoint_family = 'count_tokens' AND evidence_is_count_tokens_probe = TRUE)
            OR (execution_endpoint_family = 'models' AND evidence_is_count_tokens_probe = FALSE)
        )
    ) THEN
        RAISE EXCEPTION 'organization reservation-free attempt evidence must be non-billable zero-cost models or count_tokens evidence'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
