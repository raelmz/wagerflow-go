package messaging

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/domain"
)

// wagerMessageBody é o formato JSON esperado no corpo de uma
// mensagem em wager-transactions.fifo. Espelha processWagerRequest
// do pacote HTTP, com um campo a mais: IdempotencyKey, que no canal
// HTTP vem de um header dedicado, mas aqui precisa estar no corpo
// (SQS não tem "headers de aplicação" equivalentes de forma nativa).
type wagerMessageBody struct {
	IdempotencyKey        string `json:"idempotencyKey"`
	ProviderID            string `json:"providerId"`
	ExternalTransactionID string `json:"externalTransactionId"`
	PlayerID              string `json:"playerId"`
	WalletID              string `json:"walletId"`
	RoundID               string `json:"roundId"`
	GameID                string `json:"gameId"`
	Kind                  string `json:"kind"`
	Money                 struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	} `json:"money"`
	ReferenceExternalTransactionID string `json:"referenceExternalTransactionId,omitempty"`
}

// ParseWagerMessage decodifica o corpo de uma mensagem SQS e monta o
// InboundWagerMessage que o caso de uso espera. messageID vem de fora
// (atributo MessageId da mensagem SQS, não do corpo) — é a mesma
// separação entre "identidade de transporte" e "conteúdo de negócio"
// que o hash de payload já faz no canal HTTP.
//
// Qualquer erro aqui é permanente (application.IsPermanentWagerError
// reconhece o tipo devolvido): uma mensagem com JSON malformado ou
// campo obrigatório ausente nunca vai ficar válida só por ser
// reentregue.
func ParseWagerMessage(messageID string, body []byte) (application.InboundWagerMessage, error) {
	var raw wagerMessageBody
	if err := json.Unmarshal(body, &raw); err != nil {
		return application.InboundWagerMessage{}, application.NewInvalidMessageError("corpo da mensagem não é um JSON válido: %v", err)
	}
	if raw.IdempotencyKey == "" {
		return application.InboundWagerMessage{}, application.NewInvalidMessageError("campo idempotencyKey é obrigatório")
	}

	playerID, err := uuid.Parse(raw.PlayerID)
	if err != nil {
		return application.InboundWagerMessage{}, application.NewInvalidMessageError("playerId inválido: %v", err)
	}
	walletID, err := uuid.Parse(raw.WalletID)
	if err != nil {
		return application.InboundWagerMessage{}, application.NewInvalidMessageError("walletId inválido: %v", err)
	}

	return application.InboundWagerMessage{
		MessageID: messageID,
		Command: application.ProcessWagerCommand{
			IdempotencyKey:                 raw.IdempotencyKey,
			ProviderID:                     raw.ProviderID,
			ExternalTransactionID:          raw.ExternalTransactionID,
			PlayerID:                       playerID,
			WalletID:                       walletID,
			RoundID:                        raw.RoundID,
			GameID:                         raw.GameID,
			Kind:                           domain.WagerKind(raw.Kind),
			Amount:                         raw.Money.Amount,
			Currency:                       raw.Money.Currency,
			ReferenceExternalTransactionID: raw.ReferenceExternalTransactionID,
		},
	}, nil
}
