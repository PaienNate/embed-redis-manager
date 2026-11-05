import { describe, it, expect, beforeEach, vi } from 'vitest';
import HttpRedisClient from '../src/httpClient.js';

function jsonResponse(body) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('HttpRedisClient', () => {
  beforeEach(() => {
    globalThis.window = { __REDISUI__: { apiBase: '/api' } };
    globalThis.fetch = vi.fn();
  });

  it('call sends cmd+args and returns string', async () => {
    fetch.mockResolvedValue(jsonResponse({ error: null, reply: { t: 's', v: 'ok' } }));

    const client = new HttpRedisClient('conn1');
    const result = await client.call('SET', 'k', 'v');

    expect(result).toBe('ok');
    expect(fetch).toHaveBeenCalledTimes(1);
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe('/api/redis/conn1/command');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ cmd: 'SET', args: ['k', 'v'], binary: false });
  });

  it('callBuffer decodes base64 to Buffer', async () => {
    const base64 = Buffer.from('hello').toString('base64');
    fetch.mockResolvedValue(jsonResponse({ error: null, reply: { t: 'b', v: base64 } }));

    const client = new HttpRedisClient('conn1');
    const buf = await client.callBuffer('GET', 'k');

    expect(Buffer.isBuffer(buf)).toBe(true);
    expect(buf.equals(Buffer.from('hello'))).toBe(true);
    const [url, init] = fetch.mock.calls[0];
    expect(JSON.parse(init.body).binary).toBe(true);
  });

  it('restores array/int/nil types', async () => {
    fetch.mockResolvedValue(
      jsonResponse({
        error: null,
        reply: { t: 'a', v: [{ t: 'i', v: 42 }, { t: 'n' }, { t: 's', v: 'hi' }] },
      })
    );

    const client = new HttpRedisClient('conn1');
    const result = await client.call('EXISTS', 'k');

    expect(Array.isArray(result)).toBe(true);
    expect(result).toEqual([42, null, 'hi']);
  });

  it('call rejects on error reply', async () => {
    fetch.mockResolvedValue(jsonResponse({ error: 'ERR unknown command', reply: null }));

    const client = new HttpRedisClient('conn1');
    await expect(client.call('GET', 'k')).rejects.toThrow('ERR unknown command');
  });

  it('uses /api as the default apiBase when window.__REDISUI__ is missing', async () => {
    delete globalThis.window;
    fetch.mockResolvedValue(jsonResponse({ error: null, reply: { t: 's', v: 'ok' } }));

    const client = new HttpRedisClient('conn2');
    await client.call('PING');

    expect(fetch.mock.calls[0][0]).toBe('/api/redis/conn2/command');
  });
});

describe('HttpRedisClient scanBufferStream', () => {
  beforeEach(() => {
    globalThis.window = { __REDISUI__: { apiBase: '/api' } };
    globalThis.fetch = vi.fn();
  });

  it('iterates cursor pages and collects all keys', async () => {
    const page = (cursor, keys) => ({
      error: null,
      reply: {
        t: 'a',
        v: [
          { t: 's', v: String(cursor) },
          { t: 'a', v: keys.map((k) => ({ t: 'b', v: Buffer.from(k).toString('base64') })) },
        ],
      },
    });

    fetch
      .mockResolvedValueOnce(jsonResponse(page(12, ['k1', 'k2'])))
      .mockResolvedValueOnce(jsonResponse(page(0, ['k3'])));

    const client = new HttpRedisClient('conn1');
    const keys = [];
    for await (const k of client.scanBufferStream({ match: '*', count: 2 })) {
      keys.push(k.toString());
    }

    expect(keys).toEqual(['k1', 'k2', 'k3']);
    expect(fetch).toHaveBeenCalledTimes(2);

    const [, init1] = fetch.mock.calls[0];
    expect(JSON.parse(init1.body)).toEqual({
      cmd: 'SCAN',
      args: ['0', 'MATCH', '*', 'COUNT', '2'],
      binary: true,
    });
  });
});
