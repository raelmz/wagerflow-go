package http

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// NewRouter monta todas as rotas da seção 9 do desafio. Recebe os
// handlers já prontos (Uber Fx quem monta cada um, em cmd/api) —
// este arquivo só liga rota a método de handler e aplica os
// middlewares de auth por grupo, nada mais.
func NewRouter(wallets *WalletHandler, wagers *WagerHandler, health *HealthHandler, verifier TokenVerifier, logger *slog.Logger) *chi.Mux {
	r := chi.NewRouter()

	// RequestID/Recoverer são da própria lib chi (chi/middleware):
	// Recoverer evita que um panic num handler derrube o processo
	// inteiro (vira 500 em vez de matar o servidor). RequestID dá um
	// id de requisição para log, independente do nosso
	// X-Correlation-Id (que é de domínio, não de infraestrutura HTTP).
	//
	// Ordem importa: correlationIDMiddleware precisa rodar ANTES de
	// loggingMiddleware, porque é ele quem coloca o correlationId no
	// context — loggingMiddleware só consegue ler o que já foi
	// colocado por um middleware anterior na cadeia.
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(correlationIDMiddleware)
	r.Use(loggingMiddleware(logger))

	// Health checks públicos: sem middleware de auth (autenticação
	// nunca se aplica aqui — o próprio orquestrador/monitoramento os
	// chama sem credencial).
	r.Get("/health/live", health.Live)
	r.Get("/health/ready", health.Ready)

	authenticate := AuthMiddleware(verifier)

	// Rotas de carteira são "operações internas", restritas ao role
	// "internal" (texto literal da seção "Autenticação e
	// autorização" do desafio) — nenhum provedor externo pode
	// chamá-las, nem para a própria carteira.
	r.Route("/wallets", func(r chi.Router) {
		r.Use(authenticate)
		r.Use(RequireRole(RoleInternal))

		r.Post("/", wallets.Create)
		r.Get("/{walletId}", wallets.Get)
		r.Get("/{walletId}/ledger", wallets.Ledger)
		r.Post("/{walletId}/reconciliation", wallets.Reconciliation)
	})

	// Rotas de wagering aceitam "provider" (o dono da operação) ou
	// "internal" (acesso irrestrito). O isolamento fino — o
	// providerId do TOKEN precisa bater com o providerId da
	// REQUISIÇÃO — não dá para checar aqui (o router não lê corpo
	// nem sabe a semântica de cada rota), então é feito dentro de
	// cada handler (ver wager_handler.go).
	r.Route("/wagering/transactions", func(r chi.Router) {
		r.Use(authenticate)
		r.Use(RequireRole(RoleProvider, RoleInternal))

		r.Post("/", wagers.Process)
		r.Get("/{transactionId}", wagers.GetByID)
	})

	r.Route("/providers/{providerId}/wagering/transactions/{externalTransactionId}", func(r chi.Router) {
		r.Use(authenticate)
		r.Use(RequireRole(RoleProvider, RoleInternal))

		r.Get("/", wagers.GetByProviderAndExternalID)
	})

	return r
}
