package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// LedgerDirection indica se o lançamento é um débito ou crédito.
type LedgerDirection string

const (
	DirectionDebit  LedgerDirection = "DEBIT"
	DirectionCredit LedgerDirection = "CREDIT"
)

var ErrLedgerBalanceMismatch = errors.New("balanceAfter não bate com balanceBefore ± valor, conforme a direção")

// WalletLedgerEntry é um lançamento IMUTÁVEL no livro-razão da carteira.
// Diferente de Wallet, aqui não existem métodos que alteram o estado
// depois de criado — uma vez construído, um WalletLedgerEntry nunca
// muda (é a garantia de "append-only" que o desafio exige: correções
// geram um NOVO lançamento, nunca editam um existente).
type WalletLedgerEntry struct {
	id             uuid.UUID
	walletID       uuid.UUID
	transactionID  uuid.UUID
	direction      LedgerDirection
	amount         Money
	balanceBefore  Money
	balanceAfter   Money
	createdAt      time.Time
}

// NewWalletLedgerEntry cria um lançamento, validando que a
// aritmética bate: balanceAfter deve ser exatamente
// balanceBefore + amount (crédito) ou balanceBefore - amount (débito).
// Essa validação é o que a seção 6.4 do desafio pede explicitamente
// na "construção" do lançamento — não confiamos que quem chamou já
// calculou certo, conferimos aqui de novo.
func NewWalletLedgerEntry(
	walletID uuid.UUID,
	transactionID uuid.UUID,
	direction LedgerDirection,
	amount Money,
	balanceBefore Money,
	balanceAfter Money,
) (*WalletLedgerEntry, error) {
	var expected Money
	var err error

	switch direction {
	case DirectionCredit:
		expected, err = balanceBefore.Add(amount)
	case DirectionDebit:
		expected, err = balanceBefore.Subtract(amount)
	default:
		return nil, ErrInvalidWagerData
	}
	if err != nil {
		return nil, err
	}
	if expected.String() != balanceAfter.String() {
		return nil, ErrLedgerBalanceMismatch
	}

	return &WalletLedgerEntry{
		id:            uuid.New(),
		walletID:      walletID,
		transactionID: transactionID,
		direction:     direction,
		amount:        amount,
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
		createdAt:     time.Now().UTC(),
	}, nil
}

// --- Getters (sem setters — lançamento é imutável) ---

func (e *WalletLedgerEntry) ID() uuid.UUID                 { return e.id }
func (e *WalletLedgerEntry) WalletID() uuid.UUID           { return e.walletID }
func (e *WalletLedgerEntry) TransactionID() uuid.UUID      { return e.transactionID }
func (e *WalletLedgerEntry) Direction() LedgerDirection    { return e.direction }
func (e *WalletLedgerEntry) Amount() Money                 { return e.amount }
func (e *WalletLedgerEntry) BalanceBefore() Money          { return e.balanceBefore }
func (e *WalletLedgerEntry) BalanceAfter() Money           { return e.balanceAfter }
func (e *WalletLedgerEntry) CreatedAt() time.Time          { return e.createdAt }
