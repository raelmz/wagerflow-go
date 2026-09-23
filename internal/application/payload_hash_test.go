package application

import (
	"testing"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

func baseCommand() ProcessWagerCommand {
	return ProcessWagerCommand{
		IdempotencyKey:        "provider-a:tx-1",
		ProviderID:            "provider-a",
		ExternalTransactionID: "tx-1",
		PlayerID:              uuid.MustParse("0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1"),
		WalletID:              uuid.MustParse("0192f291-27dd-7d3f-8071-5f8685deef37"),
		RoundID:               "round-987",
		GameID:                "fortune-chimp",
		Kind:                  domain.KindBet,
		Amount:                "25.00",
		Currency:              "BRL",
	}
}

func hashOf(t *testing.T, cmd ProcessWagerCommand) string {
	t.Helper()
	money, err := domain.NewMoneyFromString(cmd.Amount, cmd.Currency)
	if err != nil {
		t.Fatal(err)
	}
	h, err := computePayloadHash(cmd, money)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestPayloadHash_EhDeterministico(t *testing.T) {
	if hashOf(t, baseCommand()) != hashOf(t, baseCommand()) {
		t.Error("o mesmo comando deve gerar sempre o mesmo hash")
	}
}

func TestPayloadHash_IgnoraAChaveDeIdempotencia(t *testing.T) {
	// A chave é a identidade da requisição, não o conteúdo — fica fora do hash.
	// É isso que permite detectar "mesma chave, conteúdo diferente".
	a := baseCommand()
	b := baseCommand()
	b.IdempotencyKey = "outra-chave"

	if hashOf(t, a) != hashOf(t, b) {
		t.Error("a chave de idempotência não deve entrar no hash")
	}
}

func TestPayloadHash_NormalizaMoeda(t *testing.T) {
	a := baseCommand()
	b := baseCommand()
	b.Currency = "brl"

	if hashOf(t, a) != hashOf(t, b) {
		t.Error("\"brl\" e \"BRL\" são a mesma operação e devem gerar o mesmo hash")
	}
}

func TestPayloadHash_MudaQuandoQualquerCampoDeNegocioMuda(t *testing.T) {
	base := hashOf(t, baseCommand())

	variacoes := map[string]func(*ProcessWagerCommand){
		"valor":      func(c *ProcessWagerCommand) { c.Amount = "25.01" },
		"tipo":       func(c *ProcessWagerCommand) { c.Kind = domain.KindWin },
		"rodada":     func(c *ProcessWagerCommand) { c.RoundID = "round-988" },
		"jogo":       func(c *ProcessWagerCommand) { c.GameID = "outro-jogo" },
		"provedor":   func(c *ProcessWagerCommand) { c.ProviderID = "provider-b" },
		"id externo": func(c *ProcessWagerCommand) { c.ExternalTransactionID = "tx-2" },
		"jogador":    func(c *ProcessWagerCommand) { c.PlayerID = uuid.New() },
		"carteira":   func(c *ProcessWagerCommand) { c.WalletID = uuid.New() },
		"moeda":      func(c *ProcessWagerCommand) { c.Currency = "USD" },
		"referência": func(c *ProcessWagerCommand) { c.ReferenceExternalTransactionID = "tx-0" },
	}

	for nome, muda := range variacoes {
		t.Run(nome, func(t *testing.T) {
			c := baseCommand()
			muda(&c)
			if hashOf(t, c) == base {
				t.Errorf("mudar %s deveria mudar o hash", nome)
			}
		})
	}
}
