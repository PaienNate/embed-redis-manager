// HttpRedisClient: an ioredis-compatible proxy that drives redis through the
// Go backend's generic command endpoint instead of a direct socket.
//
//     POST {apiBase}/redis/{connID}/command   body { cmd, args, binary }
//
// The response is `{ error: string|null, reply: <tagged> }`. Tagged replies:
//   { t:'s', v }   string
//   { t:'i', v }   int
//   { t:'a', v:[...] }  array (recursively tagged)
//   { t:'n' }      nil
//   { t:'b', v:'<base64>' }  binary bulk (present when binary=true)
//
// When `binary` is true, bulk strings/array elements are base64 and are
// restored as Buffers on the JS side.

import { Buffer } from 'buffer';

function decodeReply(reply, binary) {
  switch (reply.t) {
    case 's':
      return reply.v;
    case 'i':
      return reply.v;
    case 'n':
      return null;
    case 'b':
      return binary ? Buffer.from(reply.v, 'base64') : reply.v;
    case 'a':
      return reply.v.map((el) => decodeReply(el, binary));
    default:
      return reply.v;
  }
}

export default class HttpRedisClient {
  constructor(connID, options = {}) {
    this.connID = connID;

    const apiBase =
      (typeof window !== 'undefined' && window.__REDISUI__ && window.__REDISUI__.apiBase) || '/api';
    this.apiBase = String(apiBase).replace(/\/+$/, '');
    this.url = `${this.apiBase}/redis/${connID}/command`;

    this.status = 'ready';
    this._listeners = {};

    // The backend owns the actual socket; a SELECT is issued on demand, so we
    // track the current db here for the various components that read
    // `condition`/`options` to decide whether a db switch is needed.
    const db = parseInt(options.db) || 0;
    this.condition = { select: db, subscriber: {} };
    this.options = {
      connectionName: options.connectionName != null ? options.connectionName : null,
      db,
    };
    // INFO-derived server version info, populated by the Status tab; default to
    // an empty object so readers (e.g. hash field-TTL feature detection) don't
    // crash on an undefined `ardmInfo`.
    this.ardmInfo = {};
  }

  async _send(cmd, args, binary) {
    // Args are forwarded to Go verbatim as command arguments and the backend
    // decodes them into a []string, so every arg must be a string: Buffers are
    // converted to their UTF-8 form (matching how the value was read back as a
    // Buffer) and numbers (e.g. LRANGE start/stop) are stringified.
    const encodedArgs = args.map((a) => (Buffer.isBuffer(a) ? a.toString() : String(a)));

    const resp = await fetch(this.url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cmd, args: encodedArgs, binary }),
    });

    if (!resp.ok) {
      const e = new Error(`Redis HTTP ${resp.status} ${resp.statusText}`);
      this.emit('error', e);
      throw e;
    }

    const data = await resp.json();
    if (data.error) {
      const e = new Error(data.error);
      this.emit('error', e);
      throw e;
    }

    return decodeReply(data.reply, binary);
  }

  call(cmd, ...args) {
    return this._send(cmd, args, false);
  }

  callBuffer(cmd, ...args) {
    return this._send(cmd, args, true);
  }

  getBuffer(key) {
    return this.callBuffer('GET', key);
  }

  // Binary (base64) variants for the value-editing commands that read back
  // actual bytes. These accept either `(key, ...)` or a single array argument,
  // matching the way the content components invoke them.
  lrangeBuffer(...args) {
    if (args.length === 1 && Array.isArray(args[0])) args = args[0];
    return this.callBuffer('LRANGE', ...args);
  }

  zrangeBuffer(...args) {
    if (args.length === 1 && Array.isArray(args[0])) args = args[0];
    return this.callBuffer('ZRANGE', ...args);
  }

  zrevrangeBuffer(...args) {
    if (args.length === 1 && Array.isArray(args[0])) args = args[0];
    return this.callBuffer('ZREVRANGE', ...args);
  }

  async *_scanStream(command, key, opts, binary) {
    const callFn = binary ? this.callBuffer.bind(this) : this.call.bind(this);
    const match = opts && opts.match;
    const count = opts && opts.count;
    let cursor = (opts && opts.cursor != null) ? opts.cursor : 0;

    // SCAN takes no key; the *SCAN variants take a key as the second arg.
    while (true) {
      const args = [command];
      if (key != null) args.push(key);
      args.push(String(cursor));
      if (match != null) args.push('MATCH', match);
      if (count != null) args.push('COUNT', String(count));

      const reply = await callFn(...args);
      const [nextCursor, items] = reply;

      if (Array.isArray(items)) {
        for (const item of items) yield item;
      }

      const raw = Array.isArray(nextCursor) ? nextCursor[0] : nextCursor;
      cursor = typeof raw === 'object' && raw !== null ? Number(raw.toString()) : Number(raw);
      if (cursor === 0) break;
    }
  }

  scanBufferStream(opts) {
    return this._scanStream('SCAN', null, opts, true);
  }

  hscanBufferStream(key, opts) {
    return this._scanStream('HSCAN', key, opts, true);
  }

  sscanBufferStream(key, opts) {
    return this._scanStream('SSCAN', key, opts, true);
  }

  zscanBufferStream(key, opts) {
    return this._scanStream('ZSCAN', key, opts, true);
  }

  scan(cursor = 0, match, count) {
    const args = [String(cursor)];
    if (match != null) args.push('MATCH', match);
    if (count != null) args.push('COUNT', String(count));
    return this.call('SCAN', ...args);
  }

  nodes() {
    return Promise.resolve(null);
  }

  ping() {
    return this.call('PING');
  }

  quit() {
    return this.call('QUIT');
  }

  duplicate() {
    return new HttpRedisClient(this.connID);
  }

  on(event, fn) {
    if (!this._listeners[event]) this._listeners[event] = [];
    this._listeners[event].push(fn);
    return this;
  }

  off(event, fn) {
    const list = this._listeners[event];
    if (list) {
      const idx = list.indexOf(fn);
      if (idx >= 0) list.splice(idx, 1);
    }
    return this;
  }

  emit(event, ...args) {
    const list = this._listeners[event];
    if (list) list.forEach((fn) => fn(...args));
    return this;
  }
}

// Method-name -> command-name mapping for the common redis commands.
const COMMAND_ALIASES = {
  get: 'GET',
  set: 'SET',
  setnx: 'SETNX',
  del: 'DEL',
  unlink: 'UNLINK',
  exists: 'EXISTS',
  expire: 'EXPIRE',
  expireat: 'EXPIREAT',
  pexpire: 'PEXPIRE',
  ttl: 'TTL',
  pttl: 'PTTL',
  persist: 'PERSIST',
  hget: 'HGET',
  hset: 'HSET',
  hsetnx: 'HSETNX',
  hdel: 'HDEL',
  hgetall: 'HGETALL',
  hkeys: 'HKEYS',
  hvals: 'HVALS',
  hlen: 'HLEN',
  lpush: 'LPUSH',
  rpush: 'RPUSH',
  lpop: 'LPOP',
  rpop: 'RPOP',
  lrange: 'LRANGE',
  llen: 'LLEN',
  lindex: 'LINDEX',
  lset: 'LSET',
  linsert: 'LINSERT',
  lrem: 'LREM',
  sadd: 'SADD',
  srem: 'SREM',
  smembers: 'SMEMBERS',
  scard: 'SCARD',
  sismember: 'SISMEMBER',
  zadd: 'ZADD',
  zrem: 'ZREM',
  zrange: 'ZRANGE',
  zrangebyscore: 'ZRANGEBYSCORE',
  zscore: 'ZSCORE',
  zcard: 'ZCARD',
  incr: 'INCR',
  decr: 'DECR',
  incrby: 'INCRBY',
  decrby: 'DECRBY',
  mget: 'MGET',
  mset: 'MSET',
  getset: 'GETSET',
  append: 'APPEND',
  strlen: 'STRLEN',
  type: 'TYPE',
  rename: 'RENAME',
  select: 'SELECT',
  flushdb: 'FLUSHDB',
  flushall: 'FLUSHALL',
  dbsize: 'DBSIZE',
  info: 'INFO',
  keys: 'KEYS',
  randomkey: 'RANDOMKEY',
};

for (const alias of Object.keys(COMMAND_ALIASES)) {
  const cmd = COMMAND_ALIASES[alias];
  HttpRedisClient.prototype[alias] = function (...args) {
    return this.call(cmd, ...args);
  };
}

// `select` needs to track the current db in `condition.select`/`options.db`
// (the alias loop above would otherwise discard that state), so override it.
HttpRedisClient.prototype.select = async function (index) {
  const reply = await this.call('SELECT', index);
  const db = parseInt(index);
  this.condition.select = db;
  this.options.db = db;
  return reply;
};
