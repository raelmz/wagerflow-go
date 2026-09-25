package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PendingReferenceCandidate é uma WagerTransaction em PENDING_REFERENCE
// já reivindicada pelo worker de referências pendentes (seção 7 do
// desafio: "referências ainda indisponíveis"). Só carrega o que o
// worker precisa para DECIDIR o que fazer (tentar de novo ou desistir)
// — a transação completa é carregada depois, por FindByID, só quando o
// worker vai de fato reaplicá-la.
type PendingReferenceCandidate struct {
	ID uuid.UUID

	// Attempts conta quantas vezes o worker já tentou reaplicar esta
	// transação sem sucesso (referência ainda indisponível). Começa
	// em 0 na primeira vez que ela entra em PENDING_REFERENCE.
	Attempts int

	// FirstPendingAt é quando a transação entrou em PENDING_REFERENCE
	// pela PRIMEIRA vez — é o relógio do TTL, não muda entre
	// tentativas.
	FirstPendingAt time.Time
}

// PendingReferenceRepository é usado SÓ pelo worker de referências
// pendentes — nunca pelo caso de uso de negócio, que só grava/lê
// PENDING_REFERENCE através de WagerTransactionRepository comum. Mesma
// separação de papéis que OutboxPublisherRepository faz para a
// outbox: o negócio e o worker enxergam a mesma tabela de ângulos
// diferentes.
type PendingReferenceRepository interface {
	// Claim reivindica até `limit` transações em PENDING_REFERENCE
	// cujo next_retry já chegou e que não estão travadas por outro
	// worker "vivo" (lock mais velho que lockTimeout conta como
	// abandonado — recuperação de trabalho de um worker que morreu no
	// meio, mesmo raciocínio do publisher da outbox).
	Claim(ctx context.Context, workerID string, limit int, lockTimeout time.Duration) ([]PendingReferenceCandidate, error)

	// MarkRetryScheduled registra que a tentativa desta rodada não
	// resolveu a referência: soma 1 em attempts, agenda o próximo
	// retry (calculado pelo backoff, fora desta interface) e libera o
	// lock. Só tem efeito se `locked_by` ainda for este workerID.
	MarkRetryScheduled(ctx context.Context, id uuid.UUID, workerID string, attempts int, nextRetryAt time.Time) error

	// ReleaseLock libera o lock sem mexer em attempts/next_retry —
	// usado quando a transação SAIU de PENDING_REFERENCE nesta rodada
	// (foi processada, rejeitada, ou o próprio worker a rejeitou por
	// esgotar tentativas/TTL): não há mais nada para agendar.
	ReleaseLock(ctx context.Context, id uuid.UUID, workerID string) error
}
