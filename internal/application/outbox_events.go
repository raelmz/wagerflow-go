package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// Este arquivo concentra COMO os casos de uso gravam eventos na outbox.
// Todas as funções recebem o UnitOfWork da transação em andamento: o
// evento entra no MESMO commit da mudança de estado que o originou.

// correlationIDKey é o tipo da chave usada para guardar o correlationId
// no context. Ser um tipo próprio (e privado) evita colisão com chaves
// de outros pacotes — é a convenção idiomática de Go para context.Value.
type correlationIDKey struct{}

// WithCorrelationID devolve um context carregando o correlationId da
// requisição/mensagem. A camada de entrada (middleware HTTP, consumidor
// SQS) chama isto uma vez; os casos de uso e os eventos leem depois.
//
// Por que context e não um campo em ProcessWagerCommand? O correlationId
// é dado de RASTREAMENTO, não de negócio: não entra no hash de
// idempotência e será usado também nos logs. Assim ele atravessa as
// camadas sem mudar a assinatura de cada função.
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, correlationID)
}

// correlationIDFor devolve o correlationId do context; se a entrada
// não informou nenhum, usa o id da transação como fallback (assim todo
// evento tem correlationId, e os eventos de uma mesma operação
// compartilham o mesmo valor).
func correlationIDFor(ctx context.Context, txn *domain.WagerTransaction) string {
	if id, ok := ctx.Value(correlationIDKey{}).(string); ok && id != "" {
		return id
	}
	return txn.ID().String()
}

// emitProcessed grava WagerTransactionProcessed e devolve o eventId,
// para o WalletBalanceChanged apontar para ele como causationId.
func emitProcessed(ctx context.Context, uow domain.UnitOfWork, txn *domain.WagerTransaction) (uuid.UUID, error) {
	event, err := domain.NewWagerTransactionProcessedEvent(txn, correlationIDFor(ctx, txn))
	if err != nil {
		return uuid.Nil, err
	}
	if err := uow.Outbox().Append(ctx, event); err != nil {
		return uuid.Nil, err
	}
	return event.ID(), nil
}

// emitBalanceChanged grava WalletBalanceChanged a partir do lançamento
// do ledger. causationID é o eventId do WagerTransactionProcessed.
func emitBalanceChanged(
	ctx context.Context,
	uow domain.UnitOfWork,
	txn *domain.WagerTransaction,
	entry *domain.WalletLedgerEntry,
	walletVersion int64,
	causationID uuid.UUID,
) error {
	event, err := domain.NewWalletBalanceChangedEvent(entry, walletVersion, correlationIDFor(ctx, txn), causationID)
	if err != nil {
		return err
	}
	return uow.Outbox().Append(ctx, event)
}

func emitRejected(ctx context.Context, uow domain.UnitOfWork, txn *domain.WagerTransaction) error {
	event, err := domain.NewWagerTransactionRejectedEvent(txn, correlationIDFor(ctx, txn))
	if err != nil {
		return err
	}
	return uow.Outbox().Append(ctx, event)
}

func emitPendingReference(ctx context.Context, uow domain.UnitOfWork, txn *domain.WagerTransaction) error {
	event, err := domain.NewWagerTransactionPendingReferenceEvent(txn, correlationIDFor(ctx, txn))
	if err != nil {
		return err
	}
	return uow.Outbox().Append(ctx, event)
}
