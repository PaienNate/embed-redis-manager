package redisui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// postCommand posts a command to the generic command endpoint of a live
// httptest.Server and returns the decoded {error, reply} envelope. A non-200
// response is reported as a test failure.
func postCommand(t *testing.T, baseURL, connID, body string) json.RawMessage {
	t.Helper()
	resp, err := http.Post(baseURL+"/r/api/redis/"+connID+"/command",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST command: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read command body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST command status = %d, want 200; body=%s", resp.StatusCode, raw)
	}
	var env struct {
		Error *string         `json:"error"`
		Reply json.RawMessage `json:"reply"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal command response %q: %v", raw, err)
	}
	if env.Error != nil {
		t.Fatalf("unexpected command error %q; body=%s", *env.Error, raw)
	}
	return env.Reply
}

// assertReplyJSON confirms a command reply deep-equals the tagged wantJSON.
func assertReplyJSON(t *testing.T, reply json.RawMessage, wantJSON string) {
	t.Helper()
	var gotReply, wantReply interface{}
	if err := json.Unmarshal(reply, &gotReply); err != nil {
		t.Fatalf("unmarshal reply: %v; raw=%s", err, reply)
	}
	if err := json.Unmarshal([]byte(wantJSON), &wantReply); err != nil {
		t.Fatalf("unmarshal wantJSON: %v", err)
	}
	if !reflect.DeepEqual(gotReply, wantReply) {
		t.Fatalf("reply = %#v, want %#v", gotReply, wantReply)
	}
}

// parseScanReplyRaw decodes a SCAN command reply from a raw {t:"a", v:[...]}
// envelope and returns the cursor string and the list of key strings.
func parseScanReplyRaw(t *testing.T, reply json.RawMessage) (cursor string, keys []string) {
	t.Helper()
	var arr struct {
		T string            `json:"t"`
		V []json.RawMessage `json:"v"`
	}
	if err := json.Unmarshal(reply, &arr); err != nil {
		t.Fatalf("unmarshal scan reply: %v; raw=%s", err, reply)
	}
	if arr.T != "a" || len(arr.V) != 2 {
		t.Fatalf("unexpected scan reply shape: %s", reply)
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

// TestFullEmbeddedFlow is the end-to-end integration test. It spins up a real
// httptest.Server with a miniredis connection registered through
// Config/Register, then drives the full chain over real HTTP:
//
//	Register -> static frontend -> connection list -> generic command
//	-> SET/GET -> SCAN -> DBSIZE -> nil GET.
func TestFullEmbeddedFlow(t *testing.T) {
	c := miniredisClient(t)
	connID := "e2e"

	mux := http.NewServeMux()
	cfg := Config{
		Prefix: "/r",
		Connections: []Connection{
			{ID: connID, Name: "端到端", Client: c, Enabled: true},
		},
	}
	if err := Register(mux, cfg); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("static frontend served", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/r/")
		if err != nil {
			t.Fatalf("GET /r/: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /r/ status = %d, want 200", resp.StatusCode)
		}
		if !strings.Contains(string(body), `<div id="app">`) {
			t.Fatalf("index body missing <div id=\"app\">; body=%s", body)
		}
	})

	t.Run("connection list contains connection", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/r/api/connections")
		if err != nil {
			t.Fatalf("GET /r/api/connections: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /r/api/connections status = %d, want 200; body=%s", resp.StatusCode, body)
		}
		var list []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			t.Fatalf("unmarshal connections: %v; body=%s", err, body)
		}
		if len(list) != 1 || list[0].ID != connID || list[0].Name != "端到端" || !list[0].Enabled {
			t.Fatalf("connection list = %+v, want single enabled conn %q", list, connID)
		}
	})

	t.Run("SET then GET round trip", func(t *testing.T) {
		reply := postCommand(t, srv.URL, connID, `{"cmd":"set","args":["greeting","hello"]}`)
		assertReplyJSON(t, reply, `{"t":"s","v":"OK"}`)

		reply = postCommand(t, srv.URL, connID, `{"cmd":"get","args":["greeting"]}`)
		assertReplyJSON(t, reply, `{"t":"s","v":"hello"}`)
	})

	t.Run("SCAN lists keys and DBSIZE is correct", func(t *testing.T) {
		// Self-contained: seed every key this subtest asserts on, so it passes
		// when run alone (e.g. -run TestFullEmbeddedFlow/SCAN).
		for _, k := range []string{"user:1", "user:2", "order:9", "greeting"} {
			if err := c.Set(context.Background(), k, "v", 0).Err(); err != nil {
				t.Fatalf("seed %s: %v", k, err)
			}
		}

		reply := postCommand(t, srv.URL, connID,
			`{"cmd":"scan","args":["0","MATCH","*","COUNT","100"]}`)
		cursor, keys := parseScanReplyRaw(t, reply)
		if cursor != "0" {
			t.Fatalf("scan cursor = %q, want \"0\"", cursor)
		}
		want := map[string]bool{"user:1": true, "user:2": true, "order:9": true, "greeting": true}
		if len(keys) != len(want) {
			t.Fatalf("scan returned %v, want %d keys", keys, len(want))
		}
		for _, k := range keys {
			if !want[k] {
				t.Fatalf("unexpected key %q in %v", k, keys)
			}
		}

		reply = postCommand(t, srv.URL, connID, `{"cmd":"dbsize"}`)
		assertReplyJSON(t, reply, `{"t":"i","v":4}`)
	})

	t.Run("GET missing key returns nil", func(t *testing.T) {
		reply := postCommand(t, srv.URL, connID, `{"cmd":"get","args":["absent"]}`)
		assertReplyJSON(t, reply, `{"t":"n"}`)
	})
}

// TestDisabledConnectionReturns404 verifies that an Enabled:false connection is
// hidden from the list and that its command / cluster / stream endpoints return
// 404 (rather than falling through to the SPA catch-all).
func TestDisabledConnectionReturns404(t *testing.T) {
	c := miniredisClient(t)
	mux := http.NewServeMux()
	if err := Register(mux, Config{Prefix: "/r", Connections: []Connection{
		{ID: "off", Name: "disabled", Client: c, Enabled: false},
	}}); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/r/api/connections")
	if err != nil {
		t.Fatalf("GET connections: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var list []map[string]interface{}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("unmarshal connections: %v; body=%s", err, body)
	}
	if len(list) != 0 {
		t.Fatalf("disabled connection leaked into list: %s", body)
	}

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"command", http.MethodPost, "/r/api/redis/off/command", `{"cmd":"ping"}`},
		{"cluster nodes", http.MethodGet, "/r/api/redis/off/cluster/nodes", ""},
		{"options", http.MethodGet, "/r/api/redis/off/options", ""},
		{"stream", http.MethodGet, "/r/api/redis/off/stream/", ""},
	} {
		req, err := http.NewRequest(tc.method, srv.URL+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatalf("%s: NewRequest: %v", tc.name, err)
		}
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: Do: %v", tc.name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", tc.name, resp.StatusCode)
		}
	}
}
