package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeVerifier implementa TokenVerifier sem precisar de um Keycloak
// real — devolve as claims (ou o erro) que o teste configurar.
type fakeVerifier struct {
	claims *tokenClaims
	err    error
}

func (f *fakeVerifier) Verify(_ context.Context, _ string) (*tokenClaims, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.claims, nil
}

func TestAuthMiddleware_SemHeaderAuthorization(t *testing.T) {
	handler := AuthMiddleware(&fakeVerifier{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("não deveria chegar ao handler protegido sem Authorization")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperava 401, veio %d", rec.Code)
	}
}

func TestAuthMiddleware_TokenInvalido(t *testing.T) {
	handler := AuthMiddleware(&fakeVerifier{err: errors.New("assinatura inválida")})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("não deveria chegar ao handler protegido com token inválido")
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token-qualquer")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperava 401, veio %d", rec.Code)
	}
}

func TestAuthMiddleware_TokenExpirado(t *testing.T) {
	handler := AuthMiddleware(&fakeVerifier{err: errors.New("token is expired")})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("não deveria chegar ao handler protegido com token expirado")
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token-vencido")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperava 401, veio %d", rec.Code)
	}
}

func TestAuthMiddleware_TokenValidoPropagaIdentidadeNoContext(t *testing.T) {
	claims := &tokenClaims{AuthorizedParty: "provider-a"}
	claims.RealmAccess.Roles = []string{RoleProvider}

	var gotProviderID string
	var gotIsInternal bool
	handler := AuthMiddleware(&fakeVerifier{claims: claims})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotProviderID, _ = AuthenticatedProviderID(r.Context())
			gotIsInternal = IsInternal(r.Context())
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token-valido")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d", rec.Code)
	}
	if gotProviderID != "provider-a" {
		t.Fatalf("esperava providerId 'provider-a', veio %q", gotProviderID)
	}
	if gotIsInternal {
		t.Fatal("token de provider não deveria ter role internal")
	}
}

func TestRequireRole_BloqueiaSemORoleExigido(t *testing.T) {
	handler := RequireRole(RoleInternal)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("não deveria chegar ao handler protegido sem o role exigido")
	}))

	ctx := context.WithValue(context.Background(), ctxKeyRoles, []string{RoleProvider})
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("esperava 403, veio %d", rec.Code)
	}
}

func TestRequireRole_PermiteComQualquerUmDosRolesAceitos(t *testing.T) {
	called := false
	handler := RequireRole(RoleProvider, RoleInternal)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	ctx := context.WithValue(context.Background(), ctxKeyRoles, []string{RoleInternal})
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("handler protegido deveria ter sido chamado")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d", rec.Code)
	}
}
