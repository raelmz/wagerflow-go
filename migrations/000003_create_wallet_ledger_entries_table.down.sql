DROP TRIGGER IF EXISTS trg_ledger_no_delete ON wallet_ledger_entries;
DROP TRIGGER IF EXISTS trg_ledger_no_update ON wallet_ledger_entries;
DROP FUNCTION IF EXISTS prevent_ledger_mutation();
DROP TABLE IF EXISTS wallet_ledger_entries;
