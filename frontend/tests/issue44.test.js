const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs'); const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname,'../dist/app.js'),'utf8');
const triage = require('../dist/triage.js');

test('undo restores mixed previous decisions and retries only failed rows', async () => {
  const calls=[]; let failUndo=true;
  const selected=new Map([['a',{triage:'save'}],['b',{triage:'hold'}]]);
  const c={state:{selected,busy:false,activeId:null}, window:{Triage:triage}, api:async (name,id,value)=>{calls.push([id,value]); if(name==='RestoreTriage' && id==='b' && failUndo) throw new Error('locked'); return {previous:id==='a'?'save':'hold',state:name==='RestoreTriage'?(id==='a'?'save':'hold'):value,undoToken:'token-'+id};}, updateBusyUI(){}, updateSelectionUI(){}, cancelDragSelection(){}, toast(){}, reloadRows(){}};
  vm.createContext(c); vm.runInContext(source.slice(source.indexOf('  async function setTriage('),source.indexOf('  // ---------- summary')),c);
  await c.setTriage('discard'); await c.undoTriage();
  failUndo=false; await c.undoTriage();
  assert.deepEqual(calls,[['a','discard'],['b','discard'],['a','token-a'],['b','token-b'],['b','token-b']]);
});

test('partial triage update can undo only successful rows', async () => {
  const calls=[]; const notices=[];
  const c={state:{selected:new Map([['a',{triage:'hold'}],['b',{triage:'save'}]]),busy:false},window:{Triage:triage},api:async (name,id,value)=>{calls.push([id,value]); if(id==='b' && name==='SetTriageDetailed') throw Error('locked'); return {previous:'hold',state:name==='RestoreTriage'?'hold':value,undoToken:'token-'+id};},updateBusyUI(){},updateSelectionUI(){},cancelDragSelection(){},toast:m=>notices.push(m),reloadRows(){}};
  vm.createContext(c); vm.runInContext(source.slice(source.indexOf('  async function setTriage('),source.indexOf('  // ---------- summary')),c);
  await c.setTriage('discard'); await c.undoTriage();
  assert.deepEqual(calls,[['a','discard'],['b','discard'],['a','token-a']]);
  assert.ok(notices.some(message=>message.includes('ファイルの復元は行いません')));
});

test('discard preview requires consistent typed manual targets', () => {
 const p={token:'t',paths:['/folder'],itemCount:1,targetCount:1,fileCount:0,folderCount:1,bytes:0,targets:[{path:'/folder',kind:'folder',reason:'manual_discard',itemId:'a'}]};
 assert.equal(triage.validDiscardPreview(p),true);
 assert.equal(triage.validDiscardPreview({...p,folderCount:0}),false);
 assert.equal(triage.validDiscardPreview({...p,targets:[{...p.targets[0],reason:'duplicate'}]}),false);
});
