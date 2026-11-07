import test from 'node:test';
import assert from 'node:assert/strict';
import { TerminalAutoFit } from '../../terminal/terminal-autofit.js';
import { terminalCellSpace, terminalBusy } from '../../terminal/terminal-policy.js';
function clock() {
  let now = 0, id = 0; const queue = new Map();
  return { setTimeout(fn, delay) { const key = ++id; queue.set(key, { at: now + delay, fn }); return key; }, clearTimeout(key) { queue.delete(key); },
    async tick(ms) { now += ms; for (const [key, item] of [...queue]) if (item.at <= now) { queue.delete(key); item.fn(); } for (let i=0;i<10;i++)await Promise.resolve(); } };
}
function fixture() {
  const timers = clock(), state = { current: { cols:80, rows:24 }, desired: { cols:100, rows:30 }, visible:true, busy:false }, calls=[];
  let execute = async size => { calls.push(size); state.current = size; return true; };
  const actor = new TerminalAutoFit({ timers, measure:()=>state.desired, current:()=>state.current, allowed:manual=>state.visible && (manual || !state.busy), resize:(size, context)=>execute(size,context), changed() {} });
  return { actor, state, calls, timers, setExecute: value => { execute=value; } };
}
test('cell geometry reserves classic or overlay scrollbar space and a right gutter', () => {
  assert.deepEqual(terminalCellSpace({ width:1000,height:600,cellWidth:8,cellHeight:16,scrollbar:15 }), { cols:122,rows:37 });
  assert.deepEqual(terminalCellSpace({ width:1000,height:600,cellWidth:8,cellHeight:16,scrollbar:0 }), { cols:122,rows:37 });
  assert.equal(terminalCellSpace({ width:10,height:600,cellWidth:8,cellHeight:16 }), null);
  assert.equal(terminalCellSpace({ width:1000,height:600,cellWidth:0,cellHeight:16 }), null);
});
test('existing REST runtime flags distinguish busy, waiting and unknown legacy state', () => {
  assert.equal(terminalBusy({live:true}),true); assert.equal(terminalBusy({live:true,idle:false}),true);
  assert.equal(terminalBusy({live:true,idle:true}),false); assert.equal(terminalBusy({live:true,needs_input:true}),false); assert.equal(terminalBusy({}),false);
});
test('settled changes debounce once and identical cells never resize', async () => {
  const {actor,state,calls,timers}=fixture(); actor.settle(); await timers.tick(300); state.desired={cols:120,rows:32};actor.settle();await timers.tick(399);assert.equal(calls.length,0);await timers.tick(1);assert.deepEqual(calls,[{cols:120,rows:32}]);actor.settle();await timers.tick(1000);assert.equal(calls.length,1);actor.destroy();
});
test('hidden/busy work is canceled, idle/visible checks resume, and manual fitting can override busy', async () => {
  const {actor,state,calls,timers}=fixture();actor.settle();state.visible=false;actor.check();await timers.tick(500);assert.equal(calls.length,0);
  state.visible=true;state.busy=true;actor.check();await timers.tick(500);assert.equal(calls.length,0);await actor.manual();assert.equal(calls.length,1);
  state.desired={cols:130,rows:40};state.busy=false;actor.check();await timers.tick(400);assert.equal(calls.length,2);actor.destroy();
});
test('in-flight requests serialize and superseded geometry waits for its own settle time', async () => {
  const {actor,state,calls,timers,setExecute}=fixture();let release;setExecute(async(size,context)=>{calls.push(size);await new Promise(resolve=>{release=resolve});if(context.current())state.current=size;return true;});
  actor.settle();await timers.tick(400);state.desired={cols:110,rows:31};actor.settle();await timers.tick(400);state.desired={cols:140,rows:35};actor.settle();release();await timers.tick(399);assert.equal(calls.length,1);await timers.tick(1);assert.equal(calls.length,2);assert.deepEqual(calls[1],state.desired);release();await timers.tick(0);actor.destroy();
});
test('failed automatic fitting exposes fallback without looping on identical state', async () => {
  const {actor,timers,calls,setExecute}=fixture();setExecute(async()=>{calls.push('failed');throw new Error('fixture')});actor.settle();await timers.tick(400);assert.equal(actor.skipped,true);actor.check();await timers.tick(1000);assert.equal(calls.length,1);actor.destroy();
});
test('destroy cancels timers and invalidates preflight work', async () => {
  const {actor,timers,calls}=fixture();actor.settle();actor.destroy();await timers.tick(1000);assert.deepEqual(calls,[]);
});

test('geometry changing during task/size preflight cannot post the obsolete automatic target',async()=>{
  const {resizeTerminalSession}=await import('../../terminal/terminal-resize.js'),writes=[];
  const terminal={generation:0,abort:new AbortController(),task:{id:'a'},canFit:()=>true,path:suffix=>'/api/tasks/a'+(suffix||''),
    api:{json:async(path,options)=>{if(options?.method==='POST')writes.push(options.body);return path.endsWith('/size')?{cols:80,rows:24}:{id:'a',status:'in_progress',live:true,idle:true};}},
    term:{resize(){}},stream:{},geometry:{propose:()=>({cols:140,rows:40}),schedule(){}}};
  assert.equal(await resizeTerminalSession(terminal,{cols:100,rows:30},{automatic:true,current:()=>true}),false);assert.deepEqual(writes,[]);
});

test('manual fallback checks authoritative state even when the cached view already fits',async()=>{
  const {actor,state,calls}=fixture();state.current=state.desired;await actor.manual();assert.equal(calls.length,1,'Manual fitting delegates to the authoritative-size operation');actor.destroy();
});
test('manual fallback reports unavailable cell measurements without sending a resize',async()=>{
  const {actor,state,calls}=fixture();state.desired=null;await assert.rejects(actor.manual(),/pane|measurement/i);assert.deepEqual(calls,[]);actor.destroy();
});
