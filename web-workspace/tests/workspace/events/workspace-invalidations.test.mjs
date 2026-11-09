import {test} from 'node:test';import assert from 'node:assert/strict';import {WorkspaceEvents} from '../../../workspace/events/workspace-events.js';
test('usage and board invalidations bypass buffered task snapshots',async()=>{
 const usage=[],board=[],signal=new AbortController();
 const events=Object.assign(Object.create(WorkspaceEvents.prototype),{running:true,generation:1,cursor:0,buffering:true,pending:[],callbacks:{context:event=>usage.push(event),board:event=>{board.push(event);signal.abort();}}});
 const frames=[{id:1,type:'task.usage_changed',task_id:'a',payload:{session_id:'s'}},{id:2,type:'hera.board_changed',payload:{role_id:2,item_id:3,unread_count:1}}];
 await events.consume(new Response(new ReadableStream({start(controller){controller.enqueue(new TextEncoder().encode(frames.map(event=>'data: '+JSON.stringify(event)+'\n\n').join('')));}})),1,signal.signal);
 assert.deepEqual(usage,[frames[0]]);assert.deepEqual(board,[frames[1]]);assert.deepEqual(events.pending,[]);
});
