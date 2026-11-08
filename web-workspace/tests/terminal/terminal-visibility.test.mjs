import {test} from 'node:test';
import assert from 'node:assert/strict';
import {AgentTerminal} from '../../terminal/terminal.js';
import {TaskTerminalCache} from '../../terminal/task-terminal-cache.js';

function host(){return {contains(){return false;},firstChild:null,replaceChildren(child=null){this.firstChild=child;}};}
function fixture(t){
 t.mock.method(console,'debug',()=>{});
 globalThis.document={createElement:host};
 const container=host(),terminals=[];
 const cache=new TaskTerminalCache({},container,()=>{},()=>{},(api,container)=>{
  const terminal=new AgentTerminal(api,container);
  terminal.term={rows:40,refresh:(...range)=>terminal.refreshes.push(range)};
  terminal.refreshes=[];terminal.schedules=0;terminal.geometry={schedule:()=>terminal.schedules++};
  terminal.task={id:String(terminals.length+1),status:'in_progress',live:true};
  terminal.abort=new AbortController();terminals.push(terminal);return terminal;
 });
 return {cache,container,terminals,task:id=>({id:String(id),status:'in_progress',live:true})};
}
test('unchanged primary and secondary attachments do not refresh actual AgentTerminals',async t=>{
 const {cache,terminals,task}=fixture(t),secondary=host();
 await cache.attach(task(1));await cache.attachSecondary(task(2),secondary);
 assert.deepEqual(terminals.map(terminal=>terminal.refreshes),[[[0,39]],[[0,39]]]);
 for(let i=0;i<100;i++){await cache.attach(task(1));await cache.attachSecondary(task(2),secondary);cache.show(true);}
 assert.deepEqual(terminals.map(terminal=>terminal.refreshes.length),[1,1]);
 assert.ok(terminals.every(terminal=>terminal.schedules>100));
});
test('visibility and retained host mount transitions refresh once and hidden mounts wait',async t=>{
 const {cache,container,terminals,task}=fixture(t);
 await cache.attach(task(1));const terminal=terminals[0];terminal.refreshes=[];
 cache.show(false);cache.show(false);assert.equal(terminal.refreshes.length,0);
 cache.show(true);cache.show(true);assert.deepEqual(terminal.refreshes,[[0,39]]);
 container.replaceChildren();await cache.attach(task(1));
 assert.equal(terminal.refreshes.length,2);
 cache.show(false);container.replaceChildren();await cache.attach(task(1));
 assert.equal(terminal.refreshes.length,2);cache.show(true);assert.equal(terminal.refreshes.length,3);
});
test('moving a visible secondary retained host refreshes without a visibility change',async t=>{
 const {cache,terminals,task}=fixture(t);
 await cache.attach(task(1));await cache.attachSecondary(task(2),host());
 const terminal=terminals[1];terminal.refreshes=[];
 await cache.attachSecondary(task(2),host());
 assert.deepEqual(terminal.refreshes,[[0,39]]);
 await cache.attach(task(2));assert.equal(terminal.refreshes.length,2);
});
