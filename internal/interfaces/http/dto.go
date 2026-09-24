// Pacote http contém a camada de entrada HTTP: DTOs (structs de
// request/response em JSON), handlers (traduzem HTTP <-> casos de
// uso) e o roteamento. Nada aqui é regra de negócio — isso mora em
// domain/application. Este pacote só sabe "receber JSON, chamar o
// caso de uso certo, devolver JSON e o status HTTP certo".
package http

import (
	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// moneyDTO é como dinheiro entra e sai em JSON — sempre como STRING
// decimal ("25.00"), nunca número (a seção 6.1 do desafio proíbe
// float em qualquer ponto do contrato, e um number JSON vira float
// em quase todo client).
type moneyDTO struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

func moneyToDTO(m domain.Money) moneyDTO {
	return moneyDTO{Amount: m.String(), Currency: m.Currency()}
}

// --- POST /wallets ---

type openWalletRequest struct {
	PlayerID       string   `json:"playerId"`
	InitialBalance moneyDTO `json:"initialBalance"`
}

type walletResponse struct {
	ID       string   `json:"id"`
	PlayerID string   `json:"playerId"`
	Balance  moneyDTO `json:"balance"`
	Version  int64    `json:"version"`
}

func walletToResponse(w *domain.Wallet) walletResponse {
	return walletResponse{
		ID:       w.ID().String(),
		PlayerID: w.PlayerID().String(),
		Balance:  moneyToDTO(w.Balance()),
		Version:  w.Version(),
	}
}

// --- GET /wallets/:walletId/ledger ---

type ledgerEntryResponse struct {
	ID            string   `json:"id"`
	TransactionID string   `json:"transactionId"`
	Direction     string   `json:"direction"`
	Amount        moneyDTO `json:"amount"`
	BalanceBefore moneyDTO `json:"balanceBefore"`
	BalanceAfter  moneyDTO `json:"balanceAfter"`
	CreatedAt     string   `json:"createdAt"`
}

type ledgerResponse struct {
	Entries    []ledgerEntryResponse `json:"entries"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

// ledgerEntryToResponse recebe currency à parte porque a entidade não
// a guarda (ver comentário em ListByWallet, na infraestrutura) — ela
// é sempre a moeda da carteira dona do lançamento.
func ledgerEntryToResponse(e *domain.WalletLedgerEntry, currency string) ledgerEntryResponse {
	withCurrency := func(m domain.Money) moneyDTO {
		return moneyToDTO(domain.MoneyFromCents(m.Cents(), currency))
	}
	return ledgerEntryResponse{
		ID:            e.ID().String(),
		TransactionID: e.TransactionID().String(),
		Direction:     string(e.Direction()),
		Amount:        withCurrency(e.Amount()),
		BalanceBefore: withCurrency(e.BalanceBefore()),
		BalanceAfter:  withCurrency(e.BalanceAfter()),
		CreatedAt:     e.CreatedAt().Format("2006-01-02T15:04:05.000Z07:00"),
	}
}

// --- POST /wagering/transactions ---

type processWagerRequest struct {
	ProviderID                     string   `json:"providerId"`
	ExternalTransactionID          string   `json:"externalTransactionId"`
	PlayerID                       string   `json:"playerId"`
	WalletID                       string   `json:"walletId"`
	RoundID                        string   `json:"roundId"`
	GameID                         string   `json:"gameId"`
	Kind                           string   `json:"kind"`
	Money                          moneyDTO `json:"money"`
	ReferenceExternalTransactionID string   `json:"referenceExternalTransactionId,omitempty"`
}

type processWagerResponse struct {
	TransactionID    string    `json:"transactionId"`
	Status           string    `json:"status"`
	Balance          *moneyDTO `json:"balance,omitempty"`
	FailureCode      string    `json:"failureCode,omitempty"`
	IdempotentReplay bool      `json:"idempotentReplay"`
}

func wagerResultToResponse(tx *domain.WagerTransaction, replay bool) processWagerResponse {
	resp := processWagerResponse{
		TransactionID:    tx.ID().String(),
		Status:           string(tx.Status()),
		FailureCode:      tx.FailureCode(),
		IdempotentReplay: replay,
	}
	if balance, ok := tx.ResultingBalance(); ok {
		dto := moneyToDTO(balance)
		resp.Balance = &dto
	}
	return resp
}

// --- GET /wagering/transactions/:id e .../providers/:providerId/... ---

type wagerTransactionResponse struct {
	ID                             string    `json:"id"`
	ExternalTransactionID          string    `json:"externalTransactionId,omitempty"`
	ProviderID                     string    `json:"providerId,omitempty"`
	WalletID                       string    `json:"walletId"`
	PlayerID                       string    `json:"playerId"`
	RoundID                        string    `json:"roundId,omitempty"`
	GameID                         string    `json:"gameId,omitempty"`
	Kind                           string    `json:"kind"`
	Money                          moneyDTO  `json:"money"`
	ReferenceExternalTransactionID string    `json:"referenceExternalTransactionId,omitempty"`
	Status                         string    `json:"status"`
	FailureCode                    string    `json:"failureCode,omitempty"`
	Balance                        *moneyDTO `json:"balance,omitempty"`
	CreatedAt                      string    `json:"createdAt"`
	UpdatedAt                      string    `json:"updatedAt"`
}

func wagerTransactionToResponse(tx *domain.WagerTransaction) wagerTransactionResponse {
	resp := wagerTransactionResponse{
		ID:                             tx.ID().String(),
		ExternalTransactionID:          tx.ExternalTransactionID(),
		ProviderID:                     tx.ProviderID(),
		WalletID:                       tx.WalletID().String(),
		PlayerID:                       tx.PlayerID().String(),
		RoundID:                        tx.RoundID(),
		GameID:                         tx.GameID(),
		Kind:                           string(tx.Kind()),
		Money:                          moneyToDTO(tx.Money()),
		ReferenceExternalTransactionID: tx.ReferenceExternalTxID(),
		Status:                         string(tx.Status()),
		FailureCode:                    tx.FailureCode(),
		CreatedAt:                      tx.CreatedAt().Format("2006-01-02T15:04:05.000Z07:00"),
		UpdatedAt:                      tx.UpdatedAt().Format("2006-01-02T15:04:05.000Z07:00"),
	}
	if balance, ok := tx.ResultingBalance(); ok {
		dto := moneyToDTO(balance)
		resp.Balance = &dto
	}
	return resp
}

// --- POST /wallets/:walletId/reconciliation ---

type reconciliationResponse struct {
	WalletID          string   `json:"walletId"`
	StoredBalance     moneyDTO `json:"storedBalance"`
	CalculatedBalance moneyDTO `json:"calculatedBalance"`
	Difference        moneyDTO `json:"difference"`
	Consistent        bool     `json:"consistent"`
	CheckedEntries    int      `json:"checkedEntries"`
}

// parseUUID é um atalho comum a vários handlers: parseia e, em caso
// de erro, devolve um erro já classificável por mapError como 400.
func parseUUID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, errInvalidInput{msg: "identificador inválido: " + raw}
	}
	return id, nil
}
