package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"luma.folk3s.com/server/internal/api"
	"luma.folk3s.com/server/internal/auth"
	"luma.folk3s.com/server/internal/config"
	"luma.folk3s.com/server/internal/testdb"
)

func TestMain(m *testing.M) {
	auth.UseParamsForTesting(auth.Params{Memory: 1024, Time: 1, Threads: 1})
	os.Exit(m.Run())
}

type harness struct {
	t    *testing.T
	srv  *httptest.Server
	app  *api.App
	pool *pgxpool.Pool
	logs *bytes.Buffer
}

// newHarness uses a generous auth rate limit; productionLimits restores the real one.
func newHarness(t *testing.T, opts ...func(*api.Deps)) *harness {
	t.Helper()
	pool := testdb.New(t)
	logs := &bytes.Buffer{}
	cfg := config.Config{Registration: "invite", UploadDir: t.TempDir(), MaxUploadBytes: 1 << 20, PublicURL: "http://test"}
	deps := api.Deps{Pool: pool, Config: cfg, Logger: slog.New(slog.NewJSONHandler(logs, nil)), AuthPerMinute: 1000, SendPer10s: 1000}
	for _, o := range opts {
		o(&deps)
	}
	app := api.New(deps)
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() { srv.Close(); app.Close() })
	return &harness{t: t, srv: srv, app: app, pool: pool, logs: logs}
}

type resp struct {
	Status int
	Header http.Header
	Body   map[string]any
	Raw    []byte
}

func (r resp) errCode() string {
	e, _ := r.Body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

// do sends a JSON request. token may be "".
func (h *harness) do(method, path, token string, body any, hdr ...string) resp {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{Status: res.StatusCode, Header: res.Header, Raw: raw}
	_ = json.Unmarshal(raw, &out.Body)
	return out
}

func (h *harness) invite() string {
	h.t.Helper()
	code, err := h.app.Auth.CreateAdminInvite(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	return code
}

type user struct {
	ID, Username, Token string
}

// register creates a user with password "password-"+username via a fresh invite.
func (h *harness) register(username string) user {
	h.t.Helper()
	r := h.do("POST", "/v1/auth/register", "", map[string]any{
		"invite": h.invite(), "username": username, "display_name": username,
		"password": "password-" + username, "device_name": "test", "platform": "linux",
	}, "X-Test-Client", username)
	if r.Status != http.StatusCreated {
		h.t.Fatalf("register %s: %d %s", username, r.Status, r.Raw)
	}
	u := r.Body["user"].(map[string]any)
	return user{ID: u["id"].(string), Username: u["username"].(string), Token: r.Body["token"].(string)}
}

func (h *harness) login(username, password string) resp {
	h.t.Helper()
	return h.do("POST", "/v1/auth/login", "", map[string]any{
		"username": username, "password": password, "device_name": "test", "platform": "linux",
	})
}

func productionLimits(d *api.Deps) { d.AuthPerMinute, d.SendPer10s = 0, 0 }
