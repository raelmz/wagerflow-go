package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PendingOutboxEvent é uma linha da outbox já reivindicada pelo worker
// publicador (seção 11 do desafio) para tentativa de envio ao destino
// de saída. Diferente de OutboxEvent (que só existe através de um dos
// construtores validados em outbox_event.go), este tipo é só um DTO de
// LEITURA: o publisher não reconstrói nem revalida a regra de negócio
// que gerou o evento, só reenvia o payload que já foi gravado como
// snapshot imutável no commit original.
type PendingOutboxEvent struct {
	ID          uuid.UUID
	AggregateID uuid.UUID
	EventType   string
	Payload     []byte
	Attempts    int
}

// OutboxPublisherRepository é usado SÓ pelo worker publicador — nunca
// pelo caso de uso de negócio, que grava eventos só através de
// OutboxRepository.Append (dentro de UnitOfWork.Outbox(), na mesma
// transação da mudança de estado). São dois "papéis" diferentes sobre
// a mesma tabela outbox_events, cada um com seu próprio ciclo de vida:
// separar as interfaces deixa isso explícito no código, em vez de uma
// interface só com métodos que a metade dos chamadores nunca usa.
type OutboxPublisherRepository interface {
	// Claim reivindica até `limit` eventos pendentes: published_at
	// nulo, next_attempt_at já chegou, e NÃO travados por outro
	// worker "vivo" — um lock mais velho que lockTimeout é tratado
	// como abandonado (é isso que recupera trabalho de um publisher
	// que morreu no meio, exigido pela seção 11). workerID marca o
	// dono do lote reivindicado.
	Claim(ctx context.Context, workerID string, limit int, lockTimeout time.Duration) ([]PendingOutboxEvent, error)

	// MarkPublished confirma que o evento foi entregue ao destino de
	// saída. Só tem efeito se `locked_by` no banco ainda for este
	// workerID — evita que um worker "zumbi" (que já teve seu lock
	// considerado abandonado e reivindicado por outra instância)
	// sobrescreva o resultado de quem assumiu o evento depois dele.
	MarkPublished(ctx context.Context, id uuid.UUID, workerID string) error

	// MarkFailed registra uma tentativa que não deu certo: soma 1 em
	// attempts, agenda nextAttemptAt (calculado pelo backoff, fora
	// desta interface) e libera o lock. Mesma proteção por workerID
	// que MarkPublished.
	MarkFailed(ctx context.Context, id uuid.UUID, workerID string, nextAttemptAt time.Time) error
}
