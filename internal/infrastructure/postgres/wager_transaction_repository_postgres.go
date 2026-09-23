package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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

func textOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (r *WagerTransactionRepository) Create(ctx context.Context, tx *domain.WagerTransaction) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO wager_transactions (
			id, external_transaction_id, provider_id, idempotency_key, payload_hash,
			wallet_id, player_id, round_id, game_id, kind,
			amount_cents, currency, reference_external_tx_id, status, failure_code,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`,
		tx.ID(), nullableText(tx.ExternalTransactionID()), nullableText(tx.ProviderID()),
		nullableText(tx.IdempotencyKey()), tx.PayloadHash(),
		tx.WalletID(), tx.PlayerID(), nullableText(tx.RoundID()), nullableText(tx.GameID()), string(tx.Kind()),
		tx.Money().Cents(), tx.Money().Currency(), nullableText(tx.ReferenceExternalTxID()),
		string(tx.Status()), nullableText(tx.FailureCode()),
		tx.CreatedAt(), tx.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("falha ao inserir wager transaction: %w", err)
	}
	return nil
}

func (r *WagerTransactionRepository) Update(ctx context.Context, tx *domain.WagerTransaction) error {
	_, err := r.db.Exec(ctx, `
		UPDATE wager_transactions
		SET status = $1, failure_code = $2, updated_at = $3
		WHERE id = $4
	`, string(tx.Status()), nullableText(tx.FailureCode()), tx.UpdatedAt(), tx.ID())
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

const selectWagerTransactionSQL = `
	SELECT id, external_transaction_id, provider_id, idempotency_key, payload_hash,
	       wallet_id, player_id, round_id, game_id, kind,
	       amount_cents, currency, reference_external_tx_id, status, failure_code,
	       created_at, updated_at
	FROM wager_transactions
`

func scanWagerTransaction(row pgx.Row) (*domain.WagerTransaction, error) {
	var (
		id, walletID, playerID                                    uuid.UUID
		externalTxID, providerID, idempotencyKey, roundID, gameID *string
		payloadHash, kind, currency, status                       string
		amountCents                                               int64
		referenceExternalTxID, failureCode                        *string
		createdAt, updatedAt                                      time.Time
	)

	err := row.Scan(
		&id, &externalTxID, &providerID, &idempotencyKey, &payloadHash,
		&walletID, &playerID, &roundID, &gameID, &kind,
		&amountCents, &currency, &referenceExternalTxID, &status, &failureCode,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("falha ao ler wager transaction: %w", err)
	}

	money := domain.MoneyFromCents(amountCents, currency)
	return domain.RehydrateWagerTransaction(
		id, textOrEmpty(externalTxID), textOrEmpty(providerID), textOrEmpty(idempotencyKey),
		payloadHash, walletID, playerID, textOrEmpty(roundID), textOrEmpty(gameID),
		domain.WagerKind(kind), money, textOrEmpty(referenceExternalTxID),
		domain.WagerStatus(status), textOrEmpty(failureCode), createdAt, updatedAt,
	), nil
}
