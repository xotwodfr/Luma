// Package httpx holds the HTTP plumbing shared by every Luma handler:
// error shape, bounded JSON decoding, rate limiting, client IP and request logging.
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// Error is a client-facing failure. Services return it; handlers write it with Fail.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func E(status int, code, msg string) *Error { return &Error{status, code, msg} }

var (
	ErrUnauthorized = E(http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
	ErrNotFound     = E(http.StatusNotFound, "not_found", "Not found.")
	ErrInternal     = E(http.StatusInternalServerError, "internal", "Something went wrong on the server.")
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func WriteError(w http.ResponseWriter, status int, code, msg string) {
	WriteJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

// Fail writes err as a client error, or logs it and writes a generic 500. Internal
// details (SQL, stack traces) never reach the client.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var e *Error
	if errors.As(err, &e) {
		WriteError(w, e.Status, e.Code, e.Message)
		return
	}
	lg := loggerFrom(r)
	if lg == nil {
		lg = slog.Default()
	}
	lg.Error("internal error", "err", err, "route", r.Pattern)
	WriteError(w, ErrInternal.Status, ErrInternal.Code, ErrInternal.Message)
}
