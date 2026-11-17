(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory();
  else root.Triage = factory();
})(typeof window === 'undefined' ? globalThis : window, function () {
  'use strict';
  const values = new Set(['save', 'discard', 'hold', 'unjudged']);
  function status(row) { return values.has(row && row.triage) ? row.triage : 'unjudged'; }
  function shortcut(event) {
    const target = event.target || {};
    const isInput = /^INPUT$/i.test(target.tagName || '');
    const checkbox = isInput && /^checkbox$/i.test(target.type || '');
    if (event.ctrlKey || event.altKey || event.metaKey || event.shiftKey || event.repeat || event.isComposing || target.isContentEditable || (isInput && !checkbox) || /^(TEXTAREA|SELECT)$/i.test(target.tagName || '') || (target.closest && target.closest('[contenteditable]:not([contenteditable="false"])'))) return null;
    return ({s:'save', d:'discard', h:'hold'})[String(event.key).toLowerCase()] || null;
  }
  function savedCandidates(candidates) { return (candidates || []).filter(candidate => candidate.triage === 'save'); }
  function validPreview(preview, requests) {
    if (!preview || !Array.isArray(preview.items) || preview.items.length !== requests.length || !requests.length) return false;
    return requests.every(request => {
      const matches = preview.items.filter(item => item.id === request.id && item.method === request.method);
      if (matches.length !== 1) return false;
      const targets = matches[0].targets || [];
      const kinds = request.method === 'both' ? ['zip', 'folder'] : [request.method];
      return targets.length === kinds.length && kinds.every(kind => targets.filter(target => target.kind === kind && typeof target.path === 'string' && /^(?:[A-Za-z]:[\\/]|\\\\|\/)/.test(target.path)).length === 1);
    });
  }
  function validDiscardPreview(preview) {
    if (!preview || typeof preview.token !== 'string' || !preview.token || !Array.isArray(preview.paths) || !preview.paths.length || !Array.isArray(preview.targets) || preview.targets.length !== preview.paths.length) return false;
    if (!Number.isInteger(preview.fileCount) || !Number.isInteger(preview.folderCount) || preview.fileCount < 0 || preview.folderCount < 0 || preview.fileCount + preview.folderCount !== preview.paths.length) return false;
    const paths = new Set(preview.paths);
    if (paths.size !== preview.paths.length || !preview.paths.every(path => typeof path === 'string' && /^(?:[A-Za-z]:[\\/]|\\\\|\/)/.test(path))) return false;
    const targets = new Set();
    for (const target of preview.targets) {
      if (!target || !paths.has(target.path) || targets.has(target.path) || !['file', 'folder'].includes(target.kind) || target.reason !== 'manual_discard' || typeof target.itemId !== 'string' || !target.itemId) return false;
      targets.add(target.path);
    }
    return preview.targets.filter(target => target.kind === 'file').length === preview.fileCount && preview.targets.filter(target => target.kind === 'folder').length === preview.folderCount;
  }
  return { status, shortcut, savedCandidates, validPreview, validDiscardPreview };
});
