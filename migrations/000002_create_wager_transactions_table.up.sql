-- Tabela de transações de apostas (WagerTransaction, seção 6.3).
-- Campos de origem externa (provider_id, external_transaction_id,
-- idempotency_key, payload_hash, round_id, game_id) são NULL para
-- a transação interna de abertura (kind = 'OPENING'), conforme a
-- seção 6.3 do desafio explica.
CREATE TABLE wager_transactions (
    id                              UUID PRIMARY KEY,
    external_transaction_id         TEXT,
    provider_id                     TEXT,
    idempotency_key                 TEXT,
    payload_hash                    TEXT NOT NULL,
    wallet_id                       UUID NOT NULL REFERENCES wallets (id),
    player_id                       UUID NOT NULL,
    round_id                        TEXT,
    game_id                         TEXT,
    kind                            TEXT NOT NULL,
    amount_cents                    BIGINT NOT NULL,
    currency                        CHAR(3) NOT NULL,
    reference_external_tx_id        TEXT,
    status                          TEXT NOT NULL,
    failure_code                    TEXT,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_wager_kind CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    CONSTRAINT chk_wager_status CHECK (status IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED')),

    -- "O schema deve distinguir operações internas e externas e
    -- impedir crédito inicial duplicado" (seção 6.3).
    -- Regra: OPENING nunca tem provider_id nem idempotency_key;
    -- qualquer outro kind sempre tem os dois.
    CONSTRAINT chk_wager_opening_shape CHECK (
        (kind = 'OPENING' AND provider_id IS NULL AND idempotency_key IS NULL AND external_transaction_id IS NULL)
        OR
        (kind <> 'OPENING' AND provider_id IS NOT NULL AND idempotency_key IS NOT NULL AND external_transaction_id IS NOT NULL)
    )
);

-- Idempotência por chave: mesma chave do mesmo provedor não pode
-- gerar dois registros distintos (seção 9 — "Chave e conteúdo
-- equivalentes: retorne o resultado persistido").
CREATE UNIQUE INDEX uq_wager_provider_idempotency_key
    ON wager_transactions (provider_id, idempotency_key)
    WHERE provider_id IS NOT NULL;

-- Uma operação financeira identificada por (providerId, externalTransactionId)
-- não pode ser reaplicada usando outra chave (seção 9).
CREATE UNIQUE INDEX uq_wager_provider_external_tx
    ON wager_transactions (provider_id, external_transaction_id)
    WHERE provider_id IS NOT NULL;

-- Impede crédito inicial duplicado: só pode existir UMA transação
-- OPENING por carteira.
CREATE UNIQUE INDEX uq_wager_opening_per_wallet
    ON wager_transactions (wallet_id)
    WHERE kind = 'OPENING';

-- Índices de apoio para consultas de leitura (GET /wagering/transactions/:id
-- e GET /providers/:id/wagering/transactions/:externalId, seção 9).
CREATE INDEX idx_wager_wallet_id ON wager_transactions (wallet_id);
CREATE INDEX idx_wager_status ON wager_transactions (status) WHERE status IN ('PENDING', 'PENDING_REFERENCE');
