package domain

import "context"

// InboxRepository implementa a deduplicação adicional de mensagens
// SQS (seção 6.5 e 10 do desafio), por cima da unicidade
// (consumerName, messageId) da migration 000004.
//
// Fica no MESMO UnitOfWork da transação que aplica o efeito
// financeiro: TryInsert e o processamento da mensagem commitam juntos
// ou desfazem juntos — nunca existe uma mensagem "marcada como vista"
// sem o efeito correspondente, nem o contrário.
type InboxRepository interface {
	// TryInsert tenta registrar (consumerName, messageId) com o hash
	// do corpo da mensagem. Devolve inserted=false quando a mensagem
	// já tinha sido vista antes por este consumidor (reentrega do
	// SQS) — nesse caso o chamador NÃO deve reprocessar o efeito
	// financeiro, apenas confirmar (ack) a mensagem como já
	// tratada.
	TryInsert(ctx context.Context, consumerName, messageID, payloadHash string) (inserted bool, err error)
}
