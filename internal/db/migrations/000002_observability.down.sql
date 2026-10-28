DROP INDEX IF EXISTS idx_request_logs_created;
ALTER TABLE request_logs
    DROP COLUMN IF EXISTS request_id,
    DROP COLUMN IF EXISTS cache_hit,
    DROP COLUMN IF EXISTS attempts,
    DROP COLUMN IF EXISTS failover_count,
    DROP COLUMN IF EXISTS first_error_category,
    DROP COLUMN IF EXISTS usage_estimated;
