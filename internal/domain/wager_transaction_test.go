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

	if err := tx.MarkProcessed(money); err != nil {
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

func TestMarkProcessed_GuardaSaldoResultante(t *testing.T) {
	money, _ := NewMoneyFromString("25.00", "BRL")
	tx, _ := NewExternalWagerTransaction(
		"tx-5", "provider-a", "key-5", "hash",
		uuid.New(), uuid.New(), "round-1", "game-1",
		KindBet, money, "",
	)

	// Enquanto não concluiu, não existe saldo resultante.
	if _, ok := tx.ResultingBalance(); ok {
		t.Fatal("transação PENDING não deveria ter saldo resultante")
	}

	saldo, _ := NewMoneyFromString("975.00", "BRL")
	if err := tx.MarkProcessed(saldo); err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}
	got, ok := tx.ResultingBalance()
	if !ok || !got.Equals(saldo) {
		t.Errorf("esperava saldo resultante 975.00, veio %v (ok=%v)", got, ok)
	}
}

func TestResolveReference(t *testing.T) {
	money, _ := NewMoneyFromString("25.00", "BRL")
	tx, _ := NewExternalWagerTransaction(
		"tx-6", "provider-a", "key-6", "hash",
		uuid.New(), uuid.New(), "round-1", "game-1",
		KindRefund, money, "bet-1",
	)

	if err := tx.ResolveReference(uuid.Nil); !errors.Is(err, ErrInvalidWagerData) {
		t.Errorf("esperava ErrInvalidWagerData para referência nula, veio %v", err)
	}

	refID := uuid.New()
	if err := tx.ResolveReference(refID); err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}
	if tx.ResolvedReferenceID() != refID {
		t.Errorf("referência resolvida não foi guardada")
	}

	// Depois de terminal, não pode mais mudar.
	_ = tx.MarkRejected("ANY_CODE")
	if err := tx.ResolveReference(uuid.New()); !errors.Is(err, ErrTransactionAlreadyTerminal) {
		t.Errorf("esperava ErrTransactionAlreadyTerminal, veio %v", err)
	}
}

func TestMarkPendingReference_PodeSeguirParaProcessed(t *testing.T) {
	// PENDING_REFERENCE NÃO é terminal: o worker de referências
	// precisa conseguir concluir a transação depois.
	money, _ := NewMoneyFromString("25.00", "BRL")
	tx, _ := NewExternalWagerTransaction(
		"tx-7", "provider-a", "key-7", "hash",
		uuid.New(), uuid.New(), "round-1", "game-1",
		KindRefund, money, "bet-1",
	)
	if err := tx.MarkPendingReference(); err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}
	if err := tx.MarkProcessed(money); err != nil {
		t.Errorf("PENDING_REFERENCE deveria poder virar PROCESSED, veio erro: %v", err)
	}
}
