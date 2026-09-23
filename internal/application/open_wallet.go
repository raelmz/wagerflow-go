// Pacote application contém os casos de uso: orquestram entidades do
// domínio e repositórios, mas não são "regra de negócio" em si (essa
// mora nas entidades) — são o "roteiro" de uma operação inteira.
package application

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// OpenWalletUseCase implementa o contrato HTTP POST /wallets (seção 9
// do desafio). Repare que a única dependência é domain.TxRunner —
// não existe nenhum import de "postgres" aqui. O caso de uso não
// sabe (e não precisa saber) que por trás tem Postgres.
type OpenWalletUseCase struct {
	txRunner domain.TxRunner
}

func NewOpenWalletUseCase(txRunner domain.TxRunner) *OpenWalletUseCase {
	return &OpenWalletUseCase{txRunner: txRunner}
}

type OpenWalletResult struct {
	Wallet *domain.Wallet
}

// Execute abre uma carteira nova. Se initialBalance for positivo,
// cria também a WagerTransaction OPENING (já PROCESSED) e o
// WalletLedgerEntry de crédito correspondente — tudo no mesmo commit,
// conforme a seção 9 do desafio exige.
func (uc *OpenWalletUseCase) Execute(ctx context.Context, playerID uuid.UUID, initialBalance domain.Money) (*OpenWalletResult, error) {
	wallet, err := domain.NewWallet(playerID, initialBalance)
	if err != nil {
		return nil, fmt.Errorf("dados inválidos para abertura de carteira: %w", err)
	}

	err = uc.txRunner.WithinTransaction(ctx, func(uow domain.UnitOfWork) error {
		if err := uow.Wallets().Create(ctx, wallet); err != nil {
			return err
		}

		// Saldo inicial zero: a seção 9 do desafio diz explicitamente
		// que isso "não cria OPENING, ledger nem esses eventos
		// financeiros".
		if wallet.Balance().IsZero() {
			return nil
		}

		openingTx, err := domain.NewOpeningTransaction(wallet.ID(), wallet.PlayerID(), wallet.Balance())
		if err != nil {
			return err
		}
		if err := openingTx.MarkProcessed(wallet.Balance()); err != nil {
			return err
		}
		if err := uow.WagerTransactions().Create(ctx, openingTx); err != nil {
			return err
		}

		balanceBefore := domain.ZeroMoney(wallet.Currency())
		entry, err := domain.NewWalletLedgerEntry(
			wallet.ID(), openingTx.ID(), domain.DirectionCredit,
			wallet.Balance(), balanceBefore, wallet.Balance(),
		)
		if err != nil {
			return err
		}
		return uow.LedgerEntries().Create(ctx, entry)
	})
	if err != nil {
		return nil, err
	}

	return &OpenWalletResult{Wallet: wallet}, nil
}
