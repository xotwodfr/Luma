// Package files stores uploads on disk under random ids and serves each one only to members
// whose visibility window contains the message it is attached to.
package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif" // register decoders for DecodeConfig
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"luma.folk3s.com/server/internal/httpx"
)

const (
	// Decompression-bomb limits: a header may not promise more than this.
	MaxImageSide   = 12_000
	MaxImagePixels = 50_000_000
	// DailyQuota caps bytes uploaded per user per rolling 24 h, so one account can't fill
	// the host's disk (it is shared with other services).
	DailyQuota = 1 << 30
	OrphanAge  = 24 * time.Hour
)

// Inline lists the sniffed types served for display. Everything else is served as
// application/octet-stream with Content-Disposition: attachment.
var Inline = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true, "image/heic": true}

var (
	errTooLarge   = httpx.E(http.StatusRequestEntityTooLarge, "file_too_large", "Files can be at most 25 MB.")
	errBadImage   = httpx.E(http.StatusBadRequest, "invalid_image", "That image is damaged or in an unsupported layout.")
	errHugeImage  = httpx.E(http.StatusBadRequest, "image_too_large", "Images can be at most 12000 pixels on a side and 50 megapixels.")
	errQuota      = httpx.E(http.StatusTooManyRequests, "quota_exceeded", "You've uploaded a lot today. Try again tomorrow.")
	errNoFilePart = httpx.E(http.StatusBadRequest, "invalid_request", `Send the file as multipart/form-data in a field named "file".`)
)

type File struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Mime      string    `json:"mime"`
	Size      int64     `json:"size"`
	Width     *int      `json:"width,omitempty"`
	Height    *int      `json:"height,omitempty"`
	CreatedAt time.Time `json:"-"`
}

type Service struct {
	pool *pgxpool.Pool
	dir  string
	max  int64
}

func NewService(pool *pgxpool.Pool, dir string, maxBytes int64) *Service {
	return &Service{pool: pool, dir: dir, max: maxBytes}
}

// path is UPLOAD_DIR/ab/cd/<uuid>: server-chosen, never derived from user input.
func (s *Service) path(id uuid.UUID) string {
	h := id.String()
	return filepath.Join(s.dir, h[0:2], h[2:4], h)
}

// Upload streams r to disk (never more than the size cap), sniffs its type, checks image
// dimensions, fsyncs it, and only then commits the files row. me must be an active member of conv.
func (s *Service) Upload(ctx context.Context, me, conv uuid.UUID, name string, r io.Reader) (File, error) {
	if err := s.Admit(ctx, me, conv); err != nil {
		return File{}, err
	}

	tmpDir := filepath.Join(s.dir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return File{}, err
	}
	tmp, err := os.CreateTemp(tmpDir, "up-*") // mode 0600
	if err != nil {
		return File{}, err
	}
	keep := false
	defer func() {
		if !keep {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, sum), io.LimitReader(r, s.max))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return File{}, errTooLarge
	} else if err != nil {
		return File{}, err
	}
	if n == s.max { // at the cap: one more byte means too large, and it never touches the disk
		if k, _ := io.ReadFull(r, make([]byte, 1)); k > 0 {
			return File{}, errTooLarge
		}
	}

	head := make([]byte, 512)
	k, _ := tmp.ReadAt(head, 0)
	f := File{ID: uuid.New(), Name: name, Mime: sniff(head[:k]), Size: n}
	if Inline[f.Mime] {
		w, h, err := dimensions(f.Mime, tmp, head[:k])
		if err != nil {
			return File{}, err
		}
		f.Width, f.Height = &w, &h
	}

	if err := tmp.Sync(); err != nil {
		return File{}, err
	}
	if err := tmp.Close(); err != nil {
		return File{}, err
	}
	final := s.path(f.ID)
	if err := os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
		return File{}, err
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		return File{}, err
	}
	keep = true
	if d, err := os.Open(filepath.Dir(final)); err == nil { // make the rename durable before the row exists
		d.Sync()
		d.Close()
	}
	err = s.pool.QueryRow(ctx, `INSERT INTO files (id, owner_id, conversation_id, name, mime, size, width, height, sha256)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING created_at`,
		f.ID, me, conv, f.Name, f.Mime, f.Size, f.Width, f.Height, sum.Sum(nil)).Scan(&f.CreatedAt)
	if err != nil {
		os.Remove(final)
		return File{}, err
	}
	return f, nil
}

// Admit reports whether me may upload to conv now: an active member, under the daily quota.
func (s *Service) Admit(ctx context.Context, me, conv uuid.UUID) error {
	var used int64
	err := s.pool.QueryRow(ctx, `SELECT coalesce((SELECT sum(size) FROM files WHERE owner_id = $1 AND created_at > now() - interval '24 hours'), 0)
		FROM members WHERE conversation_id = $2 AND user_id = $1 AND left_at IS NULL`, me, conv).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	if used >= DailyQuota {
		return errQuota
	}
	return nil
}

// Open returns the file if me may see it: its uploader before it is attached, afterwards any
// member (current or former) whose (joined_seq, left_seq] window contains the message's seq.
// Everyone else, including a member removed before it was posted, gets not found.
func (s *Service) Open(ctx context.Context, me, id uuid.UUID) (File, *os.File, error) {
	f := File{ID: id}
	err := s.pool.QueryRow(ctx, `SELECT f.name, f.mime, f.size, f.created_at FROM files f WHERE f.id = $1 AND (
			(f.attached_at IS NULL AND f.owner_id = $2)
			OR EXISTS (SELECT 1 FROM members m JOIN conversations c ON c.id = m.conversation_id
				WHERE m.conversation_id = f.conversation_id AND m.user_id = $2
				  AND f.attached_seq > m.joined_seq AND f.attached_seq <= coalesce(m.left_seq, c.last_seq)))`,
		id, me).Scan(&f.Name, &f.Mime, &f.Size, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, nil, httpx.ErrNotFound
	} else if err != nil {
		return File{}, nil, err
	}
	fh, err := os.Open(s.path(id))
	return f, fh, err
}

// Sweep deletes uploads never attached within OrphanAge, and abandoned temp files.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `DELETE FROM files WHERE attached_at IS NULL AND created_at < now() - make_interval(secs => $1) RETURNING id`,
		OrphanAge.Seconds())
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	for _, id := range ids {
		os.Remove(s.path(id))
	}
	tmpDir := filepath.Join(s.dir, "tmp")
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
			os.Remove(filepath.Join(tmpDir, e.Name()))
		}
	}
	return len(ids), err
}

// CleanName reduces a client-supplied filename to a display name: last path element,
// valid UTF-8, no control characters, at most 255 characters.
func CleanName(n string) string {
	n = strings.ToValidUTF8(n, "")
	n = n[strings.LastIndexAny(n, `/\`)+1:]
	n = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return -1
		}
		return r
	}, n))
	for utf8.RuneCountInString(n) > 255 {
		_, size := utf8.DecodeLastRuneInString(n)
		n = n[:len(n)-size]
	}
	if n == "" || n == "." || n == ".." {
		return "file"
	}
	return n
}

// sniff decides the type from content only; the client's name and Content-Type are ignored.
func sniff(head []byte) string {
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		switch string(head[8:12]) {
		case "heic", "heix", "heim", "heis", "hevc", "hevx", "mif1":
			return "image/heic"
		}
	}
	t, _, _ := mime.ParseMediaType(http.DetectContentType(head))
	return t
}

// dimensions reads an inline image's declared size and rejects decompression bombs.
func dimensions(typ string, f *os.File, head []byte) (int, int, error) {
	var w, h int
	switch typ {
	case "image/webp":
		var ok bool
		if w, h, ok = webpSize(head); !ok {
			return 0, 0, errBadImage
		}
	case "image/heic":
		w, h = heicSize(f)
	default:
		cfg, _, err := image.DecodeConfig(io.NewSectionReader(f, 0, 1<<62))
		if err != nil {
			return 0, 0, errBadImage
		}
		w, h = cfg.Width, cfg.Height
	}
	if w <= 0 || h <= 0 {
		return 0, 0, errBadImage
	}
	if w > MaxImageSide || h > MaxImageSide || w*h > MaxImagePixels {
		return 0, 0, errHugeImage
	}
	return w, h, nil
}

// webpSize reads the canvas size from the first chunk header (lossy, lossless or extended).
func webpSize(b []byte) (int, int, bool) {
	if len(b) < 30 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0, false
	}
	switch string(b[12:16]) {
	case "VP8 ":
		if b[23] != 0x9d || b[24] != 0x01 || b[25] != 0x2a {
			return 0, 0, false
		}
		return int(binary.LittleEndian.Uint16(b[26:]) & 0x3fff), int(binary.LittleEndian.Uint16(b[28:]) & 0x3fff), true
	case "VP8L":
		if b[20] != 0x2f {
			return 0, 0, false
		}
		v := binary.LittleEndian.Uint32(b[21:])
		return int(v&0x3fff) + 1, int(v>>14&0x3fff) + 1, true
	case "VP8X":
		return 1 + (int(b[24]) | int(b[25])<<8 | int(b[26])<<16), 1 + (int(b[27]) | int(b[28])<<8 | int(b[29])<<16), true
	}
	return 0, 0, false
}

// heicSize returns the largest image-spatial-extents ("ispe") property in the file's first
// 1 MiB, where the meta box lives. Taking the maximum means extra or fake boxes can only make
// the file look bigger (and be rejected), never smaller. No ispe at all reads as 0×0: invalid.
// ponytail: a byte scan, not a box parser; clients must still apply their own decode limits.
func heicSize(f *os.File) (int, int) {
	buf := make([]byte, 1<<20)
	n, _ := f.ReadAt(buf, 0)
	buf = buf[:n]
	var w, h int
	for i := 0; ; {
		j := bytes.Index(buf[i:], []byte("ispe"))
		if j < 0 || i+j+16 > len(buf) {
			return w, h
		}
		p := i + j + 8 // skip "ispe" and version/flags
		w = max(w, int(binary.BigEndian.Uint32(buf[p:])))
		h = max(h, int(binary.BigEndian.Uint32(buf[p+4:])))
		i += j + 4
	}
}
