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
