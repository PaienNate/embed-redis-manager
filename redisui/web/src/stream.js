// RedisStream: a thin wrapper around sockjs-client for the backend's streaming
// channel. It drives server-side Redis events (MONITOR output, pub/sub
// messages) over a single SockJS session and demultiplexes them into JS event
// callbacks.
//
// The backend serves a WebSocket transport plus XHR-streaming and XHR-polling
// fallbacks; sockjs-client auto-degrades across them, so this works even where
// enterprise/gov proxies block WebSocket or SSE. Reconnection is handled by
// sockjs-client itself.
//
// Protocol (JSON over the session, one message per frame):
//   client -> server: { type: "monitor" }
//                     { type: "subscribe", channels: [...] }
//                     { type: "psubscribe", patterns: [...] }
//                     { type: "stop" }
//   server -> client: { type: "monitor", line }            (a raw MONITOR line)
//                     { type: "message", channel, message }
//                     { type: "pmessage", pattern, channel, message }
//                     { type: "error", error }

import SockJS from 'sockjs-client';

export default class RedisStream {
  constructor(connID, options = {}) {
    const apiBase =
      (typeof window !== 'undefined' && window.__REDISUI__ && window.__REDISUI__.apiBase) || '/api';
    const base = String(apiBase).replace(/\/+$/, '');
    this.connID = connID;
    this.url = `${base}/redis/${connID}/stream`;

    this._listeners = { open: [], message: [], error: [], close: [] };
    this._sock = null;
    this._connected = false;

    // Optional transport override (e.g. "xhr-streaming" or "xhr-polling") for
    // testing the degradation path under a blocked WebSocket network.
    this._transport = options.transport || null;
  }

  connect() {
    if (this._sock) return;
    const opts = this._transport ? { transports: [this._transport] } : undefined;
    const sock = new SockJS(this.url, undefined, opts);
    this._sock = sock;

    sock.onopen = () => {
      this._connected = true;
      this._emit('open');
    };
    sock.onmessage = (e) => {
      let data;
      try {
        data = JSON.parse(e.data);
      } catch (err) {
        this._emit('error', new Error(`redis stream: bad event ${e.data}`));
        return;
      }
      this._emit('message', data);
    };
    sock.onerror = (err) => this._emit('error', err);
    sock.onclose = () => {
      this._connected = false;
      this._emit('close');
    };
  }

  // send encodes a control message as JSON and writes it to the session.
  send(obj) {
    if (this._sock && this._connected) {
      this._sock.send(JSON.stringify(obj));
    }
  }

  monitor() {
    this.send({ type: 'monitor' });
  }

  subscribe(channels) {
    this.send({ type: 'subscribe', channels: channels || [] });
  }

  psubscribe(patterns) {
    this.send({ type: 'psubscribe', patterns: patterns || [] });
  }

  stop() {
    this.send({ type: 'stop' });
  }

  isConnected() {
    return this._connected;
  }

  on(event, fn) {
    if (!this._listeners[event]) this._listeners[event] = [];
    this._listeners[event].push(fn);
    return this;
  }

  off(event, fn) {
    const list = this._listeners[event];
    if (list) {
      const i = list.indexOf(fn);
      if (i >= 0) list.splice(i, 1);
    }
    return this;
  }

  _emit(event, ...args) {
    const list = this._listeners[event];
    if (list) list.slice().forEach((fn) => fn(...args));
  }

  close() {
    if (this._sock) {
      this._sock.close();
      this._sock = null;
    }
    this._connected = false;
  }
}

// parseMonitorLine parses a raw MONITOR line:
//   1626... [0 192.168.1.5:6379] "set" "foo" "bar"
// into { time, db, source, args }. If the line does not match (e.g. an error or
// a non-standard payload) it falls back to { raw: line }.
export function parseMonitorLine(line) {
  const m = /^([\d.]+)\s+\[(\d+)\s+(.+?)\]\s*(.*)$/.exec(line);
  if (!m) return { raw: line };
  const rest = m[4];
  const args = [];
  const re = /"((?:[^"\\]|\\.)*)"/g;
  let mm;
  while ((mm = re.exec(rest))) args.push(mm[1]);
  return { time: m[1], db: m[2], source: m[3], args };
}
