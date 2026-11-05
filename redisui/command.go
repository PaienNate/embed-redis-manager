package redisui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/redis/go-redis/v9"
)

// commandRequest is the POST body accepted by the generic command endpoint.
// Args are string command arguments; Binary selects base64-encoded bulk
// string replies (bulk strings and array elements).
type commandRequest struct {
	Cmd    string   `json:"cmd"`
	Args   []string `json:"args"`
	Binary bool     `json:"binary"`
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
// type tag. It returns 404 if the connection does not exist or is disabled.
func (s *server) handleCommand(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.conns[r.PathValue("connID")]
	if !ok || !conn.Enabled {
		http.Error(w, "redisui: connection not found", http.StatusNotFound)
		return
	}

	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "redisui: invalid request body", http.StatusBadRequest)
		return
	}

	args := make([]interface{}, 0, len(req.Args)+1)
	args = append(args, req.Cmd)
	for _, a := range req.Args {
		args = append(args, a)
	}

	cmd := conn.Client.Do(r.Context(), args...)
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
