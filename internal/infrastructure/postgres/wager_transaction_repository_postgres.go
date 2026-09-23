package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

type WagerTransactionRepository struct {
	db DBTX
}

func NewWagerTransactionRepository(db DBTX) *WagerTransactionRepository {
	return &WagerTransactionRepository{db: db}
}

// nullableText converte string vazia em nil, para gravar NULL no
// banco em vez da string vazia — importante porque as constraints
// das migrations (chk_wager_opening_shape, os índices únicos com
// WHERE provider_id IS NOT NULL) dependem de NULL de verdade, não
// de string vazia.
func nullableText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nullableUUID converte uuid.Nil em NULL. Devolve `any` (e não
// *uuid.UUID) porque um nil "puro" é o jeito mais simples de o pgx
// enviar NULL; um valor não-nulo segue como uuid.UUID normal.
func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

// Nomes dos índices únicos de IDEMPOTÊNCIA (migration 000002). Só uma
// violação DESTES índices significa "outra requisição igual venceu" —
// outros índices únicos (ex: crédito inicial duplicado) são erros
// diferentes e não devem ser confundidos com replay.
const (
	uniqueIdempotencyKeyIndex = "uq_wager_provider_idempotency_key"
	uniqueExternalTxIndex     = "uq_wager_provider_external_tx"

	// Código SQLSTATE do Postgres para unique_violation.
	pgUniqueViolation = "23505"
)

// translateDuplicate converte a violação dos índices de idempotência
// em domain.ErrDuplicateTransaction. Para qualquer outro erro devolve
// nil, e quem chamou segue com o tratamento normal.
func translateDuplicate(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgUniqueViolation {
		return nil
	}
	if pgErr.ConstraintName == uniqueIdempotencyKeyIndex || pgErr.ConstraintName == uniqueExternalTxIndex {
		return fmt.Errorf("%w (%s)", domain.ErrDuplicateTransaction, pgErr.ConstraintName)
	}
	return nil
}

// nullableCents converte o saldo resultante opcional em *int64 (NULL
// quando a transação ainda não concluiu).
func nullableCents(m domain.Money, ok bool) *int64 {
	if !ok {
		return nil
	}
	c := m.Cents()
	return &c
}

func textOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (r *WagerTransactionRepository) Create(ctx context.Context, tx *domain.WagerTransaction) error {
	resulting, hasResulting := tx.ResultingBalance()

	_, err := r.db.Exec(ctx, `
		INSERT INTO wager_transactions (
			id, external_transaction_id, provider_id, idempotency_key, payload_hash,
			wallet_id, player_id, round_id, game_id, kind,
			amount_cents, currency, reference_external_tx_id, reference_transaction_id,
			status, failure_code, resulting_balance_cents,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	`,
		tx.ID(), nullableText(tx.ExternalTransactionID()), nullableText(tx.ProviderID()),
		nullableText(tx.IdempotencyKey()), tx.PayloadHash(),
		tx.WalletID(), tx.PlayerID(), nullableText(tx.RoundID()), nullableText(tx.GameID()), string(tx.Kind()),
		tx.Money().Cents(), tx.Money().Currency(), nullableText(tx.ReferenceExternalTxID()), nullableUUID(tx.ResolvedReferenceID()),
		string(tx.Status()), nullableText(tx.FailureCode()), nullableCents(resulting, hasResulting),
		tx.CreatedAt(), tx.UpdatedAt(),
	)
	if err != nil {
		// Se outra requisição igual venceu a corrida, o índice único
		// recusa este INSERT. Sinalizamos com um erro do DOMÍNIO para
		// o caso de uso tratar como replay.
		if dup := translateDuplicate(err); dup != nil {
			return dup
		}
		return fmt.Errorf("falha ao inserir wager transaction: %w", err)
	}
	return nil
}

func (r *WagerTransactionRepository) Update(ctx context.Context, tx *domain.WagerTransaction) error {
	resulting, hasResulting := tx.ResultingBalance()

	_, err := r.db.Exec(ctx, `
		UPDATE wager_transactions
		SET status = $1, failure_code = $2, reference_transaction_id = $3,
		    resulting_balance_cents = $4, updated_at = $5
		WHERE id = $6
	`, string(tx.Status()), nullableText(tx.FailureCode()), nullableUUID(tx.ResolvedReferenceID()),
		nullableCents(resulting, hasResulting), tx.UpdatedAt(), tx.ID())
	if err != nil {
		return fmt.Errorf("falha ao atualizar wager transaction: %w", err)
	}
	return nil
}

func (r *WagerTransactionRepository) FindByProviderAndIdempotencyKey(ctx context.Context, providerID, idempotencyKey string) (*domain.WagerTransaction, error) {
	row := r.db.QueryRow(ctx, selectWagerTransactionSQL+` WHERE provider_id = $1 AND idempotency_key = $2`, providerID, idempotencyKey)
	return scanWagerTransaction(row)
}

func (r *WagerTransactionRepository) FindByProviderAndExternalTxID(ctx context.Context, providerID, externalTransactionID string) (*domain.WagerTransaction, error) {
	row := r.db.QueryRow(ctx, selectWagerTransactionSQL+` WHERE provider_id = $1 AND external_transaction_id = $2`, providerID, externalTransactionID)
	return scanWagerTransaction(row)
}

// LockByProviderAndExternalTxID faz a mesma busca, mas com FOR UPDATE:
// a linha fica travada até o fim da transação atual. Outra transação
// que tente travar a MESMA linha espera aqui — é o que serializa
// reversões concorrentes da mesma aposta. Linhas de outras
// transações/carteiras não são afetadas (nada de lock global).
func (r *WagerTransactionRepository) LockByProviderAndExternalTxID(ctx context.Context, providerID, externalTransactionID string) (*domain.WagerTransaction, error) {
	row := r.db.QueryRow(ctx, selectWagerTransactionSQL+` WHERE provider_id = $1 AND external_transaction_id = $2 FOR UPDATE`, providerID, externalTransactionID)
	return scanWagerTransaction(row)
}

// HasProcessedReversalOf diz se já existe um REFUND/ROLLBACK PROCESSED
// apontando para referenceID. A migration 000005 tem um índice único
// parcial exatamente sobre estas condições — este SELECT é a checagem
// "amigável" e o índice é a garantia final.
func (r *WagerTransactionRepository) HasProcessedReversalOf(ctx context.Context, referenceID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM wager_transactions
			WHERE reference_transaction_id = $1
			  AND kind IN ('REFUND', 'ROLLBACK')
			  AND status = 'PROCESSED'
		)
	`, referenceID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("falha ao checar reversão existente: %w", err)
	}
	return exists, nil
}

const selectWagerTransactionSQL = `
	SELECT id, external_transaction_id, provider_id, idempotency_key, payload_hash,
	       wallet_id, player_id, round_id, game_id, kind,
	       amount_cents, currency, reference_external_tx_id, reference_transaction_id::text,
	       status, failure_code, resulting_balance_cents,
	       created_at, updated_at
	FROM wager_transactions
`

func scanWagerTransaction(row pgx.Row) (*domain.WagerTransaction, error) {
	var (
		id, walletID, playerID                                    uuid.UUID
		externalTxID, providerID, idempotencyKey, roundID, gameID *string
		payloadHash, kind, currency, status                       string
		amountCents                                               int64
		referenceExternalTxID, referenceTxIDText, failureCode     *string
		resultingBalanceCents                                     *int64
		createdAt, updatedAt                                      time.Time
	)

	err := row.Scan(
		&id, &externalTxID, &providerID, &idempotencyKey, &payloadHash,
		&walletID, &playerID, &roundID, &gameID, &kind,
		&amountCents, &currency, &referenceExternalTxID, &referenceTxIDText,
		&status, &failureCode, &resultingBalanceCents,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("falha ao ler wager transaction: %w", err)
	}

	// A referência interna vem como texto (::text no SELECT); NULL vira uuid.Nil.
	resolvedRef := uuid.Nil
	if referenceTxIDText != nil {
		parsed, err := uuid.Parse(*referenceTxIDText)
		if err != nil {
			return nil, fmt.Errorf("reference_transaction_id inválido no banco: %w", err)
		}
		resolvedRef = parsed
	}

	// Saldo resultante opcional: NULL vira nil.
	var resultingBalance *domain.Money
	if resultingBalanceCents != nil {
		m := domain.MoneyFromCents(*resultingBalanceCents, currency)
		resultingBalance = &m
	}

	money := domain.MoneyFromCents(amountCents, currency)
	return domain.RehydrateWagerTransaction(
		id, textOrEmpty(externalTxID), textOrEmpty(providerID), textOrEmpty(idempotencyKey),
		payloadHash, walletID, playerID, textOrEmpty(roundID), textOrEmpty(gameID),
		domain.WagerKind(kind), money, textOrEmpty(referenceExternalTxID), resolvedRef,
		domain.WagerStatus(status), textOrEmpty(failureCode), resultingBalance,
		createdAt, updatedAt,
	), nil
}
