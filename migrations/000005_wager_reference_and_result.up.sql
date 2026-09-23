-- Duas informações que a seção 6.3 e a seção 9 do desafio exigem e que
-- a tabela original não guardava:
--
-- 1) reference_transaction_id: a referência INTERNA resolvida. Para
--    REFUND/ROLLBACK (e WIN com referência), guardamos o id da
--    WagerTransaction alvo, além do reference_external_tx_id que veio
--    no payload. É isso que permite auditar "esta reversão desfez
--    exatamente aquela aposta".
--
-- 2) resulting_balance_cents: o saldo da carteira observado quando a
--    operação foi concluída. A seção 9 diz que o replay deve devolver
--    "o saldo observado no processamento original, mesmo que a
--    carteira já tenha recebido outras movimentações". Sem guardar
--    isso, um replay só conseguiria devolver o saldo ATUAL (errado).
--    Fica NULL para operações que não concluíram (REJECTED, pendentes).
ALTER TABLE wager_transactions
    ADD COLUMN reference_transaction_id UUID REFERENCES wager_transactions (id),
    ADD COLUMN resulting_balance_cents  BIGINT;

ALTER TABLE wager_transactions
    ADD CONSTRAINT chk_wager_resulting_balance_non_negative
    CHECK (resulting_balance_cents IS NULL OR resulting_balance_cents >= 0);

-- Uma referência só pode receber UMA reversão bem-sucedida, de
-- qualquer tipo (REFUND ou ROLLBACK). Isso cobre "duas reversões do
-- mesmo tipo" e também "REFUND + ROLLBACK sobre a mesma aposta"
-- (devolução duplicada do mesmo débito), como a seção 7 exige.
-- É a rede de segurança do banco; o caso de uso já serializa por
-- referência com SELECT ... FOR UPDATE antes de chegar aqui.
CREATE UNIQUE INDEX uq_wager_single_processed_reversal
    ON wager_transactions (reference_transaction_id)
    WHERE kind IN ('REFUND', 'ROLLBACK') AND status = 'PROCESSED';
