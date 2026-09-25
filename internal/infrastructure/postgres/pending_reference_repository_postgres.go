package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// PendingReferenceRepository implementa domain.PendingReferenceRepository.
// Mesmo raciocínio do OutboxPublisherRepository: recebe o *pgxpool.Pool
// direto (não um DBTX de UnitOfWork), porque reivindicar/liberar um
// lote é uma transação curta própria do worker, separada da transação
// de negócio que de fato reaplica cada candidata (essa sim passa por
// TxRunner.WithinTransaction, em internal/application).
type PendingReferenceRepository struct {
	pool *pgxpool.Pool
}

func NewPendingReferenceRepository(pool *pgxpool.Pool) *PendingReferenceRepository {
	return &PendingReferenceRepository{pool: pool}
}

// Claim segue o MESMO padrão em duas etapas do OutboxPublisherRepository.Claim
// (ver comentário lá): SELECT ... FOR UPDATE SKIP LOCKED para resolver
// a disputa entre instâncias no mesmo instante, seguido de um UPDATE
// que grava o lock lógico (locked_by/locked_at), que sobrevive ao fim
// desta transação curta — é o que impede outra instância de
// reivindicar a MESMA transação enquanto esta ainda está reaplicando.
// Um lock mais velho que lockTimeout conta como abandonado.
func (r *PendingReferenceRepository) Claim(
	ctx context.Context,
	workerID string,
	limit int,
	lockTimeout time.Duration,
) ([]domain.PendingReferenceCandidate, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("abrindo transação para reivindicar referências pendentes: %w", err)
	}
	defer tx.Rollback(ctx) // no-op se o commit no fim já tiver acontecido

	lockTimeoutSeconds := lockTimeout.Seconds()

	rows, err := tx.Query(ctx, `
		SELECT id, reference_attempts, reference_first_pending_at
		FROM wager_transactions
		WHERE status = 'PENDING_REFERENCE'
		  AND reference_next_retry_at <= now()
		  AND (locked_by IS NULL OR locked_at < now() - make_interval(secs => $1))
		ORDER BY reference_next_retry_at
		FOR UPDATE SKIP LOCKED
		LIMIT $2
	`, lockTimeoutSeconds, limit)
	if err != nil {
		return nil, fmt.Errorf("consultando referências pendentes: %w", err)
	}

	var candidates []domain.PendingReferenceCandidate
	var ids []uuid.UUID
	for rows.Next() {
		var c domain.PendingReferenceCandidate
		if err := rows.Scan(&c.ID, &c.Attempts, &c.FirstPendingAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("lendo referência pendente: %w", err)
		}
		candidates = append(candidates, c)
		ids = append(ids, c.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando referências pendentes: %w", err)
	}
	rows.Close()

	if len(ids) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("confirmando reivindicação vazia de referências pendentes: %w", err)
		}
		return nil, nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE wager_transactions
		SET locked_by = $1, locked_at = now()
		WHERE id = ANY($2)
	`, workerID, ids); err != nil {
		return nil, fmt.Errorf("travando lote reivindicado de referências pendentes: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("confirmando reivindicação do lote de referências pendentes: %w", err)
	}

	return candidates, nil
}

// MarkRetryScheduled só tem efeito se `locked_by` ainda for este
// workerID (mesmo raciocínio de MarkFailed em OutboxPublisherRepository:
// se o lock já foi considerado abandonado e reivindicado por outra
// instância, quem o reivindicou é quem decide o próximo passo agora).
func (r *PendingReferenceRepository) MarkRetryScheduled(ctx context.Context, id uuid.UUID, workerID string, attempts int, nextRetryAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE wager_transactions
		SET reference_attempts = $3, reference_next_retry_at = $4, locked_by = NULL, locked_at = NULL
		WHERE id = $1 AND locked_by = $2
	`, id, workerID, attempts, nextRetryAt)
	if err != nil {
		return fmt.Errorf("agendando nova tentativa da transação %s: %w", id, err)
	}
	return nil
}

// ReleaseLock libera o lock sem tocar em attempts/next_retry — usado
// quando a transação já saiu de PENDING_REFERENCE nesta rodada (não há
// mais nada a agendar).
func (r *PendingReferenceRepository) ReleaseLock(ctx context.Context, id uuid.UUID, workerID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE wager_transactions
		SET locked_by = NULL, locked_at = NULL
		WHERE id = $1 AND locked_by = $2
	`, id, workerID)
	if err != nil {
		return fmt.Errorf("liberando lock da transação %s: %w", id, err)
	}
	return nil
}
