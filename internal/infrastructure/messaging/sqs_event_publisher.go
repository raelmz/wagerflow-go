package messaging

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// SQSEventPublisher implementa domain.EventPublisher publicando numa
// ÚNICA fila FIFO — a seção 11 do desafio deixa o destino dos eventos
// de saída a critério do candidato; decidimos rotear por ATRIBUTO de
// mensagem (eventType), não por fila separada por tipo (ver
// docs/PROJETO.md para a justificativa completa).
type SQSEventPublisher struct {
	client   *sqs.Client
	queueURL string
}

// NewSQSEventPublisher cria o client apontando para o endpoint do
// LocalStack e garante que a fila FIFO existe. CreateQueue no
// SQS/LocalStack é IDEMPOTENTE: se a fila já existe com os mesmos
// atributos, só devolve a URL dela — por isso não existe um passo de
// bootstrap separado, o próprio processo cria a fila que precisa no
// boot (mesma filosofia do realm do Keycloak, que se importa sozinho).
func NewSQSEventPublisher(ctx context.Context, endpointURL, region, queueName string) (*SQSEventPublisher, error) {
	client := sqs.New(sqs.Options{
		Region: region,
		// LocalStack não valida credenciais de verdade — mas o SDK
		// exige QUE existam, então usamos um par fixo de dev,
		// nunca lido por nada real.
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		BaseEndpoint: aws.String(endpointURL),
	})

	out, err := client.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(queueName),
		Attributes: map[string]string{
			"FifoQueue": "true",
			// Usamos MessageDeduplicationId explícito (eventId), não
			// dedup automática por conteúdo — dois eventos diferentes
			// podem ter o mesmo payload textual em teoria (embora
			// improvável), e o eventId é a identidade real.
			"ContentBasedDeduplication": "false",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("criando/confirmando fila %s no SQS: %w", queueName, err)
	}

	return &SQSEventPublisher{client: client, queueURL: *out.QueueUrl}, nil
}

func (p *SQSEventPublisher) Publish(ctx context.Context, msg domain.OutboxMessage) error {
	_, err := p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(p.queueURL),
		MessageBody:            aws.String(string(msg.Body)),
		MessageGroupId:         aws.String(msg.AggregateID.String()),
		MessageDeduplicationId: aws.String(msg.DeduplicationID),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"eventType": {
				DataType:    aws.String("String"),
				StringValue: aws.String(msg.EventType),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("enviando evento %s (%s) ao SQS: %w", msg.DeduplicationID, msg.EventType, err)
	}
	return nil
}
