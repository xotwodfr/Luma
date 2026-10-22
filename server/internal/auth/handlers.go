package auth

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"luma.folk3s.com/server/internal/httpx"
)

// Handlers exposes the auth service over HTTP. Limit throttles credential endpoints per client IP.
type Handlers struct {
	Svc          *Service
	Limit        *httpx.Limiter
	TrustedProxy bool
}

func (h *Handlers) device(r *http.Request, name, platform string) Device {
	return Device{Name: name, Platform: platform, IP: httpx.ClientIP(r, h.TrustedProxy)}
}

type authResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	if !h.Limit.Limit(w, "auth:"+httpx.ClientIP(r, h.TrustedProxy)) {
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
	if !h.Limit.Limit(w, "auth:"+httpx.ClientIP(r, h.TrustedProxy)) {
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
	token, u, err := h.Svc.Login(r.Context(), in.Username, in.Password, h.device(r, in.DeviceName, in.Platform))
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
