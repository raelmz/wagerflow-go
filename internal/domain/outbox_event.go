package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Este arquivo define os EVENTOS DE INTEGRAÇÃO que o serviço publica
// para o mundo externo (seção 11 do desafio) e o envelope comum deles.
//
// Como funciona o padrão "transactional outbox":
//  1. Na MESMA transação SQL que altera saldo, ledger e transação, o
//     caso de uso grava também o evento na tabela outbox_events
//     (via OutboxRepository.Append);
//  2. Só DEPOIS do commit, um worker separado lê a outbox e publica.
//
// Assim nunca existe evento sem o fato que o originou, nem fato
// confirmado sem evento — e nada é publicado antes do commit
// (critério eliminatório do desafio).
//
// Este arquivo NÃO publica nada: só CONSTRÓI os eventos. O worker
// publicador é um passo posterior.

// Tipos dos eventos (seção 11). O tipo e a versão de cada evento são
// definidos pelo CONSTRUTOR dele, nunca por quem chama.
const (
	EventTypeWagerTransactionProcessed        = "WagerTransactionProcessed"
	EventTypeWagerTransactionRejected         = "WagerTransactionRejected"
	EventTypeWalletBalanceChanged             = "WalletBalanceChanged"
	EventTypeWagerTransactionPendingReference = "WagerTransactionPendingReference"
)

// eventVersion é a versão do contrato de payload. Ao mudar o formato de
// um evento de forma incompatível, o construtor dele passa a emitir a
// versão seguinte, e consumidores antigos conseguem distinguir.
const eventVersion = 1

// occurredAtLayout: RFC 3339, sempre em UTC, com milissegundos —
// ex: "2026-09-23T12:00:00.000Z" (mesmo formato do exemplo do desafio).
const occurredAtLayout = "2006-01-02T15:04:05.000Z07:00"

var ErrInvalidOutboxEvent = errors.New("evento de outbox inválido")

// OutboxEvent é um evento de integração pronto para ser gravado na
// outbox. É IMUTÁVEL: não há setters, e o payload é um SNAPSHOT — o
// JSON completo do envelope, serializado no momento da criação. Mudar
// a transação ou a carteira depois não altera o que foi registrado.
type OutboxEvent struct {
	id            uuid.UUID // eventId: estável, inclusive se o evento for republicado
	eventType     string
	aggregateID   uuid.UUID
	correlationID string
	causationID   uuid.UUID // uuid.Nil = sem causa registrada (campo opcional)
	occurredAt    time.Time
	version       int
	payload       []byte // envelope completo em JSON
}

func (e *OutboxEvent) ID() uuid.UUID          { return e.id }
func (e *OutboxEvent) Type() string           { return e.eventType }
func (e *OutboxEvent) AggregateID() uuid.UUID { return e.aggregateID }
func (e *OutboxEvent) CorrelationID() string  { return e.correlationID }
func (e *OutboxEvent) OccurredAt() time.Time  { return e.occurredAt }
func (e *OutboxEvent) Version() int           { return e.version }

// CausationID devolve o id do evento que causou este, se houver.
func (e *OutboxEvent) CausationID() (uuid.UUID, bool) {
	return e.causationID, e.causationID != uuid.Nil
}

// Payload devolve uma CÓPIA do JSON do envelope, para o chamador não
// conseguir alterar o snapshot guardado no evento.
func (e *OutboxEvent) Payload() []byte {
	out := make([]byte, len(e.payload))
	copy(out, e.payload)
	return out
}

// ---------------------------------------------------------------------
// Envelope e tipos de dados
// ---------------------------------------------------------------------

// outboxEnvelope é o formato que vai para o payload (seção 11):
// eventId, eventType, aggregateId, correlationId, causationId
// (opcional), occurredAt, version e data tipado.
type outboxEnvelope struct {
	EventID       string `json:"eventId"`
	EventType     string `json:"eventType"`
	AggregateID   string `json:"aggregateId"`
	CorrelationID string `json:"correlationId"`
	CausationID   string `json:"causationId,omitempty"`
	OccurredAt    string `json:"occurredAt"`
	Version       int    `json:"version"`
	Data          any    `json:"data"`
}

// MoneyPayload é o dinheiro no formato do contrato externo: string
// decimal + moeda. Nenhum float passa por aqui.
type MoneyPayload struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

func moneyPayload(m Money) MoneyPayload {
	return MoneyPayload{Amount: m.String(), Currency: m.Currency()}
}

// wagerTransactionCore reúne os campos comuns aos eventos de transação.
// Os campos de origem externa (provedor, id externo, rodada, jogo,
// referência) usam omitempty: numa abertura de carteira (OPENING) eles
// não se aplicam e simplesmente não aparecem (seção 9 do desafio).
type wagerTransactionCore struct {
	TransactionID                  string       `json:"transactionId"`
	WalletID                       string       `json:"walletId"`
	PlayerID                       string       `json:"playerId"`
	Kind                           string       `json:"kind"`
	Money                          MoneyPayload `json:"money"`
	ProviderID                     string       `json:"providerId,omitempty"`
	ExternalTransactionID          string       `json:"externalTransactionId,omitempty"`
	RoundID                        string       `json:"roundId,omitempty"`
	GameID                         string       `json:"gameId,omitempty"`
	ReferenceExternalTransactionID string       `json:"referenceExternalTransactionId,omitempty"`
	ReferenceTransactionID         string       `json:"referenceTransactionId,omitempty"`
}

func newWagerTransactionCore(txn *WagerTransaction) wagerTransactionCore {
	core := wagerTransactionCore{
		TransactionID:                  txn.ID().String(),
		WalletID:                       txn.WalletID().String(),
		PlayerID:                       txn.PlayerID().String(),
		Kind:                           string(txn.Kind()),
		Money:                          moneyPayload(txn.Money()),
		ProviderID:                     txn.ProviderID(),
		ExternalTransactionID:          txn.ExternalTransactionID(),
		RoundID:                        txn.RoundID(),
		GameID:                         txn.GameID(),
		ReferenceExternalTransactionID: txn.ReferenceExternalTxID(),
	}
	if ref := txn.ResolvedReferenceID(); ref != uuid.Nil {
		core.ReferenceTransactionID = ref.String()
	}
	return core
}

// WagerTransactionProcessedData: operação concluída com sucesso
// (inclusive LOSS, que não movimenta saldo).
type WagerTransactionProcessedData struct {
	wagerTransactionCore
	ResultingBalance MoneyPayload `json:"resultingBalance"`
}

// WagerTransactionRejectedData: rejeição definitiva por regra de negócio.
type WagerTransactionRejectedData struct {
	wagerTransactionCore
	FailureCode string `json:"failureCode"`
}

// WagerTransactionPendingReferenceData: a operação ficou esperando uma
// referência que ainda não está disponível.
type WagerTransactionPendingReferenceData struct {
	wagerTransactionCore
}

// WalletBalanceChangedData: alteração efetiva do saldo (seção 11).
type WalletBalanceChangedData struct {
	WalletID      string       `json:"walletId"`
	TransactionID string       `json:"transactionId"`
	Direction     string       `json:"direction"`
	Money         MoneyPayload `json:"money"`
	BalanceBefore MoneyPayload `json:"balanceBefore"`
	BalanceAfter  MoneyPayload `json:"balanceAfter"`
	WalletVersion int64        `json:"walletVersion"`
}

// ---------------------------------------------------------------------
// Construtores dos eventos
// ---------------------------------------------------------------------

// NewWagerTransactionProcessedEvent cria o evento de operação concluída.
// Exige que a transação já esteja PROCESSED com saldo resultante: o
// evento nunca descreve algo que ainda não aconteceu.
//
// correlationID liga o evento à requisição/mensagem que o originou.
// aggregateId é o id da transação.
func NewWagerTransactionProcessedEvent(txn *WagerTransaction, correlationID string) (*OutboxEvent, error) {
	if txn.Status() != StatusProcessed {
		return nil, fmt.Errorf("%w: %s exige transação PROCESSED, veio %s",
			ErrInvalidOutboxEvent, EventTypeWagerTransactionProcessed, txn.Status())
	}
	resulting, ok := txn.ResultingBalance()
	if !ok {
		return nil, fmt.Errorf("%w: transação PROCESSED sem saldo resultante", ErrInvalidOutboxEvent)
	}

	data := WagerTransactionProcessedData{
		wagerTransactionCore: newWagerTransactionCore(txn),
		ResultingBalance:     moneyPayload(resulting),
	}
	return newOutboxEvent(EventTypeWagerTransactionProcessed, txn.ID(), correlationID, uuid.Nil, data)
}

// NewWagerTransactionRejectedEvent cria o evento de rejeição definitiva.
// Exige transação REJECTED (que, pelo domínio, sempre tem failureCode).
func NewWagerTransactionRejectedEvent(txn *WagerTransaction, correlationID string) (*OutboxEvent, error) {
	if txn.Status() != StatusRejected {
		return nil, fmt.Errorf("%w: %s exige transação REJECTED, veio %s",
			ErrInvalidOutboxEvent, EventTypeWagerTransactionRejected, txn.Status())
	}

	data := WagerTransactionRejectedData{
		wagerTransactionCore: newWagerTransactionCore(txn),
		FailureCode:          txn.FailureCode(),
	}
	return newOutboxEvent(EventTypeWagerTransactionRejected, txn.ID(), correlationID, uuid.Nil, data)
}

// NewWagerTransactionPendingReferenceEvent cria o evento de "esperando
// referência". Exige transação PENDING_REFERENCE com a referência
// externa informada (é justamente ela que está faltando).
func NewWagerTransactionPendingReferenceEvent(txn *WagerTransaction, correlationID string) (*OutboxEvent, error) {
	if txn.Status() != StatusPendingReference {
		return nil, fmt.Errorf("%w: %s exige transação PENDING_REFERENCE, veio %s",
			ErrInvalidOutboxEvent, EventTypeWagerTransactionPendingReference, txn.Status())
	}
	if txn.ReferenceExternalTxID() == "" {
		return nil, fmt.Errorf("%w: pendência de referência sem referenceExternalTransactionId", ErrInvalidOutboxEvent)
	}

	data := WagerTransactionPendingReferenceData{wagerTransactionCore: newWagerTransactionCore(txn)}
	return newOutboxEvent(EventTypeWagerTransactionPendingReference, txn.ID(), correlationID, uuid.Nil, data)
}

// NewWalletBalanceChangedEvent cria o evento de mudança de saldo a
// partir do LANÇAMENTO DO LEDGER — que já carrega direção, valor, saldo
// anterior e saldo posterior, validados pelo construtor do lançamento.
// Assim o evento e o ledger não têm como divergir.
//
// walletVersion é a versão da carteira DEPOIS da mudança.
// causationID é o eventId do evento que causou esta mudança (o
// WagerTransactionProcessed correspondente); uuid.Nil = sem causa.
// aggregateId é o id da carteira.
func NewWalletBalanceChangedEvent(
	entry *WalletLedgerEntry,
	walletVersion int64,
	correlationID string,
	causationID uuid.UUID,
) (*OutboxEvent, error) {
	if walletVersion < 1 {
		return nil, fmt.Errorf("%w: versão da carteira inválida (%d)", ErrInvalidOutboxEvent, walletVersion)
	}

	data := WalletBalanceChangedData{
		WalletID:      entry.WalletID().String(),
		TransactionID: entry.TransactionID().String(),
		Direction:     string(entry.Direction()),
		Money:         moneyPayload(entry.Amount()),
		BalanceBefore: moneyPayload(entry.BalanceBefore()),
		BalanceAfter:  moneyPayload(entry.BalanceAfter()),
		WalletVersion: walletVersion,
	}
	return newOutboxEvent(EventTypeWalletBalanceChanged, entry.WalletID(), correlationID, causationID, data)
}

// newOutboxEvent monta o envelope, serializa e devolve o evento
// imutável. É privado: os únicos jeitos de criar um OutboxEvent são os
// construtores acima, que fixam tipo e versão.
func newOutboxEvent(
	eventType string,
	aggregateID uuid.UUID,
	correlationID string,
	causationID uuid.UUID,
	data any,
) (*OutboxEvent, error) {
	if correlationID == "" {
		return nil, fmt.Errorf("%w: correlationId é obrigatório", ErrInvalidOutboxEvent)
	}
	if aggregateID == uuid.Nil {
		return nil, fmt.Errorf("%w: aggregateId é obrigatório", ErrInvalidOutboxEvent)
	}

	// Truncar para milissegundos garante que o valor guardado no banco
	// e o texto do payload representem exatamente o mesmo instante.
	occurredAt := time.Now().UTC().Truncate(time.Millisecond)
	id := uuid.New()

	envelope := outboxEnvelope{
		EventID:       id.String(),
		EventType:     eventType,
		AggregateID:   aggregateID.String(),
		CorrelationID: correlationID,
		OccurredAt:    occurredAt.Format(occurredAtLayout),
		Version:       eventVersion,
		Data:          data,
	}
	if causationID != uuid.Nil {
		envelope.CausationID = causationID.String()
	}

	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("serializando evento %s: %w", eventType, err)
	}

	return &OutboxEvent{
		id:            id,
		eventType:     eventType,
		aggregateID:   aggregateID,
		correlationID: correlationID,
		causationID:   causationID,
		occurredAt:    occurredAt,
		version:       eventVersion,
		payload:       payload,
	}, nil
}
