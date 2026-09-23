package postgres

import (
	"context"
	"fmt"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

type WalletLedgerEntryRepository struct {
	db DBTX
}

func NewWalletLedgerEntryRepository(db DBTX) *WalletLedgerEntryRepository {
	return &WalletLedgerEntryRepository{db: db}
}

func (r *WalletLedgerEntryRepository) Create(ctx context.Context, entry *domain.WalletLedgerEntry) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO wallet_ledger_entries (
			id, wallet_id, transaction_id, direction,
			amount_cents, balance_before_cents, balance_after_cents, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		entry.ID(), entry.WalletID(), entry.TransactionID(), string(entry.Direction()),
		entry.Amount().Cents(), entry.BalanceBefore().Cents(), entry.BalanceAfter().Cents(),
		entry.CreatedAt(),
	)
	if err != nil {
		return fmt.Errorf("falha ao inserir ledger entry: %w", err)
	}
	return nil
}
