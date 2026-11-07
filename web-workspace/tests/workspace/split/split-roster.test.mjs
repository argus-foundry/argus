import {test} from 'node:test';
import assert from 'node:assert/strict';
import {SplitWorkspace} from '../../../workspace/split/split-workspace.js';

function fixture(t) {
  let created=0;
  const frames=new Map();let frameID=0;
  class Element {
    constructor(tag){this.tagName=tag;this.children=[];this.dataset={};this.attributes={};this.listeners={};this.value='';this.open=false;this.replacements=0;this.classList={toggle(){},add(){}};created++;}
    append(...items){for(const item of items){item.remove();item.parent=this;this.children.push(item);}}
    get parentNode(){return this.parent;}
    before(){}
    insertBefore(item,before){item.remove();item.parent=this;this.children.splice(this.children.indexOf(before),0,item);}
    remove(){if(this.parent){this.parent.children=this.parent.children.filter(item=>item!==this);this.parent=null;}}
    replaceChildren(...items){this.replacements++;for(const item of [...this.children])item.remove();this.append(...items);}
    addEventListener(event,listener){this.listeners[event]=listener;}
    setAttribute(key,value){this.attributes[key]=value;}
    getAttribute(key){return this.attributes[key];}
    querySelectorAll(){return this.children.flatMap(child=>[...(child.attributes['aria-label']?[child]:[]),...child.querySelectorAll()]);}
    contains(target){return this===target||this.children.some(child=>child.contains(target));}
    focus(){document.activeElement=this;}
    closest(){return null;}
    get selectedOptions(){return this.children.filter(child=>child.value===this.value);}
  }
  const ids=new Map(['fit-session','terminal','agent-panel'].map(id=>[id,new Element('div')]));
  t.mock.method(globalThis,'requestAnimationFrame',callback=>{frames.set(++frameID,callback);return frameID;});
  t.mock.method(globalThis,'cancelAnimationFrame',id=>frames.delete(id));
  t.mock.method(console,'debug',()=>{});
  const saved=globalThis.document;
  globalThis.document={activeElement:null,getElementById:id=>ids.get(id),createElement:tag=>new Element(tag),createElementNS:(_,tag)=>new Element(tag)};
  t.after(()=>{globalThis.document=saved;});
  const split=new SplitWorkspace(()=>({releaseSecondary(){},attachSecondary(){}}));split.enabled=true;
  const tasks=[{id:'c',name:'Coordinator'},{id:'w',name:'Worker',live:true}];
  const roster={orchestrators:[{id:1,name:'Team',roles:[{role_id:1,kind:'coordinator',live:true,task_id:'c'},{role_id:2,kind:'worker',live:true,task_id:'w',status:'working'}]}]};
  const flush=()=>{for(const [id,callback] of [...frames]){frames.delete(id);callback();}};
  const open=()=>{split.rosterDetails.open=true;split.rosterDetails.listeners.toggle?.();flush();};
  split.select(tasks[0],tasks,roster);
  return {split,tasks,roster,frames,flush,open,created:()=>created};
}
// Node does not supply browser animation frames.
globalThis.requestAnimationFrame??=()=>{};
globalThis.cancelAnimationFrame??=()=>{};

test('unchanged split updates create no rows and retain focus and current callbacks',t=>{
  const {split,tasks,roster,frames,open,created}=fixture(t);open();
  const row=split.rosterRows.children[1];row.focus();const before=created(),replacements=split.rosterRows.replacements;
  for(let i=0;i<100;i++)split.update(structuredClone(tasks),structuredClone(roster));
  assert.equal(created(),before);assert.equal(split.rosterRows.replacements,replacements);assert.equal(frames.size,0);assert.equal(document.activeElement,row);
  const current={...tasks[1],runtime:'new'};split.update([tasks[0],current],roster);
  let selected;split.choose=id=>{selected=split.tasks.find(task=>task.id===id);};row.listeners.click();assert.equal(selected,current);
  selected=null;split.update([tasks[0]],roster);row.listeners.click();assert.equal(selected,null);
});
test('collapsed roster defers construction and opens with latest state',t=>{
  const {split,tasks,roster,flush,open}=fixture(t);
  assert.equal(split.rosterRows.children.length,0);
  for(let i=0;i<100;i++)split.update([tasks[0],{...tasks[1],name:`Worker ${i}`}],roster);
  flush();assert.equal(split.rosterRows.children.length,0);
  open();assert.equal(split.rosterRows.children.length,2);assert.equal(split.rosterRows.children[1].dataset.tooltip,'Worker 99');
});
test('changed bursts render once, preserve row focus and secondary selection, and clear cancels work',t=>{
  const {split,tasks,roster,frames,flush,open}=fixture(t);open();split.rosterRows.children[1].focus();
  const before=split.rosterRows.replacements;
  for(let i=0;i<100;i++)split.update([tasks[0],{...tasks[1],name:`Worker ${i}`}],roster);
  assert.equal(split.rosterRows.replacements,before);assert.equal(frames.size,1);
  flush();assert.equal(split.rosterRows.replacements,before+1);assert.equal(split.picker.value,'w');assert.equal(document.activeElement,split.rosterRows.children[1]);
  assert.equal(split.rosterRows.children[1].getAttribute('aria-current'),'page');
  split.update(tasks,roster);split.clear();flush();assert.equal(split.rosterRows.children.length,0);assert.equal(frames.size,0);
});

test('closing before a queued frame defers the latest rows until reopen',t=>{
  const {split,tasks,roster,flush,open}=fixture(t);open();const before=split.rosterRows.replacements;
  split.update([tasks[0],{...tasks[1],name:'Latest'}],roster);split.rosterDetails.open=false;flush();
  assert.equal(split.rosterRows.replacements,before);open();assert.equal(split.rosterRows.replacements,before+1);assert.equal(split.rosterRows.children[1].dataset.tooltip,'Latest');
});

test('a burst returning to rendered inputs needs no row replacement',t=>{
  const {split,tasks,roster,flush,open}=fixture(t);open();const before=split.rosterRows.replacements;
  split.update([tasks[0],{...tasks[1],name:'Transient'}],roster);split.update(tasks,roster);flush();
  assert.equal(split.rosterRows.replacements,before);
});

for(const [field,value] of [['status','complete'],['archived',true],['archive_pending',true],['needs_input',true],['live',false],['idle',true],['pr_state','approved']]){
  test(`split rows refresh when task ${field} changes`,t=>{
    const {split,tasks,roster,flush,open}=fixture(t);open();const row=split.rosterRows.children[1];
    split.update([tasks[0],{...tasks[1],[field]:value}],roster);flush();assert.notEqual(split.rosterRows.children[1],row);
  });
}
test('role changes and explicit secondary selection update retained rows',t=>{
  const {split,tasks,roster,flush,open}=fixture(t);open();
  const next=structuredClone(roster);next.orchestrators[0].roles[1].status='blocked';split.update(tasks,next);flush();
  assert.equal(split.rosterRows.children[1].children.at(-1).dataset.roleStatus,'blocked');
  split.choose('');flush();assert.equal(split.picker.value,'');assert.equal(split.rosterRows.children[1].getAttribute('aria-current'),undefined);
});

test('team disclosure and agent activity use plain product copy',t=>{
 const {split,open}=fixture(t);open();
 assert.equal(split.rosterSummary.textContent,'Team tasks');
 assert.equal(split.rosterRows.children[1].children.at(-1).getAttribute('aria-label'),'Agent working');
});
