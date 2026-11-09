import {test} from 'node:test';import assert from 'node:assert/strict';
import {RailActions} from '../../../workspace/rail/rail-actions.js';
test('pin posts only targeted flags and applies authoritative restoration',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});
 const calls=[], raw={id:'a',name:'Task',status:'in_progress',session_id:'session',backend:'claude',pinned:false};
 const api={json:async(path,options)=>{calls.push([path,options]);return options?.method?{task_id:'a',pinned:true,archived:false}:{...raw};}};
 globalThis.document={getElementById:()=>({textContent:'',hidden:false})};
 const actions=new RailActions(api,async()=>{});await actions.run({id:'a',pinned:false,archived:true},'pin');
 assert.equal(calls.length,1);assert.equal(calls[0][0],'/api/tasks/a/pin');assert.equal(calls[0][1].method,'POST');assert.deepEqual(JSON.parse(calls[0][1].body),{pinned:true});assert.equal(actions.pinned.has('a'),true);
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
test('older daemon selection and menu retain cached pin without extra reads',async()=>{
 let reads=0;const actions=new RailActions({json:async()=>{reads++;return{pinned:true};}},()=>{});
 actions.remember({id:'a'},true);await actions.select({id:'a'});await actions.inspect({id:'a'});assert.equal(reads,0);assert.equal(actions.pinned.has('a'),true);
 await actions.run({id:'a'},'advance');assert.equal(reads,0);actions.clear();
});
test('menu inspection of supplied pin state never redraws an opening popover synchronously',()=>{
 let draws=0,reads=0;const actions=new RailActions({json:async()=>{reads++;}},()=>{},()=>draws++);
 actions.inspect({id:'a',pinned:true});assert.equal(draws,0);assert.equal(reads,0);assert.equal(actions.pinned.has('a'),true);actions.clear();
});

test('pin invalidation rereads real DTO state and fences late disconnect reads',async()=>{
 let pinned=false,release;const task={id:'a',archived:false};
 const actions=new RailActions({json:async()=>({pinned,archived:false})},()=>{});await actions.inspect(task);pinned=true;
 await actions.event({type:'task.updated',task_id:'a',payload:{changed_fields:['pinned']}},[task]);assert.equal(actions.pinned.has('a'),true);
 actions.api.json=()=>new Promise(resolve=>release=resolve);const pending=actions.event({type:'task.updated',task_id:'a',payload:{changed_fields:['pinned']}},[task]);actions.clear();release({pinned:true});await pending;assert.equal(actions.pinned.size,0);
});

test('snapshot pin fields reconcile true and false while absent fields retain the cache',()=>{
 const actions=new RailActions({},()=>{});
 for(const pinned of [true,false,true]){const task={id:'a',pinned};actions.reconcile([task],actions.beginSnapshot());assert.equal(actions.pinned.has('a'),pinned);assert.equal(task.pinned,pinned);}
 actions.reconcile([{id:'a'}],actions.beginSnapshot());assert.equal(actions.pinned.has('a'),true);actions.clear();
});

test('a snapshot older than an SSE observation retains the newer pin on the task and rail',async()=>{
 const task={id:'a',pinned:false},actions=new RailActions({json:async path=>{assert.equal(path,'/api/tasks/a');return{pinned:true};}},()=>{});
 actions.reconcile([task]);const fence=actions.beginSnapshot();
 await actions.event({type:'task.updated',task_id:'a',payload:{changed_fields:['pinned']}},[task]);
 const stale={id:'a',pinned:false};actions.reconcile([stale],fence);assert.equal(stale.pinned,true);assert.equal(actions.pinned.has('a'),true);actions.clear();
});

test('a snapshot started during a POST cannot regress its settled pin',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});let release;
 globalThis.document={getElementById:()=>({textContent:'',hidden:false})};
 const task={id:'a',pinned:false},actions=new RailActions({json:()=>new Promise(resolve=>release=resolve)},async()=>{});
 const pending=actions.run(task,'pin'),fence=actions.beginSnapshot();release({pinned:true,archived:false});await pending;
 const stale={id:'a',pinned:false};actions.reconcile([stale],fence);assert.equal(stale.pinned,true);assert.equal(actions.pinned.has('a'),true);actions.clear();
});

test('a delayed local pin response cannot overwrite a newer reconciled external pin',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});let release;const task={id:'a',pinned:false,archived:false},published=[];
 globalThis.document={getElementById:()=>({textContent:'',hidden:false})};
 const actions=new RailActions({json:async(path,options)=>options.method?new Promise(resolve=>release=resolve):{pinned:false,archived:false}},async()=>{},()=>published.push(actions.pinned.has('a')));
 const post=actions.run(task,'pin');await actions.event({type:'task.updated',task_id:'a',payload:{changed_fields:['pinned']}},[task]);assert.equal(actions.pinned.has('a'),false);
 release({task_id:'a',pinned:true,archived:false});await post;
 assert.equal(actions.pinned.has('a'),false);assert.equal(task.pinned,false);assert.deepEqual(published,[false]);actions.clear();
});

test('overlapping pin events retain the newest publication when reads settle in reverse',async()=>{
 const releases=[],task={id:'a',pinned:false,archived:false},published=[];
 const actions=new RailActions({json:()=>new Promise(resolve=>releases.push(resolve))},()=>{},()=>published.push(actions.pinned.has('a')));
 const event={type:'task.updated',task_id:'a',payload:{changed_fields:['pinned']}};
 const old=actions.event(event,[task]),next=actions.event(event,[task]);releases[1]({pinned:true,archived:false});await next;releases[0]({pinned:false,archived:false});await old;
 assert.equal(actions.pinned.has('a'),true);assert.equal(task.pinned,true);assert.deepEqual(published,[true]);actions.clear();
});

test('snapshots during an unsettled POST skip its pin and reconcile other tasks',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});let release;
 globalThis.document={getElementById:()=>({textContent:'',hidden:false})};
 const task={id:'a',pinned:true},actions=new RailActions({json:()=>new Promise(resolve=>release=resolve)},async()=>{});
 actions.reconcile([task,{id:'b',pinned:false}]);const post=actions.run(task,'pin'),fence=actions.beginSnapshot();
 const next={id:'a',pinned:false};actions.reconcile([next,{id:'b',pinned:true}],fence);
 assert.equal(actions.pinObservations.get('a'),fence.get('a'));assert.equal(next.pinned,true);assert.equal(actions.pinned.has('b'),true);
 release({pinned:false,archived:false});await post;assert.equal(actions.pinned.has('a'),false);actions.clear();
});

test('a changed snapshot fences an older per-event DTO read',async()=>{
 let release;const task={id:'a',pinned:false},actions=new RailActions({json:()=>new Promise(resolve=>release=resolve)},()=>{});
 actions.reconcile([task]);const event=actions.event({type:'task.updated',task_id:'a',payload:{changed_fields:['pinned']}},[task]);
 actions.reconcile([{id:'a',pinned:true}],actions.beginSnapshot());
 release({pinned:false});await event;assert.equal(actions.pinned.has('a'),true);actions.clear();
});

test('ordinary event patches cannot reintroduce old DTO pin values after a targeted read',async()=>{
 const old={id:'a',pinned:false},actions=new RailActions({json:async()=>({pinned:true})},()=>{});actions.reconcile([old]);
 const event=actions.event({type:'task.updated',task_id:'a',payload:{changed_fields:['pinned']}},[old]);
 const replacement={id:'a',pinned:false};await event;actions.retain([replacement]);
 assert.equal(actions.pinned.has('a'),true);assert.equal(replacement.pinned,true);actions.clear();
});

test('unrelated task updates perform no pin reads and absent event DTO pins preserve cache',async()=>{
 let reads=0;const task={id:'a',pinned:true},actions=new RailActions({json:async()=>{reads++;return{id:'a'};}},()=>{});actions.reconcile([task]);
 await actions.event({type:'task.updated',task_id:'a',payload:{changed_fields:['name']}},[task]);assert.equal(reads,0);
 await actions.event({type:'task.updated',task_id:'a',payload:{changed_fields:['pinned']}},[task]);assert.equal(reads,1);assert.equal(actions.pinned.has('a'),true);actions.clear();
});

test('a snapshot confirming cached true fences an older event DTO containing false',async()=>{
 let release,tasks=[{id:'a',pinned:true,archived:false}];
 const actions=new RailActions({json:()=>new Promise(resolve=>release=resolve)},()=>{},()=>actions.retain(tasks));
 actions.reconcile(tasks);
 const pending=actions.event({type:'task.updated',task_id:'a',payload:{changed_fields:['pinned']}},tasks);
 tasks=[{id:'a',pinned:true,archived:false}];actions.reconcile(tasks,actions.beginSnapshot());
 assert.equal(actions.pinned.has('a'),true);
 release({id:'a',pinned:false,archived:false});await pending;
 assert.equal(actions.pinned.has('a'),true);assert.equal(tasks[0].pinned,true);actions.clear();
});

test('a precommit snapshot cannot suppress a successful pin when the followup read fails',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});let release,refreshes=0,tasks=[{id:'a',pinned:false,archived:false}];
 globalThis.document={getElementById:()=>({textContent:'',hidden:false})};
 const api={json:path=>path.endsWith('/pin')?new Promise(resolve=>release=resolve):Promise.reject(new Error('Snapshot unavailable'))};
 const actions=new RailActions(api,async()=>{refreshes++;const fence=actions.beginSnapshot();try{const result=await api.json('/api/tasks?archived=all');tasks=result.tasks;actions.reconcile(tasks,fence);}catch{}});
 actions.reconcile(tasks);const pending=actions.run(tasks[0],'pin');
 tasks=[{id:'a',pinned:false,archived:false}];actions.reconcile(tasks,actions.beginSnapshot());
 release({pinned:true,archived:false});await pending;
 assert.equal(refreshes,1);assert.equal(actions.pinned.has('a'),true);actions.clear();
});

test('a changed followup snapshot wins after POST settlement while duplicate clicks stay blocked',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});let posts=0,refreshes=0;
 globalThis.document={getElementById:()=>({textContent:'',hidden:false})};
 const task={id:'a',pinned:false},actions=new RailActions({json:async()=>{posts++;return{pinned:true,archived:false};}},async()=>{
   refreshes++;assert.equal(actions.pending.has('a'),true);await actions.run(task,'pin');
   actions.reconcile([{id:'a',pinned:false}],actions.beginSnapshot());
 });
 actions.reconcile([task]);await actions.run(task,'pin');
 assert.equal(posts,1);assert.equal(refreshes,1);assert.equal(actions.pinned.has('a'),false);assert.equal(actions.pending.size,0);actions.clear();
});

for(const outcome of ['reject','disconnect'])test(`${outcome} releases the unsettled pin guard for later snapshots`,async t=>{
 t.mock.timers.enable({apis:['setTimeout']});let resolve,reject;
 globalThis.document={getElementById:()=>({textContent:'',hidden:false})};
 const actions=new RailActions({json:()=>new Promise((yes,no)=>{resolve=yes;reject=no;})},async()=>{});
 const pending=actions.run({id:'a',pinned:false},'pin');assert.equal(actions.pinPosts.has('a'),true);
 if(outcome==='reject')reject(new Error('Pin unavailable'));else{actions.clear();resolve({pinned:true,archived:false});}
 await pending;assert.equal(actions.pinPosts.size,0);assert.equal(actions.pending.size,0);
 actions.reconcile([{id:'a',pinned:true}],actions.beginSnapshot());assert.equal(actions.pinned.has('a'),true);actions.clear();
});
