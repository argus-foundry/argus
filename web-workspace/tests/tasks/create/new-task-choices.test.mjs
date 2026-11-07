import {test} from 'node:test';
import assert from 'node:assert/strict';
import {creationBackend, creationOverrides} from '../../../tasks/create/new-task-choices.js';
const backends=[{name:'assistant',model:'sonnet',models:['opus','sonnet']},{name:'custom',model:'provider-default',models:[]}];
test('model metadata requires an explicit known backend',()=>{
  assert.equal(creationBackend(backends,'assistant'),backends[0]);
  assert.equal(creationBackend(backends,'','assistant'),null);
  assert.equal(creationBackend(backends,'missing'),null);
});
test('explicit equal-to-default model is sent',()=>{
  assert.deepEqual(creationOverrides(' sonnet ','',backends[0]),{model:'sonnet'});
});
test('curated backend rejects unlisted model',()=>{
  assert.throws(()=>creationOverrides('future-custom','',backends[0]),/listed model/);
});
test('listless backend allows trimmed free text',()=>{
  assert.deepEqual(creationOverrides(' future-custom ','',backends[1]),{model:'future-custom'});
});
test('inherited backend requires default model',()=>{
  assert.throws(()=>creationOverrides('sonnet','',null),/Choose a backend/);
});
for(const [value,expected] of [['',{}],['enabled',{sandbox_override:'enabled'}],['disabled',{sandbox_override:'disabled'}],['invalid',{}]])test(`creation sandbox override ${JSON.stringify(value)}`,()=>{
  assert.deepEqual(creationOverrides(' ',value,null),expected);
});
