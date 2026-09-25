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
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/config"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/messaging"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/postgres"
	"github.com/raelmz/wagerflow-go/internal/observability"
)

func main() {
	_ = godotenv.Load()

	logger := observability.NewLogger("outbox-publisher")

	cfg, err := config.LoadOutboxPublisherConfig()
	if err != nil {
		logger.Error("configuração inválida", "error", err.Error())
		os.Exit(1)
	}

	// signal.NotifyContext cancela ctx no SIGTERM/SIGINT — é assim que
	// o "pare de reivindicar lote novo, termine o em andamento" da
	// seção 10 do desafio chega até o loop em Run().
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("conectando ao Postgres", "error", err.Error())
		os.Exit(1)
	}
	defer pool.Close()

	publisher, err := messaging.NewSQSEventPublisher(ctx, cfg.SQSEndpointURL, cfg.AWSRegion, cfg.QueueName)
	if err != nil {
		logger.Error("configurando publisher SQS", "error", err.Error())
		os.Exit(1)
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
	useCase.SetLogger(logger)

	logger.Info("outbox-publisher iniciado",
		"workerId", workerID, "queue", cfg.QueueName, "pollInterval", cfg.PollInterval,
		"batchSize", cfg.BatchSize, "lockTimeout", cfg.LockTimeout)

	useCase.Run(ctx, cfg.PollInterval)

	logger.Info("outbox-publisher encerrado", "workerId", workerID)
}
