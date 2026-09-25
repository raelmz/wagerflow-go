package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// InboxRepository implementa domain.InboxRepository sobre a tabela
// inbox_messages (migration 000004).
type InboxRepository struct {
	db DBTX
}

func NewInboxRepository(db DBTX) *InboxRepository {
	return &InboxRepository{db: db}
}

// TryInsert usa ON CONFLICT DO NOTHING sobre o índice único
// uq_inbox_consumer_message: é o próprio Postgres que decide, de
// forma atômica, se esta é a primeira vez que vemos esta mensagem —
// sem SELECT-then-INSERT, que teria uma corrida entre a checagem e a
// escrita sob múltiplas instâncias do consumidor.
func (r *InboxRepository) TryInsert(ctx context.Context, consumerName, messageID, payloadHash string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		INSERT INTO inbox_messages (id, consumer_name, message_id, payload_hash, completed_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT ON CONSTRAINT uq_inbox_consumer_message DO NOTHING
	`, uuid.New(), consumerName, messageID, payloadHash)
	if err != nil {
		return false, fmt.Errorf("registrando mensagem %s/%s na inbox: %w", consumerName, messageID, err)
	}
	return tag.RowsAffected() == 1, nil
}
