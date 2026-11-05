package redisui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func miniredisClient(t *testing.T) redis.UniversalClient {
	t.Helper()
	m := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: m.Addr()})
}

func TestRegisterAddsRoutesToMux(t *testing.T) {
	mux := http.NewServeMux()
	cfg := Config{Prefix: "/r", Connections: []Connection{
		{ID: "c1", Name: "主缓存", Client: miniredisClient(t), Enabled: true},
	}}
	if err := Register(mux, cfg); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/r/api/connections", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /r/api/connections status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestConnectionsEndpointJSONShape(t *testing.T) {
	mux := http.NewServeMux()
	cfg := Config{Prefix: "/r", Connections: []Connection{
		{ID: "c1", Name: "主缓存", Client: miniredisClient(t), Enabled: true},
		{ID: "c2", Name: "禁用连接", Client: miniredisClient(t), Enabled: false},
	}}
	if err := Register(mux, cfg); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/r/api/connections", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (disabled connection excluded); body=%s", len(got), rec.Body.String())
	}
	if got[0].ID != "c1" || got[0].Name != "主缓存" || !got[0].Enabled {
		t.Fatalf("unexpected connection: %+v", got[0])
	}
}

func TestRegisterDefaultPrefix(t *testing.T) {
	mux := http.NewServeMux()
	cfg := Config{Connections: []Connection{
		{ID: "c1", Name: "主缓存", Client: miniredisClient(t), Enabled: true},
	}}
	if err := Register(mux, cfg); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/redisui/api/connections", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRegisterMiddlewareWraps(t *testing.T) {
	mux := http.NewServeMux()
	called := false
	cfg := Config{
		Prefix: "/r",
		Connections: []Connection{
			{ID: "c1", Name: "主缓存", Client: miniredisClient(t), Enabled: true},
		},
		Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				next.ServeHTTP(w, r)
			})
		},
	}
	if err := Register(mux, cfg); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/r/api/connections", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !called {
		t.Fatal("middleware was not called")
	}
}

// TestMiddlewareWrapsAllRoutes verifies that Config.Middleware is applied to
// every route mounted under the prefix: connections, generic command, cluster
// nodes, the SockJS stream path, and the embedded frontend catch-all. Without
// the required header each of them must be rejected by the middleware.
func TestMiddlewareWrapsAllRoutes(t *testing.T) {
	mux := http.NewServeMux()
	invoked := map[string]bool{}
	cfg := Config{
		Prefix: "/r",
		Connections: []Connection{
			{ID: "c1", Name: "主缓存", Client: miniredisClient(t), Enabled: true},
		},
		Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				invoked[r.URL.Path] = true
				if r.Header.Get("X-Auth") != "secret" {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
	}
	if err := Register(mux, cfg); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	routes := []struct {
		name, method, path string
	}{
		{"connections", http.MethodGet, "/r/api/connections"},
		{"command", http.MethodPost, "/r/api/redis/c1/command"},
		{"clusterNodes", http.MethodGet, "/r/api/redis/c1/cluster/nodes"},
		{"stream", http.MethodGet, "/r/api/redis/c1/stream/"},
		{"frontend", http.MethodGet, "/r/"},
	}

	// Without the required header every prefix route must be rejected (401).
	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s status = %d, want %d; body=%s",
					tc.method, tc.path, rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}

	// With the header the middleware must pass through to the real handler.
	req := httptest.NewRequest(http.MethodGet, "/r/api/connections", nil)
	req.Header.Set("X-Auth", "secret")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /r/api/connections with header status = %d, want 200; body=%s",
			rec.Code, rec.Body.String())
	}

	// The middleware must have executed for every route under the prefix.
	for _, tc := range routes {
		if !invoked[tc.path] {
			t.Errorf("middleware was not invoked for %s %s", tc.method, tc.path)
		}
	}
}

// TestEnabledToggleShowsOrHidesConnection verifies that a connection with
// Enabled=false is hidden from the connection list and that its command,
// cluster, and stream endpoints all return 404 (rather than being served).
func TestEnabledToggleShowsOrHidesConnection(t *testing.T) {
	mux := http.NewServeMux()
	cfg := Config{Prefix: "/r", Connections: []Connection{
		{ID: "on", Name: "启用", Client: miniredisClient(t), Enabled: true},
		{ID: "off", Name: "禁用", Client: miniredisClient(t), Enabled: false},
	}}
	if err := Register(mux, cfg); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	// List: only the enabled connection appears.
	req := httptest.NewRequest(http.MethodGet, "/r/api/connections", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if len(got) != 1 || got[0].ID != "on" {
		t.Fatalf("connections list = %+v, want only enabled id 'on'", got)
	}

	// Command on a disabled connection must 404.
	cmdReq := httptest.NewRequest(http.MethodPost, "/r/api/redis/off/command",
		strings.NewReader(`{"cmd":"PING"}`))
	cmdRec := httptest.NewRecorder()
	mux.ServeHTTP(cmdRec, cmdReq)
	if cmdRec.Code != http.StatusNotFound {
		t.Fatalf("command on disabled conn status = %d, want 404; body=%s",
			cmdRec.Code, cmdRec.Body.String())
	}

	// Cluster nodes on a disabled connection must 404.
	clReq := httptest.NewRequest(http.MethodGet, "/r/api/redis/off/cluster/nodes", nil)
	clRec := httptest.NewRecorder()
	mux.ServeHTTP(clRec, clReq)
	if clRec.Code != http.StatusNotFound {
		t.Fatalf("cluster on disabled conn status = %d, want 404; body=%s",
			clRec.Code, clRec.Body.String())
	}

	// Stream on a disabled connection must 404.
	stReq := httptest.NewRequest(http.MethodGet, "/r/api/redis/off/stream/", nil)
	stRec := httptest.NewRecorder()
	mux.ServeHTTP(stRec, stReq)
	if stRec.Code != http.StatusNotFound {
		t.Fatalf("stream on disabled conn status = %d, want 404; body=%s",
			stRec.Code, stRec.Body.String())
	}
}

func TestRegisterNilMuxErrors(t *testing.T) {
	if err := Register(nil, Config{}); err == nil {
		t.Fatal("Register(nil, ...) expected error, got nil")
	}
}

func TestRegisterRejectsRootPrefix(t *testing.T) {
	// A prefix resolving to "/" would mount the frontend catch-all on the mux
	// root and swallow every host route; Register must refuse it outright.
	for _, prefix := range []string{"/", "///", " / "} {
		mux := http.NewServeMux()
		err := Register(mux, Config{Prefix: prefix, Connections: []Connection{
			{ID: "c1", Name: "c1", Client: miniredisClient(t), Enabled: true},
		}})
		if err == nil {
			t.Fatalf("Register(Prefix=%q): want error, got nil", prefix)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/connections", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("Register(Prefix=%q) mounted routes on the mux root: status = %d, want 404", prefix, rec.Code)
		}
	}
}

func TestRegisterRejectsDuplicateConnectionID(t *testing.T) {
	mux := http.NewServeMux()
	err := Register(mux, Config{Prefix: "/r", Connections: []Connection{
		{ID: "dup", Name: "first", Client: miniredisClient(t), Enabled: true},
		{ID: "dup", Name: "second", Client: miniredisClient(t), Enabled: true},
	}})
	if err == nil {
		t.Fatal("Register with duplicate connection IDs: want error, got nil")
	}
	// nothing may be mounted when registration fails
	req := httptest.NewRequest(http.MethodGet, "/r/api/connections", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("failed Register still mounted routes: status = %d", rec.Code)
	}
}
