package application

import (
	"context"
	"log"
	"time"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// BackoffFunc calcula o atraso antes da PRÓXIMA tentativa, dado o
// número de tentativas já feitas (contando a que acabou de falhar
// agora — ou seja, na primeira falha, attempts = 1).
type BackoffFunc func(attempts int) time.Duration

// ExponentialBackoff implementa a fórmula confirmada com o
// desenvolvedor antes da implementação: 1s, 2s, 4s, 8s... dobrando a
// cada tentativa, até o teto (`cap`) — no worker publicador, 60s.
func ExponentialBackoff(cap time.Duration) BackoffFunc {
	return func(attempts int) time.Duration {
		if attempts < 1 {
			attempts = 1
		}
		shift := attempts - 1
		if shift > 62 { // evita overflow do shift (1<<63 estoura int64)
			return cap
		}
		d := time.Duration(1<<uint(shift)) * time.Second
		if d <= 0 || d > cap {
			return cap
		}
		return d
	}
}

// PublishPendingOutboxEventsUseCase é o worker publicador da seção 11
// do desafio: reivindica um lote de eventos pendentes da outbox e
// tenta entregá-los ao destino de saída, um por um, FORA da transação
// que os reivindicou (nunca segura o banco esperando uma chamada de
// rede).
type PublishPendingOutboxEventsUseCase struct {
	repo        domain.OutboxPublisherRepository
	publisher   domain.EventPublisher
	workerID    string
	batchSize   int
	lockTimeout time.Duration
	backoff     BackoffFunc
}

func NewPublishPendingOutboxEventsUseCase(
	repo domain.OutboxPublisherRepository,
	publisher domain.EventPublisher,
	workerID string,
	batchSize int,
	lockTimeout time.Duration,
	backoff BackoffFunc,
) *PublishPendingOutboxEventsUseCase {
	return &PublishPendingOutboxEventsUseCase{
		repo:        repo,
		publisher:   publisher,
		workerID:    workerID,
		batchSize:   batchSize,
		lockTimeout: lockTimeout,
		backoff:     backoff,
	}
}

// RunOnce reivindica e tenta publicar UM lote. Devolve quantos eventos
// foram REIVINDICADOS (não quantos tiveram sucesso) — é o suficiente
// para quem chama decidir se o poll deveria ter sido mais frequente;
// sucesso/falha de cada evento já fica registrado no próprio banco
// (published_at ou attempts/next_attempt_at).
func (uc *PublishPendingOutboxEventsUseCase) RunOnce(ctx context.Context) (int, error) {
	events, err := uc.repo.Claim(ctx, uc.workerID, uc.batchSize, uc.lockTimeout)
	if err != nil {
		return 0, err
	}

	for _, event := range events {
		msg := domain.OutboxMessage{
			AggregateID:     event.AggregateID,
			EventType:       event.EventType,
			DeduplicationID: event.ID.String(),
			Body:            event.Payload,
		}

		if err := uc.publisher.Publish(ctx, msg); err != nil {
			attempts := event.Attempts + 1
			delay := uc.backoff(attempts)
			if markErr := uc.repo.MarkFailed(ctx, event.ID, uc.workerID, time.Now().Add(delay)); markErr != nil {
				log.Printf("outbox-publisher: evento %s falhou ao publicar E falhou ao registrar a tentativa (próxima tentativa pode demorar mais que o esperado): publicação=%v, registro=%v", event.ID, err, markErr)
				continue
			}
			log.Printf("outbox-publisher: falha ao publicar evento %s (tentativa %d, próxima em %s): %v", event.ID, attempts, delay, err)
			continue
		}

		if err := uc.repo.MarkPublished(ctx, event.ID, uc.workerID); err != nil {
			// A mensagem JÁ foi entregue ao destino aqui — não há como
			// "desfazer" isso. No pior caso, este evento fica com
			// published_at nulo, é reivindicado de novo depois do
			// lockTimeout e reenviado — seguro, porque
			// MessageDeduplicationId = eventId faz o SQS FIFO
			// descartar o duplicado dentro da janela dele, e um
			// consumidor correto também deve ser idempotente por
			// eventId (mesma garantia que a inbox dá do lado de
			// entrada).
			log.Printf("outbox-publisher: evento %s publicado com sucesso, mas falhou ao marcar published_at: %v", event.ID, err)
		}
	}

	return len(events), nil
}

// Run mantém o loop de polling até o contexto ser cancelado (SIGTERM
// chega até aqui via context.Context — ver cmd/outbox-publisher/main.go).
// O intervalo entre polls é fixo de propósito, mesmo quando um lote
// vem cheio: um worker que acelera sozinho quando há muito trabalho
// fica mais difícil de prever com várias instâncias rodando ao mesmo
// tempo — se sobrar trabalho, a resposta é subir mais uma instância,
// não este worker "pisar no acelerador" sozinho.
func (uc *PublishPendingOutboxEventsUseCase) Run(ctx context.Context, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := uc.RunOnce(ctx); err != nil {
				log.Printf("outbox-publisher: falha ao reivindicar lote da outbox: %v", err)
			}
		}
	}
}
