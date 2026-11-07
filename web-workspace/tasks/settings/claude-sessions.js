import {captureFocus} from '../../shared/focus-recovery.js';
import { node, button } from '../../shared/dom.js';
import { log } from '../../core/api.js';

export class ClaudeSessions {
  constructor(api, changed) {
    Object.assign(this,{api,changed,generation:0});
  }
  select(task) {if(this.task?.id===task?.id)return;this.clear();this.task=task;}
  async open(task) {
    if(!task)return;
    this.close();const generation=++this.generation;task={...task}; this.abort=new AbortController();
    const dialog=this.dialog=node('dialog');dialog.id='claude-sessions-dialog';dialog.setAttribute('aria-label','Switch Claude session');
    const content=node('div'),status=node('p','','Loading sessions…'),close=button('Cancel',()=>this.close());
    status.setAttribute('role','status');
    dialog.append(node('h2','','Switch Claude session'),node('p','',task.name),status,content,close);
    dialog.addEventListener('cancel',event=>{event.preventDefault();if(!this.busy)this.close();});
    document.body.append(dialog);dialog.showModal();
    const current=()=>generation===this.generation&&!this.abort.signal.aborted&&this.dialog===dialog;
    try{
      const result=await this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/claude-sessions`,{signal:AbortSignal.any([this.abort.signal,AbortSignal.timeout(15000)])});
      if(!current())return;
      status.textContent=result.sessions?.length?'':'No sessions found.';
      for(const session of result.sessions||[]){
        const selected=session.id===result.current_session_id;
        const row=button('',async()=>{
          if(this.busy||!current())return;
          const restore=captureFocus(dialog,()=>close,{signal:this.abort.signal});
          this.busy=true;close.disabled=true;for(const control of content.querySelectorAll('button'))control.disabled=true;
          status.textContent='Switching session…';
          try{
            const response=await this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/claude-session`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({session_id:session.id}),signal:AbortSignal.any([this.abort.signal,AbortSignal.timeout(120000)])});
            if(!current())return;
            log('claude-sessions','switched');this.close();await this.changed(task,response.status);
          }catch(error){if(!current())return;status.textContent=`Switch was not confirmed. Check the task before retrying. ${error.message}`;log('claude-sessions','switch failed');}
          finally{if(current()){this.busy=false;close.disabled=false;for(const control of content.querySelectorAll('button'))control.disabled=control.dataset.current==='true';}restore();}
        },'file-row');
        row.dataset.current=String(selected);row.disabled=selected;
        const date=new Date(session.mod_time);
        row.append(node('strong','',session.title||session.id),node('span','tool-note',[selected?'Current session':'',Number.isNaN(date.getTime())?'':date.toLocaleString(),session.branch,session.pr_ref].filter(Boolean).join(' · ')));
        content.append(row);
      }
      log('claude-sessions','loaded',{count:result.sessions?.length||0});
    }catch(error){if(current()){status.textContent=error.status===400?'Session switching is unavailable for this backend.':`Sessions unavailable. ${error.message}`;content.append(button('Retry',()=>this.open(task)));log('claude-sessions','load failed');}}
  }
  close(){this.generation++;this.abort?.abort();this.dialog?.close();this.dialog?.remove();this.dialog=null;this.busy=false;}
  clear(){this.close();this.task=null;}
}
