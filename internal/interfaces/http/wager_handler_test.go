package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/domain"
)

func newWagerHandlerForTest() (*WagerHandler, *fakeStore) {
	store := newFakeStore()
	h := NewWagerHandler(
		application.NewProcessWagerTransactionUseCase(store),
		application.NewGetWagerTransactionUseCase(&fakeWagerRepo{store}),
	)
	return h, store
}

// seedWallet cria (via o repositório fake, para já ficar no formato
// que o handler espera encontrar) uma carteira com o saldo informado
// e devolve seu id.
func seedWallet(t *testing.T, store *fakeStore, playerID uuid.UUID, amount, currency string) uuid.UUID {
	t.Helper()
	balance, err := domain.NewMoneyFromString(amount, currency)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	wallet, err := domain.NewWallet(playerID, balance)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	store.wallets[wallet.ID()] = wallet
	return wallet.ID()
}

// asProvider simula uma requisição já autenticada pelo AuthMiddleware
// como o provider informado (sem role internal) — testa o handler
// isoladamente, sem precisar de um token real.
func asProvider(r *http.Request, providerID string) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyProviderID, providerID)
	ctx = context.WithValue(ctx, ctxKeyRoles, []string{RoleProvider})
	return r.WithContext(ctx)
}

func asInternal(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyRoles, []string{RoleInternal})
	return r.WithContext(ctx)
}

func TestWagerHandler_Process_SemIdempotencyKey(t *testing.T) {
	h, _ := newWagerHandlerForTest()

	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewReader([]byte(`{}`)))
	req = asProvider(req, "provider-a")
	rec := httptest.NewRecorder()
	h.Process(rec, req)

	assertErrorStatus(t, rec, http.StatusBadRequest, "INVALID_INPUT")
}

func TestWagerHandler_Process_ProviderIdDoTokenNaoBateComCorpo(t *testing.T) {
	h, _ := newWagerHandlerForTest()

	body, _ := json.Marshal(processWagerRequest{
		ProviderID: "provider-b", // token abaixo diz "provider-a"
		PlayerID:   uuid.New().String(),
		WalletID:   uuid.New().String(),
		Kind:       string(domain.KindBet),
		Money:      moneyDTO{Amount: "10.00", Currency: "BRL"},
	})
	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "key-1")
	req = asProvider(req, "provider-a")
	rec := httptest.NewRecorder()
	h.Process(rec, req)

	assertErrorStatus(t, rec, http.StatusForbidden, "PROVIDER_MISMATCH")
}

func TestWagerHandler_Process_RoleInternalIgnoraIsolamentoDeProvider(t *testing.T) {
	h, store := newWagerHandlerForTest()
	playerID := uuid.New()
	walletID := seedWallet(t, store, playerID, "100.00", "BRL")

	body, _ := json.Marshal(processWagerRequest{
		ProviderID:            "provider-qualquer",
		ExternalTransactionID: "ext-1",
		PlayerID:              playerID.String(),
		WalletID:              walletID.String(),
		Kind:                  string(domain.KindBet),
		Money:                 moneyDTO{Amount: "10.00", Currency: "BRL"},
	})
	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "key-1")
	req = asInternal(req) // sem providerId no token — role internal não checa isolamento

	rec := httptest.NewRecorder()
	h.Process(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperava 201 (internal ignora isolamento de provider), veio %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestWagerHandler_Process_BetProcessadaComSucesso_Retorna201(t *testing.T) {
	h, store := newWagerHandlerForTest()
	playerID := uuid.New()
	walletID := seedWallet(t, store, playerID, "100.00", "BRL")

	body, _ := json.Marshal(processWagerRequest{
		ProviderID:            "provider-a",
		ExternalTransactionID: "ext-1",
		PlayerID:              playerID.String(),
		WalletID:              walletID.String(),
		Kind:                  string(domain.KindBet),
		Money:                 moneyDTO{Amount: "10.00", Currency: "BRL"},
	})
	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "key-1")
	req = asProvider(req, "provider-a")

	rec := httptest.NewRecorder()
	h.Process(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperava 201, veio %d (body=%s)", rec.Code, rec.Body.String())
	}
	var resp processWagerResponse
	decodeBody(t, rec, &resp)
	if resp.Status != string(domain.StatusProcessed) {
		t.Fatalf("esperava status PROCESSED, veio %q", resp.Status)
	}
	if resp.IdempotentReplay {
		t.Fatal("primeira requisição não deveria ser marcada como replay")
	}
	if resp.Balance == nil || resp.Balance.Amount != "90.00" {
		t.Fatalf("esperava saldo resultante 90.00, veio %+v", resp.Balance)
	}
}

func TestWagerHandler_Process_ReplayIdempotente_Retorna200(t *testing.T) {
	h, store := newWagerHandlerForTest()
	playerID := uuid.New()
	walletID := seedWallet(t, store, playerID, "100.00", "BRL")

	body, _ := json.Marshal(processWagerRequest{
		ProviderID:            "provider-a",
		ExternalTransactionID: "ext-1",
		PlayerID:              playerID.String(),
		WalletID:              walletID.String(),
		Kind:                  string(domain.KindBet),
		Money:                 moneyDTO{Amount: "10.00", Currency: "BRL"},
	})

	req1 := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewReader(body))
	req1.Header.Set("Idempotency-Key", "key-1")
	req1 = asProvider(req1, "provider-a")
	rec1 := httptest.NewRecorder()
	h.Process(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("setup: esperava 201 na primeira chamada, veio %d (body=%s)", rec1.Code, rec1.Body.String())
	}

	// Mesma Idempotency-Key, mesmo corpo -> replay.
	req2 := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewReader(body))
	req2.Header.Set("Idempotency-Key", "key-1")
	req2 = asProvider(req2, "provider-a")
	rec2 := httptest.NewRecorder()
	h.Process(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("esperava 200 no replay, veio %d (body=%s)", rec2.Code, rec2.Body.String())
	}
	var resp processWagerResponse
	decodeBody(t, rec2, &resp)
	if !resp.IdempotentReplay {
		t.Fatal("segunda requisição com a mesma chave deveria vir marcada como replay")
	}
}

func TestWagerHandler_Process_CarteiraNaoEncontrada(t *testing.T) {
	h, _ := newWagerHandlerForTest()

	body, _ := json.Marshal(processWagerRequest{
		ProviderID:            "provider-a",
		ExternalTransactionID: "ext-1",
		PlayerID:              uuid.New().String(),
		WalletID:              uuid.New().String(),
		Kind:                  string(domain.KindBet),
		Money:                 moneyDTO{Amount: "10.00", Currency: "BRL"},
	})
	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "key-1")
	req = asProvider(req, "provider-a")

	rec := httptest.NewRecorder()
	h.Process(rec, req)

	assertErrorStatus(t, rec, http.StatusNotFound, "WALLET_NOT_FOUND")
}

func TestWagerHandler_GetByID_IsolamentoDeProvider_Retorna404(t *testing.T) {
	h, store := newWagerHandlerForTest()
	playerID := uuid.New()
	walletID := seedWallet(t, store, playerID, "100.00", "BRL")

	money, _ := domain.NewMoneyFromString("10.00", "BRL")
	tx, err := domain.NewExternalWagerTransaction("ext-1", "provider-dono", "key-1", "hash", walletID, playerID, "", "", domain.KindBet, money, "")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := tx.MarkProcessed(money); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store.txs[tx.ID()] = tx

	req := httptest.NewRequest(http.MethodGet, "/wagering/transactions/"+tx.ID().String(), nil)
	req = withURLParam(req, "transactionId", tx.ID().String())
	req = asProvider(req, "provider-outro") // não é o dono

	rec := httptest.NewRecorder()
	h.GetByID(rec, req)

	// 404, não 403: um provider não pode nem confirmar que o id existe
	// (ver comentário em wager_handler.go).
	assertErrorStatus(t, rec, http.StatusNotFound, "TRANSACTION_NOT_FOUND")
}

func TestWagerHandler_GetByID_DonoConsegueVer(t *testing.T) {
	h, store := newWagerHandlerForTest()
	playerID := uuid.New()
	walletID := seedWallet(t, store, playerID, "100.00", "BRL")

	money, _ := domain.NewMoneyFromString("10.00", "BRL")
	tx, err := domain.NewExternalWagerTransaction("ext-1", "provider-dono", "key-1", "hash", walletID, playerID, "", "", domain.KindBet, money, "")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := tx.MarkProcessed(money); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store.txs[tx.ID()] = tx

	req := httptest.NewRequest(http.MethodGet, "/wagering/transactions/"+tx.ID().String(), nil)
	req = withURLParam(req, "transactionId", tx.ID().String())
	req = asProvider(req, "provider-dono")

	rec := httptest.NewRecorder()
	h.GetByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestWagerHandler_GetByProviderAndExternalID_ProviderMismatch_Retorna403(t *testing.T) {
	h, _ := newWagerHandlerForTest()

	req := httptest.NewRequest(http.MethodGet, "/providers/provider-outro/wagering/transactions/ext-1", nil)
	req = withURLParam(req, "providerId", "provider-outro")
	req = withURLParam(req, "externalTransactionId", "ext-1")
	req = asProvider(req, "provider-a")

	rec := httptest.NewRecorder()
	h.GetByProviderAndExternalID(rec, req)

	// Aqui é 403, não 404: diferente de GetByID, a URL já afirma o
	// providerId pedido — não há nada a "esconder" (ver comentário em
	// wager_handler.go).
	assertErrorStatus(t, rec, http.StatusForbidden, "PROVIDER_MISMATCH")
}
