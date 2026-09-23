package domain

import (
	"context"

	"github.com/google/uuid"
)

// --- Conceito Go 11: interface ---
// Uma interface em Go é só uma lista de métodos que um tipo PRECISA
// ter para "contar" como aquela interface — não existe "implements"
// explícito como em outras linguagens. Qualquer struct que tenha
// estes 4 métodos, com essas assinaturas exatas, automaticamente
// satisfaz WalletRepository, mesmo sem mencionar o nome dela.
//
// Por que isso importa aqui: o domínio (este arquivo) declara O QUE
// precisa existir para persistir uma carteira, mas não sabe COMO
// (Postgres, memória, outro banco). Quem implementa de verdade fica
// em internal/infrastructure/postgres — o domínio nunca importa pgx.
type WalletRepository interface {
	// Create insere uma carteira nova (usado na abertura de conta).
	Create(ctx context.Context, wallet *Wallet) error

	// FindByID busca uma carteira pelo ID interno.
	// Retorna (nil, nil) se não encontrar — quem chama decide o que
	// fazer com "não encontrado" (não usamos erro para isso, pois
	// não encontrar não é uma falha, é um resultado possível).
	FindByID(ctx context.Context, id uuid.UUID) (*Wallet, error)

	// FindByPlayerAndCurrency busca pela chave natural
	// (playerId, currency), usada para checar se já existe carteira
	// antes de abrir uma nova.
	FindByPlayerAndCurrency(ctx context.Context, playerID uuid.UUID, currency string) (*Wallet, error)

	// Debit executa o UPDATE atômico condicionado combinado em
	// docs/PROJETO.md: tenta debitar amount de walletID numa única
	// operação SQL que já verifica saldo suficiente na cláusula WHERE.
	// Retorna ErrInsufficientBalance se o saldo não for suficiente
	// (nenhuma linha é afetada nesse caso).
	Debit(ctx context.Context, walletID uuid.UUID, amount Money) (*Wallet, error)

	// Credit executa o crédito atômico equivalente (sempre bem-sucedido
	// se a carteira existir, já que crédito nunca deixa saldo negativo).
	Credit(ctx context.Context, walletID uuid.UUID, amount Money) (*Wallet, error)
}
