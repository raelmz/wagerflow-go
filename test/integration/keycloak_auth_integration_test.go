//go:build integration

// Este arquivo fecha a última lacuna de autenticação/autorização
// (seção 4.7 do docs/PROJETO.md, "Verificado manualmente"): até aqui,
// o fluxo OIDC completo contra o Keycloak real (discovery, JWKS,
// client_credentials, claim "azp", realm_access.roles) só tinha sido
// confirmado à mão, com `curl`. Este teste automatiza exatamente os
// mesmos 5 cenários que já foram validados manualmente.
//
// Escopo DELIBERADAMENTE restrito à camada de autenticação/autorização
// (AuthMiddleware + RequireRole), não ao router de produção inteiro:
//   - auth_middleware_test.go (sem build tag) já cobre a LÓGICA de
//     decisão (401/403, propagação de identidade) com um TokenVerifier
//     FAKE — rápido, sem Keycloak.
//   - wallet_handler_test.go/wager_handler_test.go (sessão 017) já
//     cobrem o CONTRATO dos handlers (formato JSON, mapeamento de
//     status) também com fakes.
//   - O que NINGUÉM ainda tinha automatizado era a integração real
//     entre as duas pontas: um token EMITIDO de verdade pelo Keycloak,
//     verificado pelo oidcVerifier de verdade (assinatura, issuer,
//     JWKS buscado por discovery OIDC), com as claims reais (azp,
//     realm_access.roles) do realm "wagerflow" provisionado em
//     deployments/keycloak/realm-export.json.
//
// Por isso o handler de teste (buildAuthTestHandler) é uma versão
// mínima do router de produção — só as duas famílias de rota que
// importam para autenticação/autorização (uma que aceita
// provider OU internal, outra só internal) — em vez de montar o
// router completo com repositórios Postgres reais. Isso mantém o
// teste focado no que ele existe para provar, sem duplicar a
// cobertura que os outros dois arquivos já dão ao contrato HTTP.
package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	httpapi "github.com/raelmz/wagerflow-go/internal/interfaces/http"
)

// Credenciais de DEV dos 3 clients já provisionados no realm (ver
// deployments/keycloak/realm-export.json e o comentário no final do
// .env.example) — não são segredo real, só existem para o desafio.
const (
	testProviderAClientID     = "provider-a"
	testProviderAClientSecret = "provider-a-secret"
	testInternalClientID      = "wagerflow-internal"
	testInternalClientSecret  = "internal-secret"
)

// testKeycloakIssuerURL resolve a issuer URL do realm "wagerflow".
//
// Ao contrário de baseDatabaseURL (que EXIGE TEST_DATABASE_URL, sem
// default), aqui há um default seguro: a porta publicada no HOST pelo
// docker-compose.yml é sempre 8081 (ver .env.example), então rodar
// `go test -tags=integration` direto na máquina do desenvolvedor
// funciona sem configurar nada.
//
// A variável de ambiente TEST_KEYCLOAK_ISSUER_URL só precisa ser
// definida quando o teste roda DENTRO de um container na mesma rede
// do compose (o mesmo cenário do golang:1.27 usado para os outros
// testes de integração no Windows) — nesse caso o hostname é o nome
// do serviço (wagerflow-keycloak) e a porta é a INTERNA do container
// (8080, não 8081 — 8081 é só o mapeamento visto de fora):
//
//	TEST_KEYCLOAK_ISSUER_URL=http://wagerflow-keycloak:8080/realms/wagerflow
func testKeycloakIssuerURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("TEST_KEYCLOAK_ISSUER_URL"); v != "" {
		return v
	}
	return "http://localhost:8081/realms/wagerflow"
}

// fetchKeycloakToken pede um access token de verdade ao Keycloak via
// client_credentials — o MESMO fluxo que um provedor real ou o
// próprio serviço interno usariam em produção (seção 2 do desafio).
// Nenhuma parte do token é fabricada pelo teste: se o Keycloak não
// estiver de pé (docker compose down, ou só o Postgres subiu), a
// falha aqui já deixa isso claro, em vez de um erro confuso mais
// adiante na verificação OIDC.
func fetchKeycloakToken(t *testing.T, issuerURL, clientID, clientSecret string) string {
	t.Helper()

	tokenURL := issuerURL + "/protocol/openid-connect/token"
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("falha ao montar requisição de token para %s: %v", clientID, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf(
			"não consegui falar com o Keycloak em %s: %v\n"+
				"Confirme que o container está de pé: docker compose -f deployments/docker-compose.yml up -d keycloak",
			tokenURL, err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf(
			"Keycloak recusou o pedido de token para client_id=%s: status %d, corpo=%s",
			clientID, resp.StatusCode, string(body),
		)
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("resposta de token do Keycloak não é o JSON esperado: %v", err)
	}
	if parsed.AccessToken == "" {
		t.Fatal("Keycloak respondeu 200 mas sem access_token no corpo")
	}
	return parsed.AccessToken
}

// newRealVerifier monta o MESMO TokenVerifier usado em produção
// (cmd/api/main.go): faz discovery OIDC de verdade contra o Keycloak
// e prepara a verificação de assinatura/issuer/expiração via JWKS.
// Nenhum fake entra aqui — é este ponto que os outros testes (com
// fakeVerifier) não conseguem exercitar.
func newRealVerifier(t *testing.T, issuerURL string) httpapi.TokenVerifier {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	verifier, err := httpapi.NewOIDCVerifier(ctx, issuerURL)
	if err != nil {
		t.Fatalf(
			"falha ao inicializar o verificador OIDC contra %s: %v\n"+
				"Confirme que o Keycloak está de pé e que o realm 'wagerflow' já foi importado "+
				"(o healthcheck do serviço keycloak no docker compose só fica \"healthy\" depois do import terminar).",
			issuerURL, err,
		)
	}
	return verifier
}

// buildAuthTestHandler espelha, em miniatura, os dois grupos de rota
// de router.go que importam para autenticação/autorização — não o
// router inteiro, que precisaria de repositórios Postgres reais para
// os handlers de negócio (fora do escopo deste teste, ver comentário
// do arquivo). "/wagering-like" == grupo /wagering/transactions
// (aceita provider OU internal); "/wallets-like" == grupo /wallets
// (só internal).
func buildAuthTestHandler(verifier httpapi.TokenVerifier) http.Handler {
	echoIdentity := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerID, _ := httpapi.AuthenticatedProviderID(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"providerId": providerID,
			"internal":   httpapi.IsInternal(r.Context()),
		})
	})

	authenticate := httpapi.AuthMiddleware(verifier)

	mux := http.NewServeMux()
	mux.Handle("/wagering-like", authenticate(httpapi.RequireRole(httpapi.RoleProvider, httpapi.RoleInternal)(echoIdentity)))
	mux.Handle("/wallets-like", authenticate(httpapi.RequireRole(httpapi.RoleInternal)(echoIdentity)))
	return mux
}

// decodeIdentity lê o corpo JSON que echoIdentity devolve.
func decodeIdentity(t *testing.T, rec *httptest.ResponseRecorder) (providerID string, internal bool) {
	t.Helper()
	var body struct {
		ProviderID string `json:"providerId"`
		Internal   bool   `json:"internal"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("resposta do handler de teste não é o JSON esperado: %v (corpo: %s)", err, rec.Body.String())
	}
	return body.ProviderID, body.Internal
}

// ---------------------------------------------------------------------
// Cenário 1 (dos 5 já validados manualmente): rota protegida sem
// nenhum token → 401.
// ---------------------------------------------------------------------
func TestKeycloakAuth_SemToken_Retorna401(t *testing.T) {
	handler := buildAuthTestHandler(newRealVerifier(t, testKeycloakIssuerURL(t)))

	req := httptest.NewRequest(http.MethodGet, "/wagering-like", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperava 401 sem token, veio %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------
// Cenário adicional (não estava nos 5 de curl, mas é o caso mais
// óbvio de token FALSO — importante provar que o oidcVerifier real
// rejeita algo que nunca foi assinado pelo Keycloak, não só que o
// fake rejeita).
// ---------------------------------------------------------------------
func TestKeycloakAuth_TokenInvalido_Retorna401(t *testing.T) {
	handler := buildAuthTestHandler(newRealVerifier(t, testKeycloakIssuerURL(t)))

	req := httptest.NewRequest(http.MethodGet, "/wagering-like", nil)
	req.Header.Set("Authorization", "Bearer isto-nao-e-um-jwt-assinado-pelo-keycloak")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperava 401 com token inválido, veio %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------
// Cenário 2: token real e válido de provider-a numa rota de wagering
// → 200, com a identidade certa (providerId = "provider-a", via claim
// "azp") e SEM o role internal.
// ---------------------------------------------------------------------
func TestKeycloakAuth_TokenProviderA_PropagaIdentidadeCorreta(t *testing.T) {
	issuerURL := testKeycloakIssuerURL(t)
	handler := buildAuthTestHandler(newRealVerifier(t, issuerURL))
	token := fetchKeycloakToken(t, issuerURL, testProviderAClientID, testProviderAClientSecret)

	req := httptest.NewRequest(http.MethodGet, "/wagering-like", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200 com token real de provider-a, veio %d: %s", rec.Code, rec.Body.String())
	}

	providerID, internal := decodeIdentity(t, rec)
	if providerID != testProviderAClientID {
		t.Fatalf("esperava providerId extraído da claim azp = %q, veio %q", testProviderAClientID, providerID)
	}
	if internal {
		t.Fatal("token de provider-a não deveria carregar o role internal")
	}
}

// ---------------------------------------------------------------------
// Cenário 3: token real e válido de wagerflow-internal numa rota de
// carteira (só internal) → 200, com o role internal presente.
// ---------------------------------------------------------------------
func TestKeycloakAuth_TokenInternal_AcessaRotaDeCarteira(t *testing.T) {
	issuerURL := testKeycloakIssuerURL(t)
	handler := buildAuthTestHandler(newRealVerifier(t, issuerURL))
	token := fetchKeycloakToken(t, issuerURL, testInternalClientID, testInternalClientSecret)

	req := httptest.NewRequest(http.MethodGet, "/wallets-like", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200 com token real de wagerflow-internal, veio %d: %s", rec.Code, rec.Body.String())
	}

	_, internal := decodeIdentity(t, rec)
	if !internal {
		t.Fatal("token de wagerflow-internal deveria carregar o role internal")
	}
}

// ---------------------------------------------------------------------
// Cenário 4: token real de provider-a (role provider, sem internal)
// tentando uma rota de carteira → 403, não 401 (a identidade É válida,
// só não tem permissão) — mesma checagem que RequireRole já cobre com
// fake, agora contra roles que vieram de verdade do realm_access do
// Keycloak.
// ---------------------------------------------------------------------
func TestKeycloakAuth_TokenProviderA_NaoAcessaRotaDeCarteira(t *testing.T) {
	issuerURL := testKeycloakIssuerURL(t)
	handler := buildAuthTestHandler(newRealVerifier(t, issuerURL))
	token := fetchKeycloakToken(t, issuerURL, testProviderAClientID, testProviderAClientSecret)

	req := httptest.NewRequest(http.MethodGet, "/wallets-like", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("esperava 403 (provider não pode rota de carteira), veio %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------
// Cenário 5: provider-b tem seu PRÓPRIO client no Keycloak — prova
// que o isolamento não depende de um único client "provider" genérico
// (cada provedor realmente tem client_id/secret próprios, como a
// seção 4.7 do PROJETO.md documenta) e que ambos recebem o mesmo
// tratamento (role provider, sem internal).
// ---------------------------------------------------------------------
func TestKeycloakAuth_TokenProviderB_TambemFuncionaComIdentidadePropria(t *testing.T) {
	issuerURL := testKeycloakIssuerURL(t)
	handler := buildAuthTestHandler(newRealVerifier(t, issuerURL))
	token := fetchKeycloakToken(t, issuerURL, "provider-b", "provider-b-secret")

	req := httptest.NewRequest(http.MethodGet, "/wagering-like", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200 com token real de provider-b, veio %d: %s", rec.Code, rec.Body.String())
	}

	providerID, internal := decodeIdentity(t, rec)
	if providerID != "provider-b" {
		t.Fatalf("esperava providerId = 'provider-b', veio %q", providerID)
	}
	if internal {
		t.Fatal("token de provider-b não deveria carregar o role internal")
	}
}
