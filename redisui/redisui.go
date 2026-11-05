// Package redisui embeds a Redis UI (based on AnotherRedisDesktopManager)
// into an arbitrary Go program. The host injects already-instantiated Redis
// clients; this package only stores and serves them.
package redisui

import (
	"encoding/json"
	"errors"
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
	conns map[string]Connection // indexed by Connection.ID
	order []string              // connection IDs in config order
}

// Register mounts the Redis UI handlers under cfg.Prefix on mux. Prefix
// defaults to "/redisui". If cfg.Middleware is non-nil it wraps every handler.
// This task only mounts the placeholder GET {prefix}/api/connections endpoint.
func Register(mux *http.ServeMux, cfg Config) error {
	if mux == nil {
		return errors.New("redisui: Register: nil mux")
	}

	prefix := normalizePrefix(cfg.Prefix)
	s := &server{conns: map[string]Connection{}}
	for _, c := range cfg.Connections {
		s.conns[c.ID] = c
		s.order = append(s.order, c.ID)
	}

	var handler http.Handler = http.HandlerFunc(s.handleConnections)
	if cfg.Middleware != nil {
		handler = cfg.Middleware(handler)
	}
	mux.Handle("GET "+prefix+"/api/connections", handler)
	return nil
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
