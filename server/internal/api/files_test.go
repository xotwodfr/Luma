package api_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const testCap = 1 << 20 // harness MaxUploadBytes

// upload posts data as multipart field "file" with the given (untrusted) name and Content-Type.
func (h *harness) upload(u user, conv, name, ctype string, data []byte) resp {
	h.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="`+name+`"`)
	hdr.Set("Content-Type", ctype)
	pw, _ := mw.CreatePart(hdr)
	pw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", h.srv.URL+"/v1/files?conversation_id="+conv, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+u.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{Status: res.StatusCode, Header: res.Header, Raw: raw}
	_ = json.Unmarshal(raw, &out.Body)
	return out
}

func (h *harness) mustUpload(u user, conv, name string, data []byte) string {
	h.t.Helper()
	r := h.upload(u, conv, name, "application/octet-stream", data)
	if r.Status != 201 {
		h.t.Fatalf("upload %s: %d %s", name, r.Status, r.Raw)
	}
	return r.Body["id"].(string)
}

func (h *harness) sendFile(u user, conv, fileID string) resp {
	h.t.Helper()
	return h.do("POST", "/v1/conversations/"+conv+"/messages", u.Token, map[string]any{
		"client_id": uuid.NewString(), "kind": "file", "body": "", "meta": map[string]any{"file_id": fileID}})
}

func (h *harness) group(owner user, members ...string) string {
	h.t.Helper()
	r := h.do("POST", "/v1/conversations", owner.Token, map[string]any{"kind": "group", "title": "G", "usernames": members})
	if r.Status != 201 {
		h.t.Fatalf("group: %d %s", r.Status, r.Raw)
	}
	return r.Body["id"].(string)
}

func pngBytes(w, h int) []byte {
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

// pngClaiming is a valid tiny PNG whose IHDR (with a correct CRC) claims w×h.
func pngClaiming(w, h uint32) []byte {
	b := pngBytes(1, 1)
	binary.BigEndian.PutUint32(b[16:], w)
	binary.BigEndian.PutUint32(b[20:], h)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	return b
}

// diskFiles lists every regular file under the upload dir with its size.
func (h *harness) diskFiles() map[string]int64 {
	out := map[string]int64{}
	filepath.WalkDir(h.uploads, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			info, _ := d.Info()
			out[p] = info.Size()
		}
		return nil
	})
	return out
}

func TestFiles_SizeCapEnforcedWhileStreaming(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	h.register("bob")
	conv := h.openDM(a, "bob")
	if r := h.upload(a, conv, "exact.bin", "", bytes.Repeat([]byte{1}, testCap)); r.Status != 201 {
		t.Fatalf("exactly the cap: %d %s", r.Status, r.Raw)
	}
	for _, n := range []int{testCap + 1, 5 * testCap} {
		if r := h.upload(a, conv, "big.bin", "", bytes.Repeat([]byte{2}, n)); r.Status != 413 || r.errCode() != "file_too_large" {
			t.Fatalf("%d bytes: %d %s", n, r.Status, r.Raw)
		}
	}
	files := h.diskFiles()
	if len(files) != 1 {
		t.Fatalf("rejected uploads left files behind: %v", files)
	}
	for p, size := range files {
		if size > testCap {
			t.Fatalf("%s is %d bytes, over the cap", p, size)
		}
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: mode %v (%v), want 0600", p, st.Mode().Perm(), err)
		}
	}
}

// Review focus 4: the sniffed type wins over the name and the declared type; only the
// allowlisted image types are ever served inline.
func TestFiles_SniffedTypeWins(t *testing.T) {
	h := newHarness(t)
	a, b := h.register("alice"), h.register("bob")
	conv := h.openDM(a, "bob")
	cases := []struct {
		name, declared string
		data           []byte
		mime, served   string
		inline         bool
	}{
		{"evil.png", "image/png", []byte("<html><script>alert(document.cookie)</script></html>"), "text/html", "application/octet-stream", false},
		{"logo.svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`), "text/plain", "application/octet-stream", false},
		{"polyglot.png", "image/png", append(pngBytes(2, 2), []byte("<html><script>alert(1)</script>")...), "image/png", "image/png", true},
		{"photo.txt", "text/plain", pngBytes(3, 4), "image/png", "image/png", true},
		{"anim.gif", "image/gif", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00<html><script>alert(1)</script>"), "image/gif", "image/gif", true},
		{"report.pdf", "application/pdf", []byte("%PDF-1.7\n..."), "application/pdf", "application/octet-stream", false},
	}
	for _, c := range cases {
		up := h.upload(a, conv, c.name, c.declared, c.data)
		if up.Status != 201 || up.Body["mime"] != c.mime {
			t.Fatalf("%s: %d mime=%v, want %s (%s)", c.name, up.Status, up.Body["mime"], c.mime, up.Raw)
		}
		id := up.Body["id"].(string)
		if r := h.sendFile(a, conv, id); r.Status != 201 {
			t.Fatalf("%s: attach %d %s", c.name, r.Status, r.Raw)
		}
		r := h.do("GET", "/v1/files/"+id, b.Token, nil)
		disp := r.Header.Get("Content-Disposition")
		if r.Status != 200 || r.Header.Get("Content-Type") != c.served || strings.HasPrefix(disp, "inline") != c.inline ||
			!strings.Contains(disp, c.name) || r.Header.Get("X-Content-Type-Options") != "nosniff" ||
			!strings.Contains(r.Header.Get("Content-Security-Policy"), "sandbox") || r.Header.Get("Cache-Control") != "private" ||
			!bytes.Equal(r.Raw, c.data) {
			t.Fatalf("%s served as %d type=%q disposition=%q csp=%q cache=%q", c.name, r.Status, r.Header.Get("Content-Type"),
				disp, r.Header.Get("Content-Security-Policy"), r.Header.Get("Cache-Control"))
		}
	}
	// A file that sniffs as an image but doesn't parse is refused rather than served inline.
	if r := h.upload(a, conv, "x.gif", "image/gif", []byte("GIF89a")); r.Status != 400 || r.errCode() != "invalid_image" {
		t.Fatalf("broken gif: %d %s", r.Status, r.Raw)
	}
	// Path tricks in the name are reduced to a display name; storage paths never use it.
	up := h.upload(a, conv, `../../etc/passwd`, "", []byte("hello"))
	if up.Status != 201 || up.Body["name"] != "passwd" {
		t.Fatalf("name cleaning: %d %s", up.Status, up.Raw)
	}
}

func TestFiles_DecompressionBombRejected(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	h.register("bob")
	conv := h.openDM(a, "bob")
	webpX := func(w, ht int) []byte {
		b := make([]byte, 40)
		copy(b, "RIFF\x20\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00")
		w, ht = w-1, ht-1
		b[24], b[25], b[26] = byte(w), byte(w>>8), byte(w>>16)
		b[27], b[28], b[29] = byte(ht), byte(ht>>8), byte(ht>>16)
		return b
	}
	ispeBoxes := func(ispe ...uint32) []byte {
		var b []byte
		for i := 0; i+1 < len(ispe); i += 2 {
			box := make([]byte, 20)
			copy(box, "\x00\x00\x00\x14ispe")
			binary.BigEndian.PutUint32(box[12:], ispe[i])
			binary.BigEndian.PutUint32(box[16:], ispe[i+1])
			b = append(b, box...)
		}
		return b
	}
	box := func(typ string, body []byte) []byte {
		b := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
		return append(append(b, typ...), body...)
	}
	ftyp := box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic"))
	heic := func(ispe ...uint32) []byte { return append(slices.Clone(ftyp), box("meta", ispeBoxes(ispe...))...) }

	cases := []struct {
		name string
		data []byte
		code string
	}{
		{"wide.png", pngClaiming(20000, 10), "image_too_large"},
		{"pixels.png", pngClaiming(10000, 6000), "image_too_large"}, // 60 MP, each side under 12 000
		{"big.webp", webpX(16000, 16000), "image_too_large"},
		{"big.heic", heic(512, 512, 13000, 100), "image_too_large"}, // the largest ispe counts
		{"noextent.heic", heic(), "invalid_image"},
		{"nometa.heic", append(slices.Clone(ftyp), ispeBoxes(100, 100)...), "invalid_image"},
		{"zero.png", pngClaiming(0, 10), "invalid_image"},
	}
	for _, c := range cases {
		if r := h.upload(a, conv, c.name, "image/png", c.data); r.Status != 400 || r.errCode() != c.code {
			t.Fatalf("%s: %d %s, want %s", c.name, r.Status, r.Raw, c.code)
		}
	}
	for _, ok := range []struct {
		name string
		data []byte
		w, h float64
	}{{"edge.png", pngClaiming(12000, 4000), 12000, 4000}, {"ok.webp", webpX(800, 600), 800, 600}, {"ok.heic", heic(4032, 3024), 4032, 3024}} {
		r := h.upload(a, conv, ok.name, "", ok.data)
		if r.Status != 201 || r.Body["width"] != ok.w || r.Body["height"] != ok.h {
			t.Fatalf("%s: %d %s", ok.name, r.Status, r.Raw)
		}
	}
	if left := h.diskFiles(); len(left) != 3 {
		t.Fatalf("rejected images left files on disk: %v", left)
	}
}

// Review focus 1: a member removed from a group can still fetch what was posted while they
// were in it, and gets 404 for anything posted after (or while they were away).
func TestFiles_RemovedMemberCannotFetchLaterFile(t *testing.T) {
	h := newHarness(t)
	a, b, c, m := h.register("alice"), h.register("bob"), h.register("carol"), h.register("mallory")
	g := h.group(a, "bob", "carol")
	before := h.mustUpload(a, g, "before.txt", []byte("for everyone"))
	h.sendFile(a, g, before)
	h.do("DELETE", "/v1/conversations/"+g+"/members/"+c.ID, a.Token, nil)
	after := h.mustUpload(a, g, "after.txt", []byte("not for carol"))
	if r := h.sendFile(a, g, after); r.Status != 201 {
		t.Fatalf("attach: %d %s", r.Status, r.Raw)
	}
	get := func(u user, id string) int { return h.do("GET", "/v1/files/"+id, u.Token, nil).Status }
	if get(c, before) != 200 || get(c, after) != 404 {
		t.Fatalf("removed carol: before=%d after=%d", get(c, before), get(c, after))
	}
	if get(b, after) != 200 || get(m, before) != 404 || h.do("GET", "/v1/files/"+before, "", nil).Status != 401 {
		t.Fatal("member / non-member / anonymous access wrong")
	}
	// Re-added: the gap stays hidden, and so does her old window (a new window starts).
	h.do("POST", "/v1/conversations/"+g+"/members", a.Token, map[string]any{"usernames": []string{"carol"}})
	if get(c, after) != 404 || get(c, before) != 404 {
		t.Fatalf("rejoined carol: before=%d after=%d", get(c, before), get(c, after))
	}
	// A member added later can't fetch earlier files.
	h.register("dave")
	d := h.login("dave", "password-dave").Body["token"].(string)
	h.do("POST", "/v1/conversations/"+g+"/members", a.Token, map[string]any{"usernames": []string{"dave"}})
	if r := h.do("GET", "/v1/files/"+after, d, nil); r.Status != 404 {
		t.Fatalf("late joiner fetched an earlier file: %d", r.Status)
	}
	// Unattached uploads are visible to their uploader only.
	draft := h.mustUpload(a, g, "draft.txt", []byte("draft"))
	if get(a, draft) != 200 || get(b, draft) != 404 {
		t.Fatalf("unattached: owner=%d member=%d", get(a, draft), get(b, draft))
	}
	if r := h.upload(m, g, "x.txt", "", []byte("x")); r.Status != 404 {
		t.Fatalf("non-member upload: %d", r.Status)
	}
	if r := h.upload(c, g, "x.txt", "", []byte("x")); r.Status != 201 {
		t.Fatalf("rejoined member upload: %d", r.Status)
	}
}

func TestFiles_AttachRules(t *testing.T) {
	h := newHarness(t)
	a, b := h.register("alice"), h.register("bob")
	h.register("carol")
	ab := h.openDM(a, "bob")
	ac := h.openDM(a, "carol")
	id := h.mustUpload(a, ab, "a.txt", []byte("hello"))
	if r := h.sendFile(a, ac, id); r.Status != 400 || r.errCode() != "invalid_attachment" {
		t.Fatalf("another conversation's file: %d %s", r.Status, r.Raw)
	}
	if r := h.sendFile(b, ab, id); r.Status != 400 {
		t.Fatalf("someone else's upload: %d %s", r.Status, r.Raw)
	}
	for _, meta := range []map[string]any{{"file_id": id, "name": "spoofed.exe"}, {"file_id": id, "mime": "image/png"}, {}, {"file_id": "nope"}} {
		r := h.do("POST", "/v1/conversations/"+ab+"/messages", a.Token, map[string]any{"client_id": uuid.NewString(), "kind": "file", "body": "", "meta": meta})
		if r.Status != 400 {
			t.Fatalf("client meta %v accepted: %d", meta, r.Status)
		}
	}
	cid := uuid.NewString()
	send := func() resp {
		return h.do("POST", "/v1/conversations/"+ab+"/messages", a.Token, map[string]any{"client_id": cid, "kind": "file", "body": "caption", "meta": map[string]any{"file_id": id}})
	}
	first := send()
	meta, _ := first.Body["meta"].(map[string]any)
	if first.Status != 201 || meta["name"] != "a.txt" || meta["mime"] != "text/plain" || meta["size"] != float64(5) || first.Body["body"] != "caption" {
		t.Fatalf("attach: %d %s", first.Status, first.Raw)
	}
	if retry := send(); retry.Status != 200 || retry.Body["id"] != first.Body["id"] {
		t.Fatalf("retry with the same client_id: %d %s", retry.Status, retry.Raw)
	}
	if r := h.sendFile(a, ab, id); r.Status != 400 {
		t.Fatalf("attaching a file twice: %d %s", r.Status, r.Raw)
	}
	if r := h.do("POST", "/v1/conversations/"+ab+"/messages", a.Token, map[string]any{"client_id": uuid.NewString(), "kind": "text", "body": "x", "meta": map[string]any{"file_id": id}}); r.Status != 400 {
		t.Fatalf("text with client meta: %d", r.Status)
	}
	if r := h.do("POST", "/v1/files?conversation_id="+ab, a.Token, map[string]any{"x": 1}); r.Status != 400 {
		t.Fatalf("non-multipart upload: %d", r.Status)
	}
}

func TestFiles_OrphanSweep(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	h.register("bob")
	conv := h.openDM(a, "bob")
	old := h.mustUpload(a, conv, "old.txt", []byte("old"))
	oldAttached := h.mustUpload(a, conv, "kept.txt", []byte("kept"))
	h.sendFile(a, conv, oldAttached)
	fresh := h.mustUpload(a, conv, "fresh.txt", []byte("fresh"))
	ctx := context.Background()
	h.pool.Exec(ctx, `UPDATE files SET created_at = now() - interval '25 hours' WHERE id = ANY($1::uuid[])`, []string{old, oldAttached})
	n, err := h.app.Files.Sweep(ctx)
	if err != nil || n != 1 {
		t.Fatalf("sweep: n=%d err=%v", n, err)
	}
	if h.do("GET", "/v1/files/"+old, a.Token, nil).Status != 404 {
		t.Fatal("swept file still served")
	}
	for _, id := range []string{oldAttached, fresh} {
		if r := h.do("GET", "/v1/files/"+id, a.Token, nil); r.Status != 200 {
			t.Fatalf("%s: %d", id, r.Status)
		}
	}
	if left := h.diskFiles(); len(left) != 2 {
		t.Fatalf("disk after sweep: %v", left)
	}
}

func TestFiles_DailyQuota(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	h.register("bob")
	conv := h.openDM(a, "bob")
	h.pool.Exec(context.Background(), `INSERT INTO files (owner_id, conversation_id, name, mime, size, sha256) VALUES ($1, $2, 'big', 'x', $3, '\x00')`,
		a.ID, conv, int64(1<<30))
	if r := h.upload(a, conv, "one.txt", "", []byte("more")); r.Status != 429 || r.errCode() != "quota_exceeded" {
		t.Fatalf("over quota: %d %s", r.Status, r.Raw)
	}
	// Just under the quota: Admit passes, but the size is checked again when the row commits.
	h.pool.Exec(context.Background(), `UPDATE files SET size = $2 WHERE owner_id = $1`, a.ID, int64(1<<30-3))
	if r := h.upload(a, conv, "five.txt", "", []byte("12345")); r.Status != 429 {
		t.Fatalf("upload crossing the quota: %d %s", r.Status, r.Raw)
	}
	if left := h.diskFiles(); len(left) != 0 {
		t.Fatalf("refused upload left %v on disk", left)
	}
	if r := h.upload(a, conv, "three.txt", "", []byte("123")); r.Status != 201 {
		t.Fatalf("upload exactly reaching the quota: %d %s", r.Status, r.Raw)
	}
}
