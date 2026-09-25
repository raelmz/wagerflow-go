package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/domain"
)

// newWalletHandlerForTest monta um WalletHandler de verdade (os casos
// de uso reais da application/), só trocando a persistência por um
// fakeStore em memória. É assim que testamos o CONTRATO HTTP
// (formato do JSON, status code, validação de entrada) sem precisar
// de Postgres — a lógica de negócio em si já está coberta pelos
// testes de domain/application.
func newWalletHandlerForTest() (*WalletHandler, *fakeStore) {
	store := newFakeStore()
	h := NewWalletHandler(
		application.NewOpenWalletUseCase(store),
		application.NewGetWalletUseCase(&fakeWalletRepo{store}),
		application.NewGetWalletLedgerUseCase(&fakeWalletRepo{store}, &fakeLedgerRepo{store}),
		application.NewReconciliationUseCase(store),
	)
	return h, store
}

// withURLParam simula o roteamento do chi para testar o handler
// isoladamente (sem subir o router inteiro) — o mesmo padrão usado
// para testar handlers chi sem montar toda a árvore de rotas.
// withURLParam simula o roteamento do chi para testar o handler
// isoladamente. Pode ser encadeado (req = withURLParam(withURLParam(req,
// "a", "1"), "b", "2")) para simular rotas com mais de um parâmetro —
// ele reaproveita o *chi.Context já anexado à requisição, se houver.
func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		rctx = chi.NewRouteContext()
	}
	rctx.URLParams.Add(key, value)
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
	return r.WithContext(ctx)
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
		t.Fatalf("resposta não é um JSON válido: %v (body=%q)", err, rec.Body.String())
	}
}

func TestWalletHandler_Create_Success(t *testing.T) {
	h, _ := newWalletHandlerForTest()

	playerID := uuid.New()
	body, _ := json.Marshal(openWalletRequest{
		PlayerID:       playerID.String(),
		InitialBalance: moneyDTO{Amount: "100.00", Currency: "BRL"},
	})

	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperava 201, veio %d (body=%s)", rec.Code, rec.Body.String())
	}

	var resp walletResponse
	decodeBody(t, rec, &resp)

	if resp.PlayerID != playerID.String() {
		t.Fatalf("playerId esperado %q, veio %q", playerID.String(), resp.PlayerID)
	}
	// Dinheiro sempre como STRING decimal no contrato — nunca number
	// (a seção 6.1 do desafio proíbe float em qualquer ponto).
	if resp.Balance.Amount != "100.00" || resp.Balance.Currency != "BRL" {
		t.Fatalf("balance esperado {100.00 BRL}, veio %+v", resp.Balance)
	}
	if resp.Version != 1 {
		t.Fatalf("esperava version 1 numa carteira nova, veio %d", resp.Version)
	}
}

func TestWalletHandler_Create_JSONMalformado(t *testing.T) {
	h, _ := newWalletHandlerForTest()

	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader([]byte("{não é json")))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assertErrorStatus(t, rec, http.StatusBadRequest, "INVALID_INPUT")
}

func TestWalletHandler_Create_PlayerIDInvalido(t *testing.T) {
	h, _ := newWalletHandlerForTest()

	body, _ := json.Marshal(openWalletRequest{
		PlayerID:       "não-é-um-uuid",
		InitialBalance: moneyDTO{Amount: "10.00", Currency: "BRL"},
	})
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assertErrorStatus(t, rec, http.StatusBadRequest, "INVALID_INPUT")
}

func TestWalletHandler_Create_SaldoNegativo(t *testing.T) {
	h, _ := newWalletHandlerForTest()

	body, _ := json.Marshal(openWalletRequest{
		PlayerID:       uuid.New().String(),
		InitialBalance: moneyDTO{Amount: "-10.00", Currency: "BRL"},
	})
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	// NewMoneyFromString já rejeita valor negativo -> 400, antes
	// mesmo de chegar no domain.NewWallet.
	assertErrorStatus(t, rec, http.StatusBadRequest, "INVALID_INPUT")
}

func TestWalletHandler_Create_CarteiraDuplicada(t *testing.T) {
	h, _ := newWalletHandlerForTest()
	playerID := uuid.New()

	body, _ := json.Marshal(openWalletRequest{
		PlayerID:       playerID.String(),
		InitialBalance: moneyDTO{Amount: "10.00", Currency: "BRL"},
	})

	req1 := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(body))
	rec1 := httptest.NewRecorder()
	h.Create(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("primeira criação deveria ter sucesso, veio %d (body=%s)", rec1.Code, rec1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(body))
	rec2 := httptest.NewRecorder()
	h.Create(rec2, req2)

	assertErrorStatus(t, rec2, http.StatusConflict, "WALLET_ALREADY_EXISTS")
}

func TestWalletHandler_Get_NaoEncontrada(t *testing.T) {
	h, _ := newWalletHandlerForTest()

	req := httptest.NewRequest(http.MethodGet, "/wallets/"+uuid.New().String(), nil)
	req = withURLParam(req, "walletId", uuid.New().String())
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertErrorStatus(t, rec, http.StatusNotFound, "WALLET_NOT_FOUND")
}

func TestWalletHandler_Get_IDInvalido(t *testing.T) {
	h, _ := newWalletHandlerForTest()

	req := httptest.NewRequest(http.MethodGet, "/wallets/xyz", nil)
	req = withURLParam(req, "walletId", "xyz")
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertErrorStatus(t, rec, http.StatusBadRequest, "INVALID_INPUT")
}

func TestWalletHandler_Get_Success(t *testing.T) {
	h, store := newWalletHandlerForTest()

	playerID := uuid.New()
	balance, err := domain.NewMoneyFromString("50.00", "BRL")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	wallet, err := domain.NewWallet(playerID, balance)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	store.wallets[wallet.ID()] = wallet

	req := httptest.NewRequest(http.MethodGet, "/wallets/"+wallet.ID().String(), nil)
	req = withURLParam(req, "walletId", wallet.ID().String())
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d (body=%s)", rec.Code, rec.Body.String())
	}
	var resp walletResponse
	decodeBody(t, rec, &resp)
	if resp.ID != wallet.ID().String() {
		t.Fatalf("id esperado %q, veio %q", wallet.ID().String(), resp.ID)
	}
	if resp.Balance.Amount != "50.00" {
		t.Fatalf("balance esperado 50.00, veio %q", resp.Balance.Amount)
	}
}

func TestWalletHandler_Ledger_LimitInvalido(t *testing.T) {
	h, _ := newWalletHandlerForTest()

	walletID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/wallets/"+walletID.String()+"/ledger?limit=abc", nil)
	req = withURLParam(req, "walletId", walletID.String())
	rec := httptest.NewRecorder()
	h.Ledger(rec, req)

	assertErrorStatus(t, rec, http.StatusBadRequest, "INVALID_INPUT")
}

func TestWalletHandler_Ledger_LimitZeroOuNegativoEhInvalido(t *testing.T) {
	h, _ := newWalletHandlerForTest()
	walletID := uuid.New()

	for _, limit := range []string{"0", "-5"} {
		req := httptest.NewRequest(http.MethodGet, "/wallets/"+walletID.String()+"/ledger?limit="+limit, nil)
		req = withURLParam(req, "walletId", walletID.String())
		rec := httptest.NewRecorder()
		h.Ledger(rec, req)

		assertErrorStatus(t, rec, http.StatusBadRequest, "INVALID_INPUT")
	}
}

func TestWalletHandler_Ledger_CarteiraNaoEncontrada(t *testing.T) {
	h, _ := newWalletHandlerForTest()

	req := httptest.NewRequest(http.MethodGet, "/wallets/"+uuid.New().String()+"/ledger", nil)
	req = withURLParam(req, "walletId", uuid.New().String())
	rec := httptest.NewRecorder()
	h.Ledger(rec, req)

	assertErrorStatus(t, rec, http.StatusNotFound, "WALLET_NOT_FOUND")
}

func TestWalletHandler_Ledger_FormatoDaResposta(t *testing.T) {
	h, store := newWalletHandlerForTest()

	balance, _ := domain.NewMoneyFromString("0.00", "BRL")
	wallet, _ := domain.NewWallet(uuid.New(), balance)
	store.wallets[wallet.ID()] = wallet

	req := httptest.NewRequest(http.MethodGet, "/wallets/"+wallet.ID().String()+"/ledger", nil)
	req = withURLParam(req, "walletId", wallet.ID().String())
	rec := httptest.NewRecorder()
	h.Ledger(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d (body=%s)", rec.Code, rec.Body.String())
	}
	var resp ledgerResponse
	decodeBody(t, rec, &resp)
	// Sem lançamentos ainda: lista vazia (nunca null) e sem próxima página.
	if resp.Entries == nil {
		t.Fatal("entries deveria ser lista vazia, não null, mesmo sem lançamentos")
	}
	if resp.NextCursor != "" {
		t.Fatalf("nextCursor deveria vir vazio sem mais páginas, veio %q", resp.NextCursor)
	}
}

// assertErrorStatus confere status HTTP e o `code` estável do corpo de
// erro (apiErrorBody) — o desafio exige que o contrato seja
// "distinguível programaticamente", não só o status.
func assertErrorStatus(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("esperava status %d, veio %d (body=%s)", wantStatus, rec.Code, rec.Body.String())
	}
	var body apiErrorBody
	decodeBody(t, rec, &body)
	if body.Code != wantCode {
		t.Fatalf("esperava code %q, veio %q", wantCode, body.Code)
	}
}
