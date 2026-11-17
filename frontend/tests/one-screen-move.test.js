const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../dist/app.js'), 'utf8');
function extract(start, end, c) {
  vm.createContext(c);
  vm.runInContext(source.slice(source.indexOf(start), source.indexOf(end, source.indexOf(start))), c);
}

// 通常ケース（検証済みフォルダのみ）は、方法選択と最終確認を1画面に統合する。
// 同じ内容を2回確認させないことが Issue #46 の要件。
test('normal cleanup goes straight to the final dialog without a method step', async () => {
  const dialogs = [];
  const previewCalls = [];
  const c = {
    state: { busy: false, selected: new Map([['a', { deletable: true }]]) },
    clearPendingDragSelection() {},
    updateBusyUI() {},
    updateSelectionUI() {},
    setStatus() {},
    toast() {},
    targetsForOption: () => [{ kind: 'folder', path: 'C:\\x', size: 1 }],
    api: async (name, args) => {
      if (name === 'GetMoveOptions') return [{ id: 'a', canFolder: true, name: 'a' }];
      if (name === 'PreviewMove') {
        previewCalls.push(args);
        return { items: [{ id: 'a', name: 'a', method: 'folder', targets: [] }], itemCount: 1, targetCount: 1, bytes: 1 };
      }
      throw new Error('unexpected api ' + name);
    },
    window: { Triage: { validPreview: () => true } },
    showMoveFinalDialog: (preview, requests) => dialogs.push({ preview, requests }),
    showMoveMethodDialog: () => {
      throw new Error('method dialog must not be shown for the normal case');
    },
  };
  extract('  async function beginDelete()', '  function showMoveMethodDialog()', c);
  await c.beginDelete();

  assert.equal(dialogs.length, 1, 'exactly one dialog is shown');
  assert.equal(dialogs[0].requests.length, 1);
  assert.equal(dialogs[0].requests[0].method, 'folder');
  assert.equal(previewCalls.length, 1, 'preview is fetched once');
});

test('normal cleanup defaults to folder-only and never selects zip or both', async () => {
  const requests = [];
  const c = {
    state: { busy: false, selected: new Map([['a', { deletable: true }]]) },
    clearPendingDragSelection() {},
    updateBusyUI() {},
    updateSelectionUI() {},
    setStatus() {},
    toast() {},
    targetsForOption: () => [{ kind: 'folder', path: 'C:\\x', size: 1 }],
    api: async (name) => {
      if (name === 'GetMoveOptions') return [{ id: 'a', canFolder: true, canZip: true, canBoth: true, name: 'a' }];
      if (name === 'PreviewMove') return { items: [], itemCount: 1, targetCount: 1, bytes: 1 };
      throw new Error('unexpected api ' + name);
    },
    window: { Triage: { validPreview: () => true } },
    showMoveFinalDialog: (preview, reqs) => requests.push(...reqs),
    showMoveMethodDialog: () => {},
  };
  extract('  async function beginDelete()', '  function showMoveMethodDialog()', c);
  await c.beginDelete();

  assert.equal(requests.length, 1);
  assert.equal(requests[0].method, 'folder', 'zip and both must not be preselected');
});

test('cleanup with no movable folder reports instead of opening a dialog', async () => {
  const messages = [];
  let shown = false;
  const c = {
    state: { busy: false, selected: new Map([['a', { deletable: true }]]) },
    clearPendingDragSelection() {},
    updateBusyUI() {},
    updateSelectionUI() {},
    setStatus() {},
    toast: (m) => messages.push(m),
    targetsForOption: () => [],
    api: async (name) => {
      if (name === 'GetMoveOptions') return [{ id: 'a', canFolder: false, name: 'a' }];
      throw new Error('unexpected api ' + name);
    },
    window: { Triage: { validPreview: () => true } },
    showMoveFinalDialog: () => {
      shown = true;
    },
    showMoveMethodDialog: () => {},
  };
  extract('  async function beginDelete()', '  function showMoveMethodDialog()', c);
  await c.beginDelete();

  assert.equal(shown, false);
  assert.ok(messages.length > 0, 'user is told why nothing can move');
});

test('final dialog offers cancel (not back) for the normal folder-only case', () => {
  const dialogs = [];
  const c = {
    state: {},
    esc: String,
    fmtBytes: () => '1 B',
    moveKindLabel: () => 'フォルダ',
    methodLabel: () => 'フォルダのみ',
    openModal: (o) => dialogs.push(o),
    closeModal() {},
    showMoveMethodDialog() {},
    executeMove() {},
    updateMoveFinalButton() {},
    document: { getElementById: () => null },
  };
  extract('  function showMoveFinalDialog(', '  function updateMoveFinalButton()', c);
  c.showMoveFinalDialog(
    { items: [{ id: 'a', name: 'a', method: 'folder', targets: [] }], itemCount: 1, targetCount: 1, bytes: 1 },
    [{ id: 'a', method: 'folder' }]
  );

  const labels = dialogs[0].actions.map((a) => a.label);
  assert.ok(labels.includes('キャンセル'), 'normal case can be cancelled');
  assert.ok(!labels.includes('戻る'), 'normal case does not go back to a method step');
  assert.ok(labels.includes('ごみ箱に移動'));
});

test('final dialog keeps the back action for high-risk methods', () => {
  const dialogs = [];
  const c = {
    state: {},
    esc: String,
    fmtBytes: () => '1 B',
    moveKindLabel: () => 'フォルダ',
    methodLabel: () => '両方',
    openModal: (o) => dialogs.push(o),
    closeModal() {},
    showMoveMethodDialog() {},
    executeMove() {},
    updateMoveFinalButton() {},
    document: { getElementById: () => null },
  };
  extract('  function showMoveFinalDialog(', '  function updateMoveFinalButton()', c);
  c.showMoveFinalDialog(
    { items: [{ id: 'a', name: 'a', method: 'both', targets: [] }], itemCount: 1, targetCount: 1, bytes: 1 },
    [{ id: 'a', method: 'both' }]
  );

  const labels = dialogs[0].actions.map((a) => a.label);
  assert.ok(labels.includes('戻る'), 'high-risk case can return to method selection');
});
