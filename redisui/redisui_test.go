package redisui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestRegisterNilMuxErrors(t *testing.T) {
	if err := Register(nil, Config{}); err == nil {
		t.Fatal("Register(nil, ...) expected error, got nil")
	}
}
