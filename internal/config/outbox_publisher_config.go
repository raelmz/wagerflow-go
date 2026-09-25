package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// OutboxPublisherConfig é a configuração do cmd/outbox-publisher.
// Fica DELIBERADAMENTE separada de Config (usada pelo cmd/api): o
// publisher não sobe HTTP nem valida token, então não faz sentido
// obrigá-lo a ter HTTP_PORT/KEYCLOAK_ISSUER_URL setados só para
// iniciar — é outro binário, com outro conjunto de variáveis
// obrigatórias.
type OutboxPublisherConfig struct {
	DatabaseURL string

	// SQSEndpointURL é o endpoint do LocalStack (ex.:
	// http://localhost:4566, visto do HOST — mesmo raciocínio de
	// KEYCLOAK_ISSUER_URL em config.go).
	SQSEndpointURL string
	AWSRegion      string
	QueueName      string

	PollInterval time.Duration
	BatchSize    int
	LockTimeout  time.Duration
}

// LoadOutboxPublisherConfig lê as variáveis de ambiente obrigatórias e
// aplica defaults às opcionais (os números confirmados com o
// desenvolvedor antes da implementação: poll de 2s, lote de 20,
// timeout de lock de 2 minutos).
func LoadOutboxPublisherConfig() (*OutboxPublisherConfig, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("variável de ambiente DATABASE_URL é obrigatória")
	}

	sqsEndpointURL := os.Getenv("SQS_ENDPOINT_URL")
	if sqsEndpointURL == "" {
		return nil, fmt.Errorf("variável de ambiente SQS_ENDPOINT_URL é obrigatória")
	}

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1" // LocalStack aceita qualquer região; este é só um valor fixo de convenção.
	}

	queueName := os.Getenv("OUTBOX_QUEUE_NAME")
	if queueName == "" {
		queueName = "wagerflow-events.fifo"
	}

	pollInterval, err := durationEnv("OUTBOX_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		return nil, err
	}
	lockTimeout, err := durationEnv("OUTBOX_LOCK_TIMEOUT", 2*time.Minute)
	if err != nil {
		return nil, err
	}
	batchSize, err := intEnv("OUTBOX_BATCH_SIZE", 20)
	if err != nil {
		return nil, err
	}

	return &OutboxPublisherConfig{
		DatabaseURL:    databaseURL,
		SQSEndpointURL: sqsEndpointURL,
		AWSRegion:      region,
		QueueName:      queueName,
		PollInterval:   pollInterval,
		BatchSize:      batchSize,
		LockTimeout:    lockTimeout,
	}, nil
}

func durationEnv(name string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("variável de ambiente %s inválida (formato de duração do Go, ex. \"2s\", \"2m\"): %w", name, err)
	}
	return d, nil
}

func intEnv(name string, def int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("variável de ambiente %s inválida (esperava um número inteiro): %w", name, err)
	}
	return n, nil
}
