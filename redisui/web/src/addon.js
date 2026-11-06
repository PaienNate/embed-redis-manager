import bus from './bus';
import storage from './storage';

export default {
  setup() {
    // reload settings when init
    this.reloadSettings();
    // bing href click
    this.openHrefInBrowser();
  },
  reloadSettings() {
    this.initFont();
    this.initZoom();
  },
  initFont() {
    const fontFamily = storage.getFontFamily();
    document.body.style.fontFamily = fontFamily;
    // tell code editors (CodeMirror) about the font
    bus.$emit('fontInited', fontFamily);
  },
  initZoom() {
    // Electron's webFrame zoom is gone; apply a CSS zoom to the page instead
    // (supported by Chromium/IE11 and modern Firefox).
    const z = parseFloat(storage.getSetting('zoomFactor')) || 1;
    document.body.style.zoom = String(z);
  },
  openHrefInBrowser() {
    document.addEventListener('click', (event) => {
      const ele = event.target;

      if (ele && (ele.nodeName.toLowerCase() === 'a') && ele.href.startsWith('http')) {
        event.preventDefault();
        window.open(ele.href, '_blank', 'noopener');
      }
    });
  },
};
