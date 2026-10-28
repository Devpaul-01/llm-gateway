-- Observability columns used by the metrics endpoints.
ALTER TABLE request_logs
    ADD COLUMN IF NOT EXISTS request_id           TEXT,
    ADD COLUMN IF NOT EXISTS cache_hit            BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS attempts             INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS failover_count       INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS first_error_category TEXT,
    ADD COLUMN IF NOT EXISTS usage_estimated      BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_request_logs_created ON request_logs(created_at);
