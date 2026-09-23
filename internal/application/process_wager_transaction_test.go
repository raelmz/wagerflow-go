package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/domain"
)

// ---------------------------------------------------------------------
// Infraestrutura dos testes
// ---------------------------------------------------------------------

// env agrupa o cenário-padrão: uma carteira BRL com saldo inicial
// conhecido, e o caso de uso ligado ao "banco" em memória.
type env struct {
	store    *memStore
	uc       *ProcessWagerTransactionUseCase
	walletID uuid.UUID
	playerID uuid.UUID
}

func mustMoney(t *testing.T, amount string) domain.Money {
	t.Helper()
	m, err := domain.NewMoneyFromString(amount, "BRL")
	if err != nil {
		t.Fatalf("dinheiro inválido no teste (%s): %v", amount, err)
	}
	return m
}

func newEnv(t *testing.T, initialBalance string) *env {
	t.Helper()
	store := newMemStore()
	playerID := uuid.New()
	wallet, err := domain.NewWallet(playerID, mustMoney(t, initialBalance))
	if err != nil {
		t.Fatalf("erro criando carteira do teste: %v", err)
	}
	store.wallets[wallet.ID()] = wallet
	return &env{
		store:    store,
		uc:       NewProcessWagerTransactionUseCase(store),
		walletID: wallet.ID(),
		playerID: playerID,
	}
}

// cmd monta um comando válido do provedor "provider-a", rodada "round-1".
// A chave de idempotência segue a convenção {providerId}:{externalId}.
func (e *env) cmd(kind domain.WagerKind, externalID, amount string) ProcessWagerCommand {
	return ProcessWagerCommand{
		IdempotencyKey:        "provider-a:" + externalID,
		ProviderID:            "provider-a",
		ExternalTransactionID: externalID,
		PlayerID:              e.playerID,
		WalletID:              e.walletID,
		RoundID:               "round-1",
		GameID:                "fortune-chimp",
		Kind:                  kind,
		Amount:                amount,
		Currency:              "BRL",
	}
}

func (e *env) reversal(kind domain.WagerKind, externalID, amount, referenceID string) ProcessWagerCommand {
	c := e.cmd(kind, externalID, amount)
	c.ReferenceExternalTransactionID = referenceID
	return c
}

// run executa e falha o teste se der erro (para cenários que esperam sucesso).
func (e *env) run(t *testing.T, cmd ProcessWagerCommand) *ProcessWagerResult {
	t.Helper()
	res, err := e.uc.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("não esperava erro em %s %s: %v", cmd.Kind, cmd.ExternalTransactionID, err)
	}
	return res
}

func (e *env) balance() string { return e.store.wallet(e.walletID).Balance().String() }

func expectStatus(t *testing.T, res *ProcessWagerResult, status domain.WagerStatus, failureCode string) {
	t.Helper()
	got := res.Transaction
	if got.Status() != status {
		t.Fatalf("esperava status %s, veio %s (failureCode=%q)", status, got.Status(), got.FailureCode())
	}
	if got.FailureCode() != failureCode {
		t.Fatalf("esperava failureCode %q, veio %q", failureCode, got.FailureCode())
	}
}

func expectBalance(t *testing.T, e *env, want string) {
	t.Helper()
	if got := e.balance(); got != want {
		t.Fatalf("esperava saldo %s, veio %s", want, got)
	}
}

// ---------------------------------------------------------------------
// BET / WIN / LOSS
// ---------------------------------------------------------------------

func TestBet_Processada(t *testing.T) {
	e := newEnv(t, "100.00")

	res := e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	expectStatus(t, res, domain.StatusProcessed, "")
	if res.Replay {
		t.Error("primeira execução não pode ser replay")
	}
	if got, _ := res.Transaction.ResultingBalance(); got.String() != "75.00" {
		t.Errorf("saldo resultante esperado 75.00, veio %s", got)
	}
	expectBalance(t, e, "75.00")
	if e.store.ledgerCount() != 1 {
		t.Errorf("esperava 1 lançamento no ledger, veio %d", e.store.ledgerCount())
	}
	if v := e.store.wallet(e.walletID).Version(); v != 2 {
		t.Errorf("versão da carteira deveria ir de 1 para 2, veio %d", v)
	}
}

func TestBet_SaldoInsuficienteViraRejeicaoAuditavel(t *testing.T) {
	e := newEnv(t, "100.00")

	res := e.run(t, e.cmd(domain.KindBet, "bet-1", "150.00"))

	expectStatus(t, res, domain.StatusRejected, domain.FailureInsufficientBalance)
	expectBalance(t, e, "100.00")
	if e.store.ledgerCount() != 0 {
		t.Error("operação rejeitada não pode gerar lançamento")
	}
	if v := e.store.wallet(e.walletID).Version(); v != 1 {
		t.Errorf("rejeição não pode mudar a versão da carteira, veio %d", v)
	}
	// A rejeição foi PERSISTIDA (auditável): existe no "banco".
	if len(e.store.txs) != 1 {
		t.Errorf("a rejeição deveria estar gravada; transações no banco: %d", len(e.store.txs))
	}
}

func TestRejeicaoTambemEhIdempotente(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "150.00"))

	// Depois entra dinheiro: um replay NÃO pode "virar" processado.
	e.run(t, e.cmd(domain.KindWin, "win-1", "500.00"))

	res := e.run(t, e.cmd(domain.KindBet, "bet-1", "150.00"))
	if !res.Replay {
		t.Fatal("esperava replay")
	}
	expectStatus(t, res, domain.StatusRejected, domain.FailureInsufficientBalance)
	expectBalance(t, e, "600.00")
}

func TestWin_CreditaCarteira(t *testing.T) {
	e := newEnv(t, "100.00")

	res := e.run(t, e.cmd(domain.KindWin, "win-1", "50.00"))

	expectStatus(t, res, domain.StatusProcessed, "")
	expectBalance(t, e, "150.00")
}

func TestWin_ComReferenciaParaBetDaMesmaRodada(t *testing.T) {
	e := newEnv(t, "100.00")
	bet := e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	// O prêmio NÃO precisa ter o mesmo valor da aposta.
	res := e.run(t, e.reversal(domain.KindWin, "win-1", "80.00", "bet-1"))

	expectStatus(t, res, domain.StatusProcessed, "")
	if res.Transaction.ResolvedReferenceID() != bet.Transaction.ID() {
		t.Error("a referência interna resolvida deveria ser a BET")
	}
	expectBalance(t, e, "155.00")
}

func TestLoss_SemMovimentacaoSemLedgerSemMudarVersao(t *testing.T) {
	e := newEnv(t, "100.00")

	res := e.run(t, e.cmd(domain.KindLoss, "loss-1", "0.00"))

	expectStatus(t, res, domain.StatusProcessed, "")
	expectBalance(t, e, "100.00")
	if e.store.ledgerCount() != 0 {
		t.Error("LOSS não pode criar lançamento")
	}
	if v := e.store.wallet(e.walletID).Version(); v != 1 {
		t.Errorf("LOSS não pode alterar a versão, veio %d", v)
	}
	if got, ok := res.Transaction.ResultingBalance(); !ok || got.String() != "100.00" {
		t.Errorf("LOSS deveria registrar o saldo observado 100.00, veio %v (ok=%v)", got, ok)
	}
}

// ---------------------------------------------------------------------
// Idempotência
// ---------------------------------------------------------------------

func TestReplay_DevolveSaldoDoProcessamentoOriginal(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00")) // saldo 75.00
	e.run(t, e.cmd(domain.KindBet, "bet-2", "10.00")) // saldo 65.00

	// Replay da primeira: precisa devolver 75.00 (o saldo daquela
	// época), NÃO os 65.00 atuais — exigência da seção 9.
	res := e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	if !res.Replay {
		t.Fatal("esperava idempotentReplay = true")
	}
	if got, _ := res.Transaction.ResultingBalance(); got.String() != "75.00" {
		t.Errorf("replay deveria devolver 75.00, veio %s", got)
	}
	expectBalance(t, e, "65.00")
	if e.store.ledgerCount() != 2 {
		t.Errorf("replay não pode criar lançamento; ledger tem %d (esperado 2)", e.store.ledgerCount())
	}
}

func TestReplay_ChaveIgualConteudoDiferenteEhConflito(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	diferente := e.cmd(domain.KindBet, "bet-1", "30.00") // mesma chave e mesmo externalId, valor outro
	_, err := e.uc.Execute(context.Background(), diferente)

	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("esperava ErrIdempotencyConflict, veio %v", err)
	}
	expectBalance(t, e, "75.00")
}

func TestMesmaOperacaoComOutraChaveEhConflito(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	outraChave := e.cmd(domain.KindBet, "bet-1", "25.00")
	outraChave.IdempotencyKey = "chave-totalmente-diferente"
	_, err := e.uc.Execute(context.Background(), outraChave)

	if !errors.Is(err, domain.ErrExternalTransactionConflict) {
		t.Fatalf("esperava ErrExternalTransactionConflict, veio %v", err)
	}
	expectBalance(t, e, "75.00") // não debitou duas vezes
}

func TestIsolamentoEntreProvedores(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	// Outro provedor usando EXATAMENTE a mesma chave e o mesmo id
	// externo: são operações independentes, não há colisão nem replay.
	outro := e.cmd(domain.KindBet, "bet-1", "25.00")
	outro.ProviderID = "provider-b"
	res := e.run(t, outro)

	if res.Replay {
		t.Error("operação de outro provedor não pode ser tratada como replay")
	}
	expectStatus(t, res, domain.StatusProcessed, "")
	expectBalance(t, e, "50.00")
}

// ---------------------------------------------------------------------
// Reversões
// ---------------------------------------------------------------------

func TestRefund_DevolveOValorDaBet(t *testing.T) {
	e := newEnv(t, "100.00")
	bet := e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	res := e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1"))

	expectStatus(t, res, domain.StatusProcessed, "")
	expectBalance(t, e, "100.00")
	if res.Transaction.ResolvedReferenceID() != bet.Transaction.ID() {
		t.Error("REFUND deveria guardar o id interno da BET referenciada")
	}
	if e.store.ledgerCount() != 2 {
		t.Errorf("esperava 2 lançamentos (débito da BET + crédito do REFUND), veio %d", e.store.ledgerCount())
	}
}

func TestReversao_SegundoRefundDaMesmaBetEhRejeitado(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))
	e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1"))

	// Outro REFUND (outro id e outra chave) para a MESMA aposta.
	res := e.run(t, e.reversal(domain.KindRefund, "refund-2", "25.00", "bet-1"))

	expectStatus(t, res, domain.StatusRejected, domain.FailureReferenceAlreadyReversed)
	expectBalance(t, e, "100.00") // não devolveu duas vezes
}

func TestReversao_RollbackDepoisDeRefundDaMesmaBetEhRejeitado(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))
	e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1"))

	// REFUND + ROLLBACK sobre a mesma aposta devolveriam o débito duas vezes.
	res := e.run(t, e.reversal(domain.KindRollback, "rollback-1", "25.00", "bet-1"))

	expectStatus(t, res, domain.StatusRejected, domain.FailureReferenceAlreadyReversed)
	expectBalance(t, e, "100.00")
}

func TestRollback_DeBetCredita(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	res := e.run(t, e.reversal(domain.KindRollback, "rollback-1", "25.00", "bet-1"))

	expectStatus(t, res, domain.StatusProcessed, "")
	expectBalance(t, e, "100.00")
}

func TestRollback_DeWinDebita(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindWin, "win-1", "50.00")) // 150.00

	res := e.run(t, e.reversal(domain.KindRollback, "rollback-1", "50.00", "win-1"))

	expectStatus(t, res, domain.StatusProcessed, "")
	expectBalance(t, e, "100.00")
}

func TestRollback_DeRefundDebita(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))                     // 75.00
	e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1")) // 100.00

	res := e.run(t, e.reversal(domain.KindRollback, "rollback-1", "25.00", "refund-1"))

	expectStatus(t, res, domain.StatusProcessed, "")
	expectBalance(t, e, "75.00")
}

func TestReversao_SemSaldoUsaCodigoDiferenteDeBetSemSaldo(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindWin, "win-1", "50.00"))  // 150.00
	e.run(t, e.cmd(domain.KindBet, "bet-1", "150.00")) // 0.00: o prêmio foi gasto

	// Desfazer o WIN exigiria debitar 50.00 de uma carteira zerada.
	res := e.run(t, e.reversal(domain.KindRollback, "rollback-1", "50.00", "win-1"))

	expectStatus(t, res, domain.StatusRejected, domain.FailureReversalInsufficientBalance)
	if domain.FailureReversalInsufficientBalance == domain.FailureInsufficientBalance {
		t.Fatal("os códigos de falha precisam ser diferentes (seção 7 do desafio)")
	}
	expectBalance(t, e, "0.00")
}

func TestReversao_ValorDiferenteDaReferenciaEhRejeitado(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	res := e.run(t, e.reversal(domain.KindRefund, "refund-1", "20.00", "bet-1"))

	expectStatus(t, res, domain.StatusRejected, domain.FailureReversalAmountMismatch)
	expectBalance(t, e, "75.00")
}

func TestReversao_RodadaDiferenteEhRejeitada(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00"))

	c := e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1")
	c.RoundID = "round-2"
	res := e.run(t, c)

	expectStatus(t, res, domain.StatusRejected, domain.FailureReferenceMismatch)
}

func TestReversao_TipoDeReferenciaInvalidoEhRejeitado(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindWin, "win-1", "50.00"))

	// REFUND só vale para BET.
	res := e.run(t, e.reversal(domain.KindRefund, "refund-1", "50.00", "win-1"))

	expectStatus(t, res, domain.StatusRejected, domain.FailureReferenceInvalidKind)
}

func TestReversao_ReferenciaRejeitadaEhRejeicaoDefinitiva(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.cmd(domain.KindBet, "bet-1", "150.00")) // rejeitada por saldo

	res := e.run(t, e.reversal(domain.KindRefund, "refund-1", "150.00", "bet-1"))

	expectStatus(t, res, domain.StatusRejected, domain.FailureReferenceNotProcessed)
}

// ---------------------------------------------------------------------
// Referência ainda indisponível (PENDING_REFERENCE)
// ---------------------------------------------------------------------

func TestReferenciaAusente_FicaPendenteEDepoisConclui(t *testing.T) {
	e := newEnv(t, "100.00")

	// REFUND chega ANTES da BET que ele referencia.
	res := e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1"))

	expectStatus(t, res, domain.StatusPendingReference, "")
	expectBalance(t, e, "100.00")
	if e.store.ledgerCount() != 0 {
		t.Error("pendência não pode gerar lançamento")
	}

	// A BET chega depois.
	e.run(t, e.cmd(domain.KindBet, "bet-1", "25.00")) // 75.00

	// Simula o que o worker de referências fará: reaplicar a transação pendente.
	err := e.store.WithinTransaction(context.Background(), func(uow domain.UnitOfWork) error {
		pending, err := uow.WagerTransactions().FindByProviderAndExternalTxID(context.Background(), "provider-a", "refund-1")
		if err != nil || pending == nil {
			t.Fatalf("pendência não encontrada: %v", err)
		}
		return applyWagerTransaction(context.Background(), uow, pending)
	})
	if err != nil {
		t.Fatalf("reaplicação falhou: %v", err)
	}

	expectBalance(t, e, "100.00")
	replay := e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1"))
	expectStatus(t, replay, domain.StatusProcessed, "")
	if !replay.Replay {
		t.Error("consulta posterior deveria ser replay do resultado já concluído")
	}
}

func TestReferenciaAusente_ReplayDevolveOStatusPendente(t *testing.T) {
	e := newEnv(t, "100.00")
	e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1"))

	res := e.run(t, e.reversal(domain.KindRefund, "refund-1", "25.00", "bet-1"))

	if !res.Replay {
		t.Error("esperava replay")
	}
	expectStatus(t, res, domain.StatusPendingReference, "")
}

// ---------------------------------------------------------------------
// Entradas inválidas e carteira
// ---------------------------------------------------------------------

func TestCarteiraInexistente(t *testing.T) {
	e := newEnv(t, "100.00")
	c := e.cmd(domain.KindBet, "bet-1", "25.00")
	c.WalletID = uuid.New()

	_, err := e.uc.Execute(context.Background(), c)

	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("esperava ErrWalletNotFound, veio %v", err)
	}
	if len(e.store.txs) != 0 {
		t.Error("nada deveria ter sido gravado")
	}
}

func TestJogadorDiferenteDoDonoDaCarteira(t *testing.T) {
	e := newEnv(t, "100.00")
	c := e.cmd(domain.KindBet, "bet-1", "25.00")
	c.PlayerID = uuid.New()

	_, err := e.uc.Execute(context.Background(), c)

	if !errors.Is(err, domain.ErrPlayerWalletMismatch) {
		t.Fatalf("esperava ErrPlayerWalletMismatch, veio %v", err)
	}
	expectBalance(t, e, "100.00")
}

func TestMoedaDiferenteDaCarteira(t *testing.T) {
	e := newEnv(t, "100.00")
	c := e.cmd(domain.KindBet, "bet-1", "25.00")
	c.Currency = "USD"

	_, err := e.uc.Execute(context.Background(), c)

	if !errors.Is(err, domain.ErrWalletCurrencyMismatch) {
		t.Fatalf("esperava ErrWalletCurrencyMismatch, veio %v", err)
	}
	expectBalance(t, e, "100.00")
}

func TestEntradasInvalidas(t *testing.T) {
	e := newEnv(t, "100.00")

	casos := []struct {
		nome string
		cmd  ProcessWagerCommand
		erro error
	}{
		{"valor sem duas casas", e.cmd(domain.KindBet, "x1", "25"), domain.ErrInvalidAmount},
		{"notação científica", e.cmd(domain.KindBet, "x2", "1e3"), domain.ErrInvalidAmount},
		{"valor negativo", e.cmd(domain.KindBet, "x3", "-5.00"), domain.ErrNegativeAmount},
		{"BET com valor zero", e.cmd(domain.KindBet, "x4", "0.00"), domain.ErrInvalidWagerData},
		{"LOSS com valor diferente de zero", e.cmd(domain.KindLoss, "x5", "1.00"), domain.ErrInvalidWagerData},
		{"OPENING vindo de fora", e.cmd(domain.KindOpening, "x6", "10.00"), domain.ErrInvalidWagerData},
		{"REFUND sem referência", e.cmd(domain.KindRefund, "x7", "10.00"), domain.ErrMissingReference},
		{"tipo desconhecido", e.cmd(domain.WagerKind("XYZ"), "x8", "10.00"), domain.ErrInvalidWagerData},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := e.uc.Execute(context.Background(), c.cmd)
			if !errors.Is(err, c.erro) {
				t.Errorf("esperava %v, veio %v", c.erro, err)
			}
		})
	}

	if len(e.store.txs) != 0 || e.store.ledgerCount() != 0 {
		t.Error("entrada inválida não pode deixar nenhum efeito")
	}
	expectBalance(t, e, "100.00")
}

// ---------------------------------------------------------------------
// Corrida entre requisições iguais (caminho do ErrDuplicateTransaction)
// ---------------------------------------------------------------------

// raceRunner simula "outra instância venceu a corrida": na PRIMEIRA
// transação, as buscas de idempotência não enxergam nada (como se a
// outra requisição ainda não tivesse feito commit), mas no INSERT o
// vencedor já está gravado — e o índice único reclama.
//
// --- Conceito Go 12: embutir uma interface num struct ---
// `domain.WagerTransactionRepository` sem nome de campo, dentro do
// struct abaixo, faz o struct "herdar" todos os métodos da interface,
// delegando ao valor guardado. Só sobrescrevemos os métodos que
// queremos alterar (Find... e Create).
type raceRunner struct {
	store  *memStore
	winner *domain.WagerTransaction
	used   bool
}

func (r *raceRunner) WithinTransaction(ctx context.Context, fn func(uow domain.UnitOfWork) error) error {
	return r.store.WithinTransaction(ctx, func(uow domain.UnitOfWork) error {
		if r.used {
			return fn(uow)
		}
		r.used = true
		return fn(&raceUoW{UnitOfWork: uow, store: r.store, winner: r.winner})
	})
}

type raceUoW struct {
	domain.UnitOfWork
	store  *memStore
	winner *domain.WagerTransaction
}

func (u *raceUoW) WagerTransactions() domain.WagerTransactionRepository {
	return &blindRepo{WagerTransactionRepository: u.UnitOfWork.WagerTransactions(), store: u.store, winner: u.winner}
}

type blindRepo struct {
	domain.WagerTransactionRepository
	store  *memStore
	winner *domain.WagerTransaction
}

func (b *blindRepo) FindByProviderAndIdempotencyKey(context.Context, string, string) (*domain.WagerTransaction, error) {
	return nil, nil
}

func (b *blindRepo) FindByProviderAndExternalTxID(context.Context, string, string) (*domain.WagerTransaction, error) {
	return nil, nil
}

func (b *blindRepo) Create(ctx context.Context, t *domain.WagerTransaction) error {
	b.store.txs[b.winner.ID()] = cloneTx(b.winner) // "o vencedor fez commit agora"
	return b.WagerTransactionRepository.Create(ctx, t)
}

func TestCorrida_PerdedorVirandoReplayDoVencedor(t *testing.T) {
	e := newEnv(t, "100.00")
	cmd := e.cmd(domain.KindBet, "bet-1", "25.00")

	winner, err := buildCandidate(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err := winner.MarkProcessed(mustMoney(t, "75.00")); err != nil {
		t.Fatal(err)
	}

	uc := NewProcessWagerTransactionUseCase(&raceRunner{store: e.store, winner: winner})
	res, err := uc.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("o perdedor da corrida deveria virar replay, veio erro: %v", err)
	}

	if !res.Replay {
		t.Error("esperava replay do vencedor")
	}
	if res.Transaction.ID() != winner.ID() {
		t.Error("deveria devolver o registro do vencedor, não o candidato do perdedor")
	}
	if e.store.ledgerCount() != 0 {
		t.Error("o perdedor da corrida não pode movimentar dinheiro")
	}
}

func TestCorrida_VencedorComConteudoDiferenteViraConflito(t *testing.T) {
	e := newEnv(t, "100.00")

	// O "vencedor" usou a mesma chave, mas com valor 30.00.
	vencedorCmd := e.cmd(domain.KindBet, "bet-1", "30.00")
	winner, err := buildCandidate(vencedorCmd)
	if err != nil {
		t.Fatal(err)
	}

	uc := NewProcessWagerTransactionUseCase(&raceRunner{store: e.store, winner: winner})
	_, err = uc.Execute(context.Background(), e.cmd(domain.KindBet, "bet-1", "25.00"))

	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("esperava ErrIdempotencyConflict, veio %v", err)
	}
}
