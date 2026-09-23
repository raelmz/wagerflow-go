package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// Este arquivo concentra a regra "o que acontece com a carteira quando
// uma operação é aplicada" (tabela da seção 7 do desafio).
//
// Ele é separado do caso de uso de propósito: o caso de uso cuida de
// IDEMPOTÊNCIA (essa requisição já foi vista?), e este arquivo cuida de
// APLICAR a operação. Mais tarde, o worker de referências pendentes vai
// chamar applyWagerTransaction de novo para uma transação que ficou em
// PENDING_REFERENCE — sem duplicar nenhuma regra.
//
// Convenção importante: as funções daqui NÃO abrem transação. Elas
// recebem um UnitOfWork que já está dentro de uma. Quem chama decide
// o commit.

// applyWagerTransaction aplica a operação txn (já persistida como
// PENDING ou PENDING_REFERENCE) e persiste o resultado.
//
// O retorno de erro significa FALHA DE INFRAESTRUTURA (banco caiu etc.)
// e desfaz a transação inteira. Resultados de NEGÓCIO (rejeição,
// pendência) NÃO são erro: mudam o estado de txn, são gravados e a
// função retorna nil — assim o resultado fica registrado e um replay
// devolve a mesma resposta.
func applyWagerTransaction(ctx context.Context, uow domain.UnitOfWork, txn *domain.WagerTransaction) error {
	switch txn.Kind() {
	case domain.KindBet:
		return applyMovement(ctx, uow, txn, domain.DirectionDebit, domain.FailureInsufficientBalance)

	case domain.KindLoss:
		return applyLoss(ctx, uow, txn)

	case domain.KindWin:
		// WIN pode informar uma aposta como referência, mas não é obrigatório.
		if txn.ReferenceExternalTxID() == "" {
			return applyMovement(ctx, uow, txn, domain.DirectionCredit, "")
		}
		return applyWithReference(ctx, uow, txn)

	case domain.KindRefund, domain.KindRollback:
		return applyWithReference(ctx, uow, txn)

	default:
		// OPENING nunca chega aqui (o construtor externo o rejeita).
		return fmt.Errorf("tipo de operação sem regra de aplicação: %s", txn.Kind())
	}
}

// applyMovement move dinheiro na carteira, grava o lançamento no ledger
// e conclui a transação. Se for um débito sem saldo, rejeita com
// insufficientCode em vez de falhar.
func applyMovement(
	ctx context.Context,
	uow domain.UnitOfWork,
	txn *domain.WagerTransaction,
	direction domain.LedgerDirection,
	insufficientCode string,
) error {
	var (
		wallet *domain.Wallet
		err    error
	)

	// A garantia contra saldo negativo e contra "lost update" vem do
	// UPDATE atômico condicionado dentro do repositório (decisão
	// registrada em docs/PROJETO.md) — aqui só chamamos.
	if direction == domain.DirectionDebit {
		wallet, err = uow.Wallets().Debit(ctx, txn.WalletID(), txn.Money())
		if errors.Is(err, domain.ErrInsufficientBalance) {
			// Saldo insuficiente é resultado de NEGÓCIO: registramos a
			// rejeição e seguimos para o commit (não devolvemos erro).
			return reject(ctx, uow, txn, insufficientCode)
		}
	} else {
		wallet, err = uow.Wallets().Credit(ctx, txn.WalletID(), txn.Money())
	}
	if err != nil {
		return err
	}

	// O repositório devolve a carteira JÁ com o saldo novo. O saldo
	// anterior é reconstruído desfazendo a operação — assim ele nunca
	// depende de uma leitura separada (que poderia estar desatualizada
	// numa disputa entre escritores).
	after := wallet.Balance()
	var before domain.Money
	if direction == domain.DirectionDebit {
		before, err = after.Add(txn.Money())
	} else {
		before, err = after.Subtract(txn.Money())
	}
	if err != nil {
		return err
	}

	// O construtor do lançamento confere balanceAfter = balanceBefore ± valor.
	entry, err := domain.NewWalletLedgerEntry(txn.WalletID(), txn.ID(), direction, txn.Money(), before, after)
	if err != nil {
		return err
	}
	if err := uow.LedgerEntries().Create(ctx, entry); err != nil {
		return err
	}

	if err := txn.MarkProcessed(after); err != nil {
		return err
	}
	return uow.WagerTransactions().Update(ctx, txn)
}

// applyLoss conclui uma LOSS: sem movimentação, sem ledger e sem mexer
// na versão da carteira (seção 7). Só registra o saldo observado.
func applyLoss(ctx context.Context, uow domain.UnitOfWork, txn *domain.WagerTransaction) error {
	wallet, err := uow.Wallets().FindByID(ctx, txn.WalletID())
	if err != nil {
		return err
	}
	if wallet == nil {
		return domain.ErrWalletNotFound
	}
	if err := txn.MarkProcessed(wallet.Balance()); err != nil {
		return err
	}
	return uow.WagerTransactions().Update(ctx, txn)
}

// applyWithReference cobre REFUND, ROLLBACK e WIN com referência.
// Primeiro resolve a referência; só se ela estiver em ordem executa
// a movimentação.
func applyWithReference(ctx context.Context, uow domain.UnitOfWork, txn *domain.WagerTransaction) error {
	// LOCK na linha da referência: duas reversões simultâneas da mesma
	// aposta esperam uma pela outra aqui. A segunda só continua depois
	// do commit da primeira e, por isso, enxerga que já houve reversão.
	ref, err := uow.WagerTransactions().LockByProviderAndExternalTxID(ctx, txn.ProviderID(), txn.ReferenceExternalTxID())
	if err != nil {
		return err
	}

	// Referência ainda não chegou: fica PENDING_REFERENCE e o worker
	// tenta de novo depois (seção 7, "Referências ainda indisponíveis").
	if ref == nil {
		return markPendingReference(ctx, uow, txn)
	}

	// Guardamos o id interno da referência para auditoria, qualquer que
	// seja o resultado a seguir.
	if err := txn.ResolveReference(ref.ID()); err != nil {
		return err
	}

	switch ref.Status() {
	case domain.StatusPending, domain.StatusPendingReference:
		// A referência existe, mas ainda não terminou: esperar pode
		// resolver. Mesmo tratamento de "ainda não chegou".
		return markPendingReference(ctx, uow, txn)

	case domain.StatusRejected, domain.StatusFailed:
		// A referência terminou SEM sucesso: esperar não adianta, ela
		// nunca vai virar PROCESSED. Rejeição definitiva.
		return reject(ctx, uow, txn, domain.FailureReferenceNotProcessed)
	}

	// Daqui em diante a referência está PROCESSED.
	if code := checkReferenceRules(txn, ref); code != "" {
		return reject(ctx, uow, txn, code)
	}

	if isReversal(txn.Kind()) {
		already, err := uow.WagerTransactions().HasProcessedReversalOf(ctx, ref.ID())
		if err != nil {
			return err
		}
		if already {
			return reject(ctx, uow, txn, domain.FailureReferenceAlreadyReversed)
		}
	}

	direction := movementDirection(txn.Kind(), ref.Kind())

	// Reversão que precisa DEBITAR sem saldo usa um código próprio,
	// diferente do de "aposta sem saldo" (exigência da seção 7).
	return applyMovement(ctx, uow, txn, direction, domain.FailureReversalInsufficientBalance)
}

// checkReferenceRules confere se a operação e a referência "concordam"
// (seção 7). Devolve o failureCode da primeira regra violada, ou "" se
// tudo estiver certo.
func checkReferenceRules(txn, ref *domain.WagerTransaction) string {
	// Mesmo provedor (já garantido pela busca), jogador, carteira,
	// moeda e rodada.
	if ref.PlayerID() != txn.PlayerID() ||
		ref.WalletID() != txn.WalletID() ||
		ref.RoundID() != txn.RoundID() ||
		ref.Money().Currency() != txn.Money().Currency() {
		return domain.FailureReferenceMismatch
	}

	// Que tipo de operação pode apontar para que tipo de referência:
	//   WIN      -> BET
	//   REFUND   -> BET
	//   ROLLBACK -> BET, WIN ou REFUND
	switch txn.Kind() {
	case domain.KindWin, domain.KindRefund:
		if ref.Kind() != domain.KindBet {
			return domain.FailureReferenceInvalidKind
		}
	case domain.KindRollback:
		if ref.Kind() != domain.KindBet && ref.Kind() != domain.KindWin && ref.Kind() != domain.KindRefund {
			return domain.FailureReferenceInvalidKind
		}
	}

	// Reversão devolve INTEGRALMENTE: o valor tem que ser idêntico.
	// (WIN com referência pode ter qualquer valor positivo: o prêmio
	// não precisa ser igual à aposta.)
	if isReversal(txn.Kind()) && !txn.Money().Equals(ref.Money()) {
		return domain.FailureReversalAmountMismatch
	}

	return ""
}

// movementDirection decide se a operação credita ou debita a carteira.
//
//	WIN, REFUND                 -> crédito
//	ROLLBACK de uma BET         -> crédito (devolve o débito da aposta)
//	ROLLBACK de uma WIN/REFUND  -> débito (desfaz o crédito que foi dado)
func movementDirection(kind, refKind domain.WagerKind) domain.LedgerDirection {
	if kind == domain.KindRollback && refKind != domain.KindBet {
		return domain.DirectionDebit
	}
	return domain.DirectionCredit
}

func isReversal(kind domain.WagerKind) bool {
	return kind == domain.KindRefund || kind == domain.KindRollback
}

func reject(ctx context.Context, uow domain.UnitOfWork, txn *domain.WagerTransaction, failureCode string) error {
	if err := txn.MarkRejected(failureCode); err != nil {
		return err
	}
	return uow.WagerTransactions().Update(ctx, txn)
}

func markPendingReference(ctx context.Context, uow domain.UnitOfWork, txn *domain.WagerTransaction) error {
	if err := txn.MarkPendingReference(); err != nil {
		return err
	}
	return uow.WagerTransactions().Update(ctx, txn)
}
