package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
)

// RoleProvider e RoleInternal são os dois realm roles definidos no
// realm "wagerflow" do Keycloak (ver
// deployments/keycloak/realm-export.json). Um client com RoleInternal
// pode tudo que RoleProvider pode nas rotas de wagering, e É O ÚNICO
// que pode chamar as rotas de carteira — a seção "Autenticação e
// autorização" do desafio exige "restrição das operações internas".
const (
	RoleProvider = "provider"
	RoleInternal = "internal"
)

// tokenClaims é o subconjunto de claims do access token do Keycloak
// que a aplicação usa. AuthorizedParty (claim "azp") é o client_id de
// quem pediu o token: em client_credentials, cada provedor tem o
// PRÓPRIO client no Keycloak cujo client_id É o providerId (decisão
// confirmada com o desenvolvedor) — então "azp" já resolve a
// identidade sem precisar de nenhum mapper customizado.
//
// Embutir jwt.RegisteredClaims aqui é só para termos os campos
// padrão (exp, iat, iss...) tipados com jwt.NumericDate, úteis para
// log/depuração; a VALIDAÇÃO de fato (assinatura, issuer, expiração)
// já foi feita pelo go-oidc antes dessas claims serem decodificadas
// (ver oidcVerifier.Verify) — golang-jwt aqui não verifica nada
// sozinho, só empresta o tipo de claims.
type tokenClaims struct {
	jwt.RegisteredClaims
	AuthorizedParty string `json:"azp"`
	RealmAccess     struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

// authContextKey evita colisão com chaves de outros pacotes no
// context (mesma preocupação que application.WithCorrelationID já
// resolve para o correlationId).
type authContextKey string

const (
	ctxKeyProviderID authContextKey = "auth.providerId"
	ctxKeyRoles      authContextKey = "auth.roles"
)

// AuthenticatedProviderID devolve o providerId extraído do token
// (claim "azp") já validado pelo AuthMiddleware. Só é confiável
// DENTRO de uma rota protegida por AuthMiddleware — fora dela, o
// segundo valor de retorno vem false.
func AuthenticatedProviderID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyProviderID).(string)
	return v, ok
}

// hasRole confere se o token trazia o role informado.
func hasRole(ctx context.Context, role string) bool {
	roles, _ := ctx.Value(ctxKeyRoles).([]string)
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

// IsInternal diz se a identidade autenticada tem o role "internal" —
// usado pelos handlers de wagering para liberar o serviço interno da
// checagem de providerId (ver wager_handler.go). Isolamento entre
// provedores só se aplica a quem tem o role "provider".
func IsInternal(ctx context.Context) bool {
	return hasRole(ctx, RoleInternal)
}

// TokenVerifier isola a dependência do Keycloak atrás de uma
// interface pequena. Além de ser a mesma prática de sempre no
// projeto (application/domain recebem interfaces, nunca a
// implementação concreta), permite testar o middleware com um fake
// (ver auth_middleware_test.go), sem precisar subir um Keycloak real
// só para testar 401/403.
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (*tokenClaims, error)
}

// oidcVerifier implementa TokenVerifier usando go-oidc para resolver
// o JWKS do issuer via discovery OIDC (com cache e rotação de chaves
// automáticos) e conferir assinatura + issuer + expiração do token;
// golang-jwt entra só depois, para decodificar as claims customizadas
// (azp, realm_access) no tipo forte tokenClaims.
type oidcVerifier struct {
	verifier *oidc.IDTokenVerifier
}

// NewOIDCVerifier monta o verificador a partir da issuer URL do
// Keycloak (ex.: http://localhost:8081/realms/wagerflow). Faz
// discovery OIDC (busca automaticamente jwks_uri em
// /.well-known/openid-configuration) — por isso recebe um contexto
// com timeout de fora: se o Keycloak ainda não subiu, o boot da
// aplicação falha rápido com um erro claro, em vez de travar para
// sempre (mesmo padrão de newPool em cmd/api/main.go).
func NewOIDCVerifier(ctx context.Context, issuerURL string) (TokenVerifier, error) {
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, err
	}

	// SkipClientIDCheck: true porque QUEM está falando é resolvido
	// por nós via claim "azp" (ver tokenClaims), não pela checagem
	// padrão de "aud" do go-oidc — pensada para ID tokens, não para
	// access tokens de client_credentials, que no Keycloak não têm
	// necessariamente um client_id específico como audience.
	verifier := provider.Verifier(&oidc.Config{SkipClientIDCheck: true})
	return &oidcVerifier{verifier: verifier}, nil
}

func (v *oidcVerifier) Verify(ctx context.Context, rawToken string) (*tokenClaims, error) {
	idToken, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, err
	}

	var claims tokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

// AuthMiddleware exige um Bearer token válido (assinatura, issuer e
// expiração, via TokenVerifier) em toda rota que o usa. Não decide
// AUTORIZAÇÃO por role — isso é RequireRole, aplicado por grupo de
// rota em router.go — só confirma quem está falando e publica essa
// identidade no context para o resto da cadeia.
func AuthMiddleware(verifier TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				writeJSON(w, http.StatusUnauthorized, apiErrorBody{
					Code:    "MISSING_CREDENTIALS",
					Message: "header Authorization é obrigatório",
				})
				return
			}

			rawToken, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || rawToken == "" {
				writeJSON(w, http.StatusUnauthorized, apiErrorBody{
					Code:    "MISSING_CREDENTIALS",
					Message: "Authorization deve ser um Bearer token",
				})
				return
			}

			claims, err := verifier.Verify(r.Context(), rawToken)
			if err != nil {
				code, message := classifyTokenError(err)
				writeJSON(w, http.StatusUnauthorized, apiErrorBody{Code: code, Message: message})
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyProviderID, claims.AuthorizedParty)
			ctx = context.WithValue(ctx, ctxKeyRoles, claims.RealmAccess.Roles)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// classifyTokenError distingue expiração (o cliente sabe que precisa
// pedir um token novo, sem tentar de novo com o mesmo) do resto dos
// motivos de rejeição (assinatura inválida, issuer errado, token
// malformado etc.), agrupados como credencial inválida. O go-oidc não
// exporta um erro sentinela para "expirado", então a checagem é por
// texto — está documentado aqui exatamente por ser frágil: se a
// mensagem de erro da lib mudar de versão para versão, o pior caso é
// classificar como INVALID_CREDENTIALS em vez de EXPIRED_CREDENTIALS
// (ainda 401, só o code muda).
func classifyTokenError(err error) (code, message string) {
	if strings.Contains(err.Error(), "expired") {
		return "EXPIRED_CREDENTIALS", "token expirado"
	}
	return "INVALID_CREDENTIALS", "token inválido: " + err.Error()
}

// RequireRole exige que a identidade autenticada tenha PELO MENOS UM
// dos roles informados. Precisa vir DEPOIS de AuthMiddleware na
// cadeia (r.Use), senão o context não tem roles nenhum e toda
// requisição cai em 403.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, role := range roles {
				if hasRole(r.Context(), role) {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeJSON(w, http.StatusForbidden, apiErrorBody{
				Code:    "FORBIDDEN",
				Message: "identidade autenticada não tem permissão para esta operação",
			})
		})
	}
}
