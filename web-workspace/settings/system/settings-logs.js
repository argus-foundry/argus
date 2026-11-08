import {node,button} from '../../shared/dom.js';
import {select,settingsSignal} from '../settings-fields.js';
import {log} from '../../core/api.js';

export class LogSettings {
  constructor(context) {
    Object.assign(this,context,{active:true,version:0});
    const heading=node('div','settings-section-heading');heading.append(node('h2','','Logs'),button('Refresh log',()=>this.load()));
    const controls=node('div','settings-log-controls');this.selector=select(controls,'Log file','logFile',[['ux','UX'],['daemon','Daemon']],'ux');
    this.pane=node('pre','settings-log-pane');this.pane.tabIndex=0;this.pane.setAttribute('aria-label','Log contents');this.pane.setAttribute('role','region');
    this.selector.addEventListener('change',()=>this.load());
    this.host.replaceChildren(heading,controls,node('p','settings-note','Last 64 KiB. Refresh to read new entries.'),this.pane);this.load();
  }
  async load() {
    this.controller?.abort();const controller=this.controller=new AbortController(),version=++this.version,name=this.selector.value;
    if(!this.active || !['ux','daemon'].includes(name))return;
    this.pane.textContent='Loading log…';this.status('');
    try {
      const response=await this.api.request(`/api/logs/${name}?bytes=65536`,{signal:settingsSignal(controller.signal)});
      const text=await response.text();
      if(!this.active || controller.signal.aborted || version!==this.version){log('settings-logs','obsolete read ignored',{stale:true});return;}
      this.pane.textContent=text || '(empty)';this.pane.scrollTop=this.pane.scrollHeight;
      log('settings-logs','loaded',{bytes:new TextEncoder().encode(text).length});
    }catch(error){
      if(!this.active || controller.signal.aborted || version!==this.version)return;
      this.pane.textContent='Log unavailable. Use Refresh log to retry.';this.status(`Log read failed. ${error.message}`,true);log('settings-logs','load failed',{status:error.status});
    }
  }
  destroy(){this.active=false;this.version++;this.controller?.abort();}
}
