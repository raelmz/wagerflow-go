package domain

import "context"

// WalletLedgerEntryRepository só precisa de Create: o ledger é
// append-only, então não existe Update nem Delete na interface —
// a impossibilidade de editar um lançamento já existe até no nível
// do contrato que o domínio expõe, não só na constraint do banco.
type WalletLedgerEntryRepository interface {
	Create(ctx context.Context, entry *WalletLedgerEntry) error
}
