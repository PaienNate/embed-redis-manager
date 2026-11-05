import { Buffer } from 'buffer';
import Vue from 'vue';
import ElementUI from 'element-ui';
import 'font-awesome/css/font-awesome.css';
import App from './App';
import i18n from './i18n/i18n';
import bus from './bus';
import util from './util';
import storage from './storage';
import shortcut from './shortcut';

// browser global shims (some deps/util rely on node globals)
window.Buffer = Buffer;
window.process = window.process || { env: {}, platform: '', versions: {} };

// simple light/dark theme toggle (Electron's native theme bridge is not present)
window.globalChangeTheme = (theme) => {
  if (theme === 'dark') {
    document.body.classList.add('dark-mode');
  } else {
    document.body.classList.remove('dark-mode');
  }
};

// vxe-table
import 'vxe-table/lib/style.css';

Vue.prototype.$bus = bus;
Vue.prototype.$util = util;
Vue.prototype.$storage = storage;
Vue.prototype.$shortcut = shortcut;

Vue.use(ElementUI, { size: 'small' });
Vue.config.productionTip = false;

/* eslint-disable no-new */
const vue = new Vue({
  el: '#app',
  i18n,
  components: { App },
  template: '<App/>',
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
