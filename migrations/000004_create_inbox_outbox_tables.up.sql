-- Inbox: registra cada mensagem SQS recebida, para deduplicação
-- adicional além da chave de idempotência do domínio (seção 6.5 e 10).
CREATE TABLE inbox_messages (
    id              UUID PRIMARY KEY,
    consumer_name   TEXT NOT NULL,
    message_id      TEXT NOT NULL, -- messageId do envelope SQS
    payload_hash    TEXT NOT NULL,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ,

    -- "unicidade de (consumerName, messageId)" (seção 6.5).
    CONSTRAINT uq_inbox_consumer_message UNIQUE (consumer_name, message_id)
);

-- Outbox: eventos de integração a publicar, com suporte a
-- retry/backoff e recuperação por outra instância (seção 6.5 e 11).
CREATE TABLE outbox_events (
    id              UUID PRIMARY KEY, -- eventId, estável mesmo em republicação
    aggregate_id    UUID NOT NULL,    -- ex: walletId ou transactionId
    event_type      TEXT NOT NULL,    -- ex: WagerTransactionProcessed
    payload         JSONB NOT NULL,   -- snapshot imutável do evento
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts        INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at    TIMESTAMPTZ,

    -- Usado pelo worker de publicação para "reivindicar" um lote de
    -- eventos pendentes sem dois publishers pegarem o mesmo registro
    -- (lock otimista simples via updated_at/locked_by, se necessário
    -- evoluir; por ora, SELECT ... FOR UPDATE SKIP LOCKED no worker
    -- resolve a disputa entre publishers concorrentes).
    locked_by       TEXT,
    locked_at       TIMESTAMPTZ
);

CREATE INDEX idx_outbox_pending
    ON outbox_events (next_attempt_at)
    WHERE published_at IS NULL;
