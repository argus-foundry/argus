import test from 'node:test';
import assert from 'node:assert/strict';
import {focusDOM} from './focus-dom.mjs';
import {fileRow,paintCounts} from '../../tasks/changes/task-files.js';
import {SandboxProposals} from '../../settings/sandbox/settings-proposals.js';
import {ShareShortcut} from '../../settings/notifications/settings-share.js';
import {DeviceTokens} from '../../settings/notifications/settings-tokens.js';
import {ScheduleSettings} from '../../settings/schedules/settings-schedules.js';

function fixture(t) {
  const dom=focusDOM(t),host=dom.element('section');dom.body.append(host);
  const proto=Object.getPrototypeOf(host);
  proto.addEventListener=function(type,handler){(this.listeners??={})[type]=handler;};
  const click=control=>control.listeners.click();
  const named=(root,name)=>root.querySelectorAll('button').find(control=>control.textContent===name);
  return {...dom,host,click,named};
}
for(const [status,label] of [['A','Added'],['M','Modified'],['D','Deleted'],['??','Untracked'],['R100','Renamed']]) {
  test(`Changes row exposes ${label} and updated exact counts`,t=>{
    const {host}=fixture(t);
    host.append(fileRow({}, {path:'src/app.js',status}, {openDiff(){},counts:undefined}));
    const row=host.querySelector('.file-row'),counts=host.querySelector('.file-counts');
    assert.match(row.getAttribute('aria-label'),new RegExp(label));
    assert.match(row.getAttribute('aria-label'),/Loading line counts/);
    paintCounts(counts,{added:8,removed:2});
    assert.match(row.getAttribute('aria-label'),/8 lines added, 2 removed/);
    for(const kind of ['binary','unavailable']) {
      paintCounts(counts,{kind});
      assert.match(row.getAttribute('aria-label'),kind==='binary'?/Binary diff/:/Line counts unavailable/);
      assert.doesNotMatch(row.getAttribute('aria-label'),/8 lines/);
    }
    assert.equal(counts.getAttribute('tabindex'),null);
  });
}
for(const index of [0,1,2])test(`Remove proposed path ${index} restores next or previous input`,t=>{
  const {host,click,named}=fixture(t);
  const view=Object.assign(Object.create(SandboxProposals.prototype),{host,drafts:new Map()});
  view.render({id:'p',entries:['a','b','c'].map(path=>({path}))});
  const remove=host.querySelectorAll('button').filter(control=>control.textContent==='Remove path')[index];
  remove.focus();click(remove);
  const inputs=host.querySelectorAll('input');
  assert.equal(document.activeElement,inputs[Math.min(index,inputs.length-1)]);
  assert.equal(document.activeElement.value,index===0?'b':index===1?'c':'b');
  while(host.querySelectorAll('input').length){const control=named(host,'Remove path');control.focus();click(control);}
  assert.equal(document.activeElement,named(host,'Add path'));
});
test('Remove path leaves outside focus alone',t=>{
  const {host,tab,click,named}=fixture(t);
  const view=Object.assign(Object.create(SandboxProposals.prototype),{host,drafts:new Map()});
  view.render({id:'p',entries:[{path:'a'}]});tab.focus();click(named(host,'Remove path'));
  assert.equal(document.activeElement,tab);
});
for(const mode of ['new','edit','detached','outside'])test(`Schedule Cancel restores ${mode} opener`,t=>{
  const {host,element,tab,click,named}=fixture(t),editor=element('div'),newButton=element('button'),opener=element('button');
  host.append(newButton,opener,editor);
  const view=Object.assign(Object.create(ScheduleSettings.prototype),{host,editor,newButton,projects:[],backends:[],status(){}});
  const source=mode==='new'?newButton:opener;source.focus();view.edit(mode==='new'?null:{name:'Daily'});
  assert.equal(document.activeElement,editor.querySelector('input'));
  if(mode==='detached')opener.remove();
  const cancel=named(editor,'Cancel');cancel.focus();
  if(mode==='outside')tab.focus();click(cancel);
  assert.equal(editor.children.length,0);
  assert.equal(document.activeElement,mode==='outside'?tab:mode==='detached'?newButton:source);
});

test('Changes directory row exposes status and directory count explanation',t=>{
  const {host}=fixture(t);
  host.append(fileRow({}, {path:'vendor/',status:'??',directory:true}, {expandDirectory(){},counts:{kind:'directory'}}));
  const row=host.querySelector('.file-row');
  assert.equal(row.getAttribute('aria-label'),'Expand directory vendor/, Untracked, Expand directory to inspect its files');
  assert.equal(row.getAttribute('aria-expanded'),'false');
});

for(const mode of ['new','edit','missing','outside','moved','failed','destroyed'])test(`Schedule Save preserves ${mode} focus`,async t=>{
  const {host,element,tab,named}=fixture(t),editor=element('div'),list=element('div'),newButton=element('button');
  host.append(newButton,list,editor);
  const original={id:'job',name:'Daily',project:'demo',prompt:'hello',schedule:'@daily'};
  const other={...original,id:'other',name:'Other'};
  let release;const waiting=new Promise(resolve=>{release=resolve;});
  const view=Object.assign(Object.create(ScheduleSettings.prototype),{host,editor,list,newButton,projects:[{name:'demo'}],backends:[],rows:[other,original],abort:new AbortController(),busy(){},status(){},changed(){},
    api:{json:async()=>{await waiting;if(mode==='failed')throw new Error('unavailable');return {}; }},
    async load(){this.rows=mode==='missing'?[]:[other,{...original,name:'Renamed'}];this.renderList();}
  });
  view.renderList();view.edit(mode==='new'?null:original);
  const save=named(editor,'Save schedule');save.focus();if(mode==='outside')tab.focus();
  const pending=view.save({...original,name:'Renamed',cadence:'cron'},mode==='new'?null:original);
  if(mode==='moved')tab.focus();if(mode==='destroyed')view.destroy();
  release();await pending;
  if(['outside','moved'].includes(mode))assert.equal(document.activeElement,tab);
  else if(mode==='failed')assert.equal(document.activeElement,save);
  else if(mode==='destroyed')assert.equal(document.activeElement,save);
  else {
    const target=mode==='edit'?list.querySelectorAll('button').find(control=>control.getAttribute('aria-label')==='Edit Renamed'):newButton;
    assert.equal(document.activeElement,target);assert.equal(target.disabled,false);
  }
});

for(const kind of ['share','token'])for(const mode of ['hide','outside','destroy'])test(`${kind} credential ${mode} preserves focus and erases value`,async t=>{
  const {host,tab,click,named}=fixture(t);
  const api={shareURL:()=> 'https://example.invalid/share?token=secret',json:async(path,options)=>options?.method==='POST'?{token:'secret'}:{tokens:[]}};
  const context={api,host,status(){},busy(){}};
  const view=kind==='share'?new ShareShortcut(context):new DeviceTokens(context);
  if(kind==='share')click(named(host,'Generate share URL'));else await view.mint();
  const output=host.querySelectorAll('input').find(input=>input.name===(kind==='share'?'shareURL':'newToken'));
  const hide=named(host,kind==='share'?'Hide share URL':'Hide device token');hide.focus();
  if(mode==='outside')tab.focus();
  if(mode==='destroy')view.destroy();else click(hide);
  assert.equal(output.value,'');assert.equal(output.isConnected,false);
  const target=kind==='share'?named(host,'Generate share URL'):view.label;
  assert.equal(document.activeElement,mode==='hide'?target:mode==='outside'?tab:document.body);
});
