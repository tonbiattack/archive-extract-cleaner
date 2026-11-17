const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs'); const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname,'../dist/app.js'),'utf8');
function extract(start,end,context) { vm.createContext(context); vm.runInContext(source.slice(source.indexOf(start),source.indexOf(end,source.indexOf(start))),context); }
test('bulk triage uses a stable snapshot even if selection changes while awaiting', async () => {
  let release; const calls=[]; const selected=new Map([['a',{}],['b',{}]]);
  const c={state:{selected,busy:false,activeId:null}, api:async (_name,id)=>{calls.push(id); if(id==='a') await new Promise(r=>release=r);},updateBusyUI(){},updateSelectionUI(){},cancelDragSelection(){},toast(){},reloadRows(){}};
  extract('  async function setTriage(', '  // ---------- summary', c);
  const pending=c.setTriage('save'); selected.delete('b'); selected.set('c',{}); release(); await pending;
  assert.deepEqual(calls,['a','b']);
});
test('range crossing an unloaded row does not partially select', () => {
 const selected=[]; const messages=[]; const c={state:{lastRowIndex:0,busy:false}, getRow:i=>i===1?null:{id:i}, addSelection:r=>selected.push(r.id), updateSelectionUI(){},renderRows(){},cancelDragSelection(){},toast:m=>messages.push(m)};
 extract('  function rangeSelect(', '  const DRAG_EDGE',c); c.rangeSelect(2);
 assert.deepEqual(selected,[]); assert.ok(messages.length);
});
test('triage persistence error is surfaced to user',()=>{
 const messages=[]; const c={state:{}, renderSummary(){}, toast:m=>messages.push(m)};
 extract('  function applyMeta(', '  function clearRows(',c); c.applyMeta({triageError:'broken store'});
 assert.ok(messages.some(m=>m.includes('broken store')));
});
test('cancelling a pending drag prevents late page loads changing selection', () => {
 const selected=[]; const c={state:{dragSelect:null,pendingDragSelection:{pending:new Set([2]),visited:new Set(),mode:'add'},total:3},getRow:i=>({id:i}),applyDragRow:r=>selected.push(r.id),updateSelectionUI(){},scheduleRender(){}};
 extract('  function applyPendingDragSelection()', '  function dragAutoScrollTick()',c);
 c.clearPendingDragSelection(); c.applyPendingDragSelection(); assert.deepEqual(selected,[]);
});
test('rerender restores focus to the matching checkbox', () => {
 let focusCount=0; const old={dataset:{id:'a'},matches:()=>true}; const replacement={dataset:{id:'a'},focus:()=>focusCount++};
 const rowsEl={contains:x=>x===old,querySelectorAll:()=>[replacement],style:{},innerHTML:''};
 const c={document:{activeElement:old},rowsEl,state:{total:1},virtualEl:{style:{}},emptyState:{classList:{add(){}}},tableScroll:{scrollTop:0,clientHeight:30},sizing:{row:()=>30},OVERSCAN:0,PAGE_SIZE:100,getRow:()=>({id:'a'}),rowHtml:()=>'<input>',ensurePages(){}};
 extract('  function renderRows()', '  function updateEmptyState()',c); c.renderRows(); assert.equal(focusCount,1);
});
