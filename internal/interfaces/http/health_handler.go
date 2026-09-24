package http

import (
	"context"
	"net/http"
)

// pinger é satisfeito por *pgxpool.Pool (main.go injeta o pool real),
// mas o handler não precisa importar pgx só para checar saúde —
// mantém este pacote livre de conhecer o driver específico do banco.
type pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler implementa GET /health/live e GET /health/ready
// (seção 9 do desafio).
type HealthHandler struct {
	db pinger
	// sqs fica reservado para quando o consumidor SQS existir (item 3
	// da seção 5 do contexto de sessão) — readiness também deve
	// checar a fila, não só o Postgres.
}

func NewHealthHandler(db pinger) *HealthHandler {
	return &HealthHandler{db: db}
}

// Live é liveness pura do processo: se o handler responde, o
// processo está vivo. Não depende de nenhuma dependência externa —
// caso contrário, um Postgres fora do ar derrubaria o orquestrador
// reiniciando o processo à toa, quando na verdade é o BANCO que está
// indisponível, não a aplicação.
func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "live"})
}

// Ready confirma que as dependências externas (Postgres, e no futuro
// SQS) estão alcançáveis. Só aqui um problema de infraestrutura deve
// tirar a instância de trás do load balancer.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	if err := h.db.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not ready",
			"reason": "postgres indisponível",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
