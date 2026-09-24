package http

import (
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// NewRouter monta todas as rotas da seção 9 do desafio. Recebe os
// handlers já prontos (Uber Fx quem monta cada um, em cmd/api) —
// este arquivo só liga rota a método de handler, nada mais.
func NewRouter(wallets *WalletHandler, wagers *WagerHandler, health *HealthHandler) *chi.Mux {
	r := chi.NewRouter()

	// RequestID/Recoverer são da própria lib chi (chi/middleware):
	// Recoverer evita que um panic num handler derrube o processo
	// inteiro (vira 500 em vez de matar o servidor). RequestID dá um
	// id de requisição para log, independente do nosso
	// X-Correlation-Id (que é de domínio, não de infraestrutura HTTP).
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(correlationIDMiddleware)

	// Health checks públicos: sem middleware de auth (item 2 da seção
	// 5 vai acrescentar auth nas rotas de negócio, nunca aqui).
	r.Get("/health/live", health.Live)
	r.Get("/health/ready", health.Ready)

	r.Route("/wallets", func(r chi.Router) {
		r.Post("/", wallets.Create)
		r.Get("/{walletId}", wallets.Get)
		r.Get("/{walletId}/ledger", wallets.Ledger)
		r.Post("/{walletId}/reconciliation", wallets.Reconciliation)
	})

	r.Route("/wagering/transactions", func(r chi.Router) {
		r.Post("/", wagers.Process)
		r.Get("/{transactionId}", wagers.GetByID)
	})

	r.Get("/providers/{providerId}/wagering/transactions/{externalTransactionId}", wagers.GetByProviderAndExternalID)

	return r
}
