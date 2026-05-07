-- Create codex_registration_candidates table for Codex host-file registration discovery state.

CREATE TABLE IF NOT EXISTS codex_registration_candidates (
    id BIGSERIAL PRIMARY KEY,
    source_path VARCHAR(2048) NOT NULL,
    source_filename VARCHAR(255) NOT NULL,
    source_mtime TIMESTAMPTZ NOT NULL,
    source_fingerprint VARCHAR(128) NOT NULL,
    email VARCHAR(320),
    account_id VARCHAR(128),
    type VARCHAR(50),
    expires_at TIMESTAMPTZ,
    last_refresh_at TIMESTAMPTZ,
    liveness_status VARCHAR(20) NOT NULL DEFAULT 'error',
    workflow_state VARCHAR(20) NOT NULL DEFAULT 'detected',
    status_reason TEXT,
    last_checked_at TIMESTAMPTZ,
    imported_account_id BIGINT,
    imported_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_registration_candidates_source_path
    ON codex_registration_candidates(source_path);

CREATE INDEX IF NOT EXISTS idx_codex_registration_candidates_workflow_state
    ON codex_registration_candidates(workflow_state);

CREATE INDEX IF NOT EXISTS idx_codex_registration_candidates_liveness_status
    ON codex_registration_candidates(liveness_status);

CREATE INDEX IF NOT EXISTS idx_codex_registration_candidates_account_id
    ON codex_registration_candidates(account_id);
