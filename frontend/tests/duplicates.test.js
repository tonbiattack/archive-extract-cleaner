const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../dist/app.js'), 'utf8');
const html = fs.readFileSync(require('node:path').join(__dirname, '../dist/index.html'), 'utf8');

function extract(start, end, c) {
  vm.createContext(c);
  vm.runInContext(source.slice(source.indexOf(start), source.indexOf(end, source.indexOf(start))), c);
}

// 同名検索のフィルタと列が UI に存在すること。
test('duplicate filters and column are present in the UI', () => {
  assert.match(html, /data-filter="duplicate"/);
  assert.match(html, /data-filter="duplicate-same-size"/);
  assert.match(html, /data-sort="duplicate"/);
  assert.match(html, /id="duplicateSearchBtn"/);
  assert.match(html, /id="duplicateClearBtn"/);
  assert.match(html, /id="duplicateStatus"/);
});

// 同名は同一内容を意味しない旨が UI に明記されていること。
test('duplicate UI warns that same name is not same content', () => {
  assert.match(html, /同名は同一内容を意味しません/);
});

// 未検索は「—」で、勝手に「なし」と表示しないこと。
test('unsearched duplicate cell shows a dash, not none', () => {
  const c = { esc: String };
  extract('  function duplicateCell(', '  function rowHtml(', c);
  const cell = c.duplicateCell({ id: 'a' });
  assert.match(cell, /—/);
  assert.doesNotMatch(cell, /なし/);
});

// 未確認は「なし」ではなく「未確認」と表示すること。
test('unknown duplicate status is shown as unknown, not none', () => {
  const c = { esc: String };
  extract('  function duplicateCell(', '  function rowHtml(', c);
  const cell = c.duplicateCell({
    id: 'a',
    duplicate: { status: 'unknown', hits: [], drives: [{ drive: 'E', available: false, reason: '未接続' }] },
  });
  assert.match(cell, /未確認/);
  assert.doesNotMatch(cell, />なし</);
});

// 同名・同サイズでも「一致」とは表示しないこと。
test('same size is labelled as same name and same size, not a match', () => {
  const c = { esc: String };
  extract('  function duplicateCell(', '  function rowHtml(', c);
  const cell = c.duplicateCell({
    id: 'a',
    duplicate: { status: 'same_size', hits: [{ drive: 'E', path: 'E:\\x\\a.zip', sameSize: true }], drives: [] },
  });
  assert.match(cell, /同名・同サイズ/);
  assert.doesNotMatch(cell, /一致/);
});

// サイズ違いの表示。
test('different size is labelled distinctly', () => {
  const c = { esc: String };
  extract('  function duplicateCell(', '  function rowHtml(', c);
  const cell = c.duplicateCell({
    id: 'a',
    duplicate: { status: 'different_size', hits: [{ drive: 'E', path: 'E:\\x\\a.zip', sameSize: false }], drives: [] },
  });
  assert.match(cell, /同名・サイズ違い/);
});

// ツールチップに保存先パスと注意書きが入ること。
test('duplicate tooltip lists the destination path and the caveat', () => {
  const c = { esc: String };
  extract('  function duplicateCell(', '  function rowHtml(', c);
  const title = c.duplicateTitle({
    status: 'same_size',
    hits: [{ drive: 'E', path: 'E:\\save\\a.zip', sameSize: true }],
    drives: [{ drive: 'F', available: false, reason: '未接続' }],
  });
  assert.match(title, /E:\\save\\a\.zip/);
  assert.match(title, /F: 未接続/);
  assert.match(title, /削除の根拠にしないでください/);
});

// ドライブ状況の表示で、未確認の理由が出ること。
test('duplicate status shows why a drive is unknown', () => {
  const box = { classList: { add() {}, remove() {} }, textContent: '' };
  const c = {
    el: () => box,
    esc: String,
  };
  extract('  function renderDuplicateStatus(', '  function rowHtml(', c);
  c.renderDuplicateStatus({
    enabled: true,
    driveStates: [
      { drive: 'E', available: true, indexed: 120 },
      { drive: 'F', available: false, reason: '未接続の可能性' },
    ],
  });
  assert.match(box.textContent, /E: 120 件を確認/);
  assert.match(box.textContent, /F: 未確認/);
  assert.match(box.textContent, /未接続の可能性/);
  assert.match(box.textContent, /同一内容を意味しません/);
});
