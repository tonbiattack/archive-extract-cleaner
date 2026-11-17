const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../dist/app.js'), 'utf8');

test('transfer review displays the numeric total of both candidate groups', () => {
  const context = {
    state: { transferRecommendedChecked: false, transferReviewSelected: new Set() },
    esc: value => String(value),
    VERDICT_LABEL: {},
  };
  vm.createContext(context);
  for (const [start, end] of [
    ['  function transferGroups(', '  function showTransferReview('],
    ['  function transferReviewBody(', '  function updateTransferSelection('],
  ]) {
    const from = source.indexOf(start);
    const to = source.indexOf(end, from);
    assert.ok(from >= 0 && to > from, 'transfer rendering functions exist');
    vm.runInContext(source.slice(from, to), context);
  }
  const candidate = { id: 'a', path: 'a.zip', exists: true, review: false };
  for (const [candidates, expected] of [
    [[candidate], 1],
    [[candidate, { ...candidate, id: 'b', review: true }], 2],
    [[], 0],
  ]) {
    const html = context.transferReviewBody({ candidates, missingCount: 0, scriptAvailable: true });
    assert.match(html, new RegExp(`転送候補</div><div class="stat-value">${expected}</div>`));
  }
});
