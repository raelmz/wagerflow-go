//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/domain"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/postgres"
)

// --- Infraestrutura comum dos testes ---

// harness agrupa o pool e os dois casos de uso, prontos para os
// testes chamarem direto — o mesmo caminho que a API HTTP vai usar
// no futuro (nenhum destes testes fala SQL diretamente, exceto para
// checar o estado final).
type harness struct {
	pool    *pgxpool.Pool
	openUC  *application.OpenWalletUseCase
	wagerUC *application.ProcessWagerTransactionUseCase
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := newTestDatabase(t)
	txm := postgres.NewTxManager(pool)
	return &harness{
		pool:    pool,
		openUC:  application.NewOpenWalletUseCase(txm),
		wagerUC: application.NewProcessWagerTransactionUseCase(txm),
	}
}

func money(t *testing.T, amount string) domain.Money {
	t.Helper()
	m, err := domain.NewMoneyFromString(amount, "BRL")
	if err != nil {
		t.Fatalf("valor inválido no teste (%s): %v", amount, err)
	}
	return m
}

func openWallet(t *testing.T, h *harness, initial string) uuid.UUID {
	t.Helper()
	res, err := h.openUC.Execute(context.Background(), uuid.New(), money(t, initial))
	if err != nil {
		t.Fatalf("falha ao abrir carteira: %v", err)
	}
	return res.Wallet.ID()
}

func bet(t *testing.T, h *harness, walletID, playerID uuid.UUID, externalID, idempotencyKey, amount string) *application.ProcessWagerResult {
	t.Helper()
	res, err := h.wagerUC.Execute(context.Background(), application.ProcessWagerCommand{
		IdempotencyKey:        idempotencyKey,
		ProviderID:            "provider-x",
		ExternalTransactionID: externalID,
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  domain.KindBet,
		Amount:                amount,
		Currency:              "BRL",
	})
	if err != nil {
		t.Fatalf("falha inesperada em BET (%s): %v", externalID, err)
	}
	return res
}

// walletBalance lê o saldo direto do banco, para não depender de
// nenhum caminho de leitura ainda não implementado (não há GET
// /wallets/:id nesta fase do projeto).
func walletBalance(t *testing.T, pool *pgxpool.Pool, walletID uuid.UUID) int64 {
	t.Helper()
	var cents int64
	err := pool.QueryRow(context.Background(),
		`SELECT balance_cents FROM wallets WHERE id = $1`, walletID).Scan(&cents)
	if err != nil {
		t.Fatalf("falha ao ler saldo: %v", err)
	}
	return cents
}

func ledgerCount(t *testing.T, pool *pgxpool.Pool, walletID uuid.UUID) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM wallet_ledger_entries WHERE wallet_id = $1`, walletID).Scan(&n)
	if err != nil {
		t.Fatalf("falha ao contar ledger: %v", err)
	}
	return n
}

func outboxCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox_events`).Scan(&n)
	if err != nil {
		t.Fatalf("falha ao contar outbox: %v", err)
	}
	return n
}

// ---------------------------------------------------------------------
// 1. Migrations: up/down/up
// ---------------------------------------------------------------------

func TestMigrations_UpDownUp(t *testing.T) {
	pool := newTestDatabase(t) // já aplicou "up" uma vez, no setup

	ctx := context.Background()
	applyDownMigrations(ctx, t, pool)
	applyMigrations(ctx, t, pool)

	// Depois do ciclo up/down/up, o schema deve estar utilizável: uma
	// operação simples de ponta a ponta prova isso.
	txm := postgres.NewTxManager(pool)
	openUC := application.NewOpenWalletUseCase(txm)
	res, err := openUC.Execute(ctx, uuid.New(), money(t, "10.00"))
	if err != nil {
		t.Fatalf("schema não ficou utilizável após up/down/up: %v", err)
	}
	if res.Wallet.Balance().Cents() != 1000 {
		t.Fatalf("saldo inesperado após reaplicar migrations: %d", res.Wallet.Balance().Cents())
	}
}

// ---------------------------------------------------------------------
// 2. Imutabilidade do ledger (UPDATE, DELETE, TRUNCATE)
// ---------------------------------------------------------------------

func TestLedger_ImutavelContraUpdateDeleteTruncate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	walletID := openWallet(t, h, "100.00")

	if ledgerCount(t, h.pool, walletID) != 1 {
		t.Fatalf("esperava 1 lançamento (crédito de abertura)")
	}

	if _, err := h.pool.Exec(ctx, `UPDATE wallet_ledger_entries SET amount_cents = 1 WHERE wallet_id = $1`, walletID); err == nil {
		t.Fatal("esperava que UPDATE no ledger fosse recusado pelo trigger")
	}
	if _, err := h.pool.Exec(ctx, `DELETE FROM wallet_ledger_entries WHERE wallet_id = $1`, walletID); err == nil {
		t.Fatal("esperava que DELETE no ledger fosse recusado pelo trigger")
	}
	// TRUNCATE é um comando de outra categoria (statement-level, não
	// row-level) — é o bug real corrigido na migration 000006. Sem
	// ela, este TRUNCATE passava sem erro e apagava tudo.
	if _, err := h.pool.Exec(ctx, `TRUNCATE wallet_ledger_entries`); err == nil {
		t.Fatal("esperava que TRUNCATE no ledger fosse recusado pelo trigger (migration 000006)")
	}

	if ledgerCount(t, h.pool, walletID) != 1 {
		t.Fatal("o lançamento original não deveria ter sido afetado pelas tentativas recusadas")
	}
}

// ---------------------------------------------------------------------
// 3. Duas apostas de 80.00 sobre saldo de 100.00, simultâneas
// ---------------------------------------------------------------------

func TestConcorrencia_DuasApostasDeOitentaSobreSaldoDeCem(t *testing.T) {
	const rounds = 30

	for i := 0; i < rounds; i++ {
		i := i
		t.Run(fmt.Sprintf("rodada_%02d", i), func(t *testing.T) {
			h := newHarness(t)
			playerID := uuid.New()
			walletID := openWallet(t, h, "100.00")
			// Abrir a carteira já usa um playerID aleatório dentro do
			// use case; para as apostas usarmos o MESMO player/wallet,
			// buscamos o playerID de volta do banco.
			var dbPlayerID uuid.UUID
			if err := h.pool.QueryRow(context.Background(),
				`SELECT player_id FROM wallets WHERE id = $1`, walletID).Scan(&dbPlayerID); err != nil {
				t.Fatalf("falha ao ler playerId: %v", err)
			}
			playerID = dbPlayerID

			var wg sync.WaitGroup
			results := make([]*application.ProcessWagerResult, 2)
			wg.Add(2)
			go func() {
				defer wg.Done()
				results[0] = bet(t, h, walletID, playerID, "ext-a", "key-a", "80.00")
			}()
			go func() {
				defer wg.Done()
				results[1] = bet(t, h, walletID, playerID, "ext-b", "key-b", "80.00")
			}()
			wg.Wait()

			processed, rejected := 0, 0
			for _, r := range results {
				switch r.Transaction.Status() {
				case domain.StatusProcessed:
					processed++
				case domain.StatusRejected:
					rejected++
					if r.Transaction.FailureCode() != domain.FailureInsufficientBalance {
						t.Errorf("esperava failureCode INSUFFICIENT_BALANCE, veio %s", r.Transaction.FailureCode())
					}
				default:
					t.Errorf("status inesperado: %s", r.Transaction.Status())
				}
			}
			if processed != 1 || rejected != 1 {
				t.Fatalf("esperava exatamente 1 processada e 1 rejeitada, veio processed=%d rejected=%d", processed, rejected)
			}

			if got := walletBalance(t, h.pool, walletID); got != 2000 {
				t.Fatalf("esperava saldo final 20.00 (2000 centavos), veio %d", got)
			}
			// 1 lançamento da abertura + 1 do débito aceito = 2.
			if got := ledgerCount(t, h.pool, walletID); got != 2 {
				t.Fatalf("esperava 2 lançamentos no ledger (abertura + 1 débito), veio %d", got)
			}
		})
	}
}

// ---------------------------------------------------------------------
// 4. A mesma aposta enviada 50 vezes em paralelo (mesma idempotencyKey
//    e mesmo externalTransactionId) só pode debitar uma vez.
// ---------------------------------------------------------------------

func TestConcorrencia_MesmaApostaCinquentaVezesEmParalelo(t *testing.T) {
	h := newHarness(t)
	walletID := openWallet(t, h, "1000.00")
	var playerID uuid.UUID
	if err := h.pool.QueryRow(context.Background(),
		`SELECT player_id FROM wallets WHERE id = $1`, walletID).Scan(&playerID); err != nil {
		t.Fatalf("falha ao ler playerId: %v", err)
	}

	const attempts = 50
	var wg sync.WaitGroup
	results := make([]*application.ProcessWagerResult, attempts)
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		i := i
		go func() {
			defer wg.Done()
			results[i] = bet(t, h, walletID, playerID, "ext-repeat", "key-repeat", "10.00")
		}()
	}
	wg.Wait()

	var txID uuid.UUID
	replays := 0
	for _, r := range results {
		if r.Transaction.ID() == uuid.Nil {
			t.Fatal("transação sem id")
		}
		if txID == uuid.Nil {
			txID = r.Transaction.ID()
		} else if r.Transaction.ID() != txID {
			t.Fatalf("duas requisições iguais resultaram em transações diferentes: %s e %s", txID, r.Transaction.ID())
		}
		if r.Replay {
			replays++
		}
	}
	if replays != attempts-1 {
		t.Fatalf("esperava %d replays (só a 1ª não é replay), veio %d", attempts-1, replays)
	}

	if got := walletBalance(t, h.pool, walletID); got != 99000 {
		t.Fatalf("esperava saldo 990.00 (99000 centavos, só 1 débito de 10.00), veio %d", got)
	}
	if got := ledgerCount(t, h.pool, walletID); got != 2 {
		t.Fatalf("esperava 2 lançamentos (abertura + 1 débito), veio %d", got)
	}
}

// ---------------------------------------------------------------------
// 5. Carteiras diferentes em paralelo: sem lock global.
//    Prova indireta: todas terminam com o saldo correto mesmo
//    concorrendo ao mesmo tempo (se houvesse lock global travando por
//    engano, o teste ainda passaria, só mais devagar — o objetivo
//    aqui é a CORREÇÃO sob concorrência real entre carteiras distintas).
// ---------------------------------------------------------------------

func TestConcorrencia_CarteirasDiferentesEmParalelo(t *testing.T) {
	h := newHarness(t)
	const nWallets = 10

	type wallet struct {
		id       uuid.UUID
		playerID uuid.UUID
	}
	wallets := make([]wallet, nWallets)
	for i := range wallets {
		wallets[i].id = openWallet(t, h, "50.00")
		if err := h.pool.QueryRow(context.Background(),
			`SELECT player_id FROM wallets WHERE id = $1`, wallets[i].id).Scan(&wallets[i].playerID); err != nil {
			t.Fatalf("falha ao ler playerId: %v", err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(nWallets)
	for i, w := range wallets {
		i, w := i, w
		go func() {
			defer wg.Done()
			bet(t, h, w.id, w.playerID, fmt.Sprintf("ext-%d", i), fmt.Sprintf("key-%d", i), "20.00")
		}()
	}
	wg.Wait()

	for _, w := range wallets {
		if got := walletBalance(t, h.pool, w.id); got != 3000 {
			t.Errorf("carteira %s: esperava saldo 30.00 (3000 centavos), veio %d", w.id, got)
		}
	}
}

// ---------------------------------------------------------------------
// 6. Duas reversões concorrentes da mesma aposta: só uma pode ganhar.
// ---------------------------------------------------------------------

func TestConcorrencia_DuasReversoesDaMesmaAposta(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	walletID := openWallet(t, h, "100.00")
	var playerID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT player_id FROM wallets WHERE id = $1`, walletID).Scan(&playerID); err != nil {
		t.Fatalf("falha ao ler playerId: %v", err)
	}

	betResult := bet(t, h, walletID, playerID, "ext-rollback", "key-bet", "40.00")
	if betResult.Transaction.Status() != domain.StatusProcessed {
		t.Fatalf("aposta base deveria ter sido processada, status=%s", betResult.Transaction.Status())
	}

	rollback := func(externalID, key string) *application.ProcessWagerResult {
		res, err := h.wagerUC.Execute(context.Background(), application.ProcessWagerCommand{
			IdempotencyKey:                 key,
			ProviderID:                     "provider-x",
			ExternalTransactionID:          externalID,
			PlayerID:                       playerID,
			WalletID:                       walletID,
			RoundID:                        "round-1",
			GameID:                         "game-1",
			Kind:                           domain.KindRollback,
			Amount:                         "40.00",
			Currency:                       "BRL",
			ReferenceExternalTransactionID: "ext-rollback",
		})
		if err != nil {
			t.Fatalf("falha inesperada em ROLLBACK (%s): %v", externalID, err)
		}
		return res
	}

	var wg sync.WaitGroup
	results := make([]*application.ProcessWagerResult, 2)
	wg.Add(2)
	go func() { defer wg.Done(); results[0] = rollback("ext-rb-a", "key-rb-a") }()
	go func() { defer wg.Done(); results[1] = rollback("ext-rb-b", "key-rb-b") }()
	wg.Wait()

	processed, rejected := 0, 0
	for _, r := range results {
		switch r.Transaction.Status() {
		case domain.StatusProcessed:
			processed++
		case domain.StatusRejected:
			rejected++
			if r.Transaction.FailureCode() != domain.FailureReferenceAlreadyReversed {
				t.Errorf("esperava REFERENCE_ALREADY_REVERSED, veio %s", r.Transaction.FailureCode())
			}
		default:
			t.Errorf("status inesperado: %s", r.Transaction.Status())
		}
	}
	if processed != 1 || rejected != 1 {
		t.Fatalf("esperava 1 reversão processada e 1 rejeitada, veio processed=%d rejected=%d", processed, rejected)
	}

	// Saldo voltou ao original: 100.00 - 40.00 (bet) + 40.00 (1 rollback aceito).
	if got := walletBalance(t, h.pool, walletID); got != 10000 {
		t.Fatalf("esperava saldo de volta a 100.00, veio %d centavos", got)
	}
}

// ---------------------------------------------------------------------
// 7. Idempotência sobrevive a um "reinício": um pool novo, apontando
//    para o MESMO banco, ainda reconhece a operação como replay.
// ---------------------------------------------------------------------

func TestIdempotencia_SobrevivenciaAReinicio(t *testing.T) {
	h := newHarness(t)
	walletID := openWallet(t, h, "100.00")
	var playerID uuid.UUID
	if err := h.pool.QueryRow(context.Background(),
		`SELECT player_id FROM wallets WHERE id = $1`, walletID).Scan(&playerID); err != nil {
		t.Fatalf("falha ao ler playerId: %v", err)
	}

	first := bet(t, h, walletID, playerID, "ext-restart", "key-restart", "10.00")
	if first.Replay {
		t.Fatal("a primeira chamada não deveria ser replay")
	}

	// Simula reinício do processo: pool novo, casos de uso novos,
	// mesma URL de conexão (mesmo banco).
	restartedPool, err := pgxpool.New(context.Background(), h.pool.Config().ConnString())
	if err != nil {
		t.Fatalf("falha ao reconectar: %v", err)
	}
	defer restartedPool.Close()
	restartedTxm := postgres.NewTxManager(restartedPool)
	restartedUC := application.NewProcessWagerTransactionUseCase(restartedTxm)

	res, err := restartedUC.Execute(context.Background(), application.ProcessWagerCommand{
		IdempotencyKey:        "key-restart",
		ProviderID:            "provider-x",
		ExternalTransactionID: "ext-restart",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  domain.KindBet,
		Amount:                "10.00",
		Currency:              "BRL",
	})
	if err != nil {
		t.Fatalf("falha inesperada após reinício: %v", err)
	}
	if !res.Replay {
		t.Fatal("esperava replay depois do 'reinício'")
	}
	if res.Transaction.ID() != first.Transaction.ID() {
		t.Fatal("replay depois do reinício devolveu uma transação diferente")
	}
}

// ---------------------------------------------------------------------
// 8. Atomicidade: um erro depois de passos parciais não deixa nada
//    gravado (nem panic deixa).
// ---------------------------------------------------------------------

func TestAtomicidade_ErroNoMeioDaTransacaoNaoDeixaResiduo(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	walletID := openWallet(t, h, "100.00")

	txm := postgres.NewTxManager(h.pool)

	sentinel := errors.New("erro proposital de teste")
	err := txm.WithinTransaction(ctx, func(uow domain.UnitOfWork) error {
		w, err := uow.Wallets().Debit(ctx, walletID, money(t, "30.00"))
		if err != nil {
			return err
		}
		_ = w
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("esperava o erro sentinela de volta, veio: %v", err)
	}
	if got := walletBalance(t, h.pool, walletID); got != 10000 {
		t.Fatalf("débito deveria ter sido desfeito pelo rollback, saldo veio %d", got)
	}

	// Mesma prova, mas com panic no meio: defer tx.Rollback(ctx) no
	// TxManager precisa rodar mesmo assim.
	func() {
		defer func() { _ = recover() }()
		_ = txm.WithinTransaction(ctx, func(uow domain.UnitOfWork) error {
			if _, err := uow.Wallets().Debit(ctx, walletID, money(t, "30.00")); err != nil {
				return err
			}
			panic("panic proposital de teste")
		})
	}()
	if got := walletBalance(t, h.pool, walletID); got != 10000 {
		t.Fatalf("débito deveria ter sido desfeito mesmo após panic, saldo veio %d", got)
	}
}

// ---------------------------------------------------------------------
// 9. Outbox: evento gravado no MESMO commit; replay não duplica evento.
// ---------------------------------------------------------------------

func TestOutbox_AtomicoComOEstadoENaoDuplicaEmReplay(t *testing.T) {
	h := newHarness(t)
	walletID := openWallet(t, h, "100.00") // 2 eventos: Processed(OPENING) + BalanceChanged
	var playerID uuid.UUID
	if err := h.pool.QueryRow(context.Background(),
		`SELECT player_id FROM wallets WHERE id = $1`, walletID).Scan(&playerID); err != nil {
		t.Fatalf("falha ao ler playerId: %v", err)
	}

	before := outboxCount(t, h.pool)

	first := bet(t, h, walletID, playerID, "ext-outbox", "key-outbox", "10.00")
	if first.Replay {
		t.Fatal("primeira chamada não deveria ser replay")
	}
	// BET processado: Processed + BalanceChanged = +2 eventos.
	afterFirst := outboxCount(t, h.pool)
	if afterFirst-before != 2 {
		t.Fatalf("esperava +2 eventos na outbox após BET processado, veio +%d", afterFirst-before)
	}

	// Replay da MESMA operação: não deve emitir evento nenhum.
	replay := bet(t, h, walletID, playerID, "ext-outbox", "key-outbox", "10.00")
	if !replay.Replay {
		t.Fatal("segunda chamada deveria ser replay")
	}
	afterReplay := outboxCount(t, h.pool)
	if afterReplay != afterFirst {
		t.Fatalf("replay não deveria gravar novos eventos na outbox (tinha %d, ficou %d)", afterFirst, afterReplay)
	}
}
