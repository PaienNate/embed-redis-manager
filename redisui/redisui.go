// Package redisui embeds a Redis UI (based on AnotherRedisDesktopManager)
// into an arbitrary Go program. The host injects already-instantiated Redis
// clients; this package only stores and serves them.
package redisui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Connection describes a single Redis connection exposed through the UI.
// Client is injected by the host and is not created here.
type Connection struct {
	ID      string
	Name    string
	Client  Client
	Enabled bool
}

// Config configures the embedded Redis UI.
// Version is the default Redis UI version reported to the frontend when
// Config.Version is empty. Hosts embedding the UI can override it per Register
// call to surface their own product version in the settings panel.
const Version = "0.1.0"

// Config configures a registered Redis UI instance.
type Config struct {
	Prefix      string
	Connections []Connection
	Middleware  func(http.Handler) http.Handler
	// Version is shown in the UI settings panel. When empty, Version is used.
	Version string
}

// server holds the internal state for a registered UI instance.
type server struct {
	conns    map[string]Connection // indexed by Connection.ID
	order    []string              // connection IDs in config order
	prefix   string                // normalized mount prefix
	version  string                // version reported to the UI settings panel
	frontend fs.FS                 // embedded dist, rooted at web/dist
	middle   func(http.Handler) http.Handler
	stream   map[string]http.Handler // connID -> per-connection SockJS handler

	// dbClients caches the db-pinned clients behind dbClientFor:
	// connID -> db -> dedicated client. Guarded by dbMu.
	dbMu      sync.Mutex
	dbClients map[string]map[int]Client
}

// Register mounts the Redis UI handlers under cfg.Prefix on mux. Prefix
// defaults to "/redisui". It returns an error for an invalid prefix (one
// resolving to the mux root) or for duplicate Connection IDs, in which case
// nothing is mounted. If cfg.Middleware is non-nil it wraps every handler.
func Register(mux *http.ServeMux, cfg Config) error {
	if mux == nil {
		return errors.New("redisui: Register: nil mux")
	}

	prefix, err := normalizePrefix(cfg.Prefix)
	if err != nil {
		return err
	}
	frontend, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		return fmt.Errorf("redisui: embed frontend sub: %w", err)
	}
	version := cfg.Version
	if version == "" {
		version = Version
	}
	s := &server{
		conns:    map[string]Connection{},
		prefix:   prefix,
		version:  version,
		frontend: frontend,
		middle:   cfg.Middleware,
		stream:   map[string]http.Handler{},
	}
	for _, c := range cfg.Connections {
		// A duplicate ID would silently overwrite the earlier connection in
		// s.conns while appearing twice in the UI list; reject it instead.
		if _, dup := s.conns[c.ID]; dup {
			return fmt.Errorf("redisui: Register: duplicate connection ID %q", c.ID)
		}
		s.conns[c.ID] = c
		s.order = append(s.order, c.ID)
	}

	// Every route mounted under the prefix is passed through wrap() so
	// Config.Middleware (auth, logging, CORS, ...) applies uniformly. Any new
	// prefix route added later must also be wrapped via s.wrap.
	mux.Handle("GET "+prefix+"/api/connections", s.wrap(http.HandlerFunc(s.handleConnections)))
	mux.Handle("POST "+prefix+"/api/redis/{connID}/command", s.wrap(http.HandlerFunc(s.handleCommand)))
	mux.Handle("GET "+prefix+"/api/redis/{connID}/cluster/nodes", s.wrap(http.HandlerFunc(s.handleClusterNodes)))
	mux.Handle("GET "+prefix+"/api/redis/{connID}/options", s.wrap(http.HandlerFunc(s.handleConnectionOptions)))

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
	// No method restriction: SockJS serves its transports over a mix of GET
	// (websocket, eventsource, info, iframe) and POST (xhr, xhr_send,
	// xhr_streaming), so the path must be routed for every method; the sockjs
	// handler itself dispatches by method.
	mux.Handle(prefix+"/api/redis/{connID}/stream/", s.wrap(http.HandlerFunc(s.handleStream)))

	// Serve the embedded frontend under the prefix. This catch-all pattern is
	// less specific than the API routes above, so API requests are unaffected.
	mux.Handle(prefix+"/", s.wrap(http.HandlerFunc(s.serveFrontend)))
	return nil
}

// wrap applies Config.Middleware (if any) to h. Every route mounted by
// Register goes through wrap so middleware coverage is uniform across the API
// and the embedded frontend.
func (s *server) wrap(h http.Handler) http.Handler {
	if s.middle != nil {
		return s.middle(h)
	}
	return h
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
	script := `<script>window.__REDISUI__={apiBase:"` + s.prefix + `/api",version:` + strconv.Quote(s.version) + `}</script>`
	html := strings.Replace(string(data), "</head>", script+"</head>", 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

// normalizePrefix trims and normalizes cfg.Prefix into the mount prefix. An
// empty prefix keeps the documented "/redisui" default. A prefix that resolves
// to the mux root ("/", "///", ...) is rejected: the frontend catch-all
// mounted there would swallow every host route on the mux.
func normalizePrefix(prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "/redisui", nil
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return "", errors.New(`redisui: Register: prefix "/" would mount the UI at the mux root`)
	}
	return prefix, nil
}

type connectionDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Host/Port are derived from the injected client's Options().Addr and are
	// shown in the connection tooltip. Empty when the address can't be
	// determined (the frontend then omits them).
	Host string `json:"host,omitempty"`
	Port string `json:"port,omitempty"`
}

func (s *server) handleConnections(w http.ResponseWriter, _ *http.Request) {
	out := make([]connectionDTO, 0, len(s.conns))
	for _, id := range s.order {
		c := s.conns[id]
		if !c.Enabled {
			continue
		}
		host, port := c.Client.HostPort()
		out = append(out, connectionDTO{ID: c.ID, Name: c.Name, Enabled: true, Host: host, Port: port})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// handleConnectionOptions serves GET {prefix}/api/redis/{connID}/options. It
// describes the injected client's configuration (address, db, pool, timeouts,
// TLS, ...) for the UI's connection-options card. It never exposes secrets:
// passwords are reduced to a yes/no flag and TLS certificate material is not
// serialized (only booleans/summary values).
func (s *server) handleConnectionOptions(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.conns[r.PathValue("connID")]
	if !ok || !conn.Enabled {
		http.Error(w, "redisui: connection not found", http.StatusNotFound)
		return
	}
	if conn.Client == nil {
		http.Error(w, "redisui: connection has no client", http.StatusInternalServerError)
		return
	}

	opts := []Option{{Key: "Kind", Value: kindLabel(conn.Client.Kind())}}
	if p, ok := conn.Client.(OptionsProvider); ok {
		opts = append(opts, p.Options()...)
	}
	// Custom Client implementations may not implement OptionsProvider; still
	// show a best-effort address derived from HostPort().
	if !hasOption(opts, "Address") && !hasOption(opts, "Addresses") {
		if host, port := conn.Client.HostPort(); host != "" {
			opts = append(opts, Option{Key: "Address", Value: net.JoinHostPort(host, port)})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(opts)
}

func kindLabel(k Kind) string {
	switch k {
	case KindSingle:
		return "Single"
	case KindCluster:
		return "Cluster"
	case KindRing:
		return "Ring"
	default:
		return "Other"
	}
}

func hasOption(opts []Option, key string) bool {
	for _, o := range opts {
		if o.Key == key {
			return true
		}
	}
	return false
}
