package domain

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------
// Auxiliares
// ---------------------------------------------------------------------

func evMoney(t *testing.T, amount string) Money {
	t.Helper()
	m, err := NewMoneyFromString(amount, "BRL")
	if err != nil {
		t.Fatalf("dinheiro inválido no teste (%s): %v", amount, err)
	}
	return m
}

// evExternalTxn cria uma transação externa (ainda PENDING).
func evExternalTxn(t *testing.T, kind WagerKind, amount, referenceExternalID string) *WagerTransaction {
	t.Helper()
	txn, err := NewExternalWagerTransaction(
		"transaction-123", "provider-a", "provider-a:transaction-123", "hash-abc",
		uuid.New(), uuid.New(), "round-987", "fortune-chimp",
		kind, evMoney(t, amount), referenceExternalID,
	)
	if err != nil {
		t.Fatalf("erro criando transação do teste: %v", err)
	}
	return txn
}

// decodeEvent lê o payload do evento como um mapa genérico, do jeito
// que um consumidor externo o leria.
func decodeEvent(t *testing.T, e *OutboxEvent) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(e.Payload(), &out); err != nil {
		t.Fatalf("payload não é JSON válido: %v", err)
	}
	return out
}

func dataOf(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("envelope sem objeto \"data\": %v", envelope)
	}
	return data
}

func moneyOf(t *testing.T, m any) (string, string) {
	t.Helper()
	obj, ok := m.(map[string]any)
	if !ok {
		t.Fatalf("esperava objeto de dinheiro, veio %v", m)
	}
	amount, _ := obj["amount"].(string)
	currency, _ := obj["currency"].(string)
	return amount, currency
}

func evLedgerEntry(t *testing.T, txn *WagerTransaction) *WalletLedgerEntry {
	t.Helper()
	entry, err := NewWalletLedgerEntry(
		txn.WalletID(), txn.ID(), DirectionDebit,
		evMoney(t, "25.00"), evMoney(t, "100.00"), evMoney(t, "75.00"),
	)
	if err != nil {
		t.Fatalf("erro criando lançamento do teste: %v", err)
	}
	return entry
}

// ---------------------------------------------------------------------
// WagerTransactionProcessed
// ---------------------------------------------------------------------

func TestProcessedEvent_EnvelopeCompleto(t *testing.T) {
	txn := evExternalTxn(t, KindBet, "25.00", "")
	if err := txn.MarkProcessed(evMoney(t, "75.00")); err != nil {
		t.Fatal(err)
	}

	ev, err := NewWagerTransactionProcessedEvent(txn, "corr-1")
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}

	env := decodeEvent(t, ev)

	// Campos do envelope exigidos pela seção 11.
	if env["eventId"] != ev.ID().String() {
		t.Errorf("eventId do payload (%v) deve ser o id do evento (%s)", env["eventId"], ev.ID())
	}
	if env["eventType"] != EventTypeWagerTransactionProcessed {
		t.Errorf("eventType inesperado: %v", env["eventType"])
	}
	if env["aggregateId"] != txn.ID().String() {
		t.Errorf("aggregateId deveria ser o id da transação, veio %v", env["aggregateId"])
	}
	if env["correlationId"] != "corr-1" {
		t.Errorf("correlationId inesperado: %v", env["correlationId"])
	}
	if _, has := env["causationId"]; has {
		t.Error("causationId é opcional e não deve aparecer quando não há causa")
	}
	if env["version"] != float64(1) {
		t.Errorf("version deveria ser 1, veio %v", env["version"])
	}

	// occurredAt: RFC 3339, UTC, com milissegundos.
	occurredAt, _ := env["occurredAt"].(string)
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`).MatchString(occurredAt) {
		t.Errorf("occurredAt fora do formato RFC 3339 UTC com ms: %q", occurredAt)
	}
	parsed, err := time.Parse(time.RFC3339, occurredAt)
	if err != nil {
		t.Fatalf("occurredAt não é RFC 3339: %v", err)
	}
	if !parsed.Equal(ev.OccurredAt()) {
		t.Errorf("occurredAt do payload (%s) difere do instante do evento (%s)", parsed, ev.OccurredAt())
	}

	// Dados tipados.
	data := dataOf(t, env)
	if data["transactionId"] != txn.ID().String() || data["walletId"] != txn.WalletID().String() {
		t.Errorf("ids de transação/carteira errados: %v", data)
	}
	if data["providerId"] != "provider-a" || data["externalTransactionId"] != "transaction-123" ||
		data["roundId"] != "round-987" || data["gameId"] != "fortune-chimp" || data["kind"] != "BET" {
		t.Errorf("metadados da operação externa errados: %v", data)
	}
	if amount, cur := moneyOf(t, data["money"]); amount != "25.00" || cur != "BRL" {
		t.Errorf("money errado: %s %s", amount, cur)
	}
	if amount, cur := moneyOf(t, data["resultingBalance"]); amount != "75.00" || cur != "BRL" {
		t.Errorf("resultingBalance errado: %s %s", amount, cur)
	}
}

func TestProcessedEvent_DinheiroSaiComoStringNuncaComoNumero(t *testing.T) {
	txn := evExternalTxn(t, KindBet, "25.00", "")
	_ = txn.MarkProcessed(evMoney(t, "75.00"))

	ev, err := NewWagerTransactionProcessedEvent(txn, "corr-1")
	if err != nil {
		t.Fatal(err)
	}

	raw := string(ev.Payload())
	if !strings.Contains(raw, `"amount":"25.00"`) || !strings.Contains(raw, `"amount":"75.00"`) {
		t.Errorf("valores monetários deveriam ser strings decimais: %s", raw)
	}
}

func TestProcessedEvent_ExigeTransacaoProcessada(t *testing.T) {
	txn := evExternalTxn(t, KindBet, "25.00", "") // ainda PENDING

	_, err := NewWagerTransactionProcessedEvent(txn, "corr-1")

	if !errors.Is(err, ErrInvalidOutboxEvent) {
		t.Fatalf("esperava ErrInvalidOutboxEvent, veio %v", err)
	}
}

func TestProcessedEvent_AberturaDeCarteiraNaoTemMetadadosExternos(t *testing.T) {
	txn, err := NewOpeningTransaction(uuid.New(), uuid.New(), evMoney(t, "1000.00"))
	if err != nil {
		t.Fatal(err)
	}
	if err := txn.MarkProcessed(evMoney(t, "1000.00")); err != nil {
		t.Fatal(err)
	}

	ev, err := NewWagerTransactionProcessedEvent(txn, "corr-open")
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}

	data := dataOf(t, decodeEvent(t, ev))
	if data["kind"] != "OPENING" {
		t.Errorf("kind deveria ser OPENING, veio %v", data["kind"])
	}
	// Seção 9: eventos de origem interna não carregam os metadados
	// externos que não se aplicam.
	for _, key := range []string{"providerId", "externalTransactionId", "roundId", "gameId", "referenceExternalTransactionId", "referenceTransactionId"} {
		if _, has := data[key]; has {
			t.Errorf("campo %q não se aplica à abertura e não deveria aparecer", key)
		}
	}
}

func TestProcessedEvent_IncluiReferenciaResolvida(t *testing.T) {
	txn := evExternalTxn(t, KindRefund, "25.00", "bet-1")
	refID := uuid.New()
	if err := txn.ResolveReference(refID); err != nil {
		t.Fatal(err)
	}
	if err := txn.MarkProcessed(evMoney(t, "100.00")); err != nil {
		t.Fatal(err)
	}

	ev, err := NewWagerTransactionProcessedEvent(txn, "corr-1")
	if err != nil {
		t.Fatal(err)
	}

	data := dataOf(t, decodeEvent(t, ev))
	if data["referenceExternalTransactionId"] != "bet-1" {
		t.Errorf("referência externa ausente: %v", data)
	}
	if data["referenceTransactionId"] != refID.String() {
		t.Errorf("referência interna resolvida ausente: %v", data)
	}
}

// ---------------------------------------------------------------------
// WagerTransactionRejected
// ---------------------------------------------------------------------

func TestRejectedEvent_TrazFailureCode(t *testing.T) {
	txn := evExternalTxn(t, KindBet, "150.00", "")
	if err := txn.MarkRejected(FailureInsufficientBalance); err != nil {
		t.Fatal(err)
	}

	ev, err := NewWagerTransactionRejectedEvent(txn, "corr-1")
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}

	env := decodeEvent(t, ev)
	if env["eventType"] != EventTypeWagerTransactionRejected {
		t.Errorf("eventType inesperado: %v", env["eventType"])
	}
	if dataOf(t, env)["failureCode"] != FailureInsufficientBalance {
		t.Errorf("failureCode ausente ou errado: %v", dataOf(t, env))
	}
}

func TestRejectedEvent_ExigeTransacaoRejeitada(t *testing.T) {
	txn := evExternalTxn(t, KindBet, "25.00", "")

	if _, err := NewWagerTransactionRejectedEvent(txn, "corr-1"); !errors.Is(err, ErrInvalidOutboxEvent) {
		t.Fatalf("esperava ErrInvalidOutboxEvent, veio %v", err)
	}
}

// ---------------------------------------------------------------------
// WagerTransactionPendingReference
// ---------------------------------------------------------------------

func TestPendingReferenceEvent_TrazAReferenciaEsperada(t *testing.T) {
	txn := evExternalTxn(t, KindRefund, "25.00", "bet-1")
	if err := txn.MarkPendingReference(); err != nil {
		t.Fatal(err)
	}

	ev, err := NewWagerTransactionPendingReferenceEvent(txn, "corr-1")
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}

	env := decodeEvent(t, ev)
	if env["eventType"] != EventTypeWagerTransactionPendingReference {
		t.Errorf("eventType inesperado: %v", env["eventType"])
	}
	if dataOf(t, env)["referenceExternalTransactionId"] != "bet-1" {
		t.Errorf("referência esperada ausente: %v", dataOf(t, env))
	}
}

func TestPendingReferenceEvent_ExigeEstadoPendingReference(t *testing.T) {
	txn := evExternalTxn(t, KindRefund, "25.00", "bet-1") // ainda PENDING

	if _, err := NewWagerTransactionPendingReferenceEvent(txn, "corr-1"); !errors.Is(err, ErrInvalidOutboxEvent) {
		t.Fatalf("esperava ErrInvalidOutboxEvent, veio %v", err)
	}
}

// ---------------------------------------------------------------------
// WalletBalanceChanged
// ---------------------------------------------------------------------

func TestBalanceChangedEvent_PayloadDaSecao11(t *testing.T) {
	txn := evExternalTxn(t, KindBet, "25.00", "")
	entry := evLedgerEntry(t, txn)
	causation := uuid.New()

	ev, err := NewWalletBalanceChangedEvent(entry, 2, "corr-1", causation)
	if err != nil {
		t.Fatalf("não esperava erro: %v", err)
	}

	env := decodeEvent(t, ev)
	if env["eventType"] != EventTypeWalletBalanceChanged {
		t.Errorf("eventType inesperado: %v", env["eventType"])
	}
	if env["aggregateId"] != entry.WalletID().String() {
		t.Errorf("aggregateId deveria ser o id da carteira, veio %v", env["aggregateId"])
	}
	if env["causationId"] != causation.String() {
		t.Errorf("causationId deveria ser %s, veio %v", causation, env["causationId"])
	}
	if got, ok := ev.CausationID(); !ok || got != causation {
		t.Errorf("CausationID() inesperado: %v %v", got, ok)
	}

	// walletId, transactionId, direction, money, balanceBefore,
	// balanceAfter e walletVersion (seção 11).
	data := dataOf(t, env)
	if data["walletId"] != entry.WalletID().String() || data["transactionId"] != txn.ID().String() {
		t.Errorf("ids errados: %v", data)
	}
	if data["direction"] != "DEBIT" {
		t.Errorf("direction errada: %v", data["direction"])
	}
	if amount, _ := moneyOf(t, data["money"]); amount != "25.00" {
		t.Errorf("money errado: %s", amount)
	}
	if amount, _ := moneyOf(t, data["balanceBefore"]); amount != "100.00" {
		t.Errorf("balanceBefore errado: %s", amount)
	}
	if amount, _ := moneyOf(t, data["balanceAfter"]); amount != "75.00" {
		t.Errorf("balanceAfter errado: %s", amount)
	}
	if data["walletVersion"] != float64(2) {
		t.Errorf("walletVersion errada: %v", data["walletVersion"])
	}
}

func TestBalanceChangedEvent_RejeitaVersaoInvalida(t *testing.T) {
	txn := evExternalTxn(t, KindBet, "25.00", "")
	entry := evLedgerEntry(t, txn)

	if _, err := NewWalletBalanceChangedEvent(entry, 0, "corr-1", uuid.Nil); !errors.Is(err, ErrInvalidOutboxEvent) {
		t.Fatalf("esperava ErrInvalidOutboxEvent, veio %v", err)
	}
}

// ---------------------------------------------------------------------
// Regras gerais do evento
// ---------------------------------------------------------------------

func TestOutboxEvent_CorrelationIDEhObrigatorio(t *testing.T) {
	txn := evExternalTxn(t, KindBet, "25.00", "")
	_ = txn.MarkProcessed(evMoney(t, "75.00"))

	if _, err := NewWagerTransactionProcessedEvent(txn, ""); !errors.Is(err, ErrInvalidOutboxEvent) {
		t.Fatalf("esperava ErrInvalidOutboxEvent, veio %v", err)
	}
}

func TestOutboxEvent_CadaEventoTemEventIdProprio(t *testing.T) {
	txn := evExternalTxn(t, KindBet, "25.00", "")
	_ = txn.MarkProcessed(evMoney(t, "75.00"))

	a, _ := NewWagerTransactionProcessedEvent(txn, "corr-1")
	b, _ := NewWagerTransactionProcessedEvent(txn, "corr-1")

	if a.ID() == b.ID() {
		t.Error("dois eventos criados separadamente não podem ter o mesmo eventId")
	}
}

func TestOutboxEvent_PayloadEhSnapshotImutavel(t *testing.T) {
	txn := evExternalTxn(t, KindBet, "25.00", "")
	_ = txn.MarkProcessed(evMoney(t, "75.00"))
	ev, err := NewWagerTransactionProcessedEvent(txn, "corr-1")
	if err != nil {
		t.Fatal(err)
	}

	original := string(ev.Payload())

	// Mexer na cópia devolvida não pode alterar o evento.
	copyOfPayload := ev.Payload()
	copyOfPayload[0] = 'X'
	if string(ev.Payload()) != original {
		t.Error("alterar a cópia do payload não deveria alterar o evento")
	}
}
