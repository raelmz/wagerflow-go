// Pacote domain contém as entidades e regras de negócio do sistema.
// IMPORTANTE: este pacote NÃO pode importar nada de Fx, HTTP, SQS ou
// bibliotecas de banco de dados. Ele precisa funcionar sozinho, testável
// com "go test" puro, sem subir nenhuma infraestrutura. Essa é a regra
// de "domínio independente de framework" que o desafio pede.
package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// --- Conceito Go 1: erros como valores ---
// Em Go não existe try/catch. Uma função que pode falhar retorna um
// segundo valor do tipo "error". `errors.New(...)` cria um erro simples,
// e comparamos erros com `errors.Is(err, ErrX)` em vez de capturar
// exceções. As variáveis abaixo são os erros "conhecidos" do domínio.
var (
	ErrInvalidAmount    = errors.New("valor monetário inválido")
	ErrNegativeAmount   = errors.New("valor monetário não pode ser negativo em entradas externas")
	ErrCurrencyMismatch = errors.New("moedas incompatíveis para esta operação")
	ErrAmountOverflow   = errors.New("valor monetário excede o limite representável")
)

// --- Conceito Go 2: struct ---
// Uma struct é como um "objeto" sem herança: só agrupa campos.
// Aqui os campos são minúsculos (amountCents, currency) — em Go,
// identificador que começa com minúscula é PRIVADO ao pacote.
// Isso é de propósito: ninguém fora deste arquivo pode fazer
// `Money{amountCents: -999}` e criar um valor inválido "por fora".
// A ÚNICA forma de criar um Money válido é pelas funções abaixo
// (NewMoney, ZeroMoney), que validam antes de construir.
type Money struct {
	amountCents int64  // valor em centavos (ex: R$ 25,00 = 2500)
	currency    string // código ISO 4217, ex: "BRL"
}

// ZeroMoney cria um Money zerado na moeda informada.
// Usado, por exemplo, como saldo inicial de uma carteira nova.
func ZeroMoney(currency string) Money {
	return Money{amountCents: 0, currency: normalizeCurrency(currency)}
}

// NewMoneyFromString cria um Money a partir de uma string decimal,
// exatamente como chega no contrato HTTP: {"amount":"25.00","currency":"BRL"}.
//
// --- Conceito Go 3: múltiplos retornos ---
// Repare que a função devolve DOIS valores: (Money, error).
// É o padrão idiomático em Go para "aqui está o resultado, e aqui
// está se algo deu errado". Quem chama SEMPRE deve checar o erro
// antes de usar o resultado.
func NewMoneyFromString(amount string, currency string) (Money, error) {
	raw := strings.TrimSpace(amount)
	currency = normalizeCurrency(currency)

	if raw == "" {
		return Money{}, fmt.Errorf("%w: valor vazio", ErrInvalidAmount)
	}

	// Rejeita notação científica explicitamente (ex: "1e10"), porque
	// o desafio exige isso e strconv.ParseFloat aceitaria silenciosamente.
	if strings.ContainsAny(raw, "eE") {
		return Money{}, fmt.Errorf("%w: notação científica não é permitida", ErrInvalidAmount)
	}

	// O sinal é tratado separadamente, ANTES de dividir em parte
	// inteira/decimal. Corrige um bug real: "-0.50" tinha wholePart
	// "-0", que o ParseInt lê como 0 (nem positivo nem negativo) — a
	// checagem antiga de "wholePart < 0" nunca disparava para esse
	// caso e o valor virava +0.50 silenciosamente.
	negative := strings.HasPrefix(raw, "-")
	numeric := raw
	if negative {
		numeric = raw[1:]
	}

	// Exige exatamente duas casas decimais (escala fixa), como o
	// desafio pede: "25.00" é válido, "25.0" ou "25.005" não são.
	parts := strings.Split(numeric, ".")
	if len(parts) != 2 || len(parts[1]) != 2 {
		return Money{}, fmt.Errorf("%w: valor deve ter exatamente duas casas decimais", ErrInvalidAmount)
	}

	// Só dígitos em cada parte. Sem isso, ParseInt aceita prefixos
	// "+"/"-" próprios (ex: "25.+5" virava 25.05, porque "+5" é um
	// int64 válido para o parser, mesmo não sendo um dígito puro).
	if !isDigitsOnly(parts[0]) || !isDigitsOnly(parts[1]) {
		return Money{}, fmt.Errorf("%w: apenas dígitos são permitidos", ErrInvalidAmount)
	}

	// Convertemos a parte inteira e a parte decimal separadamente,
	// para montar o total em centavos sem nunca passar por float.
	wholePart, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("%w: %v", ErrInvalidAmount, err)
	}
	centsPart, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("%w: %v", ErrInvalidAmount, err)
	}

	// Checagem de overflow: se wholePart já é gigantesco, multiplicar
	// por 100 pode "estourar" o int64. Fazemos a conta seguindo essa
	// checagem antes de somar.
	const maxSafeWhole = (1<<63 - 1) / 100
	if wholePart > maxSafeWhole {
		return Money{}, ErrAmountOverflow
	}

	total := wholePart*100 + centsPart

	if negative {
		// Entradas externas nunca podem ser negativas, mesmo "-0.00":
		// o sinal em si já indica uma intenção inválida para este
		// contrato (seção 6.1 exige valor >= 0 na entrada).
		return Money{}, ErrNegativeAmount
	}

	return Money{amountCents: total, currency: currency}, nil
}

// isDigitsOnly diz se s é composto só por dígitos ASCII (0-9) e não é
// vazio. Usada para recusar sinais ("+"/"-") escondidos dentro da
// parte inteira ou decimal de um valor.
func isDigitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Add soma dois valores monetários. Retorna erro se as moedas
// forem diferentes — nunca faz sentido somar BRL com USD direto.
//
// --- Conceito Go 4: método com receiver ---
// `func (m Money) Add(...)` significa "este é um método do tipo Money".
// `m` aqui é uma CÓPIA do Money original (Go passa struct por valor
// por padrão) — por isso Money é seguro de usar sem se preocupar
// com alguém alterando o valor por baixo dos panos.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	sum := m.amountCents + other.amountCents
	// Overflow de int64: se somamos algo positivo e o resultado ficou
	// MENOR que m, ou somamos algo negativo e o resultado ficou MAIOR
	// que m, o valor "deu a volta". É o jeito padrão de detectar
	// overflow em soma de inteiros com sinal sem usar tipos maiores.
	if (other.amountCents > 0 && sum < m.amountCents) || (other.amountCents < 0 && sum > m.amountCents) {
		return Money{}, ErrAmountOverflow
	}
	return Money{amountCents: sum, currency: m.currency}, nil
}

// Subtract subtrai other de m. O resultado pode ser negativo
// (usado em cálculos internos, como diferenças) — quem não permite
// negativo é a Wallet, na hora de aplicar um débito, não o Money em si.
func (m Money) Subtract(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	diff := m.amountCents - other.amountCents
	if (other.amountCents < 0 && diff < m.amountCents) || (other.amountCents > 0 && diff > m.amountCents) {
		return Money{}, ErrAmountOverflow
	}
	return Money{amountCents: diff, currency: m.currency}, nil
}

// Negate devolve o mesmo valor com o sinal invertido, na mesma moeda.
// Exigido pela seção 6.1 do desafio; usado, por exemplo, para expressar
// um débito como "o negativo de um crédito" em cálculos internos.
func (m Money) Negate() Money {
	return Money{amountCents: -m.amountCents, currency: m.currency}
}

// IsNegative diz se o valor é negativo.
func (m Money) IsNegative() bool {
	return m.amountCents < 0
}

// IsZero diz se o valor é exatamente zero.
func (m Money) IsZero() bool {
	return m.amountCents == 0
}

// GreaterThanOrEqual compara m >= other, exigindo mesma moeda.
func (m Money) GreaterThanOrEqual(other Money) (bool, error) {
	if m.currency != other.currency {
		return false, ErrCurrencyMismatch
	}
	return m.amountCents >= other.amountCents, nil
}

// Equals diz se dois valores são idênticos: mesma moeda E mesmo valor.
// Diferente de GreaterThanOrEqual, não devolve erro em moedas
// diferentes — para igualdade, "BRL 10.00" e "USD 10.00" simplesmente
// não são iguais. Usado, por exemplo, para conferir que uma reversão
// tem exatamente o valor da operação que ela desfaz.
func (m Money) Equals(other Money) bool {
	return m.currency == other.currency && m.amountCents == other.amountCents
}

// Currency devolve o código da moeda (ex: "BRL").
func (m Money) Currency() string {
	return m.currency
}

// String formata o valor de volta como decimal, no formato do
// contrato externo: "25.00". Implementa a interface fmt.Stringer,
// então dá para usar Money direto em fmt.Println / logs.
func (m Money) String() string {
	sign := ""
	cents := m.amountCents
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	whole := cents / 100
	frac := cents % 100
	return fmt.Sprintf("%s%d.%02d", sign, whole, frac)
}

// normalizeCurrency garante consistência (ex: sempre maiúsculo).
// Função privada (minúscula) — só usada dentro deste arquivo.
func normalizeCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}

// MoneyFromCents reconstrói um Money a partir do valor já em
// centavos, exatamente como ele é lido de volta do banco (coluna
// BIGINT). Diferente de NewMoneyFromString, aqui não há parsing de
// texto — é usado só pela camada de persistência, na "reidratação"
// (RehydrateWallet e afins), nunca para validar entrada externa.
func MoneyFromCents(cents int64, currency string) Money {
	return Money{amountCents: cents, currency: normalizeCurrency(currency)}
}

// Cents expõe o valor em centavos, para a camada de persistência
// gravar na coluna BIGINT do banco.
func (m Money) Cents() int64 {
	return m.amountCents
}
