package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// fakeOutboxPublisherRepo simula domain.OutboxPublisherRepository em
// memória. NÃO simula a disputa real entre publishers (isso exige
// Postgres de verdade com FOR UPDATE SKIP LOCKED — fica para um teste
// de integração futuro, ver limitação registrada em docs/PROJETO.md);
// aqui o foco é só a lógica do caso de uso: o que fazer com o que o
// repositório devolve.
type fakeOutboxPublisherRepo struct {
	toClaim   []domain.PendingOutboxEvent
	published []uuid.UUID
	failed    []uuid.UUID
}

func (f *fakeOutboxPublisherRepo) Claim(_ context.Context, _ string, limit int, _ time.Duration) ([]domain.PendingOutboxEvent, error) {
	if len(f.toClaim) == 0 {
		return nil, nil
	}
	n := limit
	if n > len(f.toClaim) {
		n = len(f.toClaim)
	}
	claimed := f.toClaim[:n]
	f.toClaim = f.toClaim[n:]
	return claimed, nil
}

func (f *fakeOutboxPublisherRepo) MarkPublished(_ context.Context, id uuid.UUID, _ string) error {
	f.published = append(f.published, id)
	return nil
}

func (f *fakeOutboxPublisherRepo) MarkFailed(_ context.Context, id uuid.UUID, _ string, _ time.Time) error {
	f.failed = append(f.failed, id)
	return nil
}

// fakeEventPublisher simula domain.EventPublisher. failFor é indexado
// pelo EventID (via DeduplicationID) para escolher quais mensagens
// devem falhar.
type fakeEventPublisher struct {
	failFor map[string]bool
	sent    []domain.OutboxMessage
}

func (f *fakeEventPublisher) Publish(_ context.Context, msg domain.OutboxMessage) error {
	f.sent = append(f.sent, msg)
	if f.failFor[msg.DeduplicationID] {
		return errors.New("falha simulada de envio")
	}
	return nil
}

func TestPublishPendingOutboxEventsUseCase_PublicaComSucesso(t *testing.T) {
	eventID := uuid.New()
	aggID := uuid.New()
	repo := &fakeOutboxPublisherRepo{toClaim: []domain.PendingOutboxEvent{
		{ID: eventID, AggregateID: aggID, EventType: "WagerTransactionProcessed", Payload: []byte(`{}`)},
	}}
	pub := &fakeEventPublisher{}

	uc := NewPublishPendingOutboxEventsUseCase(repo, pub, "worker-1", 10, time.Minute, ExponentialBackoff(time.Minute))

	claimed, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if claimed != 1 {
		t.Fatalf("esperava 1 evento reivindicado, veio %d", claimed)
	}
	if len(repo.published) != 1 || repo.published[0] != eventID {
		t.Fatalf("esperava MarkPublished chamado para %s, veio %v", eventID, repo.published)
	}
	if len(repo.failed) != 0 {
		t.Fatalf("não esperava nenhuma falha registrada, veio %v", repo.failed)
	}
	if len(pub.sent) != 1 {
		t.Fatalf("esperava 1 mensagem enviada, veio %d", len(pub.sent))
	}
	sent := pub.sent[0]
	if sent.DeduplicationID != eventID.String() {
		t.Errorf("DeduplicationID = %q, esperava %q (eventId)", sent.DeduplicationID, eventID.String())
	}
	if sent.AggregateID != aggID {
		t.Errorf("AggregateID = %s, esperava %s (vira o MessageGroupId)", sent.AggregateID, aggID)
	}
}

func TestPublishPendingOutboxEventsUseCase_RegistraFalhaComBackoff(t *testing.T) {
	eventID := uuid.New()
	repo := &fakeOutboxPublisherRepo{toClaim: []domain.PendingOutboxEvent{
		{ID: eventID, AggregateID: uuid.New(), EventType: "WagerTransactionRejected", Payload: []byte(`{}`), Attempts: 2},
	}}
	pub := &fakeEventPublisher{failFor: map[string]bool{eventID.String(): true}}

	uc := NewPublishPendingOutboxEventsUseCase(repo, pub, "worker-1", 10, time.Minute, ExponentialBackoff(time.Minute))

	if _, err := uc.RunOnce(context.Background()); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	if len(repo.published) != 0 {
		t.Fatalf("não esperava publicação bem-sucedida, veio %v", repo.published)
	}
	if len(repo.failed) != 1 || repo.failed[0] != eventID {
		t.Fatalf("esperava MarkFailed chamado para %s, veio %v", eventID, repo.failed)
	}
}

func TestPublishPendingOutboxEventsUseCase_LoteComSucessoEFalhaMisturados(t *testing.T) {
	okID, failID := uuid.New(), uuid.New()
	repo := &fakeOutboxPublisherRepo{toClaim: []domain.PendingOutboxEvent{
		{ID: okID, AggregateID: uuid.New(), EventType: "WalletBalanceChanged", Payload: []byte(`{}`)},
		{ID: failID, AggregateID: uuid.New(), EventType: "WalletBalanceChanged", Payload: []byte(`{}`)},
	}}
	pub := &fakeEventPublisher{failFor: map[string]bool{failID.String(): true}}

	uc := NewPublishPendingOutboxEventsUseCase(repo, pub, "worker-1", 10, time.Minute, ExponentialBackoff(time.Minute))

	claimed, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if claimed != 2 {
		t.Fatalf("esperava 2 eventos reivindicados, veio %d", claimed)
	}
	// A falha de um evento não pode impedir a tentativa dos demais no
	// mesmo lote.
	if len(repo.published) != 1 || repo.published[0] != okID {
		t.Fatalf("esperava só %s publicado, veio %v", okID, repo.published)
	}
	if len(repo.failed) != 1 || repo.failed[0] != failID {
		t.Fatalf("esperava só %s com falha registrada, veio %v", failID, repo.failed)
	}
}

func TestExponentialBackoff_CresceECapaNoLimite(t *testing.T) {
	backoff := ExponentialBackoff(10 * time.Second)

	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 10 * time.Second}, // 16s estouraria o cap de 10s
		{10, 10 * time.Second},
	}

	for _, c := range cases {
		got := backoff(c.attempts)
		if got != c.want {
			t.Errorf("backoff(%d) = %s, esperava %s", c.attempts, got, c.want)
		}
	}
}
