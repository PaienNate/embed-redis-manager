// Bridge the proxy's async-iterable *SCAN stream into the EventEmitter-style
// scanStream interface the original ARDM consumers use.
//
// `HttpRedisClient.scanBufferStream()` / `hscanBufferStream()` / ... return an
// async iterable that yields one element at a time while streaming cursor
// pages, whereas the original ioredis scanStream emitted page arrays through
// `on('data')` and supported `pause()`/`resume()` (used for "load more"). This
// adapter turns the async iterable back into that shape: elements are buffered
// and flushed to `data` as page-sized arrays (matching ioredis semantics), and
// `pause()`/`resume()` gate the underlying `next()` calls so the generator
// keeps its cursor between pages. Batching also avoids the O(n^2) cost of
// re-concatenating an array one element at a time when loading many keys.
export function createScanStream(iterable, batchSize) {
  const listeners = { data: [], error: [], end: [] };
  const iterator = iterable && iterable[Symbol.asyncIterator]
    ? iterable[Symbol.asyncIterator]()
    : iterable;
  const batch = batchSize || 500;

  let paused = false;
  let pumping = false;
  let buffer = [];

  const stream = {
    on(event, fn) {
      (listeners[event] = listeners[event] || []).push(fn);
      return stream;
    },
    off(event, fn) {
      const arr = listeners[event];
      if (arr) {
        const i = arr.indexOf(fn);
        if (i >= 0) arr.splice(i, 1);
      }
      return stream;
    },
    pause() {
      paused = true;
    },
    resume() {
      paused = false;
      pump();
    },
    emit(event, ...args) {
      (listeners[event] || []).forEach((fn) => fn(...args));
    },
  };

  const flush = () => {
    if (!buffer.length) {
      return;
    }
    const batchOut = buffer;
    buffer = [];
    stream.emit('data', batchOut);
  };

  const pump = async () => {
    if (pumping) {
      return;
    }
    pumping = true;
    try {
      while (!paused) {
        const { value, done } = await iterator.next();
        if (done) {
          flush();
          stream.emit('end');
          break;
        }
        buffer.push(value);
        if (buffer.length >= batch) {
          flush();
        }
      }
    } catch (e) {
      stream.emit('error', e);
    } finally {
      pumping = false;
    }
  };

  pump();
  return stream;
}

export default createScanStream;
