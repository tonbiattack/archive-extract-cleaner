const test = require('node:test');
const assert = require('node:assert/strict');
const triage = require('../dist/triage.js');

test('discard preview requires a token and only absolute full paths', () => {
  assert.equal(triage.validDiscardPreview({token:'abc', paths:['C:\\data\\a.zip', '/data/b'], fileCount:1, folderCount:1, targets:[{path:'C:\\data\\a.zip',kind:'file',reason:'manual_discard',itemId:'a'},{path:'/data/b',kind:'folder',reason:'manual_discard',itemId:'b'}]}), true);
  for (const preview of [{token:'',paths:['C:\\a']}, {token:'x',paths:[]}, {token:'x',paths:['relative']}, {token:'x',paths:['C:\\a', null]}]) {
    assert.equal(triage.validDiscardPreview(preview), false);
  }
});
