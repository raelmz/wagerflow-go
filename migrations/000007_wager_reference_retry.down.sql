DROP INDEX IF EXISTS idx_wager_pending_reference_retry;

ALTER TABLE wager_transactions
    DROP COLUMN IF EXISTS locked_at,
    DROP COLUMN IF EXISTS locked_by,
    DROP COLUMN IF EXISTS reference_next_retry_at,
    DROP COLUMN IF EXISTS reference_first_pending_at,
    DROP COLUMN IF EXISTS reference_attempts;
