const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const dist = path.join(__dirname, '../dist');

test('layout sizing is loaded by the application', () => {
  const html = fs.readFileSync(path.join(dist, 'index.html'), 'utf8');
  assert.match(html, /src="layout\.js"/);
});

test('sizing restores, clamps and saves values while preserving preferred height on resize', () => {
  const { createSizing } = require('../dist/layout.js');
  const data = new Map([['row', '999'], ['upper', '500']]);
  const storage = { getItem: k => data.get(k), setItem: (k, v) => data.set(k, v) };
  const sizing = createSizing(storage);
  assert.equal(sizing.row(), 64);
  assert.equal(sizing.upper(300), 180);
  assert.equal(sizing.upper(900), 500);
  sizing.setRow(1);
  sizing.setUpper(9000);
  assert.equal(sizing.row(), 24);
  assert.equal(sizing.upper(2000), 800);
  assert.equal(data.get('row'), '24');
  assert.equal(data.get('upper'), '800');
  assert.equal(createSizing(storage).upper(2000), 800);
});

test('invalid or unavailable storage falls back to safe defaults', () => {
  const { createSizing } = require('../dist/layout.js');
  const broken = { getItem() { throw Error('denied'); }, setItem() { throw Error('denied'); } };
  const sizing = createSizing(broken);
  assert.equal(sizing.row(), 34);
  assert.equal(sizing.upper(1000), 280);
  assert.doesNotThrow(() => sizing.setRow(48));
  assert.equal(sizing.row(), 48);
  const invalid = createSizing({getItem: () => 'invalid'});
  assert.equal(invalid.row(), 34);
  assert.equal(invalid.upper(1000), 280);
});

test('grid and splitter expose controls and use the active row height', () => {
  const html = fs.readFileSync(path.join(dist, 'index.html'), 'utf8');
  const app = fs.readFileSync(path.join(dist, 'app.js'), 'utf8');
  assert.match(html, /id="upperSplitter"[^>]*role="separator"/);
  assert.match(html, /id="rowHeight"[^>]*type="range"/);
  assert.doesNotMatch(app, /\bROW_H\b/);
  assert.match(app, /sizing\.row\(\)/);
  assert.match(app, /setupLayoutSizing\(\)/);
});
