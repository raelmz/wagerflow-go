package domain

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestNewWalletLedgerEntry_DebitoValido(t *testing.T) {
	before, _ := NewMoneyFromString("100.00", "BRL")
	amount, _ := NewMoneyFromString("80.00", "BRL")
	after, _ := NewMoneyFromString("20.00", "BRL")

	entry, err := NewWalletLedgerEntry(uuid.New(), uuid.New(), DirectionDebit, amount, before, after)
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}
	if entry.BalanceAfter().String() != "20.00" {
		t.Errorf("esperava balanceAfter 20.00, veio %s", entry.BalanceAfter().String())
	}
}

func TestNewWalletLedgerEntry_CreditoValido(t *testing.T) {
	before := ZeroMoney("BRL")
	amount, _ := NewMoneyFromString("50.00", "BRL")
	after, _ := NewMoneyFromString("50.00", "BRL")

	_, err := NewWalletLedgerEntry(uuid.New(), uuid.New(), DirectionCredit, amount, before, after)
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}
}

// Este teste é a proteção central da seção 6.4: se alguém tentar
// construir um lançamento com a matemática errada, deve falhar.
func TestNewWalletLedgerEntry_RejeitaMatematicaErrada(t *testing.T) {
	before, _ := NewMoneyFromString("100.00", "BRL")
	amount, _ := NewMoneyFromString("80.00", "BRL")
	afterErrado, _ := NewMoneyFromString("30.00", "BRL") // deveria ser 20.00

	_, err := NewWalletLedgerEntry(uuid.New(), uuid.New(), DirectionDebit, amount, before, afterErrado)
	if !errors.Is(err, ErrLedgerBalanceMismatch) {
		t.Errorf("esperava ErrLedgerBalanceMismatch, veio %v", err)
	}
}
