package domain

import (
	"errors"
	"math"
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

// --- Testes novos: bugs encontrados nos testes de integração ---

func TestNewMoneyFromString_RejeitaZeroComSinalNegativo(t *testing.T) {
	// Bug real: "-0.50" tinha parte inteira "-0", que ParseInt lê como
	// 0 — nem positivo nem negativo — então a checagem antiga (baseada
	// só no sinal de wholePart) deixava passar como +0.50.
	_, err := NewMoneyFromString("-0.50", "BRL")
	if !errors.Is(err, ErrNegativeAmount) {
		t.Fatalf("esperava ErrNegativeAmount para -0.50, veio: %v", err)
	}

	_, err = NewMoneyFromString("-0.00", "BRL")
	if !errors.Is(err, ErrNegativeAmount) {
		t.Fatalf("esperava ErrNegativeAmount para -0.00, veio: %v", err)
	}
}

func TestNewMoneyFromString_RejeitaSinalEscondidoNaParteDecimal(t *testing.T) {
	// Bug real: "25.+5" tinha parte decimal "+5", que ParseInt aceita
	// como int64 válido (5) — o valor virava 25.05 silenciosamente.
	_, err := NewMoneyFromString("25.+5", "BRL")
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("esperava ErrInvalidAmount para 25.+5, veio: %v", err)
	}

	_, err = NewMoneyFromString("+25.00", "BRL")
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("esperava ErrInvalidAmount para +25.00, veio: %v", err)
	}
}

func TestNewMoneyFromString_RejeitaOverflowNoParse(t *testing.T) {
	_, err := NewMoneyFromString("999999999999999999999.00", "BRL")
	if !errors.Is(err, ErrInvalidAmount) && !errors.Is(err, ErrAmountOverflow) {
		t.Fatalf("esperava erro de valor inválido/overflow, veio: %v", err)
	}
}

func TestAdd_DetectaOverflow(t *testing.T) {
	quaseMax := MoneyFromCents(math.MaxInt64-10, "BRL")
	umPouco := MoneyFromCents(100, "BRL")

	_, err := quaseMax.Add(umPouco)
	if !errors.Is(err, ErrAmountOverflow) {
		t.Fatalf("esperava ErrAmountOverflow, veio: %v", err)
	}
}

func TestSubtract_DetectaOverflow(t *testing.T) {
	quaseMin := MoneyFromCents(math.MinInt64+10, "BRL")
	umPouco := MoneyFromCents(100, "BRL")

	_, err := quaseMin.Subtract(umPouco)
	if !errors.Is(err, ErrAmountOverflow) {
		t.Fatalf("esperava ErrAmountOverflow, veio: %v", err)
	}
}

func TestMoney_Negate(t *testing.T) {
	credito, _ := NewMoneyFromString("25.00", "BRL")
	debito := credito.Negate()

	if !debito.IsNegative() {
		t.Error("esperava que o valor negado fosse negativo")
	}
	if debito.Negate().Equals(credito) == false {
		t.Error("negar duas vezes deveria devolver o valor original")
	}
	if debito.Currency() != credito.Currency() {
		t.Error("Negate não deveria mudar a moeda")
	}
}
