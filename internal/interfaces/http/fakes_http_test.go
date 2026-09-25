package http

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// Este arquivo contém implementações EM MEMÓRIA das interfaces do
// domínio, usadas SÓ pelos testes de handler HTTP deste pacote. É o
// mesmo espírito de internal/application/fakes_test.go (que já
// existia antes destes testes) — só que aqui, para poder construir os
// casos de uso de verdade (application.WalletHandler etc. recebem
// *application.XxxUseCase concretos, não interfaces), precisamos das
// nossas próprias implementações porque aquele arquivo é interno ao
// pacote application e não pode ser importado daqui.
//
// LIMITES conscientes (mesmos do arquivo que inspirou este): sem
// rollback simulado, sem concorrência real. Isso é papel dos testes
// de integração contra Postgres real (test/integration/), não destes
// testes de handler, cujo objetivo é só o contrato HTTP (formato do
// JSON, mapeamento de status, isolamento de providerId, formato do
// cursor).

type fakeStore struct {
	mu      sync.Mutex
	wallets map[uuid.UUID]*domain.Wallet
	txs     map[uuid.UUID]*domain.WagerTransaction
	entries []*domain.WalletLedgerEntry
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		wallets: map[uuid.UUID]*domain.Wallet{},
		txs:     map[uuid.UUID]*domain.WagerTransaction{},
	}
}

func (s *fakeStore) WithinTransaction(_ context.Context, fn func(uow domain.UnitOfWork) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(&fakeUoW{store: s})
}

type fakeUoW struct{ store *fakeStore }

func (u *fakeUoW) Wallets() domain.WalletRepository { return &fakeWalletRepo{u.store} }
func (u *fakeUoW) WagerTransactions() domain.WagerTransactionRepository {
	return &fakeWagerRepo{u.store}
}
func (u *fakeUoW) LedgerEntries() domain.WalletLedgerEntryRepository {
	return &fakeLedgerRepo{u.store}
}
func (u *fakeUoW) Outbox() domain.OutboxRepository { return &fakeOutboxRepo{} }
func (u *fakeUoW) Inbox() domain.InboxRepository   { return &fakeInboxRepo{} }

// --- Carteiras ---

type fakeWalletRepo struct{ s *fakeStore }

func cloneFakeWallet(w *domain.Wallet) *domain.Wallet {
	return domain.RehydrateWallet(w.ID(), w.PlayerID(), w.Currency(), w.Balance(), w.Version(), w.CreatedAt(), w.UpdatedAt())
}

func (r *fakeWalletRepo) Create(_ context.Context, w *domain.Wallet) error {
	for _, existing := range r.s.wallets {
		if existing.PlayerID() == w.PlayerID() && existing.Currency() == w.Currency() {
			return domain.ErrWalletAlreadyExists
		}
	}
	r.s.wallets[w.ID()] = cloneFakeWallet(w)
	return nil
}

func (r *fakeWalletRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Wallet, error) {
	w, ok := r.s.wallets[id]
	if !ok {
		return nil, nil
	}
	return cloneFakeWallet(w), nil
}

func (r *fakeWalletRepo) FindByPlayerAndCurrency(_ context.Context, playerID uuid.UUID, currency string) (*domain.Wallet, error) {
	for _, w := range r.s.wallets {
		if w.PlayerID() == playerID && w.Currency() == currency {
			return cloneFakeWallet(w), nil
		}
	}
	return nil, nil
}

func (r *fakeWalletRepo) Debit(_ context.Context, id uuid.UUID, amount domain.Money) (*domain.Wallet, error) {
	w, ok := r.s.wallets[id]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	if err := w.Debit(amount); err != nil {
		return nil, err
	}
	return cloneFakeWallet(w), nil
}

func (r *fakeWalletRepo) Credit(_ context.Context, id uuid.UUID, amount domain.Money) (*domain.Wallet, error) {
	w, ok := r.s.wallets[id]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	if err := w.Credit(amount); err != nil {
		return nil, err
	}
	return cloneFakeWallet(w), nil
}

// --- Transações ---

type fakeWagerRepo struct{ s *fakeStore }

func (r *fakeWagerRepo) Create(_ context.Context, tx *domain.WagerTransaction) error {
	r.s.txs[tx.ID()] = tx
	return nil
}

func (r *fakeWagerRepo) Update(_ context.Context, tx *domain.WagerTransaction) error {
	r.s.txs[tx.ID()] = tx
	return nil
}

func (r *fakeWagerRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.WagerTransaction, error) {
	tx, ok := r.s.txs[id]
	if !ok {
		return nil, nil
	}
	return tx, nil
}

func (r *fakeWagerRepo) FindByProviderAndIdempotencyKey(_ context.Context, providerID, idempotencyKey string) (*domain.WagerTransaction, error) {
	for _, tx := range r.s.txs {
		if tx.ProviderID() == providerID && tx.IdempotencyKey() == idempotencyKey {
			return tx, nil
		}
	}
	return nil, nil
}

func (r *fakeWagerRepo) FindByProviderAndExternalTxID(_ context.Context, providerID, externalTransactionID string) (*domain.WagerTransaction, error) {
	for _, tx := range r.s.txs {
		if tx.ProviderID() == providerID && tx.ExternalTransactionID() == externalTransactionID {
			return tx, nil
		}
	}
	return nil, nil
}

func (r *fakeWagerRepo) LockByProviderAndExternalTxID(ctx context.Context, providerID, externalTransactionID string) (*domain.WagerTransaction, error) {
	return r.FindByProviderAndExternalTxID(ctx, providerID, externalTransactionID)
}

func (r *fakeWagerRepo) HasProcessedReversalOf(_ context.Context, referenceID uuid.UUID) (bool, error) {
	for _, tx := range r.s.txs {
		reversal := tx.Kind() == domain.KindRefund || tx.Kind() == domain.KindRollback
		if reversal && tx.ResolvedReferenceID() == referenceID && tx.Status() == domain.StatusProcessed {
			return true, nil
		}
	}
	return false, nil
}

// --- Ledger ---

type fakeLedgerRepo struct{ s *fakeStore }

func (r *fakeLedgerRepo) Create(_ context.Context, entry *domain.WalletLedgerEntry) error {
	r.s.entries = append(r.s.entries, entry)
	return nil
}

// ListByWallet aqui é deliberadamente simples (sem paginação real por
// cursor opaco): devolve tudo que pertence à carteira, numa página só.
// Isso basta para os testes de handler, que checam o FORMATO da
// resposta (contrato JSON), não o algoritmo de paginação em si — esse
// já está coberto pelos testes de integração contra Postgres real.
func (r *fakeLedgerRepo) ListByWallet(_ context.Context, walletID uuid.UUID, _ string, limit int) ([]*domain.WalletLedgerEntry, string, error) {
	var out []*domain.WalletLedgerEntry
	for _, e := range r.s.entries {
		if e.WalletID() == walletID {
			out = append(out, e)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, "", nil
}

func (r *fakeLedgerRepo) SumByWallet(_ context.Context, walletID uuid.UUID) (int64, int, error) {
	var sum int64
	checked := 0
	for _, e := range r.s.entries {
		if e.WalletID() != walletID {
			continue
		}
		checked++
		if e.Direction() == domain.DirectionCredit {
			sum += e.Amount().Cents()
		} else {
			sum -= e.Amount().Cents()
		}
	}
	return sum, checked, nil
}

// --- Outbox/Inbox: sem-op, os testes de handler não verificam eventos ---

type fakeOutboxRepo struct{}

func (r *fakeOutboxRepo) Append(_ context.Context, _ *domain.OutboxEvent) error { return nil }

type fakeInboxRepo struct{}

func (r *fakeInboxRepo) TryInsert(_ context.Context, _, _, _ string) (bool, error) {
	return true, nil
}
