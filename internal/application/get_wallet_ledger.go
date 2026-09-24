package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// GetWalletLedgerUseCase implementa GET /wallets/:walletId/ledger
// (seção 9: "cursor opaco, ordenação estável").
type GetWalletLedgerUseCase struct {
	wallets domain.WalletRepository
	ledger  domain.WalletLedgerEntryRepository
}

func NewGetWalletLedgerUseCase(wallets domain.WalletRepository, ledger domain.WalletLedgerEntryRepository) *GetWalletLedgerUseCase {
	return &GetWalletLedgerUseCase{wallets: wallets, ledger: ledger}
}

type GetWalletLedgerResult struct {
	Entries    []*domain.WalletLedgerEntry
	NextCursor string
	// Currency é a moeda da carteira — os lançamentos não a guardam
	// individualmente (ver comentário em ListByWallet), então quem
	// monta a resposta HTTP usa este campo para formatar cada valor.
	Currency string
}

// Execute confere que a carteira existe (404 se não) e devolve a
// página de lançamentos pedida.
func (uc *GetWalletLedgerUseCase) Execute(ctx context.Context, walletID uuid.UUID, cursor string, limit int) (*GetWalletLedgerResult, error) {
	wallet, err := uc.wallets.FindByID(ctx, walletID)
	if err != nil {
		return nil, err
	}
	if wallet == nil {
		return nil, domain.ErrWalletNotFound
	}

	entries, nextCursor, err := uc.ledger.ListByWallet(ctx, walletID, cursor, limit)
	if err != nil {
		return nil, err
	}

	return &GetWalletLedgerResult{
		Entries:    entries,
		NextCursor: nextCursor,
		Currency:   wallet.Currency(),
	}, nil
}
