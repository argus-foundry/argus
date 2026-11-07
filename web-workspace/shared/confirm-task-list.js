import {node} from './dom.js';
import {safeTaskLinks} from '../tasks/task-information.js';
import {deleteEffects} from '../tasks/task-delete-policy.js';

export function taskWarnings(task,roster={},links=new Map()){
 const roles=(roster.orchestrators||[]).flatMap(orch=>(orch.roles||[]).filter(role=>role.live&&role.task_id===task.id).map(role=>`${role.name||'Agent'} in ${orch.name||`team ${orch.id}`}`));
 roles.push(...(roster.freelance||[]).filter(role=>role.live&&role.task_id===task.id).map(role=>`${role.name||'Agent'} attached to a team`));
 const pr=['draft','awaiting-review','changes-requested','approved'].includes(task.pr_state);
 const url=safeTaskLinks([{url:links.get(task.id)}])[0]?.url;
 return {roles,pr,url,attention:!!task.live||roles.length>0||pr};
}
export function confirmTaskList(tasks,{action,roster,links,enrich,signal}){
 const body=node('div','task-confirm-body'),attention=node('section','task-attention'),ordinary=node('section','task-ordinary');
 const flagged=tasks.filter(task=>taskWarnings(task,roster,links).attention),rest=tasks.filter(task=>!taskWarnings(task,roster,links).attention);
 attention.append(node('h3','',`Needs attention (${flagged.length})`));const important=node('div','task-confirm-scroll');attention.append(important);
 ordinary.append(node('h3','',`Everything else (${rest.length})`));const disclosure=node('details','task-show-list');disclosure.append(node('summary','','Show list'));const others=node('div','task-confirm-scroll');disclosure.append(others);ordinary.append(disclosure);disclosure.open=tasks.length===1;
 const reads=[];
 function entry(task){
  const item=node('section','delete-task-review');item.dataset.reviewTaskId=task.id;
  item.append(node('h4','',task.name||task.id));
  const warnings=taskWarnings(task,roster,links);
  if(task.live)item.append(node('p','','Live session'));
  if(warnings.roles.length)item.append(node('p','',`Team agents ${warnings.roles.join(', ')}`));
  if(warnings.pr)item.append(node('p','',`PR ${task.pr_state}${warnings.url?'':' (link unavailable)'}`));
  if(warnings.url&&!(tasks.length===1&&enrich)){const link=node('a','','Pull request');link.href=warnings.url;link.target='_blank';link.rel='noopener noreferrer';item.append(link);}
  const facts=node('div','task-confirm-facts');facts.append(node('p','delete-task-facts',`Task ID ${task.id}\nProject ${task.project||'Unknown'}\nBranch ${task.branch||'Unavailable'}\nWorktree ${task.worktree_path||'Unavailable'}`));item.append(facts);
  if(tasks.length===1&&enrich)reads.push(enrich(task,facts,signal));
  return item;
 }
 for(const task of flagged)important.append(entry(task));
 for(const task of rest)others.append(entry(task));
 body.append(attention,ordinary);
 if(action==='delete'){const effects=node('details','task-effects');effects.append(node('summary','','What will be deleted'),node('p','',deleteEffects));body.append(effects);}
 return {body,ready:Promise.all(reads)};
}
