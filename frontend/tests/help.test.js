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

// 仕様ボタンが UI に存在すること。
test('help button is present in the header', () => {
  assert.match(html, /id="helpBtn"/);
  assert.match(html, />仕様</);
});

// 仕様ダイアログが開き、タブが並ぶこと。
test('help dialog opens with tabs', () => {
  const dialogs = [];
  const c = {
    esc: String,
    openModal: (o) => dialogs.push(o),
    closeModal() {},
  };
  extract('  const HELP_SECTIONS = [', '  function showSettings()', c);
  c.showHelp();

  assert.equal(dialogs.length, 1);
  assert.match(dialogs[0].title, /仕様/);
  assert.match(dialogs[0].body, /data-help-tab="verdict"/);
  assert.match(dialogs[0].body, /data-help-tab="flow"/);
  assert.match(dialogs[0].body, /data-help-tab="safety"/);
  assert.match(dialogs[0].body, /data-help-tab="duplicate"/);
  assert.match(dialogs[0].body, /data-help-tab="settings"/);
});

// 判定の意味が仕様に含まれること。
test('help explains every verdict', () => {
  const dialogs = [];
  const c = { esc: String, openModal: (o) => dialogs.push(o), closeModal() {} };
  extract('  const HELP_SECTIONS = [', '  function showSettings()', c);
  c.showHelp("verdict");

  const body = dialogs[0].body;
  for (const verdict of [
    'KEEP_ARCHIVE',
    'KEEP_DIRECTORY',
    'DELETE_CANDIDATE',
    'DELETE_CANDIDATE_IGNORABLE',
    'MISMATCH',
    'AMBIGUOUS',
    'UNVERIFIED',
  ]) {
    assert.match(body, new RegExp(verdict), verdict + ' must be documented');
  }
  assert.match(body, /名前が同じ.*だけでは削除候補になりません/);
});

// 安全条件が仕様に含まれること。
test('help documents the safety rules', () => {
  const dialogs = [];
  const c = { esc: String, openModal: (o) => dialogs.push(o), closeModal() {} };
  extract('  const HELP_SECTIONS = [', '  function showSettings()', c);
  c.showHelp("safety");

  const body = dialogs[0].body;
  assert.match(body, /完全削除はしません/);
  assert.match(body, /移動直前に必ず再照合/);
  assert.match(body, /空き容量の増加ではありません/);
  assert.match(body, /バックアップ/);
});

// 同名検索の仕様で「未確認をなしと読み替えない」ことが明記されること。
test('help warns not to read unknown as none', () => {
  const dialogs = [];
  const c = { esc: String, openModal: (o) => dialogs.push(o), closeModal() {} };
  extract('  const HELP_SECTIONS = [', '  function showSettings()', c);
  c.showHelp("duplicate");

  const body = dialogs[0].body;
  assert.match(body, /未確認.*を.*なし.*と読み替えないでください/);
  assert.match(body, /内容一致は意味しません/);
  assert.match(body, /archiveSearch\.enabled/);
});

// タブを切り替えると本文が変わること。
test('switching tabs changes the body', () => {
  const dialogs = [];
  const c = { esc: String, openModal: (o) => dialogs.push(o), closeModal() {} };
  extract('  const HELP_SECTIONS = [', '  function showSettings()', c);
  c.showHelp("verdict");
  const first = dialogs[0].body;
  c.showHelp("settings");
  const second = dialogs[1].body;

  assert.notEqual(first, second);
  assert.match(second, /config\.json/);
  assert.match(second, /workers/);
});
