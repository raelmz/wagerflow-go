package domain

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestNewWallet_Valido(t *testing.T) {
	saldo, _ := NewMoneyFromString("1000.00", "BRL")
	w, err := NewWallet(uuid.New(), saldo)
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}
	if w.Version() != 1 {
		t.Errorf("esperava versão inicial 1, veio %d", w.Version())
	}
	if w.Balance().String() != "1000.00" {
		t.Errorf("esperava saldo 1000.00, veio %s", w.Balance().String())
	}
}

func TestNewWallet_RejeitaPlayerIDVazio(t *testing.T) {
	saldo := ZeroMoney("BRL")
	_, err := NewWallet(uuid.Nil, saldo)
	if !errors.Is(err, ErrInvalidPlayerID) {
		t.Errorf("esperava ErrInvalidPlayerID, veio %v", err)
	}
}

func TestDebit_ComSaldoSuficiente(t *testing.T) {
	saldo, _ := NewMoneyFromString("100.00", "BRL")
	w, _ := NewWallet(uuid.New(), saldo)

	aposta, _ := NewMoneyFromString("80.00", "BRL")
	err := w.Debit(aposta)
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}
	if w.Balance().String() != "20.00" {
		t.Errorf("esperava saldo 20.00, veio %s", w.Balance().String())
	}
	if w.Version() != 2 {
		t.Errorf("esperava versão 2 após débito, veio %d", w.Version())
	}
}

// Este é o teste que espelha o cenário obrigatório do desafio (seção 8):
// carteira com 100.00, duas apostas de 80.00. Aqui testamos a REGRA
// (uma passa, outra falha) — o teste de concorrência real com múltiplos
// processos entra depois, na camada de infraestrutura/integração.
func TestDebit_RejeitaSaldoInsuficienteNaSegundaAposta(t *testing.T) {
	saldo, _ := NewMoneyFromString("100.00", "BRL")
	w, _ := NewWallet(uuid.New(), saldo)
	aposta, _ := NewMoneyFromString("80.00", "BRL")

	if err := w.Debit(aposta); err != nil {
		t.Fatalf("primeira aposta deveria passar: %v", err)
	}
	err := w.Debit(aposta)
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Errorf("esperava ErrInsufficientBalance na segunda aposta, veio %v", err)
	}
	// Saldo deve continuar em 20.00, não pode ter sido debitado duas vezes.
	if w.Balance().String() != "20.00" {
		t.Errorf("esperava saldo 20.00 após rejeição, veio %s", w.Balance().String())
	}
}

func TestCredit(t *testing.T) {
	w, _ := NewWallet(uuid.New(), ZeroMoney("BRL"))
	valor, _ := NewMoneyFromString("50.00", "BRL")

	if err := w.Credit(valor); err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}
	if w.Balance().String() != "50.00" {
		t.Errorf("esperava saldo 50.00, veio %s", w.Balance().String())
	}
}

func TestDebit_RejeitaMoedaDiferente(t *testing.T) {
	w, _ := NewWallet(uuid.New(), ZeroMoney("BRL"))
	valorUSD, _ := NewMoneyFromString("10.00", "USD")

	err := w.Debit(valorUSD)
	if !errors.Is(err, ErrWalletCurrencyMismatch) {
		t.Errorf("esperava ErrWalletCurrencyMismatch, veio %v", err)
	}
}