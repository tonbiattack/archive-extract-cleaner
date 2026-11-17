const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../dist/app.js'), 'utf8');
function triageHarness(api) {
  const notices = [];
  const state = {selected:new Map([['a',{triage:'save'}],['b',{triage:'hold'}]]), busy:false};
  const c = {state, api, window:{Triage:require('../dist/triage.js')}, toast:(...args)=>notices.push(args), cancelDragSelection(){}, updateBusyUI(){}, updateSelectionUI(){}, reloadRows(){}};
  vm.createContext(c);
  vm.runInContext(source.slice(source.indexOf('  async function setTriage('), source.indexOf('  // ---------- summary')), c);
  return {c,state,notices};
}
test('undo passes opaque tokens, retains stale failures, and retries only failed rows', async () => {
  const calls=[]; let conflict=true;
  const h=triageHarness(async (name,...args)=>{
    calls.push([name,...args]);
    if(name==='SetTriageDetailed') return {previous:args[0]==='a'?'hold':'save',state:'discard',undoToken:'opaque-'+args[0],warning:args[0]==='a'?'audit failed':''};
    if(name==='RestoreTriage') { if(args[0]==='b' && conflict) throw Error('stale token'); return {state:args[0]==='a'?'hold':'save',warning:'restore audit failed'}; }
    throw Error('unexpected method '+name);
  });
  await h.c.setTriage('discard');
  assert.equal(h.state.selected.get('a').triage,'discard');
  assert.ok(h.notices.some(([m])=>m.includes('audit failed')));
  await h.c.undoTriage();
  assert.deepEqual(calls.slice(2),[['RestoreTriage','a','opaque-a'],['RestoreTriage','b','opaque-b']]);
  assert.equal(h.state.selected.get('a').triage,'hold');
  assert.equal(h.state.selected.get('b').triage,'discard');
  assert.equal(h.state.triageUndo.length,1);
  assert.ok(h.notices.some(([m])=>m.includes('restore audit failed')));
  conflict=false; await h.c.undoTriage();
  assert.deepEqual(calls.at(-1),['RestoreTriage','b','opaque-b']);
  assert.equal(h.state.triageUndo.length,0);
});

test('missing undo token never permits an unguarded restore and is reported', async () => {
  for (const token of [undefined, null, '', 42]) {
    const calls=[];
    const h=triageHarness(async (name,...args)=>{ calls.push([name,...args]); return {previous:'save',state:'discard',undoToken:args[0]==='b'?'valid-b':token}; });
    await h.c.setTriage('discard');
    assert.equal(h.state.selected.get('a').triage,'discard');
    assert.ok(h.notices.some(([message, isError])=>isError && message.includes('元に戻')));
    await h.c.undoTriage();
    assert.deepEqual(calls.slice(2),[['RestoreTriage','b','valid-b']]);
  }
});

test('legacy undo entries without tokens never invoke restore', async () => {
  const calls=[];
  const h=triageHarness(async (...args)=>{ calls.push(args); });
  h.state.triageUndo=[{id:'a',expected:'discard',previous:'save'}];
  await h.c.undoTriage();
  assert.deepEqual(calls,[]);
  assert.equal(h.state.triageUndo.length,1);
});

test('recycle shows each result and escaped audit warning rather than failure', async () => {
  const dialogs=[], calls=[];
  const state={busy:false,selected:new Map([['a',{}]])};
  const c={state,api:async (...args)=>{calls.push(args); return {results:[{path:'/a',success:true},{path:'/b',success:false,reason:'locked'}],warning:'audit <failed>'};},openModal:o=>dialogs.push(o),closeModal(){},updateBusyUI(){},updateSelectionUI(){},reloadRows(){},esc:s=>String(s).replaceAll('&','&amp;').replaceAll('<','&lt;')};
  vm.createContext(c); vm.runInContext(source.slice(source.indexOf('  async function executeDiscard('),source.indexOf('  // ---------- discard end ----------')),c);
  await c.executeDiscard('tok');
  assert.deepEqual(calls,[['RecycleDiscardDetailed','tok',true]]);
  assert.match(dialogs.at(-1).body,/\/a/); assert.match(dialogs.at(-1).body,/locked/); assert.match(dialogs.at(-1).body,/audit &lt;failed>/);
  assert.equal(dialogs.at(-1).title,'ごみ箱移動の結果');
});
