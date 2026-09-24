// cmd/api é o ponto de entrada da API HTTP. É o ÚNICO lugar do
// projeto que conhece Uber Fx — a composição (quem depende de quem)
// fica toda aqui, fora de domain/application/infrastructure.
package main

import (
	"context"
	stdhttp "net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"go.uber.org/fx"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/config"
	"github.com/raelmz/wagerflow-go/internal/domain"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/postgres"
	wfhttp "github.com/raelmz/wagerflow-go/internal/interfaces/http"
)

func main() {
	// Carregado ANTES do fx.New: se não existir .env (ex: produção,
	// onde as variáveis vêm do ambiente de verdade), godotenv.Load
	// simplesmente falha em silêncio — config.Load() é quem realmente
	// valida o que é obrigatório.
	_ = godotenv.Load()

	fx.New(
		fx.Provide(
			config.Load,
			newPool,
			newTxRunner,
			newWalletRepository,
			newWagerTransactionRepository,
			newWalletLedgerEntryRepository,
			newTokenVerifier,

			application.NewOpenWalletUseCase,
			application.NewGetWalletUseCase,
			application.NewGetWalletLedgerUseCase,
			application.NewReconciliationUseCase,
			application.NewProcessWagerTransactionUseCase,
			application.NewGetWagerTransactionUseCase,

			newHealthHandler,
			wfhttp.NewWalletHandler,
			wfhttp.NewWagerHandler,
			wfhttp.NewRouter,
		),
		fx.Invoke(registerHTTPServer),
	).Run()
}

// newPool cria o pool do Postgres e registra no ciclo de vida do Fx
// o fechamento dele quando a aplicação for encerrada (fx.Lifecycle é
// como Fx sabe "isso aqui precisa ser desligado no shutdown", sem
// precisar de um defer solto em main()).
func newPool(lc fx.Lifecycle, cfg *config.Config) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			pool.Close()
			return nil
		},
	})

	return pool, nil
}

// newTokenVerifier monta o verificador OIDC (Keycloak) a partir da
// issuer URL configurada. Faz discovery na hora do boot (busca
// jwks_uri em /.well-known/openid-configuration) — por isso o
// timeout: se o Keycloak não estiver de pé ainda, a aplicação falha
// rápido com um erro claro em vez de subir "quebrada" e só falhar na
// primeira requisição autenticada.
func newTokenVerifier(cfg *config.Config) (wfhttp.TokenVerifier, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return wfhttp.NewOIDCVerifier(ctx, cfg.KeycloakIssuerURL)
}

// As funções abaixo só existem para "converter" o tipo concreto
// (implementação Postgres) na INTERFACE de domínio correspondente.
// É a mesma regra de sempre: application/ recebe interfaces, nunca
// sabe que por trás tem pgx — aqui, na composição, é o único lugar
// que conecta as duas pontas.
func newTxRunner(pool *pgxpool.Pool) domain.TxRunner {
	return postgres.NewTxManager(pool)
}

func newWalletRepository(pool *pgxpool.Pool) domain.WalletRepository {
	return postgres.NewWalletRepository(pool)
}

func newWagerTransactionRepository(pool *pgxpool.Pool) domain.WagerTransactionRepository {
	return postgres.NewWagerTransactionRepository(pool)
}

func newWalletLedgerEntryRepository(pool *pgxpool.Pool) domain.WalletLedgerEntryRepository {
	return postgres.NewWalletLedgerEntryRepository(pool)
}

func newHealthHandler(pool *pgxpool.Pool) *wfhttp.HealthHandler {
	return wfhttp.NewHealthHandler(pool)
}

// registerHTTPServer sobe o servidor HTTP num goroutine (OnStart não
// pode bloquear) e o desliga graciosamente no OnStop, dando 10s para
// requisições em andamento terminarem antes de matar as conexões.
func registerHTTPServer(lc fx.Lifecycle, cfg *config.Config, router *chi.Mux) {
	server := &stdhttp.Server{
		Addr:    ":" + cfg.HTTPPort,
		Handler: router,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				if err := server.ListenAndServe(); err != nil && err != stdhttp.ErrServerClosed {
					panic("falha ao subir servidor HTTP: " + err.Error())
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return server.Shutdown(shutdownCtx)
		},
	})
}
