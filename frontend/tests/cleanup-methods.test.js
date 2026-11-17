const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../dist/app.js'), 'utf8');

test('cleanup only offers and accepts folder moves, directing archive disposal to discard', () => {
  const start = source.indexOf('  function moveKindLabel(');
  const end = source.indexOf('  function updateMoveAggregate()', start);
  assert.ok(start >= 0 && end > start);
  const option = {id:'a', name:'A', canFolder:true, canZip:true, canBoth:true, targets:[{kind:'folder',path:'C:\\folder'}, {kind:'zip',path:'C:\\file.zip'}]};
  const state = {moveOptions:[option], moveMethods:new Map([['a','folder']])};
  let dialog;
  const context = {state, esc:String, fmtBytes:String, verdictBadge:()=>'', openModal:d=>dialog=d, closeModal:()=>{}, confirmMoveMethods:()=>{}, updateMoveAggregate:()=>{}, MOVE_METHOD_LABEL:{}};
  vm.createContext(context);
  vm.runInContext(source.slice(start,end), context);
  context.showMoveMethodDialog();
  assert.match(dialog.body, /value="folder"/);
  assert.doesNotMatch(dialog.body, /value="(?:zip|both)"(?![^>]*disabled)/);
  assert.match(dialog.body, /破棄/);
  assert.equal(context.buildMoveRequests().valid, true);
  for (const method of ['zip','both']) {
    state.moveMethods.set('a', method);
    assert.equal(context.buildMoveRequests().valid, false, `${method} must be rejected`);
  }
});
