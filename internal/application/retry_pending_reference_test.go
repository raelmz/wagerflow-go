package application

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// memPendingReferenceRepo é um fake em memória de
// domain.PendingReferenceRepository, sobre o MESMO memStore usado por
// env (fakes_test.go) — mesmos limites conscientes descritos lá (sem
// concorrência real; isso fica para o teste de integração contra
// Postgres, a escrever à parte).
type memPendingReferenceRepo struct {
	mu    sync.Mutex
	store *memStore

	// bookkeeping por transação — o equivalente em memória das
	// colunas reference_attempts/reference_first_pending_at/
	// reference_next_retry_at/locked_by da migration 000007.
	state map[uuid.UUID]*pendingRefState
}

type pendingRefState struct {
	attempts       int
	firstPendingAt time.Time
	nextRetryAt    time.Time
	lockedBy       string
}

func newMemPendingReferenceRepo(store *memStore) *memPendingReferenceRepo {
	return &memPendingReferenceRepo{store: store, state: map[uuid.UUID]*pendingRefState{}}
}

func (r *memPendingReferenceRepo) Claim(_ context.Context, workerID string, limit int, lockTimeout time.Duration) ([]domain.PendingReferenceCandidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	var out []domain.PendingReferenceCandidate
	for id, txn := range r.store.txs {
		if txn.Status() != domain.StatusPendingReference {
			continue
		}
		st, ok := r.state[id]
		if !ok {
			st = &pendingRefState{firstPendingAt: now, nextRetryAt: now}
			r.state[id] = st
		}
		if st.lockedBy != "" {
			continue // simplificado: sem simulação de lock abandonado/lockTimeout no fake
		}
		if st.nextRetryAt.After(now) {
			continue
		}
		st.lockedBy = workerID
		out = append(out, domain.PendingReferenceCandidate{ID: id, Attempts: st.attempts, FirstPendingAt: st.firstPendingAt})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *memPendingReferenceRepo) MarkRetryScheduled(_ context.Context, id uuid.UUID, workerID string, attempts int, nextRetryAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.state[id]
	if !ok || st.lockedBy != workerID {
		return nil
	}
	st.attempts = attempts
	st.nextRetryAt = nextRetryAt
	st.lockedBy = ""
	return nil
}

func (r *memPendingReferenceRepo) ReleaseLock(_ context.Context, id uuid.UUID, workerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if st, ok := r.state[id]; ok && st.lockedBy == workerID {
		st.lockedBy = ""
	}
	return nil
}

// noBackoff facilita asserções: sempre agenda "já", em vez de esperar
// um tempo real de verdade durante o teste.
func noBackoff(int) time.Duration { return 0 }

func findPendingID(t *testing.T, e *env, providerID, externalID string) uuid.UUID {
	t.Helper()
	var found *domain.WagerTransaction
	err := e.store.WithinTransaction(context.Background(), func(uow domain.UnitOfWork) error {
		txn, err := uow.WagerTransactions().FindByProviderAndExternalTxID(context.Background(), providerID, externalID)
		found = txn
		return err
	})
	if err != nil || found == nil {
		t.Fatalf("transação pendente não encontrada: %v", err)
	}
	return found.ID()
}

func TestPendingReferenceWorker_TentaDeNovoEAgendaBackoff(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1")) // referência ainda não existe

	repo := newMemPendingReferenceRepo(e.store)
	uc := NewRetryPendingReferenceUseCase(repo, e.store, "worker-1", 10, time.Minute, noBackoff, 5, time.Hour)

	claimed, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce falhou: %v", err)
	}
	if claimed != 1 {
		t.Fatalf("esperava reivindicar 1 transação, veio %d", claimed)
	}

	id := findPendingID(t, e, "provider-a", "refund-1")
	st := repo.state[id]
	if st.attempts != 1 {
		t.Errorf("esperava attempts=1 após uma tentativa sem sucesso, veio %d", st.attempts)
	}
	if st.lockedBy != "" {
		t.Error("esperava lock liberado depois da rodada")
	}
	expectBalance(t, e, "100.00") // ainda pendente, saldo intocado
}

func TestPendingReferenceWorker_ReferenciaApareceResolveEDestrava(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1"))
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00")) // a referência chega

	repo := newMemPendingReferenceRepo(e.store)
	uc := NewRetryPendingReferenceUseCase(repo, e.store, "worker-1", 10, time.Minute, noBackoff, 5, time.Hour)

	if _, err := uc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce falhou: %v", err)
	}

	expectBalance(t, e, "100.00") // REFUND devolve os 25.00 debitados pela BET
	id := findPendingID(t, e, "provider-a", "refund-1")
	txn := e.store.txs[id]
	if txn.Status() != domain.StatusProcessed {
		t.Fatalf("esperava PROCESSED depois da referência aparecer, veio %s", txn.Status())
	}
	if st := repo.state[id]; st.lockedBy != "" {
		t.Error("esperava lock liberado após resolver")
	}
}

func TestPendingReferenceWorker_DesisteAposMaxAttempts(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1"))

	repo := newMemPendingReferenceRepo(e.store)
	id := findPendingID(t, e, "provider-a", "refund-1")
	// Simula 5 tentativas já esgotadas.
	repo.state[id] = &pendingRefState{attempts: 5, firstPendingAt: time.Now(), nextRetryAt: time.Now()}

	uc := NewRetryPendingReferenceUseCase(repo, e.store, "worker-1", 10, time.Minute, noBackoff, 5, time.Hour)
	if _, err := uc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce falhou: %v", err)
	}

	txn := e.store.txs[id]
	if txn.Status() != domain.StatusRejected || txn.FailureCode() != domain.FailureReferenceNotFound {
		t.Fatalf("esperava REJECTED/REFERENCE_NOT_FOUND, veio %s/%s", txn.Status(), txn.FailureCode())
	}
}

func TestPendingReferenceWorker_DesisteAposTTL(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1"))

	repo := newMemPendingReferenceRepo(e.store)
	id := findPendingID(t, e, "provider-a", "refund-1")
	// Poucas tentativas, mas pendente há muito mais tempo que o TTL.
	repo.state[id] = &pendingRefState{attempts: 1, firstPendingAt: time.Now().Add(-time.Hour), nextRetryAt: time.Now()}

	uc := NewRetryPendingReferenceUseCase(repo, e.store, "worker-1", 10, time.Minute, noBackoff, 5, 5*time.Minute)
	if _, err := uc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce falhou: %v", err)
	}

	txn := e.store.txs[id]
	if txn.Status() != domain.StatusRejected || txn.FailureCode() != domain.FailureReferenceNotFound {
		t.Fatalf("esperava REJECTED/REFERENCE_NOT_FOUND por TTL, veio %s/%s", txn.Status(), txn.FailureCode())
	}
}
