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
    expect(JSON.parse(init.body)).toEqual({
      cmd: 'SET',
      args: [{ t: 's', v: 'k' }, { t: 's', v: 'v' }],
      binary: false,
    });
  });

  it('call encodes Buffer args as base64 binary args', async () => {
    fetch.mockResolvedValue(jsonResponse({ error: null, reply: { t: 's', v: 'OK' } }));

    const client = new HttpRedisClient('conn1');
    await client.set(Buffer.from('key'), Buffer.from([0x00, 0x01, 0xff]));

    const [, init] = fetch.mock.calls[0];
    const body = JSON.parse(init.body);
    expect(body.cmd).toBe('SET');
    expect(body.args[0]).toEqual({ t: 'b', v: Buffer.from('key').toString('base64') });
    expect(body.args[1]).toEqual({ t: 'b', v: Buffer.from([0x00, 0x01, 0xff]).toString('base64') });
    expect(body.binary).toBe(false);
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
      args: [{ t: 's', v: '0' }, { t: 's', v: 'MATCH' }, { t: 's', v: '*' }, { t: 's', v: 'COUNT' }, { t: 's', v: '2' }],
      binary: true,
    });
  });
});

describe('HttpRedisClient stream / array-arg / nodes', () => {
  beforeEach(() => {
    globalThis.window = { __REDISUI__: { apiBase: '/api' } };
    globalThis.fetch = vi.fn();
  });

  it('xadd flattens a field/value array into the command args', async () => {
    fetch.mockResolvedValue(jsonResponse({ error: null, reply: { t: 's', v: '1-0' } }));

    const client = new HttpRedisClient('conn1');
    await client.xadd('mystream', '*', ['f1', 'v1', 'f2', 'v2']);

    const [, init] = fetch.mock.calls[0];
    expect(JSON.parse(init.body)).toEqual({
      cmd: 'XADD',
      args: [
        { t: 's', v: 'mystream' },
        { t: 's', v: '*' },
        { t: 's', v: 'f1' },
        { t: 's', v: 'v1' },
        { t: 's', v: 'f2' },
        { t: 's', v: 'v2' },
      ],
      binary: false,
    });
  });

  it('xrevrangeBuffer issues XREVRANGE in binary mode and decodes Buffers', async () => {
    const id = Buffer.from('1-0').toString('base64');
    fetch.mockResolvedValue(jsonResponse({
      error: null,
      reply: { t: 'a', v: [{ t: 'a', v: [{ t: 's', v: '1-0' }, { t: 'a', v: [{ t: 'b', v: id }] }] }] },
    }));

    const client = new HttpRedisClient('conn1');
    const reply = await client.xrevrangeBuffer(['mystream', '+', '-', 'COUNT', '10']);

    const [, init] = fetch.mock.calls[0];
    expect(JSON.parse(init.body)).toEqual({
      cmd: 'XREVRANGE',
      args: [
        { t: 's', v: 'mystream' },
        { t: 's', v: '+' },
        { t: 's', v: '-' },
        { t: 's', v: 'COUNT' },
        { t: 's', v: '10' },
      ],
      binary: true,
    });
    expect(reply[0][1][0].equals(Buffer.from('1-0'))).toBe(true);
  });

  it('xdel / xlen go through the generic command endpoint', async () => {
    fetch
      .mockResolvedValueOnce(jsonResponse({ error: null, reply: { t: 'i', v: 1 } }))
      .mockResolvedValueOnce(jsonResponse({ error: null, reply: { t: 'i', v: 1 } }));
    const client = new HttpRedisClient('conn1');

    await client.xdel('mystream', '1-0');
    expect(JSON.parse(fetch.mock.calls[0][1].body).cmd).toBe('XDEL');

    await client.xlen('mystream');
    expect(JSON.parse(fetch.mock.calls[1][1].body).cmd).toBe('XLEN');
  });

  it('call flattens a single array of arguments (JSON.SET style)', async () => {
    fetch.mockResolvedValue(jsonResponse({ error: null, reply: { t: 's', v: 'OK' } }));

    const client = new HttpRedisClient('conn1');
    await client.call('JSON.SET', ['doc', '$', '{"a":1}']);

    const [, init] = fetch.mock.calls[0];
    expect(JSON.parse(init.body)).toEqual({
      cmd: 'JSON.SET',
      args: [
        { t: 's', v: 'doc' },
        { t: 's', v: '$' },
        { t: 's', v: '{"a":1}' },
      ],
      binary: false,
    });
  });

  it('nodes() fetches the cluster nodes endpoint and returns the node list', async () => {
    fetch.mockResolvedValue(jsonResponse({
      error: null,
      nodes: [
        { id: 'a', addr: '10.0.0.1:6379', role: 'master', slots: ['0-5460'] },
      ],
    }));

    const client = new HttpRedisClient('conn1');
    const nodes = await client.nodes();

    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][0]).toBe('/api/redis/conn1/cluster/nodes');
    expect(nodes).toHaveLength(1);
    expect(nodes[0].role).toBe('master');
  });

  it('nodes() returns an empty array for a non-cluster (empty nodes) reply', async () => {
    fetch.mockResolvedValue(jsonResponse({ error: null, nodes: [] }));

    const client = new HttpRedisClient('conn1');
    const nodes = await client.nodes();
    expect(nodes).toEqual([]);
  });
});
