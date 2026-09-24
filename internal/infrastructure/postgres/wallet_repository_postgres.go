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

// Nome da constraint única (playerId, currency) — migration 000001.
const uniqueWalletPlayerCurrencyConstraint = "uq_wallets_player_currency"

// WalletRepository é a implementação real (Postgres) da interface
// domain.WalletRepository. Repare que o campo agora é DBTX, não mais
// *pgxpool.Pool — isso permite criar um WalletRepository "por cima"
// tanto do pool quanto de uma transação em andamento (ver tx_manager.go).
type WalletRepository struct {
	db DBTX
}

// NewWalletRepository cria o repositório sobre qualquer DBTX — pode
// ser o pool (fora de transação) ou um pgx.Tx (dentro de uma).
func NewWalletRepository(db DBTX) *WalletRepository {
	return &WalletRepository{db: db}
}

func (r *WalletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO wallets (id, player_id, currency, balance_cents, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		wallet.ID(), wallet.PlayerID(), wallet.Currency(),
		wallet.Balance().Cents(), wallet.Version(),
		wallet.CreatedAt(), wallet.UpdatedAt(),
	)
	if err != nil {
		// Violação da constraint (playerId, currency): já existe
		// carteira para este par. A seção 9 do desafio exige que essa
		// tentativa vire conflito, não um erro genérico de banco.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == uniqueWalletPlayerCurrencyConstraint {
			return domain.ErrWalletAlreadyExists
		}
		return fmt.Errorf("falha ao inserir carteira: %w", err)
	}
	return nil
}

func (r *WalletRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, player_id, currency, balance_cents, version, created_at, updated_at
		FROM wallets WHERE id = $1
	`, id)
	return scanWallet(row)
}

func (r *WalletRepository) FindByPlayerAndCurrency(ctx context.Context, playerID uuid.UUID, currency string) (*domain.Wallet, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, player_id, currency, balance_cents, version, created_at, updated_at
		FROM wallets WHERE player_id = $1 AND currency = $2
	`, playerID, currency)
	return scanWallet(row)
}

// Debit é o UPDATE ATÔMICO CONDICIONADO combinado em docs/PROJETO.md.
// A condição "balance_cents >= $1" está no próprio WHERE: o Postgres
// só aplica o UPDATE se isso for verdade NA LINHA ATUAL, no exato
// instante da operação — mesmo com duas instâncias tentando ao mesmo
// tempo, o banco serializa e só uma vence. Quando chamado DENTRO de
// uma transação (via TxManager), essa garantia vale para a transação
// inteira: ninguém mais enxerga essa linha até o commit.
func (r *WalletRepository) Debit(ctx context.Context, walletID uuid.UUID, amount domain.Money) (*domain.Wallet, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE wallets
		SET balance_cents = balance_cents - $1,
		    version = version + 1,
		    updated_at = now()
		WHERE id = $2 AND balance_cents >= $1
		RETURNING id, player_id, currency, balance_cents, version, created_at, updated_at
	`, amount.Cents(), walletID)

	wallet, err := scanWallet(row)
	if err != nil {
		return nil, err
	}
	if wallet == nil {
		return nil, domain.ErrInsufficientBalance
	}
	return wallet, nil
}

// Credit é o equivalente para crédito.
func (r *WalletRepository) Credit(ctx context.Context, walletID uuid.UUID, amount domain.Money) (*domain.Wallet, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE wallets
		SET balance_cents = balance_cents + $1,
		    version = version + 1,
		    updated_at = now()
		WHERE id = $2
		RETURNING id, player_id, currency, balance_cents, version, created_at, updated_at
	`, amount.Cents(), walletID)

	wallet, err := scanWallet(row)
	if err != nil {
		return nil, err
	}
	if wallet == nil {
		return nil, fmt.Errorf("carteira %s não encontrada", walletID)
	}
	return wallet, nil
}

func scanWallet(row pgx.Row) (*domain.Wallet, error) {
	var (
		id, playerID          uuid.UUID
		currency              string
		balanceCents, version int64
		createdAt, updatedAt  time.Time
	)

	err := row.Scan(&id, &playerID, &currency, &balanceCents, &version, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("falha ao ler carteira: %w", err)
	}

	balance := domain.MoneyFromCents(balanceCents, currency)
	return domain.RehydrateWallet(id, playerID, currency, balance, version, createdAt, updatedAt), nil
}
