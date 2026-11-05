package redisui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestCommandDBFieldIsolatesDatabases proves the `db` request field actually
// scopes a command to the requested database: the same key holds different
// values in db0 and db1, and a write in one db never shows up in the other.
func TestCommandDBFieldIsolatesDatabases(t *testing.T) {
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	rec := doCommand(t, mux, "c1", `{"cmd":"set","args":["k","db0"],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)

	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":["k"],"db":1}`)
	assertReply(t, rec, `{"t":"n"}`)

	rec = doCommand(t, mux, "c1", `{"cmd":"set","args":["k","db1"],"db":1}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)

	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":["k"],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"db0"}`)

	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":["k"],"db":1}`)
	assertReply(t, rec, `{"t":"s","v":"db1"}`)
}

// TestCommandSelectDoesNotLeakToPool pins the pool to a single connection: if
// a user-issued SELECT (or the db switch) stuck to the pooled connection,
// every later command would be forced through the wrong db. The SELECT must
// run on the dedicated db-pinned client, leaving the injected pool untouched.
func TestCommandSelectDoesNotLeakToPool(t *testing.T) {
	m := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: m.Addr(), PoolSize: 1, MinIdleConns: 1})
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	rec := doCommand(t, mux, "c1", `{"cmd":"select","args":["1"]}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)

	rec = doCommand(t, mux, "c1", `{"cmd":"set","args":["k","v"],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)

	// If the SELECT had leaked into the single pooled connection, k would
	// live in db1 and the first GET would find it there.
	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":["k"],"db":1}`)
	assertReply(t, rec, `{"t":"n"}`)
	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":["k"],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"v"}`)
}

// TestCommandDBConcurrentIsolation hammers the endpoint from two goroutines
// pinned to different dbs and verifies afterwards that no key crossed over.
func TestCommandDBConcurrentIsolation(t *testing.T) {
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	const perDB = 20
	var wg sync.WaitGroup
	for db := 0; db < 2; db++ {
		db := db
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perDB; i++ {
				key := fmt.Sprintf("k:%d:%d", db, i)
				val := fmt.Sprintf("v:%d:%d", db, i)
				rec := doCommand(t, mux, "c1",
					fmt.Sprintf(`{"cmd":"set","args":[%q,%q],"db":%d}`, key, val, db))
				if rec.Code != http.StatusOK {
					t.Errorf("db %d SET %s: status = %d, body=%s", db, key, rec.Code, rec.Body.String())
					return
				}
			}
		}()
	}
	wg.Wait()

	for db := 0; db < 2; db++ {
		for i := 0; i < perDB; i++ {
			key := fmt.Sprintf("k:%d:%d", db, i)
			val := fmt.Sprintf(`{"t":"s","v":"v:%d:%d"}`, db, i)
			rec := doCommand(t, mux, "c1", fmt.Sprintf(`{"cmd":"get","args":[%q],"db":%d}`, key, db))
			assertReply(t, rec, val)

			other := 1 - db
			rec = doCommand(t, mux, "c1", fmt.Sprintf(`{"cmd":"get","args":[%q],"db":%d}`, key, other))
			assertReply(t, rec, `{"t":"n"}`)
		}
	}
}

// universalWrapper hides the concrete client behind a UniversalClient that
// has no EphemeralConn, standing in for exotic client types (ring, custom
// wrappers) that cannot scope a db to one connection.
type universalWrapper struct {
	redis.UniversalClient
}

func TestCommandDBUnsupportedClientTypeRejected(t *testing.T) {
	mux := commandServer(t, Connection{
		ID: "c1", Name: "c1", Client: universalWrapper{miniredisClient(t)}, Enabled: true,
	})

	rec := doCommand(t, mux, "c1", `{"cmd":"get","args":["k"],"db":3}`)
	// 200 with a command-level error envelope: the frontend must not treat
	// this as a broken connection (non-2xx would tear it down).
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Error *string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if env.Error == nil || !strings.Contains(*env.Error, "cannot scope a db") {
		t.Fatalf("expected a db-scoping error, got %#v", env.Error)
	}
}
