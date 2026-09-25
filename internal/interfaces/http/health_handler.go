package http

import (
	"context"
	"net/http"
)

// DBPinger, SQSPinger e KeycloakPinger são três interfaces
// estruturalmente IDÊNTICAS (todas só exigem Ping(ctx) error), mas
// declaradas em separado de propósito: é assim que o Uber Fx — que
// resolve cada parâmetro de construtor pelo TIPO declarado, não pelo
// formato — consegue diferenciar "o pinger do Postgres" do "pinger
// do SQS" e do "pinger do Keycloak" na hora de montar o
// HealthHandler. Ver cmd/api/main.go (newDBPinger/newSQSPinger/
// newKeycloakPinger), que é quem devolve cada um desses tipos.
type DBPinger interface {
	Ping(ctx context.Context) error
}

type SQSPinger interface {
	Ping(ctx context.Context) error
}

type KeycloakPinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler implementa GET /health/live e GET /health/ready
// (seção 9 do desafio).
type HealthHandler struct {
	db       DBPinger
	sqs      SQSPinger
	keycloak KeycloakPinger
}

func NewHealthHandler(db DBPinger, sqs SQSPinger, keycloak KeycloakPinger) *HealthHandler {
	return &HealthHandler{db: db, sqs: sqs, keycloak: keycloak}
}

// Live é liveness pura do processo: se o handler responde, o
// processo está vivo. Não depende de nenhuma dependência externa —
// caso contrário, um Postgres (ou SQS, ou Keycloak) fora do ar
// derrubaria o orquestrador reiniciando o processo à toa, quando na
// verdade é uma DEPENDÊNCIA que está indisponível, não a aplicação
// em si.
func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "live"})
}

// Ready confirma que TODAS as dependências externas que a aplicação
// precisa para funcionar — Postgres, SQS e Keycloak — estão
// alcançáveis. Só aqui um problema de infraestrutura deve tirar a
// instância de trás do load balancer.
//
// Checa as três mesmo se a primeira já falhar (em vez de parar no
// primeiro erro) e devolve TODAS as que falharam de uma vez: é mais
// fácil diagnosticar "Postgres E Keycloak fora do ar" lendo uma
// resposta só do que precisando bater no endpoint várias vezes até
// a dependência anterior ser corrigida.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	checks := []struct {
		name string
		ping func(context.Context) error
	}{
		{"postgres", h.db.Ping},
		{"sqs", h.sqs.Ping},
		{"keycloak", h.keycloak.Ping},
	}

	failures := map[string]string{}
	for _, check := range checks {
		if err := check.ping(r.Context()); err != nil {
			failures[check.name] = err.Error()
		}
	}

	if len(failures) > 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":  "not ready",
			"reasons": failures,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
