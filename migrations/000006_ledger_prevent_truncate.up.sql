-- A migration 000003 criou triggers BEFORE UPDATE/DELETE (FOR EACH
-- ROW) em wallet_ledger_entries. Isso não cobre TRUNCATE: é um
-- comando de outra categoria (statement-level, não row-level), então
-- um TRUNCATE apagava a tabela inteira sem disparar nenhum dos dois
-- triggers e sem erro. Esta migration fecha essa brecha com um
-- terceiro trigger, FOR EACH STATEMENT, no mesmo evento BEFORE
-- TRUNCATE — reaproveitando a função prevent_ledger_mutation() já
-- existente.
CREATE TRIGGER trg_ledger_no_truncate
    BEFORE TRUNCATE ON wallet_ledger_entries
    FOR EACH STATEMENT EXECUTE FUNCTION prevent_ledger_mutation();
