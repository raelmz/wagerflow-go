// Este é um programinha "descartável" só para você VER o repositório
// funcionando contra o Postgres real, antes de termos a API HTTP.
// Não faz parte da aplicação final — é só uma ferramenta de conferência
// manual. Fica em cmd/smoketest porque, em Go, cada pasta dentro de
// cmd/ vira um binário executável separado (main.go dentro dela).
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/raelmz/wagerflow-go/internal/domain"
	"github.com/raelmz/wagerflow-go/internal/infrastructure/postgres"
)

func main() {
	// Carrega o .env para pegar a DATABASE_URL, igual a aplicação
	// real vai fazer depois.
	_ = godotenv.Load()

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("erro ao conectar: %v", err)
	}
	defer pool.Close()

	repo := postgres.NewWalletRepository(pool)

	// 1. Cria uma carteira nova com saldo inicial de 1000.00 BRL.
	saldoInicial, _ := domain.NewMoneyFromString("1000.00", "BRL")
	wallet, err := domain.NewWallet(uuid.New(), saldoInicial)
	if err != nil {
		log.Fatalf("erro ao construir wallet: %v", err)
	}
	if err := repo.Create(ctx, wallet); err != nil {
		log.Fatalf("erro ao criar wallet no banco: %v", err)
	}
	fmt.Printf("✅ Carteira criada: %s | saldo: %s | versão: %d\n", wallet.ID(), wallet.Balance(), wallet.Version())

	// 2. Debita 80.00 (deve funcionar).
	aposta, _ := domain.NewMoneyFromString("80.00", "BRL")
	afterDebit, err := repo.Debit(ctx, wallet.ID(), aposta)
	if err != nil {
		log.Fatalf("erro inesperado no débito: %v", err)
	}
	fmt.Printf("✅ Após débito de 80.00: saldo: %s | versão: %d\n", afterDebit.Balance(), afterDebit.Version())

	// 3. Tenta debitar 5000.00 (deve FALHAR com saldo insuficiente —
	// esse erro aparecendo é o resultado ESPERADO, não um bug).
	valorAlto, _ := domain.NewMoneyFromString("5000.00", "BRL")
	_, err = repo.Debit(ctx, wallet.ID(), valorAlto)
	if err != nil {
		fmt.Printf("✅ Débito de 5000.00 rejeitado como esperado: %v\n", err)
	} else {
		fmt.Println("❌ ERRO: débito de 5000.00 deveria ter sido rejeitado!")
	}

	// 4. Credita 20.00.
	credito, _ := domain.NewMoneyFromString("20.00", "BRL")
	afterCredit, err := repo.Credit(ctx, wallet.ID(), credito)
	if err != nil {
		log.Fatalf("erro inesperado no crédito: %v", err)
	}
	fmt.Printf("✅ Após crédito de 20.00: saldo: %s | versão: %d\n", afterCredit.Balance(), afterCredit.Version())

	fmt.Println("\nSaldo esperado ao final: 1000.00 - 80.00 + 20.00 = 940.00")
}
