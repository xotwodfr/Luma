// Package api wires Luma's services into one HTTP handler.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"luma.folk3s.com/server/internal/auth"
	"luma.folk3s.com/server/internal/config"
	"luma.folk3s.com/server/internal/db"
	"luma.folk3s.com/server/internal/httpx"
	"luma.folk3s.com/server/internal/users"
)

type Deps struct {
	Pool   *pgxpool.Pool
	Config config.Config
	Logger *slog.Logger
	// AuthPerMinute caps register/login attempts per client IP. 0 means the production default of 10.
	AuthPerMinute int
}

// App holds the services so tests and the admin CLI can reach them directly.
type App struct {
	Auth    *auth.Service
	Users   *users.Service
	deps    Deps
	handler http.Handler
}

func New(d Deps) *App {
	a := &App{
		Auth:  auth.NewService(d.Pool, d.Config.Registration),
		Users: users.NewService(d.Pool),
		deps:  d,
	}
	if d.AuthPerMinute == 0 {
		d.AuthPerMinute = 10
	}
	authH := &auth.Handlers{Svc: a.Auth, Limit: httpx.NewLimiter(d.AuthPerMinute, time.Minute), TrustedProxy: d.Config.TrustedProxy}
	usersH := &users.Handlers{Svc: a.Users}
	req := a.Auth.Require

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health/live", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/health/ready", a.ready)

	mux.HandleFunc("POST /v1/auth/register", authH.Register)
	mux.HandleFunc("POST /v1/auth/login", authH.Login)
	mux.HandleFunc("POST /v1/auth/logout", req(authH.Logout))
	mux.HandleFunc("GET /v1/auth/sessions", req(authH.Sessions))
	mux.HandleFunc("DELETE /v1/auth/sessions/{id}", req(authH.RevokeSession))
	mux.HandleFunc("POST /v1/auth/sessions/revoke-others", req(authH.RevokeOthers))
	mux.HandleFunc("POST /v1/invites", req(authH.CreateInvite))

	mux.HandleFunc("GET /v1/me", req(usersH.GetMe))
	mux.HandleFunc("PATCH /v1/me", req(usersH.PatchMe))
	mux.HandleFunc("POST /v1/me/password", req(authH.ChangePassword))
	mux.HandleFunc("GET /v1/users/{username}", req(usersH.Lookup))
	mux.HandleFunc("POST /v1/users/{username}/block", req(usersH.Block))
	mux.HandleFunc("DELETE /v1/users/{username}/block", req(usersH.Unblock))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Fail(w, r, httpx.ErrNotFound)
	})

	a.handler = httpx.AccessLog(d.Logger, securityHeaders(mux))
	return a
}

func (a *App) Handler() http.Handler { return a.handler }

// Close releases background resources (realtime hub, sweepers). The pool is owned by the caller.
func (a *App) Close() {}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// ready reports DB reachability, schema version and upload-dir writability.
func (a *App) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	v, err := db.Version(ctx, a.deps.Pool)
	if err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "check": "database"})
		return
	}
	if v != db.LatestVersion() {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "check": "migration", "migration": v})
		return
	}
	f, err := os.CreateTemp(a.deps.Config.UploadDir, ".ready-*")
	if err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "check": "uploads"})
		return
	}
	f.Close()
	os.Remove(f.Name())
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "migration": v})
}
