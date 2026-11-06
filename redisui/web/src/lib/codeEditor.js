// Lightweight code editor wrapper.
//
// Replaces Monaco (which is ~3.8MB and dropped IE11 support) with CodeMirror 5
// (~200KB, IE11-compatible). Only the features the app actually used are wired
// up: value get/set, read-only mode, JSON syntax highlighting, folding
// (foldAll/unfoldAll), wrapping and refresh-on-resize.

import CodeMirror from 'codemirror';
import 'codemirror/lib/codemirror.css';
import 'codemirror/mode/javascript/javascript';
import 'codemirror/addon/fold/foldcode';
import 'codemirror/addon/fold/brace-fold';
import 'codemirror/addon/fold/foldgutter';
import 'codemirror/addon/fold/foldgutter.css';
import 'codemirror/theme/material-darker.css';

export default CodeMirror;

export function createEditor(el, opts = {}) {
  const cm = CodeMirror(el, {
    value: opts.value || '',
    // JSON is highlighted via the javascript mode's json flag; plaintext (CLI)
    // uses no mode.
    mode: opts.json ? { name: 'javascript', json: true } : null,
    theme: opts.theme || 'default',
    readOnly: !!opts.readOnly,
    lineNumbers: false,
    lineWrapping: true,
    foldGutter: !!opts.json,
    gutters: opts.json ? ['CodeMirror-foldgutter'] : [],
    indentUnit: 4,
    tabSize: 4,
    viewportMargin: 10,
  });

  // CodeMirror measures its layout once on creation. If the editor mounts while
  // hidden or before its container has a final size (inactive tab, theme load,
  // pane resize), the text/caret is misaligned until refresh(). Monaco handled
  // this with automaticLayout; CodeMirror needs an explicit refresh, so observe
  // the container and refresh on size changes (also covers tab activation).
  if (typeof ResizeObserver !== 'undefined') {
    const ro = new ResizeObserver(() => cm.refresh());
    ro.observe(el);
    cm._redisuiResizeObserver = ro;
  }

  return cm;
}

// destroyEditor disconnects the size observer and removes the editor DOM.
export function destroyEditor(cm) {
  if (!cm) {
    return;
  }
  if (cm._redisuiResizeObserver) {
    cm._redisuiResizeObserver.disconnect();
    cm._redisuiResizeObserver = null;
  }
  const wrapper = cm.getWrapperElement();
  if (wrapper && wrapper.parentNode) {
    wrapper.parentNode.removeChild(wrapper);
  }
}
