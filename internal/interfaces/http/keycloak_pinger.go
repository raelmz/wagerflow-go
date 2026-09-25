package http

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"time"
)

// httpKeycloakPinger implementa KeycloakPinger com uma chamada HTTP
// simples ao endpoint de discovery OIDC — o mesmo
// /.well-known/openid-configuration que oidc.NewProvider já consulta
// uma vez, no boot, dentro de NewOIDCVerifier. Não reaproveitamos o
// TokenVerifier aqui de propósito: ele não expõe nenhum método de
// "ping" (só Verify, que exige um token), então um GET direto nesse
// endpoint é a forma mais simples de confirmar que o Keycloak
// continua respondendo, sem inventar um método novo só para isso.
type httpKeycloakPinger struct {
	client       *stdhttp.Client
	discoveryURL string
}

// NewKeycloakPinger monta o pinger a partir da mesma issuer URL usada
// pelo TokenVerifier (cfg.KeycloakIssuerURL). Timeout curto (5s): o
// /health/ready pode ser chamado com frequência pelo orquestrador, e
// uma dependência lenta não deve travar a checagem por muito tempo.
func NewKeycloakPinger(issuerURL string) KeycloakPinger {
	return &httpKeycloakPinger{
		client:       &stdhttp.Client{Timeout: 5 * time.Second},
		discoveryURL: issuerURL + "/.well-known/openid-configuration",
	}
}

func (p *httpKeycloakPinger) Ping(ctx context.Context) error {
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, p.discoveryURL, nil)
	if err != nil {
		return fmt.Errorf("montando requisição de ping ao Keycloak: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("Keycloak indisponível: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != stdhttp.StatusOK {
		return fmt.Errorf("Keycloak respondeu status %d no endpoint de discovery", resp.StatusCode)
	}
	return nil
}
