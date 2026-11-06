package redisui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

// maxCommandBodyBytes caps the request body accepted by the generic command
// endpoint (spec §14: size-limit protection). Larger bodies are rejected
// with 413 instead of being buffered unboundedly.
const maxCommandBodyBytes = 32 << 20 // 32 MiB

// commandArg is a single typed command argument. T is "s" for a plain string
// (V is the raw value) or "b" for a binary-safe argument (V is base64). The
// latter lets the frontend round-trip arbitrary non-UTF-8 bytes without being
// mangled by JSON, because redis keys/values are byte sequences.
type commandArg struct {
	T string `json:"t"`
	V string `json:"v"`
}

// commandRequest is the POST body accepted by the generic command endpoint.
// Args are typed command arguments (see commandArg); Binary selects
// base64-encoded bulk string replies (bulk strings and array elements); DB is
// the logical db the frontend currently has selected (see handleCommand).
type commandRequest struct {
	Cmd    string            `json:"cmd"`
	Args   []json.RawMessage `json:"args"`
	Binary bool              `json:"binary"`
	DB     int               `json:"db"`
}

// typedReply is the tagged JSON envelope for a single Redis reply value:
//
//	{t:"s",v:...} string | {t:"i",v:...} int | {t:"a",v:[...]} array
//	{t:"n"} nil | {t:"b",v:"<base64>"} binary bulk string
//
// Replies are mapped by their Go type; a status reply is exposed by go-redis
// as a plain string, so it is emitted as {t:"s"}.
type typedReply struct {
	t string
	v interface{}
}

func (r typedReply) MarshalJSON() ([]byte, error) {
	if r.t == "n" {
		return []byte(`{"t":"n"}`), nil
	}
	return json.Marshal(struct {
		T string      `json:"t"`
		V interface{} `json:"v"`
	}{T: r.t, V: r.v})
}

// commandResponse is the unified response body: error is null on success and
// reply is null when the command failed.
type commandResponse struct {
	Error *string     `json:"error"`
	Reply *typedReply `json:"reply"`
}

// handleCommand executes an arbitrary Redis command on the connection
// identified by the {connID} path segment and serializes its reply with a
// type tag. It returns 404 if the connection does not exist or is disabled,
// 413 when the request body exceeds maxCommandBodyBytes.
func (s *server) handleCommand(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.conns[r.PathValue("connID")]
	if !ok || !conn.Enabled {
		http.Error(w, "redisui: connection not found", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCommandBodyBytes)
	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "redisui: request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "redisui: invalid request body", http.StatusBadRequest)
		return
	}

	args := make([]interface{}, 0, len(req.Args)+1)
	args = append(args, req.Cmd)
	for _, raw := range req.Args {
		arg, err := decodeCommandArg(raw)
		if err != nil {
			http.Error(w, "redisui: invalid command argument: "+err.Error(), http.StatusBadRequest)
			return
		}
		args = append(args, arg)
	}

	// Resolve the executor: normally the injected client, but a dedicated
	// db-pinned client when the frontend asks for a non-zero db (or issues
	// SELECT itself). See commandExecutor for the invariants.
	run, err := s.commandExecutor(conn, req)
	if err != nil {
		// Reported as a command-level error (200 + {error}), not an HTTP
		// error: the frontend treats non-2xx responses as a broken
		// connection and would tear it down.
		msg := err.Error()
		writeCommandResponse(w, &msg, nil)
		return
	}

	cmd := run(r.Context(), args...)
	if err := cmd.Err(); err != nil {
		if err == redis.Nil {
			// A nil reply (e.g. GET of a missing key) is not an error.
			writeCommandResponse(w, nil, &typedReply{t: "n"})
			return
		}
		msg := err.Error()
		writeCommandResponse(w, &msg, nil)
		return
	}

	reply := serializeReply(cmd.Val(), req.Binary)
	writeCommandResponse(w, nil, &reply)
}

// commandRunner executes one command and returns its reply.
type commandRunner func(ctx context.Context, args ...interface{}) *redis.Cmd

// commandExecutor decides which client a command request runs on.
//
//   - Plain command with db == 0: the injected client as-is. db 0 means "the
//     db the host configured the client with" (0 in the common case), so the
//     host's own pool, hooks, and failover handling all apply unchanged.
//   - Non-zero db: a dedicated client (see dbClientFor) whose pool is pinned
//     to that db when it dials. Routing through a separate client keeps the
//     injected client's pool free of SELECT session state — a pooled SELECT
//     would silently retarget every later command reusing that connection —
//     and gives each db its own consistent pool.
//   - A user-issued SELECT is routed to the target db's dedicated client too
//     (its conns already open on that db), so even a raw CLI SELECT cannot
//     pollute the injected pool. Cluster servers reject SELECT themselves,
//     which the frontend relies on to snap the db selector back to 0.
//   - Cluster clients have a single db (0) and no SELECT support, so a
//     non-zero db is ignored there (it still addresses the only db).
//   - Any other client type cannot scope a db to one connection; refuse the
//     request instead of running it against the wrong db.
func (s *server) commandExecutor(conn Connection, req commandRequest) (commandRunner, error) {
	// cluster: single db; SELECT is rejected by the server itself.
	if _, isCluster := conn.Client.(*redis.ClusterClient); isCluster {
		return conn.Client.Do, nil
	}

	// db the request effectively addresses: an explicit SELECT overrides the
	// body's db field with its target.
	desiredDB := req.DB
	selectTarget, hasSelectTarget := selectTargetDB(req.Args)
	// A negative db is invalid: don't pin (and cache) a client to it. Leave the
	// SELECT to the server, which rejects it, so no useless db client is kept.
	if hasSelectTarget && selectTarget < 0 {
		hasSelectTarget = false
	}
	if strings.EqualFold(req.Cmd, "SELECT") && hasSelectTarget {
		desiredDB = selectTarget
	}

	pc, isPlain := conn.Client.(*redis.Client)
	if !isPlain {
		if desiredDB > 0 {
			return nil, fmt.Errorf("db %d requested but this client type cannot scope a db to one connection", desiredDB)
		}
		return conn.Client.Do, nil
	}

	if strings.EqualFold(req.Cmd, "SELECT") && hasSelectTarget {
		return s.dbClientFor(conn, pc, desiredDB).Do, nil
	}
	if req.DB > 0 {
		return s.dbClientFor(conn, pc, req.DB).Do, nil
	}
	return conn.Client.Do, nil
}

// selectTargetDB extracts the target db of a SELECT command from its
// arguments (e.g. SELECT 3). The frontend always encodes arguments as typed
// objects ({"t":"s","v":"1"}; binary args arrive base64-encoded), while older
// callers sent bare JSON strings/integers — both shapes are recognized. It
// reports false when the argument is missing or not a number, leaving the
// server to reject the malformed command.
func selectTargetDB(args []json.RawMessage) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}

	// Typed argument in the shape the frontend actually sends.
	var typed struct {
		T string          `json:"t"`
		V json.RawMessage `json:"v"`
	}
	if err := json.Unmarshal(args[0], &typed); err == nil && typed.T != "" {
		switch typed.T {
		case "s", "b":
			var s string
			if err := json.Unmarshal(typed.V, &s); err != nil {
				return 0, false
			}
			if typed.T == "b" {
				raw, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return 0, false
				}
				s = string(raw)
			}
			db, err := strconv.Atoi(s)
			return db, err == nil
		case "i":
			var n int
			if err := json.Unmarshal(typed.V, &n); err != nil {
				return 0, false
			}
			return n, true
		default:
			return 0, false
		}
	}

	// Legacy bare shapes: JSON string ("1") or integer (1).
	var s string
	if err := json.Unmarshal(args[0], &s); err == nil {
		db, err := strconv.Atoi(s)
		return db, err == nil
	}
	var n int
	if err := json.Unmarshal(args[0], &n); err == nil {
		return n, true
	}
	return 0, false
}

// dbClientFor returns a cached client whose connection pool is pinned to the
// given db. It is built from a copy of the injected client's options with
// DB overridden (redis.NewClient clones what it is given, so the injected
// client's live options are never mutated) — the pool is fully independent:
// no shared options, no shared session state, no data race with the injected
// client's own connection setup. Host-registered hooks (AddHook) do not carry
// over to these clients; db 0 keeps using the injected client, so hooks apply
// to the default path. The clients live as long as the server (connections
// are host-owned and never unregistered), which matches the process lifetime
// of the embedded UI.
func (s *server) dbClientFor(conn Connection, pc *redis.Client, db int) *redis.Client {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	if s.dbClients == nil {
		s.dbClients = map[string]map[int]*redis.Client{}
	}
	byDB := s.dbClients[conn.ID]
	if byDB == nil {
		byDB = map[int]*redis.Client{}
		s.dbClients[conn.ID] = byDB
	}
	if c := byDB[db]; c != nil {
		return c
	}
	opts := *pc.Options() // shallow copy; NewClient deep-clones it
	opts.DB = db
	// The shallow copy still points at the host's *live* MaintNotificationsConfig;
	// NewClient's clone() dereferences that pointer, racing with the host's
	// initConn writing Mode under its own optLock (go-redis v9.23 gives a
	// derived Conn its own lock over the shared Options). Drop the pointer —
	// Options.init() re-applies the defaults to the fresh copy (nil-safe).
	opts.MaintNotificationsConfig = nil
	c := redis.NewClient(&opts)
	byDB[db] = c
	return c
}

// decodeCommandArg turns one raw JSON argument into a Go value safe to pass to
// go-redis. It accepts a typed object ({t:"b"|"s", v}) and, for backward
// compatibility with earlier clients, a bare JSON string (treated as "s").
func decodeCommandArg(raw json.RawMessage) (interface{}, error) {
	// legacy: a plain JSON string argument
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}

	var a commandArg
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("argument must be a string or {t,v} object")
	}
	switch a.T {
	case "b":
		b, err := base64.StdEncoding.DecodeString(a.V)
		if err != nil {
			return nil, fmt.Errorf("bad base64 in binary arg: %v", err)
		}
		return b, nil
	case "s", "":
		return a.V, nil
	default:
		return nil, fmt.Errorf("unknown argument type %q", a.T)
	}
}

// serializeReply maps a Redis reply value to its tagged JSON representation.
// When binary is true, bulk strings and array elements are base64-encoded as
// {t:"b"}; otherwise strings are emitted as {t:"s"}.
func serializeReply(v interface{}, binary bool) typedReply {
	switch val := v.(type) {
	case nil:
		return typedReply{t: "n"}
	case string:
		if binary {
			return typedReply{t: "b", v: base64.StdEncoding.EncodeToString([]byte(val))}
		}
		return typedReply{t: "s", v: val}
	case []byte:
		return typedReply{t: "b", v: base64.StdEncoding.EncodeToString(val)}
	case int64:
		return typedReply{t: "i", v: val}
	case int:
		return typedReply{t: "i", v: val}
	case []interface{}:
		items := make([]typedReply, 0, len(val))
		for _, e := range val {
			items = append(items, serializeReply(e, binary))
		}
		return typedReply{t: "a", v: items}
	default:
		// Unreachable for the standard reply types; keep a safe fallback so a
		// value is never silently dropped (e.g. RESP3 floats/nullable types).
		return typedReply{t: "s", v: fmt.Sprint(v)}
	}
}

func writeCommandResponse(w http.ResponseWriter, errMsg *string, reply *typedReply) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(commandResponse{Error: errMsg, Reply: reply})
}
