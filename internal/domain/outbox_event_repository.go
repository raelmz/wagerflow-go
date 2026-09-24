package domain

import "context"

// OutboxRepository só precisa de Append: a outbox é escrita pelo caso
// de uso, dentro da MESMA transação SQL da mudança de estado (via
// UnitOfWork.Outbox()), e lida por um worker publicador separado.
//
// A leitura/publicação (reivindicar lote com SKIP LOCKED, marcar
// publicado, backoff) NÃO faz parte desta interface de propósito: é
// outra responsabilidade, com outro ciclo de vida, e entra junto com o
// worker publicador.
type OutboxRepository interface {
	Append(ctx context.Context, event *OutboxEvent) error
}
