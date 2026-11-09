import {node,button} from '../shared/dom.js';
import {captureFocus} from '../shared/focus-recovery.js';
import {usageCounters} from './session-usage.js';

function counters(value){
 const list=node('dl','status-list');
 for(const [label,count] of usageCounters(value)){const row=node('div','context-row');row.append(node('dt','',label),node('dd','',count));list.append(row);}return list;
}
export class SessionUsageView {
 constructor(refresh,older){
  this.group=node('section','context-group session-usage');this.group.setAttribute('aria-label','Session tokens');this.group.tabIndex=-1;
  this.lifetime=new AbortController();
  this.refresh=button('Refresh usage',refresh);this.older=button('Older turns',older);
  this.error=node('p','context-note');this.error.setAttribute('role','alert');this.status=node('p','context-note');this.status.setAttribute('role','status');
  this.meta=node('p','context-note');this.totals=node('div');this.turns=node('div');this.rows=new Map();
  this.limit=node('p','context-note','Showing up to 500 turns. Refresh usage for the latest turns.');
  this.group.append(node('h3','','Session tokens'),this.refresh,this.error,this.status,this.meta,this.totals,this.turns,this.older,this.limit);
 }
 cancel(){this.lifetime.abort();this.recoverBusy?.();this.recoverBusy=null;this.wasBusy=false;this.lifetime=new AbortController();}
 update(data,error,busy){
  if(busy&&!this.wasBusy)this.recoverBusy=captureFocus(this.group,()=>this.refresh.hidden?this.group:this.refresh,{signal:this.lifetime.signal});
  this.wasBusy=!!busy;
  const unmeasured=data?.session_id===''&&data?.totals===null&&!error;
  this.refresh.hidden=unmeasured;
  this.refresh.disabled=!!busy;this.older.disabled=!!busy;
  this.error.hidden=!error;if(this.error.textContent!==(error||''))this.error.textContent=error||'';
  const status=unmeasured?'Session tokens are not measured for this agent yet.':data?.totals?'':busy?'Loading session tokens…':'Session token measurements unavailable.';
  this.status.hidden=!status;if(this.status.textContent!==status)this.status.textContent=status;
  const totalKey=JSON.stringify([data?.session_id,data?.measured_at,data?.totals]);
  if(this.totalKey!==totalKey){
   this.totalKey=totalKey;this.meta.textContent=data?.totals?`Session ${data.session_id}${data.measured_at?' · '+new Date(data.measured_at).toLocaleString():''}`:'';
   this.totals.replaceChildren(...(data?.totals?[counters(data.totals)]:[]));
  }
  const turnKey=JSON.stringify([data?.session_id,data?.turns]);
  if(this.turnKey!==turnKey){
   const recover=captureFocus(this.group,()=>this.refresh.hidden?this.group:this.refresh);this.turnKey=turnKey;
   if(this.session!==data?.session_id){this.rows.clear();this.turns.replaceChildren();this.session=data?.session_id;}
   const wanted=new Set();
   for(const [index,turn] of (data?.turns||[]).entries()){
    wanted.add(turn.turn_id);let row=this.rows.get(turn.turn_id);const fingerprint=JSON.stringify(turn);
    if(!row){const details=node('details'),summary=node('summary','',`Turn ${turn.turn_id}`),body=node('div');summary.dataset.focusKey=`usage-${data.session_id}-${turn.turn_id}`;details.append(summary,body);row={details,body};this.rows.set(turn.turn_id,row);}
    if(row.key!==fingerprint){row.key=fingerprint;row.body.replaceChildren(counters(turn));}
    if(this.turns.children[index]!==row.details)this.turns.insertBefore(row.details,this.turns.children[index]||null);
   }
   for(const [id,row] of this.rows)if(!wanted.has(id)){row.details.remove();this.rows.delete(id);}
   recover();
  }
  this.older.hidden=!data?.next_cursor||data.turns.length>=500;this.limit.hidden=!(data?.turns.length>=500);
  if(!busy){const recover=this.recoverBusy;this.recoverBusy=null;recover?.();}
  return this.group;
 }
}
