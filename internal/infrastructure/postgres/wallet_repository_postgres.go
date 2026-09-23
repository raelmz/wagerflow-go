package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// WalletRepository é a implementação real (Postgres) da interface
// domain.WalletRepository. O nome do tipo pode repetir o da interface
// porque estão em pacotes diferentes (domain.WalletRepository vs
// postgres.WalletRepository) — em Go isso não gera conflito.
type WalletRepository struct {
	pool *pgxpool.Pool
}

// NewWalletRepository cria o repositório. Recebe o pool já pronto
// (criado em cmd/api, onde a aplicação for montada) — o repositório
// em si não decide como conectar, só usa a conexão recebida.
func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{pool: pool}
}

func (r *WalletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO wallets (id, player_id, currency, balance_cents, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		wallet.ID(), wallet.PlayerID(), wallet.Currency(),
		wallet.Balance().Cents(), wallet.Version(),
		wallet.CreatedAt(), wallet.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("falha ao inserir carteira: %w", err)
	}
	return nil
}

func (r *WalletRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, player_id, currency, balance_cents, version, created_at, updated_at
		FROM wallets WHERE id = $1
	`, id)
	return scanWallet(row)
}

func (r *WalletRepository) FindByPlayerAndCurrency(ctx context.Context, playerID uuid.UUID, currency string) (*domain.Wallet, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, player_id, currency, balance_cents, version, created_at, updated_at
		FROM wallets WHERE player_id = $1 AND currency = $2
	`, playerID, currency)
	return scanWallet(row)
}

// Debit é o UPDATE ATÔMICO CONDICIONADO combinado em docs/PROJETO.md.
// Repare que a condição "balance_cents >= $1" está no próprio WHERE:
// o Postgres só aplica o UPDATE se essa condição for verdadeira NA
// LINHA ATUAL do banco, no exato instante da operação — mesmo que
// duas goroutines/instâncias tentem isso ao mesmo tempo, o banco
// serializa as duas tentativas e só uma vê o saldo "antes" da outra.
// Não precisamos de lock explícito nem de retry: se a condição falhar,
// simplesmente 0 linhas são afetadas.
func (r *WalletRepository) Debit(ctx context.Context, walletID uuid.UUID, amount domain.Money) (*domain.Wallet, error) {
	row := r.pool.QueryRow(ctx, `
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
		// 0 linhas afetadas: ou a carteira não existe, ou o saldo
		// era insuficiente. Para o escopo do desafio, tratamos ambos
		// como "operação rejeitada" — quem chama decide o failureCode
		// exato (o caso "não existe" é raro, pois a wallet é
		// resolvida antes, na camada de aplicação).
		return nil, domain.ErrInsufficientBalance
	}
	return wallet, nil
}

// Credit é o equivalente para crédito. Não tem condição de saldo
// (crédito nunca deixa a carteira negativa), mas ainda é atômico:
// leitura e escrita acontecem na mesma instrução SQL.
func (r *WalletRepository) Credit(ctx context.Context, walletID uuid.UUID, amount domain.Money) (*domain.Wallet, error) {
	row := r.pool.QueryRow(ctx, `
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

// scanWallet lê uma linha do banco e reconstrói um *domain.Wallet
// usando RehydrateWallet (não NewWallet — estamos reconstruindo uma
// carteira que já existe, não criando uma nova). Devolve (nil, nil)
// quando a linha não existe (pgx.ErrNoRows), que é o contrato
// documentado na interface — "não encontrado" é um resultado válido,
// não um erro de infraestrutura.
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
