package postgres

import (
	"context"
	"fmt"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

type OutboxRepository struct {
	db DBTX
}

func NewOutboxRepository(db DBTX) *OutboxRepository {
	return &OutboxRepository{db: db}
}

// Append grava o evento em outbox_events. Como o repositório recebe o
// mesmo DBTX da transação do UnitOfWork, este INSERT só fica visível
// (e só vira "pendente de publicação" para o worker) se a transação
// inteira fizer commit.
//
// Colunas que NÃO preenchemos de propósito: attempts (0), next_attempt_at
// (agora) e published_at (nulo) usam os DEFAULTs da migration 000004 —
// o evento nasce "pronto para publicar, nunca tentado".
//
// O payload é o envelope completo em JSON (snapshot imutável); o
// Postgres o valida como JSONB. id é o eventId, que permanece o mesmo
// se o evento for republicado.
func (r *OutboxRepository) Append(ctx context.Context, event *domain.OutboxEvent) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO outbox_events (id, aggregate_id, event_type, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5)
	`,
		event.ID(), event.AggregateID(), event.Type(), event.Payload(), event.OccurredAt(),
	)
	if err != nil {
		return fmt.Errorf("falha ao inserir evento %s na outbox: %w", event.Type(), err)
	}
	return nil
}
