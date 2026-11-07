import {node,button} from '../../shared/dom.js';
import {log} from '../../core/api.js';
import {settingsSignal} from '../settings-fields.js';
import {systemMetrics} from './settings-system-policy.js';

export class SystemSettings {
  constructor(context) {
    Object.assign(this,context,{active:true,version:0});
    const heading=node('div','settings-section-heading');heading.append(node('h2','','System'),button('Refresh metrics',()=>this.load()));
    this.warning=node('section','settings-sleep-warning');this.warning.hidden=true;
    this.reading=node('p','settings-note');this.metrics=node('div','settings-metrics');
    this.host.replaceChildren(heading,this.reading,this.warning,this.metrics);
    this.visibility=()=>{this.stop();if(this.visible())this.load();};
    document.addEventListener('visibilitychange',this.visibility);this.load();
  }
  visible(){return this.active && !document.hidden && this.host.isConnected && !this.host.closest('[hidden]') && this.host.getClientRects().length>0;}
  stop(){clearTimeout(this.timer);this.controller?.abort();this.version++;}
  async load() {
    this.stop();if(!this.visible()){log('settings-system','poll skipped; section hidden');return;}
    const controller=this.controller=new AbortController(),version=this.version;
    try {
      const result=await this.api.json('/api/system-metrics',{signal:settingsSignal(controller.signal)});
      if(!this.active || controller.signal.aborted || version!==this.version)return;
      this.render(result);this.status('');log('settings-system','metrics loaded',{count:1});
    }catch(error){
      if(!controller.signal.aborted && this.active){this.status(`Metrics refresh failed. The last reading may be stale. ${error.message}`,true);this.reading.textContent='Refresh failed';}
      if(!controller.signal.aborted)log('settings-system','refresh failed',{status:error.status});
    }finally{if(this.visible() && version===this.version)this.timer=setTimeout(()=>this.load(),2000);}
  }
  render(m) {
    this.reading.textContent=m.sampled_at && Number.isFinite(Date.parse(m.sampled_at)) && Date.parse(m.sampled_at)>0?`Sampled ${new Date(m.sampled_at).toLocaleString()}`:'Latest available reading';
    this.metrics.replaceChildren();
    for(const metric of systemMetrics(m)) {
      const group=node('div','settings-metric'),row=node('div','settings-metric-row');row.append(node('span','',metric.label),node('span','',metric.value));group.append(row);
      if(metric.percent!==null){const bar=node('progress');bar.max=100;bar.value=metric.percent;bar.setAttribute('aria-label',metric.label);bar.dataset.level=metric.percent>=90?'critical':metric.percent>=75?'warning':'normal';group.append(bar);}
      if(metric.label==='Disk' && m.disk_path)group.append(node('p','settings-note',m.disk_path));
      this.metrics.append(group);
    }
    const sleep=m.host_sleep || {};this.warning.hidden=sleep.status!=='at_risk';this.warning.replaceChildren();
    if(!this.warning.hidden) {
      this.warning.append(node('h3','','Host may sleep'),node('p','settings-note',sleep.message || ''));
      if(sleep.fix)this.warning.append(node('p','settings-note','Permanent fix'),node('code','',sleep.fix));
      this.warning.append(node('p','settings-note','Until daemon restart'),node('code','','caffeinate -is -w $(cat ~/.argus/daemon.pid)'));
    }
  }
  destroy(){this.active=false;this.stop();document.removeEventListener('visibilitychange',this.visibility);}
}
