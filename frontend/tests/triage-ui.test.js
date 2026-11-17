const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const dist = path.join(__dirname, '../dist');
const html = fs.readFileSync(path.join(dist, 'index.html'), 'utf8');
const app = fs.readFileSync(path.join(dist, 'app.js'), 'utf8');

test('triage actions, filter and shortcuts are wired independently of recycle', () => {
  assert.match(html, /id="triageFilter"/);
  for (const state of ['save', 'discard', 'hold', 'unjudged']) assert.match(html, new RegExp(`data-triage="${state}"`));
  assert.match(app, /api\("SetTriageDetailed", id, value\)/);
  assert.match(app, /triage: state\.triageFilter/);
  assert.match(app, /window\.Triage\.shortcut\(event\)/);
  assert.doesNotMatch(app, /if \(!row \|\| !row\.deletable\) return/);
});

test('recycling requires complete backend preview and transfer fails closed without state', () => {
  assert.match(app, /Triage\.validPreview\(preview, requests\)/);
  assert.match(app, /Triage\.savedCandidates\(review\.candidates\)/);
  assert.match(app, /hasOwnProperty\.call\(candidate, "triage"\)/);
});
