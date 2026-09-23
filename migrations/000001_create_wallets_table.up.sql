-- Tabela de carteiras. É a "raiz do agregado financeiro" (seção 6.2).
CREATE TABLE wallets (
    id              UUID PRIMARY KEY,
    player_id       UUID NOT NULL,
    currency        CHAR(3) NOT NULL,
    -- Guardamos o saldo em CENTAVOS (BIGINT), nunca em float,
    -- conforme a decisão registrada em docs/PROJETO.md.
    balance_cents   BIGINT NOT NULL,
    version         BIGINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- "O par (playerId, currency) identifica uma única carteira" (seção 6.2).
    CONSTRAINT uq_wallets_player_currency UNIQUE (player_id, currency),

    -- Garantia de não-negatividade IMPOSTA PELO BANCO, não só pelo Go.
    -- Mesmo que um bug no código tente gravar saldo negativo, o
    -- Postgres recusa o INSERT/UPDATE.
    CONSTRAINT chk_wallets_balance_non_negative CHECK (balance_cents >= 0)
);

-- Índice de apoio para buscas por jogador (usado, por exemplo, para
-- verificar se já existe carteira antes de abrir uma nova).
CREATE INDEX idx_wallets_player_id ON wallets (player_id);
