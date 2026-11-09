import {coordinatorRoles} from '../../tasks/messages/coordinator-inbox.js';
import {captureFocus} from '../../shared/focus-recovery.js';
import {TaskSelection} from './task-selection.js';
import {coordinators} from './hera-kanban.js';
import {KanbanDisclosure} from './kanban-group.js';
import { $, node, button, empty } from '../../shared/dom.js';
import {taskFamily} from './task-family.js';
import {taskTriage} from './task-triage.js';
import { taskParents } from './task-hierarchy.js';
import { log } from '../../core/api.js';
import {compactTaskRow} from './task-row.js';
import {taskRoles} from './task-grouping.js';
import {drawTaskGroups} from './task-groups.js';
import { taskMenu } from './task-menu.js';

export function taskState(task) {
  if (task.archived) return 'Archived';
  if (task.needs_input) return 'Needs input';
  if (task.status === 'in_progress') return task.live ? (task.idle ? 'Waiting' : 'Running') : 'Session offline';
  return { pending: 'Pending', in_review: 'In review', complete: 'Complete' }[task.status] || task.status || 'Unknown';
}
export class TaskNavigation {
  constructor(onSelect, onCreate = () => {}, actions = {}) {
    this.onSelect = onSelect;
    this.onCreate = onCreate; this.actions = actions; this.menuTask = null;
    this.tasks = [];
    this.projects = [];
    this.filter = 'active'; this.pinned = new Set();
    this.collapsed = new Set();
    this.collapsedTasks = new Set();
    this.kanbanDisclosure = new KanbanDisclosure();
    this.roster = {};
    this.hierarchy = taskParents([]);
    this.hierarchyWarning = node('p', 'projects-warning');
    this.hierarchyWarning.id = 'hierarchy-warning';
    this.hierarchyWarning.hidden = true;
    $('projects-warning').after(this.hierarchyWarning);
    this.query = '';
    this.selection=new TaskSelection(this,tasks=>this.actions.bulkAction?.(tasks));
    $('search-clear').addEventListener('click', () => { $('search').value = ''; $('search-clear').hidden = true; $('search').dispatchEvent(new Event('input')); $('search').focus(); });
    $('search').addEventListener('input', event => { this.query = event.target.value; $('search-clear').hidden = !this.query; this.menuTask = null; this.render(); });
    for (const filter of ['active', 'archive']) $(filter === 'active' ? 'active-filter' : 'archive-filter').addEventListener('click', () => { if(this.selection.active)this.selection.exit(false); this.filter = filter; this.menuTask = null; this.render(); });
  }
  update(tasks, selected, projectState, hierarchyState, { defer = false } = {}) {
    this.tasks = tasks; this.selected = selected;
    if (projectState) {
      if (projectState.projects) this.projects = projectState.projects;
      $('projects-warning').textContent = projectState.warning || '';
      $('projects-warning').hidden = !projectState.warning;
    }
    if (hierarchyState) {
      this.roster = hierarchyState.roster;
      this.hierarchyWarning.textContent = hierarchyState.warning || '';
      this.hierarchyWarning.hidden = !hierarchyState.warning;
    }
    this.roles = taskRoles(this.roster, tasks);
    this.hierarchy = taskParents(tasks, this.roster);
    if (defer) this.scheduleRender();
    else if (this.renderKey() !== this.renderedKey) this.render();
  }
  coordinators() { return new Map([...coordinators(this.roster)].map(([id,coordinator])=>[id,this.actions.heraKanban?.value(coordinator)||coordinator])); }
  renderKey(coordinatorMap = this.coordinators()) {
    return JSON.stringify([
      this.tasks.map(task => [task.id, task.name, task.project, task.branch, task.worktree_path, task.status, task.archived, task.archive_pending, task.needs_input, task.live, task.idle, task.pr_state, task.pinned]),
      this.projects.map(project => project.name), this.selected, this.selection?.active,[...this.selection?.ids||[]],
      [...this.pinned], [...coordinatorMap].map(([id,orch])=>[id,orch.id,orch.root,orch.kanban_status,orch.pinned,this.actions.heraKanban?.pending.has(orch.id)]),
      [...this.roles || []].map(([id, role]) => [id, role.role_id, role.name, role.kind, role.live, role.status, role.ready_to_close, role.archived, role.needs_input]),
      (this.roster.orchestrators||[]).flatMap(orch=>(orch.roles||[]).filter(role=>role.kind==='coordinator').map(role=>[role.role_id,role.task_id,role.board_unread])),
      [...this.hierarchy.parents], [...this.hierarchy.attention], [...this.hierarchy.failed],
      this.tasks.map(task => this.actions.pinKnown?.(task) ?? true),
    ]);
  }
  scheduleRender() {
    if (this.renderKey() === this.renderedKey || this.renderFrame != null) return;
    this.renderFrame = requestAnimationFrame(() => { this.renderFrame = null; if (this.renderKey() !== this.renderedKey) this.render(); });
  }
  filteredTasks() {
    const query=this.query.toLocaleLowerCase().trim();
    return this.tasks.filter(task=>!!task.archived===(this.filter==='archive')&&`${task.name} ${task.project} ${task.branch||''}`.toLocaleLowerCase().includes(query));
  }
  reveal(task) {
    let root=task;
    for(let parent=this.hierarchy.parents.get(root.id);parent;parent=this.hierarchy.parents.get(root.id)){const next=this.tasks.find(item=>item.id===parent);if(!next)break;root=next;}
    const coordinator=this.coordinators().get(root.id);
    if(coordinator?.root)this.kanbanDisclosure.set(root.project||'Other tasks',coordinator.kanban_status||'active',false);
    this.collapsed.delete(task.project || 'Other tasks');
    for (let ancestor = this.hierarchy.parents.get(task.id); ancestor; ancestor = this.hierarchy.parents.get(ancestor)) {
      this.collapsedTasks.delete(ancestor);
      const parent = this.tasks.find(item => item.id === ancestor);
      if (parent) this.collapsed.delete(parent.project || 'Other tasks');
    }
  }
  latestTask(task) { return this.tasks.find(item => item.id === task.id) || task; }
  select(task) {
    if(this.selection?.active){this.selection.toggle(task);return;}
    task = this.latestTask(task);
    this.selected = task.id;
    this.reveal(task);
    this.onSelect(task);
    this.render();
  }
  render() {
    if (this.renderFrame != null) { cancelAnimationFrame(this.renderFrame); this.renderFrame = null; }
    const coordinatorMap = this.coordinators();
    this.renderedKey = this.renderKey(coordinatorMap);
    log('rail', 'rendered', { count: this.tasks.length });
    const restore=captureFocus($('task-groups'),()=>$(this.filter==='archive'?'archive-filter':'active-filter'),{selector:'.task-row,.task-archive,.task-more,.task-control,.task-mail'});
    const scrollTop = $('task-groups').scrollTop;
    const focusedKanban=document.activeElement?.dataset.kanbanKey;
    const focusedKey=document.activeElement?.dataset.rowKey;
    const focusedTask = document.activeElement?.dataset.taskId;
    const focusedArchive = document.activeElement?.classList.contains('task-archive');
    const focusedTaskAction = document.activeElement?.dataset.taskAction;
    const focusedParent = document.activeElement?.dataset.parentTaskId;
    const focusedProject = document.activeElement?.dataset.project;
    const focusedProjectAction = document.activeElement?.dataset.projectAction;
    $('active-count').textContent = this.tasks.filter(task => !task.archived).length;
    $('archive-count').textContent = this.tasks.filter(task => task.archived).length;
    $('active-filter').setAttribute('aria-pressed', String(this.filter === 'active'));
    $('archive-filter').setAttribute('aria-pressed', String(this.filter === 'archive'));
    const query = this.query.toLocaleLowerCase().trim();
    const filtered = this.filteredTasks();
    $('task-count').textContent = `${filtered.length} ${filtered.length === 1 ? 'task' : 'tasks'}`;
    const fragment = drawTaskGroups(this, filtered, query, coordinatorMap);
    const hasGroups=fragment.childNodes.length>0;
    $('task-groups').replaceChildren(fragment);
    if (focusedTaskAction) queueMicrotask(() => {
      const control=[...$('task-groups').querySelectorAll('.task-control,.task-mail')].find(item => item.dataset.taskId === focusedTask && item.dataset.taskAction === focusedTaskAction && (!focusedKey || item.dataset.rowKey===focusedKey));
      const group=control?.closest('.kanban-group');
      if(group&&!group.open)group.querySelector('summary')?.focus({preventScroll:true});
      else if(control?.getClientRects().length)control.focus({preventScroll:true});
    });
    else if(focusedKanban){
      const group=[...$('task-groups').querySelectorAll('.kanban-header')].find(item=>item.dataset.kanbanKey===focusedKanban);
      const source=[...$('task-groups').querySelectorAll('.task-more')].find(item=>item.dataset.taskId===this.kanbanFocusTask);
      (group||source)?.focus({preventScroll:true});
    }
    else if (focusedTask) [...$('task-groups').querySelectorAll(focusedArchive ? '.task-archive' : '.task-row')].find(item => item.dataset.taskId === focusedTask && (!focusedKey || item.dataset.rowKey===focusedKey))?.focus();
    else if (focusedParent) [...$('task-groups').querySelectorAll('.task-disclosure')].find(item => item.dataset.parentTaskId === focusedParent && (!focusedKey || item.dataset.rowKey===focusedKey))?.focus();
    else if (focusedProjectAction) this.focusProject(focusedProject, focusedProjectAction);
    else if (focusedProject) [...$('task-groups').querySelectorAll('summary')].find(item => item.dataset.project === focusedProject)?.focus();
    $('task-groups').scrollTop = scrollTop;
    if(this.selection?.active)this.selection.prune();else this.selection?.header();
    this.renderedKey=this.renderKey(coordinatorMap);
    if (!hasGroups) empty($('task-groups'), '', query ? 'No tasks match your search.' : 'No tasks match this view.');
    if(focusedTaskAction)queueMicrotask(restore);else restore();
  }
  focusProject(project, action) {
    [...$('task-groups').querySelectorAll('.project-action')].find(control => control.dataset.project === project && control.dataset.projectAction === action)?.focus();
  }
  taskItem(entry, query, coordinatorMap) {
    const { task, needsInput } = entry;
    const key=task.id;
    const role=this.roles?.get(task.id);
    const coordinator=coordinatorMap.get(task.id);
    const row=compactTaskRow({task,role,selected:this.selected,needsInput,ownInput:!task.archived&&(task.needs_input||this.hierarchy.attention.has(task.id)),failed:role?.status==='failed'||this.hierarchy.failed.has(task.id),pinned:!task.archived&&(coordinator?coordinator.pinned:this.pinned.has(task.id)),select:selected=>this.select(selected)});
    row.dataset.rowKey=key;
    const item = node('div', 'task-item');
    item.append(row);taskTriage(item,row,{task,query,coordinatorMap,hierarchy:this.hierarchy});
    if(this.selection?.active){
      row.classList.toggle('batch-selected',this.selection.ids.has(task.id));row.setAttribute('aria-pressed',String(this.selection.ids.has(task.id)));row.removeAttribute('aria-current');item.append(this.selection.checkbox(task));
      return taskFamily(this,item,entry,query,key,coordinatorMap);
    }
    const mailbox=coordinatorRoles(this.roster,task.id).find(role=>role.board_unread>0);
    if(mailbox&&!task.archived){
      row.querySelector('.task-status-icon').style.visibility='hidden';
      const mail=button('',()=>this.actions.openInbox?.(this.latestTask(task),mailbox.role_id),'task-mail');
      mail.dataset.taskId=task.id;mail.dataset.taskAction='inbox';mail.dataset.rowKey=key;
      mail.setAttribute('aria-label',`Open Inbox for ${task.name}`);mail.dataset.tooltip='Unread coordinator Inbox';
      const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('viewBox','0 0 16 16');svg.setAttribute('aria-hidden','true');
      const path=document.createElementNS(svg.namespaceURI,'path');path.setAttribute('d','M2 3h12v10H2ZM2 3l6 5 6-5');svg.append(path);mail.append(svg);item.append(mail);
    }
    const action = button('', event => {
      event.stopPropagation();
      item.dispatchEvent(new CustomEvent('task-archive', { bubbles: true, detail: this.latestTask(task) }));
    }, 'task-archive');
    action.dataset.taskId = task.id;
    const label = `${task.archived ? 'Restore' : 'Archive'} ${task.name}`;
    action.setAttribute('aria-label', label);
    action.dataset.tooltip = label;
    const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    icon.setAttribute('viewBox', '0 0 24 24');
    icon.setAttribute('aria-hidden', 'true');
    const outline = document.createElementNS(icon.namespaceURI, 'path');
    outline.setAttribute('d', task.archived ? 'M4 14v6h16v-6M12 16V4m-4 4 4-4 4 4' : 'M4 8v12h16V8M3 4h18v4H3zM9 12h6');
    icon.append(outline);
    action.append(icon);
    item.append(action, ...taskMenu({...task,pinned:typeof task.pinned==='boolean'?task.pinned:this.pinned.has(task.id)}, {
      coordinator,heraPending:this.actions.heraKanban?.pending.has(coordinator?.id),
      heraAction:(operation,status)=>{this.menuTask=null;this.kanbanFocusTask=task.id;item.querySelector('.task-more').focus();this.actions.heraKanban?.run(this.coordinators().get(task.id),operation,status);},
      pinKnown:this.actions.pinKnown?.(task)??true,
      quickAction: (task, operation) => this.actions.quickAction?.(this.latestTask(task), operation),
      open: this.menuTask === key,
      changed: open => { if (open) {this.menuTask = key;if(!coordinator)this.actions.inspect?.(this.latestTask(task));} else if (this.menuTask === key) this.menuTask = null; },
      deleteTask: selected => this.actions.deleteTask?.(this.latestTask(selected)),
      settings: selected => this.actions.taskSettings?.(this.latestTask(selected)),
    }));
    item.addEventListener('contextmenu', event => { if (event.target.closest('.task-menu')) return; event.preventDefault(); setTimeout(() => { if (item.isConnected) item.querySelector('.task-more').click(); }, 0); });
    for(const control of item.querySelectorAll('[data-task-id]'))control.dataset.rowKey=key;
    return taskFamily(this,item,entry,query,key,coordinatorMap);
  }

  clear() {
    this.selection?.clear();this.menuTask = null; this.pinned.clear();
    this.tasks = []; this.projects = []; this.selected = null; this.query = ''; this.filter = 'active'; this.collapsed.clear(); this.collapsedTasks.clear(); this.roster = {}; this.roles=new Map(); this.hierarchy = taskParents([]); this.hierarchyWarning.hidden = true;
    $('search').value = ''; $('search-clear').hidden = true; $('projects-warning').hidden = true; $('projects-warning').textContent = ''; this.render();
  }
}
