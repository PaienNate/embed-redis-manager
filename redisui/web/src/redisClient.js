// Embedded/browser build of the redis client.
//
// The original ARDM `redisClient.js` established direct redis connections in the
// Electron main process using `ioredis` + `tunnel-ssh` + `fs`, none of which are
// usable from a browser. In the embedded web build the Go backend owns the actual
// redis connections; the frontend talks to it over HTTP. The connection/command
// glue is wired to the backend proxy in a later task, so the client factory is
// stubbed here for now to keep the UI scaffold building & rendering.

export default {
  createConnection(host, port, auth, config, promise = true, forceStandalone = false, removeDb = false) {
    return Promise.reject(new Error('Redis connection proxy is not wired yet'));
  },

  createSSHConnection(sshOptions, host, port, auth, config) {
    return Promise.reject(new Error('Redis connection proxy is not wired yet'));
  },

  getRedisOptions(host, port, auth, config) {
    return {
      host,
      port,
      family: 0,
      connectTimeout: 30000,
      enableReadyCheck: false,
      connectionName: config.connectionName ? config.connectionName : null,
      password: auth,
      db: config.db ? config.db : undefined,
      username: config.username ? config.username : undefined,
      tls: config.sslOptions ? this.getTLSOptions(config.sslOptions) : undefined,
      connectionReadOnly: config.connectionReadOnly ? true : undefined,
      // return int as string to avoid big number issues
      stringNumbers: true,
    };
  },

  getSentinelOptions(host, port, auth, config) {
    return {
      sentinels: [{ host, port }],
      sentinelPassword: auth,
      password: config.sentinelOptions.nodePassword,
      name: config.sentinelOptions.masterName,
      connectTimeout: 30000,
      enableReadyCheck: false,
      connectionName: config.connectionName ? config.connectionName : null,
      db: config.db ? config.db : undefined,
      username: config.username ? config.username : undefined,
      tls: config.sslOptions ? this.getTLSOptions(config.sslOptions) : undefined,
    };
  },

  getClusterOptions(redisOptions, natMap = {}) {
    return {
      connectionName: redisOptions.connectionName,
      enableReadyCheck: false,
      slotsRefreshTimeout: 30000,
      redisOptions,
      natMap,
    };
  },

  getTLSOptions(options) {
    // CA/key/cert were previously read from local files via `fs` in Electron;
    // in the browser they must be provided inline (no filesystem access).
    return {
      key: options.key,
      cert: options.cert,
      ca: options.ca,
      servername: options.servername || undefined,
      checkServerIdentity: () => undefined,
      rejectUnauthorized: false,
    };
  },

  retryStragety(times) {
    if (times >= 3) {
      return false;
    }
    return Math.min(times * 200, 1000);
  },
};
