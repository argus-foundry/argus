import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const html = readFileSync(new URL('../../internal/api/static/decide.html', import.meta.url),'utf8');
const context = {globalThis: {}, document: {addEventListener() {}}};
vm.runInNewContext(html.match(/<script id="decision-logic">([\s\S]*?)<\/script>/)[1],context);
const logic = context.globalThis.BoardDecisions;

test('payload includes only decided cards and keeps revisions',()=>{
 const items=[{id:1,updated_at:'a',structured:null},{id:2,updated_at:'b',structured:{options:[{id:'retry'}]}}];
 assert.equal(logic.payload(items,{1:{choice:'other',note:' '},2:{choice:'retry',note:''}}).length,1);
 const answer=logic.payload(items,{1:{choice:'other',note:'Custom'}})[0];
 assert.equal(answer.expected_updated_at,'a');assert.equal(answer.choice,'other');
});

test('conflict refresh preserves draft and clears stale choice',()=>{
 const state=logic.outcome({id:1,updated_at:'a'}, {choice:'retry',note:'Keep this'}, {outcome:'conflict',item:{id:1,updated_at:'b'}});
 assert.equal(state.item.updated_at,'b');assert.equal(state.draft.note,'Keep this');assert.equal(state.draft.choice,'');assert.equal(state.status,'conflict');
 assert.equal(logic.outcome({id:1},{},{outcome:'resolved'}).status,'answered');
 assert.equal(logic.outcome({id:1},{},{outcome:'already_resolved'}).status,'closed');
});

test('draft keys isolate roles, items and revisions',()=>{
 assert.equal(logic.draftKey(2,{id:3,updated_at:'a'}),'board-decision:2:3:a');
 assert.notEqual(logic.draftKey(2,{id:3,updated_at:'a'}),logic.draftKey(2,{id:3,updated_at:'b'}));
});

test('markup renderer treats hostile HTML as text',()=>{
 class Node {constructor(tag,text=''){this.tag=tag;this.text=text;this.children=[];}append(...values){this.children.push(...values);} get textContent(){return this.text+this.children.map(c=>typeof c==='string'?c:c.textContent).join('');}}
 const doc={createElement:t=>new Node(t),createTextNode:t=>new Node('#text',t)};
 const node=logic.markup('Hello **bold** and `code`\n\n- <img src=x onerror=alert(1)>\n- second\n\n1. first',doc);
 assert.match(node.textContent,/<img src=x onerror=alert\(1\)>/);
 const tags=[];const walk=n=>{tags.push(n.tag);n.children?.filter(c=>typeof c!=='string').forEach(walk);};walk(node);
 assert.ok(tags.includes('strong'));assert.ok(tags.includes('code'));assert.ok(tags.includes('ul'));assert.ok(tags.includes('ol'));assert.ok(!tags.includes('img'));
});
