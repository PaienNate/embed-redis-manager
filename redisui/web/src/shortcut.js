import keymaster from 'keymaster';

// enable shortcut in input, textarea, select
keymaster.filter = e => true;

// prevent ctrl+r
keymaster('ctrl+r, ⌘+r', e => false);

export default {
  bind: (...args) => keymaster(...args),
  ...keymaster,
};
