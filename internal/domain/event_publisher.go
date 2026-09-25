package domain

import (
	"context"

	"github.com/google/uuid"
)

// OutboxMessage é o que o worker publicador entrega ao destino de
// saída dos eventos (hoje, uma fila SQS FIFO — seção 11 do desafio
// deixa esse destino a critério do candidato). Construído a partir de
// um PendingOutboxEvent já gravado: não recalcula nada do evento de
// negócio, só reempacota para o formato de transporte.
type OutboxMessage struct {
	// AggregateID vira o MessageGroupId da mensagem FIFO: garante que
	// eventos da MESMA carteira/transação cheguem em ordem a quem
	// consumir, mesmo com vários publishers enviando em paralelo.
	AggregateID uuid.UUID

	// EventType vai como atributo de mensagem (não no roteamento por
	// fila): um consumidor que só quer um tipo de evento filtra por
	// esse atributo, sem precisar abrir o body inteiro pra decidir se
	// quer aquela mensagem. Decisão registrada em docs/PROJETO.md.
	EventType string

	// DeduplicationID = eventId (estável mesmo em republicação — é a
	// mesma garantia que a seção 11 exige, agora também no nível do
	// SQS FIFO, não só no nosso próprio banco).
	DeduplicationID string

	// Body é o envelope JSON já serializado (OutboxEvent.Payload()) —
	// nada é montado de novo aqui.
	Body []byte
}

// EventPublisher isola o destino de saída dos eventos atrás de uma
// interface pequena — o worker publicador (em application/) não sabe
// que existe AWS/SQS por trás; poderia ser trocado por outra coisa sem
// tocar em domain/application.
type EventPublisher interface {
	Publish(ctx context.Context, msg OutboxMessage) error
}
