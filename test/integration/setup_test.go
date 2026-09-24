//go:build integration

// Pacote integration roda os testes obrigatórios da seção 13 do
// desafio: concorrência, atomicidade e constraints, todos contra um
// Postgres REAL — nunca contra fakes em memória. Por isso este
// arquivo tem a build tag "integration": ele só compila e roda com
// `go test -tags=integration`, nunca em `go test ./...` normal (que
// não precisa de banco nenhum).
//
// Cada teste ganha um BANCO isolado (não um schema, um banco de
// verdade), criado e apagado nesta função, com as migrations
// aplicadas do zero. Isso evita testes de concorrência contaminando
// uns aos outros por causa de estado deixado para trás.
package integration

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// baseDatabaseURL lê a variável de ambiente que aponta para um
// Postgres já rodando (Docker Compose local ou CI), usada só para
// CRIAR/APAGAR os bancos de teste — cada teste roda contra o SEU
// PRÓPRIO banco, criado a partir desta conexão "administrativa".
//
// Exemplo (o mesmo Postgres do docker-compose.yml do projeto):
//
//	TEST_DATABASE_URL=postgres://wagerflow:wagerflow@localhost:5432/postgres?sslmode=disable
func baseDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("defina TEST_DATABASE_URL antes de rodar os testes de integração " +
			"(ex: postgres://wagerflow:wagerflow@localhost:5432/postgres?sslmode=disable)")
	}
	return url
}

// migrationsDir localiza a pasta migrations/ subindo a partir deste
// arquivo — assim o teste funciona não importa de onde `go test` é
// chamado.
func migrationsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatalf("não consegui localizar migrations/: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("migrations/ não encontrada em %s: %v", dir, err)
	}
	return dir
}

// applyMigrations aplica, em ordem, todos os arquivos *.up.sql da
// pasta migrations/. Não usa nenhuma lib de migration: é só ler os
// arquivos em ordem alfabética (o prefixo 000001, 000002... já
// garante a ordem certa) e executar.
func applyMigrations(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	dir := migrationsDir(t)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("falha ao ler migrations/: %v", err)
	}

	var ups []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) == ".sql" && len(e.Name()) > 6 && e.Name()[len(e.Name())-6:] == "up.sql" {
			ups = append(ups, e.Name())
		}
	}
	sort.Strings(ups)
	if len(ups) == 0 {
		t.Fatal("nenhuma migration *.up.sql encontrada")
	}

	for _, name := range ups {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("falha ao ler %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			t.Fatalf("falha ao aplicar %s: %v", name, err)
		}
	}
}

// applyDownMigrations desfaz na ordem inversa — usado pelo teste
// "up/down/up" que prova que as migrations são reversíveis.
func applyDownMigrations(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	dir := migrationsDir(t)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("falha ao ler migrations/: %v", err)
	}

	var downs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if len(e.Name()) > 8 && e.Name()[len(e.Name())-8:] == "down.sql" {
			downs = append(downs, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(downs)))
	if len(downs) == 0 {
		t.Fatal("nenhuma migration *.down.sql encontrada")
	}

	for _, name := range downs {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("falha ao ler %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			t.Fatalf("falha ao desfazer %s: %v", name, err)
		}
	}
}

// newTestDatabase cria um banco novo com nome aleatório, devolve um
// pool JÁ conectado a ele com as migrations aplicadas, e registra o
// cleanup (fecha o pool e apaga o banco) para quando o teste acabar —
// mesmo em caso de falha (t.Cleanup roda sempre).
func newTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	adminURL := baseDatabaseURL(t)
	adminConn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("falha ao conectar no Postgres administrativo: %v", err)
	}
	defer adminConn.Close(ctx)

	dbName := fmt.Sprintf("wagerflow_test_%d_%d", time.Now().UnixNano(), rand.Intn(1_000_000))
	if _, err := adminConn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, pgx.Identifier{dbName}.Sanitize())); err != nil {
		t.Fatalf("falha ao criar banco de teste %s: %v", dbName, err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		conn, err := pgx.Connect(cleanupCtx, adminURL)
		if err != nil {
			t.Logf("aviso: não consegui conectar para apagar %s: %v", dbName, err)
			return
		}
		defer conn.Close(cleanupCtx)
		// Força o fim de conexões residuais antes do DROP, senão o
		// Postgres recusa (\"database is being accessed by other users\").
		_, _ = conn.Exec(cleanupCtx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, dbName)
		if _, err := conn.Exec(cleanupCtx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, pgx.Identifier{dbName}.Sanitize())); err != nil {
			t.Logf("aviso: não consegui apagar %s: %v", dbName, err)
		}
	})

	testURL := replaceDatabaseName(adminURL, dbName)
	pool, err := pgxpool.New(ctx, testURL)
	if err != nil {
		t.Fatalf("falha ao conectar no banco de teste %s: %v", dbName, err)
	}
	t.Cleanup(pool.Close)

	applyMigrations(ctx, t, pool)

	return pool
}

// replaceDatabaseName troca o nome do banco em uma URL de conexão
// Postgres (postgres://user:pass@host:port/DBNAME?params), mantendo
// usuário, host e parâmetros — só o path muda.
func replaceDatabaseName(databaseURL, newName string) string {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		// Fallback simples baseado em string, caso ParseConfig não
		// aceite algum parâmetro exótico da URL original.
		return databaseURL
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Host, cfg.Port, newName)
}
