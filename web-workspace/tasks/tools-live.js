const writeEvents=new Set(['session.idle','session.needs_input','session.exited','task.status_changed']);
const staleDelays=[250,1000,3000];

export class ToolRefresh {
  constructor({visible,read,apply,failed,log=()=>{},timers=globalThis}) {
    Object.assign(this,{visible,read,apply,failed,log,timers,generation:0});
  }
  select(task,tool) {
    this.stop();this.task=task;this.tool=tool;
    if(!task||!['changes','artifacts'].includes(tool))return;
    if(tool==='artifacts')this.timer=this.timers.setInterval(()=>this.trigger('poll'),10000);
    this.trigger('selection');
  }
  event(event) {
    if(event.type==='worktree.changed') {
      if(event.task_id!==this.task||this.tool!=='changes')return;
      const revision=event.payload?.revision;
      if(revision&&(revision===this.eventRevision||revision===this.revision&&!this.stale))return;
      this.eventRevision=revision;
    } else if(event.type!=='resync'&&!(event.task_id===this.task&&writeEvents.has(event.type)))return;
    this.invalidation++;this.trigger(event.type);
  }
  async trigger(reason) {
    if(!this.task||!['changes','artifacts'].includes(this.tool))return;
    if(reason!=='stale'){this.staleAttempt=0;this.timers.clearTimeout(this.retry);this.retry=null;}
    if(!this.visible()){this.log('skipped hidden '+reason);return;}
    if(this.flight){this.pending=true;this.log('skipped in-flight '+reason);return;}
    const generation=this.generation,invalidation=this.invalidation,abort=new AbortController();this.flight=abort;
    this.log('fetch '+reason);
    try {
      const result=await this.read(this.task,this.tool,abort.signal);
      if(generation!==this.generation||abort.signal.aborted)return;
      if(invalidation!==this.invalidation){this.pending=true;this.log('skipped invalidated snapshot');return;}
      this.revision=result.git_revision;this.eventRevision=null;this.stale=!!result.stale;
      this.apply(result);this.log('fetched',{stale:this.stale});
      if(this.tool==='changes'&&this.stale){
        const delay=staleDelays[this.staleAttempt++];
        if(delay!==undefined)this.retry=this.timers.setTimeout(()=>{this.retry=null;this.trigger('stale');},delay);
        else this.failed(new Error('Git snapshot is stale. Retry to check the latest changes.'));
      }else this.staleAttempt=0;
    } catch(error) {
      if(generation===this.generation&&!abort.signal.aborted){this.eventRevision=null;this.failed(error);this.log('fetch failed',{status:error.status||0});}
    } finally {
      if(generation===this.generation){this.flight=null;const pending=this.pending;this.pending=false;if(pending)this.trigger('coalesced');}
    }
  }
  stop() {
    this.generation++;this.flight?.abort();this.flight=null;this.pending=false;
    this.timers.clearInterval(this.timer);this.timers.clearTimeout(this.retry);this.timer=null;this.retry=null;this.task=null;
    this.invalidation=0;this.revision=null;this.eventRevision=null;this.stale=false;this.staleAttempt=0;
  }
}
