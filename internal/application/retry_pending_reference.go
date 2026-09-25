package application

import (
	"context"
	"log"
	"time"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// RetryPendingReferenceUseCase é o worker de referências pendentes da
// seção 7 do desafio ("referências ainda indisponíveis"): reivindica
// um lote de WagerTransaction em PENDING_REFERENCE e tenta reaplicá-las
// reaproveitando applyWagerTransaction — o MESMO código que HTTP, SQS
// e a primeira tentativa já usam, sem duplicar nenhuma regra.
//
// Cada candidata tem um prazo próprio: se `maxAttempts` tentativas ou
// `ttl` de espera (desde a PRIMEIRA vez que entrou em
// PENDING_REFERENCE) se esgotarem antes da referência aparecer, o
// worker desiste e rejeita definitivamente com
// domain.FailureReferenceNotFound — sem nem tentar reaplicar de novo.
type RetryPendingReferenceUseCase struct {
	repo        domain.PendingReferenceRepository
	txRunner    domain.TxRunner
	workerID    string
	batchSize   int
	lockTimeout time.Duration
	backoff     BackoffFunc
	maxAttempts int
	ttl         time.Duration
}

func NewRetryPendingReferenceUseCase(
	repo domain.PendingReferenceRepository,
	txRunner domain.TxRunner,
	workerID string,
	batchSize int,
	lockTimeout time.Duration,
	backoff BackoffFunc,
	maxAttempts int,
	ttl time.Duration,
) *RetryPendingReferenceUseCase {
	return &RetryPendingReferenceUseCase{
		repo:        repo,
		txRunner:    txRunner,
		workerID:    workerID,
		batchSize:   batchSize,
		lockTimeout: lockTimeout,
		backoff:     backoff,
		maxAttempts: maxAttempts,
		ttl:         ttl,
	}
}

// RunOnce reivindica e processa UM lote. Devolve quantas transações
// foram REIVINDICADAS (mesma convenção de
// PublishPendingOutboxEventsUseCase.RunOnce) — sucesso/desistência de
// cada uma já fica registrado no próprio banco.
func (uc *RetryPendingReferenceUseCase) RunOnce(ctx context.Context) (int, error) {
	candidates, err := uc.repo.Claim(ctx, uc.workerID, uc.batchSize, uc.lockTimeout)
	if err != nil {
		return 0, err
	}

	for _, candidate := range candidates {
		uc.processOne(ctx, candidate)
	}

	return len(candidates), nil
}

func (uc *RetryPendingReferenceUseCase) processOne(ctx context.Context, candidate domain.PendingReferenceCandidate) {
	giveUp := candidate.Attempts >= uc.maxAttempts || time.Since(candidate.FirstPendingAt) >= uc.ttl

	var stillPending bool

	err := uc.txRunner.WithinTransaction(ctx, func(uow domain.UnitOfWork) error {
		txn, err := uow.WagerTransactions().FindByID(ctx, candidate.ID)
		if err != nil {
			return err
		}
		if txn == nil || txn.Status() != domain.StatusPendingReference {
			// Já foi resolvida por outro caminho (ex: replay HTTP
			// concorrente) entre o Claim e agora — nada a fazer.
			return nil
		}

		if giveUp {
			// Desiste sem tentar de novo: reaproveita a MESMA função
			// de rejeição que qualquer outra regra de negócio usa,
			// então o evento Rejected e o failureCode ficam
			// idênticos a qualquer outra rejeição definitiva.
			return reject(ctx, uow, txn, domain.FailureReferenceNotFound)
		}

		if err := applyWagerTransaction(ctx, uow, txn); err != nil {
			return err
		}
		stillPending = txn.Status() == domain.StatusPendingReference
		return nil
	})
	if err != nil {
		log.Printf("pending-reference-worker: falha ao reaplicar transação %s (será tentada de novo após lockTimeout): %v", candidate.ID, err)
		return
	}

	if !stillPending {
		// Resolvida (PROCESSED/REJECTED, pela desistência acima ou
		// porque a referência finalmente apareceu): não há mais nada
		// a agendar, só liberar o lock.
		if err := uc.repo.ReleaseLock(ctx, candidate.ID, uc.workerID); err != nil {
			log.Printf("pending-reference-worker: falha ao liberar lock da transação %s: %v", candidate.ID, err)
		}
		return
	}

	// Ainda PENDING_REFERENCE: agenda a próxima tentativa.
	attempts := candidate.Attempts + 1
	delay := uc.backoff(attempts)
	if err := uc.repo.MarkRetryScheduled(ctx, candidate.ID, uc.workerID, attempts, time.Now().Add(delay)); err != nil {
		log.Printf("pending-reference-worker: falha ao agendar nova tentativa da transação %s (próxima tentativa pode demorar mais que o esperado): %v", candidate.ID, err)
	}
}

// Run mantém o loop de polling até o contexto ser cancelado. Mesmo
// raciocínio de intervalo fixo do worker publicador (ver
// PublishPendingOutboxEventsUseCase.Run): se sobrar trabalho, a
// resposta é subir mais uma instância, não acelerar sozinho.
func (uc *RetryPendingReferenceUseCase) Run(ctx context.Context, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := uc.RunOnce(ctx); err != nil {
				log.Printf("pending-reference-worker: falha ao reivindicar lote de referências pendentes: %v", err)
			}
		}
	}
}
