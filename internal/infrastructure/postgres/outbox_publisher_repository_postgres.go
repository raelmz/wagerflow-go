package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// OutboxPublisherRepository implementa domain.OutboxPublisherRepository.
// Diferente dos repositórios de negócio (que recebem um DBTX porque
// precisam rodar DENTRO da transação do UnitOfWork), este recebe o
// *pgxpool.Pool diretamente: o worker publicador não faz parte da saga
// de negócio, cada operação sua (reivindicar lote, marcar resultado) é
// sua própria transação curta.
type OutboxPublisherRepository struct {
	pool *pgxpool.Pool
}

func NewOutboxPublisherRepository(pool *pgxpool.Pool) *OutboxPublisherRepository {
	return &OutboxPublisherRepository{pool: pool}
}

// Claim reivindica um lote em DUAS etapas, na MESMA transação curta:
//  1. SELECT ... FOR UPDATE SKIP LOCKED — resolve a disputa entre
//     publishers que tentam reivindicar no MESMO instante (cada um só
//     enxerga as linhas que o outro não travou).
//  2. UPDATE dessas linhas com locked_by/locked_at — é isso que
//     mantém o "lock lógico" DEPOIS que a transação der commit e o
//     lock de linha do Postgres for liberado. Sem isso, um segundo
//     publisher poderia reivindicar a MESMA linha minutos depois,
//     enquanto o primeiro ainda está tentando enviar ao SQS.
//
// Um lock mais velho que lockTimeout conta como abandonado (condição
// `locked_by IS NULL OR locked_at < now() - lockTimeout`) — é a
// recuperação de trabalho de um publisher que morreu no meio, exigida
// pela seção 11 do desafio.
func (r *OutboxPublisherRepository) Claim(
	ctx context.Context,
	workerID string,
	limit int,
	lockTimeout time.Duration,
) ([]domain.PendingOutboxEvent, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("abrindo transação para reivindicar outbox: %w", err)
	}
	defer tx.Rollback(ctx) // no-op se o commit no fim já tiver acontecido

	lockTimeoutSeconds := lockTimeout.Seconds()

	rows, err := tx.Query(ctx, `
		SELECT id, aggregate_id, event_type, payload, attempts
		FROM outbox_events
		WHERE published_at IS NULL
		  AND next_attempt_at <= now()
		  AND (locked_by IS NULL OR locked_at < now() - make_interval(secs => $1))
		ORDER BY occurred_at
		FOR UPDATE SKIP LOCKED
		LIMIT $2
	`, lockTimeoutSeconds, limit)
	if err != nil {
		return nil, fmt.Errorf("consultando eventos pendentes da outbox: %w", err)
	}

	var events []domain.PendingOutboxEvent
	var ids []uuid.UUID
	for rows.Next() {
		var e domain.PendingOutboxEvent
		if err := rows.Scan(&e.ID, &e.AggregateID, &e.EventType, &e.Payload, &e.Attempts); err != nil {
			rows.Close()
			return nil, fmt.Errorf("lendo evento pendente da outbox: %w", err)
		}
		events = append(events, e)
		ids = append(ids, e.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando eventos pendentes da outbox: %w", err)
	}
	rows.Close()

	if len(ids) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("confirmando reivindicação vazia da outbox: %w", err)
		}
		return nil, nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE outbox_events
		SET locked_by = $1, locked_at = now()
		WHERE id = ANY($2)
	`, workerID, ids); err != nil {
		return nil, fmt.Errorf("travando lote reivindicado da outbox: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("confirmando reivindicação do lote da outbox: %w", err)
	}

	return events, nil
}

// MarkPublished só tem efeito se `locked_by` ainda for este workerID
// (ver comentário da interface em domain/outbox_publisher_repository.go).
// Não retorna erro se nenhuma linha bateu a condição — pode significar
// só que o lock já tinha sido reivindicado por outra instância como
// abandonado; o evento será reenviado por quem o reivindicou, e o SQS
// FIFO (MessageDeduplicationId = eventId) descarta o duplicado.
func (r *OutboxPublisherRepository) MarkPublished(ctx context.Context, id uuid.UUID, workerID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE outbox_events
		SET published_at = now(), locked_by = NULL, locked_at = NULL
		WHERE id = $1 AND locked_by = $2
	`, id, workerID)
	if err != nil {
		return fmt.Errorf("marcando evento %s como publicado: %w", id, err)
	}
	return nil
}

// MarkFailed soma 1 em attempts, agenda nextAttemptAt (já calculado
// pelo backoff em application/) e libera o lock. Mesma proteção por
// workerID que MarkPublished.
func (r *OutboxPublisherRepository) MarkFailed(ctx context.Context, id uuid.UUID, workerID string, nextAttemptAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE outbox_events
		SET attempts = attempts + 1, next_attempt_at = $3, locked_by = NULL, locked_at = NULL
		WHERE id = $1 AND locked_by = $2
	`, id, workerID, nextAttemptAt)
	if err != nil {
		return fmt.Errorf("marcando evento %s como falho: %w", id, err)
	}
	return nil
}
