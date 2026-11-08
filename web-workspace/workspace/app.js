import {TaskBatch} from '../tasks/task-batch.js';
import {TaskDelete} from '../tasks/task-delete.js';
import {deletionNeighbor} from '../tasks/task-delete-policy.js';
import {HeraKanban} from './rail/hera-kanban.js';
import { connectionState } from './connection-state.js';
import { installTooltips } from '../shared/tooltip.js';
import { workspaceShortcutAction } from './shortcuts/workspace-shortcut-actions.js';
import { WorkspaceAttention } from './attention/workspace-attention.js';
import { WorkspaceEvents } from './events/workspace-events.js';
import { patchEvent } from './events/workspace-events-policy.js';
import { TaskHeaderLinks } from '../tasks/task-links.js';
import { TaskClipboard } from '../tasks/task-clipboard.js';
import { RailActions } from './rail/rail-actions.js';
import { SplitWorkspace } from './split/split-workspace.js';
import { WorkspaceRoster } from './workspace-roster.js';
import { ClaudeSessions } from '../tasks/settings/claude-sessions.js';
import { WorkspaceShortcuts } from './shortcuts/shortcuts.js';
import { Api, log } from '../core/api.js';
import { TaskTerminalCache } from '../terminal/task-terminal-cache.js';
import { $, tabs, notice } from '../shared/dom.js';
import { WorkspaceLayout } from './layout.js';
import { TaskNavigation } from './rail/tasks.js';
import { TaskComposer } from '../composer/composer.js';
import { TaskReview } from '../review/review.js';
import { TaskTools } from '../tasks/tools.js';
import { TaskArchive } from '../tasks/task-archive.js';
import { WorkspaceHistory } from './workspace-history.js';
import { readProjects } from './rail/projects.js';
import { readHierarchy } from './rail/task-hierarchy.js';
import { WorkspaceTaskView } from '../tasks/task-view.js';
import { WorkspaceConnection } from './workspace-connection.js';
import { WorkspaceSettings } from '../settings/settings-workspace.js';
import { NewTaskView } from '../tasks/create/new-task.js';
import { TaskSettingsView } from '../tasks/settings/task-settings.js';
import { restoreWorkspaceView } from './workspace-route.js';

installTooltips();
const api = new Api(id => tasks.find(task => task.id === id));
let historyState, events, attention, deleteFocus;
let split, rosterWatch, taskSettings, settings, newTask, terminal, currentTask, tasks = [], refreshAbort, refreshGeneration = 0;
const taskTabs = new Map();
const taskLinks = new TaskHeaderLinks(api);
const railActions = new RailActions(api, async () => { navigation.pinned = railActions.pinned; await refreshTasks(); }, () => { navigation.pinned=railActions.pinned; navigation.render(); });
const heraKanban = new HeraKanban(api,()=>navigation.render(),()=>refreshTasks());
const sessions = new ClaudeSessions(api, async (task, status) => { if(status !== 'unchanged')tools.invalidateIdentity(task.id); if (currentTask?.id === task.id && status !== 'unchanged') { composer.context.invalidate(); terminal?.reconnect(); } await refreshTasks();if(taskSettings?.active&&taskSettings.task?.id===task.id)await taskSettings.load(); });
const composer = new TaskComposer((text, task, options) => terminal.send(text, task, options), api);
const stagedClipboard = new TaskClipboard(api, $('terminal-actions'), () => {if($('draft').disabled){const filter=$(navigation.filter==='archive'?'archive-filter':'active-filter');(filter.getClientRects().length?filter:$('toggle-nav')).focus({preventScroll:true});}else composer.focus();});
function showAgent(){const composerVisible=!taskSettings?.active&&!settings?.active&&!newTask?.active;const visible=composerVisible&&!$('agent-panel').hidden;terminal?.show(visible);split?.setVisible(visible);composer.suggestion.setVisible(composerVisible);}
const layout = new WorkspaceLayout(showAgent);
const selectTab = tabs(document.querySelector('.workspace-tabs'), tab => {
  newTask?.close(false);
  const agent = tab.id === 'tab-agent';
  $('agent-panel').hidden = !agent;
  $('review-panel').hidden = agent;
  $('terminal-actions').hidden = !agent;
  showAgent();
  if (currentTask) { taskTabs.set(currentTask.id, agent ? 'agent' : 'review'); writeLocation(); }
});
const review = new TaskReview(api, () => {
  selectTab('tab-review');
  if (layout.compact.matches && layout.visible.tools) layout.close('tools');
}, writeLocation);
const tools = new TaskTools(api, review);
const navigation = new TaskNavigation(selectTask, (project, action) => {
  if (!settings.leaveForTask()) return;
  taskSettings.leaveForTask();
  newTask.open(project, () => {
    if (layout.compact.matches) $('toggle-nav').focus(); else navigation.focusProject(project, action);
  });
}, {
  heraKanban,
  bulkAction: tasks => taskBatch.open(tasks),
  deleteTask: task => {deleteFocus=taskNeighbor(task)?.id;taskDelete.open(task);},
  projectConfiguration: project => { taskSettings.leaveForTask(); settings.openProject(project); },
  taskSettings: task => taskSettings.open(task),
  quickAction: (task, operation) => railActions.run(task, operation),
  inspect:task=>railActions.inspect(task),pinKnown:task=>typeof task.pinned==='boolean'||railActions.knownPins.has(task.id),
});
function createdTask(task) {
  if (!connection.connected) return;
  tasks = [task, ...tasks.filter(item => item.id !== task.id)];
  navigation.filter = 'active'; navigation.query = ''; $('search').value = '';
  navigation.update(tasks, task.id);
  selectTask(task);
  refreshTasks();
}
newTask = new NewTaskView(api, createdTask, open => {
  if (open) layout.taskSelected();
  showAgent();
  if (!open && connection.connected && currentTask && !tasks.some(task => task.id === currentTask.id)) historyState.restore();
});
function applyArchive(task,archived) {
  tasks = tasks.map(item => item.id === task.id ? { ...item, archived } : item);
  if (currentTask?.id === task.id) {
    currentTask = tasks.find(item => item.id === task.id) || { ...currentTask, archived };
    composer.task = currentTask; composer.render(); tools.updateTask(currentTask); header(currentTask);
    terminal?.attach(currentTask);
  }
  navigation.update(tasks, currentTask?.id); split?.select(currentTask,tasks,navigation.roster);
}
const archive = new TaskArchive(api, async task => {
  applyArchive(task,!task.archived);
  await refreshTasks();
  if (taskSettings.active && taskSettings.task?.id === task.id) await taskSettings.load();
},()=>({roster:navigation.roster,links:taskLinks.urls}));
function taskNeighbor(task) {
  const rows=[...$('task-groups').querySelectorAll('.task-row')].filter(row=>row.getClientRects().length);
  const ordered=rows.map(row=>tasks.find(item=>item.id===row.dataset.taskId)).filter(Boolean);
  const excluded=new Set([task.id,...taskBatch.deletingIds]);
  return deletionNeighbor(ordered,excluded)||deletionNeighbor(navigation.filteredTasks(),excluded);
}
function removeDeletedTask(task,{selectNeighbor=true}={}) {
  if(taskSettings?.active&&taskSettings.task?.id===task.id)taskSettings.leaveForTask();
  const neighbor=currentTask?.id===task.id?taskNeighbor(task):null;
  tasks=tasks.filter(item=>item.id!==task.id);
  navigation.update(tasks,currentTask?.id);
  if(currentTask?.id===task.id){
    currentTask=null;terminal?.attach(null);composer.select(null);review.selectTask(null);tools.selectTask(null);header(null);
    historyState.write(null,'agent',null,true);
    if(neighbor&&selectNeighbor)selectTask(neighbor,{replace:true});
    else terminalState({message:'Choose a task to connect.',interactive:false});
  }
  terminal?.retain(new Set(tasks.map(item=>item.id)));
  return neighbor;
}
const taskDelete = new TaskDelete(api, async (task,options) => {removeDeletedTask(task,options);await refreshTasks();},()=>navigation.roster,task=>{
  const rows=[...$('task-groups').querySelectorAll('.task-row')].filter(row=>row.getClientRects().length);
  const target=rows.find(row=>row.dataset.taskId===task.id)?.closest('.task-item')?.querySelector('.task-more')||rows.find(row=>row.dataset.taskId===deleteFocus)||rows[0];
  (target||$(navigation.filter==='archive'?'archive-filter':'active-filter')).focus({preventScroll:true});
},()=>taskLinks.urls);
const taskBatch = new TaskBatch(api,navigation.selection,taskDelete,(task,action)=>{if(action==='delete')removeDeletedTask(task);else applyArchive(task,true);},()=>refreshTasks());
const taskView = new WorkspaceTaskView(composer, archive);

function terminalState(state) { taskView.state(state, currentTask); }
function header(task) { taskView.header(task, connection.connected); taskLinks.select(task); stagedClipboard.select(task); attention?.update(tasks,task?.id);if(!task)split?.select(null,tasks,navigation.roster); }
function writeLocation(replace = false) {
  if (currentTask) historyState?.write(currentTask, $('review-panel').hidden ? 'agent' : 'review', review.selections.get(currentTask.id), replace);
}
function selectTask(task, options = {}) {
  if (!settings.leaveForTask()) return;
  split?.beforeSelect(task);
  taskSettings?.leaveForTask();
  newTask?.close(false);
  if (currentTask?.id === task.id && !options.restore) { layout.taskSelected(); composer.focus(); return; }
  historyState.suspend(() => {
    const changed = currentTask?.id !== task.id;
    currentTask = task;
    sessions.select(task); railActions.select(task);
    if (changed) composer.select(task);
    else { composer.task = task; composer.render(); }
    review.selectTask(task, options.selection);
    if (changed) tools.selectTask(task); else tools.updateTask(task);
    header(task);
    selectTab(`tab-${options.view || taskTabs.get(task.id) || 'agent'}`);
    if (changed) terminal?.attach(task);
    split?.select(task,tasks,navigation.roster);
    navigation.selected = task.id;
    if (options.restore && changed) navigation.filter = task.archived ? 'archive' : 'active';
    navigation.reveal(task);
    navigation.render();
    layout.taskSelected();
  });
  if (!options.restore) { writeLocation(options.replace); composer.focus(); }
}
function restoreLocation(state) {
  if (!connection.connected) return;
  return restoreWorkspaceView(state, { newTask, taskSettings, settings, tasks, selectTask, unavailable: () => {
    currentTask = null; terminal?.attach(null); composer.select(null); review.selectTask(null); tools.selectTask(null); header(null);
    navigation.update(tasks, null);
    $('task-title').textContent = 'Task unavailable';
    $('task-title').title = 'This task no longer exists or cannot be read. Choose another task.';
    terminalState({ message: 'Task unavailable', kind: 'warning', interactive: false });
  } });
}
historyState = new WorkspaceHistory(restoreLocation);
async function refreshTasks() {
  if (!connection.connected) return;
  events?.beginSnapshot();
  refreshAbort?.abort();
  refreshAbort = new AbortController();
  const generation = ++refreshGeneration;
  try {
    const [result, projects, hierarchy] = await Promise.all([
      api.json('/api/tasks?archived=all', { signal: refreshAbort.signal }),
      readProjects(api, refreshAbort.signal),
      readHierarchy(api, refreshAbort.signal),
    ]);
    if (generation !== refreshGeneration || !connection.connected) return;
    tasks = events ? events.snapshot(result.tasks || [],tasks) : result.tasks || [];
    terminal?.retain(new Set(tasks.map(task=>task.id))); railActions.reconcile(tasks);
    attention?.update(tasks,currentTask?.id);
    navigation.update(tasks, currentTask?.id, projects, hierarchy); tools.updateRoster(navigation.roster);

    if (currentTask) {
      const next = tasks.find(task => task.id === currentTask.id);
      if (next) { currentTask = next; composer.task = next; composer.render(); tools.updateTask(next); header(next); terminal?.attach(next); }
      else if (!newTask.active && !taskSettings.active && !settings.active) historyState.restore();
    } else if (!newTask.active && !taskSettings.active && !settings.active) {
      if (historyState.read().task || historyState.read().settings || historyState.read().taskSettings) historyState.restore();
      else { const initial = tasks.find(task => !task.archived); if (initial) selectTask(initial, { replace: true }); }
    }
    split?.select(currentTask,tasks,navigation.roster);
    connectionState('Connected to Argus', 'connected');
    if(projects.warning||hierarchy.warning)events?.problem('Navigation details unavailable. Retry connection keeps the current task and draft.');else events?.healthy(); log('tasks', 'loaded', { count: tasks.length });
  } catch (error) {
    if (generation !== refreshGeneration || error.name === 'AbortError') return;
    connectionState(error.status === 401 || error.status === 403 ? 'Token expired' : 'Connection unavailable', 'error');
    events?.problem('Task refresh failed. Retry connection keeps the current task and draft.');
    log('tasks', 'refresh failed', { status: error.status || 'error' });
  }
}
settings = new WorkspaceSettings(api, {
  history: historyState,
  beforeOpen: () => { taskSettings.leaveForTask(); newTask.close(false); },
  opened: () => { if (layout.compact.matches) layout.taskSelected(); showAgent(); },
  closed: () => { showAgent(); composer.focus(); },
  restore: () => historyState.restore(), changed: () => refreshTasks(),
});
taskSettings = new TaskSettingsView(api, {
  deleteTask:(task,signal)=>taskDelete.open(task,{id:'task-action-dialog',confirmLabel:'Confirm',signal,selectNeighbor:false}),
  history: historyState,
  beforeOpen: () => { if (!settings.leaveForTask()) return false; newTask.close(false); },
  opened: () => { layout.taskSelected(); showAgent(); },
  closed: () => { sessions.close();showAgent(); },
  restore: () => historyState.restore(), archive: task => archive.open(task), created: createdTask, switchSession:task=>sessions.open(task),
  changed: async () => {
    tools.invalidateIdentity(taskSettings.task?.id);
    if (currentTask?.id === taskSettings.task?.id) composer.context.invalidate();
    await refreshTasks();
  },
});
const connection = new WorkspaceConnection(api, {
  refresh: refreshTasks, openEvents: () => events.start(), closeEvents: () => events.stop(),
  connected: (loaded, projects, hierarchy) => {
    tasks = events.snapshot(loaded); terminal = new TaskTerminalCache(api, $('terminal'), terminalState, state => taskView.fit(state), undefined, (id,value) => stagedClipboard.update(id,value));
    railActions.reconcile(tasks);navigation.pinned=railActions.pinned;
    navigation.update(tasks, null, projects, hierarchy); tools.updateRoster(navigation.roster); settings.setConnected(true); rosterWatch.start();
    if (historyState.read().task || historyState.read().settings || historyState.read().taskSettings) historyState.restore();
    else { const initial = tasks.find(task => !task.archived); if (initial) selectTask(initial, { replace: true }); else header(null); }
    composer.focus();
  },
  disconnected: () => {
    refreshGeneration++; refreshAbort?.abort(); attention?.clear(); sessions.clear(); taskLinks.clear(); taskLinks.urls.clear(); railActions.clear(); heraKanban.clear(); rosterWatch?.stop(); split?.clear(); newTask.clear(); taskSettings.dismiss(); settings.setConnected(false);
    terminal?.destroy(); terminal = null; currentTask = null; tasks = [];
    composer.clear(); review.clear(); tools.clear(); navigation.clear(); archive.clear(); taskDelete.clear(); taskBatch.clear(); taskTabs.clear();
    $('terminal').replaceChildren(); $('notice').hidden = true; header(null); selectTab('tab-agent');
    terminalState({ message: 'No task selected', interactive: false });
  },
});
$('reconnect-terminal').addEventListener('click', () => terminal?.reconnect());
$('fit-session').addEventListener('click', async () => {
  try { await terminal?.fitSession(); }
  catch (error) { notice(`Could not fit the session. ${error.message}`); }
});
window.addEventListener('pagehide', () => { attention?.destroy(); sessions.clear(); taskLinks.clear(); stagedClipboard.select(null); railActions.clear(); heraKanban.clear(); rosterWatch?.stop(); split?.clear(); newTask.clear(); refreshAbort?.abort(); terminal?.destroy(); composer.clear(); review.clear(); tools.clear(); archive.clear(); taskDelete.clear(); taskBatch.clear(); settings.clear(); taskSettings.dismiss(); });
events = new WorkspaceEvents(api, { refresh: refreshTasks, context: event => composer.context.event(event), suggestion: event => { if (connection.connected) composer.suggestion.event(event); }, event: event => {
  if (!connection.connected) return;
  tools.event(event);
  if(event.type==='task.deleted')removeDeletedTask({id:event.task_id});
  const result = patchEvent(tasks, event); tasks = result.tasks;
  terminal?.retain(new Set(tasks.map(task=>task.id))); railActions.reconcile(tasks);
  navigation.update(tasks,currentTask?.id,undefined,undefined,{defer:true});
  if (currentTask) { const next=tasks.find(task=>task.id===currentTask.id); if(next){currentTask=next;composer.task=next;composer.render();tools.updateTask(next);header(next);terminal?.attach(next);}else historyState.restore(); }
  split?.select(currentTask,tasks,navigation.roster);
  attention?.update(tasks,currentTask?.id); attention?.event(event);
  if(result.resnapshot)events.scheduleSnapshot();
  log('events','applied',{count:tasks.length});
} });
split = new SplitWorkspace(() => terminal);
rosterWatch = new WorkspaceRoster(api, () => navigation.roles?.size>0||split.enabled&&split.visible, result => {
  const hierarchy=result.warning?{roster:navigation.roster,warning:result.warning}:result;
  navigation.update(tasks,currentTask?.id,undefined,hierarchy,{defer:true});tools.updateRoster(navigation.roster);split.select(currentTask,tasks,navigation.roster);
});
attention = new WorkspaceAttention(task => selectTask(task));
new WorkspaceShortcuts({ connected: () => connection.connected, run: action => workspaceShortcutAction(action, {
  state: () => ({split,currentTask,tasks}), api, layout, navigation, settings, newTask, tools, railActions, archive, taskSettings, selectTask, selectTab, composer,
}) }, target => split?.terminalFor(target) || terminal);
connection.start();
