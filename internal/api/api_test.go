package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"proxier/internal/app"
	"proxier/internal/auth"
	"proxier/internal/config"
	"proxier/internal/store"
)

type env struct {
	t    *testing.T
	srv  *httptest.Server
	app  *app.App
	auth *auth.Service
	c    *http.Client
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Listen: "127.0.0.1:0", TLSMode: config.TLSOff, DataDir: t.TempDir()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := app.New(cfg, st, log, "test")
	ctx, cancel := context.WithCancel(context.Background())
	go a.Run(ctx)
	authSvc := auth.NewService(st)
	h := New(a, authSvc, http.NotFoundHandler(), false)
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close(); a.Proxy.Shutdown(); cancel(); st.Close() })
	jar, _ := cookiejar.New(nil)
	return &env{t: t, srv: srv, app: a, auth: authSvc, c: &http.Client{Jar: jar}}
}

func (e *env) do(method, path string, body any, headers ...string) (*http.Response, map[string]any) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet {
		req.Header.Set("X-Proxier", "1")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i+1] == "" {
			req.Header.Del(headers[i])
		} else {
			req.Header.Set(headers[i], headers[i+1])
		}
	}
	resp, err := e.c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{"_raw": string(raw)}
	}
	return resp, out
}

func (e *env) setup() {
	e.t.Helper()
	tok, _ := e.auth.SetupToken(context.Background())
	resp, body := e.do("POST", "/api/auth/setup", map[string]string{"token": tok, "password": "correct horse battery"})
	if resp.StatusCode != 200 {
		e.t.Fatalf("setup: %d %v", resp.StatusCode, body)
	}
}

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestSetupFlow(t *testing.T) {
	e := newEnv(t)
	_, st := e.do("GET", "/api/auth/status", nil)
	if st["needs_setup"] != true || st["authenticated"] != false {
		t.Fatalf("status %v", st)
	}
	// Wrong token
	tok, _ := e.auth.SetupToken(context.Background())
	if resp, _ := e.do("POST", "/api/auth/setup", map[string]string{"token": "nope", "password": "correct horse battery"}); resp.StatusCode != 401 {
		t.Fatalf("bad token: %d", resp.StatusCode)
	}
	// Weak password
	if resp, _ := e.do("POST", "/api/auth/setup", map[string]string{"token": tok, "password": "short"}); resp.StatusCode != 400 {
		t.Fatalf("weak pw: %d", resp.StatusCode)
	}
	e.setup()
	_, st = e.do("GET", "/api/auth/status", nil)
	if st["needs_setup"] != false || st["authenticated"] != true {
		t.Fatalf("status after setup %v", st)
	}
	// Setup can't run twice.
	if resp, _ := e.do("POST", "/api/auth/setup", map[string]string{"token": tok, "password": "another password!"}); resp.StatusCode == 200 {
		t.Fatal("second setup should fail")
	}
}

func TestAuthRequiredEverywhere(t *testing.T) {
	e := newEnv(t)
	e.setup()
	anon := &http.Client{}
	paths := []string{"/api/rules", "/api/connections", "/api/bans", "/api/events", "/api/presets",
		"/api/settings", "/api/system", "/api/export", "/api/stats/live", "/api/stats/traffic", "/api/ws"}
	for _, p := range paths {
		resp, err := anon.Get(e.srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("%s: got %d, want 401", p, resp.StatusCode)
		}
	}
}

func TestCSRFHeaderRequired(t *testing.T) {
	e := newEnv(t)
	e.setup()
	resp, _ := e.do("POST", "/api/rules", map[string]any{}, "X-Proxier", "")
	if resp.StatusCode != 403 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestLoginThrottle(t *testing.T) {
	e := newEnv(t)
	e.setup()
	e.do("POST", "/api/auth/logout", nil)
	for i := range 5 {
		resp, _ := e.do("POST", "/api/auth/login", map[string]string{"password": "wrong"})
		if resp.StatusCode != 401 {
			t.Fatalf("attempt %d: %d", i, resp.StatusCode)
		}
	}
	resp, _ := e.do("POST", "/api/auth/login", map[string]string{"password": "correct horse battery"})
	if resp.StatusCode != 429 {
		t.Fatalf("expected 429 after 5 failures, got %d", resp.StatusCode)
	}
}

func TestLoginLogout(t *testing.T) {
	e := newEnv(t)
	e.setup()
	e.do("POST", "/api/auth/logout", nil)
	if resp, _ := e.do("GET", "/api/rules", nil); resp.StatusCode != 401 {
		t.Fatal("expected 401 after logout")
	}
	if resp, _ := e.do("POST", "/api/auth/login", map[string]string{"password": "correct horse battery"}); resp.StatusCode != 200 {
		t.Fatal("login failed")
	}
	if resp, _ := e.do("GET", "/api/rules", nil); resp.StatusCode != 200 {
		t.Fatal("expected 200 after login")
	}
}

func TestRuleCRUDAndConflicts(t *testing.T) {
	e := newEnv(t)
	e.setup()
	port := freePort(t)
	rule := map[string]any{"name": "ts", "enabled": true, "protocol": "udp", "listen_ip": "127.0.0.1",
		"listen_port_start": port, "target_host": "127.0.0.1", "target_port_start": 9987}

	// Validation errors come back per field.
	resp, body := e.do("POST", "/api/rules", map[string]any{"name": "", "protocol": "tcp", "listen_port_start": 0, "target_host": ""})
	if resp.StatusCode != 400 || body["fields"] == nil {
		t.Fatalf("validation: %d %v", resp.StatusCode, body)
	}

	resp, body = e.do("POST", "/api/rules", rule)
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := int(body["id"].(float64))
	if body["status"].(map[string]any)["state"] != "running" {
		t.Fatalf("status %v", body["status"])
	}

	// Overlap with the existing rule.
	dup := map[string]any{}
	for k, v := range rule {
		dup[k] = v
	}
	dup["name"] = "dup"
	resp, body = e.do("POST", "/api/rules/check", dup)
	if resp.StatusCode != 400 || !strings.Contains(body["fields"].(map[string]any)["listen_port_start"].(string), "overlaps") {
		t.Fatalf("overlap: %d %v", resp.StatusCode, body)
	}
	// TCP on the same port is fine.
	dup["protocol"] = "tcp"
	if resp, body = e.do("POST", "/api/rules/check", dup); resp.StatusCode != 200 {
		t.Fatalf("tcp same port should be OK: %v", body)
	}

	// Port occupied by another process.
	blocker, _ := net.Listen("tcp", "127.0.0.1:0")
	defer blocker.Close()
	dup["listen_port_start"] = blocker.Addr().(*net.TCPAddr).Port
	resp, body = e.do("POST", "/api/rules/check", dup)
	if resp.StatusCode != 400 || !strings.Contains(body["fields"].(map[string]any)["listen_port_start"].(string), "in use") {
		t.Fatalf("os conflict: %d %v", resp.StatusCode, body)
	}

	// Update, disable, enable, delete.
	rule["name"] = "renamed"
	path := "/api/rules/" + strconv.Itoa(id)
	if resp, body = e.do("PUT", path, rule); resp.StatusCode != 200 || body["name"] != "renamed" {
		t.Fatalf("update: %d %v", resp.StatusCode, body)
	}
	if resp, body = e.do("POST", path+"/disable", nil); body["status"].(map[string]any)["state"] != "disabled" {
		t.Fatalf("disable: %v", body)
	}
	if resp, body = e.do("POST", path+"/enable", nil); body["status"].(map[string]any)["state"] != "running" {
		t.Fatalf("enable: %v", body)
	}
	if resp, _ = e.do("DELETE", path, nil); resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp, _ = e.do("GET", path, nil); resp.StatusCode != 404 {
		t.Fatalf("get deleted: %d", resp.StatusCode)
	}
}

func TestBansAndPresets(t *testing.T) {
	e := newEnv(t)
	e.setup()
	if resp, body := e.do("POST", "/api/bans", map[string]any{"cidr": "0.0.0.0/0"}); resp.StatusCode != 400 {
		t.Fatalf("ban everything should fail: %v", body)
	}
	resp, body := e.do("POST", "/api/bans", map[string]any{"cidr": "203.0.113.7", "reason": "spam", "expires_in_s": 3600})
	if resp.StatusCode != 201 || body["cidr"] != "203.0.113.7" {
		t.Fatalf("ban: %d %v", resp.StatusCode, body)
	}

	// Apply a preset with port overrides so the test doesn't need real TS3 ports.
	p1, p2 := freePort(t), freePort(t)
	req := map[string]any{"target_host": "ts.example.ru", "listen_ip": "127.0.0.1", "items": []map[string]any{
		{"index": 0, "selected": true, "listen_port_start": p1},
		{"index": 1, "selected": true, "listen_port_start": p2, "target_port_start": 30033},
	}}
	resp, body = e.do("POST", "/api/presets/builtin:ts3/apply", req)
	if resp.StatusCode != 201 {
		t.Fatalf("apply preset: %d %v", resp.StatusCode, body)
	}
	var rules []map[string]any
	r2, _ := e.c.Get(e.srv.URL + "/api/rules")
	json.NewDecoder(r2.Body).Decode(&rules)
	r2.Body.Close()
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}

	// Applying again conflicts and creates nothing.
	if resp, _ = e.do("POST", "/api/presets/builtin:ts3/apply", req); resp.StatusCode != 400 {
		t.Fatalf("second apply should conflict: %d", resp.StatusCode)
	}

	// Save as custom preset, then export/import round trip.
	ids := []int64{int64(rules[0]["id"].(float64)), int64(rules[1]["id"].(float64))}
	if resp, body = e.do("POST", "/api/presets", map[string]any{"name": "My TS", "rule_ids": ids}); resp.StatusCode != 201 {
		t.Fatalf("custom preset: %d %v", resp.StatusCode, body)
	}
	exp, err := e.c.Get(e.srv.URL + "/api/export")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	json.NewDecoder(exp.Body).Decode(&doc)
	exp.Body.Close()
	resp, body = e.do("POST", "/api/import?mode=replace", doc)
	if resp.StatusCode != 200 || body["rules"].(float64) != 2 || body["presets"].(float64) != 1 || body["bans"].(float64) != 1 {
		t.Fatalf("import: %d %v", resp.StatusCode, body)
	}
	if body["rules_disabled"].(float64) != 0 {
		t.Fatalf("replace import should re-bind cleanly: %v", body)
	}
}

func TestWebSocketStream(t *testing.T) {
	e := newEnv(t)
	e.setup()
	u, _ := http.NewRequest("GET", e.srv.URL, nil)
	cookies := e.c.Jar.Cookies(u.URL)
	hdr := http.Header{}
	for _, c := range cookies {
		hdr.Add("Cookie", c.Name+"="+c.Value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/api/ws"

	// Cross-origin is rejected.
	bad := hdr.Clone()
	bad.Set("Origin", "https://evil.example")
	if c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: bad}); err == nil {
		c.CloseNow()
		t.Fatal("cross-origin websocket accepted")
	}

	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	seen := map[string]bool{}
	for !(seen["hello"] && seen["stats"]) {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (seen %v)", err, seen)
		}
		var m struct{ Type string }
		json.Unmarshal(b, &m)
		seen[m.Type] = true
	}
}
