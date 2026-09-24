package http

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/domain"
)

// WalletHandler agrupa os handlers de /wallets/... . Cada dependência
// é o caso de uso específico daquela rota — o handler não conhece
// repositório nem transação, só chama Execute e traduz o resultado.
type WalletHandler struct {
	openWallet     *application.OpenWalletUseCase
	getWallet      *application.GetWalletUseCase
	getLedger      *application.GetWalletLedgerUseCase
	reconciliation *application.ReconciliationUseCase
}

func NewWalletHandler(
	openWallet *application.OpenWalletUseCase,
	getWallet *application.GetWalletUseCase,
	getLedger *application.GetWalletLedgerUseCase,
	reconciliation *application.ReconciliationUseCase,
) *WalletHandler {
	return &WalletHandler{
		openWallet:     openWallet,
		getWallet:      getWallet,
		getLedger:      getLedger,
		reconciliation: reconciliation,
	}
}

// Create trata POST /wallets.
func (h *WalletHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req openWalletRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}

	playerID, err := parseUUID(req.PlayerID)
	if err != nil {
		writeError(w, err)
		return
	}

	initialBalance, err := domain.NewMoneyFromString(req.InitialBalance.Amount, req.InitialBalance.Currency)
	if err != nil {
		writeError(w, err)
		return
	}

	result, err := h.openWallet.Execute(r.Context(), playerID, initialBalance)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, walletToResponse(result.Wallet))
}

// Get trata GET /wallets/:walletId.
func (h *WalletHandler) Get(w http.ResponseWriter, r *http.Request) {
	walletID, err := parseUUID(chi.URLParam(r, "walletId"))
	if err != nil {
		writeError(w, err)
		return
	}

	wallet, err := h.getWallet.Execute(r.Context(), walletID)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, walletToResponse(wallet))
}

// Ledger trata GET /wallets/:walletId/ledger?cursor=...&limit=50.
func (h *WalletHandler) Ledger(w http.ResponseWriter, r *http.Request) {
	walletID, err := parseUUID(chi.URLParam(r, "walletId"))
	if err != nil {
		writeError(w, err)
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed <= 0 {
			writeError(w, errInvalidInput{msg: "limit deve ser um inteiro positivo"})
			return
		}
		limit = parsed
	}

	result, err := h.getLedger.Execute(r.Context(), walletID, cursor, limit)
	if err != nil {
		writeError(w, err)
		return
	}

	entries := make([]ledgerEntryResponse, 0, len(result.Entries))
	for _, e := range result.Entries {
		entries = append(entries, ledgerEntryToResponse(e, result.Currency))
	}

	writeJSON(w, http.StatusOK, ledgerResponse{Entries: entries, NextCursor: result.NextCursor})
}

// Reconciliation trata POST /wallets/:walletId/reconciliation.
func (h *WalletHandler) Reconciliation(w http.ResponseWriter, r *http.Request) {
	walletID, err := parseUUID(chi.URLParam(r, "walletId"))
	if err != nil {
		writeError(w, err)
		return
	}

	result, err := h.reconciliation.Execute(r.Context(), walletID)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, reconciliationResponse{
		WalletID:          result.WalletID.String(),
		StoredBalance:     moneyToDTO(result.StoredBalance),
		CalculatedBalance: moneyToDTO(result.CalculatedBalance),
		Difference:        moneyToDTO(result.Difference),
		Consistent:        result.Consistent,
		CheckedEntries:    result.CheckedEntries,
	})
}
