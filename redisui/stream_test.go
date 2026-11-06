package redisui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/igm/sockjs-go/sockjs"
	"github.com/redis/go-redis/v9"

	"embedRedisClient/redisui/goredisv9"
)

// ----------------------------------------------------------------------------
// Pure unit tests (no Redis required)
// ----------------------------------------------------------------------------

func TestStartSubscriptionUnknownType(t *testing.T) {
	_, stop, err := startSubscription(nil, streamControl{Type: "bogus"})
	if err == nil {
		t.Fatal("startSubscription(bogus) expected an error, got nil")
	}
	if stop != nil {
		t.Fatal("expected no stop func on error")
	}
}

func TestStartSubscriptionSubscribeNeedsChannels(t *testing.T) {
	_, _, err := startSubscription(nil, streamControl{Type: "subscribe"})
	if err == nil {
		t.Fatal("startSubscription(subscribe, no channels) expected an error")
	}
}

func TestStartSubscriptionPsNeedsPatterns(t *testing.T) {
	_, _, err := startSubscription(nil, streamControl{Type: "psubscribe"})
	if err == nil {
		t.Fatal("startSubscription(psubscribe, no patterns) expected an error")
	}
}

func TestStartSubscriptionStopIsIdle(t *testing.T) {
	for _, typ := range []string{"stop", "unsubscribe", "monitor-stop"} {
		events, stop, err := startSubscription(nil, streamControl{Type: typ})
		if err != nil {
			t.Fatalf("startSubscription(%q) unexpected error: %v", typ, err)
		}
		if stop != nil {
			t.Fatalf("startSubscription(%q) expected nil stop", typ)
		}
		if events != nil {
			t.Fatalf("startSubscription(%q) expected nil events channel", typ)
		}
	}
}

func TestStartMonitorRejectsNonSingleNodeClient(t *testing.T) {
	// A nil UniversalClient is not a *redis.Client.
	_, _, err := startMonitor(nil)
	if err == nil {
		t.Fatal("startMonitor(nil) expected an error")
	}
}

func TestStreamEventEnvelopeJSON(t *testing.T) {
	got := mustJSON(streamEvent{Type: "message", Channel: "news", Message: "hello"})
	want := `{"type":"message","channel":"news","message":"hello"}`
	if got != want {
		t.Fatalf("envelope = %s, want %s", got, want)
	}

	// Monitor/message variants omit the irrelevant fields.
	got = mustJSON(streamEvent{Type: "monitor", Line: "123 [0 x] \"set\" \"k\""})
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(got), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["type"] != "monitor" || obj["line"] != `123 [0 x] "set" "k"` {
		t.Fatalf("unexpected monitor payload: %s", got)
	}
	if _, ok := obj["channel"]; ok {
		t.Fatalf("monitor event should not carry channel: %s", got)
	}
}

// TestSockJSInfoEndpoint verifies the SockJS endpoint is mounted under the
// connection's stream path (no Redis interaction: /info does not open a
// session), and that disabled connections are left unmounted (404).
func TestSockJSInfoEndpoint(t *testing.T) {
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: miniredisClient(t), Enabled: true})

	req := httptest.NewRequest(http.MethodGet, "/r/api/redis/c1/stream/info", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET stream/info status = %d, want 200", rec.Code)
	}
	var info map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("unmarshal info: %v; body=%s", err, rec.Body.String())
	}
	if info["websocket"] != true {
		t.Fatalf("info.websocket = %v, want true (sockjs serves websocket transport); body=%s", info["websocket"], rec.Body.String())
	}
}

// TestSockJSXHRTransportRouted verifies the SockJS transports that use POST
// (xhr / xhr_send) are routed to the stream handler rather than falling through
// to the SPA catch-all (which would serve index.html). The XHR open frame ("o")
// is produced without any Redis interaction, so a miniredis client suffices.
func TestSockJSXHRTransportRouted(t *testing.T) {
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: miniredisClient(t), Enabled: true})

	req := httptest.NewRequest(http.MethodPost, "/r/api/redis/c1/stream/srv/sess/xhr", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("xhr POST status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Body.String(), "o") {
		t.Fatalf("xhr open frame = %q, want to start with 'o' (SockJS open frame) not the SPA index", rec.Body.String())
	}

	// xhr_send must also reach the handler (204 No Content for a valid send).
	sendReq := httptest.NewRequest(http.MethodPost,
		"/r/api/redis/c1/stream/srv/sess/xhr_send",
		strings.NewReader(`["{\"type\":\"stop\"}"]`))
	sendRec := httptest.NewRecorder()
	mux.ServeHTTP(sendRec, sendReq)
	if sendRec.Code != http.StatusNoContent {
		t.Fatalf("xhr_send status = %d, want 204; body=%s", sendRec.Code, sendRec.Body.String())
	}
}

func TestSockJSDisabledConnectionNotFound(t *testing.T) {
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: miniredisClient(t), Enabled: false})

	req := httptest.NewRequest(http.MethodGet, "/r/api/redis/c1/stream/info", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled connection stream/info status = %d, want 404", rec.Code)
	}
}

// ----------------------------------------------------------------------------
// Real-Redis integration helpers
// ----------------------------------------------------------------------------

// integrationRedisAddr returns the Redis address to test against. It is
// overridable with REDISUI_TEST_REDIS_ADDR; when no Redis is reachable the
// integration tests are skipped so `go test ./redisui` still passes in a bare
// environment (miniredis cannot exercise MONITOR / pub-sub streaming).
func integrationRedisAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("REDISUI_TEST_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6389"
	}
	probe := redis.NewClient(&redis.Options{Addr: addr, DialTimeout: 500 * time.Millisecond})
	defer probe.Close()
	if err := probe.Ping(context.Background()).Err(); err != nil {
		t.Skipf("integration Redis not reachable at %s: %v", addr, err)
	}
	return addr
}

// realRedisRawClient returns the unwrapped go-redis client bound to the
// integration Redis, or skips the test when it is unavailable. Writers use it
// for typed commands (Set/Publish); the client under test is wrapped.
func realRedisRawClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := integrationRedisAddr(t)
	return redis.NewClient(&redis.Options{Addr: addr})
}

// realRedisClient returns the adapter the UI consumes.
func realRedisClient(t *testing.T) Client {
	return goredisv9.New(realRedisRawClient(t))
}

// ----------------------------------------------------------------------------
// Integration tests for the subscription wrappers
// ----------------------------------------------------------------------------

func TestMonitorStreamsCommands(t *testing.T) {
	// Must use a *redis.Client (the only concrete type MONITOR supports).
	addr := integrationRedisAddr(t)
	client := goredisv9.New(redis.NewClient(&redis.Options{Addr: addr}))

	events, stop, err := startMonitor(client)
	if err != nil {
		t.Fatalf("startMonitor: %v", err)
	}
	defer stop()

	// Let the MONITOR connection enter monitoring mode, then drive traffic
	// through a separate client.
	time.Sleep(150 * time.Millisecond)
	writer := redis.NewClient(&redis.Options{Addr: addr})
	defer writer.Close()
	if err := writer.Set(context.Background(), "mon:key", "mon:val", 0).Err(); err != nil {
		t.Fatalf("writer set: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type == "monitor" && strings.Contains(ev.Line, `"set" "mon:key" "mon:val"`) {
				return // success
			}
		case <-deadline:
			t.Fatal("timed out waiting for a MONITOR line")
		}
	}
}

func TestSubscribeStreamsMessages(t *testing.T) {
	client := realRedisClient(t)
	events, stop, err := startSubscription(client, streamControl{Type: "subscribe", Channels: []string{"stream:chan"}})
	if err != nil {
		t.Fatalf("startSubscription: %v", err)
	}
	defer stop()

	time.Sleep(150 * time.Millisecond)
	writer := realRedisRawClient(t)
	defer writer.Close()
	if err := writer.Publish(context.Background(), "stream:chan", "hello-sub").Err(); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type == "message" && ev.Channel == "stream:chan" && ev.Message == "hello-sub" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for a subscribe message")
		}
	}
}

func TestPSubscribeStreamsMessages(t *testing.T) {
	client := realRedisClient(t)
	events, stop, err := startSubscription(client, streamControl{Type: "psubscribe", Patterns: []string{"stream:*"}})
	if err != nil {
		t.Fatalf("startSubscription: %v", err)
	}
	defer stop()

	time.Sleep(150 * time.Millisecond)
	writer := realRedisRawClient(t)
	defer writer.Close()
	if err := writer.Publish(context.Background(), "stream:tech", "hello-psub").Err(); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type == "pmessage" && ev.Pattern == "stream:*" &&
				ev.Channel == "stream:tech" && ev.Message == "hello-psub" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for a psubscribe message")
		}
	}
}

// ----------------------------------------------------------------------------
// End-to-end: runStream against a fake SockJS session backed by real Redis
// ----------------------------------------------------------------------------

// fakeSession is a minimal sockjs.Session backed by in-memory channels so we can
// drive runStream without a real WebSocket/XHR transport.
type fakeSession struct {
	in   chan string // control messages the "client" has sent
	out  chan string // events the server sent to the "client"
	once sync.Once
}

func newFakeSession() *fakeSession {
	return &fakeSession{in: make(chan string, 16), out: make(chan string, 64)}
}

func (f *fakeSession) Recv() (string, error) {
	s, ok := <-f.in
	if !ok {
		return "", http.ErrAbortHandler
	}
	return s, nil
}

func (f *fakeSession) Send(msg string) error {
	f.out <- msg
	return nil
}

func (f *fakeSession) Close(status uint32, reason string) error { f.close(); return nil }
func (f *fakeSession) ID() string                               { return "fake" }
func (f *fakeSession) Request() *http.Request                   { return nil }
func (f *fakeSession) GetSessionState() sockjs.SessionState     { return sockjs.SessionActive }
func (f *fakeSession) close()                                   { f.once.Do(func() { close(f.in) }) }

func TestRunStreamEndToEnd(t *testing.T) {
	client := realRedisClient(t)
	sess := newFakeSession()

	go func() {
		(&server{}).runStream(client, sess)
	}()

	// Start a subscribe mode.
	sess.in <- `{"type":"subscribe","channels":["e2e:chan"]}`
	time.Sleep(150 * time.Millisecond)
	writer := realRedisRawClient(t)
	defer writer.Close()
	if err := writer.Publish(context.Background(), "e2e:chan", "payload-1").Err(); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg := <-sess.out:
			var ev streamEvent
			if err := json.Unmarshal([]byte(msg), &ev); err != nil {
				t.Fatalf("unmarshal stream event %q: %v", msg, err)
			}
			if ev.Type == "message" && ev.Channel == "e2e:chan" && ev.Message == "payload-1" {
				// Prove that closing the session tears the subscription down.
				sess.close()
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for an end-to-end stream event")
		}
	}
}

// parseWSFrame extracts the message strings from a SockJS websocket frame, e.g.
// `a["msg1","msg2"]` -> ["msg1","msg2"]. It tolerates the leading transport
// marker ("a" for data, "o" for open, "h" for heartbeat) and any raw JSON.
func parseWSFrame(frame string) ([]string, error) {
	if len(frame) == 0 {
		return nil, nil
	}
	// Strip a single leading SockJS frame marker (a/o/h/c).
	if c := frame[0]; c == 'a' || c == 'o' || c == 'h' || c == 'c' {
		frame = frame[1:]
	}
	var msgs []string
	if err := json.Unmarshal([]byte(frame), &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

// TestSockJSEndpointSubscribeOverWebSocket exercises the real SockJS HTTP
// endpoint (registerStream + sockjs-go websocket transport) end-to-end: the
// client connects over WebSocket, subscribes to a channel, and receives the
// published message as a stream event.
func TestSockJSEndpointSubscribeOverWebSocket(t *testing.T) {
	client := realRedisClient(t)
	mux := commandServer(t, Connection{ID: "ws1", Name: "ws1", Client: client, Enabled: true})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") +
		"/r/api/redis/ws1/stream/srv1/sess1/websocket"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("sockjs websocket dial: %v", err)
	}
	defer conn.Close()

	// Ignore the initial "o" (open) frame that sockjs sends on connect.
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read open frame: %v", err)
	}

	// Send a SockJS-framed subscribe control message.
	ctrlRaw, _ := json.Marshal(streamControl{Type: "subscribe", Channels: []string{"wschan"}})
	frame, _ := json.Marshal([]string{string(ctrlRaw)})
	if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
		t.Fatalf("ws write subscribe: %v", err)
	}

	// Let the subscription establish, then publish from a separate connection.
	time.Sleep(150 * time.Millisecond)
	writer := realRedisRawClient(t)
	defer writer.Close()
	if err := writer.Publish(context.Background(), "wschan", "ws-hello").Err(); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-time.After(50 * time.Millisecond):
			_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			_, raw, err := conn.ReadMessage()
			if err != nil {
				// Timeout/no frame yet; keep waiting until the overall deadline.
				continue
			}
			msgs, err := parseWSFrame(string(raw))
			if err != nil {
				t.Fatalf("parse ws frame %q: %v", raw, err)
			}
			for _, msg := range msgs {
				var ev streamEvent
				if err := json.Unmarshal([]byte(msg), &ev); err != nil {
					continue // not a stream event (e.g. heartbeat)
				}
				if ev.Type == "message" && ev.Channel == "wschan" && ev.Message == "ws-hello" {
					return
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for a subscribe event over WebSocket")
		}
	}
}
