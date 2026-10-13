-- Platform organization foundation. Organizations are retained by lifecycle
-- state; there is intentionally no deleted_at column or delete API.
CREATE TABLE IF NOT EXISTS organizations (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    code VARCHAR(64) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT organizations_code_normalized_check
        CHECK (code = lower(trim(code))),
    CONSTRAINT organizations_status_check
        CHECK (status IN ('active', 'disabled', 'archived')),
    CONSTRAINT organizations_code_unique UNIQUE (code)
);

CREATE INDEX IF NOT EXISTS organizations_status_idx ON organizations (status);
CREATE INDEX IF NOT EXISTS organizations_created_by_idx ON organizations (created_by);
