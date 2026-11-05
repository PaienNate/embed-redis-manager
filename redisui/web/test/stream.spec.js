import { describe, it, expect, vi, beforeEach } from 'vitest';

// A fake sockjs-client whose onopen/onclose we drive manually to simulate a
// reconnect (the real library reopens on the same instance, re-firing onopen).
const { FakeSockJS } = vi.hoisted(() => {
  class FakeSockJS {
    constructor() {
      FakeSockJS.instances.push(this);
      this.sent = [];
    }
    send(msg) {
      this.sent.push(msg);
    }
    close() {}
    static instances = [];
  }
  return { FakeSockJS };
});

vi.mock('sockjs-client', () => ({ default: FakeSockJS }));

import RedisStream from '../src/stream.js';

function connectedStream(name = 'conn1') {
  const s = new RedisStream(name);
  s.connect();
  const sock = FakeSockJS.instances[FakeSockJS.instances.length - 1];
  sock.onopen(); // initial session open
  return { s, sock };
}

function sentTypes(sock) {
  return sock.sent.map((m) => JSON.parse(m).type);
}

describe('RedisStream reconnect replay', () => {
  beforeEach(() => {
    FakeSockJS.instances = [];
  });

  it('replays the last subscription control on reconnect', () => {
    const { s, sock } = connectedStream();
    s.subscribe(['news']);
    expect(JSON.parse(sock.sent[sock.sent.length - 1])).toEqual({
      type: 'subscribe',
      channels: ['news'],
    });

    // Server session resets on reconnect: onclose then onopen on the same sock.
    sock.onclose();
    expect(s.isConnected()).toBe(false);
    sock.onopen();

    const replayed = JSON.parse(sock.sent[sock.sent.length - 1]);
    expect(replayed).toEqual({ type: 'subscribe', channels: ['news'] });
    expect(s.isConnected()).toBe(true);
  });

  it('replays monitor intent with no params', () => {
    const { s, sock } = connectedStream();
    s.monitor();
    sock.onclose();
    sock.onopen();
    expect(JSON.parse(sock.sent[sock.sent.length - 1])).toEqual({ type: 'monitor' });
  });

  it('replays psubscribe intent on reconnect', () => {
    const { s, sock } = connectedStream();
    s.psubscribe(['news.*']);
    sock.onclose();
    sock.onopen();
    expect(JSON.parse(sock.sent[sock.sent.length - 1])).toEqual({
      type: 'psubscribe',
      patterns: ['news.*'],
    });
  });

  it('does not replay after an explicit stop', () => {
    const { s, sock } = connectedStream();
    s.subscribe(['news']);
    s.stop();

    sock.onclose();
    sock.onopen();

    // Only the initial subscribe and the stop are sent; no subscribe replayed.
    expect(sentTypes(sock)).toEqual(['subscribe', 'stop']);
  });

  it('captures an intent issued before the session is open and replays it on open', () => {
    const s = new RedisStream('conn2');
    s.connect();
    const sock = FakeSockJS.instances[FakeSockJS.instances.length - 1];
    // Issue intent while still connecting (not yet open).
    s.subscribe(['pending']);
    expect(sock.sent).toHaveLength(0); // nothing sent yet
    sock.onopen();
    expect(JSON.parse(sock.sent[sock.sent.length - 1])).toEqual({
      type: 'subscribe',
      channels: ['pending'],
    });
  });
});
