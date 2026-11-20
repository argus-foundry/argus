import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const html = readFileSync(new URL('../../internal/api/static/decide.html', import.meta.url),'utf8');
const context = {globalThis: {}, URL, TextEncoder, document: {addEventListener() {}}};
vm.runInNewContext(html.match(/<script id="decision-logic">([\s\S]*?)<\/script>/)[1],context);
const logic = context.globalThis.BoardDecisions;

test('typed annotations queue decision feedback without satisfying required answer notes',()=>{
 const annotation={annotation_id:'one',type:'DELETION',comment:''};
 const d={choice:'',action:'comment',note:'',annotations:[annotation]};
 assert.equal(logic.valid({kind:'decision'},d),true);
 assert.equal(logic.valid({kind:'decision'},{...d,choice:'other'}),false);
 assert.equal(logic.valid({kind:'fyi'},d),true);
 assert.equal(logic.valid({kind:'fyi'},{...d,action:'dismiss'}),false);
 assert.equal(logic.valid({kind:'decision'},{...d,annotations:[{...annotation,type:'COMMENT'}]}),false);
 assert.equal(logic.valid({kind:'decision'},{...d,annotations:Array.from({length:21},()=>annotation)}),false);
 assert.equal(logic.valid({kind:'decision'},{...d,stale:{annotations:[annotation]}}),false);
});

test('decision comments require matching durable action receipts and never accept a closed item',()=>{
 const sent={action_id:'comment-1',item_id:3,action:'comment'};
 assert.equal(logic.accepted('decision',sent,{action_id:'comment-1',item_id:3,outcome:'commented'}),true);
 assert.equal(logic.accepted('decision',sent,{action_id:'wrong',item_id:3,outcome:'commented'}),false);
 assert.equal(logic.accepted('decision',sent,{action_id:'comment-1',item_id:3,outcome:'already_resolved'}),false);
});

test('mixed actions validate comments and partition by role and kind in bounded batches',()=>{
 const records=Array.from({length:18},(_,n)=>({role:1,item:{id:n,kind:'fyi',updated_at:'r'},draft:{action:'comment',note:'feedback'}}));
 records.push({role:2,item:{id:99,kind:'decision'},draft:{choice:'other',note:'yes'}});
 assert.equal(logic.valid(records[0].item,records[0].draft),true);
 assert.equal(logic.valid({kind:'fyi'},{action:'comment',note:' '}),false);
 assert.equal(logic.valid({kind:'fyi'},{action:'dismiss',note:''}),true);
 const groups=logic.batches(records);
 assert.deepEqual(Array.from(groups,g=>[g.role,g.kind,g.records.length]),[[1,'fyi',16],[1,'fyi',2],[2,'decision',1]]);
});

test('FYI results must match action and item IDs and comment-only requires delivered feedback',()=>{
 const sent={action_id:'retry-1',item_id:3,action:'comment'};
 assert.equal(logic.accepted('fyi',sent,{action_id:'retry-1',item_id:3,outcome:'commented'}),true);
 for(const r of [{action_id:'wrong',item_id:3,outcome:'commented'},{action_id:'retry-1',item_id:4,outcome:'commented'},{action_id:'retry-1',item_id:3,outcome:'already_resolved'}])assert.equal(logic.accepted('fyi',sent,r),false);
 assert.equal(logic.accepted('fyi',{...sent,action:'dismiss'},{action_id:'retry-1',item_id:3,outcome:'already_resolved'}),true);
});

test('keyboard traverses empty rows with clamping and ignores editing, modifiers and IME',()=>{
 const rows=[{role_id:1},{role_id:2},{role_id:3}];
 const event={key:'ArrowDown',metaKey:true};
 assert.equal(logic.navigate(event,rows,1,'decision').role,2);
 assert.equal(logic.navigate(event,rows,3,'decision').role,3);
 assert.equal(logic.navigate({...event,key:'ArrowRight'},rows,1,'decision').tab,'fyi');
 for(const flags of [{isComposing:true},{shiftKey:true},{altKey:true},{metaKey:false}])assert.equal(logic.navigate({...event,...flags},rows,1,'decision'),null);
 assert.equal(logic.navigate({...event,target:{closest:()=>({})}},rows,1,'decision'),null);
});

function finish(results,{closeAllowed=true,closeThrows=false,answers=[{item_id:1},{item_id:2}]}={}) {
 let attempts=0;const confirmations=[];
 const win={closed:false,close(){attempts++;if(closeThrows)throw Error('Blocked');if(closeAllowed)this.closed=true;}};
 const complete=logic.finish(answers,results,win,count=>confirmations.push(count));
 return {complete,closed:win.closed,attempts,confirmations};
}

test('every submitted item resolved or already resolved closes the tab',()=>{
 assert.deepEqual(finish([{item_id:1,outcome:'resolved'},{item_id:2,outcome:'already_resolved'}]),{complete:true,closed:true,attempts:1,confirmations:[]});
});

test('a refused or throwing close leaves a sent confirmation with the submitted count',()=>{
 const results=[{item_id:1,outcome:'resolved'},{item_id:2,outcome:'resolved'}];
 for(const options of [{closeAllowed:false},{closeThrows:true}])assert.deepEqual(finish(results,options),{complete:true,closed:false,attempts:1,confirmations:[2]});
});

test('conflict, invalid, not found and incomplete or mismatched results never close',()=>{
 const resolved={item_id:1,outcome:'resolved'};
 for(const results of [
  ...['conflict','invalid','not_found'].map(outcome=>[resolved,{item_id:2,outcome}]),
  [resolved], [resolved,{item_id:3,outcome:'resolved'}], [resolved,resolved], [], null,
 ])assert.deepEqual(finish(results),{complete:false,closed:false,attempts:0,confirmations:[]});
 assert.deepEqual(finish([],{answers:[]}),{complete:false,closed:false,attempts:0,confirmations:[]});
});

class Node {
 constructor(tag,text=''){this.tag=tag;this.text=text;this.children=[];this.attributes={};}
 append(...values){this.children.push(...values);}
 setAttribute(key,value){this.attributes[key]=value;}
 set textContent(value){this.text=value;this.children=[];}
 get textContent(){return this.text+this.children.map(c=>typeof c==='string'?c:c.textContent).join('');}
}
const doc={createElement:t=>new Node(t),createTextNode:t=>new Node('#text',t)};
function nodes(root,tag){return [root,...root.children.flatMap(c=>typeof c==='string'?[]:nodes(c))].filter(n=>!tag || n.tag===tag);}

function theme(storage) {
 const root={dataset:{}},env={};
 Object.defineProperty(env,'localStorage',{get:typeof storage==='function'?storage:()=>storage});
 const source=html.match(/<script id="decision-theme">([\s\S]*?)<\/script>/)?.[1];
 assert.ok(source,'theme restoration must run in the head before styles/body');
 assert.ok(html.indexOf('id="decision-theme"')<html.indexOf('<style>'));
 vm.runInNewContext(source,{globalThis:env,document:{documentElement:root}});
 return {root,api:env.BoardDecisionTheme};
}

test('theme defaults to system and persists only a validated theme across pages',()=>{
 const saved=new Map(),storage={getItem:k=>saved.get(k),setItem:(k,v)=>saved.set(k,v)};
 const first=theme(storage);
 assert.equal(first.root.dataset.theme,'system');
 first.api.set('dark');
 assert.deepEqual([...saved.values()],['dark']);
 assert.equal(theme(storage).root.dataset.theme,'dark');
 first.api.set('light');assert.equal(theme(storage).root.dataset.theme,'light');
 first.api.set('system');assert.equal(theme(storage).root.dataset.theme,'system');
 first.api.set('javascript:payload');assert.equal(first.root.dataset.theme,'system');
 assert.deepEqual([...saved.values()],['system']);
 assert.equal(theme({getItem:()=> 'invalid'}).root.dataset.theme,'system');
});

test('throwing storage access, reads and writes never prevent theme rendering or changes',()=>{
 for(const [storage,initial] of [[()=>{throw Error('denied');},'system'],[{getItem(){throw Error('read');},setItem(){throw Error('write');}},'system'], [{getItem:()=> 'dark',setItem(){throw Error('full');}},'dark']]) {
  const page=theme(storage);
  assert.equal(page.root.dataset.theme,initial);
  assert.doesNotThrow(()=>page.api.set('light'));assert.equal(page.root.dataset.theme,'light');
  assert.doesNotThrow(()=>page.api.set('dark'));assert.equal(page.root.dataset.theme,'dark');
 }
});

test('invalid URL candidates remain text and uppercase HTTP(S) references link',()=>{
 const text='https://). HTTPS://example.com/path! http://, https://example.org/next';
 const rendered=logic.markup(text,doc);
 assert.equal(rendered.textContent,text);
 assert.deepEqual(nodes(rendered,'a').map(n=>n.attributes.href),['HTTPS://example.com/path','https://example.org/next']);
});

test('balanced URL brackets survive while prose closing brackets remain outside links',()=>{
 const text='Local http://[::1] and [https://example.com/path].';
 const rendered=logic.markup(text,doc);
 assert.equal(rendered.textContent,text);
 assert.deepEqual(nodes(rendered,'a').map(n=>n.attributes.href),['http://[::1]','https://example.com/path']);
});

test('links only HTTP(S), trims terminal punctuation and preserves balanced parentheses',()=>{
 const text='See http://example.com/a, https://example.com/b). https://example.com/wiki_(test). javascript:alert(1) data:text/html,payload';
 const rendered=logic.markup(text,doc),links=nodes(rendered,'a');
 assert.deepEqual(links.map(n=>n.attributes.href),['http://example.com/a','https://example.com/b','https://example.com/wiki_(test)']);
 assert.equal(rendered.textContent,text);
 for(const link of links){assert.equal(link.attributes.target,'_blank');assert.equal(link.attributes.rel,'noopener noreferrer');assert.equal(link.textContent,link.attributes.href);}
});

test('formatting links bold text but never URLs in code, including bold code',()=>{
 const rendered=logic.markup('Paragraph https://example.com\n\n**See https://example.org and `https://code.example`**\n\n- `http://code.test`',doc);
 assert.equal(nodes(rendered,'p').length,2);
 assert.equal(nodes(rendered,'ul').length,1);
 assert.deepEqual(nodes(rendered,'a').map(n=>n.attributes.href),['https://example.com','https://example.org']);
 assert.equal(nodes(rendered,'code').length,2);
 assert.ok(nodes(rendered,'code').every(n=>nodes(n,'a').length===0));
});

test('nested lists retain document-order newline projection',()=>{
 const rendered=logic.markup('A **bold** paragraph\nwith `code`.\n\n- First\n  - Nested **item**\n1. Last',doc);
 assert.equal(rendered.textContent,'A bold paragraph with code.\nFirst\nNested item\nLast');
 assert.equal(nodes(rendered,'ul').length,2);
 assert.equal(nodes(rendered,'ol').length,1);
});

test('linkification preserves XSS payloads as literal text and rejects unsafe schemes',()=>{
 const text='https://example.com/\"><img src=x onerror=alert(1)> javascript:alert(1) data:text/html,<svg onload=alert(1)>';
 const rendered=logic.markup(text,doc);
 assert.equal(rendered.textContent,text);
 assert.equal(nodes(rendered,'img').length,0);assert.equal(nodes(rendered,'svg').length,0);
 assert.deepEqual(nodes(rendered,'a').map(n=>n.attributes.href),['https://example.com/']);
});

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
 const node=logic.markup('Hello **bold** and `code`\n\n- <img src=x onerror=alert(1)>\n- second\n\n1. first',doc);
 assert.match(node.textContent,/<img src=x onerror=alert\(1\)>/);
 const tags=[];const walk=n=>{tags.push(n.tag);n.children?.filter(c=>typeof c!=='string').forEach(walk);};walk(node);
 assert.ok(tags.includes('strong'));assert.ok(tags.includes('code'));assert.ok(tags.includes('ul'));assert.ok(tags.includes('ol'));assert.ok(!tags.includes('img'));
});
