package application

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// Este arquivo tem implementações EM MEMÓRIA das interfaces do domínio,
// usadas só em testes unitários das regras do caso de uso (rápidos, sem
// Docker).
//
// LIMITES conscientes — por isso existem testes de integração à parte:
//   - não simulam rollback: se um caso de uso devolver erro no meio, o
//     que já foi gravado no fake continua lá. Os testes unitários só
//     usam cenários em que isso não importa;
//   - não simulam isolamento de transação nem concorrência real.
//     Concorrência, atomicidade e constraints são provadas contra o
//     Postgres de verdade (requisito da seção 13 do desafio).

type memStore struct {
	mu      sync.Mutex
	wallets map[uuid.UUID]*domain.Wallet
	txs     map[uuid.UUID]*domain.WagerTransaction
	entries []*domain.WalletLedgerEntry
	events  []*domain.OutboxEvent
	inbox   map[string]string
}

func newMemStore() *memStore {
	return &memStore{
		wallets: map[uuid.UUID]*domain.Wallet{},
		txs:     map[uuid.UUID]*domain.WagerTransaction{},
		inbox:   map[string]string{},
	}
}

// WithinTransaction serializa tudo com um mutex (o "banco" em memória
// atende uma transação por vez) e entrega o UnitOfWork.
func (s *memStore) WithinTransaction(ctx context.Context, fn func(uow domain.UnitOfWork) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(&memUoW{store: s})
}

type memUoW struct{ store *memStore }

func (u *memUoW) Wallets() domain.WalletRepository { return &memWalletRepo{u.store} }
func (u *memUoW) WagerTransactions() domain.WagerTransactionRepository {
	return &memWagerRepo{u.store}
}
func (u *memUoW) LedgerEntries() domain.WalletLedgerEntryRepository {
	return &memLedgerRepo{u.store}
}
func (u *memUoW) Outbox() domain.OutboxRepository { return &memOutboxRepo{u.store} }
func (u *memUoW) Inbox() domain.InboxRepository   { return &memInboxRepo{u.store} }

// --- Carteiras ---

type memWalletRepo struct{ s *memStore }

func cloneWallet(w *domain.Wallet) *domain.Wallet {
	return domain.RehydrateWallet(w.ID(), w.PlayerID(), w.Currency(), w.Balance(), w.Version(), w.CreatedAt(), w.UpdatedAt())
}

func (r *memWalletRepo) Create(_ context.Context, w *domain.Wallet) error {
	r.s.wallets[w.ID()] = cloneWallet(w)
	return nil
}

func (r *memWalletRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Wallet, error) {
	w, ok := r.s.wallets[id]
	if !ok {
		return nil, nil
	}
	return cloneWallet(w), nil
}

func (r *memWalletRepo) FindByPlayerAndCurrency(_ context.Context, playerID uuid.UUID, currency string) (*domain.Wallet, error) {
	for _, w := range r.s.wallets {
		if w.PlayerID() == playerID && w.Currency() == currency {
			return cloneWallet(w), nil
		}
	}
	return nil, nil
}

// Debit imita o UPDATE condicionado do Postgres: só debita se houver saldo.
func (r *memWalletRepo) Debit(_ context.Context, id uuid.UUID, amount domain.Money) (*domain.Wallet, error) {
	w, ok := r.s.wallets[id]
	if !ok {
		return nil, domain.ErrInsufficientBalance
	}
	if err := w.Debit(amount); err != nil {
		return nil, err
	}
	return cloneWallet(w), nil
}

func (r *memWalletRepo) Credit(_ context.Context, id uuid.UUID, amount domain.Money) (*domain.Wallet, error) {
	w, ok := r.s.wallets[id]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	if err := w.Credit(amount); err != nil {
		return nil, err
	}
	return cloneWallet(w), nil
}

// --- Transações ---

type memWagerRepo struct{ s *memStore }

// cloneTx imita "gravar e ler do banco": o que fica guardado é uma
// cópia, então mudar o objeto em memória depois do Create/Update não
// altera o "banco" sem um Update explícito.
func cloneTx(t *domain.WagerTransaction) *domain.WagerTransaction {
	var resulting *domain.Money
	if m, ok := t.ResultingBalance(); ok {
		resulting = &m
	}
	return domain.RehydrateWagerTransaction(
		t.ID(), t.ExternalTransactionID(), t.ProviderID(), t.IdempotencyKey(), t.PayloadHash(),
		t.WalletID(), t.PlayerID(), t.RoundID(), t.GameID(), t.Kind(), t.Money(),
		t.ReferenceExternalTxID(), t.ResolvedReferenceID(), t.Status(), t.FailureCode(),
		resulting, t.CreatedAt(), t.UpdatedAt(),
	)
}

// Create imita os dois índices únicos da migration 000002.
func (r *memWagerRepo) Create(_ context.Context, t *domain.WagerTransaction) error {
	for _, existing := range r.s.txs {
		if existing.ProviderID() != t.ProviderID() {
			continue
		}
		if existing.IdempotencyKey() == t.IdempotencyKey() || existing.ExternalTransactionID() == t.ExternalTransactionID() {
			return domain.ErrDuplicateTransaction
		}
	}
	r.s.txs[t.ID()] = cloneTx(t)
	return nil
}

func (r *memWagerRepo) Update(_ context.Context, t *domain.WagerTransaction) error {
	r.s.txs[t.ID()] = cloneTx(t)
	return nil
}

func (r *memWagerRepo) FindByProviderAndIdempotencyKey(_ context.Context, providerID, key string) (*domain.WagerTransaction, error) {
	for _, t := range r.s.txs {
		if t.ProviderID() == providerID && t.IdempotencyKey() == key {
			return cloneTx(t), nil
		}
	}
	return nil, nil
}

func (r *memWagerRepo) FindByProviderAndExternalTxID(_ context.Context, providerID, externalID string) (*domain.WagerTransaction, error) {
	for _, t := range r.s.txs {
		if t.ProviderID() == providerID && t.ExternalTransactionID() == externalID {
			return cloneTx(t), nil
		}
	}
	return nil, nil
}

// No fake não há linhas para travar (o mutex do TxRunner já serializa).
func (r *memWagerRepo) LockByProviderAndExternalTxID(ctx context.Context, providerID, externalID string) (*domain.WagerTransaction, error) {
	return r.FindByProviderAndExternalTxID(ctx, providerID, externalID)
}

// FindByID imita a leitura por ID interno (GET /wagering/transactions/:id).
// Mesma convenção do Postgres: não achou → (nil, nil).
func (r *memWagerRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.WagerTransaction, error) {
	t, ok := r.s.txs[id]
	if !ok {
		return nil, nil
	}
	return cloneTx(t), nil
}

func (r *memWagerRepo) HasProcessedReversalOf(_ context.Context, referenceID uuid.UUID) (bool, error) {
	for _, t := range r.s.txs {
		isReversalKind := t.Kind() == domain.KindRefund || t.Kind() == domain.KindRollback
		if isReversalKind && t.Status() == domain.StatusProcessed && t.ResolvedReferenceID() == referenceID {
			return true, nil
		}
	}
	return false, nil
}

// --- Ledger ---

type memLedgerRepo struct{ s *memStore }

func (r *memLedgerRepo) Create(_ context.Context, e *domain.WalletLedgerEntry) error {
	r.s.entries = append(r.s.entries, e)
	return nil
}

// ListByWallet devolve os lançamentos da carteira na ordem de gravação.
// O fake não implementa cursor de verdade (a paginação real é coberta
// no Postgres); aqui só precisa satisfazer a interface. Se algum teste
// unitário passar a depender de paginação, implemente-a aqui.
func (r *memLedgerRepo) ListByWallet(_ context.Context, walletID uuid.UUID, _ string, limit int) ([]*domain.WalletLedgerEntry, string, error) {
	var out []*domain.WalletLedgerEntry
	for _, e := range r.s.entries {
		if e.WalletID() == walletID {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, "", nil
}

// SumByWallet: créditos - débitos, igual ao SQL da versão Postgres.
func (r *memLedgerRepo) SumByWallet(_ context.Context, walletID uuid.UUID) (int64, int, error) {
	var net int64
	count := 0
	for _, e := range r.s.entries {
		if e.WalletID() != walletID {
			continue
		}
		count++
		if e.Direction() == domain.DirectionCredit {
			net += e.Amount().Cents()
		} else {
			net -= e.Amount().Cents()
		}
	}
	return net, count, nil
}

// --- Outbox ---

type memOutboxRepo struct{ s *memStore }

func (r *memOutboxRepo) Append(_ context.Context, e *domain.OutboxEvent) error {
	r.s.events = append(r.s.events, e)
	return nil
}

// --- Inbox ---

type memInboxRepo struct{ s *memStore }

func (r *memInboxRepo) TryInsert(_ context.Context, consumerName, messageID, payloadHash string) (bool, error) {
	key := consumerName + "|" + messageID
	if _, seen := r.s.inbox[key]; seen {
		return false, nil
	}
	r.s.inbox[key] = payloadHash
	return true, nil
}

// --- Auxiliares de conferência para os testes ---

func (s *memStore) ledgerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

func (s *memStore) wallet(id uuid.UUID) *domain.Wallet {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneWallet(s.wallets[id])
}

// eventTypes devolve os tipos dos eventos gravados, na ordem em que
// foram gravados.
func (s *memStore) eventTypes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	types := make([]string, 0, len(s.events))
	for _, e := range s.events {
		types = append(types, e.Type())
	}
	return types
}

// outboxEvents devolve uma cópia da lista de eventos gravados.
func (s *memStore) outboxEvents() []*domain.OutboxEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*domain.OutboxEvent(nil), s.events...)
}
