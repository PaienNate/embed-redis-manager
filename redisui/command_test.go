package redisui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func commandServer(t *testing.T, conns ...Connection) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	cfg := Config{Prefix: "/r", Connections: conns}
	if err := Register(mux, cfg); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	return mux
}

func doCommand(t *testing.T, mux *http.ServeMux, connID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/r/api/redis/"+connID+"/command", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// assertReply asserts a 200 response, a null error field, and that the
// tagged reply JSON deep-equals wantJSON (which need not be compact).
func assertReply(t *testing.T, rec *httptest.ResponseRecorder, wantJSON string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Error *string         `json:"error"`
		Reply json.RawMessage `json:"reply"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}
	if got.Error != nil {
		t.Fatalf("expected error null, got %q; body=%s", *got.Error, rec.Body.String())
	}
	var gotReply, wantReply interface{}
	if err := json.Unmarshal(got.Reply, &gotReply); err != nil {
		t.Fatalf("unmarshal reply: %v; raw=%s", err, got.Reply)
	}
	if err := json.Unmarshal([]byte(wantJSON), &wantReply); err != nil {
		t.Fatalf("unmarshal wantJSON: %v", err)
	}
	if !reflect.DeepEqual(gotReply, wantReply) {
		t.Fatalf("reply = %#v, want %#v", gotReply, wantReply)
	}
}

func TestCommandExecutesViaUniversalClient(t *testing.T) {
	c := miniredisClient(t)
	if err := c.Set(context.Background(), "k", "v", 0).Err(); err != nil {
		t.Fatalf("seed set: %v", err)
	}
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})
	rec := doCommand(t, mux, "c1", `{"cmd":"get","args":["k"],"binary":false}`)
	assertReply(t, rec, `{"t":"s","v":"v"}`)
}

func TestCommandIntReplyPreserved(t *testing.T) {
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})
	rec := doCommand(t, mux, "c1", `{"cmd":"incr","args":["n"]}`)
	assertReply(t, rec, `{"t":"i","v":1}`)
}

func TestCommandNilReplyPreserved(t *testing.T) {
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})
	rec := doCommand(t, mux, "c1", `{"cmd":"get","args":["missing"]}`)
	assertReply(t, rec, `{"t":"n"}`)
}

func TestCommandArrayReplyPreserved(t *testing.T) {
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})
	doCommand(t, mux, "c1", `{"cmd":"lpush","args":["l","a","b"]}`)
	rec := doCommand(t, mux, "c1", `{"cmd":"lrange","args":["l","0","-1"]}`)
	assertReply(t, rec, `{"t":"a","v":[{"t":"s","v":"b"},{"t":"s","v":"a"}]}`)
}

func TestCommandBinaryReplyBase64(t *testing.T) {
	c := miniredisClient(t)
	val := "\x00\x01"
	if err := c.Set(context.Background(), "bin", val, 0).Err(); err != nil {
		t.Fatalf("seed bin: %v", err)
	}
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})
	rec := doCommand(t, mux, "c1", `{"cmd":"get","args":["bin"],"binary":true}`)
	want := base64.StdEncoding.EncodeToString([]byte(val))
	assertReply(t, rec, `{"t":"b","v":"`+want+`"}`)
}

func TestCommandBinaryArgRoundTrip(t *testing.T) {
	// A value containing non-UTF-8 / NUL bytes must survive a write through a
	// typed binary arg ({t:"b"}) and read back byte-for-byte via binary=true.
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	data := []byte{0x00, 0x01, 0xfe, 0xff, 'a', 0x7f}
	encoded := base64.StdEncoding.EncodeToString(data)
	rec := doCommand(t, mux, "c1",
		`{"cmd":"set","args":[{"t":"s","v":"bin"},{"t":"b","v":"`+encoded+`"}]}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)

	rec = doCommand(t, mux, "c1", `{"cmd":"get","args":["bin"],"binary":true}`)
	assertReply(t, rec, `{"t":"b","v":"`+encoded+`"}`)
}

func TestCommandMixedStringAndBinaryArgs(t *testing.T) {
	// Both plain-string args (legacy) and typed args are accepted in one call.
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	rec := doCommand(t, mux, "c1", `{"cmd":"hset","args":[{"t":"s","v":"h"},{"t":"b","v":"`+
		base64.StdEncoding.EncodeToString([]byte("f"))+`"},{"t":"s","v":"v"}]}`)
	assertReply(t, rec, `{"t":"i","v":1}`)

	rec = doCommand(t, mux, "c1", `{"cmd":"hget","args":["h","f"],"binary":true}`)
	assertReply(t, rec, `{"t":"b","v":"`+base64.StdEncoding.EncodeToString([]byte("v"))+`"}`)
}

func TestCommandStatusReplyMappedAsString(t *testing.T) {
	// SET returns a status reply ("OK"); go-redis exposes it as a plain Go
	// string, indistinguishable from a bulk string, so by Go-type mapping it
	// is emitted as {t:"s"} (the "status" tag is not reachable via Do).
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})
	rec := doCommand(t, mux, "c1", `{"cmd":"set","args":["k","v"]}`)
	assertReply(t, rec, `{"t":"s","v":"OK"}`)
}

func TestCommandErrorReturnsErrorMessage(t *testing.T) {
	c := miniredisClient(t)
	if err := c.Set(context.Background(), "k", "v", 0).Err(); err != nil {
		t.Fatalf("seed set: %v", err)
	}
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})
	rec := doCommand(t, mux, "c1", `{"cmd":"lpush","args":["k","x"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Error *string         `json:"error"`
		Reply json.RawMessage `json:"reply"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if got.Error == nil || *got.Error == "" {
		t.Fatalf("expected a redis error message, got %#v", got.Error)
	}
	if string(got.Reply) != "null" {
		t.Fatalf("reply should be null on error, got %s", got.Reply)
	}
}

func TestCommandBadConnIDReturns404(t *testing.T) {
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: miniredisClient(t), Enabled: true})
	rec := doCommand(t, mux, "nope", `{"cmd":"get","args":["k"]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCommandDisabledConnReturns404(t *testing.T) {
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: miniredisClient(t), Enabled: false})
	rec := doCommand(t, mux, "c1", `{"cmd":"get","args":["k"]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// parseScanReply decodes a SCAN command reply as {cursor, keys}. A SCAN reply
// is `{t:"a", v:[<cursor>, <keys>]}`, the cursor being a tagged string and the
// keys a tagged array of tagged strings.
func parseScanReply(t *testing.T, rec *httptest.ResponseRecorder) (cursor string, keys []string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Error *string         `json:"error"`
		Reply json.RawMessage `json:"reply"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if env.Error != nil {
		t.Fatalf("expected error null, got %q", *env.Error)
	}
	var arr struct {
		T string            `json:"t"`
		V []json.RawMessage `json:"v"`
	}
	if err := json.Unmarshal(env.Reply, &arr); err != nil {
		t.Fatalf("unmarshal reply: %v; raw=%s", err, env.Reply)
	}
	if arr.T != "a" || len(arr.V) != 2 {
		t.Fatalf("unexpected scan reply shape: %s", env.Reply)
	}
	var cur struct {
		T string `json:"t"`
		V string `json:"v"`
	}
	if err := json.Unmarshal(arr.V[0], &cur); err != nil {
		t.Fatalf("unmarshal cursor: %v; raw=%s", err, arr.V[0])
	}
	cursor = cur.V

	var karr struct {
		T string `json:"t"`
		V []struct {
			T string `json:"t"`
			V string `json:"v"`
		} `json:"v"`
	}
	if err := json.Unmarshal(arr.V[1], &karr); err != nil {
		t.Fatalf("unmarshal keys: %v; raw=%s", err, arr.V[1])
	}
	for _, k := range karr.V {
		keys = append(keys, k.V)
	}
	return cursor, keys
}

func TestCommandScanMatchAndCount(t *testing.T) {
	// Verify SCAN through the generic endpoint returns only keys matching
	// `MATCH` (multiple matches) and that the reply cursor is a string.
	// NOTE: miniredis returns the whole matching set in a single page (cursor
	// "0"), so genuine multi-page cursor iteration cannot be exercised here;
	// the frontend HttpRedisClient scan loop (paging) is covered by
	// web/test/httpClient.spec.js.
	c := miniredisClient(t)
	for _, k := range []string{"user:1", "user:2", "order:9", "user:3"} {
		if err := c.Set(context.Background(), k, "v", 0).Err(); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})
	rec := doCommand(t, mux, "c1", `{"cmd":"scan","args":["0","MATCH","user:*","COUNT","100"]}`)
	cursor, keys := parseScanReply(t, rec)
	if cursor != "0" {
		t.Fatalf("cursor = %q, want \"0\"", cursor)
	}
	want := map[string]bool{"user:1": true, "user:2": true, "user:3": true}
	if len(keys) != len(want) {
		t.Fatalf("matched %v, want %d keys", keys, len(want))
	}
	for _, k := range keys {
		if !want[k] {
			t.Fatalf("unexpected matched key %q in %v", k, keys)
		}
	}
}

func TestCommandScanNoMatch(t *testing.T) {
	c := miniredisClient(t)
	if err := c.Set(context.Background(), "a", "v", 0).Err(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})
	rec := doCommand(t, mux, "c1", `{"cmd":"scan","args":["0","MATCH","missing:*","COUNT","10"]}`)
	cursor, keys := parseScanReply(t, rec)
	if cursor != "0" {
		t.Fatalf("cursor = %q, want \"0\"", cursor)
	}
	if len(keys) != 0 {
		t.Fatalf("keys = %v, want none", keys)
	}
}

func TestCommandPing(t *testing.T) {
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})
	rec := doCommand(t, mux, "c1", `{"cmd":"ping"}`)
	assertReply(t, rec, `{"t":"s","v":"PONG"}`)
}

// TestCommandStreamCommands exercises the stream command family (XADD / XLEN /
// XREVRANGE) through the generic endpoint, which is what KeyContentStream.vue
// relies on via HttpRedisClient.xadd/xlen/xrevrangeBuffer. miniredis supports
// streams, so this is a real round-trip through the command proxy.
func TestCommandStreamCommands(t *testing.T) {
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	// XADD returns the generated entry id as a bulk string.
	rec := doCommand(t, mux, "c1", `{"cmd":"xadd","args":["s","*","f","v"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("xadd status = %d; body=%s", rec.Code, rec.Body.String())
	}

	rec = doCommand(t, mux, "c1", `{"cmd":"xadd","args":["s","*","f","v2"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("xadd2 status = %d; body=%s", rec.Code, rec.Body.String())
	}

	// XLEN returns an integer.
	rec = doCommand(t, mux, "c1", `{"cmd":"xlen","args":["s"]}`)
	assertReply(t, rec, `{"t":"i","v":2}`)

	// XREVRANGE returns [ [id, [field, value, ...]], ... ]; in binary mode
	// each field/value is a base64 bulk string.
	rec = doCommand(t, mux, "c1",
		`{"cmd":"xrevrange","args":["s","+","-","COUNT","10"],"binary":true}`)
	var env struct {
		Error *string         `json:"error"`
		Reply json.RawMessage `json:"reply"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if env.Error != nil {
		t.Fatalf("expected null error, got %q", *env.Error)
	}
	arr := decodeArray(t, env.Reply)
	if len(arr) != 2 {
		t.Fatalf("xrevrange returned %d entries, want 2", len(arr))
	}
	// Each entry is [id, [field,value,...]]; assert the first entry shape.
	entry := decodeArray(t, arr[0])
	if len(entry) != 2 {
		t.Fatalf("entry len = %d, want 2", len(entry))
	}
	if tag, _ := decodeTag(t, entry[0]); tag != "b" {
		t.Fatalf("entry id tag = %q, want b", tag)
	}
	flat := decodeArray(t, entry[1])
	if len(flat) != 2 {
		t.Fatalf("stream flat fields len = %d, want 2", len(flat))
	}
	if got := decodeBin(t, flat[0]); got != "f" {
		t.Fatalf("first field = %q, want f", got)
	}
}

func TestCommandDBSize(t *testing.T) {
	c := miniredisClient(t)
	for _, k := range []string{"a", "b", "c"} {
		if err := c.Set(context.Background(), k, "v", 0).Err(); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})
	rec := doCommand(t, mux, "c1", `{"cmd":"dbsize"}`)
	assertReply(t, rec, `{"t":"i","v":3}`)
}
