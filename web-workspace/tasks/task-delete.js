import {confirmDialog} from '../shared/confirm-dialog.js';
import {node} from '../shared/dom.js';
import {log} from '../core/api.js';
import {resolvePR} from './task-information.js';
import {deleteTaskFacts} from './task-delete-policy.js';

export class TaskDelete {
 constructor(api,onDeleted,roster=()=>({}),focus=()=>{},links=()=>new Map()) { Object.assign(this,{api,onDeleted,roster,focus,links}); }
 open(task,options={}) {
  if(!task||this.busy)return;
  this.clear();this.abort=new AbortController();
  const owner=this.abort,signal=owner.signal,captured={...task},origin=document.activeElement,cancelExternal=()=>owner.abort();let completed=false;
  options.signal?.addEventListener('abort',cancelExternal,{once:true});if(options.signal?.aborted)owner.abort();
  this.result=confirmDialog({id:options.id||'delete-dialog',title:'Delete 1 task permanently',message:'This cannot be undone. Task data, sessions and worktrees will be removed.',tasks:[captured],keepFailuresOpen:true,taskAction:'delete',roster:this.roster(),links:this.links(),enrich:(task,host)=>this.review(task,host,signal),restoreFocus:()=>{if(!completed&&origin?.isConnected&&origin.getClientRects().length)origin.focus({preventScroll:true});else this.focus(captured);},confirmLabel:options.confirmLabel||'Delete',signal,request:(path,options)=>this.api.json(path,options),onBusy:busy=>{if(this.abort===owner)this.busy=busy;},onTaskSuccess:async task=>{
   if(!signal.aborted){completed=true;options.signal?.removeEventListener('abort',cancelExternal);await this.onDeleted(task,options);log('task-delete','deleted',{count:1});}
  }});
  this.result.then(()=>{options.signal?.removeEventListener('abort',cancelExternal);if(this.abort===owner)this.abort.abort();});
  return this.result;
 }
 async review(task,host,signal) {
  const roster=this.roster(),roles=[...(roster.orchestrators||[]).flatMap(orch=>orch.roles||[]),...(roster.freelance||[])].filter(role=>role.task_id===task.id&&role.live);
  const facts=node('p','delete-task-facts',deleteTaskFacts(task,roles));host.replaceChildren(facts);
  try {
   const base=`/api/tasks/${encodeURIComponent(task.id)}`;
   const reads=await Promise.allSettled(['/git/status','/links','/meta?namespace=pr'].map(path=>this.api.json(base+path,{signal:AbortSignal.any([signal,AbortSignal.timeout(5000)])})));
   if(signal.aborted)return;
   const [git,links,meta]=reads.map(read=>read.status==='fulfilled'?read.value:null);
   facts.textContent=deleteTaskFacts(task,roles,git);
   const url=resolvePR(meta?.entries||[],links?.links||[]);
   if(url){const link=node('a','','Pull request');link.href=url;link.target='_blank';link.rel='noopener noreferrer';host.append(link);}
   if(reads.some(read=>read.status==='rejected'))log('task-delete','review partially unavailable',{status:false});
   else log('task-delete','review loaded',{count:1});
  }catch(error){if(!signal.aborted)log('task-delete','review failed',{status:error.status||0});}
 }
 clear(){this.abort?.abort();}
}
