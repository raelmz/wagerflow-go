package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// --- Conceito Go 8: tipo customizado como "enum" ---
// Go não tem enum nativo. O padrão idiomático é criar um tipo novo
// baseado em string (ou int) e declarar constantes desse tipo.
// Isso dá segurança: uma função que espera "Kind" não aceita
// qualquer string solta, só os valores declarados abaixo.
type WagerKind string

const (
	KindOpening  WagerKind = "OPENING" // uso interno, nunca vem de fora
	KindBet      WagerKind = "BET"
	KindWin      WagerKind = "WIN"
	KindLoss     WagerKind = "LOSS"
	KindRefund   WagerKind = "REFUND"
	KindRollback WagerKind = "ROLLBACK"
)

type WagerStatus string

const (
	StatusPending          WagerStatus = "PENDING"
	StatusPendingReference WagerStatus = "PENDING_REFERENCE"
	StatusProcessed        WagerStatus = "PROCESSED" // terminal
	StatusRejected         WagerStatus = "REJECTED"  // terminal
	StatusFailed           WagerStatus = "FAILED"    // terminal
)

// isTerminal centraliza a regra "esses três estados não aceitam
// mais nenhuma transição" — usada em todo método de transição abaixo,
// pra não repetir a mesma checagem em cada um.
func (s WagerStatus) isTerminal() bool {
	return s == StatusProcessed || s == StatusRejected || s == StatusFailed
}

var (
	ErrTransactionAlreadyTerminal = errors.New("transação já está em estado terminal e não pode mudar")
	ErrInvalidWagerData           = errors.New("dados inválidos para este tipo de transação")
	ErrMissingReference           = errors.New("referenceExternalTransactionId é obrigatório para este tipo")
)

// WagerTransaction representa uma operação financeira (externa ou
// interna) sobre uma carteira. Guarda tanto os dados de entrada
// quanto o resultado do processamento.
type WagerTransaction struct {
	id                     uuid.UUID
	externalTransactionID  string // vazio para OPENING
	providerID             string // vazio para OPENING
	idempotencyKey         string // vazio para OPENING
	payloadHash            string
	walletID               uuid.UUID
	playerID               uuid.UUID
	roundID                string // vazio para OPENING
	gameID                 string // vazio para OPENING
	kind                   WagerKind
	money                  Money
	referenceExternalTxID  string // preenchido só em REFUND/ROLLBACK
	status                 WagerStatus
	failureCode            string
	createdAt              time.Time
	updatedAt              time.Time
}

// NewExternalWagerTransaction cria uma transação vinda de HTTP ou SQS
// (BET, WIN, LOSS, REFUND, ROLLBACK — nunca OPENING). Valida as regras
// de cada tipo antes de aceitar a criação, conforme a tabela da
// seção 7 do desafio.
func NewExternalWagerTransaction(
	externalTransactionID string,
	providerID string,
	idempotencyKey string,
	payloadHash string,
	walletID uuid.UUID,
	playerID uuid.UUID,
	roundID string,
	gameID string,
	kind WagerKind,
	money Money,
	referenceExternalTxID string,
) (*WagerTransaction, error) {
	if kind == KindOpening {
		// OPENING é reservado para uso interno — a seção 6.3 do
		// desafio exige rejeitar explicitamente se chegar por fora.
		return nil, ErrInvalidWagerData
	}
	if externalTransactionID == "" || providerID == "" || idempotencyKey == "" {
		return nil, ErrInvalidWagerData
	}

	// --- Conceito Go 9: switch sobre tipo customizado ---
	// Como WagerKind é baseado em string, dá pra usar switch normal.
	// Aqui validamos a regra específica de cada tipo, igual a tabela
	// da seção 7 do desafio descreve.
	switch kind {
	case KindBet, KindWin:
		if money.IsNegative() || money.IsZero() {
			return nil, ErrInvalidWagerData
		}
	case KindLoss:
		if !money.IsZero() {
			return nil, ErrInvalidWagerData
		}
	case KindRefund, KindRollback:
		if money.IsNegative() || money.IsZero() {
			return nil, ErrInvalidWagerData
		}
		if referenceExternalTxID == "" {
			return nil, ErrMissingReference
		}
	default:
		return nil, ErrInvalidWagerData
	}

	now := time.Now().UTC()
	return &WagerTransaction{
		id:                    uuid.New(),
		externalTransactionID: externalTransactionID,
		providerID:            providerID,
		idempotencyKey:        idempotencyKey,
		payloadHash:           payloadHash,
		walletID:              walletID,
		playerID:              playerID,
		roundID:               roundID,
		gameID:                gameID,
		kind:                  kind,
		money:                 money,
		referenceExternalTxID: referenceExternalTxID,
		status:                StatusPending,
		createdAt:             now,
		updatedAt:             now,
	}, nil
}

// NewOpeningTransaction cria a transação interna de abertura de
// carteira com saldo inicial positivo. Só o próprio sistema chama
// isso — nunca vem de HTTP/SQS diretamente (por isso não tem
// providerID, idempotencyKey, roundId, gameId — a seção 6.3 do
// desafio diz que esses campos "não se aplicam" aqui).
func NewOpeningTransaction(walletID uuid.UUID, playerID uuid.UUID, money Money) (*WagerTransaction, error) {
	if money.IsNegative() || money.IsZero() {
		return nil, ErrInvalidWagerData
	}
	now := time.Now().UTC()
	return &WagerTransaction{
		id:        uuid.New(),
		walletID:  walletID,
		playerID:  playerID,
		kind:      KindOpening,
		money:     money,
		status:    StatusPending,
		createdAt: now,
		updatedAt: now,
	}, nil
}

// --- Transições de estado ---
// Cada método abaixo é um "guard": só permite a mudança se o estado
// atual fizer sentido para aquela transição. Chamar um desses métodos
// numa transação já terminal (PROCESSED/REJECTED/FAILED) sempre falha
// com ErrTransactionAlreadyTerminal — essa é a regra que impede um
// replay de reprocessar algo que já tem resultado definitivo.

// MarkProcessed finaliza a transação como concluída com sucesso.
func (t *WagerTransaction) MarkProcessed() error {
	if t.status.isTerminal() {
		return ErrTransactionAlreadyTerminal
	}
	t.status = StatusProcessed
	t.updatedAt = time.Now().UTC()
	return nil
}

// MarkPendingReference indica que a transação depende de uma
// referência (REFUND/ROLLBACK) que ainda não chegou.
func (t *WagerTransaction) MarkPendingReference() error {
	if t.status.isTerminal() {
		return ErrTransactionAlreadyTerminal
	}
	t.status = StatusPendingReference
	t.updatedAt = time.Now().UTC()
	return nil
}

// MarkRejected finaliza a transação como recusada por regra de
// negócio (ex: saldo insuficiente, referência não encontrada).
// failureCode é obrigatório e estável, conforme a seção 7 do desafio.
func (t *WagerTransaction) MarkRejected(failureCode string) error {
	if t.status.isTerminal() {
		return ErrTransactionAlreadyTerminal
	}
	if failureCode == "" {
		return ErrInvalidWagerData
	}
	t.status = StatusRejected
	t.failureCode = failureCode
	t.updatedAt = time.Now().UTC()
	return nil
}

// MarkFailed finaliza a transação como falha permanente de
// infraestrutura (para auditoria), distinta de uma rejeição de negócio.
func (t *WagerTransaction) MarkFailed(failureCode string) error {
	if t.status.isTerminal() {
		return ErrTransactionAlreadyTerminal
	}
	if failureCode == "" {
		return ErrInvalidWagerData
	}
	t.status = StatusFailed
	t.failureCode = failureCode
	t.updatedAt = time.Now().UTC()
	return nil
}

// RehydrateWagerTransaction reconstrói uma transação a partir de
// dados já persistidos no banco. Não reaplica nenhuma regra de
// negócio nem dispara nenhuma transição — só monta a struct com o
// estado que já existia, exatamente como RehydrateWallet faz para
// a carteira.
func RehydrateWagerTransaction(
	id uuid.UUID,
	externalTransactionID string,
	providerID string,
	idempotencyKey string,
	payloadHash string,
	walletID uuid.UUID,
	playerID uuid.UUID,
	roundID string,
	gameID string,
	kind WagerKind,
	money Money,
	referenceExternalTxID string,
	status WagerStatus,
	failureCode string,
	createdAt time.Time,
	updatedAt time.Time,
) *WagerTransaction {
	return &WagerTransaction{
		id:                    id,
		externalTransactionID: externalTransactionID,
		providerID:            providerID,
		idempotencyKey:        idempotencyKey,
		payloadHash:           payloadHash,
		walletID:              walletID,
		playerID:              playerID,
		roundID:               roundID,
		gameID:                gameID,
		kind:                  kind,
		money:                 money,
		referenceExternalTxID: referenceExternalTxID,
		status:                status,
		failureCode:           failureCode,
		createdAt:             createdAt,
		updatedAt:             updatedAt,
	}
}

// --- Getters ---

func (t *WagerTransaction) ID() uuid.UUID                    { return t.id }
func (t *WagerTransaction) ExternalTransactionID() string    { return t.externalTransactionID }
func (t *WagerTransaction) ProviderID() string                { return t.providerID }
func (t *WagerTransaction) IdempotencyKey() string             { return t.idempotencyKey }
func (t *WagerTransaction) PayloadHash() string                { return t.payloadHash }
func (t *WagerTransaction) WalletID() uuid.UUID                { return t.walletID }
func (t *WagerTransaction) PlayerID() uuid.UUID                { return t.playerID }
func (t *WagerTransaction) RoundID() string                    { return t.roundID }
func (t *WagerTransaction) GameID() string                     { return t.gameID }
func (t *WagerTransaction) Kind() WagerKind                    { return t.kind }
func (t *WagerTransaction) Money() Money                       { return t.money }
func (t *WagerTransaction) ReferenceExternalTxID() string      { return t.referenceExternalTxID }
func (t *WagerTransaction) Status() WagerStatus                { return t.status }
func (t *WagerTransaction) FailureCode() string                { return t.failureCode }
func (t *WagerTransaction) CreatedAt() time.Time                { return t.createdAt }
func (t *WagerTransaction) UpdatedAt() time.Time                { return t.updatedAt }
