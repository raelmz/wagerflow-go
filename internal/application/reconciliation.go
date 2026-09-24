package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// ReconciliationUseCase implementa POST /wallets/:walletId/reconciliation
// (seção 9): reconstrói o saldo a partir do ledger inteiro e compara
// com o saldo armazenado na carteira. NÃO altera nada — é só leitura,
// mas usa TxRunner mesmo assim, para que a leitura da carteira e a
// soma do ledger aconteçam na MESMA transação (mesma "foto" do banco,
// como o desafio pede: "em uma visão consistente dos dados").
type ReconciliationUseCase struct {
	txRunner domain.TxRunner
}

func NewReconciliationUseCase(txRunner domain.TxRunner) *ReconciliationUseCase {
	return &ReconciliationUseCase{txRunner: txRunner}
}

type ReconciliationResult struct {
	WalletID          uuid.UUID
	Currency          string
	StoredBalance     domain.Money
	CalculatedBalance domain.Money
	Difference        domain.Money // storedBalance - calculatedBalance
	Consistent        bool
	CheckedEntries    int
}

func (uc *ReconciliationUseCase) Execute(ctx context.Context, walletID uuid.UUID) (*ReconciliationResult, error) {
	var result *ReconciliationResult

	err := uc.txRunner.WithinTransaction(ctx, func(uow domain.UnitOfWork) error {
		wallet, err := uow.Wallets().FindByID(ctx, walletID)
		if err != nil {
			return err
		}
		if wallet == nil {
			return domain.ErrWalletNotFound
		}

		netCents, checked, err := uow.LedgerEntries().SumByWallet(ctx, walletID)
		if err != nil {
			return err
		}

		stored := wallet.Balance()
		calculated := domain.MoneyFromCents(netCents, wallet.Currency())

		// difference = stored - calculated. Se Subtract falhar por
		// moedas incompatíveis, não deveria acontecer aqui (ambos usam
		// wallet.Currency()) — mas propagamos o erro por segurança em
		// vez de ignorar.
		difference, err := stored.Subtract(calculated)
		if err != nil {
			return err
		}

		result = &ReconciliationResult{
			WalletID:          walletID,
			Currency:          wallet.Currency(),
			StoredBalance:     stored,
			CalculatedBalance: calculated,
			Difference:        difference,
			Consistent:        stored.Equals(calculated),
			CheckedEntries:    checked,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
