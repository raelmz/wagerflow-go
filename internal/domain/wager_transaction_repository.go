package domain

import (
	"context"

	"github.com/google/uuid"
)

// WagerTransactionRepository define o que a camada de persistência
// precisa oferecer para WagerTransaction, sem dizer como.
type WagerTransactionRepository interface {
	Create(ctx context.Context, tx *WagerTransaction) error

	// Update persiste uma mudança de estado (ex: depois de chamar
	// tx.MarkProcessed()). Salva status, failureCode e updatedAt.
	Update(ctx context.Context, tx *WagerTransaction) error

	// FindByProviderAndIdempotencyKey é a consulta central da
	// idempotência HTTP/SQS (seção 9 do desafio): se já existe um
	// registro com essa chave, devolvemos o resultado persistido em
	// vez de reprocessar.
	FindByProviderAndIdempotencyKey(ctx context.Context, providerID, idempotencyKey string) (*WagerTransaction, error)

	// FindByProviderAndExternalTxID resolve referências de REFUND/ROLLBACK
	// (seção 7: "resolvido por (providerId, referenceExternalTransactionId)").
	FindByProviderAndExternalTxID(ctx context.Context, providerID, externalTransactionID string) (*WagerTransaction, error)

	// LockByProviderAndExternalTxID é igual à busca acima, mas trava a
	// linha até o fim da transação (SELECT ... FOR UPDATE). Usado ao
	// resolver a referência de uma reversão: duas reversões
	// concorrentes da mesma aposta ficam em fila aqui, e a segunda
	// já enxerga o resultado da primeira. Retorna (nil, nil) se não existir.
	LockByProviderAndExternalTxID(ctx context.Context, providerID, externalTransactionID string) (*WagerTransaction, error)

	// HasProcessedReversalOf diz se a transação referenceID já recebeu
	// uma reversão (REFUND ou ROLLBACK) em estado PROCESSED.
	HasProcessedReversalOf(ctx context.Context, referenceID uuid.UUID) (bool, error)
}
