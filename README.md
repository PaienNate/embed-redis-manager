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
    Version: "myapp-1.2.3", // shown in the UI settings panel
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

- `ID` — unique identifier used in API paths. Must be unique per `Register`
  call; a duplicate makes `Register` return an error.
- `Name` — display name shown in the UI.
- `Client` — an injected `redis.UniversalClient`.
- `Enabled` — when `false`, the connection is hidden from the UI list and its
  command / cluster / stream endpoints return `404`. Set it to toggle a
  connection on or off without removing it.

`Version` (optional) is displayed in the UI settings panel. When empty it falls
back to the library's `redisui.Version`; set it to surface the host
application's version instead.

### 3. Configure the prefix

`Prefix` may include a leading slash or not (`/redisui` or `redisui`); it
defaults to `/redisui`. A trailing slash is normalized away, but a prefix that
resolves to the mux root (`"/"`) is rejected with an error from `Register` —
the UI's SPA catch-all would otherwise swallow every host route. The UI is
served at that prefix and the API under `{prefix}/api`. Because the frontend
is served from a relative base, it works under any prefix or reverse-proxy
mount. Serve your own routes on the same mux without conflict.

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
            // The UI is loaded by navigating a browser to {prefix}/, and a
            // browser navigation cannot send an Authorization header. Check a
            // cookie/session here (or terminate auth at your gateway). A
            // Bearer-header check only works if the UI is driven by an API
            // client, not by opening the page in a browser.
            if c, err := r.Cookie("session"); err != nil || !validSession(c.Value) {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
            }
            next.ServeHTTP(w, r)
        })
    },
}
```

(`validSession` is your own session check.) A request without a valid session is
rejected (`401`) before reaching any handler. Leave `Middleware` nil to disable
wrapping.

### 5. DB selection (multiple databases)

The UI's db selector is implemented by attaching the selected db to **every**
command request (`{"cmd":..., "db":N}`); the backend resolves each request to
the right connection:

- `db == 0` (the default) executes on the injected client exactly as-is — the
  db the host configured it with (0 in the common case) — so host-registered
  hooks and failover handling apply unchanged.
- `db > 0` executes on a dedicated `redis.NewClient` built from a **copy** of
  the injected `(*redis.Client).Options()` with `DB` overridden. Each db gets
  its own connection pool that issues `SELECT` when it dials, so no `SELECT`
  ever sticks to a pooled connection of the injected client and concurrent
  requests can never land in the wrong db. These clients are created lazily
  and cached for the lifetime of the server.
- A raw `SELECT` typed in the CLI is routed to the target db's dedicated
  client too, so it cannot pollute the injected pool. Cluster servers reject
  `SELECT` themselves; the UI shows the error and snaps back to db 0.
- Cluster connections ignore the `db` field — a cluster has exactly one db
  (0), so any db value still addresses it.
- Other client types (e.g. ring) cannot scope a db to one connection; a
  `db > 0` request is refused with a command-level error instead of silently
  running against the wrong db.

Caveats: the per-db clients do not carry host-registered `AddHook` hooks (db 0
does), and a per-db client derived from a sentinel-backed client pins the
address it saw at first use. Hosts that prefer no db switching at all can
register one `Connection` per database instead, each with its own client
configured for that db; the UI then only operates db 0 per connection.

## Building the library

The frontend is compiled by Vite and embedded into the Go binary via
`//go:embed` in `redisui/web`, so no external files are needed at runtime.

If you change the frontend, rebuild the dist first, then the Go binary:

```bash
cd redisui/web && npm install && npm run build
cd ../.. && go build ./...
```

### Browser support (including IE11)

`npm run build` emits **two** bundles via `@vitejs/plugin-legacy`:

- a modern ES module bundle (`assets/index-*.js`)
- an IE11-targeted legacy bundle (`assets/index-legacy-*.js` + `assets/polyfills-legacy-*.js`) loaded through `<script nomodule>`

`whatwg-fetch` (fetch), `url-polyfill` (URL, used by SockJS) and a tiny `crypto`
shim are included for IE11. The editor is CodeMirror 5 (IE11-compatible) rather
than Monaco, which also keeps the bundle much smaller.

> Note: browsers are detected by the `nomodule` mechanism, so IE11 loads the
> legacy bundle automatically; no configuration is needed by the host.

## Testing

```bash
go test ./...          # Go tests (uses miniredis, no live Redis needed)
go vet ./redisui
cd redisui/web && npx vitest run   # frontend tests
```

## See also

- [`example/`](example/README.md) — a runnable host program with optional auth.
