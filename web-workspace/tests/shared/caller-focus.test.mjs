import {test} from 'node:test';
import assert from 'node:assert/strict';
import {focusDOM} from './focus-dom.mjs';
import {TaskSettingsView} from '../../tasks/settings/task-settings.js';
import {WorkspaceEvents} from '../../workspace/events/workspace-events.js';

function setup(t){
 const dom=focusDOM(t),proto=Object.getPrototypeOf(dom.body),listeners=new Set();
 proto.getClientRects=function(){for(let node=this;node;node=node.parentElement)if(node.hidden)return [];return this.isConnected?[{}]:[];};
 document.addEventListener=(type,listener)=>{if(type==='focusin')listeners.add(listener);};document.removeEventListener=(type,listener)=>listeners.delete(listener);
 const focus=proto.focus;proto.focus=function(){focus.call(this);for(const listener of [...listeners])listener({target:this});};
 document.querySelector=selector=>dom.body.querySelector(selector);document.getElementById=id=>dom.body.querySelectorAll('*').find(item=>item.id===id);
 dom.body.classList={toggle(){}};
 return {...dom,listeners};
}
for(const cancelled of [false,true])test(`task settings request timeout differs from view cancellation cancelled=${cancelled}`,async t=>{
 const dom=setup(t),host=dom.element('section'),back=dom.element('button'),retry=dom.element('button');dom.body.append(back,host);host.append(retry);retry.textContent='Retry task settings';retry.focus();
 const deadline=new AbortController();t.mock.method(AbortSignal,'timeout',()=>deadline.signal);
 const view=Object.assign(Object.create(TaskSettingsView.prototype),{active:true,generation:1,readVersion:0,task:{id:'a'},abort:new AbortController(),host,back,message(){},api:{json(path,{signal}){return new Promise((resolve,reject)=>signal.addEventListener('abort',()=>reject(signal.reason),{once:true}));}}});
 const read=view.load();
 if(cancelled){back.focus();view.active=false;view.abort.abort();}else deadline.abort(new DOMException('Timed out','TimeoutError'));
 await read;
 if(cancelled)assert.equal(document.activeElement,back);
 else{assert.equal(view.abort.signal.aborted,false);assert.equal(view.readAbort.signal.aborted,false);assert.equal(document.activeElement,host.querySelector('button'));}
 assert.equal(dom.listeners.size,0);
});
for(const mode of ['healthy','failed retry','initial'])test(`connection banner focus recovery ${mode}`,async t=>{
 const dom=setup(t),header=dom.element('header');header.className='workspace-header';header.after=item=>dom.body.append(item);dom.body.append(header);dom.tab.id='draft';
 const events=new WorkspaceEvents({request:async()=>{throw new Error('Unavailable');}},{refresh:async()=>{}});events.running=true;events.problem('Unavailable');
 try {
 if(mode!=='initial')events.retry.focus();else document.activeElement=document.body;
 if(mode==='failed retry')await events.reconnect();else events.healthy();
 assert.equal(document.activeElement,mode==='initial'?document.body:mode==='failed retry'?events.retry:dom.tab);
 assert.equal(dom.listeners.size,0);
 } finally { events.stop(); }
});

for(const settlement of ['success','unchanged','failure','status','empty'])test(`Tools consumes its ${settlement} load capture`,async t=>{
 const dom=setup(t),panel=dom.element('section'),origin=dom.element('button');panel.id='tool-panel';origin.textContent='Report';panel.append(origin);dom.body.append(panel);dom.tab.id='tool-artifacts';origin.focus();
 const {TaskTools}=await import('../../tasks/tools.js');
 const tools=Object.assign(Object.create(TaskTools.prototype),{task:settlement==='empty'?null:{id:'a'},tool:settlement==='status'?'status':'artifacts',generation:0,abort:new AbortController(),live:{stop(){},select(){}},visible:()=>true,invalidateIdentity(){},review:{},drawStatus:async()=>panel.replaceChildren(dom.element('button'))});
 if(settlement==='status')dom.tab.id='tool-status';
 await tools.load();
 const result={artifacts:[{filename:'report.txt',name:'Report',type:'text',size:1}]};
 if(settlement==='unchanged')tools.artifactFingerprint=JSON.stringify(result.artifacts);
 if(settlement==='success'||settlement==='unchanged')tools.apply(result);else if(settlement==='failure')tools.failed(new Error('Unavailable'));
 assert.equal(tools.restoreFocus,null);
 const outside=dom.element('button');dom.body.append(outside);outside.focus();outside.remove();tools.tool='artifacts';tools.apply({artifacts:[]});
 assert.equal(document.activeElement,document.body);
 assert.equal(dom.listeners.size,0);
});
