import 'url-polyfill';
import ResizeObserverPolyfill from 'resize-observer-polyfill';
import { Buffer } from 'buffer';

// IE11 has no ResizeObserver; the key tree's virtual scroller / Element UI use
// it when present. Install a polyfill before the app mounts.
if (typeof window !== 'undefined' && !window.ResizeObserver) {
  window.ResizeObserver = ResizeObserverPolyfill;
}
import Vue from 'vue';
import ElementUI from 'element-ui';
import 'font-awesome/css/font-awesome.css';
import App from './App';
import i18n from './i18n/i18n';
import bus from './bus';
import util from './util';
import storage from './storage';
import shortcut from './shortcut';
import { setCommandLogSink } from './httpClient';

// browser global shims (some deps/util rely on node globals)
window.Buffer = Buffer;
window.process = window.process || { env: {}, platform: '', versions: {} };

// ---- Element UI theme ----------------------------------------------------
// ARDM ships the full Element UI theme prebuilt from element-variables.scss at
// static/theme/<theme>/index.css and activates it with a <link> (webpack copied
// `static/` verbatim). Replicate that in the browser. Paths are relative so the
// stylesheet resolves correctly under any host prefix (e.g. /redisui/).
const THEME_CSS = {
  light: 'static/theme/light/index.css',
  dark: 'static/theme/dark/index.css',
};

function applyTheme(theme) {
  const t = theme === 'dark' ? 'dark' : 'light';
  let link = document.getElementById('theme-link');
  if (!link) {
    link = document.createElement('link');
    link.id = 'theme-link';
    link.rel = 'stylesheet';
    // Insert BEFORE the app's bundled CSS so app-level overrides of Element
    // styles (equal specificity, e.g. `.status-filter-input { width:100px }`
    // vs Element's `.el-input { width:100% }`) win. Appending here would let the
    // theme stylesheet win the cascade and break many layout tweaks.
    document.head.insertBefore(link, document.head.firstChild);
  }
  link.href = THEME_CSS[t];
  document.body.classList.toggle('dark-mode', t === 'dark');
}

// used by the Setting page's theme switch
window.globalChangeTheme = (theme) => {
  applyTheme(theme);
};

// initial theme: stored preference, otherwise follow the OS preference
(() => {
  const stored = localStorage.theme;
  const prefersDark = window.matchMedia
    && window.matchMedia('(prefers-color-scheme: dark)').matches;
  applyTheme(stored === 'dark' || stored === 'light' ? stored : (prefersDark ? 'dark' : 'light'));
})();

// vxe-table
import 'vxe-table/lib/style.css';

Vue.prototype.$bus = bus;
Vue.prototype.$util = util;
Vue.prototype.$storage = storage;
Vue.prototype.$shortcut = shortcut;

// Forward every redis command to the command-log panel (CommandLog.vue listens
// for `commandLog` on the bus). Registered here so all HttpRedisClient
// instances — including CLI duplicates — are covered.
setCommandLogSink((record) => bus.$emit('commandLog', record));

Vue.use(ElementUI, { size: 'small' });
Vue.config.productionTip = false;

/* eslint-disable no-new */
const vue = new Vue({
  el: '#app',
  i18n,
  // render function (no runtime template compilation needed — the bundle ships
  // the runtime-only Vue build; a `template` string would compile to nothing).
  render: (h) => h(App),
});

// handle uncaught exception (browser: window error handler)
window.addEventListener('error', (event) => {
  const err = event.error || event.message;
  if (!err) {
    return;
  }

  vue.$message.error({
    message: `Uncaught Exception: ${err}`,
    duration: 5000,
  });

  vue.$bus.$emit('closeConnection');
});

export default vue;
