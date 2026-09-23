package domain

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestNewExternalWagerTransaction_BetValida(t *testing.T) {
	money, _ := NewMoneyFromString("25.00", "BRL")
	tx, err := NewExternalWagerTransaction(
		"transaction-123", "provider-a", "provider-a:transaction-123", "hash-abc",
		uuid.New(), uuid.New(), "round-987", "fortune-chimp",
		KindBet, money, "",
	)
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}
	if tx.Status() != StatusPending {
		t.Errorf("esperava status inicial PENDING, veio %s", tx.Status())
	}
}

func TestNewExternalWagerTransaction_RejeitaOpening(t *testing.T) {
	money, _ := NewMoneyFromString("10.00", "BRL")
	_, err := NewExternalWagerTransaction(
		"tx-1", "provider-a", "key-1", "hash",
		uuid.New(), uuid.New(), "round-1", "game-1",
		KindOpening, money, "",
	)
	if !errors.Is(err, ErrInvalidWagerData) {
		t.Errorf("esperava ErrInvalidWagerData ao tentar criar OPENING externamente, veio %v", err)
	}
}

func TestNewExternalWagerTransaction_LossExigeValorZero(t *testing.T) {
	naoZero, _ := NewMoneyFromString("10.00", "BRL")
	_, err := NewExternalWagerTransaction(
		"tx-1", "provider-a", "key-1", "hash",
		uuid.New(), uuid.New(), "round-1", "game-1",
		KindLoss, naoZero, "",
	)
	if !errors.Is(err, ErrInvalidWagerData) {
		t.Errorf("esperava ErrInvalidWagerData para LOSS com valor != 0, veio %v", err)
	}

	zero := ZeroMoney("BRL")
	_, err = NewExternalWagerTransaction(
		"tx-1", "provider-a", "key-1", "hash",
		uuid.New(), uuid.New(), "round-1", "game-1",
		KindLoss, zero, "",
	)
	if err != nil {
		t.Errorf("LOSS com valor 0.00 deveria ser válido, veio erro: %v", err)
	}
}

func TestNewExternalWagerTransaction_RefundExigeReferencia(t *testing.T) {
	money, _ := NewMoneyFromString("25.00", "BRL")
	_, err := NewExternalWagerTransaction(
		"tx-2", "provider-a", "key-2", "hash",
		uuid.New(), uuid.New(), "round-1", "game-1",
		KindRefund, money, "", // sem referência
	)
	if !errors.Is(err, ErrMissingReference) {
		t.Errorf("esperava ErrMissingReference, veio %v", err)
	}
}

func TestMarkProcessed_NaoPermiteTransicaoAposTerminal(t *testing.T) {
	money, _ := NewMoneyFromString("25.00", "BRL")
	tx, _ := NewExternalWagerTransaction(
		"tx-3", "provider-a", "key-3", "hash",
		uuid.New(), uuid.New(), "round-1", "game-1",
		KindBet, money, "",
	)

	if err := tx.MarkProcessed(); err != nil {
		t.Fatalf("não esperava erro ao marcar como processada: %v", err)
	}
	// Tentar transicionar de novo deve falhar — replay não reaplica.
	err := tx.MarkRejected("SOME_CODE")
	if !errors.Is(err, ErrTransactionAlreadyTerminal) {
		t.Errorf("esperava ErrTransactionAlreadyTerminal, veio %v", err)
	}
}

func TestMarkRejected_ExigeFailureCode(t *testing.T) {
	money, _ := NewMoneyFromString("25.00", "BRL")
	tx, _ := NewExternalWagerTransaction(
		"tx-4", "provider-a", "key-4", "hash",
		uuid.New(), uuid.New(), "round-1", "game-1",
		KindBet, money, "",
	)
	err := tx.MarkRejected("")
	if !errors.Is(err, ErrInvalidWagerData) {
		t.Errorf("esperava ErrInvalidWagerData para failureCode vazio, veio %v", err)
	}
}

func TestNewOpeningTransaction_RejeitaValorZero(t *testing.T) {
	_, err := NewOpeningTransaction(uuid.New(), uuid.New(), ZeroMoney("BRL"))
	if !errors.Is(err, ErrInvalidWagerData) {
		t.Errorf("esperava ErrInvalidWagerData para saldo inicial zero, veio %v", err)
	}
}
