import {test} from 'node:test';
import assert from 'node:assert/strict';
import {SessionUsage,usageCounters} from '../../composer/session-usage.js';
const page=(session='s',turn='t')=>({task_id:'a',session_id:session,totals:{input_tokens:100,cached_input_tokens:60,output_tokens:null},turns:[{turn_id:turn,input_tokens:100}],next_cursor:'cursor'});
test('nullable counters preserve measured zero and cached subset',()=>{
 assert.deepEqual(usageCounters({input_tokens:0,cached_input_tokens:0,output_tokens:null}),[['Input','0'],['Cached input (included in input)','0'],['Output','Unavailable']]);
});
test('older pages pin the session, deduplicate turns and reject foreign sessions',async()=>{
 const paths=[],responses=[page(),page('s','older'),page('other','foreign')];
 const usage=new SessionUsage({json:async path=>{paths.push(path);return responses.shift();}},()=>{});
 usage.select({id:'a'});await usage.refresh();await usage.older();
 assert.match(paths[1],/session_id=s/);assert.match(paths[1],/before=cursor/);assert.deepEqual(usage.data.turns.map(t=>t.turn_id),['t','older']);
 await usage.older();assert.deepEqual(usage.data.turns.map(t=>t.turn_id),['t','older']);assert.match(usage.error,/session/i);
});
test('task change and session event fence ignored abort responses',async()=>{
 const releases=[];const usage=new SessionUsage({json:()=>new Promise(resolve=>releases.push(resolve))},()=>{});
 usage.select({id:'a'});const old=usage.refresh();usage.event({type:'task.usage_changed',task_id:'a',payload:{session_id:'new'}});
 releases[0](page());await old;assert.equal(usage.data,null);
 releases[1](page('new'));await usage.flight;assert.equal(usage.data.session_id,'new');
 usage.select({id:'b'});assert.equal(usage.data,null);
});
test('usage events queue one fresh read behind an in-flight observation',async()=>{
 const releases=[];const usage=new SessionUsage({json:()=>new Promise(resolve=>releases.push(resolve))},()=>{});
 usage.select({id:'a'});const first=usage.refresh();usage.event({task_id:'a',type:'task.usage_changed',payload:{session_id:'s'}});
 releases[0](page());await first;await new Promise(setImmediate);assert.equal(releases.length,2);
 releases[1](page('s','next'));await usage.flight;assert.equal(usage.data.turns[0].turn_id,'next');
});

test('a changed context session invalidates pending usage and resets paging',async()=>{
 const releases=[];const usage=new SessionUsage({json:()=>new Promise(resolve=>releases.push(resolve))},()=>{});
 usage.select({id:'a',session_id:'old'});const first=usage.refresh();
 usage.event({task_id:'a',type:'task.context_changed',payload:{session_id:'new'}});
 releases[0](page('old'));await first;assert.equal(usage.data,null);
 releases[1](page('new'));await usage.flight;assert.equal(usage.data.session_id,'new');
});

test('a later baseline can discover a replacement session after a missed event',async()=>{
 const responses=[page('s'),page('replacement')];const usage=new SessionUsage({json:async()=>responses.shift()},()=>{});
 usage.select({id:'a'});usage.event({task_id:'a',type:'task.usage_changed',payload:{session_id:'s'}});await usage.flight;
 await usage.refresh();assert.equal(usage.data.session_id,'replacement');assert.equal(usage.error,null);
});
