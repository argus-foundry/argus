import test from 'node:test';
import assert from 'node:assert/strict';
import {Api} from '../../core/api.js';
import {TerminalInput} from '../../terminal/terminal-input.js';
const settle=()=>new Promise(setImmediate);
const task={id:'focus',backend:'alias',status:'in_progress',live:true};
const roster=kind=>({backends:[{name:'alias',kind}]});
function fixture(t){
 t.mock.timers.enable({apis:['setTimeout']});
 let resolve;
 const pending=new Promise(done=>{resolve=done;}),writes=[],abort=new AbortController();
 const api={json:async path=>path==='/api/backends'?pending:task,inputTask:()=>task,request:async(path,options)=>writes.push(options.body)};
 const input=new TerminalInput(api,task,abort.signal,()=>true);
 t.after(()=>abort.abort());
 return {input,writes,abort,resolve};
}
for(const [kind,expected] of [['claude',[]],['',[]],['codex',['\x1b[O']],[undefined,['\x1b[O']]])test(`held focus-out settles for kind ${JSON.stringify(kind)}`,async t=>{
 const {input,writes,resolve}=fixture(t);
 await input.raw('\x1b[O');await input.raw('\x1b[O');
 assert.deepEqual(writes,[]);
 resolve(roster(kind));await settle();await input.chain;
 assert.deepEqual(writes,expected);
 t.mock.timers.tick(2000);await input.chain;assert.deepEqual(writes,expected);
});
for(const data of ['\x1b[I','typed','\x03'])test(`intervening ${JSON.stringify(data)} makes held focus-out stale`,async t=>{
 const {input,writes,resolve}=fixture(t);
 await input.raw('\x1b[O');await input.raw(data);
 resolve(roster('codex'));await settle();await input.chain;
 assert.deepEqual(writes,[data]);
});
test('composer bytes invalidate held blur without waiting for classification',async t=>{
 const {input,writes,resolve}=fixture(t);
 await input.raw('\x1b[O');const sending=input.send('hello');await settle();
 assert.deepEqual(writes,['\x1b[200~hello\x1b[201~']);
 t.mock.timers.tick(50);await sending;
 resolve(roster('codex'));await settle();await input.chain;
 assert.deepEqual(writes,['\x1b[200~hello\x1b[201~','\r']);
});
test('a fresh blur replaces a stale held report',async t=>{
 const {input,writes,resolve}=fixture(t);
 await Promise.all([input.raw('\x1b[O'),input.raw('\x1b[I'),input.raw('\x1b[O')]);
 resolve(roster('codex'));await settle();await input.chain;
 assert.deepEqual(writes,['\x1b[I','\x1b[O']);
});
for(const stale of [false,true])test(`held blur times out once after two seconds, stale=${stale}`,async t=>{
 const {input,writes,resolve}=fixture(t);
 await input.raw('\x1b[O');if(stale)await input.raw('after');
 t.mock.timers.tick(1999);await input.chain;assert.deepEqual(writes,stale?['after']:[]);
 if(!stale)await input.raw('\x1b[O');
 t.mock.timers.tick(1);await input.chain;assert.deepEqual(writes,stale?['after']:['\x1b[O']);
 resolve(roster('codex'));await settle();await input.chain;
 assert.deepEqual(writes,stale?['after']:['\x1b[O']);
});
test('teardown drops held blur and fences late classification',async t=>{
 const {input,writes,resolve,abort}=fixture(t);
 await input.raw('\x1b[O');abort.abort();t.mock.timers.tick(2000);
 resolve(roster('claude'));await settle();await input.chain;
 assert.deepEqual(writes,[]);assert.equal(input.claudeFamily,undefined);
});
for(const status of [401,500])test(`failed prefetch adopts a later shared Api cache result on bounded retry (${status})`,async t=>{
 t.mock.timers.enable({apis:['setTimeout']});
 const original=globalThis.fetch,abort=new AbortController();let reads=0;
 globalThis.fetch=async()=>++reads===1?new Response('',{status}):new Response(JSON.stringify(roster('claude')));
 t.after(()=>{abort.abort();globalThis.fetch=original;});
 const api=new Api(()=>task),input=new TerminalInput(api,task,abort.signal,()=>true);
 await settle();assert.equal(input.claudeFamily,undefined);assert.equal(reads,1);
 await input.raw('\x1b[O');
 await api.json('/api/backends');
 t.mock.timers.tick(249);await settle();assert.equal(input.claudeFamily,undefined);
 t.mock.timers.tick(1);await settle();assert.equal(input.claudeFamily,true);assert.equal(reads,2);
});
test('failed prefetch retries three total attempts with backoff and stops on teardown',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});
 let reads=0;const abort=new AbortController();
 const api={json:async()=>{reads++;throw new Error('offline');}};
 const input=new TerminalInput(api,task,abort.signal,()=>true);await settle();
 t.mock.timers.tick(250);await settle();assert.equal(reads,2);
 t.mock.timers.tick(1000);await settle();assert.equal(reads,3);
 t.mock.timers.tick(10000);await settle();assert.equal(reads,3);
 abort.abort();
 const nextAbort=new AbortController();new TerminalInput(api,task,nextAbort.signal,()=>true);await settle();
 nextAbort.abort();t.mock.timers.tick(10000);await settle();assert.equal(reads,4);
});
