import {test} from 'node:test';
import assert from 'node:assert/strict';
import {scopeState,subtreeScope} from '../../../workspace/rail/task-selection-policy.js';
test('mixed state is derived from current matches and never selects arrivals',()=>{
 const ids=new Set(['a','b']);assert.deepEqual(scopeState([{id:'a'},{id:'b'}],ids),{checked:true,mixed:false});assert.deepEqual(scopeState([{id:'a'},{id:'b'},{id:'new'}],ids),{checked:false,mixed:true});assert.deepEqual(scopeState([],ids),{checked:false,mixed:false});
});
test('coordinator cascades traverse hierarchy through nonmatching parents and stop at search matches',()=>{
 const parents=new Map([['child','root'],['grandchild','child'],['other','else']]);
 assert.deepEqual(subtreeScope('root',[{id:'grandchild'},{id:'root'},{id:'other'}],parents).map(task=>task.id),['grandchild','root']);
});
