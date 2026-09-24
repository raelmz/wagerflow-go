package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// skewedWagerRepo simula o que acontece em READ COMMITTED no Postgres
// real sob corrida: a busca por idempotencyKey ainda não enxerga a
// linha que a busca por externalId já enxerga (duas buscas, dois
// instantes diferentes do banco). É exatamente o cenário do bug real
// encontrado nos testes de integração (seção 4 do contexto de sessão):
// o perdedor da corrida recebia ErrExternalTransactionConflict por
// engano, quando na verdade era a MESMA operação.
type skewedWagerRepo struct {
	*memWagerRepo
}

func (r *skewedWagerRepo) FindByProviderAndIdempotencyKey(_ context.Context, _, _ string) (*domain.WagerTransaction, error) {
	return nil, nil // "ainda não visível" para esta busca
}

type skewedUoW struct{ inner *memUoW }

func (u *skewedUoW) Wallets() domain.WalletRepository { return u.inner.Wallets() }
func (u *skewedUoW) WagerTransactions() domain.WagerTransactionRepository {
	return &skewedWagerRepo{u.inner.WagerTransactions().(*memWagerRepo)}
}
func (u *skewedUoW) LedgerEntries() domain.WalletLedgerEntryRepository {
	return u.inner.LedgerEntries()
}
func (u *skewedUoW) Outbox() domain.OutboxRepository { return u.inner.Outbox() }

func TestFindExisting_MesmaChaveVistaSoPorExternalID_EhReplay(t *testing.T) {
	store := newMemStore()
	walletID, playerID := uuid.New(), uuid.New()
	store.wallets[walletID] = domain.RehydrateWallet(walletID, playerID, "BRL", mustMoney(t, "100.00"), 1, time.Now(), time.Now())

	winner, err := domain.NewExternalWagerTransaction(
		"ext-1", "provider-x", "key-1", "hash-1",
		walletID, playerID, "round-1", "game-1",
		domain.KindBet, mustMoney(t, "10.00"), "",
	)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	winner.MarkProcessed(mustMoney(t, "90.00"))
	store.txs[winner.ID()] = winner

	loser, err := domain.NewExternalWagerTransaction(
		"ext-1", "provider-x", "key-1", "hash-1",
		walletID, playerID, "round-1", "game-1",
		domain.KindBet, mustMoney(t, "10.00"), "",
	)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	uow := &skewedUoW{inner: &memUoW{store: store}}

	existing, err := findExisting(context.Background(), uow, loser)
	if err != nil {
		t.Fatalf("não esperava erro (esperava replay), veio: %v", err)
	}
	if existing == nil || existing.ID() != winner.ID() {
		t.Fatalf("esperava devolver o vencedor (%s) como replay, veio: %+v", winner.ID(), existing)
	}
}

func TestFindExisting_ExternalIDDeOutraChaveContinuaConflito(t *testing.T) {
	store := newMemStore()
	walletID, playerID := uuid.New(), uuid.New()
	store.wallets[walletID] = domain.RehydrateWallet(walletID, playerID, "BRL", mustMoney(t, "100.00"), 1, time.Now(), time.Now())

	existingTx, err := domain.NewExternalWagerTransaction(
		"ext-1", "provider-x", "key-A", "hash-1",
		walletID, playerID, "round-1", "game-1",
		domain.KindBet, mustMoney(t, "10.00"), "",
	)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	existingTx.MarkProcessed(mustMoney(t, "90.00"))
	store.txs[existingTx.ID()] = existingTx

	candidate, err := domain.NewExternalWagerTransaction(
		"ext-1", "provider-x", "key-B", "hash-2",
		walletID, playerID, "round-1", "game-1",
		domain.KindBet, mustMoney(t, "10.00"), "",
	)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	uow := &memUoW{store: store}

	_, err = findExisting(context.Background(), uow, candidate)
	if !errors.Is(err, domain.ErrExternalTransactionConflict) {
		t.Fatalf("esperava ErrExternalTransactionConflict, veio: %v", err)
	}
}
