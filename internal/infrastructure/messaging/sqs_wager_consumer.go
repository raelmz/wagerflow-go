package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/raelmz/wagerflow-go/internal/application"
)

// SQSWagerConsumer consome wager-transactions.fifo e chama o caso de
// uso compartilhado com o canal HTTP. Erros permanentes (validação,
// conflito de idempotência) mandam a mensagem direto para a DLQ —
// não faz sentido esperar o redrive por maxReceiveCount para algo que
// nunca vai mudar de resultado. Erros transitórios simplesmente NÃO
// apagam a mensagem: ela volta a ficar visível após o
// VisibilityTimeout e o SQS tenta de novo sozinho (o redrive por
// maxReceiveCount é a rede de segurança para o caso de uma falha
// transitória virar, na prática, permanente — ex: um bug que sempre
// derruba o processamento daquela mensagem).
type SQSWagerConsumer struct {
	client     *sqs.Client
	queueURL   string
	dlqURL     string
	useCase    *application.ConsumeWagerTransactionUseCase
	maxWorkers int
}

// NewSQSWagerConsumer garante que a fila FIFO principal e a DLQ
// existam (CreateQueue é idempotente, mesma filosofia do
// SQSEventPublisher) e liga a fila principal à DLQ via RedrivePolicy.
func NewSQSWagerConsumer(
	ctx context.Context,
	endpointURL, region, queueName string,
	maxReceiveCount int,
	useCase *application.ConsumeWagerTransactionUseCase,
) (*SQSWagerConsumer, error) {
	client := sqs.New(sqs.Options{
		Region:       region,
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		BaseEndpoint: aws.String(endpointURL),
	})

	dlqName := queueName[:len(queueName)-len(".fifo")] + "-dlq.fifo"
	dlqOut, err := client.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(dlqName),
		Attributes: map[string]string{
			"FifoQueue": "true",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("criando/confirmando DLQ %s no SQS: %w", dlqName, err)
	}

	dlqAttrs, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       dlqOut.QueueUrl,
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return nil, fmt.Errorf("obtendo ARN da DLQ %s: %w", dlqName, err)
	}
	dlqArn := dlqAttrs.Attributes[string(types.QueueAttributeNameQueueArn)]

	redrivePolicy, err := json.Marshal(map[string]any{
		"deadLetterTargetArn": dlqArn,
		"maxReceiveCount":     maxReceiveCount,
	})
	if err != nil {
		return nil, fmt.Errorf("montando RedrivePolicy: %w", err)
	}

	mainOut, err := client.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(queueName),
		Attributes: map[string]string{
			"FifoQueue":                 "true",
			"ContentBasedDeduplication": "false",
			"RedrivePolicy":             string(redrivePolicy),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("criando/confirmando fila %s no SQS: %w", queueName, err)
	}

	return &SQSWagerConsumer{
		client:     client,
		queueURL:   *mainOut.QueueUrl,
		dlqURL:     *dlqOut.QueueUrl,
		useCase:    useCase,
		maxWorkers: 1,
	}, nil
}

// Run faz long-polling até ctx ser cancelado (SIGTERM/SIGINT — o
// mesmo padrão de shutdown limpo do outbox-publisher): a chamada
// ReceiveMessage em andamento retorna (ou por timeout de poll, ou
// cancelada pelo contexto), o lote atual termina de ser processado, e
// só então a função retorna. Nenhuma mensagem nova é reivindicada
// depois do cancelamento.
func (c *SQSWagerConsumer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		out, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(c.queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     10, // long polling
			VisibilityTimeout:   30,
		})
		if err != nil {
			if ctx.Err() != nil {
				return // cancelado durante o poll: encerra sem logar como erro
			}
			log.Printf("wager-consumer: erro no ReceiveMessage: %v", err)
			continue
		}

		for _, msg := range out.Messages {
			c.handle(ctx, msg)
		}
	}
}

func (c *SQSWagerConsumer) handle(ctx context.Context, msg types.Message) {
	messageID := aws.ToString(msg.MessageId)

	inbound, err := ParseWagerMessage(messageID, []byte(aws.ToString(msg.Body)))
	if err == nil {
		_, err = c.useCase.Execute(ctx, inbound)
	}

	switch {
	case err == nil:
		c.ack(ctx, msg)
	case application.IsPermanentWagerError(err):
		log.Printf("wager-consumer: mensagem %s rejeitada definitivamente (%v) — enviando à DLQ", messageID, err)
		c.deadLetter(ctx, msg)
	default:
		// Transitório: NÃO apaga a mensagem. Ela volta a ficar
		// visível após VisibilityTimeout e é tentada de novo.
		log.Printf("wager-consumer: erro transitório processando %s, mensagem voltará à fila: %v", messageID, err)
	}
}

func (c *SQSWagerConsumer) ack(ctx context.Context, msg types.Message) {
	_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(c.queueURL),
		ReceiptHandle: msg.ReceiptHandle,
	})
	if err != nil {
		log.Printf("wager-consumer: falha ao confirmar (delete) mensagem %s: %v", aws.ToString(msg.MessageId), err)
	}
}

// deadLetter publica explicitamente na DLQ e remove da fila
// principal. Não dependemos só do redrive automático por
// maxReceiveCount porque um erro de VALIDAÇÃO é permanente já na
// primeira tentativa — esperar a mensagem ser reentregue várias vezes
// só adiaria, sem necessidade, a hora de alguém olhar a DLQ.
func (c *SQSWagerConsumer) deadLetter(ctx context.Context, msg types.Message) {
	_, err := c.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(c.dlqURL),
		MessageBody:            msg.Body,
		MessageGroupId:         aws.String(aws.ToString(msg.MessageId)),
		MessageDeduplicationId: aws.String(aws.ToString(msg.MessageId)),
	})
	if err != nil {
		log.Printf("wager-consumer: falha ao enviar mensagem %s à DLQ, mantendo na fila principal para retry: %v",
			aws.ToString(msg.MessageId), err)
		return
	}
	c.ack(ctx, msg)
}
