// Command example shows how to embed the Redis UI into an arbitrary Go
// program: the host builds one (or more) redis clients, registers a redisui
// server under a prefix, and serves it on its own net/http mux.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/redis/go-redis/v9"

	"embedRedisClient/redisui"
	"embedRedisClient/redisui/goredisv9"
)

func main() {
	// The host owns the client and its lifecycle; the library only stores and
	// serves it. Point this at your Redis (or use a *redis.ClusterClient /
	// *redis.Ring) and wrap it with goredisv9.New (or goredisv8.New for a v8
	// client). Set a running Redis at localhost:6379 to see it work.
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password
		DB:       0,
	})

	mux := http.NewServeMux()
	err := redisui.Register(mux, redisui.Config{
		Prefix:  "/redisui", // defaults to "/redisui" when empty
		Version: "example-1.0.0",
		Connections: []redisui.Connection{
			{ID: "local", Name: "Local Redis", Client: goredisv9.New(client), Enabled: true},
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
