const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../dist/app.js'), 'utf8');

function extract(start, end, c) {
  vm.createContext(c);
  vm.runInContext(source.slice(source.indexOf(start), source.indexOf(end, source.indexOf(start))), c);
}

// 設定で無効なとき、検索ボタンを押せないようにすること。
// 押してからエラーを出すのではなく、押せない状態にして理由を示す。
test('disabled duplicate search disables the buttons and explains why', () => {
  const searchBtn = { disabled: false, title: '' };
  const clearBtn = { disabled: false, title: '' };
  const box = { classList: { add() {}, remove() {} }, textContent: '' };
  const buttons = [{ dataset: { filter: 'all' }, classList: { toggle() {} } }];
  const c = {
    state: { duplicateInfo: { enabled: false }, busy: false, filter: 'all' },
    el: (id) => (id === 'duplicateSearchBtn' ? searchBtn : id === 'duplicateClearBtn' ? clearBtn : box),
    filterBar: { querySelectorAll: () => buttons },
  };
  extract('  function applyDuplicateAvailability(', '  // E/F/G の同名アーカイブ検索。', c);
  c.applyDuplicateAvailability();

  assert.equal(searchBtn.disabled, true, 'search button must be disabled');
  assert.equal(clearBtn.disabled, true, 'clear button must be disabled');
  assert.match(searchBtn.title, /archiveSearch\.enabled/);
  assert.match(box.textContent, /無効/);
});

// 有効なときはボタンを押せること。
test('enabled duplicate search leaves the buttons usable', () => {
  const searchBtn = { disabled: true, title: '' };
  const clearBtn = { disabled: true, title: '' };
  const box = { classList: { add() {}, remove() {} }, textContent: '' };
  const c = {
    state: { duplicateInfo: { enabled: true }, busy: false, filter: 'all' },
    el: (id) => (id === 'duplicateSearchBtn' ? searchBtn : id === 'duplicateClearBtn' ? clearBtn : box),
    filterBar: { querySelectorAll: () => [] },
  };
  extract('  function applyDuplicateAvailability(', '  // E/F/G の同名アーカイブ検索。', c);
  c.applyDuplicateAvailability();

  assert.equal(searchBtn.disabled, false);
  assert.equal(clearBtn.disabled, false);
  assert.match(searchBtn.title, /同一内容を意味しません/);
});

// 無効に切り替わったとき、重複フィルタの選択を全件へ戻すこと。
test('disabling duplicate search resets a duplicate filter to all', () => {
  const toggled = [];
  const buttons = [
    { dataset: { filter: 'all' }, classList: { toggle: (name, on) => toggled.push(['all', on]) } },
    { dataset: { filter: 'duplicate' }, classList: { toggle: (name, on) => toggled.push(['duplicate', on]) } },
  ];
  const c = {
    state: { duplicateInfo: { enabled: false }, busy: false, filter: 'duplicate' },
    el: () => ({ disabled: false, title: '', classList: { add() {}, remove() {} }, textContent: '' }),
    filterBar: { querySelectorAll: () => buttons },
  };
  extract('  function applyDuplicateAvailability(', '  // E/F/G の同名アーカイブ検索。', c);
  c.applyDuplicateAvailability();

  assert.equal(c.state.filter, 'all', 'filter must fall back to all');
  assert.deepEqual(toggled.find((entry) => entry[0] === 'all'), ['all', true]);
});

// 検索実行時にも無効なら API を呼ばずに案内すること。
test('running a disabled search does not call the backend', async () => {
  const messages = [];
  let called = false;
  const c = {
    state: { busy: false, duplicateInfo: { enabled: false } },
    toast: (m) => messages.push(m),
    api: async () => {
      called = true;
      return {};
    },
  };
  extract('  async function runDuplicateSearch(', '  async function clearDuplicateResults(', c);
  await c.runDuplicateSearch(null);

  assert.equal(called, false, 'backend must not be called when disabled');
  assert.ok(messages.length > 0);
  assert.match(messages[0], /archiveSearch\.enabled/);
});
