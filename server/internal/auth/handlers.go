package auth

import (
	"net/http"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"luma.folk3s.com/server/internal/httpx"
)

// Handlers exposes the auth service over HTTP. Limit throttles credential endpoints per client IP;
// AccountLimit throttles login attempts per username from addresses new to that account, so
// guessing one password stays slow even if client IPs are spoofed or rotated; known addresses
// have their own larger bucket. Unknown usernames behave like known ones from new addresses.
type Handlers struct {
	Svc          *Service
	Limit        *httpx.Limiter
	AccountLimit *httpx.Limiter
	// KnownAccountLimit is the per-(username, address) bucket for addresses that held a session before.
	KnownAccountLimit *httpx.Limiter
	TrustedProxies    []netip.Prefix
}

func (h *Handlers) device(r *http.Request, name, platform string) Device {
	return Device{Name: name, Platform: platform, IP: httpx.ClientIP(r, h.TrustedProxies)}
}

type authResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	if !h.Limit.Limit(w, "auth:"+httpx.ClientIP(r, h.TrustedProxies)) {
		return
	}
	var in RegisterInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	token, u, err := h.Svc.Register(r.Context(), in, h.device(r, in.DeviceName, in.Platform))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, authResponse{token, u})
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	if !h.Limit.Limit(w, "auth:"+httpx.ClientIP(r, h.TrustedProxies)) {
		return
	}
	var in struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		DeviceName string `json:"device_name"`
		Platform   string `json:"platform"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	dev := h.device(r, in.DeviceName, in.Platform)
	// A strict per-username bucket for addresses that never held a session for this account, and
	// a larger but finite bucket per (username, known address). Strangers can't lock the owner out
	// of their usual networks, and a forged "known" IP still can't guess without limit.
	known, err := h.Svc.KnownIP(r.Context(), in.Username, dev.IP)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	bucket, key := h.AccountLimit, NormalizeUsername(in.Username)
	if known { // per (account, address): draining one known address doesn't block the others
		bucket, key = h.KnownAccountLimit, key+"|"+dev.IP
	}
	if !bucket.Limit(w, key) {
		return
	}
	token, u, err := h.Svc.Login(r.Context(), in.Username, in.Password, dev)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, authResponse{token, u})
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Svc.Logout(r.Context(), MustPrincipal(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) Sessions(w http.ResponseWriter, r *http.Request) {
	list, err := h.Svc.ListSessions(r.Context(), MustPrincipal(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

func (h *Handlers) RevokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	if err := h.Svc.RevokeSession(r.Context(), MustPrincipal(r), id); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) RevokeOthers(w http.ResponseWriter, r *http.Request) {
	if err := h.Svc.RevokeOthers(r.Context(), MustPrincipal(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	p := MustPrincipal(r)
	if !h.Limit.Limit(w, "pw:"+p.UserID.String()) {
		return
	}
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := h.Svc.ChangePassword(r.Context(), p, in.Current, in.New); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) CreateInvite(w http.ResponseWriter, r *http.Request) {
	code, exp, err := h.Svc.CreateInvite(r.Context(), MustPrincipal(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
	}{code, exp})
}
