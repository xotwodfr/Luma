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
	"luma.folk3s.com/server/internal/chat"
	"luma.folk3s.com/server/internal/config"
	"luma.folk3s.com/server/internal/db"
	"luma.folk3s.com/server/internal/httpx"
	"luma.folk3s.com/server/internal/realtime"
	"luma.folk3s.com/server/internal/users"
)

type Deps struct {
	Pool   *pgxpool.Pool
	Config config.Config
	Logger *slog.Logger
	// AuthPerMinute caps register/login attempts per client IP. 0 means the production default of 10.
	AuthPerMinute int
	// SendPer10s caps message sends per user (REST + WebSocket). 0 means the production default of 30.
	SendPer10s int
	// HelloTimeout bounds unauthenticated WebSocket connections. 0 means 10s.
	HelloTimeout time.Duration
}

// App holds the services so tests and the admin CLI can reach them directly.
type App struct {
	Auth    *auth.Service
	Users   *users.Service
	Chat    *chat.Service
	Hub     *realtime.Hub
	deps    Deps
	handler http.Handler
	routes  []string
}

// Routes lists every registered "METHOD /pattern" (used by the authorization-matrix coverage test).
func (a *App) Routes() []string { return a.routes }

func New(d Deps) *App {
	a := &App{
		Auth:  auth.NewService(d.Pool, d.Config.Registration),
		Users: users.NewService(d.Pool),
		Chat:  chat.NewService(d.Pool, nil),
		deps:  d,
	}
	if d.AuthPerMinute == 0 {
		d.AuthPerMinute = 10
	}
	authH := &auth.Handlers{Svc: a.Auth, Limit: httpx.NewLimiter(d.AuthPerMinute, time.Minute), TrustedProxy: d.Config.TrustedProxy}
	usersH := &users.Handlers{Svc: a.Users}
	// One send bucket per user, shared by REST and WebSocket so neither path bypasses it.
	if d.SendPer10s == 0 {
		d.SendPer10s = 30
	}
	sendLimit := httpx.NewLimiter(d.SendPer10s, 10*time.Second)
	chatH := &chat.Handlers{Svc: a.Chat, SendLimit: sendLimit, OpenLimit: httpx.NewLimiter(20, 24*time.Hour)}
	a.Hub = realtime.NewHub(realtime.Options{
		Chat: a.Chat, Auth: a.Auth, Pool: d.Pool, Logger: d.Logger, SendLimit: sendLimit, HelloTimeout: d.HelloTimeout,
	})
	a.Chat.SetNotifier(a.Hub)
	a.Auth.OnRevoke = a.Hub.CloseSession
	req := a.Auth.Require

	mux := http.NewServeMux()
	handle := func(pattern string, h http.HandlerFunc) {
		a.routes = append(a.routes, pattern)
		mux.HandleFunc(pattern, h)
	}
	handle("GET /v1/health/live", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	handle("GET /v1/health/ready", a.ready)

	handle("POST /v1/auth/register", authH.Register)
	handle("POST /v1/auth/login", authH.Login)
	handle("POST /v1/auth/logout", req(authH.Logout))
	handle("GET /v1/auth/sessions", req(authH.Sessions))
	handle("DELETE /v1/auth/sessions/{id}", req(authH.RevokeSession))
	handle("POST /v1/auth/sessions/revoke-others", req(authH.RevokeOthers))
	handle("POST /v1/invites", req(authH.CreateInvite))

	handle("GET /v1/me", req(usersH.GetMe))
	handle("PATCH /v1/me", req(usersH.PatchMe))
	handle("POST /v1/me/password", req(authH.ChangePassword))
	handle("GET /v1/users/{username}", req(usersH.Lookup))
	handle("POST /v1/users/{username}/block", req(usersH.Block))
	handle("DELETE /v1/users/{username}/block", req(usersH.Unblock))

	handle("POST /v1/conversations", req(chatH.Create))
	handle("GET /v1/conversations", req(chatH.List))
	handle("GET /v1/conversations/{id}", req(chatH.Get))
	handle("GET /v1/conversations/{id}/messages", req(chatH.History))
	handle("POST /v1/conversations/{id}/messages", req(chatH.Send))
	handle("POST /v1/conversations/{id}/read", req(chatH.Read))
	handle("GET /v1/realtime", a.Hub.ServeHTTP)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Fail(w, r, httpx.ErrNotFound)
	})

	a.handler = httpx.AccessLog(d.Logger, securityHeaders(mux))
	return a
}

func (a *App) Handler() http.Handler { return a.handler }

// Close releases background resources (realtime hub, sweepers). The pool is owned by the caller.
func (a *App) Close() { a.Hub.Close() }

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
