import { $, node, button, empty } from '../../shared/dom.js';
import {railChevron} from './rail-chevron.js';
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
    this.roster = {};
    this.hierarchy = taskParents([]);
    this.hierarchyWarning = node('p', 'projects-warning');
    this.hierarchyWarning.id = 'hierarchy-warning';
    this.hierarchyWarning.hidden = true;
    $('projects-warning').after(this.hierarchyWarning);
    this.query = '';
    $('search-clear').addEventListener('click', () => { $('search').value = ''; $('search-clear').hidden = true; $('search').dispatchEvent(new Event('input')); $('search').focus(); });
    $('search').addEventListener('input', event => { this.query = event.target.value; $('search-clear').hidden = !this.query; this.menuTask = null; this.render(); });
    for (const filter of ['active', 'archive']) $(filter === 'active' ? 'active-filter' : 'archive-filter').addEventListener('click', () => { this.filter = filter; this.menuTask = null; this.render(); });
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
    this.roles = taskRoles(this.roster);
    this.hierarchy = taskParents(tasks, this.roster);
    if (defer) this.scheduleRender();
    else if (this.renderKey() !== this.renderedKey) this.render();
  }
  renderKey() {
    return JSON.stringify([
      this.tasks.map(task => [task.id, task.name, task.project, task.branch, task.worktree_path, task.status, task.archived, task.archive_pending, task.needs_input, task.live, task.idle, task.pr_state, task.pinned]),
      this.projects.map(project => project.name), this.selected,
      [...this.pinned],
      [...this.roles || []].map(([id, role]) => [id, role.role_id, role.name, role.kind, role.live, role.status, role.ready_to_close, role.archived, role.needs_input]),
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
    this.collapsed.delete(task.project || 'Other tasks');
    for (let ancestor = this.hierarchy.parents.get(task.id); ancestor; ancestor = this.hierarchy.parents.get(ancestor)) {
      this.collapsedTasks.delete(ancestor);
      const parent = this.tasks.find(item => item.id === ancestor);
      if (parent) this.collapsed.delete(parent.project || 'Other tasks');
    }
  }
  latestTask(task) { return this.tasks.find(item => item.id === task.id) || task; }
  select(task) {
    task = this.latestTask(task);
    this.selected = task.id;
    this.reveal(task);
    this.onSelect(task);
    this.render();
  }
  render() {
    if (this.renderFrame != null) { cancelAnimationFrame(this.renderFrame); this.renderFrame = null; }
    this.renderedKey = this.renderKey();
    log('rail', 'rendered', { count: this.tasks.length });
    const scrollTop = $('task-groups').scrollTop;
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
    const fragment = drawTaskGroups(this, filtered, query);
    const hasGroups=fragment.childNodes.length>0;
    $('task-groups').replaceChildren(fragment);
    if (focusedTaskAction) queueMicrotask(() => {
      [...$('task-groups').querySelectorAll('.task-control')].find(item => item.dataset.taskId === focusedTask && item.dataset.taskAction === focusedTaskAction && (!focusedKey || item.dataset.rowKey===focusedKey))?.focus({ preventScroll: true });
    });
    else if (focusedTask) [...$('task-groups').querySelectorAll(focusedArchive ? '.task-archive' : '.task-row')].find(item => item.dataset.taskId === focusedTask && (!focusedKey || item.dataset.rowKey===focusedKey))?.focus();
    else if (focusedParent) [...$('task-groups').querySelectorAll('.task-disclosure')].find(item => item.dataset.parentTaskId === focusedParent && (!focusedKey || item.dataset.rowKey===focusedKey))?.focus();
    else if (focusedProjectAction) this.focusProject(focusedProject, focusedProjectAction);
    else if (focusedProject) [...$('task-groups').querySelectorAll('summary')].find(item => item.dataset.project === focusedProject)?.focus();
    $('task-groups').scrollTop = scrollTop;
    if (!hasGroups) empty($('task-groups'), '', query ? 'No tasks match your search.' : 'No tasks match this view.');
  }
  focusProject(project, action) {
    [...$('task-groups').querySelectorAll('.project-action')].find(control => control.dataset.project === project && control.dataset.projectAction === action)?.focus();
  }
  taskItem(entry, query) {
    const { task, needsInput } = entry;
    const key=task.id;
    const role=this.roles?.get(task.id);
    const row=compactTaskRow({task,role,selected:this.selected,needsInput,ownInput:!task.archived&&(task.needs_input||this.hierarchy.attention.has(task.id)),failed:role?.status==='failed'||this.hierarchy.failed.has(task.id),pinned:!task.archived&&this.pinned.has(task.id),select:selected=>this.select(selected)});
    row.dataset.rowKey=key;
    const item = node('div', 'task-item');
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
    item.append(row, action, ...taskMenu({...task,pinned:typeof task.pinned==='boolean'?task.pinned:this.pinned.has(task.id)}, {
      pinKnown:this.actions.pinKnown?.(task)??true,
      quickAction: (task, operation) => this.actions.quickAction?.(this.latestTask(task), operation),
      open: this.menuTask === key,
      changed: open => { if (open) {this.menuTask = key;this.actions.inspect?.(this.latestTask(task));} else if (this.menuTask === key) this.menuTask = null; },
      settings: selected => this.actions.taskSettings?.(this.latestTask(selected)),
    }));
    item.addEventListener('contextmenu', event => { if (event.target.closest('.task-menu')) return; event.preventDefault(); setTimeout(() => { if (item.isConnected) item.querySelector('.task-more').click(); }, 0); });
    for(const control of item.querySelectorAll('[data-task-id]'))control.dataset.rowKey=key;
    return this.taskFamily(item,entry,query,key);
  }
  taskFamily(item,entry,query,key) {
    const {task,children}=entry;
    if(!children.length)return item;
    const collapseKey=task.id;
    const family = node('details', 'task-family');
    family.open = !this.collapsedTasks.has(collapseKey) || !!query;
    const summary = node('summary', 'task-disclosure');
    summary.dataset.parentTaskId = task.id;summary.dataset.rowKey=key;
    summary.setAttribute('aria-label', `Toggle children of ${task.name}`);
    const describeDisclosure = () => { summary.dataset.tooltip = `${family.open ? 'Collapse' : 'Expand'} ${children.length} ${children.length === 1 ? 'task' : 'tasks'}`; };
    describeDisclosure();
    summary.append(railChevron('task-caret'), item);
    family.append(summary);
    const descendants = node('div', 'task-children');
    descendants.append(...children.map(child => this.taskItem(child, query)));
    family.append(descendants);
    const setExpanded = expanded => {
      family.open = expanded;
      describeDisclosure();
      if (family.open) this.collapsedTasks.delete(collapseKey); else this.collapsedTasks.add(collapseKey);
      log('task-hierarchy', 'disclosure', { expanded: family.open });
    };
    summary.addEventListener('click', event => {
      if (event.target.closest('button')) return;
      event.preventDefault();
      setExpanded(!family.open);
    });
    summary.addEventListener('keydown', event => {
      if (event.target !== summary || !['ArrowRight', 'ArrowLeft'].includes(event.key)) return;
      event.preventDefault();
      setExpanded(event.key === 'ArrowRight');
    });
    return family;
  }

  clear() {
    this.menuTask = null; this.pinned.clear();
    this.tasks = []; this.projects = []; this.selected = null; this.query = ''; this.filter = 'active'; this.collapsed.clear(); this.collapsedTasks.clear(); this.roster = {}; this.roles=new Map(); this.hierarchy = taskParents([]); this.hierarchyWarning.hidden = true;
    $('search').value = ''; $('search-clear').hidden = true; $('projects-warning').hidden = true; $('projects-warning').textContent = ''; this.render();
  }
}
