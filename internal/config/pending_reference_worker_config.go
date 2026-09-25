package config

import (
	"fmt"
	"os"
	"time"
)

// PendingReferenceWorkerConfig é a configuração do
// cmd/pending-reference-worker. Separada de Config e das demais pelo
// mesmo motivo já documentado em outbox_publisher_config.go: este
// binário não sobe HTTP nem fala com SQS/Keycloak, só com o Postgres.
type PendingReferenceWorkerConfig struct {
	DatabaseURL string

	PollInterval time.Duration
	BatchSize    int
	LockTimeout  time.Duration

	// MaxAttempts e TTL são os dois limites confirmados com o
	// desenvolvedor antes da implementação: o worker desiste e
	// rejeita com REFERENCE_NOT_FOUND assim que QUALQUER um dos dois
	// se esgotar primeiro (o que vier antes).
	MaxAttempts int
	TTL         time.Duration
}

func LoadPendingReferenceWorkerConfig() (*PendingReferenceWorkerConfig, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("variável de ambiente DATABASE_URL é obrigatória")
	}

	pollInterval, err := durationEnv("PENDING_REFERENCE_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		return nil, err
	}
	lockTimeout, err := durationEnv("PENDING_REFERENCE_LOCK_TIMEOUT", 2*time.Minute)
	if err != nil {
		return nil, err
	}
	batchSize, err := intEnv("PENDING_REFERENCE_BATCH_SIZE", 20)
	if err != nil {
		return nil, err
	}
	maxAttempts, err := intEnv("PENDING_REFERENCE_MAX_ATTEMPTS", 5)
	if err != nil {
		return nil, err
	}
	ttl, err := durationEnv("PENDING_REFERENCE_TTL", 5*time.Minute)
	if err != nil {
		return nil, err
	}

	return &PendingReferenceWorkerConfig{
		DatabaseURL:  databaseURL,
		PollInterval: pollInterval,
		BatchSize:    batchSize,
		LockTimeout:  lockTimeout,
		MaxAttempts:  maxAttempts,
		TTL:          ttl,
	}, nil
}
