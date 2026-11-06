import { describe, it, expect, beforeEach, vi } from 'vitest';

// httpClient now talks to the backend via axios (XHR, IE11-friendly + timeout).
// Mock axios so tests assert on the request it would make.
vi.mock('axios', () => ({
  default: { post: vi.fn(), get: vi.fn() },
}));

import axios from 'axios';
import HttpRedisClient from '../src/httpClient.js';

const ok = (data) => ({ data });

describe('HttpRedisClient', () => {
  beforeEach(() => {
    globalThis.window = { __REDISUI__: { apiBase: '/api' } };
    axios.post.mockReset();
    axios.get.mockReset();
  });

  it('call sends cmd+args and returns string', async () => {
    axios.post.mockResolvedValue(ok({ error: null, reply: { t: 's', v: 'ok' } }));

    const client = new HttpRedisClient('conn1');
    const result = await client.call('SET', 'k', 'v');

    expect(result).toBe('ok');
    expect(axios.post).toHaveBeenCalledTimes(1);
    const [url, body] = axios.post.mock.calls[0];
    expect(url).toBe('/api/redis/conn1/command');
    expect(body).toEqual({
      cmd: 'SET',
      args: [{ t: 's', v: 'k' }, { t: 's', v: 'v' }],
      binary: false,
      db: 0,
    });
  });

  it('call encodes Buffer args as base64 binary args', async () => {
    axios.post.mockResolvedValue(ok({ error: null, reply: { t: 's', v: 'OK' } }));

    const client = new HttpRedisClient('conn1');
    await client.set(Buffer.from('key'), Buffer.from([0x00, 0x01, 0xff]));

    const [, body] = axios.post.mock.calls[0];
    expect(body.cmd).toBe('SET');
    expect(body.args[0]).toEqual({ t: 'b', v: Buffer.from('key').toString('base64') });
    expect(body.args[1]).toEqual({ t: 'b', v: Buffer.from([0x00, 0x01, 0xff]).toString('base64') });
    expect(body.binary).toBe(false);
  });

  it('callBuffer decodes base64 to Buffer', async () => {
    const base64 = Buffer.from('hello').toString('base64');
    axios.post.mockResolvedValue(ok({ error: null, reply: { t: 'b', v: base64 } }));

    const client = new HttpRedisClient('conn1');
    const buf = await client.callBuffer('GET', 'k');

    expect(Buffer.isBuffer(buf)).toBe(true);
    expect(buf.equals(Buffer.from('hello'))).toBe(true);
    const [, body] = axios.post.mock.calls[0];
    expect(body.binary).toBe(true);
  });

  it('restores array/int/nil types', async () => {
    axios.post.mockResolvedValue(
      ok({
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
    axios.post.mockResolvedValue(ok({ error: 'ERR unknown command', reply: null }));

    const client = new HttpRedisClient('conn1');
    await expect(client.call('GET', 'k')).rejects.toThrow('ERR unknown command');
  });

  // C1: a Redis command error is NOT a connection error. ConnectionWrapper
  // tears the connection down on every 'error' event, so emitting here would
  // close the connection as soon as a refused CONFIG GET/INFO runs at open.
  it('command errors reject without emitting the error event', async () => {
    axios.post.mockResolvedValue(ok(
      { error: "NOPERM this user has no permissions to run the 'config' command", reply: null },
    ));

    const client = new HttpRedisClient('conn1');
    const emitted = [];
    client.on('error', (e) => emitted.push(e));

    await expect(client.call('CONFIG', 'GET', 'databases')).rejects.toThrow('NOPERM');
    await expect(client.call('INFO')).rejects.toThrow('NOPERM');
    expect(emitted).toHaveLength(0);
  });

  it('network failures still emit the error event', async () => {
    axios.post.mockRejectedValue(new Error('Network Error'));

    const client = new HttpRedisClient('conn1');
    const emitted = [];
    client.on('error', (e) => emitted.push(e));

    await expect(client.call('PING')).rejects.toThrow('Network Error');
    expect(emitted).toHaveLength(1);
    expect(emitted[0].message).toBe('Network Error');
  });

  it('non-2xx HTTP responses still emit the error event', async () => {
    axios.post.mockRejectedValue({ response: { status: 502, statusText: 'Bad Gateway' } });

    const client = new HttpRedisClient('conn1');
    const emitted = [];
    client.on('error', (e) => emitted.push(e));

    await expect(client.call('PING')).rejects.toThrow('Redis HTTP 502');
    expect(emitted).toHaveLength(1);
  });

  // I1 (option A): every command carries the selected db so the backend can
  // scope it to a dedicated db-pinned client.
  it('sends the currently selected db with every command', async () => {
    axios.post.mockResolvedValue(ok({ error: null, reply: { t: 's', v: 'OK' } }));

    const client = new HttpRedisClient('conn1', { db: 3 });
    await client.call('GET', 'k');
    expect(axios.post.mock.calls[0][1].db).toBe(3);

    await client.select(5);
    expect(axios.post.mock.calls[1][1].db).toBe(3); // SELECT itself still carries the old db
    expect(axios.post.mock.calls[1][1].cmd).toBe('SELECT');

    await client.call('GET', 'k');
    expect(axios.post.mock.calls[2][1].db).toBe(5);
  });

  it('uses /api as the default apiBase when window.__REDISUI__ is missing', async () => {
    delete globalThis.window;
    axios.post.mockResolvedValue(ok({ error: null, reply: { t: 's', v: 'ok' } }));

    const client = new HttpRedisClient('conn2');
    await client.call('PING');

    expect(axios.post.mock.calls[0][0]).toBe('/api/redis/conn2/command');
  });
});

describe('HttpRedisClient scanBufferStream', () => {
  beforeEach(() => {
    globalThis.window = { __REDISUI__: { apiBase: '/api' } };
    axios.post.mockReset();
    axios.get.mockReset();
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

    axios.post
      .mockResolvedValueOnce(ok(page(12, ['k1', 'k2'])))
      .mockResolvedValueOnce(ok(page(0, ['k3'])));

    const client = new HttpRedisClient('conn1');
    const keys = [];
    for await (const k of client.scanBufferStream({ match: '*', count: 2 })) {
      keys.push(k.toString());
    }

    expect(keys).toEqual(['k1', 'k2', 'k3']);
    expect(axios.post).toHaveBeenCalledTimes(2);

    const [, body] = axios.post.mock.calls[0];
    expect(body).toEqual({
      cmd: 'SCAN',
      args: [{ t: 's', v: '0' }, { t: 's', v: 'MATCH' }, { t: 's', v: '*' }, { t: 's', v: 'COUNT' }, { t: 's', v: '2' }],
      binary: true,
      db: 0,
    });
  });
});

describe('HttpRedisClient stream / array-arg / nodes', () => {
  beforeEach(() => {
    globalThis.window = { __REDISUI__: { apiBase: '/api' } };
    axios.post.mockReset();
    axios.get.mockReset();
  });

  it('xadd flattens a field/value array into the command args', async () => {
    axios.post.mockResolvedValue(ok({ error: null, reply: { t: 's', v: '1-0' } }));

    const client = new HttpRedisClient('conn1');
    await client.xadd('mystream', '*', ['f1', 'v1', 'f2', 'v2']);

    const [, body] = axios.post.mock.calls[0];
    expect(body).toEqual({
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
      db: 0,
    });
  });

  it('xrevrangeBuffer issues XREVRANGE in binary mode and decodes Buffers', async () => {
    const id = Buffer.from('1-0').toString('base64');
    axios.post.mockResolvedValue(ok({
      error: null,
      reply: { t: 'a', v: [{ t: 'a', v: [{ t: 's', v: '1-0' }, { t: 'a', v: [{ t: 'b', v: id }] }] }] },
    }));

    const client = new HttpRedisClient('conn1');
    const reply = await client.xrevrangeBuffer(['mystream', '+', '-', 'COUNT', '10']);

    const [, body] = axios.post.mock.calls[0];
    expect(body).toEqual({
      cmd: 'XREVRANGE',
      args: [
        { t: 's', v: 'mystream' },
        { t: 's', v: '+' },
        { t: 's', v: '-' },
        { t: 's', v: 'COUNT' },
        { t: 's', v: '10' },
      ],
      binary: true,
      db: 0,
    });
    expect(reply[0][1][0].equals(Buffer.from('1-0'))).toBe(true);
  });

  it('xdel / xlen go through the generic command endpoint', async () => {
    axios.post
      .mockResolvedValueOnce(ok({ error: null, reply: { t: 'i', v: 1 } }))
      .mockResolvedValueOnce(ok({ error: null, reply: { t: 'i', v: 1 } }));
    const client = new HttpRedisClient('conn1');

    await client.xdel('mystream', '1-0');
    expect(axios.post.mock.calls[0][1].cmd).toBe('XDEL');

    await client.xlen('mystream');
    expect(axios.post.mock.calls[1][1].cmd).toBe('XLEN');
  });

  it('call flattens a single array of arguments (JSON.SET style)', async () => {
    axios.post.mockResolvedValue(ok({ error: null, reply: { t: 's', v: 'OK' } }));

    const client = new HttpRedisClient('conn1');
    await client.call('JSON.SET', ['doc', '$', '{"a":1}']);

    const [, body] = axios.post.mock.calls[0];
    expect(body).toEqual({
      cmd: 'JSON.SET',
      args: [
        { t: 's', v: 'doc' },
        { t: 's', v: '$' },
        { t: 's', v: '{"a":1}' },
      ],
      binary: false,
      db: 0,
    });
  });

  it('nodes() fetches the cluster nodes endpoint and returns the node list', async () => {
    axios.get.mockResolvedValue(ok({
      error: null,
      nodes: [
        { id: 'a', addr: '10.0.0.1:6379', role: 'master', slots: ['0-5460'] },
      ],
    }));

    const client = new HttpRedisClient('conn1');
    const nodes = await client.nodes();

    expect(axios.get).toHaveBeenCalledTimes(1);
    expect(axios.get.mock.calls[0][0]).toBe('/api/redis/conn1/cluster/nodes');
    expect(nodes).toHaveLength(1);
    expect(nodes[0].role).toBe('master');
  });

  it('nodes() returns an empty array for a non-cluster (empty nodes) reply', async () => {
    axios.get.mockResolvedValue(ok({ error: null, nodes: [] }));

    const client = new HttpRedisClient('conn1');
    const nodes = await client.nodes();
    expect(nodes).toEqual([]);
  });

  it('nodes() rejects a command error without emitting the error event', async () => {
    axios.get.mockResolvedValue(ok({ error: 'ERR cluster support disabled', nodes: null }));

    const client = new HttpRedisClient('conn1');
    const emitted = [];
    client.on('error', (e) => emitted.push(e));

    await expect(client.nodes()).rejects.toThrow('cluster support disabled');
    expect(emitted).toHaveLength(0);
  });
});
