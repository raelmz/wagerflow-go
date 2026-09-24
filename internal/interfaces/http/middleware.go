package http

import (
	"encoding/json"
	"net/http"

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
