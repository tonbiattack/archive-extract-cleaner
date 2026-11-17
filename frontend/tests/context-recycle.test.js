const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../dist/app.js'), 'utf8');
const html = fs.readFileSync(require('node:path').join(__dirname, '../dist/index.html'), 'utf8');
function extract(start, end, c) { vm.createContext(c); vm.runInContext(source.slice(source.indexOf(start), source.indexOf(end, source.indexOf(start))), c); }
test('context menu offers a selected-items recycle action without replacing selection', () => {
  assert.match(html, /data-menu-action="recycle-selected"/);
  const selected = new Map([['checked', {id:'checked'}]]);
  const buttons = {'[data-menu-action="reveal"]': {}, '[data-menu-action="recycle-selected"]': {}};
  const c = {state:{selected}, contextMenu:{querySelectorAll:()=>[],querySelector:s=>buttons[s],classList:{remove(){}},getBoundingClientRect:()=>({width:10,height:10}),style:{}},window:{innerWidth:100,innerHeight:100}};
  extract('  function showRowContextMenu(', '  function hideContextMenu()', c);
  c.showRowContextMenu({clientX:0,clientY:0}, {id:'other',zipPath:'z'});
  assert.deepEqual([...selected.keys()], ['checked']);
  assert.equal(buttons['[data-menu-action="recycle-selected"]'].disabled, false);
});

test('cleanup rejects missing backend options rather than a partial subset', async () => {
  const messages=[]; let shown=false;
  const c={state:{busy:false,selected:new Map([['a',{deletable:true}],['b',{deletable:true}]])},clearPendingDragSelection(){},updateBusyUI(){},updateSelectionUI(){},setStatus(){},api:async()=>[{id:'a',canFolder:true}],toast:m=>messages.push(m),showMoveMethodDialog(){shown=true}};
  extract('  async function beginDelete()', '  function showMoveMethodDialog()', c);
  await c.beginDelete(); assert.equal(shown,false); assert.ok(messages.length);
});

test('context route explains ineligible cleanup and offers explicit discard', () => {
 const dialogs=[]; const c={state:{busy:false,selected:new Map([['a',{name:'bad',deletable:false}]])},openModal:o=>dialogs.push(o),closeModal(){},cancelDragSelection(){},beginDelete(){},beginDiscard(){},esc:String};
 extract('  function beginContextRecycle()', '  async function beginDelete()',c); c.beginContextRecycle();
 assert.match(dialogs[0].body,/bad/); assert.match(dialogs[0].body,/検証/); assert.equal(dialogs[0].actions[1].disabled,true); assert.equal(dialogs[0].actions[2].disabled,undefined);
});
