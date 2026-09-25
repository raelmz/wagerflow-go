package messaging

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// SQSPinger é uma checagem de conectividade "barata" contra o SQS
// (LocalStack em dev, SQS de verdade em produção), usada só pelo
// GET /health/ready da API (seção 9 do desafio: readiness precisa
// cobrir mais que o Postgres). Não cria nem depende de nenhuma fila
// específica — quem garante que wager-transactions.fifo e
// wagerflow-events.fifo existem são os workers (SQSWagerConsumer e
// SQSEventPublisher), cada um a sua. Aqui só interessa saber se o
// broker responde.
type SQSPinger struct {
	client *sqs.Client
}

// NewSQSPinger monta o client apontando para o mesmo endpoint que os
// workers usam. Não faz nenhuma chamada de rede na construção — só
// no primeiro Ping — então não atrasa o boot da API.
func NewSQSPinger(endpointURL, region string) *SQSPinger {
	client := sqs.New(sqs.Options{
		Region: region,
		// LocalStack não valida credenciais de verdade, mas o SDK
		// exige QUE existam — mesmo par fixo de dev usado em
		// SQSEventPublisher/SQSWagerConsumer.
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		BaseEndpoint: aws.String(endpointURL),
	})
	return &SQSPinger{client: client}
}

// Ping usa ListQueues como sinal de vida: é a chamada mais barata da
// API do SQS que não depende de nenhuma fila já existir (ao
// contrário de GetQueueAttributes, por exemplo, que exigiria saber o
// nome de uma fila de antemão).
func (p *SQSPinger) Ping(ctx context.Context) error {
	maxResults := int32(1)
	if _, err := p.client.ListQueues(ctx, &sqs.ListQueuesInput{MaxResults: &maxResults}); err != nil {
		return fmt.Errorf("SQS indisponível: %w", err)
	}
	return nil
}
