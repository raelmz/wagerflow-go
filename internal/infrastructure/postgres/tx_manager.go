package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// DBTX é o "denominador comum" entre *pgxpool.Pool (conexão avulsa)
// e pgx.Tx (transação em andamento) — os dois têm Exec e QueryRow
// com essa mesma assinatura. Os repositórios guardam um DBTX, então
// funcionam tanto soltos quanto dentro de uma transação.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// unitOfWork é a implementação real de domain.UnitOfWork: constrói
// os 3 repositórios sempre por cima do MESMO pgx.Tx, garantindo que
// todos enxerguem as mesmas mudanças ainda não commitadas.
type unitOfWork struct {
	tx DBTX
}

func (u *unitOfWork) Wallets() domain.WalletRepository { return NewWalletRepository(u.tx) }
func (u *unitOfWork) WagerTransactions() domain.WagerTransactionRepository {
	return NewWagerTransactionRepository(u.tx)
}
func (u *unitOfWork) LedgerEntries() domain.WalletLedgerEntryRepository {
	return NewWalletLedgerEntryRepository(u.tx)
}

// TxManager implementa domain.TxRunner usando transações reais do
// Postgres via pgx.
type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

// WithinTransaction abre uma transação, monta um UnitOfWork por cima
// dela, executa fn, e decide commit ou rollback pelo retorno de fn.
// Se fn retornar erro (ou der panic), TUDO é desfeito — é assim que
// "debita carteira + marca transação + grava ledger" vira uma coisa
// só, confirmada junto no mesmo commit.
func (tm *TxManager) WithinTransaction(ctx context.Context, fn func(uow domain.UnitOfWork) error) error {
	tx, err := tm.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // no-op se já houve commit

	uow := &unitOfWork{tx: tx}
	if err := fn(uow); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
