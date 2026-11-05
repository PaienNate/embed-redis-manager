// Package redisui embeds a Redis UI (based on AnotherRedisDesktopManager)
// into an arbitrary Go program. The host injects already-instantiated Redis
// clients; this package only stores and serves them.
package redisui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Connection describes a single Redis connection exposed through the UI.
// Client is injected by the host and is not created here.
type Connection struct {
	ID      string
	Name    string
	Client  redis.UniversalClient
	Enabled bool
}

// Config configures the embedded Redis UI.
type Config struct {
	Prefix      string
	Connections []Connection
	Middleware  func(http.Handler) http.Handler
}

// server holds the internal state for a registered UI instance.
type server struct {
	conns    map[string]Connection // indexed by Connection.ID
	order    []string              // connection IDs in config order
	prefix   string                // normalized mount prefix
	frontend fs.FS                 // embedded dist, rooted at web/dist
	middle   func(http.Handler) http.Handler
	stream   map[string]http.Handler // connID -> per-connection SockJS handler
}

// Register mounts the Redis UI handlers under cfg.Prefix on mux. Prefix
// defaults to "/redisui". If cfg.Middleware is non-nil it wraps every handler.
// This task only mounts the placeholder GET {prefix}/api/connections endpoint.
func Register(mux *http.ServeMux, cfg Config) error {
	if mux == nil {
		return errors.New("redisui: Register: nil mux")
	}

	prefix := normalizePrefix(cfg.Prefix)
	frontend, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		return fmt.Errorf("redisui: embed frontend sub: %w", err)
	}
	s := &server{
		conns:    map[string]Connection{},
		prefix:   prefix,
		frontend: frontend,
		middle:   cfg.Middleware,
		stream:   map[string]http.Handler{},
	}
	for _, c := range cfg.Connections {
		s.conns[c.ID] = c
		s.order = append(s.order, c.ID)
	}

	var handler http.Handler = http.HandlerFunc(s.handleConnections)
	if cfg.Middleware != nil {
		handler = cfg.Middleware(handler)
	}
	mux.Handle("GET "+prefix+"/api/connections", handler)

	var cmdHandler http.Handler = http.HandlerFunc(s.handleCommand)
	if cfg.Middleware != nil {
		cmdHandler = cfg.Middleware(cmdHandler)
	}
	mux.Handle("POST "+prefix+"/api/redis/{connID}/command", cmdHandler)

	var nodesHandler http.Handler = http.HandlerFunc(s.handleClusterNodes)
	if cfg.Middleware != nil {
		nodesHandler = cfg.Middleware(nodesHandler)
	}
	mux.Handle("GET "+prefix+"/api/redis/{connID}/cluster/nodes", nodesHandler)

	// Build one SockJS handler per enabled connection. A single generic
	// dispatcher below validates the connID and routes to the right handler
	// (404 for disabled/unknown connections), so those requests do not fall
	// through to the SPA catch-all.
	for _, c := range s.conns {
		if !c.Enabled {
			continue
		}
		s.registerStream(c)
	}
	var streamHandler http.Handler = http.HandlerFunc(s.handleStream)
	if cfg.Middleware != nil {
		streamHandler = cfg.Middleware(streamHandler)
	}
	// No method restriction: SockJS serves its transports over a mix of GET
	// (websocket, eventsource, info, iframe) and POST (xhr, xhr_send,
	// xhr_streaming), so the path must be routed for every method; the sockjs
	// handler itself dispatches by method.
	mux.Handle(prefix+"/api/redis/{connID}/stream/", streamHandler)

	// Serve the embedded frontend under the prefix. This catch-all pattern is
	// less specific than the API routes above, so API requests are unaffected.
	var frontendHandler http.Handler = http.HandlerFunc(s.serveFrontend)
	if cfg.Middleware != nil {
		frontendHandler = cfg.Middleware(frontendHandler)
	}
	mux.Handle(prefix+"/", frontendHandler)
	return nil
}

// serveFrontend serves the embedded static frontend under the configured
// prefix. Real static files are served as-is; any other path (an SPA history
// route) falls back to index.html.
func (s *server) serveFrontend(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, s.prefix)
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		name = "index.html"
	}

	if _, err := fs.Stat(s.frontend, name); err != nil {
		// Not an existing file: treat as an SPA history route.
		name = "index.html"
	}

	if name == "index.html" {
		s.serveIndexHTML(w)
		return
	}
	http.ServeFileFS(w, r, s.frontend, name)
}

// serveIndexHTML serves the embedded index.html with a script that exposes the
// API base URL to the frontend (read by task 5). The script is injected on
// response so the source index.html in web/dist is left untouched.
func (s *server) serveIndexHTML(w http.ResponseWriter) {
	data, err := fs.ReadFile(s.frontend, "index.html")
	if err != nil {
		http.Error(w, "redisui: frontend index not found", http.StatusInternalServerError)
		return
	}
	script := `<script>window.__REDISUI__={apiBase:"` + s.prefix + `/api"}</script>`
	html := strings.Replace(string(data), "</head>", script+"</head>", 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func normalizePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "/redisui"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimRight(prefix, "/")
}

type connectionDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func (s *server) handleConnections(w http.ResponseWriter, _ *http.Request) {
	out := make([]connectionDTO, 0, len(s.conns))
	for _, id := range s.order {
		c := s.conns[id]
		if !c.Enabled {
			continue
		}
		out = append(out, connectionDTO{ID: c.ID, Name: c.Name, Enabled: true})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
