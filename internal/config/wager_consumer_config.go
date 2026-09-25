package config

import (
	"fmt"
	"os"
)

// WagerConsumerConfig é a configuração do cmd/wager-consumer. Fica
// separada de Config e de OutboxPublisherConfig pelo mesmo motivo já
// documentado em outbox_publisher_config.go: cada binário só exige as
// variáveis que de fato usa.
type WagerConsumerConfig struct {
	DatabaseURL string

	SQSEndpointURL string
	AWSRegion      string
	QueueName      string

	// ConsumerName identifica este consumidor na tabela
	// inbox_messages (coluna consumer_name). Fixo por design: todas
	// as instâncias deste binário compartilham o mesmo nome, porque
	// a deduplicação é por (consumerName, messageId), não por
	// instância — senão duas instâncias do MESMO consumidor lógico
	// poderiam processar a mesma mensagem duas vezes.
	ConsumerName string

	// MaxReceiveCount é o maxReceiveCount da RedrivePolicy da fila
	// principal: quantas vezes uma mensagem pode ser reentregue antes
	// do SQS mandá-la para a DLQ sozinho. É a rede de segurança para
	// falhas transitórias que nunca se resolvem; erros de validação
	// já vão direto à DLQ sem esperar por isso (ver
	// SQSWagerConsumer.handle).
	MaxReceiveCount int
}

func LoadWagerConsumerConfig() (*WagerConsumerConfig, error) {
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
		region = "us-east-1"
	}

	queueName := os.Getenv("WAGER_QUEUE_NAME")
	if queueName == "" {
		queueName = "wager-transactions.fifo"
	}

	consumerName := os.Getenv("WAGER_CONSUMER_NAME")
	if consumerName == "" {
		consumerName = "wager-consumer"
	}

	maxReceiveCount, err := intEnv("WAGER_MAX_RECEIVE_COUNT", 5)
	if err != nil {
		return nil, err
	}

	return &WagerConsumerConfig{
		DatabaseURL:     databaseURL,
		SQSEndpointURL:  sqsEndpointURL,
		AWSRegion:       region,
		QueueName:       queueName,
		ConsumerName:    consumerName,
		MaxReceiveCount: maxReceiveCount,
	}, nil
}
