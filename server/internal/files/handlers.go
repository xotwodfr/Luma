package files

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/google/uuid"

	"luma.folk3s.com/server/internal/auth"
	"luma.folk3s.com/server/internal/httpx"
)

type Handlers struct {
	Svc *Service
	// UploadLimit is per user (20/minute in production).
	UploadLimit *httpx.Limiter
}

// Upload: POST /v1/files?conversation_id=… with multipart field "file". Only the first
// "file" part is stored; its filename is kept for display only.
func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	me := auth.MustPrincipal(r).UserID
	conv, err := uuid.Parse(r.URL.Query().Get("conversation_id"))
	if err != nil {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	if !h.UploadLimit.Limit(w, me.String()) {
		return
	}
	if err := h.Svc.Admit(r.Context(), me, conv); err != nil { // before reading any of the body
		httpx.Fail(w, r, err)
		return
	}
	// The server has no global read timeout (it would cut WebSockets); bound slow uploads here.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(10 * time.Minute))
	r.Body = http.MaxBytesReader(w, r.Body, h.Svc.max+1<<20) // + multipart framing
	mr, err := r.MultipartReader()
	if err != nil {
		httpx.Fail(w, r, errNoFilePart)
		return
	}
	for {
		p, err := mr.NextPart()
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.Fail(w, r, errTooLarge)
			return
		} else if err != nil {
			httpx.Fail(w, r, errNoFilePart)
			return
		}
		if p.FormName() != "file" {
			io.Copy(io.Discard, p)
			continue
		}
		f, err := h.Svc.Upload(r.Context(), me, conv, CleanName(p.FileName()), p)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, f)
		return
	}
}

// Get: GET /v1/files/{id}. Inline-safe images are served as themselves; everything else as
// an octet-stream attachment. The sandbox CSP stops anything served here from running script
// even if a client navigates to it.
func (h *Handlers) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	f, fh, err := h.Svc.Open(r.Context(), auth.MustPrincipal(r).UserID, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer fh.Close()
	typ, disp := "application/octet-stream", "attachment"
	if Inline[f.Mime] {
		typ, disp = f.Mime, "inline"
	}
	hd := w.Header()
	hd.Set("Content-Type", typ)
	if cd := mime.FormatMediaType(disp, map[string]string{"filename": f.Name}); cd != "" {
		hd.Set("Content-Disposition", cd)
	} else {
		hd.Set("Content-Disposition", disp)
	}
	hd.Set("Cache-Control", "private")
	hd.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, "", f.CreatedAt, fh)
}
