-- Organization execution telemetry attribution.
--
-- These columns are nullable by design: requests that fail before trusted
-- organization admission must remain unattributed. Authenticated request-path
-- writers may populate them from trusted server-derived organization context;
-- client-supplied organization headers must never be used as attribution.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS organization_id BIGINT,
    ADD COLUMN IF NOT EXISTS execution_id UUID;

ALTER TABLE ops_error_logs
    ADD COLUMN IF NOT EXISTS organization_id BIGINT,
    ADD COLUMN IF NOT EXISTS execution_id UUID;

-- usage_logs may be deployed as a created_at-partitioned table.  PostgreSQL
-- requires a parent-table index to be created transactionally, and a global
-- unique key cannot omit the partition key.  Both projection tables therefore
-- use ordinary execution lookup indexes; exactly-once delivery and reporting
-- deduplication remain owned by the durable execution outbox/final evidence.
CREATE INDEX IF NOT EXISTS idx_usage_logs_organization_created_at
    ON usage_logs (organization_id, created_at DESC)
    WHERE organization_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_usage_logs_execution_id
    ON usage_logs (execution_id)
    WHERE execution_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ops_error_logs_organization_created_at
    ON ops_error_logs (organization_id, created_at DESC)
    WHERE organization_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ops_error_logs_execution_id
    ON ops_error_logs (execution_id)
    WHERE execution_id IS NOT NULL;
