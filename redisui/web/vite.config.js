import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue2';
import legacy from '@vitejs/plugin-legacy';
import { nodePolyfills } from 'vite-plugin-node-polyfills';
import { fileURLToPath, URL } from 'node:url';

// https://vitejs.dev/config/
export default defineConfig({
  // relative base so the built SPA can be served under any host prefix
  // (e.g. /redisui/) via the Go backend; asset refs become ./assets/...
  base: './',
  plugins: [
    vue(),
    // IE11 (and other legacy 政企 browsers): emit a nomodule ES5 bundle with
    // core-js polyfills. Modern browsers load the normal module bundle.
    legacy({
      targets: ['ie >= 11'],
      additionalLegacyPolyfills: ['regenerator-runtime/runtime'],
      renderLegacyChunks: true,
    }),
    // provide browser globals (Buffer/process/global) used directly across
    // components. Node builtin polyfills are NOT needed at runtime (the app is
    // fully HTTP-based), so keep only the globals — otherwise the `crypto`
    // polyfill drags in the whole crypto-browserify/elliptic stack (~0.8MB).
    nodePolyfills({
      exclude: ['crypto'],
      protocolImports: false,
      globals: {
        Buffer: true,
        process: true,
        global: true,
        globalThis: true,
      },
    }),
  ],
  resolve: {
    extensions: ['.mjs', '.js', '.mts', '.ts', '.jsx', '.tsx', '.json', '.vue', '.scss'],
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      // NOTE: the bundle uses the runtime-only Vue build; SFCs are compiled at
      // build time by @vitejs/plugin-vue2. Do not add a webpack-style 'vue$'
      // alias here — Vite's alias matches by prefix and ignores the '$' suffix,
      // so 'vue$' is silently dead. main.js mounts via a render function.
      // node builtin shims for browser build
      assert: fileURLToPath(new URL('./src/shims/assert.js', import.meta.url)),
      'assert/': fileURLToPath(new URL('./src/shims/', import.meta.url)),
      // replace Node's crypto (used only by sockjs-client for randomBytes) with
      // a tiny shim instead of the ~0.8MB crypto-browserify/elliptic stack.
      crypto: fileURLToPath(new URL('./src/shims/crypto.js', import.meta.url)),
      // @qii404/vue-easy-tree published package declares a missing `dist/` main;
      // point to the actual ESM entry it ships.
      '@qii404/vue-easy-tree': fileURLToPath(new URL('./node_modules/@qii404/vue-easy-tree/index.js', import.meta.url)),
    },
  },
  define: {
    'process.env': {},
    global: 'globalThis',
  },
  optimizeDeps: {
    include: [
      'element-ui',
      'vue-i18n',
      '@qii404/json-bigint',
      '@qii404/redis-splitargs',
      '@qii404/vue-easy-tree',
      'algo-msgpack-with-bigint',
      'java-object-serialization',
      'php-serialize',
      'pickleparser',
      'protobufjs',
      'rawproto',
      'vxe-table',
      'codemirror',
      'keymaster',
      'getopts',
      'sortablejs',
    ],
  },
  build: {
    outDir: 'dist',
    chunkSizeWarningLimit: 4000,
    // Emit small assets (notably the subset woff/ttf) as files instead of
    // inlining them as data URIs: some legacy browsers (IE11) are unreliable
    // with data-URI web fonts.
    assetsInlineLimit: 0,
  },
});
