import {copyNotice} from '../../shared/clipboard.js';
import { $ , notice } from '../../shared/dom.js';

export function workspaceShortcutAction(action, dependencies) {
  const { state, api, layout, navigation, settings, newTask, tools, railActions, archive, taskSettings, selectTask, selectTab, composer }=dependencies;
  const {split,currentTask,tasks}=state();
  const opensSurface = ['new','rename','fork','delete','archive','split','filter','previous','next','attention','agent','review','files','info','tab-next','tab-previous'].includes(action);
  if (opensSurface && settings?.leaveForTask() === false) return;
  if (opensSurface && !['rename','fork','delete'].includes(action)) taskSettings?.leaveForTask();
  return execute();
  async function execute(){
  if (action==='split'){split?.toggle();return;}
  if (action === 'filter') { if (!layout.visible.nav) layout.toggle('nav'); $('search').focus(); return; }
  if (action === 'schedules') { settings.open('schedules'); return; }
  if (action === 'new') { const project = currentTask?.project || navigation.projects[0]?.name; if (project) newTask.open(project, () => composer.focus()); return; }
  if (['previous', 'next', 'attention'].includes(action)) {
    const available = (navigation.filteredTasks?.() || tasks.filter(task=>!task.archived)).filter(task => action !== 'attention' || !task.archived && (task.needs_input || navigation.hierarchy.attention.has(task.id)));
    const index = available.findIndex(task => task.id === currentTask?.id);
    const task = available[(index < 0 ? (action==='previous'?available.length-1:0) : (index + (action === 'previous' ? -1 : 1) + available.length) % available.length)];
    if (task) selectTask(task); return;
  }
  if (['tab-next', 'tab-previous'].includes(action)) {
    const order=['agent','review','files','info'];
    const inTools=document.activeElement?.closest('#tools');
    const current=inTools ? (tools.tool==='status'?'info':tools.tool==='changes'?'files':'review') : $('agent-panel').hidden?'review':'agent';
    const next=order[(order.indexOf(current)+(action==='tab-next'?1:3))%4];
    if(next==='files'||next==='info'){if(!layout.visible.tools)layout.toggle('tools');tools.selectTab(next==='files'?'tool-changes':'tool-status');$('tool-'+(next==='files'?'changes':'status')).focus();}
    else selectTab('tab-'+next);
    return;
  }
  if (['agent','review'].includes(action)) { selectTab('tab-' + action); return; }
  if (['files','info'].includes(action)) { if (!layout.visible.tools) layout.toggle('tools'); tools.selectTab(action === 'info' ? 'tool-status' : 'tool-changes'); return; }
  if (!currentTask) return;
  if (action === 'pin') { await railActions.run(currentTask,'pin'); return; }
  if (action === 'archive') { archive.open(currentTask); return; }
  if (action === 'open-worktree') { if(currentTask.worktree_path){await copyNotice(currentTask.worktree_path,'Copied worktree path.');}else notice('Worktree path unavailable.'); return; }
  if (action === 'open-pr') {
    const base='/api/tasks/'+encodeURIComponent(currentTask.id);
    const [meta,result]=await Promise.all([api.json(base+'/meta?namespace=pr').catch(error=>{if([401,403].includes(error.status))throw error;return {};}),api.json(base+'/links').catch(error=>{if([401,403].includes(error.status))throw error;return {};})]);
    const {resolvePR}=await import('../../tasks/task-information.js');const url=resolvePR(meta.entries||[],result.links||[]);
    if(url)window.open(url,'_blank','noopener,noreferrer');else notice('No pull request recorded.');return;
  }
  const captured={...currentTask};
  const loaded=await taskSettings.open(captured);
  if(!loaded || state().currentTask?.id!==captured.id || !taskSettings.active || taskSettings.task?.id!==captured.id)return;
  if (action === 'rename') $('task-field-task-name')?.focus();
  else if (action === 'delete') taskSettings.confirm('delete');
  else if (action === 'fork') taskSettings.mutate('fork', {});


  }
}
