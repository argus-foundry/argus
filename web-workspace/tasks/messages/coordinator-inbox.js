import {node,button} from '../../shared/dom.js';
import {captureFocus} from '../../shared/focus-recovery.js';
import {log} from '../../core/api.js';

export function coordinatorRoles(roster={},taskID){return (roster?.orchestrators||[]).flatMap(orch=>orch.roles||[]).filter(role=>role.kind==='coordinator'&&role.live&&!role.archived&&role.task_id===taskID).sort((a,b)=>a.role_id-b.role_id);}
export function replyText(draft,item){return `${draft}${draft?'\n\n':''}Reply to Inbox #${item.id}: ${item.subject}\n`;}
export class CoordinatorInbox {
 constructor(api,panel,callbacks={}){Object.assign(this,{api,panel,callbacks,generation:0,items:[],expanded:new Set()});}
 select(task,role){this.destroy();this.task=task;this.role=role;this.items=[];this.error=null;this.expanded.clear();}
 destroy(){this.generation++;this.abort?.abort();this.task=null;this.role=null;this.flight=null;this.busy=false;this.queued=false;}
 event(event){if(event.type==='resync'||event.type==='hera.board_changed'&&event.payload?.role_id===this.role?.role_id){if(this.flight||this.busy)this.queued=true;else this.refresh();}}
 refresh(){
  if(!this.role||this.flight)return this.flight||Promise.resolve();
  const generation=this.generation,abort=this.abort=new AbortController();const recover=captureFocus(this.panel,()=>this.panel?.querySelector('button'),{signal:abort.signal});this.busy=true;this.render();
  const flight=(async()=>{
   try{
    const result=await this.api.json(`/api/hera/roles/${this.role.role_id}/board`,{signal:AbortSignal.any([abort.signal,AbortSignal.timeout(10000)])});
    if(generation!==this.generation||abort.signal.aborted)return;
    this.items=result.items||[];this.error=null;this.role.board_unread=result.unread_count;this.callbacks.changed?.(this.role.role_id,result.unread_count);log('human-inbox','loaded',{count:this.items.length});
   }catch(error){if(generation!==this.generation||abort.signal.aborted)return;this.error=this.failure(error);log('human-inbox','read failed',{status:error.status||0});}
   finally{if(generation===this.generation){this.busy=false;this.flight=null;this.render();recover();if(this.queued){this.queued=false;queueMicrotask(()=>this.refresh());}}}
  })();this.flight=flight;return flight;
 }
 failure(error){return error.status===401||error.status===403?'Token expired. Reconnect to open Inbox.':error.status===404?'Coordinator or item unavailable. Refresh Inbox.':'Inbox change was not confirmed. Refresh before retrying.';}
 async mutate(item,operation,fields){
  if(!this.role||this.busy)return;
  const generation=this.generation,role=this.role,abort=this.abort=new AbortController();const recover=captureFocus(this.panel,()=>this.panel?.querySelector('button'),{signal:abort.signal});this.busy=true;this.render();
  try{
   const result=await this.api.json(`/api/hera/roles/${role.role_id}/board/${item.id}/${operation}`,{method:operation==='read-state'?'PUT':'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({...fields,expected_updated_at:item.updated_at}),signal:AbortSignal.any([abort.signal,AbortSignal.timeout(10000)])});
   if(generation!==this.generation||abort.signal.aborted)return;
   this.items=this.items.map(old=>old.id===item.id?result.item:old).filter(value=>!value.resolved);this.error=null;
   role.board_unread=result.unread_count;this.callbacks.changed?.(role.role_id,result.unread_count);log('human-inbox','updated',{count:this.items.length});
  }catch(error){
   if(generation!==this.generation||abort.signal.aborted)return;
   log('human-inbox','mutation failed',{status:error.status||0});
   if(error.status===409){this.expanded.delete(item.id);this.busy=false;await this.refresh();if(generation!==this.generation)return;this.error='Item changed. Open its latest revision before trying again.';}
   else this.error=this.failure(error);
  }finally{if(generation===this.generation){this.busy=false;this.render();recover();if(this.queued){this.queued=false;this.refresh();}}}
 }
 render(){
  if(!this.panel)return;const root=this.panel,top=root.scrollTop;
  this.content ||= node('div');this.status ||= node('p','tool-note');if(!this.content.isConnected)root.replaceChildren(this.status,this.content);const panel=this.content;
  const recover=captureFocus(root,()=>panel.querySelector('button'));
  panel.replaceChildren(node('h2','','Inbox'));
  if(this.roles?.length>1){
    const label=node('label','','Coordinator Inbox'),select=node('select');select.setAttribute('aria-label','Coordinator Inbox');
    for(const role of this.roles){const option=node('option','',`${role.name||'Coordinator'} (${role.role_id})`);option.value=String(role.role_id);select.append(option);}
    select.value=String(this.role.role_id);select.disabled=this.busy;select.addEventListener('change',()=>this.callbacks.selectRole?.(Number(select.value)));label.append(select);panel.append(label);
  }
  const refresh=button('Refresh Inbox',()=>this.refresh());refresh.disabled=this.busy;panel.append(refresh);
  if(this.error){const error=node('p','tool-note',this.error);error.setAttribute('role','alert');panel.append(error);}
  this.status ||= node('p','tool-note');this.status.setAttribute('role','status');this.status.setAttribute('aria-live','polite');this.status.textContent=this.busy?'Loading Inbox…':this.items.length?`${this.items.length} open Inbox items.`:'No open Inbox items.';
  for(const item of this.items){
   const entry=node('section','inbox-item');entry.dataset.itemId=String(item.id);
   const open=this.expanded.has(item.id),toggle=button(`${item.read?'':'Unread · '}${item.subject}`,()=>{
    if(open)this.expanded.delete(item.id);else this.expanded.add(item.id);
    if(!open&&!item.read)this.mutate(item,'read-state',{read:true});else this.render();
   });toggle.dataset.focusKey=`inbox-${item.id}-open`;toggle.setAttribute('aria-expanded',String(open));toggle.setAttribute('aria-controls',`inbox-body-${item.id}`);toggle.disabled=this.busy;entry.append(toggle);
   if(open){
    const body=node('div');body.id=`inbox-body-${item.id}`;
    body.append(node('p','tool-note',`${item.kind} · ${item.updated_at}`),node('pre','message-body',item.body));
    const unread=button('Mark unread',()=>{this.expanded.delete(item.id);this.mutate(item,'read-state',{read:false});});
    const reply=button('Reply in composer',()=>this.callbacks.reply?.(this.task,item));
    const dismiss=button('Dismiss',()=>this.mutate(item,'dismiss',{}));
    for(const [index,action] of [unread,reply,dismiss].entries()){action.disabled=this.busy;action.dataset.focusKey=`inbox-${item.id}-${index}`;}
    body.append(unread,reply,dismiss);entry.append(body);
   }
   panel.append(entry);
  }
  root.scrollTop=top;recover();
 }
}
