package auth

import (
	"context"
	"net/http"
	"strings"

	"luma.folk3s.com/server/internal/httpx"
)

type ctxKey struct{}

// BearerToken extracts the token from "Authorization: Bearer <token>".
func BearerToken(r *http.Request) string {
	scheme, tok, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(tok)
}

// Require wraps h so it only runs for an authenticated session.
func (s *Service) Require(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Authenticate(r.Context(), BearerToken(r))
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		httpx.SetLogUser(r.Context(), p.UserID.String())
		h(w, r.WithContext(WithPrincipal(r.Context(), p)))
	}
}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// MustPrincipal is for handlers wrapped by Require.
func MustPrincipal(r *http.Request) Principal {
	p, ok := FromContext(r.Context())
	if !ok {
		panic("auth: handler not wrapped by Require")
	}
	return p
}
