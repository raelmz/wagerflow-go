DROP INDEX IF EXISTS uq_wager_single_processed_reversal;
ALTER TABLE wager_transactions DROP CONSTRAINT IF EXISTS chk_wager_resulting_balance_non_negative;
ALTER TABLE wager_transactions
    DROP COLUMN IF EXISTS resulting_balance_cents,
    DROP COLUMN IF EXISTS reference_transaction_id;
