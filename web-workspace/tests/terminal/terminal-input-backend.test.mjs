import test from 'node:test';
import assert from 'node:assert/strict';
import { Api } from '../../core/api.js';
import { TerminalInput } from '../../terminal/terminal-input.js';

const settle = () => new Promise(setImmediate);
const task = {id:'wrapper-task',backend:'claude-wrapper',live:true,status:'in_progress'};
// newBackendJSON emits the empty string for custom backend families.
const roster = {backends:[{name:'claude-wrapper',command:'my-claude-wrapper',prompt_flag:'-p',kind:'',label:'claude-wrapper',skill_prefix:'/',session_switcher:true}]};

function fixture(t, backendResponse) {
 const writes=[], original=globalThis.fetch;
 globalThis.fetch=async(path,options)=>{
  if(path==='/api/backends')return new Response(JSON.stringify(await backendResponse()));
  if(path.endsWith('/input')){writes.push({path,body:options.body});return new Response('');}
  return new Response(JSON.stringify(task));
 };
 t.after(()=>{globalThis.fetch=original;});
 const api=new Api(()=>task);
 return {api,writes,create:(selected=task,signal=new AbortController().signal)=>new TerminalInput(api,selected,signal,()=>true,()=>true)};
}

test('custom Claude wrapper uses the exact empty-kind daemon DTO',async t=>{
 const {create,writes}=fixture(t,async()=>roster), input=create();
 await settle();
 await input.raw('\x1b[O');await input.raw('\x1b[I');
 assert.deepEqual(writes,[{path:'/api/tasks/wrapper-task/input',body:'\x1b[I'}]);
});

test('pending backend read never delays raw input, interrupt or composer submission',async t=>{
 let release;
 const pending=new Promise(resolve=>{release=resolve;});
 const {api,create,writes}=fixture(t,()=>pending);
 const read=api.json('/api/backends'),input=create();
 const sent=Promise.all([input.raw('\x1b[O'),input.raw('a'),input.raw('\x03'),input.raw('\x1b[I'),input.send('hello')]);
 try {
  await settle();
  assert.deepEqual(writes.map(write=>write.body),['a','\x03','\x1b[I','\x1b[200~hello\x1b[201~']);
  await sent;
  assert.deepEqual(writes.map(write=>write.body),['a','\x03','\x1b[I','\x1b[200~hello\x1b[201~','\r']);
 } finally {release(roster);await read;await sent;}
});

test('task switch cancels old classification without canceling the shared Api read',async t=>{
 let release,reads=0;
 const pending=new Promise(resolve=>{release=resolve;});
 const {create,writes}=fixture(t,()=>{reads++;return pending;});
 const oldAbort=new AbortController(),old=create({...task,backend:'codex-alias'},oldAbort.signal);
 const next=create({...task,id:'next-task'});
 oldAbort.abort();
 await next.raw('\x1b[O');await next.raw('before');
 release({backends:[...roster.backends,{name:'codex-alias',kind:'codex'}]});
 await settle();
 await assert.rejects(async()=>old.raw('old'),/Terminal is not connected/);
 assert.equal(old.claudeFamily,undefined);
 assert.equal(next.claudeFamily,true);
 await next.raw('\x1b[O');await next.raw('after');
 assert.equal(reads,1);
 assert.deepEqual(writes,[
  {path:'/api/tasks/next-task/input',body:'before'},
  {path:'/api/tasks/next-task/input',body:'after'},
 ]);
});

test('task departure after Api fulfillment cannot publish classification',async t=>{
 const {api,create}=fixture(t,async()=>roster),abort=new AbortController();
 const json=api.json.bind(api);
 api.json=(...args)=>{const result=json(...args);result.then(()=>abort.abort());return result;};
 const input=create(task,abort.signal);
 await settle();
 assert.equal(abort.signal.aborted,true);assert.equal(input.claudeFamily,undefined);
});
