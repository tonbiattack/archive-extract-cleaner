const test = require('node:test');
const assert = require('node:assert/strict');
const triage = require('../dist/triage.js');

test('triage status and keyboard shortcuts are safe and explicit', () => {
  assert.equal(triage.status({triage: 'save'}), 'save');
  assert.equal(triage.status({}), 'unjudged');
  assert.equal(triage.shortcut({key:'S', target:{tagName:'DIV'}}), 'save');
  assert.equal(triage.shortcut({key:'d', target:{tagName:'INPUT'}}), null);
  assert.equal(triage.shortcut({key:'h', target:{isContentEditable:true}}), null);
  assert.equal(triage.shortcut({key:'s', ctrlKey:true, target:{tagName:'DIV'}}), null);
});

test('triage shortcuts work on focused checkboxes but not text editing controls', () => {
  for (const [key, value] of [['s', 'save'], ['d', 'discard'], ['h', 'hold']]) {
    assert.equal(triage.shortcut({key, target:{tagName:'INPUT', type:'checkbox'}}), value);
    for (const target of [{tagName:'INPUT', type:'text'}, {tagName:'INPUT', type:'search'}, {tagName:'TEXTAREA'}, {tagName:'SELECT'}, {tagName:'SPAN', isContentEditable:true}]) {
      assert.equal(triage.shortcut({key, target}), null);
    }
  }
});

test('saved-only transfer excludes unknown and non-save statuses', () => {
  const candidates = [{triage:'save',path:'a'}, {triage:'discard',path:'b'}, {path:'c'}, {triage:'hold',path:'d'}];
  assert.deepEqual(triage.savedCandidates(candidates), [candidates[0]]);
});

test('recycle preview rejects missing or incomplete exact targets', () => {
  const requests = [{id:'a',method:'both'}];
  assert.equal(triage.validPreview({items:[{id:'a',method:'both',targets:[{kind:'zip',path:'C:\\a.zip'}]}]}, requests), false);
  assert.equal(triage.validPreview({items:[{id:'a',method:'both',targets:[{kind:'zip',path:'C:\\a.zip'}, {kind:'folder',path:'C:\\a'}]}]}, requests), true);
});
