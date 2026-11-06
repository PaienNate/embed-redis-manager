package redisui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/igm/sockjs-go/sockjs"
)

// This file implements the SockJS streaming endpoint that pushes server-side
// Redis events (MONITOR output, pub/sub messages) to the browser.
//
// SockJS is used (rather than WebSocket/SSE directly) so that the channel keeps
// working behind enterprise/government proxies that block WebSocket or SSE:
// sockjs-go serves a WebSocket transport plus XHR-streaming / XHR-polling
// fallbacks, and the sockjs-client frontend degrades across them automatically.

// streamControl is the one-time control message a frontend sends over a stream
// session to start (or switch) a subscription. It is re-sent only when the
// desired subscription mode changes; each session holds at most one at a time.
type streamControl struct {
	Type     string   `json:"type"`     // "monitor" | "subscribe" | "psubscribe" | "stop"
	Channels []string `json:"channels"` // for type "subscribe"
	Patterns []string `json:"patterns"` // for type "psubscribe"
}

// streamEvent is the JSON envelope pushed to the frontend. Exactly one of
// Line / Message / Channel / Pattern / Error is meaningful per Type.
type streamEvent struct {
	Type    string `json:"type"`              // "monitor"|"message"|"pmessage"|"error"
	Channel string `json:"channel,omitempty"` // pub/sub channel
	Pattern string `json:"pattern,omitempty"` // psubscribe pattern
	Message string `json:"message,omitempty"` // pub/sub payload
	Line    string `json:"line,omitempty"`    // raw MONITOR line
	Error   string `json:"error,omitempty"`   // error text
}

// registerStream builds the SockJS handler for one enabled connection and
// stores it on the server keyed by connection ID. SockJS routes its transport
// sub-paths under a fixed literal prefix (it embeds that prefix in its own path
// regexps), so a dedicated handler (and prefix) is needed per connection.
func (s *server) registerStream(c Connection) {
	streamPrefix := s.prefix + "/api/redis/" + c.ID + "/stream"
	client := c.Client
	handler := sockjs.NewHandler(streamPrefix, sockjs.DefaultOptions,
		func(sess sockjs.Session) { s.runStream(client, sess) })
	s.stream[c.ID] = handler
}

// handleStream is the single entry point for the stream path. It validates the
// {connID} and dispatches to the corresponding per-connection SockJS handler,
// returning 404 for disabled or unknown connections (so those requests are not
// silently served by the SPA catch-all).
func (s *server) handleStream(w http.ResponseWriter, r *http.Request) {
	connID := r.PathValue("connID")
	handler, ok := s.stream[connID]
	if !ok {
		http.NotFound(w, r)
		return
	}
	handler.ServeHTTP(w, r)
}

// runStream drives one SockJS session: it reads the frontend's control messages
// on a dedicated goroutine, maintains the current subscription, and forwards
// every Redis event to the session. When the session closes (client disconnects
// or Recv/Send errors), the active subscription is torn down.
func (s *server) runStream(client Client, sess sockjs.Session) {
	ctrl := make(chan streamControl)
	// done lets the Recv goroutine exit instead of blocking forever on `ctrl <-`
	// if runStream returns (e.g. Send failed) while the goroutine is between
	// Recv and the channel send.
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(ctrl)
		for {
			raw, err := sess.Recv()
			if err != nil {
				// Session closed on the client side (or an unrecoverable error);
				// close ctrl so runStream tears down the subscription.
				return
			}
			var ctrlMsg streamControl
			if err := json.Unmarshal([]byte(raw), &ctrlMsg); err != nil {
				sendStream(sess, streamEvent{Type: "error", Error: "invalid stream control message"})
				continue
			}
			select {
			case ctrl <- ctrlMsg:
			case <-done:
				return
			}
		}
	}()

	// The currently active subscription's event channel and teardown func. A nil
	// channel blocks (never fires) in the select below, which is what we want
	// when no subscription is active.
	var events <-chan streamEvent
	var stop func()
	defer func() {
		if stop != nil {
			stop()
		}
	}()

	for {
		select {
		case ctrlMsg, ok := <-ctrl:
			if !ok {
				return
			}
			if stop != nil {
				stop()
				stop = nil
				events = nil
			}
			var err error
			events, stop, err = startSubscription(client, ctrlMsg)
			if err != nil {
				sendStream(sess, streamEvent{Type: "error", Error: err.Error()})
			}
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if err := sess.Send(mustJSON(ev)); err != nil {
				// Client is gone; the deferred stop cleans up.
				return
			}
		}
	}
}

// startSubscription establishes the subscription described by ctrl and returns
// its event channel plus a teardown function. On an invalid request it returns
// a nil channel, a nil stop, and an error (the caller surfaces the error to the
// frontend and keeps the session idle).
func startSubscription(client Client, ctrl streamControl) (<-chan streamEvent, func(), error) {
	switch ctrl.Type {
	case "monitor":
		return startMonitor(client)
	case "subscribe":
		if len(ctrl.Channels) == 0 {
			return nil, nil, fmt.Errorf("redisui: subscribe requires at least one channel")
		}
		return startPubSub(client, ctrl.Channels, false)
	case "psubscribe":
		if len(ctrl.Patterns) == 0 {
			return nil, nil, fmt.Errorf("redisui: psubscribe requires at least one pattern")
		}
		return startPubSub(client, ctrl.Patterns, true)
	case "stop", "unsubscribe", "monitor-stop":
		// Idle: no subscription. (The caller already stopped the prior one.)
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("redisui: unknown stream control type %q", ctrl.Type)
	}
}

// startMonitor starts a MONITOR feed and streams raw lines to the returned
// event channel. The adapter owns the dedicated connection: MONITOR takes over
// the connection it runs on, so it must never land on a host-pooled connection.
func startMonitor(client Client) (<-chan streamEvent, func(), error) {
	if client == nil {
		return nil, nil, fmt.Errorf("redisui: MONITOR requires a single-node redis client")
	}
	mon, err := client.Monitor(context.Background())
	if err != nil {
		return nil, nil, err
	}

	out := make(chan streamEvent, 256)
	done := make(chan struct{})
	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				return
			case line, ok := <-mon.Lines():
				if !ok {
					return
				}
				select {
				case out <- streamEvent{Type: "monitor", Line: line}:
				case <-done:
					return
				}
			}
		}
	}()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(done)
			mon.Stop()
		})
	}
	return out, stop, nil
}

// startPubSub starts a SUBSCRIBE (pattern=false) or PSUBSCRIBE (pattern=true)
// on the client and streams each received message to the returned event channel.
// The Redis subscription is created from the injected UniversalClient, which
// exposes Subscribe/PSubscribe regardless of the concrete client type.
func startPubSub(client Client, keys []string, pattern bool) (<-chan streamEvent, func(), error) {
	ctx, cancel := context.WithCancel(context.Background())
	var sub Subscription
	if pattern {
		sub = client.PSubscribe(ctx, keys...)
	} else {
		sub = client.Subscribe(ctx, keys...)
	}

	out := make(chan streamEvent, 256)
	go func() {
		defer close(out)
		defer sub.Close()
		msgs := sub.Messages()
		for {
			var ev streamEvent
			select {
			case <-ctx.Done():
				return
			case m, ok := <-msgs:
				if !ok {
					return
				}
				if pattern {
					ev = streamEvent{Type: "pmessage", Pattern: m.Pattern, Channel: m.Channel, Message: m.Payload}
				} else {
					ev = streamEvent{Type: "message", Channel: m.Channel, Message: m.Payload}
				}
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	stop := func() { cancel() }
	return out, stop, nil
}

// sendStream marshals an event and sends it to the SockJS session, ignoring a
// send failure (the session is going away; runStream will notice on the next
// Recv/Send and tear down).
func sendStream(sess sockjs.Session, ev streamEvent) {
	_ = sess.Send(mustJSON(ev))
}

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"type":"error","error":"redisui: failed to encode stream event"}`
	}
	return string(b)
}
