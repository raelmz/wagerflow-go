-- Ledger append-only (seção 6.4 e seção 5: "correções financeiras
-- exigem novos lançamentos"). A imutabilidade é garantida por um
-- TRIGGER que recusa qualquer UPDATE ou DELETE nesta tabela — não
-- basta confiar que o código Go "nunca vai chamar UPDATE", o banco
-- barra fisicamente.
CREATE TABLE wallet_ledger_entries (
    id                  UUID PRIMARY KEY,
    wallet_id           UUID NOT NULL REFERENCES wallets (id),
    transaction_id      UUID NOT NULL REFERENCES wager_transactions (id),
    direction           TEXT NOT NULL,
    amount_cents        BIGINT NOT NULL,
    balance_before_cents BIGINT NOT NULL,
    balance_after_cents  BIGINT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_ledger_direction CHECK (direction IN ('DEBIT', 'CREDIT')),

    -- Confere no próprio banco que balanceAfter = balanceBefore ± amount,
    -- como segunda camada de proteção (a primeira é o construtor em Go).
    CONSTRAINT chk_ledger_arithmetic CHECK (
        (direction = 'CREDIT' AND balance_after_cents = balance_before_cents + amount_cents)
        OR
        (direction = 'DEBIT' AND balance_after_cents = balance_before_cents - amount_cents)
    ),

    -- "Imponha no banco a unicidade de (walletId, transactionId)" (seção 6.4).
    CONSTRAINT uq_ledger_wallet_transaction UNIQUE (wallet_id, transaction_id)
);

CREATE INDEX idx_ledger_wallet_id ON wallet_ledger_entries (wallet_id, created_at);

-- Função de trigger: recusa qualquer tentativa de UPDATE ou DELETE.
CREATE OR REPLACE FUNCTION prevent_ledger_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'wallet_ledger_entries é append-only: % não é permitido', TG_OP;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_ledger_no_update
    BEFORE UPDATE ON wallet_ledger_entries
    FOR EACH ROW EXECUTE FUNCTION prevent_ledger_mutation();

CREATE TRIGGER trg_ledger_no_delete
    BEFORE DELETE ON wallet_ledger_entries
    FOR EACH ROW EXECUTE FUNCTION prevent_ledger_mutation();
