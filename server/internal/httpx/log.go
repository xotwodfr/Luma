package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"
)

type ctxKey int

const (
	keyLogger ctxKey = iota
	keyUser
)

// ClientIP returns the caller's IP. Proxy headers are honoured only when the TCP peer is
// in a trusted prefix, i.e. the request came through Caddy on this host.
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if peer, err := netip.ParseAddr(host); err == nil && len(trusted) > 0 {
		if slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(peer.Unmap()) }) {
			if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); net.ParseIP(cf) != nil {
				return cf
			}
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				first, _, _ := strings.Cut(xff, ",")
				if ip := strings.TrimSpace(first); net.ParseIP(ip) != nil {
					return ip
				}
			}
		}
	}
	return host
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(s int)           { w.status = s; w.ResponseWriter.WriteHeader(s) }
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// userSlot lets the auth middleware report the user id back to the access log.
type userSlot struct{ id string }

// SetLogUser records the authenticated user for this request's access log line.
func SetLogUser(ctx context.Context, id string) {
	if s, ok := ctx.Value(keyUser).(*userSlot); ok {
		s.id = id
	}
}

func loggerFrom(r *http.Request) *slog.Logger {
	l, _ := r.Context().Value(keyLogger).(*slog.Logger)
	return l
}

// AccessLog logs one line per request: id, method, route pattern, status, latency, user.
// It deliberately never logs bodies, query strings or headers.
func AccessLog(lg *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		id := hex.EncodeToString(b)
		slot := &userSlot{}
		ctx := context.WithValue(r.Context(), keyLogger, lg.With("req", id))
		ctx = context.WithValue(ctx, keyUser, slot)
		sw := &statusWriter{ResponseWriter: w, status: 200}
		start := time.Now()
		w.Header().Set("X-Request-Id", id)
		inner := r.WithContext(ctx) // ServeMux records the matched pattern on this copy
		next.ServeHTTP(sw, inner)
		lg.Info("request", "req", id, "method", r.Method, "route", inner.Pattern, "status", sw.status,
			"ms", time.Since(start).Milliseconds(), "user", slot.id)
	})
}
