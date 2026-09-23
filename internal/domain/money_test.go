package domain

import (
	"errors"
	"testing"
)

// --- Conceito Go 5: testes nativos ---
// Go não precisa de biblioteca externa pra testar. Basta uma função
// que comece com "Test", receba *testing.T, e esteja em arquivo
// "*_test.go". Rodamos tudo com "go test ./...".
func TestNewMoneyFromString_Valido(t *testing.T) {
	m, err := NewMoneyFromString("25.00", "brl")
	if err != nil {
		t.Fatalf("esperava sucesso, veio erro: %v", err)
	}
	if m.String() != "25.00" {
		t.Errorf("esperava \"25.00\", veio %q", m.String())
	}
	if m.Currency() != "BRL" {
		t.Errorf("esperava moeda normalizada BRL, veio %q", m.Currency())
	}
}

func TestNewMoneyFromString_RejeitaVazio(t *testing.T) {
	_, err := NewMoneyFromString("", "BRL")
	if !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("esperava ErrInvalidAmount, veio %v", err)
	}
}

func TestNewMoneyFromString_RejeitaNotacaoCientifica(t *testing.T) {
	_, err := NewMoneyFromString("1e10", "BRL")
	if !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("esperava ErrInvalidAmount, veio %v", err)
	}
}

func TestNewMoneyFromString_RejeitaEscalaErrada(t *testing.T) {
	casos := []string{"25", "25.0", "25.005"}
	for _, c := range casos {
		_, err := NewMoneyFromString(c, "BRL")
		if !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("caso %q: esperava ErrInvalidAmount, veio %v", c, err)
		}
	}
}

func TestNewMoneyFromString_RejeitaNegativo(t *testing.T) {
	_, err := NewMoneyFromString("-10.00", "BRL")
	if !errors.Is(err, ErrNegativeAmount) {
		t.Errorf("esperava ErrNegativeAmount, veio %v", err)
	}
}

func TestAdd_MesmaMoeda(t *testing.T) {
	a, _ := NewMoneyFromString("10.50", "BRL")
	b, _ := NewMoneyFromString("5.25", "BRL")
	result, err := a.Add(b)
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}
	if result.String() != "15.75" {
		t.Errorf("esperava 15.75, veio %s", result.String())
	}
}

func TestAdd_MoedasIncompativeis(t *testing.T) {
	a, _ := NewMoneyFromString("10.00", "BRL")
	b, _ := NewMoneyFromString("10.00", "USD")
	_, err := a.Add(b)
	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("esperava ErrCurrencyMismatch, veio %v", err)
	}
}

func TestGreaterThanOrEqual(t *testing.T) {
	saldo, _ := NewMoneyFromString("100.00", "BRL")
	aposta, _ := NewMoneyFromString("80.00", "BRL")

	ok, err := saldo.GreaterThanOrEqual(aposta)
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}
	if !ok {
		t.Error("esperava que 100.00 >= 80.00 fosse true")
	}
}

func TestZeroMoney(t *testing.T) {
	m := ZeroMoney("brl")
	if !m.IsZero() {
		t.Error("esperava valor zero")
	}
	if m.Currency() != "BRL" {
		t.Errorf("esperava BRL, veio %s", m.Currency())
	}
}
func TestMoney_Equals(t *testing.T) {
	a, _ := NewMoneyFromString("10.00", "BRL")
	b, _ := NewMoneyFromString("10.00", "brl") // moeda é normalizada
	c, _ := NewMoneyFromString("10.01", "BRL")
	d, _ := NewMoneyFromString("10.00", "USD")

	if !a.Equals(b) {
		t.Error("10.00 BRL deveria ser igual a 10.00 brl")
	}
	if a.Equals(c) {
		t.Error("10.00 não deveria ser igual a 10.01")
	}
	if a.Equals(d) {
		t.Error("BRL não deveria ser igual a USD, mesmo com o mesmo valor")
	}
}
