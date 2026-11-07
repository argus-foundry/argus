import {copyNotice} from '../../shared/clipboard.js';
import {rowMenu} from '../../shared/row-menu.js';

export function taskMenu(task, {open,changed,settings,quickAction,pinKnown=true}) {
  const entries=[];
  const entry=(label,action,run,disabled=false)=>entries.push({label,run,disabled,dataset:{taskId:task.id,taskAction:action}});
  entry('Task settings','settings',()=>settings(task));
  if(quickAction&&!task.archived)entry(task.pinned?'Unpin':'Pin','pin',()=>quickAction(task,'pin'),!pinKnown);
  entry('Copy task ID','copy-id',()=>copyNotice(task.id));
  if(task.branch)entry('Copy branch','copy-branch',()=>copyNotice(task.branch));
  if(task.worktree_path)entry('Copy worktree path','copy-worktree',()=>copyNotice(task.worktree_path));
  const [more,menu]=rowMenu({label:`Task actions for ${task.name}`,open,changed,entries});
  more.dataset.tooltip=more.getAttribute('aria-label');more.removeAttribute('title');
  more.dataset.taskId=task.id;more.dataset.taskAction='more';
  return [more,menu];
}
