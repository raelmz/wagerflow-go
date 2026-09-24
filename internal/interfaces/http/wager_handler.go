package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/domain"
)

type WagerHandler struct {
	processWager   *application.ProcessWagerTransactionUseCase
	getTransaction *application.GetWagerTransactionUseCase
}

func NewWagerHandler(
	processWager *application.ProcessWagerTransactionUseCase,
	getTransaction *application.GetWagerTransactionUseCase,
) *WagerHandler {
	return &WagerHandler{processWager: processWager, getTransaction: getTransaction}
}

// Process trata POST /wagering/transactions. O header Idempotency-Key
// é obrigatório (seção 9 do desafio) — o servidor NUNCA o substitui
// por um valor calculado, mesmo que o cliente pudesse ter construído
// ele como "{providerId}:{externalTransactionId}".
func (h *WagerHandler) Process(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, errInvalidInput{msg: "header Idempotency-Key é obrigatório"})
		return
	}

	var req processWagerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}

	// Isolamento entre provedores (seção "Autenticação e
	// autorização" do desafio): o providerId do TOKEN (claim "azp",
	// já validado pelo AuthMiddleware) precisa bater com o
	// providerId que veio no CORPO da requisição. Sem essa checagem,
	// um provider autenticado poderia processar operação em nome de
	// outro só preenchendo outro providerId no JSON. O role
	// "internal" fica de fora dessa regra de propósito (é o serviço
	// interno, não "é dono" de um provider só).
	if !IsInternal(r.Context()) {
		tokenProviderID, _ := AuthenticatedProviderID(r.Context())
		if req.ProviderID != tokenProviderID {
			writeError(w, errForbidden{msg: "providerId do token não corresponde ao providerId da requisição"})
			return
		}
	}

	playerID, err := parseUUID(req.PlayerID)
	if err != nil {
		writeError(w, err)
		return
	}
	walletID, err := parseUUID(req.WalletID)
	if err != nil {
		writeError(w, err)
		return
	}

	cmd := application.ProcessWagerCommand{
		IdempotencyKey:                 idempotencyKey,
		ProviderID:                     req.ProviderID,
		ExternalTransactionID:          req.ExternalTransactionID,
		PlayerID:                       playerID,
		WalletID:                       walletID,
		RoundID:                        req.RoundID,
		GameID:                         req.GameID,
		Kind:                           domain.WagerKind(req.Kind),
		Amount:                         req.Money.Amount,
		Currency:                       req.Money.Currency,
		ReferenceExternalTransactionID: req.ReferenceExternalTransactionID,
	}

	result, err := h.processWager.Execute(r.Context(), cmd)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, statusForWagerResult(result), wagerResultToResponse(result.Transaction, result.Replay))
}

// statusForWagerResult escolhe o status HTTP de sucesso conforme o
// que aconteceu com a operação — a seção 9 do desafio exige que
// "processamento pendente" seja distinguível no contrato:
//
//	200 OK       -> replay idempotente, ou operação processada e REJECTED
//	               (a requisição foi aceita e avaliada; a recusa é o
//	               RESULTADO do negócio, não um erro HTTP)
//	201 Created  -> operação nova, PROCESSED com sucesso
//	202 Accepted -> operação nova, aguardando referência (PENDING_REFERENCE)
func statusForWagerResult(result *application.ProcessWagerResult) int {
	if result.Replay {
		return http.StatusOK
	}
	switch result.Transaction.Status() {
	case domain.StatusProcessed:
		return http.StatusCreated
	case domain.StatusPendingReference:
		return http.StatusAccepted
	default: // StatusRejected
		return http.StatusOK
	}
}

// GetByID trata GET /wagering/transactions/:transactionId.
func (h *WagerHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "transactionId"))
	if err != nil {
		writeError(w, err)
		return
	}

	tx, err := h.getTransaction.ByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	// Isolamento entre provedores também vale para replay/consulta
	// por id interno (a seção "Autenticação e autorização" do
	// desafio é explícita: "inclusive em consultas e replays"). Aqui
	// devolvemos 404 em vez de 403: um provider tentando adivinhar
	// ids de outro provider não pode nem CONFIRMAR que aquele id
	// existe — é a mesma resposta que ele receberia para um id que
	// nunca existiu.
	if !IsInternal(r.Context()) {
		tokenProviderID, _ := AuthenticatedProviderID(r.Context())
		if tx.ProviderID() != tokenProviderID {
			writeError(w, application.ErrWagerTransactionNotFound)
			return
		}
	}

	writeJSON(w, http.StatusOK, wagerTransactionToResponse(tx))
}

// GetByProviderAndExternalID trata
// GET /providers/:providerId/wagering/transactions/:externalTransactionId.
func (h *WagerHandler) GetByProviderAndExternalID(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "providerId")
	externalTransactionID := chi.URLParam(r, "externalTransactionId")

	// Aqui o providerId já vem na URL, então a checagem acontece
	// ANTES de consultar o banco (não precisa nem gastar uma query
	// para um provider pedindo o recurso de outro provider — a URL
	// já denuncia a tentativa, então 403 é a resposta certa, não 404:
	// diferente do GetByID acima, aqui o provider já está afirmando
	// "quero o providerId X", então não há nada a esconder sobre
	// existência).
	if !IsInternal(r.Context()) {
		tokenProviderID, _ := AuthenticatedProviderID(r.Context())
		if providerID != tokenProviderID {
			writeError(w, errForbidden{msg: "providerId do token não corresponde ao providerId da URL"})
			return
		}
	}

	tx, err := h.getTransaction.ByProviderAndExternalID(r.Context(), providerID, externalTransactionID)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, wagerTransactionToResponse(tx))
}
