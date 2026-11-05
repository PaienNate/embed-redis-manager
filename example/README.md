# Example host program

`main.go` is a minimal host program that embeds the Redis UI into its own
`net/http` mux.

## Run

The example expects a Redis server at `localhost:6379`. Start one, then:

```bash
go run ./example
```

Open <http://localhost:8080/redisui/>. Requests to `/` are redirected to the
UI's prefix `/redisui/`.

## Optional auth

By default the example registers no middleware so you can browse immediately.
To demonstrate the pluggable auth middleware, set a token:

```bash
REQUIRE_AUTH=secret go run ./example
```

Now every route under the prefix (connection list, generic command, cluster
nodes, the SockJS stream, and the embedded frontend) requires an
`Authorization: Bearer secret` header and returns `401 Unauthorized` without
it. This shows the `Config.Middleware` hook wrapping all prefix routes.
