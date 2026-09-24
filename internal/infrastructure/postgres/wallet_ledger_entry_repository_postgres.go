package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

type WalletLedgerEntryRepository struct {
	db DBTX
}

func NewWalletLedgerEntryRepository(db DBTX) *WalletLedgerEntryRepository {
	return &WalletLedgerEntryRepository{db: db}
}

func (r *WalletLedgerEntryRepository) Create(ctx context.Context, entry *domain.WalletLedgerEntry) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO wallet_ledger_entries (
			id, wallet_id, transaction_id, direction,
			amount_cents, balance_before_cents, balance_after_cents, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		entry.ID(), entry.WalletID(), entry.TransactionID(), string(entry.Direction()),
		entry.Amount().Cents(), entry.BalanceBefore().Cents(), entry.BalanceAfter().Cents(),
		entry.CreatedAt(),
	)
	if err != nil {
		return fmt.Errorf("falha ao inserir ledger entry: %w", err)
	}
	return nil
}

// --- Cursor opaco ---
// Codifica o ponto de parada da última página como "createdAt|id" em
// base64. Opaco de propósito: o cliente só guarda e devolve a string,
// nunca interpreta o conteúdo — assim podemos trocar o formato depois
// sem quebrar quem consome a API.
func encodeLedgerCursor(createdAt time.Time, id uuid.UUID) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

func decodeLedgerCursor(cursor string) (time.Time, uuid.UUID, error) {
	raw, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("cursor inválido: %w", err)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, errors.New("cursor inválido: formato inesperado")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("cursor inválido: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("cursor inválido: %w", err)
	}
	return createdAt, id, nil
}

// ListByWallet pagina o ledger em ordem estável (createdAt, id
// crescentes). Pede limit+1 linhas: se vier a linha extra, existe
// próxima página, e ela vira a base do próximo cursor (sem precisar
// de um segundo SELECT de contagem).
func (r *WalletLedgerEntryRepository) ListByWallet(ctx context.Context, walletID uuid.UUID, cursor string, limit int) ([]*domain.WalletLedgerEntry, string, error) {
	if limit <= 0 {
		limit = 50
	}

	var (
		rows pgx.Rows
		err  error
	)
	if cursor == "" {
		rows, err = r.db.Query(ctx, `
			SELECT id, wallet_id, transaction_id, direction,
			       amount_cents, balance_before_cents, balance_after_cents, created_at
			FROM wallet_ledger_entries
			WHERE wallet_id = $1
			ORDER BY created_at ASC, id ASC
			LIMIT $2
		`, walletID, limit+1)
	} else {
		cursorTime, cursorID, decodeErr := decodeLedgerCursor(cursor)
		if decodeErr != nil {
			return nil, "", decodeErr
		}
		rows, err = r.db.Query(ctx, `
			SELECT id, wallet_id, transaction_id, direction,
			       amount_cents, balance_before_cents, balance_after_cents, created_at
			FROM wallet_ledger_entries
			WHERE wallet_id = $1 AND (created_at, id) > ($2, $3)
			ORDER BY created_at ASC, id ASC
			LIMIT $4
		`, walletID, cursorTime, cursorID, limit+1)
	}
	if err != nil {
		return nil, "", fmt.Errorf("falha ao listar ledger: %w", err)
	}
	defer rows.Close()

	entries := make([]*domain.WalletLedgerEntry, 0, limit)
	for rows.Next() {
		var (
			id, walletIDCol, transactionID                     uuid.UUID
			direction                                          string
			amountCents, balanceBeforeCents, balanceAfterCents int64
			createdAt                                          time.Time
		)
		if err := rows.Scan(&id, &walletIDCol, &transactionID, &direction,
			&amountCents, &balanceBeforeCents, &balanceAfterCents, &createdAt); err != nil {
			return nil, "", fmt.Errorf("falha ao ler ledger entry: %w", err)
		}
		// A tabela wallet_ledger_entries não guarda moeda (a moeda é
		// sempre a da carteira dona do lançamento, já validada na
		// criação). Aqui reidratamos com currency vazia; quem monta a
		// resposta HTTP preenche com wallet.Currency() antes de serializar.
		entries = append(entries, domain.RehydrateWalletLedgerEntry(
			id, walletIDCol, transactionID, domain.LedgerDirection(direction),
			domain.MoneyFromCents(amountCents, ""), domain.MoneyFromCents(balanceBeforeCents, ""),
			domain.MoneyFromCents(balanceAfterCents, ""), createdAt,
		))
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("falha ao ler ledger: %w", err)
	}

	nextCursor := ""
	if len(entries) > limit {
		last := entries[limit]
		entries = entries[:limit]
		nextCursor = encodeLedgerCursor(last.CreatedAt(), last.ID())
	}
	return entries, nextCursor, nil
}

// SumByWallet reconstrói o saldo a partir do ledger inteiro: soma
// créditos, subtrai débitos. Usado pela reconciliação.
func (r *WalletLedgerEntryRepository) SumByWallet(ctx context.Context, walletID uuid.UUID) (int64, int, error) {
	var (
		netCents int64
		count    int
	)
	err := r.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount_cents ELSE -amount_cents END), 0),
			COUNT(*)
		FROM wallet_ledger_entries
		WHERE wallet_id = $1
	`, walletID).Scan(&netCents, &count)
	if err != nil {
		return 0, 0, fmt.Errorf("falha ao somar ledger: %w", err)
	}
	return netCents, count, nil
}
