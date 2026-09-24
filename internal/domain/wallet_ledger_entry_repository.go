package domain

import (
	"context"

	"github.com/google/uuid"
)

// WalletLedgerEntryRepository: o ledger é append-only, então não
// existe Update nem Delete na interface — a impossibilidade de
// editar um lançamento já existe até no nível do contrato que o
// domínio expõe, não só na constraint do banco.
type WalletLedgerEntryRepository interface {
	Create(ctx context.Context, entry *WalletLedgerEntry) error

	// ListByWallet devolve uma página de lançamentos de walletID,
	// em ordem estável (createdAt, id, ambos crescentes), usada por
	// GET /wallets/:walletId/ledger (seção 9: "cursor opaco,
	// ordenação estável"). cursor vazio pede a primeira página.
	// nextCursor vem vazio quando não há mais páginas.
	ListByWallet(ctx context.Context, walletID uuid.UUID, cursor string, limit int) (entries []*WalletLedgerEntry, nextCursor string, err error)

	// SumByWallet reconstrói o saldo de walletID a partir do ledger
	// inteiro (créditos - débitos), usado pela reconciliação (seção 9:
	// "reconstrua o saldo a partir do ledger, incluindo a abertura").
	// Roda dentro da MESMA transação da leitura da carteira (o caso de
	// uso de reconciliação garante isso via TxRunner), para comparar
	// os dois valores numa visão consistente dos dados.
	SumByWallet(ctx context.Context, walletID uuid.UUID) (netCents int64, checkedEntries int, err error)
}
