package domain

import "context"

// UnitOfWork agrupa os repositórios que precisam enxergar a MESMA
// transação SQL. Em vez de o caso de uso pedir "me dá um
// WalletRepository" e "me dá um WagerTransactionRepository"
// separados (arriscando pegar dois de transações diferentes por
// engano), ele pede um UnitOfWork e usa os repositórios de dentro
// dele — todos garantidamente compartilhando a mesma transação.
type UnitOfWork interface {
	Wallets() WalletRepository
	WagerTransactions() WagerTransactionRepository
	LedgerEntries() WalletLedgerEntryRepository

	// Outbox grava eventos de integração NA MESMA transação das
	// demais alterações — é isso que garante "evento só existe se o
	// fato foi confirmado, e todo fato confirmado tem seu evento".
	Outbox() OutboxRepository
}

// TxRunner é o que o caso de uso realmente depende — não sabe se por
// trás é Postgres, outro banco, ou até uma implementação em memória
// usada em teste. Isso é o que mantém application/ independente de
// infrastructure/, cumprindo a regra de "domínio independente de
// framework" que o desafio pede.
type TxRunner interface {
	WithinTransaction(ctx context.Context, fn func(uow UnitOfWork) error) error
}
