import {test} from 'node:test';
import assert from 'node:assert/strict';
import {focusDOM} from './focus-dom.mjs';
import {BrowserPushSettings} from '../../settings/notifications/settings-push.js';
import {SplitWorkspace} from '../../workspace/split/split-workspace.js';

function setup(t){
 const dom=focusDOM(t),proto=Object.getPrototypeOf(dom.body);
 proto.classList={toggle(){},add(){}};
 proto.getClientRects=function(){return this.isConnected&&!this.hidden?[{}]:[];};
 document.addEventListener=()=>{};document.removeEventListener=()=>{};document.querySelector=selector=>dom.body.querySelector(selector);
 return dom;
}
for(const action of ['Enable desktop alerts','Send test notification'])test(`initial subscription lookup preserves ${action}`,async t=>{
 const dom=setup(t),controls=dom.element('div');dom.body.append(controls);
 const navigatorDescriptor=Object.getOwnPropertyDescriptor(globalThis,'navigator');
 let release;Object.defineProperty(globalThis,'navigator',{configurable:true,value:{serviceWorker:{getRegistration:()=>new Promise(resolve=>release=resolve)}}});
 t.after(()=>{if(navigatorDescriptor)Object.defineProperty(globalThis,'navigator',navigatorDescriptor);else delete globalThis.navigator;});
 const view=Object.assign(Object.create(BrowserPushSettings.prototype),{supported:true,controls,abort:new AbortController(),generation:0});view.render();
 controls.querySelectorAll('button').find(control=>control.textContent===action).focus();const read=view.loadBrowser();release(null);await read;
 assert.equal(document.activeElement,controls.querySelectorAll('button').find(control=>control.textContent===action));
});
for(const kind of ['disabled','removed'])test(`split roster recovers when focused role is ${kind}`,t=>{
 const dom=setup(t),pane=dom.element('section'),details=dom.element('details'),summary=dom.element('summary'),rows=dom.element('div'),picker=dom.element('select');dom.body.append(pane);pane.append(picker,details);details.append(summary,rows);details.open=true;
 const role={role_id:2,kind:'worker',task_id:'w',live:true},task={id:'w',name:'Worker'};
 const view=Object.assign(Object.create(SplitWorkspace.prototype),{enabled:true,visible:true,task:{id:'c'},tasks:[task],roster:{orchestrators:[{id:1,roles:[{role_id:1,kind:'coordinator',task_id:'c',live:true},role]}]},pane,picker,rosterDetails:details,rosterSummary:summary,rosterRows:rows});
 view.renderRoster();rows.children[1].focus();
 if(kind==='disabled'){role.live=false;view.renderRoster();assert.equal(document.activeElement,summary);}
 else {view.roster={};view.scheduleRosterRender();assert.equal(document.activeElement,picker);}
});

for(const surface of ['general','projects','sandbox','proposals','schedules'])test(`${surface} retry loader restores a surviving control`,async t=>{
 const dom=setup(t),host=dom.element('section');dom.body.append(host);
 const retry=dom.element('button');retry.textContent='Retry';host.append(retry);retry.focus();
 const choices={general:['../../settings/settings-general.js','GeneralSettings'],projects:['../../settings/projects/settings-projects.js','ProjectSettings'],sandbox:['../../settings/sandbox/settings-sandbox.js','SandboxSettings'],proposals:['../../settings/sandbox/settings-proposals.js','SandboxProposals'],schedules:['../../settings/schedules/settings-schedules.js','ScheduleSettings']};
 const [path,name]=choices[surface],Type=(await import(path))[name];
 const view=Object.assign(Object.create(Type.prototype),{host,abort:new AbortController(),version:0,newButton:dom.tab,list:host,status(){},baseline:x=>x,api:{async json(){throw new Error('Read unavailable');}}});
 await view.load();assert.notEqual(document.activeElement,document.body);assert.equal(host.contains(document.activeElement),true);
});
for(const surface of ['push','tokens'])test(`${surface} settled list refresh restores a surviving action`,async t=>{
 const dom=setup(t),host=dom.element('section'),list=dom.element('div');dom.body.append(host);host.append(list);
 const old=dom.element('button');old.textContent='Remove';list.append(old);
 const Type=surface==='push'?BrowserPushSettings:(await import('../../settings/notifications/settings-tokens.js')).DeviceTokens;
 let release;const view=Object.assign(Object.create(Type.prototype),{host,devices:list,list,label:dom.tab,abort:new AbortController(),api:{json:()=>new Promise(resolve=>release=resolve)}});
 const read=surface==='push'?view.loadDevices():view.load();old.focus();release(surface==='push'?{subscriptions:[{id:'a',label:'Daily'}]}:{tokens:[{id:'a',label:'Daily'}]});await read;
 assert.equal(document.activeElement,list.querySelector('button'));
});

for(const secondary of [true,false])test(`terminal prune recovers focus secondary=${secondary}`,async t=>{
 const dom=setup(t),container=dom.element('div'),input=dom.element('textarea');dom.body.append(container);container.append(input);input.focus();
 dom.tab.id=secondary?'secondary-task-picker':'active-filter';
 const {TaskTerminalCache}=await import('../../terminal/task-terminal-cache.js');
 const cache=Object.assign(Object.create(TaskTerminalCache.prototype),{container,selected:secondary?null:'a',secondary:secondary?{id:'a',container}:null,records:new Map([['a',{terminal:{show(){},destroy(){}}}]]),onFit(){}});
 cache.retain(new Set());assert.equal(document.activeElement,dom.tab);
});

test('Agents refresh retains its replacement refresh control',async t=>{
 const dom=setup(t),host=dom.element('section');dom.body.append(host);
 const {AgentSettings}=await import('../../settings/agents/settings-agents.js');
 const view=Object.assign(Object.create(AgentSettings.prototype),{host,abort:new AbortController(),api:{async json(){return {};}}});
 await view.load();host.querySelector('button').focus();await view.load();assert.equal(document.activeElement,host.querySelector('button'));
});
for(const replaced of [false,true])test(`token secret settlement recovers focus replaced=${replaced}`,async t=>{
 const dom=setup(t),secret=dom.element('div'),list=dom.element('div');dom.body.append(secret,list);
 const button=dom.element('button');button.textContent='Copy device token';secret.append(button);button.focus();
 const {DeviceTokens}=await import('../../settings/notifications/settings-tokens.js');
 const view=Object.assign(Object.create(DeviceTokens.prototype),{secret,list,label:dom.tab,abort:new AbortController(),busy(){},load(){}});
 let expected=dom.tab;
 await view.action(async()=>({}),()=>{secret.replaceChildren();if(replaced){expected=dom.element('button');expected.textContent='Copy device token';secret.append(expected);}});
 assert.equal(document.activeElement,expected);
});
