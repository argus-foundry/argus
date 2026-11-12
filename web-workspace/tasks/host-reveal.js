import {confirmDialog} from '../shared/confirm-dialog.js';
import {notice} from '../shared/dom.js';
import {log} from '../core/api.js';

export class HostReveal {
  constructor(api,{notify=notice,confirm=confirmDialog,denied=()=>{}}={}) {
    Object.assign(this,{api,notify,confirm,denied});this.abort=new AbortController();this.disabled=false;
  }
  open(task,path,viewSignal) {
    if(this.disabled){log('host-reveal','skipped master-only');return Promise.resolve(false);}
    const id=task.id,signal=viewSignal?AbortSignal.any([viewSignal,this.abort.signal]):this.abort.signal;
    const body=path===undefined?{}:{path};
    return this.confirm({title:path===undefined?'Open worktree in Finder?':'Reveal file in Finder?',message:`${path??task.worktree_path??'This task worktree'}\nFinder opens on the daemon host, which may be a different computer from this browser.`,confirmLabel:'Open Finder',signal,onConfirm:()=>this.request(id,body,signal)});
  }
  async request(id,body,signal) {
    signal.throwIfAborted();
    if(this.disabled)throw new Error('A master token is required to open Finder on the daemon host. Reconnect with that token.');
    try {
      const response=await this.api.request(`/api/tasks/${encodeURIComponent(id)}/reveal`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body),signal,allowErrorResponse:true});
      const result=await response.json();signal.throwIfAborted();
      if(response.status===403){this.disabled=true;this.denied();}
      const errors={403:'A master token is required to open Finder on the daemon host. Reconnect with that token.',501:'Finder is unavailable because the daemon host does not support macOS Finder.',404:result.error==='task_not_found'?'The task or its worktree is unavailable. Refresh tasks.':'This path no longer exists on the daemon host.',400:'The path is invalid or cannot be revealed safely.',500:'The daemon host could not open Finder. Check Finder on that computer.'};
      if(!response.ok)throw Object.assign(new Error(errors[response.status]||`Finder launch was not confirmed (${response.status}). Check the daemon host before trying again.`),{status:response.status});
      if(result.status!=='opened'||result.host!=='daemon')throw new Error('Finder launch was not confirmed. Check the daemon host before trying again.');
      this.notify('Finder opened on the daemon host.');log('host-reveal','opened',{status:response.status});
    }catch(error){log('host-reveal','failed',{status:error.status||0});throw error;}
  }
  clear(){this.abort.abort();this.abort=new AbortController();this.disabled=false;}
}
