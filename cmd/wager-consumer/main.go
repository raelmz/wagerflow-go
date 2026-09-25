// cmd/wager-consumer é um binário SEPARADO do cmd/api e do
// cmd/outbox-publisher, pelo mesmo raciocínio dos dois: multi-instância
// é só subir mais cópias, sem Fx (poucas dependências, DI não compensa
// aqui).
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/config"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/messaging"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/postgres"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.LoadWagerConsumerConfig()
	if err != nil {
		log.Fatalf("configuração inválida: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("conectando ao Postgres: %v", err)
	}
	defer pool.Close()

	txRunner := postgres.NewTxManager(pool)
	useCase := application.NewConsumeWagerTransactionUseCase(txRunner, cfg.ConsumerName)

	consumer, err := messaging.NewSQSWagerConsumer(
		ctx, cfg.SQSEndpointURL, cfg.AWSRegion, cfg.QueueName, cfg.MaxReceiveCount, useCase,
	)
	if err != nil {
		log.Fatalf("configurando consumidor SQS: %v", err)
	}

	log.Printf("wager-consumer iniciado (consumerName=%s, fila=%s)", cfg.ConsumerName, cfg.QueueName)
	consumer.Run(ctx)
	log.Printf("wager-consumer (consumerName=%s) encerrado", cfg.ConsumerName)
}
