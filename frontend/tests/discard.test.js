const test = require('node:test');
const assert = require('node:assert/strict');
const triage = require('../dist/triage.js');

test('discard preview requires a token and only absolute full paths', () => {
  assert.equal(triage.validDiscardPreview({token:'abc', paths:['C:\\data\\a.zip', '/data/b']}), true);
  for (const preview of [{token:'',paths:['C:\\a']}, {token:'x',paths:[]}, {token:'x',paths:['relative']}, {token:'x',paths:['C:\\a', null]}]) {
    assert.equal(triage.validDiscardPreview(preview), false);
  }
});
