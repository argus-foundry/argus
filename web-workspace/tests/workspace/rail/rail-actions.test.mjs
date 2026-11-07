import {test} from 'node:test';import assert from 'node:assert/strict';
import {RailActions} from '../../../workspace/rail/rail-actions.js';
test('pin preserves raw fields, respects captured id, and marks known state only on success',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});
 const calls=[], raw={id:'a',name:'Task',status:'in_progress',session_id:'session',backend:'claude',pinned:false};
 const api={json:async(path,options)=>{calls.push([path,options]);return options?.method?{}:{...raw};}};
 globalThis.document={getElementById:()=>({textContent:'',hidden:false})};
 const actions=new RailActions(api,async()=>{});await actions.run({id:'a'},'pin');
 assert.equal(calls[1][0],'/api/tasks/a/raw');assert.deepEqual(JSON.parse(calls[1][1].body),{...raw,pinned:true});assert.equal(actions.pinned.has('a'),true);
 actions.clear();
});
test('archived and disappeared task snapshots clear known pins',()=>{
 const actions=new RailActions({},()=>{});actions.pinned=new Set(['archived','gone','kept']);actions.reconcile([{id:'archived',archived:true},{id:'kept'}]);assert.deepEqual([...actions.pinned],['kept']);actions.clear();
});
test('supplied boolean pin state avoids a raw read and snapshot reconciliation updates it',async()=>{
 let reads=0;const actions=new RailActions({json:async()=>{reads++;return{pinned:true};}},()=>{});
 await actions.select({id:'a',pinned:true});assert.equal(reads,0);assert.equal(actions.pinned.has('a'),true);
 actions.reconcile([{id:'a',pinned:false}]);assert.equal(actions.pinned.has('a'),false);assert.equal(actions.knownPins.has('a'),true);
 actions.clear();
});
test('menu inspection resolves unknown pin state once and unsupported removed actions send no request',async()=>{
 let reads=0;const actions=new RailActions({json:async()=>{reads++;return{pinned:true};}},()=>{});
 await actions.inspect({id:'a'});await actions.inspect({id:'a'});assert.equal(reads,1);assert.equal(actions.pinned.has('a'),true);
 await actions.run({id:'a'},'advance');assert.equal(reads,1);actions.clear();
});
test('menu inspection of supplied pin state never redraws an opening popover synchronously',()=>{
 let draws=0,reads=0;const actions=new RailActions({json:async()=>{reads++;}},()=>{},()=>draws++);
 actions.inspect({id:'a',pinned:true});assert.equal(draws,0);assert.equal(reads,0);assert.equal(actions.pinned.has('a'),true);actions.clear();
});
