// Pacote bootstrap existe só para que a composição do Fx da API
// (quem depende de quem) possa ser testada de fora: um pacote
// "main" (como cmd/api) não pode ser importado por nenhum outro
// pacote em Go — nem por um teste. Antes desta mudança, a
// verificação exigida pela seção 13 do desafio ("verificação da
// composição Fx e de seu início e encerramento") era impossível de
// escrever, porque não existia nenhum jeito de pegar essa composição
// fora de cmd/api/main.go.
//
// A solução é o padrão comum em projetos Go com Fx: a lista de
// fx.Provide/fx.Invoke mora aqui, exportada como Module, e
// cmd/api/main.go passa a ser só "fx.New(bootstrap.Module).Run()".
// O comportamento em produção não muda em nada — é só uma mudança de
// ONDE o código mora, não do que ele faz.
package bootstrap

import (
	"context"
	"log/slog"
	stdhttp "net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/config"
	"github.com/raelmz/wagerflow-go/internal/domain"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/messaging"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/postgres"
	wfhttp "github.com/raelmz/wagerflow-go/internal/interfaces/http"
	"github.com/raelmz/wagerflow-go/internal/observability"
)

// Module é a composição inteira da API HTTP: todo provider e o
// fx.Invoke que sobe o servidor. cmd/api/main.go usa isto direto;
// os testes de composição (test/integration) também usam isto
// direto, com fx.Populate para inspecionar o que foi montado antes
// e depois do Stop.
var Module = fx.Options(
	fx.Provide(
		newLogger,
		config.Load,
		newPool,
		newTxRunner,
		newWalletRepository,
		newWagerTransactionRepository,
		newWalletLedgerEntryRepository,
		newTokenVerifier,
		newDBPinger,
		newSQSPinger,
		newKeycloakPinger,

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
)

// newLogger monta o logger estruturado em JSON usado por toda a API
// (seção 12 do desafio). Fica disponível para qualquer componente que
// o Fx monta depois — hoje, o router (log de acesso HTTP); no futuro,
// qualquer handler ou caso de uso que precise logar algo pode receber
// *slog.Logger no construtor do mesmo jeito.
func newLogger() *slog.Logger {
	return observability.NewLogger("api")
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

func newHealthHandler(db wfhttp.DBPinger, sqs wfhttp.SQSPinger, keycloak wfhttp.KeycloakPinger) *wfhttp.HealthHandler {
	return wfhttp.NewHealthHandler(db, sqs, keycloak)
}

// newDBPinger devolve o próprio pool do Postgres como implementação
// de wfhttp.DBPinger — *pgxpool.Pool já tem um método Ping(ctx)
// error, então não precisa de nenhum wrapper. Declarar o RETORNO
// desta função como a interface wfhttp.DBPinger (em vez do tipo
// concreto *pgxpool.Pool) é o que permite o Fx diferenciar este
// provider dos outros dois pingers abaixo — os três têm o mesmo
// formato (só Ping(ctx) error), mas tipos Go diferentes.
func newDBPinger(pool *pgxpool.Pool) wfhttp.DBPinger {
	return pool
}

// newSQSPinger monta o pinger de conectividade com o SQS/LocalStack
// para o /health/ready. Não faz chamada de rede aqui — só na hora do
// Ping — então não atrasa (nem depende de) o boot da API.
func newSQSPinger(cfg *config.Config) wfhttp.SQSPinger {
	return messaging.NewSQSPinger(cfg.SQSEndpointURL, cfg.AWSRegion)
}

// newKeycloakPinger monta o pinger de conectividade com o Keycloak
// para o /health/ready, a partir da MESMA issuer URL que
// newTokenVerifier já usa para o discovery OIDC.
func newKeycloakPinger(cfg *config.Config) wfhttp.KeycloakPinger {
	return wfhttp.NewKeycloakPinger(cfg.KeycloakIssuerURL)
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
