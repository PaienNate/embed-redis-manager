// Embedded/browser build of the redis client.
//
// The original ARDM `redisClient.js` established direct redis connections in the
// Electron main process using `ioredis` + `tunnel-ssh` + `fs`, none of which are
// usable from a browser. In the embedded web build the Go backend owns the actual
// redis connections; the frontend talks to it over HTTP through `HttpRedisClient`.
// A connection is identified purely by the backend-provided `connID`, so the
// factory no longer reads host/port/auth/ssh options.

import HttpRedisClient from '@/httpClient.js';

export default {
  // Return an ioredis-compatible proxy bound to the backend connection `connID`.
  // `options` optionally carries the display `connectionName` / initial `db`
  // so the proxy can track `condition.select`/`options.db` for db switching.
  createConnection(connID, options) {
    return Promise.resolve(new HttpRedisClient(connID, options));
  },

  // SSH tunnels are not supported in the embedded build: the Go backend
  // establishes (and already holds) the redis connections, not the frontend.
  createSSHConnection() {
    return Promise.reject(new Error('SSH connections are not supported in the embedded build'));
  },
};
