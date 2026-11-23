const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../dist/app.js'), 'utf8');

// scriptUnavailableNote とそれを使う transferReviewBody を読み込む。
function loadTransferHelpers() {
  const context = {
    state: { transferRecommendedChecked: false, transferReviewSelected: new Set() },
    esc: (value) => String(value),
    VERDICT_LABEL: {},
  };
  vm.createContext(context);
  for (const [start, end] of [
    ['  function transferGroups(', '  function showTransferReview('],
    ['  function scriptUnavailableNote(', '  function transferReviewBody('],
    ['  function transferReviewBody(', '  function updateTransferSelection('],
  ]) {
    const from = source.indexOf(start);
    const to = source.indexOf(end, from);
    assert.ok(from >= 0 && to > from, 'transfer helper exists: ' + start.trim());
    vm.runInContext(source.slice(from, to), context);
  }
  return context;
}

// multi-drive-copy が未導入でも、コピー以外は使えることを案内すること。
test('missing multi-drive-copy tells the user the rest still works', () => {
  const c = loadTransferHelpers();
  const note = c.scriptUnavailableNote({
    scriptAvailable: false,
    scriptReason: 'not_installed',
    scriptPath: 'C:\\Users\\me\\AppData\\Local\\Programs\\MultiDriveCopy\\MultiDriveCopy.ps1',
  });
  assert.match(note, /multi-drive-copy が見つかりません/);
  assert.match(note, /スキャン・照合・ごみ箱への移動は、このまま使えます/);
  assert.match(note, /github\.com\/tonbiattack\/multi-drive-copy/);
});

// 設定したパスが消えている場合は、未導入とは別の案内にすること。
test('configured but missing script is reported as a path problem', () => {
  const c = loadTransferHelpers();
  const note = c.scriptUnavailableNote({
    scriptAvailable: false,
    scriptReason: 'not_found',
    scriptPath: 'D:\\Tools\\MultiDriveCopy.ps1',
  });
  assert.match(note, /設定されたスクリプトが見つかりません/);
  assert.match(note, /multiDriveCopy\.scriptPath/);
  assert.doesNotMatch(note, /スキャン・照合・ごみ箱への移動は、このまま使えます/);
});

// 権限不足と、ファイルでないパスを区別すること。
test('permission and non-file reasons are distinct', () => {
  const c = loadTransferHelpers();
  const denied = c.scriptUnavailableNote({ scriptAvailable: false, scriptReason: 'permission_denied', scriptPath: 'C:\\locked.ps1' });
  assert.match(denied, /アクセスできません/);

  const notFile = c.scriptUnavailableNote({ scriptAvailable: false, scriptReason: 'not_file', scriptPath: 'C:\\Tools' });
  assert.match(notFile, /ファイルではありません/);
});

// 未導入でも転送候補の一覧そのものは表示されること。
test('candidates are still rendered when the script is unavailable', () => {
  const c = loadTransferHelpers();
  const body = c.transferReviewBody({
    candidates: [{ id: 'a', path: 'D:\\save\\a.zip', kind: 'zip', exists: true, review: false, selected: true }],
    recommendedCount: 1,
    reviewCount: 0,
    missingCount: 0,
    scriptAvailable: false,
    scriptReason: 'not_installed',
    scriptPath: 'C:\\missing\\MultiDriveCopy.ps1',
  });
  assert.match(body, /転送候補/);
  assert.match(body, /multi-drive-copy が見つかりません/);
});
