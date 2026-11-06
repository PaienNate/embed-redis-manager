// Command example shows how to embed the Redis UI into an arbitrary Go
// program: the host builds one (or more) redis clients, registers a redisui
// server under a prefix, and serves it on its own net/http mux.
package main

import (
	"log"
	"net/http"
	"os"

	v8 "github.com/go-redis/redis/v8"
	"github.com/redis/go-redis/v9"

	"embedRedisClient/redisui"
	"embedRedisClient/redisui/goredisv8"
	"embedRedisClient/redisui/goredisv9"
)

func main() {
	// The host owns the clients and their lifecycle; the library only stores and
	// serves them. This example registers the same Redis twice — once through
	// the go-redis v9 adapter and once through v8 — so you can compare both.
	// Point these at your Redis (or use *redis.ClusterClient / *redis.Ring).
	v9Client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	v8Client := v8.NewClient(&v8.Options{Addr: "localhost:6379"})

	mux := http.NewServeMux()
	err := redisui.Register(mux, redisui.Config{
		Prefix:  "/redisui", // defaults to "/redisui" when empty
		Version: "example-1.0.0",
		Connections: []redisui.Connection{
			{ID: "v9", Name: "Redis v9", Client: goredisv9.New(v9Client), Enabled: true},
			{ID: "v8", Name: "Redis v8", Client: goredisv8.New(v8Client), Enabled: true},
		},
		// Optional: plug in auth/logging/CORS. When set, the middleware wraps
		// every route under the prefix — connections, generic command, cluster
		// nodes, the SockJS stream path, and the embedded frontend.
		Middleware: buildMiddleware(),
	})
	if err != nil {
		log.Fatalf("register redisui: %v", err)
	}

	// Serve your own app routes alongside the embedded UI on the same mux.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redisui/", http.StatusFound)
	})

	log.Println("Redis UI available at http://localhost:8080/redisui/")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// buildMiddleware returns the middleware to wrap the UI routes with. When
// REQUIRE_AUTH is set it protects every route with a bearer-token check
// (demonstrating how to add auth); otherwise it is nil so you can browse the
// UI immediately.
func buildMiddleware() func(http.Handler) http.Handler {
	token := os.Getenv("REQUIRE_AUTH")
	if token == "" {
		return nil
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
