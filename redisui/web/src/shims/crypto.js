// Minimal `crypto` shim.
//
// The only runtime consumer is sockjs-client's random-string generator, which
// calls `crypto.randomBytes(n)`. Polyfilling Node's `crypto` pulls in
// crypto-browserify + browserify-sign + elliptic + … (~0.8MB); this shim is a
// few hundred bytes and works in IE11 too (window.msCrypto).

import { Buffer } from 'buffer';

function fillRandom(byteArray) {
  const c = typeof self !== 'undefined' ? (self.crypto || self.msCrypto) : null;
  if (c && typeof c.getRandomValues === 'function') {
    c.getRandomValues(byteArray);
    return byteArray;
  }
  // Fallback (should not be reached in a browser); keep sockjs functional.
  for (let i = 0; i < byteArray.length; i += 1) {
    byteArray[i] = Math.floor(Math.random() * 256);
  }
  return byteArray;
}

export function randomBytes(size, callback) {
  const buf = Buffer.from(fillRandom(new Uint8Array(size)));
  if (typeof callback === 'function') {
    callback(null, buf);
    return undefined;
  }
  return buf;
}

export function randomFillSync(buf) {
  fillRandom(buf);
  return buf;
}

export default { randomBytes, randomFillSync };
