import {readHierarchy} from './rail/task-hierarchy.js';
import {log} from '../core/api.js';

export class WorkspaceRoster {
  constructor(api,enabled,apply){Object.assign(this,{api,enabled,apply,version:0});document.addEventListener('visibilitychange',()=>this.refresh());}
  start(){this.stop();this.running=true;this.schedule();}
  schedule(){if(this.running)this.timer=setTimeout(()=>this.refresh(),5000);}
  async refresh(){
    if(!this.running)return;
    clearTimeout(this.timer);this.abort?.abort();const version=++this.version;
    if(!this.enabled()||document.hidden){this.schedule();return;}
    const abort=this.abort=new AbortController();
    try{const result=await readHierarchy(this.api,AbortSignal.any([abort.signal,AbortSignal.timeout(10000)]));if(!this.running||version!==this.version)return;this.apply(result);log('roster','refreshed',{count:result.roster?.orchestrators?.length||0});}
    catch(error){if(!abort.signal.aborted)log('roster','refresh failed',{status:error.status||0});}
    finally{if(this.running&&version===this.version)this.schedule();}
  }
  stop(){this.running=false;this.version++;clearTimeout(this.timer);this.abort?.abort();}
}
