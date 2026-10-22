package users

import (
	"net/http"

	"luma.folk3s.com/server/internal/auth"
	"luma.folk3s.com/server/internal/httpx"
)

type Handlers struct{ Svc *Service }

func (h *Handlers) GetMe(w http.ResponseWriter, r *http.Request) {
	me, err := h.Svc.Me(r.Context(), auth.MustPrincipal(r).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, me)
}

func (h *Handlers) PatchMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DisplayName *string `json:"display_name"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.DisplayName == nil {
		httpx.Fail(w, r, httpx.E(http.StatusBadRequest, "invalid_request", "Nothing to update."))
		return
	}
	me, err := h.Svc.UpdateDisplayName(r.Context(), auth.MustPrincipal(r).UserID, *in.DisplayName)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, me)
}

func (h *Handlers) Lookup(w http.ResponseWriter, r *http.Request) {
	p, err := h.Svc.Lookup(r.Context(), auth.MustPrincipal(r).UserID, r.PathValue("username"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, p)
}

func (h *Handlers) Block(w http.ResponseWriter, r *http.Request) {
	if err := h.Svc.Block(r.Context(), auth.MustPrincipal(r).UserID, r.PathValue("username")); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) Unblock(w http.ResponseWriter, r *http.Request) {
	if err := h.Svc.Unblock(r.Context(), auth.MustPrincipal(r).UserID, r.PathValue("username")); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
