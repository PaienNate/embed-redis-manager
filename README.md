# embedRedisClient

`embedRedisClient` embeds a Redis UI (based on
[AnotherRedisDesktopManager](https://github.com/qishibo/AnotherRedisDesktopManager))
into an arbitrary Go program. The host injects already-instantiated Redis
clients; the library stores and serves them through an embedded browser UI on
whatever `net/http` mux you already use.

This is useful in environments where Redis is hard to debug directly — no
public address, inconsistent per-account permissions, or no way to reach a UI
host. Your own program exposes the UI at a prefix you choose.

## Library layout

- `redisui/` — the embeddable Go package (`import "embedRedisClient/redisui"`).
- `example/` — a minimal host program that registers the UI and serves it.

## Usage

### 1. Inject a client

The host creates and owns the Redis client (single node, cluster, or ring).
The library only stores and serves it:

```go
import (
    "github.com/redis/go-redis/v9"
    "embedRedisClient/redisui"
)

client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
```

### 2. Register the UI on your mux

`Register` mounts the UI handlers under `cfg.Prefix` (default `/redisui`):

```go
mux := http.NewServeMux()
err := redisui.Register(mux, redisui.Config{
    Prefix: "/redisui",
    Connections: []redisui.Connection{
        {ID: "local", Name: "Local Redis", Client: client, Enabled: true},
    },
})
if err != nil {
    log.Fatal(err)
}
http.ListenAndServe(":8080", mux)
```

`Connection` fields:

- `ID` — unique identifier used in API paths.
- `Name` — display name shown in the UI.
- `Client` — an injected `redis.UniversalClient`.
- `Enabled` — when `false`, the connection is hidden from the UI list and its
  command / cluster / stream endpoints return `404`. Set it to toggle a
  connection on or off without removing it.

### 3. Configure the prefix

`Prefix` may include a leading slash or not (`/redisui` or `redisui`); it
defaults to `/redisui`. The UI is served at that prefix and the API under
`{prefix}/api`. Because the frontend is served from a relative base, it works
under any prefix or reverse-proxy mount. Serve your own routes on the same mux
without conflict.

### 4. Add auth (pluggable middleware)

`Config.Middleware` is a `func(http.Handler) http.Handler`. When non-nil it
wraps **every** route mounted under the prefix — the connection list, generic
command endpoint, cluster nodes, the SockJS stream path, and the embedded
frontend catch-all — so you get uniform auth, logging, CORS, or rate limiting:

```go
cfg := redisui.Config{
    Prefix: "/redisui",
    Connections: []redisui.Connection{{ID: "local", Client: client, Enabled: true}},
    Middleware: func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if r.Header.Get("Authorization") != "Bearer secret" {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
            }
            next.ServeHTTP(w, r)
        })
    },
}
```

A request missing the required header is rejected (`401`) before reaching any
handler. Leave `Middleware` nil to disable wrapping.

## Building the library

The frontend is compiled by Vite and embedded into the Go binary via
`//go:embed` in `redisui/web`, so no external files are needed at runtime.

If you change the frontend, rebuild the dist first, then the Go binary:

```bash
cd redisui/web && npm install && npm run build
cd ../.. && go build ./...
```

## Testing

```bash
go test ./...          # Go tests (uses miniredis, no live Redis needed)
go vet ./redisui
cd redisui/web && npx vitest run   # frontend tests
```

## See also

- [`example/`](example/README.md) — a runnable host program with optional auth.
