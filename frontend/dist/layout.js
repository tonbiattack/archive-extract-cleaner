(function (root) {
  "use strict";
  function clamp(value, min, max, fallback) {
    const n = Number(value);
    return Number.isFinite(n) ? Math.min(max, Math.max(min, Math.round(n))) : fallback;
  }
  function createSizing(storage) {
    function read(key, min, max, fallback) {
      try {
        const value = storage && storage.getItem(key);
        return value == null || value === '' ? fallback : clamp(value, min, max, fallback);
      } catch (_) { return fallback; }
    }
    function save(key, value) {
      try { if (storage) storage.setItem(key, String(value)); } catch (_) { /* session only */ }
    }
    let row = read('row', 24, 64, 34);
    let upper = read('upper', 80, 800, 280);
    return {
      row: () => row,
      upper: available => Math.min(upper, Math.max(0, Math.round(available - 120))),
      setRow(value) { row = clamp(value, 24, 64, 34); save('row', row); },
      setUpper(value) { upper = clamp(value, 80, 800, 280); save('upper', upper); },
    };
  }
  const api = { createSizing };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  else root.LayoutSizing = api;
})(typeof window !== 'undefined' ? window : globalThis);
