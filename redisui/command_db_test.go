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

// The frontend (httpClient.js) encodes every command argument as a typed
// object — {"t":"s","v":...} ({"t":"b",v:<base64>} for binary). All db tests
// use that exact production shape so they cannot pass on a code path the
// frontend never exercises.

// TestCommandDBFieldIsolatesDatabases proves the `db` request field actually
// scopes a command to the requested database: the same key holds different
// values in db0 and db1, and a write in one db never shows up in the other.
func TestCommandDBFieldIsolatesDatabases(t *testing.T) {
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	rec := doCommand(t, mux, "c1", `{"cmd":"set","args":[{"t":"s","v":"k"},{"t":"s","v":"db0"}],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)

	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":[{"t":"s","v":"k"}],"db":1}`)
	assertReply(t, rec, `{"t":"n"}`)

	rec = doCommand(t, mux, "c1", `{"cmd":"set","args":[{"t":"s","v":"k"},{"t":"s","v":"db1"}],"db":1}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)

	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":[{"t":"s","v":"k"}],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"db0"}`)

	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":[{"t":"s","v":"k"}],"db":1}`)
	assertReply(t, rec, `{"t":"s","v":"db1"}`)
}

// TestCommandSelectDoesNotLeakToPool pins the pool to a single connection: if
// a user-issued SELECT (or the db switch) stuck to the pooled connection,
// every later command would be forced through the wrong db. The SELECT — in
// the typed argument shape the frontend sends — must run on the dedicated
// db-pinned client, leaving the injected pool untouched.
func TestCommandSelectDoesNotLeakToPool(t *testing.T) {
	m := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: m.Addr(), PoolSize: 1, MinIdleConns: 1})
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	rec := doCommand(t, mux, "c1", `{"cmd":"select","args":[{"t":"s","v":"1"}],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)

	rec = doCommand(t, mux, "c1", `{"cmd":"set","args":[{"t":"s","v":"k"},{"t":"s","v":"v"}],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)

	// If the SELECT had leaked into the single pooled connection, k would
	// live in db1 and the first GET would find it there.
	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":[{"t":"s","v":"k"}],"db":1}`)
	assertReply(t, rec, `{"t":"n"}`)
	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":[{"t":"s","v":"k"}],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"v"}`)
}

// TestCommandTypedSelectRoutesToTargetDBClient drives the endpoint with the
// exact production payload ({"cmd":"select","args":[{"t":"s","v":"1"}],"db":0})
// and proves the SELECT is routed to the target db's dedicated client: the
// injected pool is never re-targeted, so a later db:0 write lands in db 0 —
// not in the db the SELECT asked for — while db1 writes stay in db1.
func TestCommandTypedSelectRoutesToTargetDBClient(t *testing.T) {
	m := miniredis.RunT(t)
	// A single pooled connection makes the routing deterministic: if SELECT
	// leaked into the injected pool, every later command must reuse it.
	c := redis.NewClient(&redis.Options{Addr: m.Addr(), PoolSize: 1, MinIdleConns: 1})
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	rec := doCommand(t, mux, "c1", `{"cmd":"select","args":[{"t":"s","v":"1"}],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)

	// db0 write must land in db0, not in the db SELECT targeted
	rec = doCommand(t, mux, "c1", `{"cmd":"set","args":[{"t":"s","v":"k"},{"t":"s","v":"v0"}],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)
	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":[{"t":"s","v":"k"}],"db":1}`)
	assertReply(t, rec, `{"t":"n"}`)
	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":[{"t":"s","v":"k"}],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"v0"}`)

	// db1 writes (what the frontend sends after select(1)) land in db1 and
	// do not disturb db0
	rec = doCommand(t, mux, "c1", `{"cmd":"set","args":[{"t":"s","v":"k"},{"t":"s","v":"v1"}],"db":1}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)
	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":[{"t":"s","v":"k"}],"db":1}`)
	assertReply(t, rec, `{"t":"s","v":"v1"}`)
	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":[{"t":"s","v":"k"}],"db":0}`)
	assertReply(t, rec, `{"t":"s","v":"v0"}`)
}

// TestSelectTargetDBShapes covers every argument shape selectTargetDB must
// recognize: the typed objects the frontend sends (string, int, binary) and
// the legacy bare string/integer forms.
func TestSelectTargetDBShapes(t *testing.T) {
	cases := []struct {
		name string
		arg  string
		want int
		ok   bool
	}{
		{"typed string (frontend)", `{"t":"s","v":"1"}`, 1, true},
		{"typed int", `{"t":"i","v":7}`, 7, true},
		{"typed binary (base64 of 3)", `{"t":"b","v":"Mw=="}`, 3, true},
		{"bare string (legacy)", `"2"`, 2, true},
		{"bare int (legacy)", `5`, 5, true},
		{"missing", ``, 0, false},
		{"non-numeric typed", `{"t":"s","v":"abc"}`, 0, false},
		{"unknown typed", `{"t":"x","v":"1"}`, 0, false},
		{"garbage", `{"t":"s"}`, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var args []json.RawMessage
			if tc.arg != "" {
				args = []json.RawMessage{json.RawMessage(tc.arg)}
			}
			got, ok := selectTargetDB(args)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Fatalf("selectTargetDB(%s) = (%d, %v), want (%d, %v)", tc.arg, got, ok, tc.want, tc.ok)
			}
		})
	}
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
					fmt.Sprintf(`{"cmd":"set","args":[{"t":"s","v":%q},{"t":"s","v":%q}],"db":%d}`, key, val, db))
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
			rec := doCommand(t, mux, "c1", fmt.Sprintf(`{"cmd":"get","args":[{"t":"s","v":%q}],"db":%d}`, key, db))
			assertReply(t, rec, val)

			other := 1 - db
			rec = doCommand(t, mux, "c1", fmt.Sprintf(`{"cmd":"get","args":[{"t":"s","v":%q}],"db":%d}`, key, other))
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

	rec := doCommand(t, mux, "c1", `{"cmd":"get","args":[{"t":"s","v":"k"}],"db":3}`)
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
