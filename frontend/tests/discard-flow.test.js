const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../dist/app.js'), 'utf8');
const triage = require('../dist/triage.js');
function harness(api) {
  const start = source.indexOf('  async function beginDiscard()');
  const end = source.indexOf('  // ---------- discard end ----------', start);
  assert.ok(start >= 0 && end > start, 'discard flow exists');
  const dialogs = [], notifications = [], reloads = [];
  const state = {busy:false, deleting:false, selected:new Map([['a', {}], ['b', {}]])};
  const context = {state, api, window:{Triage:triage}, openModal:o=>dialogs.push(o), closeModal:()=>{}, toast:(...args)=>notifications.push(args), updateBusyUI:()=>{}, updateSelectionUI:()=>{}, cancelDragSelection:()=>{}, reloadRows:()=>reloads.push(true), esc:s=>String(s).replaceAll('&','&amp;').replaceAll('<','&lt;'), fmtBytes:n=>n+' bytes', setStatus:()=>{}};
  vm.createContext(context);
  vm.runInContext(source.slice(start,end),context);
  return {context,state,dialogs,notifications,reloads};
}
test('discard previews exact paths and confirms with token; reports partial results and refreshes', async () => {
  const calls=[];
  const h=harness(async (name,...args)=>{ calls.push([name,...args]); return name==='PreviewDiscard' ? {token:'tok',paths:['C:\\one.zip','C:\\two'],itemCount:2,targetCount:2,bytes:123} : [{path:'C:\\one.zip',success:true},{path:'C:\\two',success:false,reason:'locked'}]; });
  await h.context.beginDiscard();
  assert.deepEqual(Array.from(calls[0][1]), ['a','b']);
  assert.match(h.dialogs[0].body, /C:\\one\.zip/);
  assert.match(h.dialogs[0].body, /C:\\two/);
  assert.match(h.dialogs[0].body, /123 bytes/);
  await h.dialogs[0].actions[1].onClick();
  assert.equal(calls.length,1);
  assert.match(h.dialogs[1].body, /アーカイブ/);
  await h.dialogs[1].actions[1].onClick();
  assert.deepEqual(calls[1], ['RecycleDiscard','tok',true]);
  assert.match(h.dialogs.at(-1).body, /locked/);
  assert.match(h.dialogs.at(-1).body, /C:\\one\.zip/);
  assert.equal(h.reloads.length,1);
});
test('successful recycle displays escaped backend warning while retaining success', async () => {
  const h = harness(async () => [{path:'C:\\one.zip', success:true, reason:'warning <unsafe>'}]);
  await h.context.executeDiscard('tok');
  assert.match(h.dialogs.at(-1).body, /成功/);
  assert.match(h.dialogs.at(-1).body, /警告/);
  assert.match(h.dialogs.at(-1).body, /warning &lt;unsafe>/);
  assert.equal(h.reloads.length, 1);
});
test('discard rejects malformed preview and never recycles', async () => {
  const calls=[]; const h=harness(async name=>{calls.push(name); return {token:'',paths:['relative']};});
  await h.context.beginDiscard();
  assert.deepEqual(calls,['PreviewDiscard']); assert.equal(h.dialogs.length,0); assert.ok(h.notifications.length);
});
