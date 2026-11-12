import {kanbanStatuses,kanbanLabel} from './hera-kanban.js';
import {copyNotice} from '../../shared/clipboard.js';
import {rowMenu} from '../../shared/row-menu.js';

export function taskMenu(task, {open,changed,settings,quickAction,pinKnown=true,coordinator,heraAction,heraPending=false,deleteTask,reveal}) {
  const entries=[];
  const entry=(label,action,run,disabled=false)=>{
    const icon=action.startsWith('kanban-')?action.slice(7):action.includes('pin')?(label==='Unpin'?'unpin':'pin'):
      {settings:'settings','copy-id':'copy','copy-branch':'branch','copy-worktree':'folder'}[action];
    entries.push({label,run,disabled,icon,dataset:{taskId:task.id,taskAction:action}});
  };
  entry('Task settings','settings',()=>settings(task));
  if(coordinator&&heraAction&&!task.archived){
    entry(coordinator.pinned?'Unpin':'Pin','hera-pin',()=>heraAction('pin'),heraPending);
    if(coordinator.root)for(const status of kanbanStatuses)entry(`Move to ${kanbanLabel(status)}`,`kanban-${status}`,()=>heraAction('kanban',status),heraPending||status===(coordinator.kanban_status||'active'));
  }
  if(!coordinator&&quickAction&&!task.archived)entry(task.pinned?'Unpin':'Pin','pin',()=>quickAction(task,'pin'),!pinKnown);
  entry('Copy task ID','copy-id',()=>copyNotice(task.id));
  if(task.branch)entry('Copy branch','copy-branch',()=>copyNotice(task.branch));
  if(task.worktree_path)entry('Copy worktree path','copy-worktree',()=>copyNotice(task.worktree_path));
  if(reveal&&task.worktree_path)entries.push({label:'Open in Finder',icon:'folder',disabled:reveal.disabled,dataset:{hostReveal:'true',taskId:task.id,taskAction:'reveal'},run:()=>reveal.open(task)});
  if(deleteTask)entries.push({label:'Delete',icon:'delete',run:()=>deleteTask(task),danger:true,dataset:{taskId:task.id,taskAction:'delete'}});
  const [more,menu]=rowMenu({label:`Task actions for ${task.name}`,open,changed,entries});
  more.dataset.tooltip=more.getAttribute('aria-label');more.removeAttribute('title');
  more.dataset.taskId=task.id;more.dataset.taskAction='more';
  return [more,menu];
}
