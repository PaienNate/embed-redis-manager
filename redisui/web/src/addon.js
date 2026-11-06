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
    // window zoom is not applicable in a browser, noop
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
