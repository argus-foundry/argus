const writeEvents=new Set(['session.idle','session.needs_input','session.exited','task.status_changed']);

export class ToolRefresh {
  constructor({visible,read,apply,failed,log=()=>{},timers=globalThis}) {
    Object.assign(this,{visible,read,apply,failed,log,timers,generation:0});
  }
  select(task,tool) {
    this.stop();this.task=task;this.tool=tool;
    if(!task||!['changes','artifacts'].includes(tool))return;
    this.timer=this.timers.setInterval(()=>this.trigger('poll'),10000);
    this.trigger('selection');
  }
  event(event) {
    if(event.type==='resync'||event.task_id===this.task&&writeEvents.has(event.type))this.trigger(event.type);
  }
  async trigger(reason) {
    if(!this.task||!['changes','artifacts'].includes(this.tool))return;
    if(!this.visible()){this.log('skipped hidden '+reason);return;}
    if(this.flight){this.pending=true;this.log('skipped in-flight '+reason);return;}
    const generation=this.generation,abort=new AbortController();this.flight=abort;
    this.log('fetch '+reason);
    try {
      const result=await this.read(this.task,this.tool,abort.signal);
      if(generation===this.generation&&!abort.signal.aborted)this.apply(result);
    } catch(error) {
      if(generation===this.generation&&!abort.signal.aborted){this.failed(error);this.log('fetch failed',{status:error.status||0});}
    } finally {
      if(generation===this.generation){this.flight=null;const pending=this.pending;this.pending=false;if(pending)this.trigger('coalesced');}
    }
  }
  stop() {
    this.generation++;this.flight?.abort();this.flight=null;this.pending=false;
    this.timers.clearInterval(this.timer);this.timer=null;this.task=null;
  }
}
