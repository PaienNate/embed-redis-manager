import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue2';
import { nodePolyfills } from 'vite-plugin-node-polyfills';
import { fileURLToPath, URL } from 'node:url';

// https://vitejs.dev/config/
export default defineConfig({
  // relative base so the built SPA can be served under any host prefix
  // (e.g. /redisui/) via the Go backend; asset refs become ./assets/...
  base: './',
  plugins: [
    vue(),
    // provide browser globals (Buffer/process/global/globalThis) used directly
    // across components; the browser runtime has none of these by default.
    nodePolyfills({
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
      'keymaster',
      'getopts',
      'sortablejs',
    ],
  },
  build: {
    outDir: 'dist',
    chunkSizeWarningLimit: 4000,
  },
});
