//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/domain"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/postgres"
)

// consumeCmd monta o InboundWagerMessage de teste, com valores padrão
// de providerId/round/game iguais aos usados em bet() (harness
// principal), para os dois caminhos (HTTP e SQS) ficarem comparáveis.
func consumeCmd(walletID, playerID uuid.UUID, externalID, idempotencyKey, amount string) application.InboundWagerMessage {
	return application.InboundWagerMessage{
		MessageID: uuid.New().String(),
		Command: application.ProcessWagerCommand{
			IdempotencyKey:        idempotencyKey,
			ProviderID:            "provider-x",
			ExternalTransactionID: externalID,
			PlayerID:              playerID,
			WalletID:              walletID,
			RoundID:               "round-1",
			GameID:                "game-1",
			Kind:                  domain.KindBet,
			Amount:                amount,
			Currency:              "BRL",
		},
	}
}

// ---------------------------------------------------------------------
// 9. Consumidor SQS + inbox
// ---------------------------------------------------------------------

// TestConsumidor_ProcessaMensagemNova prova o caminho feliz: uma
// mensagem nova debita a carteira, grava o ledger e a inbox no mesmo
// commit.
func TestConsumidor_ProcessaMensagemNova(t *testing.T) {
	h := newHarness(t)
	txm := postgres.NewTxManager(h.pool)
	consumeUC := application.NewConsumeWagerTransactionUseCase(txm, "wager-consumer")

	walletID := openWallet(t, h, "100.00")
	playerID := walletPlayerID(t, h.pool, walletID)

	msg := consumeCmd(walletID, playerID, "ext-consumer-1", "idem-consumer-1", "30.00")
	res, err := consumeUC.Execute(context.Background(), msg)
	if err != nil {
		t.Fatalf("falha inesperada consumindo mensagem nova: %v", err)
	}
	if res == nil || res.Replay {
		t.Fatalf("esperava resultado novo (não replay), obtido: %+v", res)
	}
	if res.Transaction.Status() != domain.StatusProcessed {
		t.Fatalf("esperava PROCESSED, obtido %s", res.Transaction.Status())
	}

	if got := walletBalance(t, h.pool, walletID); got != 7000 {
		t.Fatalf("saldo esperado 7000 centavos, obtido %d", got)
	}
	if got := ledgerCount(t, h.pool, walletID); got != 1 {
		t.Fatalf("esperava 1 lançamento no ledger, obtido %d", got)
	}
	if got := inboxCount(t, h.pool, "wager-consumer"); got != 1 {
		t.Fatalf("esperava 1 registro na inbox, obtido %d", got)
	}
}

// TestConsumidor_ReentregaDaMesmaMensagemNaoReprocessa é o cenário
// central da inbox: a MESMA mensagem (mesmo messageId) chega duas
// vezes — reentrega comum do SQS (at-least-once) — e a segunda
// chamada não pode debitar a carteira de novo.
func TestConsumidor_ReentregaDaMesmaMensagemNaoReprocessa(t *testing.T) {
	h := newHarness(t)
	txm := postgres.NewTxManager(h.pool)
	consumeUC := application.NewConsumeWagerTransactionUseCase(txm, "wager-consumer")

	walletID := openWallet(t, h, "100.00")
	playerID := walletPlayerID(t, h.pool, walletID)

	msg := consumeCmd(walletID, playerID, "ext-consumer-2", "idem-consumer-2", "40.00")

	first, err := consumeUC.Execute(context.Background(), msg)
	if err != nil {
		t.Fatalf("falha na primeira entrega: %v", err)
	}
	if first == nil {
		t.Fatal("primeira entrega não deveria ser tratada como duplicata")
	}

	// MESMO MessageID: reentrega da mesma entrega lógica do SQS.
	second, err := consumeUC.Execute(context.Background(), msg)
	if err != nil {
		t.Fatalf("falha na reentrega: %v", err)
	}
	if second != nil {
		t.Fatalf("reentrega da mesma mensagem deveria devolver (nil, nil) — mensagem já vista; obtido %+v", second)
	}

	if got := walletBalance(t, h.pool, walletID); got != 6000 {
		t.Fatalf("saldo esperado 6000 centavos (1 débito só), obtido %d", got)
	}
	if got := ledgerCount(t, h.pool, walletID); got != 1 {
		t.Fatalf("esperava 1 lançamento no ledger (sem duplicar), obtido %d", got)
	}
}

// TestConsumidor_MesmaOperacaoPorMensagensDiferentesEhReplay cobre o
// caso em que a MESMA operação de negócio (idempotencyKey/externalId
// iguais) chega em duas mensagens SQS DIFERENTES (messageId
// diferente) — por exemplo, o provedor reenviou manualmente. A
// deduplicação da inbox não pega esse caso (messageId é diferente),
// mas a idempotência do domínio (já provada no harness HTTP) continua
// protegendo: a segunda mensagem é tratada como replay.
func TestConsumidor_MesmaOperacaoPorMensagensDiferentesEhReplay(t *testing.T) {
	h := newHarness(t)
	txm := postgres.NewTxManager(h.pool)
	consumeUC := application.NewConsumeWagerTransactionUseCase(txm, "wager-consumer")

	walletID := openWallet(t, h, "100.00")
	playerID := walletPlayerID(t, h.pool, walletID)

	first := consumeCmd(walletID, playerID, "ext-consumer-3", "idem-consumer-3", "25.00")
	second := consumeCmd(walletID, playerID, "ext-consumer-3", "idem-consumer-3", "25.00") // messageId novo, mesma operação

	if _, err := consumeUC.Execute(context.Background(), first); err != nil {
		t.Fatalf("falha na primeira mensagem: %v", err)
	}
	res, err := consumeUC.Execute(context.Background(), second)
	if err != nil {
		t.Fatalf("falha na segunda mensagem: %v", err)
	}
	if res == nil || !res.Replay {
		t.Fatalf("esperava replay da operação já processada, obtido %+v", res)
	}

	if got := walletBalance(t, h.pool, walletID); got != 7500 {
		t.Fatalf("saldo esperado 7500 centavos (1 débito só), obtido %d", got)
	}
	// Duas entradas na inbox (mensagens diferentes), um débito só.
	if got := inboxCount(t, h.pool, "wager-consumer"); got != 2 {
		t.Fatalf("esperava 2 registros na inbox (mensagens distintas), obtido %d", got)
	}
	if got := ledgerCount(t, h.pool, walletID); got != 1 {
		t.Fatalf("esperava 1 lançamento no ledger, obtido %d", got)
	}
}

// TestConsumidor_MensagensConcorrentesDaMesmaOperacaoSoDebitaUmaVez
// simula duas instâncias do consumidor processando, ao mesmo tempo, a
// MESMA operação recebida em mensagens diferentes (ex: alguma
// duplicação a montante do SQS) — a corrida cai no mesmo
// ErrDuplicateTransaction já coberto no caminho HTTP, e o vencedor é
// relido em uma transação nova.
func TestConsumidor_MensagensConcorrentesDaMesmaOperacaoSoDebitaUmaVez(t *testing.T) {
	h := newHarness(t)
	txm := postgres.NewTxManager(h.pool)
	consumeUC := application.NewConsumeWagerTransactionUseCase(txm, "wager-consumer")

	walletID := openWallet(t, h, "100.00")
	playerID := walletPlayerID(t, h.pool, walletID)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := consumeCmd(walletID, playerID, "ext-consumer-4", "idem-consumer-4", "10.00")
			_, err := consumeUC.Execute(context.Background(), msg)
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("mensagem concorrente %d falhou: %v", i, err)
		}
	}

	if got := walletBalance(t, h.pool, walletID); got != 9000 {
		t.Fatalf("saldo esperado 9000 centavos (1 débito só entre %d concorrentes), obtido %d", n, got)
	}
	if got := ledgerCount(t, h.pool, walletID); got != 1 {
		t.Fatalf("esperava 1 lançamento no ledger entre %d concorrentes, obtido %d", n, got)
	}
}

// --- Auxiliares específicos deste arquivo ---

func walletPlayerID(t *testing.T, pool *pgxpool.Pool, walletID uuid.UUID) uuid.UUID {
	t.Helper()
	var playerID uuid.UUID
	err := pool.QueryRow(context.Background(),
		`SELECT player_id FROM wallets WHERE id = $1`, walletID).Scan(&playerID)
	if err != nil {
		t.Fatalf("falha ao ler playerId da carteira: %v", err)
	}
	return playerID
}

func inboxCount(t *testing.T, pool *pgxpool.Pool, consumerName string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM inbox_messages WHERE consumer_name = $1`, consumerName).Scan(&n)
	if err != nil {
		t.Fatalf("falha ao contar inbox: %v", err)
	}
	return n
}
