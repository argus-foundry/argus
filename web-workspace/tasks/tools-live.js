const writeEvents=new Set(['session.idle','session.needs_input','session.exited','task.status_changed']);
const staleDelays=[250,1000,3000],degradedDelay=15000;
const tools=new Set(['changes','artifacts','diff']);

export class ToolRefresh {
  constructor({visible,read,apply,failed,log=()=>{},timers=globalThis}) {
    Object.assign(this,{visible,read,apply,failed,log,timers,generation:0});
  }
  select(task,tool) {
    this.stop();this.task=task;this.tool=tool;
    if(!task||!tools.has(tool))return;
    if(tool==='artifacts')this.timer=this.timers.setInterval(()=>this.trigger('poll'),10000);
    return this.trigger('selection');
  }
  event(event) {
    let revision;
    if(event.type==='worktree.changed') {
      if(event.task_id!==this.task||!['changes','diff'].includes(this.tool))return;
      revision=event.payload?.revision;
      if(revision&&revision===this.expectedRevision&&!this.stale&&!this.degraded)return;
    } else if(event.type!=='resync'&&!(event.task_id===this.task&&writeEvents.has(event.type)))return;
    return this.trigger(event.type,revision);
  }
  trigger(reason,revision) {
    if(!this.task||!tools.has(this.tool))return;
    this.expectedRevision=revision;this.invalidation++;this.staleAttempt=0;this.suspend();
    return this.fetch(reason);
  }
  async fetch(reason) {
    if(!this.task||!tools.has(this.tool))return;
    if(!this.visible()){this.log('skipped hidden '+reason);return;}
    if(this.flight){this.pending=true;this.log('skipped in-flight '+reason);return;}
    const generation=this.generation,invalidation=this.invalidation,expected=this.expectedRevision,abort=new AbortController();this.flight=abort;
    this.log('fetch '+reason);
    try {
      const result=await this.read(this.task,this.tool,abort.signal);
      if(generation!==this.generation||abort.signal.aborted)return;
      if(invalidation!==this.invalidation){this.pending=true;this.log('skipped invalidated snapshot');return;}
      if(!this.visible()){this.log('skipped hidden snapshot');return;}
      this.revision=result.git_revision??expected;this.expectedRevision=this.revision;this.stale=!!result.stale;this.degraded=!!result.watch_degraded;
      this.apply(result);this.log('fetched',{stale:this.stale});
      if(this.tool==='changes'&&this.stale){
        const delay=staleDelays[this.staleAttempt++];
        if(delay!==undefined)this.retry=this.timers.setTimeout(()=>{this.retry=null;this.fetch('stale');},delay);
        else this.failed(new Error('Git snapshot is stale. Retry to check the latest changes.'));
      }else this.staleAttempt=0;
      this.scheduleReconcile();
    } catch(error) {
      if(generation===this.generation&&!abort.signal.aborted){if(invalidation!==this.invalidation){this.pending=true;this.log('skipped invalidated failure');}else{this.expectedRevision=null;this.failed(error);this.log('fetch failed',{status:error.status||0});this.scheduleReconcile();}}
    } finally {
      if(generation===this.generation){this.flight=null;const pending=this.pending;this.pending=false;if(pending)this.fetch('coalesced');}
    }
  }
  scheduleReconcile() {if(this.tool==='changes'&&this.degraded&&this.visible()&&!this.retry&&!this.reconcile)this.reconcile=this.timers.setTimeout(()=>{this.reconcile=null;this.trigger('degraded');},degradedDelay);}
  suspend() {this.timers.clearTimeout(this.retry);this.timers.clearTimeout(this.reconcile);this.retry=null;this.reconcile=null;}
  stop() {
    this.generation++;this.flight?.abort();this.flight=null;this.pending=false;this.suspend();
    this.timers.clearInterval(this.timer);this.timer=null;this.task=null;
    this.invalidation=0;this.revision=null;this.expectedRevision=null;this.stale=false;this.degraded=false;this.staleAttempt=0;
  }
}
