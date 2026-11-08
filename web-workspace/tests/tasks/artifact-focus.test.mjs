import test from 'node:test';
import assert from 'node:assert/strict';
import {TaskTools} from '../../tasks/tools.js';
import {projectForm} from '../../settings/projects/settings-project-form.js';

function fixture(t) {
  const original=globalThis.document;
  class Element {
    constructor(tag) { this.tagName=tag;this.children=[];this.dataset={};this.className=''; }
    append(...items) { this.children.push(...items); }
    replaceChildren(...items) { this.children=items; }
    setAttribute() {}
    addEventListener() {}
    focus() { document.activeElement=this; }
    querySelectorAll(selector) { return this.children.filter(item=>selector==='.artifact-row'&&item.className==='artifact-row'); }
  }
  const panel=new Element('section'), tab=new Element('button'), outside=new Element('button');
  globalThis.document={activeElement:outside,createElement:tag=>new Element(tag),getElementById:id=>id==='tool-panel'?panel:tab};
  t.after(()=>{globalThis.document=original;});
  const tools=Object.assign(Object.create(TaskTools.prototype),{task:{id:'a'},review:{openArtifact(){}}});
  return {panel,tab,outside,tools};
}
const artifacts=names=>names.map(filename=>({filename,type:'text',size:1}));
test('repository form rendering leaves navigation focus alone',t=>{
  const {panel,outside}=fixture(t);
  projectForm(panel,null,[],()=>{},()=>{});
  assert.equal(document.activeElement,outside);
});
test('artifact focus follows filename rather than display name or row position',t=>{
  const {panel,tools}=fixture(t);
  tools.drawArtifacts(artifacts(['a','b']).map(item=>({...item,name:'Shared label'})));panel.querySelectorAll('.artifact-row')[1].focus();
  tools.drawArtifacts(artifacts(['b','a','c']).map(item=>({...item,name:'Updated label'})));
  assert.equal(document.activeElement,panel.querySelectorAll('.artifact-row')[0]);
});
test('removed artifact chooses a neighbour then tab for an empty list',t=>{
  const {panel,tab,tools}=fixture(t);
  tools.drawArtifacts(artifacts(['a','b','c']));panel.querySelectorAll('.artifact-row')[2].focus();
  tools.drawArtifacts(artifacts(['a','b']));
  assert.equal(document.activeElement,panel.querySelectorAll('.artifact-row')[1]);
  tools.drawArtifacts([]);assert.equal(document.activeElement,tab);
});
test('artifact background rendering leaves outside focus alone',t=>{
  const {outside,tools}=fixture(t);
  tools.drawArtifacts(artifacts(['a']));tools.drawArtifacts(artifacts(['b']));
  assert.equal(document.activeElement,outside);
});

test('removing the first focused artifact selects the next row',t=>{
  const {panel,tools}=fixture(t);
  tools.drawArtifacts(artifacts(['a','b']));panel.querySelectorAll('.artifact-row')[0].focus();
  tools.drawArtifacts(artifacts(['b']));
  assert.equal(document.activeElement,panel.querySelectorAll('.artifact-row')[0]);
});
