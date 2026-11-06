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
import 'codemirror/addon/fold/foldgutter.css';
import 'codemirror/theme/material-darker.css';

export default CodeMirror;

export function createEditor(el, opts = {}) {
  return CodeMirror(el, {
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
}
