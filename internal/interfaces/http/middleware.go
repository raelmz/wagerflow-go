package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/raelmz/wagerflow-go/internal/application"
)

// correlationIDMiddleware lê o header X-Correlation-Id da requisição
// (ou gera um novo, se ausente) e o coloca no context via
// application.WithCorrelationID — é a mesma chave que
// wager_applier.go e outbox_events.go usam para gravar nos eventos
// da outbox, então uma requisição HTTP e os eventos que ela produz
// compartilham o mesmo correlationId de ponta a ponta.
func correlationIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := r.Header.Get("X-Correlation-Id")
		if correlationID == "" {
			correlationID = uuid.NewString()
		}
		ctx := application.WithCorrelationID(r.Context(), correlationID)
		w.Header().Set("X-Correlation-Id", correlationID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder envolve http.ResponseWriter só para guardar o status
// HTTP que o handler escreveu — o pacote net/http não expõe isso
// depois do fato, então é preciso interceptar a chamada a
// WriteHeader. WriteHeader nunca é chamado quando o handler usa o
// status implícito 200 (ex.: escreve direto com Write) — por isso o
// valor inicial já é 200.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(status int) {
	rec.status = status
	rec.ResponseWriter.WriteHeader(status)
}

// loggingMiddleware é o log de acesso HTTP pedido na seção 12 do
// desafio: uma linha JSON por requisição, com método, rota, status,
// duração e o correlationId (o mesmo que correlationIDMiddleware
// injeta no context e que os eventos da outbox também carregam — por
// isso este middleware precisa rodar DEPOIS dele, ver a ordem em
// router.go). Nunca loga corpo de requisição/resposta: poderia conter
// dados financeiros ou, na autenticação, nada de sensível deveria
// vazar mesmo assim, mas a regra do desafio é clara — sem payload
// completo nos logs.
func loggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			logger.Info("requisição HTTP",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"durationMs", time.Since(start).Milliseconds(),
				"correlationId", application.CorrelationID(r.Context()),
			)
		})
	}
}

// writeJSON serializa v como JSON com o status HTTP informado. Usado
// por todo handler de sucesso.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError traduz err com mapError e escreve a resposta de erro
// padrão. Usado por todo handler quando um caso de uso falha.
func writeError(w http.ResponseWriter, err error) {
	status, body := mapError(err)
	writeJSON(w, status, body)
}

// decodeJSON lê e decodifica o corpo da requisição. Erro de JSON
// malformado é sempre entrada inválida (400).
func decodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errInvalidInput{msg: "corpo da requisição não é um JSON válido: " + err.Error()}
	}
	return nil
}
