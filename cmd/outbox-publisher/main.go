// cmd/outbox-publisher é um binário SEPARADO do cmd/api, de propósito
// (seção 11 do desafio: "deve suportar múltiplos publishers"). Rodar
// mais de um publisher é só subir mais de uma instância deste binário
// — nenhuma coordenação extra é necessária além da que já existe no
// banco (ver Claim em
// internal/infrastructure/postgres/outbox_publisher_repository_postgres.go).
//
// Não usa Uber Fx: o desafio exige Fx para a composição da API HTTP
// (seção 3), não para todo binário do projeto. Este processo tem só 3
// dependências (pool, publisher, repositório) — um container de DI
// aqui seria complexidade sem ganho.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/config"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/messaging"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/postgres"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.LoadOutboxPublisherConfig()
	if err != nil {
		log.Fatalf("configuração inválida: %v", err)
	}

	// signal.NotifyContext cancela ctx no SIGTERM/SIGINT — é assim que
	// o "pare de reivindicar lote novo, termine o em andamento" da
	// seção 10 do desafio chega até o loop em Run().
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("conectando ao Postgres: %v", err)
	}
	defer pool.Close()

	publisher, err := messaging.NewSQSEventPublisher(ctx, cfg.SQSEndpointURL, cfg.AWSRegion, cfg.QueueName)
	if err != nil {
		log.Fatalf("configurando publisher SQS: %v", err)
	}

	repo := postgres.NewOutboxPublisherRepository(pool)

	// workerID novo a cada boot: identifica o dono do lock lógico
	// (locked_by) enquanto o processo estiver de pé. Não precisa ser
	// estável entre reinícios — se este processo morrer, o próximo
	// boot (deste ou de outro publisher) vai gerar um id diferente, e
	// os locks antigos deste processo vão expirar sozinhos pelo
	// lockTimeout (recuperação de trabalho abandonado).
	workerID := uuid.New().String()

	useCase := application.NewPublishPendingOutboxEventsUseCase(
		repo,
		publisher,
		workerID,
		cfg.BatchSize,
		cfg.LockTimeout,
		application.ExponentialBackoff(60*time.Second),
	)

	log.Printf("outbox-publisher iniciado (workerId=%s, fila=%s, poll=%s, lote=%d, lockTimeout=%s)",
		workerID, cfg.QueueName, cfg.PollInterval, cfg.BatchSize, cfg.LockTimeout)

	useCase.Run(ctx, cfg.PollInterval)

	log.Printf("outbox-publisher (workerId=%s) encerrado", workerID)
}
