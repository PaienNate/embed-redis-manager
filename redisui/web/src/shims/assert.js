// Minimal browser-compatible replacement for node's `assert` module.
export default function assert(condition, message) {
  if (!condition) {
    throw new Error(message || 'Assertion failed');
  }
}

export const ok = assert;

export function equal(actual, expected, message) {
  if (actual !== expected) {
    throw new Error(message || `${actual} !== ${expected}`);
  }
}
