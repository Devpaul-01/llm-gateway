CREATE TABLE projects (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                     TEXT NOT NULL,
    rate_limit_per_min       INTEGER,
    max_concurrent_streams   INTEGER,
    token_budget             INTEGER,
    status                   TEXT NOT NULL DEFAULT 'active',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE gateway_api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key_hash     TEXT NOT NULL UNIQUE,
    key_prefix   TEXT NOT NULL,
    label        TEXT,
    status       TEXT NOT NULL DEFAULT 'active',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE provider_credentials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL,
    label           TEXT,
    encrypted_key   BYTEA NOT NULL,
    key_nonce       BYTEA NOT NULL,
    key_version     INTEGER NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_credentials_project_provider ON provider_credentials(project_id, provider);


CREATE TABLE request_logs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id          UUID NOT NULL REFERENCES projects(id),
    gateway_key_id      UUID NOT NULL REFERENCES gateway_api_keys(id),
    provider            TEXT NOT NULL,
    model               TEXT NOT NULL,
    credential_id       UUID REFERENCES provider_credentials(id),
    status              TEXT NOT NULL,
    error_category      TEXT,
    tokens_in           INTEGER,
    tokens_out          INTEGER,
    ttft_ms             INTEGER,
    total_duration_ms   INTEGER,
    stream              BOOLEAN NOT NULL,
    used_vision         BOOLEAN NOT NULL DEFAULT false,
    models_used         TEXT[],
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_request_logs_project_time ON request_logs(project_id, created_at);
CREATE INDEX idx_request_logs_provider_time ON request_logs(provider, created_at);