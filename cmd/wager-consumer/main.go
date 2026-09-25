// cmd/wager-consumer é um binário SEPARADO do cmd/api e do
// cmd/outbox-publisher, pelo mesmo raciocínio dos dois: multi-instância
// é só subir mais cópias, sem Fx (poucas dependências, DI não compensa
// aqui).
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/config"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/messaging"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/postgres"
	"github.com/raelmz/wagerflow-go/internal/observability"
)

func main() {
	_ = godotenv.Load()

	logger := observability.NewLogger("wager-consumer")

	cfg, err := config.LoadWagerConsumerConfig()
	if err != nil {
		logger.Error("configuração inválida", "error", err.Error())
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("conectando ao Postgres", "error", err.Error())
		os.Exit(1)
	}
	defer pool.Close()

	txRunner := postgres.NewTxManager(pool)
	useCase := application.NewConsumeWagerTransactionUseCase(txRunner, cfg.ConsumerName)

	consumer, err := messaging.NewSQSWagerConsumer(
		ctx, cfg.SQSEndpointURL, cfg.AWSRegion, cfg.QueueName, cfg.MaxReceiveCount, useCase,
	)
	if err != nil {
		logger.Error("configurando consumidor SQS", "error", err.Error())
		os.Exit(1)
	}
	consumer.SetLogger(logger)

	logger.Info("wager-consumer iniciado", "consumerName", cfg.ConsumerName, "queue", cfg.QueueName)
	consumer.Run(ctx)
	logger.Info("wager-consumer encerrado", "consumerName", cfg.ConsumerName)
}
