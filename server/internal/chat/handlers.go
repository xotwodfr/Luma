package chat

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"luma.folk3s.com/server/internal/auth"
	"luma.folk3s.com/server/internal/httpx"
)

// Handlers exposes conversations over REST. SendLimit and OpenLimit are per-user buckets.
type Handlers struct {
	Svc       *Service
	SendLimit *httpx.Limiter
	OpenLimit *httpx.Limiter
}

func convID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	me := auth.MustPrincipal(r).UserID
	var in struct {
		Kind     string `json:"kind"`
		Username string `json:"username"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.Kind != "direct" {
		httpx.Fail(w, r, httpx.E(http.StatusBadRequest, "invalid_request", `Only kind "direct" is supported.`))
		return
	}
	if !h.OpenLimit.Limit(w, me.String()) {
		return
	}
	c, err := h.Svc.OpenDirect(r.Context(), me, in.Username)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.Svc.List(r.Context(), auth.MustPrincipal(r).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"conversations": list})
}

func (h *Handlers) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := convID(w, r)
	if !ok {
		return
	}
	c, err := h.Svc.Get(r.Context(), auth.MustPrincipal(r).UserID, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *Handlers) History(w http.ResponseWriter, r *http.Request) {
	id, ok := convID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	num := func(k string, def int64) (int64, bool) {
		if q.Get(k) == "" {
			return def, true
		}
		v, err := strconv.ParseInt(q.Get(k), 10, 64)
		return v, err == nil
	}
	before, ok1 := num("before_seq", 0)
	after, ok2 := num("after_seq", 0)
	limit, ok3 := num("limit", 50)
	if !ok1 || !ok2 || !ok3 {
		httpx.Fail(w, r, errBadPage)
		return
	}
	msgs, err := h.Svc.History(r.Context(), auth.MustPrincipal(r).UserID, id, before, after, int(limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (h *Handlers) Send(w http.ResponseWriter, r *http.Request) {
	id, ok := convID(w, r)
	if !ok {
		return
	}
	me := auth.MustPrincipal(r).UserID
	if !h.SendLimit.Limit(w, me.String()) {
		return
	}
	var in SendInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	in.ConversationID = id
	m, created, err := h.Svc.Send(r.Context(), me, in)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, m)
}

func (h *Handlers) Read(w http.ResponseWriter, r *http.Request) {
	id, ok := convID(w, r)
	if !ok {
		return
	}
	var in struct {
		Seq int64 `json:"seq"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	seq, err := h.Svc.MarkRead(r.Context(), auth.MustPrincipal(r).UserID, id, in.Seq)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int64{"last_read_seq": seq})
}
