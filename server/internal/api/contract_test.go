package api_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// The OpenAPI document and the router must describe exactly the same routes.
func TestContract_RoutesDocumented(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for path, ops := range doc.Paths {
		for method := range ops {
			documented[strings.ToUpper(method)+" "+path] = true
		}
	}
	h := newHarness(t)
	registered := map[string]bool{}
	for _, r := range h.app.Routes() {
		registered[r] = true
		if !documented[r] {
			t.Errorf("route %q is not in protocol/openapi.yaml", r)
		}
	}
	for r := range documented {
		if !registered[r] {
			t.Errorf("openapi.yaml documents %q but the server has no such route", r)
		}
	}
}

type vector struct {
	Setup struct {
		Users []string `json:"users"`
		DM    []string `json:"dm"`
	} `json:"setup"`
	Steps []struct {
		Actor    string                  `json:"actor"`
		Connect  *struct{ Cursor int64 } `json:"connect"`
		Replayed []map[string]any        `json:"replayed"`
		Ready    map[string]any          `json:"ready"`
		Send     map[string]any          `json:"send"`
		Expect   map[string]any          `json:"expect"`
	} `json:"steps"`
}

// lookup resolves a dotted path ("d.message.seq") in a decoded frame.
func lookup(v any, path string) (any, bool) {
	for _, k := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		if v, ok = m[k]; !ok {
			return nil, false
		}
	}
	return v, true
}

func matches(f frame, want map[string]any, vars map[string]string) bool {
	var obj map[string]any
	b, _ := json.Marshal(f)
	_ = json.Unmarshal(b, &obj)
	for path, w := range want {
		got, ok := lookup(obj, path)
		if !ok {
			return false
		}
		if s, isStr := w.(string); isStr && strings.HasPrefix(s, "$") {
			w = vars[s[1:]]
		}
		if fmt.Sprint(got) != fmt.Sprint(w) {
			return false
		}
	}
	return true
}

func TestContract_Vectors(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/vectors/realtime-basic.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vector
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	us := map[string]user{}
	for _, n := range v.Setup.Users {
		us[n] = h.register(n)
	}
	vars := map[string]string{"dm": h.openDM(us[v.Setup.DM[0]], v.Setup.DM[1])}
	subst := func(m map[string]any) map[string]any {
		b, _ := json.Marshal(m)
		s := string(b)
		for i := 1; i <= 9; i++ {
			key := fmt.Sprintf("uuid%d", i)
			if strings.Contains(s, "$"+key) && vars[key] == "" {
				vars[key] = uuid.NewString()
			}
		}
		for k, val := range vars {
			s = strings.ReplaceAll(s, `"$`+k+`"`, `"`+val+`"`)
		}
		var out map[string]any
		_ = json.Unmarshal([]byte(s), &out)
		return out
	}
	socks := map[string]*wsClient{}
	for i, st := range v.Steps {
		switch {
		case st.Connect != nil:
			c := dialWS(t, h.wsURL(), us[st.Actor].Token, st.Connect.Cursor, dialOpts{})
			socks[st.Actor] = c
			if len(c.replayed) != len(st.Replayed) {
				t.Fatalf("step %d: replayed %d frames, vector says %d", i, len(c.replayed), len(st.Replayed))
			}
			for j, want := range st.Replayed {
				if !matches(c.replayed[j], want, vars) {
					t.Fatalf("step %d: replayed[%d] %s does not match %v", i, j, c.replayed[j].D, want)
				}
			}
			if c.cursor != int64(st.Ready["d.cursor"].(float64)) {
				t.Fatalf("step %d: ready cursor %d", i, c.cursor)
			}
		case st.Send != nil:
			m := subst(st.Send)
			socks[st.Actor].write(m["t"].(string), fmt.Sprint(m["id"]), m["d"])
		case st.Expect != nil:
			want := st.Expect
			socks[st.Actor].expect(func(f frame) bool { return matches(f, want, vars) }, 3*time.Second)
		}
	}
}
