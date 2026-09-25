-- Colunas de apoio SÓ ao worker de referências pendentes (seção 7:
-- "referências ainda indisponíveis"). Nenhuma delas participa das
-- regras de negócio em internal/domain — são bookkeeping do worker,
-- pelo mesmo motivo que outbox_events tem attempts/locked_by/locked_at
-- para o worker publicador.
--
-- reference_attempts        : quantas vezes já tentamos reaplicar esta
--                              transação sem sucesso.
-- reference_first_pending_at: quando ela entrou em PENDING_REFERENCE
--                              pela PRIMEIRA vez (relógio do TTL).
-- reference_next_retry_at   : quando pode ser reivindicada de novo.
-- locked_by / locked_at     : lock lógico do worker (mesmo padrão de
--                              outbox_events.locked_by/locked_at).
ALTER TABLE wager_transactions
    ADD COLUMN reference_attempts         INT NOT NULL DEFAULT 0,
    ADD COLUMN reference_first_pending_at TIMESTAMPTZ,
    ADD COLUMN reference_next_retry_at    TIMESTAMPTZ,
    ADD COLUMN locked_by                  UUID,
    ADD COLUMN locked_at                  TIMESTAMPTZ;

-- Índice parcial: só linhas PENDING_REFERENCE interessam ao worker, e
-- são uma fração pequena da tabela em regime normal.
CREATE INDEX idx_wager_pending_reference_retry
    ON wager_transactions (reference_next_retry_at)
    WHERE status = 'PENDING_REFERENCE';
