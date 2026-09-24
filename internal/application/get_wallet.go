package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// GetWalletUseCase implementa GET /wallets/:walletId (seção 9).
// Não precisa de transação (é uma leitura simples fora do fluxo
// financeiro), então depende só do repositório, não de TxRunner.
type GetWalletUseCase struct {
	wallets domain.WalletRepository
}

func NewGetWalletUseCase(wallets domain.WalletRepository) *GetWalletUseCase {
	return &GetWalletUseCase{wallets: wallets}
}

// Execute devolve domain.ErrWalletNotFound se não existir — o handler
// HTTP mapeia isso para 404.
func (uc *GetWalletUseCase) Execute(ctx context.Context, walletID uuid.UUID) (*domain.Wallet, error) {
	wallet, err := uc.wallets.FindByID(ctx, walletID)
	if err != nil {
		return nil, err
	}
	if wallet == nil {
		return nil, domain.ErrWalletNotFound
	}
	return wallet, nil
}
