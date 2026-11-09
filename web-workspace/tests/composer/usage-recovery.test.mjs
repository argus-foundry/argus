import {test} from 'node:test';import assert from 'node:assert/strict';
import {ComposerContext} from '../../composer/composer-context.js';import {SessionUsage} from '../../composer/session-usage.js';
const page=session=>({task_id:'a',session_id:session,totals:{input_tokens:42},turns:[],next_cursor:null});
test('composed usage defers hidden cadence and events, then reconciles once visible',async t=>{
 const original=globalThis.document;globalThis.document={hidden:true};t.after(()=>globalThis.document=original);
 const calls=[],context=Object.assign(Object.create(ComposerContext.prototype),{task:{id:'a'},render(){},api:{json:async path=>{calls.push(path);return page('s');}}});
 context.sessionUsage=new SessionUsage(context.api,()=>{});context.sessionUsage.select(context.task);
 await context.refresh();context.event({type:'task.usage_changed',task_id:'a',payload:{session_id:'s'}});await context.sessionUsage.flight;
 assert.deepEqual(calls,[]);document.hidden=false;await context.sessionUsage.refresh();assert.equal(calls.length,1);
});
test('composer forwards usage events and retries a mismatched session baseline',async()=>{
 const context=Object.assign(Object.create(ComposerContext.prototype),{task:{id:'a'},render(){}}),usage=context.sessionUsage=new SessionUsage({json:async()=>page('current')},()=>{});
 usage.select(context.task);context.event({type:'task.usage_changed',task_id:'a',payload:{session_id:'replayed'}});await usage.flight;
 assert.match(usage.error,/session/i);assert.equal(usage.data,null);await usage.refresh();assert.equal(usage.data.session_id,'current');
});

test('unsupported usage is quiet and bounded while historical 404 stays actionable',async()=>{
 let reads=0;const usage=new SessionUsage({request:async()=>{reads++;return new Response('<html>old daemon</html>',{headers:{'content-type':'text/html'}});}},()=>{});
 usage.select({id:'a'});await usage.refresh();await usage.refresh();assert.equal(reads,1);assert.equal(usage.error,null);assert.equal(usage.data,null);
 usage.unsupportedUntil=0;usage.data={...page('s'),next_cursor:'cursor'};usage.api.request=async()=>new Response('',{status:404});await usage.older();assert.match(usage.error,/unavailable/i);
});
