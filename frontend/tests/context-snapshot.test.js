const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const source = fs.readFileSync(require("node:path").join(__dirname, "../dist/app.js"), "utf8");
const triage = require("../dist/triage.js");
function section(start, end) { return source.slice(source.indexOf(start), source.indexOf(end, source.indexOf(start))); }
for (const [action, apiName] of [[1, "GetMoveOptions"], [2, "PreviewDiscard"]]) {
  test('context recycle keeps its displayed selection after asynchronous drift: ' + apiName, async () => {
    const calls = [], dialogs = [];
    const state = {busy:false, selected:new Map([["a", {id:"a", name:"A", deletable:true}]]), dragSelect:null,
      pendingDragSelection:{mode:"add", visited:new Set(), pending:new Set([1])}, total:2};
    const c = {state, window:{Triage:triage}, getRow:()=>({id:"b", name:"B", deletable:false}),
      updateSelectionUI(){}, scheduleRender(){}, updateBusyUI(){}, setStatus(){}, toast(){}, esc:String, fmtBytes:String,
      openModal:o=>dialogs.push(o), closeModal(){}, showMoveMethodDialog(){},
      api:async (name, ids)=>{ calls.push([name, Array.from(ids)]); return name === "GetMoveOptions" ? ids.map(id=>({id,canFolder:true})) : {token:"tok",paths:["C:\one.zip"],itemCount:1,targetCount:1,bytes:1}; }};
    vm.createContext(c);
    vm.runInContext(section("  function addSelection(", "  function rangeSelect(") + section("  function applyDragRow(", "  function dragIndexAtPoint(") + section("  function applyPendingDragSelection(", "  function dragAutoScrollTick(") + section("  function cancelDragSelection(", "  function startDragSelection(") + section("  async function beginDiscard()", "  // ---------- discard end ----------") + section("  function beginContextRecycle()", "  function showMoveMethodDialog()"), c);
    c.beginContextRecycle();
    assert.match(dialogs[0].body, /1 項目/);
    await Promise.resolve(); c.applyPendingDragSelection();
    assert.deepEqual(Array.from(state.selected.keys()), ["a"]);
    state.selected.clear(); state.selected.set("b", {id:"b", deletable:false});
    await dialogs[0].actions[action].onClick();
    assert.deepEqual(calls, [[apiName, ["a"]]]);
  });
}
