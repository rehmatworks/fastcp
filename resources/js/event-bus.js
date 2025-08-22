// Small, zero-dependency event bus compatible with Vue2-style $on/$off/$emit
// and also exposes on/off/emit aliases for modern usage.
const _listeners = Object.create(null);

function $on(event, fn) {
  (_listeners[event] || (_listeners[event] = [])).push(fn);
}

function $off(event, fn) {
  if (!event) {
    // remove all
    for (const k in _listeners) delete _listeners[k];
    return;
  }
  const list = _listeners[event];
  if (!list) return;
  if (!fn) {
    // remove all for event
    delete _listeners[event];
    return;
  }
  for (let i = list.length - 1; i >= 0; i--) {
    if (list[i] === fn || list[i].fn === fn) list.splice(i, 1);
  }
}

function $emit(event /*, ...args */) {
  const list = _listeners[event];
  if (!list) return;
  const args = Array.prototype.slice.call(arguments, 1);
  // call a copy in case listeners mutate the array
  list.slice().forEach((fn) => {
    try {
      fn.apply(null, args);
    } catch (e) {
      // swallow - matches previous behaviour
    }
  });
}

const bus = {
  $on,
  $off,
  $emit,
  // aliases
  on: $on,
  off: $off,
  emit: $emit,
};

export default bus;
