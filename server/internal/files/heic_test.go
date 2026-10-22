package files

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func box(typ string, body []byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	return append(append(b, typ...), body...)
}

func ispe(w, h uint32) []byte {
	b := append(make([]byte, 4), 0, 0, 0, 0) // version/flags
	return box("ispe", binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(b[4:], w), h))
}

// Review finding: only the meta box counts, and it must lie inside the bytes read.
func TestHeicSize_OnlyMetaInsideWindow(t *testing.T) {
	ftyp := box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic"))
	cat := func(parts ...[]byte) []byte {
		var b []byte
		for _, p := range parts {
			b = append(b, p...)
		}
		return b
	}
	cases := []struct {
		name string
		data []byte
		w, h int
	}{
		{"meta first", cat(ftyp, box("meta", ispe(4032, 3024)), box("mdat", make([]byte, 100))), 4032, 3024},
		{"largest wins", cat(ftyp, box("meta", cat(ispe(512, 512), ispe(13000, 100)))), 13000, 512},
		{"decoy in mdat, real meta past 1 MiB", cat(ftyp, box("mdat", cat(ispe(100, 100), make([]byte, 1<<20))), box("meta", ispe(40000, 40000))), 0, 0},
		{"ispe outside any meta", cat(ftyp, ispe(100, 100)), 0, 0},
		{"box size past end", cat(ftyp, []byte("\x00\x00\xff\xffmeta")), 0, 0},
		{"truncated header", []byte("\x00\x00"), 0, 0},
	}
	for _, c := range cases {
		p := filepath.Join(t.TempDir(), "f")
		os.WriteFile(p, c.data, 0o600)
		f, _ := os.Open(p)
		w, h := heicSize(f)
		f.Close()
		if w != c.w || h != c.h {
			t.Errorf("%s: got %dx%d, want %dx%d", c.name, w, h, c.w, c.h)
		}
	}
}
