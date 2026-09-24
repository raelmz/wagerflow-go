package application

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// Estes testes conferem QUAIS eventos de outbox saem em cada cenário e
// com que conteúdo. Lembrete dos limites do fake em memória (ver
// fakes_test.go): ele não simula rollback nem concorrência, então a
// prova de que "evento e mudança de estado são atômicos" vem do teste
// de integração contra o Postgres real — aqui provamos as REGRAS
// (o que emitir, quando, e com quais dados).

func envelopeOf(t *testing.T, e *domain.OutboxEvent) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(e.Payload(), &out); err != nil {
		t.Fatalf("payload não é JSON válido: %v", err)
	}
	return out
}

func dataMap(t *testing.T, e *domain.OutboxEvent) map[string]any {
	t.Helper()
	data, ok := envelopeOf(t, e)["data"].(map[string]any)
	if !ok {
		t.Fatalf("envelope sem \"data\"")
	}
	return data
}

func expectEventTypes(t *testing.T, e *env, want ...string) {
	t.Helper()
	got := e.store.eventTypes()
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("eventos gravados = %v, esperava %v", got, want)
	}
}

// ---------------------------------------------------------------------
// Operações externas
// ---------------------------------------------------------------------

func TestOutbox_BetProcessadaGravaProcessedEBalanceChanged(t *testing.T) {
	e := newEnv(t, "100.00")

	res := e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	expectEventTypes(t, e, domain.EventTypeWagerTransactionProcessed, domain.EventTypeWalletBalanceChanged)

	events := e.store.outboxEvents()
	processed, balance := events[0], events[1]

	// Processed: aggregateId = transação; saldo resultante no payload.
	if processed.AggregateID() != res.Transaction.ID() {
		t.Errorf("aggregateId do Processed deveria ser a transação")
	}
	if got := dataMap(t, processed)["resultingBalance"].(map[string]any)["amount"]; got != "75.00" {
		t.Errorf("resultingBalance esperado 75.00, veio %v", got)
	}

	// BalanceChanged: aggregateId = carteira; causado pelo Processed;
	// versão da carteira depois da mudança (1 -> 2).
	if balance.AggregateID() != e.walletID {
		t.Errorf("aggregateId do BalanceChanged deveria ser a carteira")
	}
	if cause, ok := balance.CausationID(); !ok || cause != processed.ID() {
		t.Errorf("causationId do BalanceChanged deveria ser o eventId do Processed")
	}
	data := dataMap(t, balance)
	if data["direction"] != "DEBIT" || data["walletVersion"] != float64(2) {
		t.Errorf("payload do BalanceChanged errado: %v", data)
	}
	if data["balanceBefore"].(map[string]any)["amount"] != "100.00" ||
		data["balanceAfter"].(map[string]any)["amount"] != "75.00" {
		t.Errorf("saldos do BalanceChanged errados: %v", data)
	}
}

func TestOutbox_EventosDeUmaOperacaoCompartilhamCorrelationId(t *testing.T) {
	e := newEnv(t, "100.00")

	// Sem correlationId no context: usa o id da transação como fallback.
	res := e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	for _, ev := range e.store.outboxEvents() {
		if ev.CorrelationID() != res.Transaction.ID().String() {
			t.Errorf("%s: correlationId esperado %s (fallback), veio %s",
				ev.Type(), res.Transaction.ID(), ev.CorrelationID())
		}
	}
}

func TestOutbox_UsaCorrelationIdDoContext(t *testing.T) {
	e := newEnv(t, "100.00")
	ctx := WithCorrelationID(context.Background(), "req-42")

	if _, err := e.uc.Execute(ctx, e.cmd(domain.KindBet, "bet-1", "25.00")); err != nil {
		t.Fatal(err)
	}

	events := e.store.outboxEvents()
	if len(events) != 2 {
		t.Fatalf("esperava 2 eventos, veio %d", len(events))
	}
	for _, ev := range events {
		if ev.CorrelationID() != "req-42" || envelopeOf(t, ev)["correlationId"] != "req-42" {
			t.Errorf("%s: correlationId deveria ser req-42", ev.Type())
		}
	}
}

func TestOutbox_WinProcessadaCreditaEEmiteDuasVezes(t *testing.T) {
	e := newEnv(t, "100.00")

	e.run(t, e.cmd(domain.KindWin, "win-1", "50.00"))

	expectEventTypes(t, e, domain.EventTypeWagerTransactionProcessed, domain.EventTypeWalletBalanceChanged)
	if dataMap(t, e.store.outboxEvents()[1])["direction"] != "CREDIT" {
		t.Error("WIN deveria gerar BalanceChanged com direção CREDIT")
	}
}

func TestOutbox_LossGravaSoProcessedSemBalanceChanged(t *testing.T) {
	e := newEnv(t, "100.00")

	e.run(t, e.cmd(domain.KindLoss, "loss-1", "0.00"))

	// Seção 7 do desafio: LOSS produz WagerTransactionProcessed, sem
	// WalletBalanceChanged.
	expectEventTypes(t, e, domain.EventTypeWagerTransactionProcessed)
}

func TestOutbox_RejeicaoGravaSoRejected(t *testing.T) {
	e := newEnv(t, "100.00")

	e.run(t, e.cmd(domain.KindBet, "bet-1", "150.00")) // sem saldo

	expectEventTypes(t, e, domain.EventTypeWagerTransactionRejected)
	if got := dataMap(t, e.store.outboxEvents()[0])["failureCode"]; got != domain.FailureInsufficientBalance {
		t.Errorf("failureCode esperado %s, veio %v", domain.FailureInsufficientBalance, got)
	}
}

func TestOutbox_RollbackDeWinGeraBalanceChangedDeDebito(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindWin, "win-1", "50.00"))

	e.run(t, e.reversal(domain.KindRollback, "rb-1", "50.00", "win-1"))

	expectEventTypes(t, e,
		domain.EventTypeWagerTransactionProcessed, domain.EventTypeWalletBalanceChanged, // win-1
		domain.EventTypeWagerTransactionProcessed, domain.EventTypeWalletBalanceChanged, // rb-1
	)
	last := e.store.outboxEvents()[3]
	if dataMap(t, last)["direction"] != "DEBIT" {
		t.Error("ROLLBACK de uma WIN deveria gerar BalanceChanged de DEBIT")
	}
}

// ---------------------------------------------------------------------
// Replay e entradas sem efeito
// ---------------------------------------------------------------------

func TestOutbox_ReplayNaoGravaEventosNovos(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))
	before := len(e.store.outboxEvents())

	replay := e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	if !replay.Replay {
		t.Fatal("esperava replay")
	}
	if got := len(e.store.outboxEvents()); got != before {
		t.Errorf("replay não pode gerar eventos: tinha %d, agora %d", before, got)
	}
}

func TestOutbox_EntradaInvalidaNaoGravaEvento(t *testing.T) {
	e := newEnv(t, "100.00")
	c := e.cmd(domain.KindBet, "bet-1", "25.00")
	c.WalletID = uuid.New() // carteira inexistente

	if _, err := e.uc.Execute(context.Background(), c); err == nil {
		t.Fatal("esperava erro")
	}

	expectEventTypes(t, e) // nenhum
}

// ---------------------------------------------------------------------
// Referência pendente
// ---------------------------------------------------------------------

func TestOutbox_ReferenciaAusenteGravaPendingReferenceUmaVezSo(t *testing.T) {
	e := newEnv(t, "100.00")

	e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1"))

	expectEventTypes(t, e, domain.EventTypeWagerTransactionPendingReference)
	if got := dataMap(t, e.store.outboxEvents()[0])["referenceExternalTransactionId"]; got != "bet-1" {
		t.Errorf("evento deveria informar a referência esperada, veio %v", got)
	}

	// O worker reaplica e a referência continua ausente: segue
	// esperando, SEM publicar outro aviso.
	reapplyPending(t, e, "refund-1")
	expectEventTypes(t, e, domain.EventTypeWagerTransactionPendingReference)
}

func TestOutbox_PendenciaResolvidaGravaProcessedEBalanceChanged(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1"))
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	reapplyPending(t, e, "refund-1")

	expectEventTypes(t, e,
		domain.EventTypeWagerTransactionPendingReference, // refund-1 esperando
		domain.EventTypeWagerTransactionProcessed,        // bet-1
		domain.EventTypeWalletBalanceChanged,             // bet-1
		domain.EventTypeWagerTransactionProcessed,        // refund-1 concluído
		domain.EventTypeWalletBalanceChanged,             // refund-1 concluído
	)
}

// reapplyPending simula o que o worker de referências pendentes fará:
// reaplicar uma transação que está em PENDING_REFERENCE.
func reapplyPending(t *testing.T, e *env, externalID string) {
	t.Helper()
	err := e.store.WithinTransaction(context.Background(), func(uow domain.UnitOfWork) error {
		pending, err := uow.WagerTransactions().FindByProviderAndExternalTxID(context.Background(), "provider-a", externalID)
		if err != nil || pending == nil {
			t.Fatalf("pendência não encontrada: %v", err)
		}
		return applyWagerTransaction(context.Background(), uow, pending)
	})
	if err != nil {
		t.Fatalf("reaplicação falhou: %v", err)
	}
}

// ---------------------------------------------------------------------
// Abertura de carteira
// ---------------------------------------------------------------------

func TestOutbox_AberturaComSaldoPositivoGravaOsDoisEventos(t *testing.T) {
	store := newMemStore()
	uc := NewOpenWalletUseCase(store)
	balance, err := domain.NewMoneyFromString("1000.00", "BRL")
	if err != nil {
		t.Fatal(err)
	}

	res, err := uc.Execute(context.Background(), uuid.New(), balance)
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}

	types := store.eventTypes()
	want := []string{domain.EventTypeWagerTransactionProcessed, domain.EventTypeWalletBalanceChanged}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("eventos = %v, esperava %v", types, want)
	}

	events := store.outboxEvents()
	processedData := dataMap(t, events[0])
	if processedData["kind"] != "OPENING" {
		t.Errorf("kind deveria ser OPENING: %v", processedData["kind"])
	}
	// Origem interna: sem metadados externos.
	for _, key := range []string{"providerId", "externalTransactionId", "roundId", "gameId"} {
		if _, has := processedData[key]; has {
			t.Errorf("campo %q não se aplica à abertura", key)
		}
	}

	balanceData := dataMap(t, events[1])
	if balanceData["direction"] != "CREDIT" || balanceData["walletVersion"] != float64(1) {
		t.Errorf("BalanceChanged da abertura errado: %v", balanceData)
	}
	if events[1].AggregateID() != res.Wallet.ID() {
		t.Error("aggregateId do BalanceChanged deveria ser a carteira aberta")
	}
	if cause, ok := events[1].CausationID(); !ok || cause != events[0].ID() {
		t.Error("BalanceChanged da abertura deveria ter o Processed como causa")
	}
}

func TestOutbox_AberturaComSaldoZeroNaoGravaEventos(t *testing.T) {
	store := newMemStore()
	uc := NewOpenWalletUseCase(store)

	if _, err := uc.Execute(context.Background(), uuid.New(), domain.ZeroMoney("BRL")); err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}

	if got := store.eventTypes(); len(got) != 0 {
		t.Errorf("saldo inicial zero não gera eventos financeiros (seção 9), veio %v", got)
	}
}
