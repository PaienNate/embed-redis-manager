package redisui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// findAsset walks the embedded dist and returns the first file with the given
// extension relative to the dist root, e.g. "assets/index-B-swAWm9.js".
func findAsset(t *testing.T, ext string) string {
	t.Helper()
	var found string
	err := fs.WalkDir(distFS, "web/dist", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ext) {
			found = strings.TrimPrefix(path, "web/dist/")
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded dist: %v", err)
	}
	if found == "" {
		t.Fatalf("no %s asset found in embedded dist", ext)
	}
	return found
}

func TestEmbeddedDistServesIndex(t *testing.T) {
	mux := http.NewServeMux()
	cfg := Config{Prefix: "/r", Connections: []Connection{
		{ID: "c1", Name: "主缓存", Client: miniredisClient(t), Enabled: true},
	}}
	if err := Register(mux, cfg); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/r/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /r/ status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `<div id="app">`) {
		t.Fatalf("index body missing <div id=\"app\">; body=%s", rec.Body.String())
	}
}

func TestEmbeddedDistInjectsAPIPrefix(t *testing.T) {
	mux := http.NewServeMux()
	cfg := Config{Prefix: "/redisui", Connections: []Connection{
		{ID: "c1", Name: "主缓存", Client: miniredisClient(t), Enabled: true},
	}}
	if err := Register(mux, cfg); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/redisui/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `window.__REDISUI__`) {
		t.Fatalf("body missing window.__REDISUI__; body=%s", body)
	}
	if !strings.Contains(body, `apiBase:"/redisui/api"`) {
		t.Fatalf("apiBase not injected correctly; body=%s", body)
	}
}

func TestStaticAssetServed(t *testing.T) {
	mux := http.NewServeMux()
	cfg := Config{Prefix: "/r", Connections: []Connection{
		{ID: "c1", Name: "主缓存", Client: miniredisClient(t), Enabled: true},
	}}
	if err := Register(mux, cfg); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	for _, tc := range []struct {
		ext      string
		ctypeSub string
	}{
		{ext: ".js", ctypeSub: "javascript"},
		{ext: ".css", ctypeSub: "css"},
	} {
		asset := findAsset(t, tc.ext)
		req := httptest.NewRequest(http.MethodGet, "/r/"+asset, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /%s status = %d, want %d", asset, rec.Code, http.StatusOK)
		}
		ctype := rec.Header().Get("Content-Type")
		if !strings.Contains(ctype, tc.ctypeSub) {
			t.Fatalf("GET /%s Content-Type = %q, want containing %q", asset, ctype, tc.ctypeSub)
		}
	}
}

func TestSPAHistoryRouteFallsBackToIndex(t *testing.T) {
	mux := http.NewServeMux()
	cfg := Config{Prefix: "/r", Connections: []Connection{
		{ID: "c1", Name: "主缓存", Client: miniredisClient(t), Enabled: true},
	}}
	if err := Register(mux, cfg); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	// A non-asset path (a client-side history route) must serve index.html.
	req := httptest.NewRequest(http.MethodGet, "/r/connections", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /r/connections status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `<div id="app">`) ||
		!strings.Contains(rec.Body.String(), `window.__REDISUI__`) {
		t.Fatalf("SPA fallback did not return index.html; body=%s", rec.Body.String())
	}
}
