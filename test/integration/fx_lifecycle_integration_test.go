//go:build integration

// A seção 13 do desafio pede explicitamente: "Adicione uma
// verificação da composição Fx e de seu início e encerramento,
// incluindo liberação de recursos dos workers." Este arquivo é essa
// verificação.
//
// Ele NÃO testa lógica de negócio (isso já é coberto pelos outros
// testes de integração e pelos testes de handler HTTP) — testa a
// COMPOSIÇÃO em si: será que fx.New(bootstrap.Module) resolve todas
// as dependências sem erro, sobe de verdade (servidor respondendo),
// e no Stop libera os recursos que abriu (pool do Postgres fechado,
// servidor HTTP não aceita mais conexão)?
//
// Usa o MESMO bootstrap.Module que cmd/api/main.go usa em produção —
// não uma cópia simplificada — porque o que se quer confirmar é a
// composição real, não uma composição parecida.
package integration

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"go.uber.org/fx"

	"github.com/raelmz/wagerflow-go/internal/bootstrap"
	"github.com/raelmz/wagerflow-go/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

// fxTestEnv define as variáveis de ambiente que config.Load() exige,
// com o mesmo default "visto do host" que .env.example já documenta
// para os outros testes de integração — exceto HTTP_PORT, que usa
// 8090 (não 8080) de propósito: evita conflitar com o container
// wagerflow-api caso o desenvolvedor tenha rodado
// `docker compose up --build` e o deixado no ar enquanto roda este
// teste separadamente.
var fxTestEnvDefaults = map[string]string{
	"DATABASE_URL":        "postgres://wagerflow:wagerflow@localhost:5432/wagerflow?sslmode=disable",
	"KEYCLOAK_ISSUER_URL": "http://localhost:8081/realms/wagerflow",
	"SQS_ENDPOINT_URL":    "http://localhost:4566",
	"AWS_REGION":          "us-east-1",
	"HTTP_PORT":           "8090",
}

// setFxTestEnv aplica os defaults acima só para as variáveis que
// ainda não estão setadas (permite o desenvolvedor sobrescrever, ex.
// rodando dentro de um container na rede do compose, igual já se faz
// com TEST_KEYCLOAK_ISSUER_URL nos outros arquivos), e devolve uma
// função para desfazer no final do teste — para não vazar env var
// para os outros testes do mesmo processo `go test`.
func setFxTestEnv(t *testing.T) {
	t.Helper()
	for key, def := range fxTestEnvDefaults {
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, def); err != nil {
			t.Fatalf("falha ao definir %s: %v", key, err)
		}
		t.Cleanup(func() { _ = os.Unsetenv(key) })
	}
}

// TestFxAppLifecycle monta a composição REAL de cmd/api (via
// bootstrap.Module), chama Start, confirma que subiu de verdade, e
// depois confirma que Stop libera os recursos.
//
// Requer Postgres, Keycloak e LocalStack reais no ar — o mesmo
// docker-compose.yml completo que os outros testes de integração já
// exigem (não cria um banco isolado como wagerflow_integration_test.go,
// porque este teste não grava dado nenhum; só abre e fecha o pool).
func TestFxAppLifecycle(t *testing.T) {
	setFxTestEnv(t)

	var (
		cfg  *config.Config
		pool *pgxpool.Pool
	)

	app := fx.New(
		bootstrap.Module,
		// fx.Populate deixa o TESTE segurar uma referência aos
		// mesmos objetos que a composição de produção monta — sem
		// isso não teria como confirmar depois do Stop que o pool
		// realmente fechou, já que ele nunca sai de dentro do Fx em
		// produção.
		fx.Populate(&cfg, &pool),
	)

	startCtx, cancelStart := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStart()

	if err := app.Start(startCtx); err != nil {
		t.Fatalf("app.Start falhou — composição Fx não resolveu ou boot deu erro: %v", err)
	}

	if pool == nil {
		t.Fatal("fx.Populate não preencheu o pool do Postgres — composição mudou?")
	}
	if cfg == nil {
		t.Fatal("fx.Populate não preencheu o config.Config — composição mudou?")
	}

	// Confirma que o servidor HTTP registrado pelo OnStart de
	// registerHTTPServer está de fato aceitando conexão — Start()
	// não bloqueia até o listener estar pronto (é assíncrono de
	// propósito, para não travar o boot), então dá um retry curto
	// antes de desistir.
	addr := "localhost:" + cfg.HTTPPort
	if !waitForServerUp(addr, 5*time.Second) {
		t.Fatalf("servidor HTTP não respondeu em %s depois do Start", addr)
	}

	resp, err := http.Get("http://" + addr + "/health/live")
	if err != nil {
		t.Fatalf("GET /health/live falhou depois do Start: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/health/live retornou %d, esperava 200", resp.StatusCode)
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStop()

	if err := app.Stop(stopCtx); err != nil {
		t.Fatalf("app.Stop falhou — encerramento não foi limpo: %v", err)
	}

	// Liberação de recurso nº 1: o pool do Postgres. newPool registra
	// um OnStop que chama pool.Close() — confirmamos que rodou de
	// verdade tentando usar o pool depois do Stop: um pool fechado
	// recusa Acquire/Ping.
	pingCtx, cancelPing := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelPing()
	if err := pool.Ping(pingCtx); err == nil {
		t.Fatal("pool do Postgres respondeu Ping depois do Stop — OnStop não fechou o pool")
	}

	// Liberação de recurso nº 2: o servidor HTTP. registerHTTPServer
	// registra um OnStop que chama server.Shutdown() — confirmamos
	// que a porta não aceita mais conexão.
	if waitForServerDown(addr, 3*time.Second) {
		return
	}
	t.Fatalf("servidor HTTP em %s ainda aceita conexão depois do Stop — OnStop não desligou o servidor", addr)
}

func waitForServerUp(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func waitForServerDown(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return true
		}
		_ = conn.Close()
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
