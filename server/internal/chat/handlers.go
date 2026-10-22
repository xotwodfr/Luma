package chat

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"luma.folk3s.com/server/internal/auth"
	"luma.folk3s.com/server/internal/httpx"
)

// Handlers exposes conversations over REST. SendLimit, OpenLimit (new DMs only) and LookupLimit are per-user buckets.
type Handlers struct {
	Svc       *Service
	SendLimit *httpx.Limiter
	OpenLimit *httpx.Limiter
	// LookupLimit throttles username probing (shared with GET /v1/users/{username}).
	LookupLimit *httpx.Limiter
	// GroupLimit counts group creations; AddLimit counts people added to groups (create or add).
	GroupLimit, AddLimit *httpx.Limiter
	SearchLimit          *httpx.Limiter
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
		Kind      string   `json:"kind"`
		Username  string   `json:"username"`
		Title     string   `json:"title"`
		Usernames []string `json:"usernames"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.Kind == "group" {
		if !h.GroupLimit.Limit(w, me.String()) || !h.AddLimit.LimitN(w, me.String(), len(in.Usernames)) {
			return
		}
		c, err := h.Svc.CreateGroup(r.Context(), me, in.Title, in.Usernames)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, c)
		return
	}
	if in.Kind != "direct" {
		httpx.Fail(w, r, httpx.E(http.StatusBadRequest, "invalid_request", `kind must be "direct" or "group".`))
		return
	}
	if !h.LookupLimit.Limit(w, me.String()) {
		return
	}
	c, err := h.Svc.OpenDirect(r.Context(), me, in.Username, func() error {
		if ok, wait := h.OpenLimit.Allow(me.String()); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			return httpx.E(http.StatusTooManyRequests, "rate_limited", "You've started a lot of new conversations today. Try again later.")
		}
		return nil
	})
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

func targetID(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	conv, ok := convID(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	u, err := uuid.Parse(r.PathValue("user_id"))
	if err != nil {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return uuid.Nil, uuid.Nil, false
	}
	return conv, u, true
}

func reply(w http.ResponseWriter, r *http.Request, c Conversation, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *Handlers) AddMembers(w http.ResponseWriter, r *http.Request) {
	id, ok := convID(w, r)
	if !ok {
		return
	}
	me := auth.MustPrincipal(r).UserID
	var in struct {
		Usernames []string `json:"usernames"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !h.AddLimit.LimitN(w, me.String(), len(in.Usernames)) {
		return
	}
	c, err := h.Svc.AddMembers(r.Context(), me, id, in.Usernames)
	reply(w, r, c, err)
}

func (h *Handlers) RemoveMember(w http.ResponseWriter, r *http.Request) {
	conv, target, ok := targetID(w, r)
	if !ok {
		return
	}
	c, err := h.Svc.RemoveMember(r.Context(), auth.MustPrincipal(r).UserID, conv, target)
	reply(w, r, c, err)
}

func (h *Handlers) SetRole(w http.ResponseWriter, r *http.Request) {
	conv, target, ok := targetID(w, r)
	if !ok {
		return
	}
	var in struct {
		Role string `json:"role"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	c, err := h.Svc.SetRole(r.Context(), auth.MustPrincipal(r).UserID, conv, target, in.Role)
	reply(w, r, c, err)
}

func (h *Handlers) Leave(w http.ResponseWriter, r *http.Request) {
	id, ok := convID(w, r)
	if !ok {
		return
	}
	if err := h.Svc.Leave(r.Context(), auth.MustPrincipal(r).UserID, id); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) Rename(w http.ResponseWriter, r *http.Request) {
	id, ok := convID(w, r)
	if !ok {
		return
	}
	var in struct {
		Title string `json:"title"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	c, err := h.Svc.Rename(r.Context(), auth.MustPrincipal(r).UserID, id, in.Title)
	reply(w, r, c, err)
}

func (h *Handlers) Settings(w http.ResponseWriter, r *http.Request) {
	id, ok := convID(w, r)
	if !ok {
		return
	}
	var in SettingsInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	c, err := h.Svc.UpdateSettings(r.Context(), auth.MustPrincipal(r).UserID, id, in)
	reply(w, r, c, err)
}

// Search: GET /v1/search?q=&conversation_id=&limit= (30/minute per user).
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	me := auth.MustPrincipal(r).UserID
	if !h.SearchLimit.Limit(w, me.String()) {
		return
	}
	q := r.URL.Query()
	var conv *uuid.UUID
	if v := q.Get("conversation_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			httpx.Fail(w, r, httpx.ErrNotFound)
			return
		}
		conv = &id
	}
	limit := 20
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			httpx.Fail(w, r, errBadSearch)
			return
		}
		limit = n
	}
	res, err := h.Svc.Search(r.Context(), me, q.Get("q"), conv, limit)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"results": res})
}
