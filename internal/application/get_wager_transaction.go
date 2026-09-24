package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// ErrWagerTransactionNotFound sinaliza 404 nas duas consultas deste
// caso de uso. Não é um erro do domínio (não representa uma regra de
// negócio quebrada), então fica aqui, na camada de aplicação.
var ErrWagerTransactionNotFound = domainNotFoundError("transação não encontrada")

// domainNotFoundError é só um alias pequeno para criar o erro acima
// sem importar "errors" só para isso num arquivo de uma linha.
type domainNotFoundError string

func (e domainNotFoundError) Error() string { return string(e) }

// GetWagerTransactionUseCase implementa as duas consultas de
// transação da seção 9: por ID interno e por (providerId,
// externalTransactionId) — usada para "acompanhar pendências e
// consultar códigos de rejeição ou falha".
type GetWagerTransactionUseCase struct {
	transactions domain.WagerTransactionRepository
}

func NewGetWagerTransactionUseCase(transactions domain.WagerTransactionRepository) *GetWagerTransactionUseCase {
	return &GetWagerTransactionUseCase{transactions: transactions}
}

// ByID implementa GET /wagering/transactions/:transactionId.
func (uc *GetWagerTransactionUseCase) ByID(ctx context.Context, id uuid.UUID) (*domain.WagerTransaction, error) {
	tx, err := uc.transactions.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, ErrWagerTransactionNotFound
	}
	return tx, nil
}

// ByProviderAndExternalID implementa
// GET /providers/:providerId/wagering/transactions/:externalTransactionId.
func (uc *GetWagerTransactionUseCase) ByProviderAndExternalID(ctx context.Context, providerID, externalTransactionID string) (*domain.WagerTransaction, error) {
	tx, err := uc.transactions.FindByProviderAndExternalTxID(ctx, providerID, externalTransactionID)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, ErrWagerTransactionNotFound
	}
	return tx, nil
}
