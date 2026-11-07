import {copyNotice} from '../shared/clipboard.js';
import {node,button} from '../shared/dom.js';
import {safeTaskLinks,resolvePR} from './task-information.js';
import {log} from '../core/api.js';
export function renderTaskLinks(host,links){
 for(const link of safeTaskLinks(links)){const control=node('a','',link.label||link.url);control.href=link.url;control.target='_blank';control.rel='noopener noreferrer';host.append(control,document.createTextNode(' '));}
}
export class TaskHeaderLinks {
 constructor(api){this.api=api;this.urls=new Map();this.host=node('div','task-header-links');document.getElementById('task-title').after(this.host);this.generation=0;}
 async select(task){
  if(!task){this.clear();return;}
  const sameSelection=this.task?.id===task?.id&&this.task?.branch===task?.branch;
  if(sameSelection&&this.prState===task?.pr_state&&(this.prLoaded||this.pending))return;
  if(!sameSelection){
   this.clear();this.task=task;
   this.selectionAbort=new AbortController();
   const selectionSignal=this.selectionAbort.signal,signal=AbortSignal.any([selectionSignal,AbortSignal.timeout(10000)]);
   this.linksRead=this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/links`,{signal}).catch(error=>{if(selectionSignal.aborted||[401,403].includes(error.status))throw error;log('task-links','output links unavailable',{status:error.status||0});return {};});
   if(task.branch){const copy=button(task.branch,()=>copyNotice(task.branch,'Copied branch.'));copy.dataset.tooltip='Copy branch';copy.setAttribute('aria-label',`Copy branch ${task.branch}`);this.host.append(copy);}
   this.links=node('span');this.host.append(this.links);
  }
  this.prState=task.pr_state;this.prLoaded=false;this.pending=true;
  this.abort?.abort();this.abort=new AbortController();
  const generation=++this.generation,links=this.links;
  try{
   const signal=AbortSignal.any([this.abort.signal,AbortSignal.timeout(10000)]);
   const [result,meta]=await Promise.all([this.linksRead,this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/meta?namespace=pr`,{signal})]);
   if(generation!==this.generation)return;
   links.replaceChildren();links.removeAttribute('role');
   const url=resolvePR(meta.entries||[],result.links||[]);
   if(url)this.urls.set(task.id,url);else this.urls.delete(task.id);
   if(url){renderTaskLinks(links,[{url,label:'PR ↗'}]);const link=links.querySelector('a');link.dataset.tooltip='Open pull request';link.setAttribute('aria-label','Open pull request');}
   this.prLoaded=true;
   log('task-links','loaded',{count:result.links?.length||0});
  }
  catch(error){if(generation===this.generation){links.setAttribute('role','alert');links.textContent='Links unavailable';log('task-links','load failed',{status:error.status||0});}}
  finally{if(generation===this.generation)this.pending=false;}
 }
 clear(){this.generation++;this.abort?.abort();this.selectionAbort?.abort();this.task=null;this.prState=undefined;this.prLoaded=false;this.pending=false;this.linksRead=null;this.host.replaceChildren();}
}
