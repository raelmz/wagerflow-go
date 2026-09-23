package domain

import "context"

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
}
