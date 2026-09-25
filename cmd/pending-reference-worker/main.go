// cmd/pending-reference-worker é um binário SEPARADO, no mesmo
// espírito do cmd/outbox-publisher e do cmd/wager-consumer: reivindica
// e reaplica WagerTransaction em PENDING_REFERENCE (seção 7 do
// desafio). Rodar mais de uma instância é só subir mais de um
// processo — a coordenação entre elas é só o lock lógico no banco (ver
// PendingReferenceRepository.Claim).
//
// Sem Uber Fx pelo mesmo motivo do outbox-publisher: poucas
// dependências, container de DI seria complexidade sem ganho.
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
	"github.com/raelmz/wagerflow-go/internal/infrastructure/postgres"
	"github.com/raelmz/wagerflow-go/internal/observability"
)

func main() {
	_ = godotenv.Load()

	logger := observability.NewLogger("pending-reference-worker")

	cfg, err := config.LoadPendingReferenceWorkerConfig()
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

	repo := postgres.NewPendingReferenceRepository(pool)
	txRunner := postgres.NewTxManager(pool)

	// workerID novo a cada boot — mesmo raciocínio do outbox-publisher:
	// não precisa ser estável entre reinícios, os locks antigos deste
	// processo expiram sozinhos pelo lockTimeout.
	workerID := uuid.New().String()

	useCase := application.NewRetryPendingReferenceUseCase(
		repo,
		txRunner,
		workerID,
		cfg.BatchSize,
		cfg.LockTimeout,
		application.ExponentialBackoff(60*time.Second), // mesmo teto do outbox-publisher
		cfg.MaxAttempts,
		cfg.TTL,
	)
	useCase.SetLogger(logger)

	logger.Info("pending-reference-worker iniciado",
		"workerId", workerID, "pollInterval", cfg.PollInterval, "batchSize", cfg.BatchSize,
		"lockTimeout", cfg.LockTimeout, "maxAttempts", cfg.MaxAttempts, "ttl", cfg.TTL)

	useCase.Run(ctx, cfg.PollInterval)

	logger.Info("pending-reference-worker encerrado", "workerId", workerID)
}
