// Programa de teste manual (smoke test) — não faz parte da aplicação
// final, é só para conferir visualmente que o fluxo completo (Wallet
// + WagerTransaction + WalletLedgerEntry, tudo no mesmo commit)
// funciona contra o Postgres real.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/raelmz/wagerflow-go/internal/application"
	"github.com/raelmz/wagerflow-go/internal/domain"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/postgres"
)

func main() {
	_ = godotenv.Load()

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("erro ao conectar: %v", err)
	}
	defer pool.Close()

	txManager := postgres.NewTxManager(pool)
	openWallet := application.NewOpenWalletUseCase(txManager)

	// 1. Abre uma carteira com saldo inicial de 1000.00 BRL.
	// Isso deve criar: a wallet, a WagerTransaction OPENING (PROCESSED)
	// e o WalletLedgerEntry de crédito — tudo no mesmo commit.
	saldoInicial, _ := domain.NewMoneyFromString("1000.00", "BRL")
	result, err := openWallet.Execute(ctx, uuid.New(), saldoInicial)
	if err != nil {
		log.Fatalf("erro ao abrir carteira: %v", err)
	}
	fmt.Printf("✅ Carteira aberta: %s | saldo: %s | versão: %d\n",
		result.Wallet.ID(), result.Wallet.Balance(), result.Wallet.Version())

	// 2. Confere direto no repositório que a transação OPENING e o
	// ledger entry realmente foram gravados (não só a wallet).
	wagerRepo := postgres.NewWagerTransactionRepository(pool)
	_ = wagerRepo // (a consulta de OPENING não tem um Find dedicado ainda;
	// a conferência visual via psql abaixo, no passo a passo, cobre isso por ora)

	fmt.Println("\nPara conferir manualmente no banco:")
	fmt.Printf("SELECT * FROM wager_transactions WHERE wallet_id = '%s';\n", result.Wallet.ID())
	fmt.Printf("SELECT * FROM wallet_ledger_entries WHERE wallet_id = '%s';\n", result.Wallet.ID())
}
