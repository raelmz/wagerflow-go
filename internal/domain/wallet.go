package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Erros específicos da carteira.
var (
	ErrInsufficientBalance    = errors.New("saldo insuficiente")
	ErrInvalidPlayerID        = errors.New("playerId inválido")
	ErrWalletCurrencyMismatch = errors.New("a moeda da operação não corresponde à moeda da carteira")
)

// --- Conceito Go 6: ponteiro (*Wallet) ---
// Repare que os métodos de Wallet abaixo usam "(w *Wallet)", não
// "(w Wallet)" como fizemos em Money. O asterisco (*) significa
// "ponteiro para Wallet" — ou seja, o método recebe o ENDEREÇO da
// struct original, não uma cópia. Isso é necessário aqui porque
// Debit/Credit precisam ALTERAR o saldo de verdade. Se usássemos
// receiver por valor (sem *), a alteração aconteceria só numa cópia
// e se perderia assim que o método terminasse.
//
// Regra prática: Money é um "valor" (não muda, você troca por um novo).
// Wallet é uma "entidade com identidade e estado" (o mesmo registro
// muda ao longo do tempo) — por isso usa ponteiro.
type Wallet struct {
	id        uuid.UUID
	playerID  uuid.UUID
	currency  string
	balance   Money
	version   int64 // incrementada a cada mudança de saldo (ver PROJETO.md, decisão de concorrência)
	createdAt time.Time
	updatedAt time.Time
}

// NewWallet cria uma carteira NOVA (não confundir com Rehydrate,
// que reconstrói uma carteira já existente vinda do banco).
//
// A versão inicial é sempre 1, conforme o desafio especifica.
func NewWallet(playerID uuid.UUID, initialBalance Money) (*Wallet, error) {
	if playerID == uuid.Nil {
		return nil, ErrInvalidPlayerID
	}
	if initialBalance.IsNegative() {
		return nil, ErrNegativeAmount
	}

	now := time.Now().UTC()
	return &Wallet{
		id:        uuid.New(),
		playerID:  playerID,
		currency:  initialBalance.Currency(),
		balance:   initialBalance,
		version:   1,
		createdAt: now,
		updatedAt: now,
	}, nil
}

// RehydrateWallet reconstrói uma carteira a partir de dados já
// persistidos no banco (usado pelo repositório, ao carregar uma
// carteira existente). NÃO deve reaplicar nenhuma movimentação —
// só "monta" a struct com o estado que já existia.
//
// --- Conceito Go 7: por que ID e versão vêm de fora aqui ---
// Em NewWallet, o ID é gerado agora (uuid.New()) porque a carteira
// é nova. Em RehydrateWallet, o ID já existe no banco — por isso
// ele é um parâmetro, não gerado de novo.
func RehydrateWallet(
	id uuid.UUID,
	playerID uuid.UUID,
	currency string,
	balance Money,
	version int64,
	createdAt time.Time,
	updatedAt time.Time,
) *Wallet {
	return &Wallet{
		id:        id,
		playerID:  playerID,
		currency:  currency,
		balance:   balance,
		version:   version,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}
}

// Debit tenta debitar amount do saldo. Retorna ErrInsufficientBalance
// se o saldo for menor que amount — essa é a invariante central do
// desafio: NUNCA deixar a carteira ficar negativa.
//
// IMPORTANTE (ligado à decisão de concorrência no PROJETO.md):
// este método representa a regra de negócio em memória, mas a
// garantia REAL contra concorrência vem do UPDATE atômico condicionado
// que o repositório vai executar no banco (UPDATE ... WHERE balance >= X).
// Este método aqui é o que roda depois de o banco já ter confirmado
// que a operação é segura — ele mantém o objeto em memória consistente
// com o que foi persistido.
func (w *Wallet) Debit(amount Money) error {
	if amount.Currency() != w.currency {
		return ErrWalletCurrencyMismatch
	}
	if amount.IsNegative() || amount.IsZero() {
		return ErrInvalidAmount
	}

	ok, err := w.balance.GreaterThanOrEqual(amount)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInsufficientBalance
	}

	newBalance, err := w.balance.Subtract(amount)
	if err != nil {
		return err
	}

	w.balance = newBalance
	w.version++
	w.updatedAt = time.Now().UTC()
	return nil
}

// Credit adiciona amount ao saldo (usado em WIN, REFUND, ROLLBACK
// de uma BET, e na abertura inicial com saldo positivo).
func (w *Wallet) Credit(amount Money) error {
	if amount.Currency() != w.currency {
		return ErrWalletCurrencyMismatch
	}
	if amount.IsNegative() || amount.IsZero() {
		return ErrInvalidAmount
	}

	newBalance, err := w.balance.Add(amount)
	if err != nil {
		return err
	}

	w.balance = newBalance
	w.version++
	w.updatedAt = time.Now().UTC()
	return nil
}

// --- Getters ---
// Assim como fizemos em Money, os campos de Wallet são privados
// (minúsculos) e só podem ser lidos por fora através destes métodos.
// Isso impede que outro pacote faça "wallet.balance = ..." direto
// e quebre a invariante de saldo sem passar por Debit/Credit.

func (w *Wallet) ID() uuid.UUID        { return w.id }
func (w *Wallet) PlayerID() uuid.UUID  { return w.playerID }
func (w *Wallet) Currency() string     { return w.currency }
func (w *Wallet) Balance() Money       { return w.balance }
func (w *Wallet) Version() int64       { return w.version }
func (w *Wallet) CreatedAt() time.Time { return w.createdAt }
func (w *Wallet) UpdatedAt() time.Time { return w.updatedAt }
