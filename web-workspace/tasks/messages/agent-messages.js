import {node,button} from '../../shared/dom.js';
import {captureFocus} from '../../shared/focus-recovery.js';
import {log} from '../../core/api.js';

export class AgentMessages {
 constructor(api,panel){Object.assign(this,{api,panel,generation:0,messages:[],expanded:new Set()});}
 select(task){this.destroy();this.task=task;this.messages=[];this.cursor=null;this.error=null;this.expanded.clear();}
 destroy(){this.generation++;this.abort?.abort();this.task=null;this.flight=null;this.busy=false;}
 refresh(){return this.read(false);}
 older(){return this.read(true);}
 read(older){
  if(!this.task||this.flight||older&&!this.cursor)return this.flight||Promise.resolve();
  const generation=this.generation,task=this.task,abort=this.abort=new AbortController();const recover=captureFocus(this.panel,()=>this.panel?.querySelector('button'),{signal:abort.signal});this.busy=true;this.render();
  const query=new URLSearchParams({limit:'100'});if(older)query.set('before_id',this.cursor);
  const flight=(async()=>{
   try{
    const result=await this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/hera-messages?${query}`,{signal:AbortSignal.any([abort.signal,AbortSignal.timeout(10000)])});
    if(generation!==this.generation||abort.signal.aborted)return;
    const messages=older?[...(result.messages||[]),...this.messages]:result.messages||[];
    this.messages=[...new Map(messages.map(message=>[`${message.source}:${message.id}`,message])).values()].slice(-1000);this.cursor=result.next_before_id;this.error=null;const retained=new Set(this.messages.map(message=>`${message.source}:${message.id}`));for(const key of this.expanded)if(!retained.has(key))this.expanded.delete(key);
    log('agent-messages','loaded',{count:this.messages.length});
   }catch(error){if(generation!==this.generation||abort.signal.aborted)return;this.error=error.status===401||error.status===403?'Token expired. Reconnect to read messages.':error.status===404?'Task unavailable. Select another task.':'Messages unavailable. Retry Refresh.';log('agent-messages','read failed',{status:error.status||0});}
   finally{if(generation===this.generation){this.busy=false;this.flight=null;this.render(older);recover();}}
  })();this.flight=flight;return flight;
 }
 render(older=false){
  if(!this.panel)return;const root=this.panel,height=root.scrollHeight,top=root.scrollTop;
  this.content ||= node('div');this.status ||= node('p','tool-note');if(!this.content.isConnected)root.replaceChildren(this.status,this.content);const panel=this.content;
  const anchor=older?[...panel.querySelectorAll('.agent-message')].find(message=>message.getBoundingClientRect().bottom>root.getBoundingClientRect().top):null;
  const anchorKey=anchor?.dataset.messageKey,anchorTop=anchor?.getBoundingClientRect().top;
  const recover=captureFocus(root,()=>panel.querySelector('[data-action="older"]')||panel.querySelector('button'));
  panel.replaceChildren(node('h2','','Agent messages'));
  const refresh=button('Refresh',()=>this.refresh());refresh.disabled=this.busy;panel.append(refresh);
  if(this.error){const error=node('p','tool-note',this.error);error.setAttribute('role','alert');panel.append(error);}
  this.status ||= node('p','tool-note');this.status.setAttribute('role','status');this.status.setAttribute('aria-live','polite');this.status.textContent=this.busy?'Loading agent messages…':this.messages.length?`${this.messages.length} agent messages.`:'No agent messages.';
  for(const message of this.messages){
   const details=node('details','agent-message');details.dataset.messageKey=`${message.source}:${message.id}`;
   details.open=this.expanded.has(details.dataset.messageKey);
   details.addEventListener('toggle',()=>{if(details.open&&this.messages.some(message=>`${message.source}:${message.id}`===details.dataset.messageKey))this.expanded.add(details.dataset.messageKey);else this.expanded.delete(details.dataset.messageKey);});
   details.append(node('summary','',`${message.from_name||'(deleted role)'} → ${message.to_name||'(deleted role)'} · ${message.tldr||'Message'}`),node('p','tool-note',`${message.sent_at||''} · ${message.read_at?'Read':'Unread'} · ${message.delivery_mode||'Delivery unknown'}${message.delivered_at?' · Delivered '+message.delivered_at:''}`),node('pre','message-body',message.body||''));panel.append(details);
  }
  if(this.cursor&&this.messages.length<1000){const more=button('Older messages',()=>this.older());more.dataset.action='older';more.disabled=this.busy;panel.append(more);}
  if(this.messages.length>=1000)panel.append(node('p','tool-note','Showing up to 1,000 messages. Refresh for the latest history.'));
  root.scrollTop=top;
  if(older){const retained=[...panel.querySelectorAll('.agent-message')].find(message=>message.dataset.messageKey===anchorKey);const desired=retained?top+retained.getBoundingClientRect().top-anchorTop:top+root.scrollHeight-height;
   const gap=desired-(root.scrollHeight-root.clientHeight);if(gap>0){const spacer=node('div');spacer.setAttribute('aria-hidden','true');spacer.style.height=`${gap}px`;spacer.style.flexShrink='0';panel.append(spacer);}
   root.scrollTop=desired;}
  recover();
 }
}
