import {teamModelRows} from './team-models.js';
import {AgentMessages} from './messages/agent-messages.js';
import {CoordinatorInbox,coordinatorRoles} from './messages/coordinator-inbox.js';
import {captureFocus} from '../shared/focus-recovery.js';
import {taskCostRows} from './task-cost.js';
import { renderTaskLinks } from './task-links.js';
import {ToolRefresh} from './tools-live.js';
import {TaskChanges} from './changes/task-changes.js';
import { $, node, button, empty, tabs } from '../shared/dom.js';
import { changedFiles } from './changes/git.js';
import { taskState } from '../workspace/rail/tasks.js';
import { sizeLabel } from '../review/artifact-view.js';
import { log } from '../core/api.js';

export class TaskTools {
  constructor(api, review, callbacks = {}) {
    this.callbacks=callbacks;
    this.api = api;
    this.review = review;
    this.tool = 'changes';
    this.selectedTools = new Map();
    this.generation = 0;
    this.live = new ToolRefresh({visible:()=>this.visible(),read:(id,tool,signal)=>this.api.json(`/api/tasks/${encodeURIComponent(id)}/${tool==='artifacts'?'artifacts':'git/status'}`,{signal}),apply:result=>this.apply(result),failed:error=>this.failed(error),log:(message,fields)=>log('tools-live',message,fields)});
    this.wake=()=>{const visible=this.visible();if(!visible)this.live.suspend();if(visible&&!this.wasVisible){this.live.trigger('visible');if(this.tool==='status')this.drawStatus();}this.wasVisible=visible;};
    document.addEventListener('visibilitychange',this.wake);
    window.addEventListener('focus',()=>{this.wasVisible=this.visible();this.live.trigger('focus');if(this.tool==='status')this.drawStatus();});
    window.addEventListener('blur',()=>{this.wasVisible=false;this.live.suspend();});
    this.visibilityObserver=new MutationObserver(this.wake);
    for(const target of [document.body,$('workspace'),$('tools')])this.visibilityObserver.observe(target,{attributes:true,attributeFilter:['class','hidden','style']});
    this.selectTab = tabs(document.querySelector('.tool-tabs'), tab => {
      this.tool = tab.dataset.tool;
      tab.scrollIntoView({block:'nearest',inline:'nearest'});
      $('tool-panel').setAttribute('aria-labelledby', tab.id);
      if (this.task) this.selectedTools.set(this.task.id, this.tool);
      this.load(false);
    });
  }
  selectTask(task) {
    this.invalidateIdentity();this.statusView=null;
    this.abort?.abort(); this.generation++;
    this.api.selectTask?.(task?.id);
    this.task = task;
    this.inboxTab();
    const saved=this.selectedTools.get(task?.id);
    this.selectTab(`tool-${saved==='inbox'&&!coordinatorRoles(this.roster,task?.id).length?'changes':saved||'changes'}`);
  }
  inboxTab(){const tab=$('tool-inbox');if(tab)tab.hidden=!coordinatorRoles(this.roster,this.task?.id).length;}
  updateRoster(roster) {
    this.roster = roster;this.inboxTab();
    if(this.tool==='inbox'){
      const roles=coordinatorRoles(roster,this.task?.id),role=roles.find(role=>role.role_id===this.inboxRoleID)||roles[0];
      if(!role)this.selectTab('tool-changes');else if(this.mail?.role?.role_id!==role.role_id)this.load(false);
      else if(this.mail.role.board_unread!==role.board_unread){this.mail.role=role;this.mail.event({type:'resync'});}
    }
    if (this.tool === 'status') this.drawStatus();
  }
  updateTask(task) { this.task = task; if (this.tool === 'status') this.drawStatus(); }
  restoreLoadFocus() { const restore=this.restoreFocus;this.restoreFocus=null;restore?.(); }
  async load(refresh = true) {
    this.mail?.destroy();this.mail=null;
    this.abort?.abort();this.restoreLoadFocus();
    if(refresh)this.invalidateIdentity();
    this.statusView=null;
    this.live.stop();this.changes=null;this.artifactFingerprint=null;this.error=null;
    this.abort = new AbortController();
    this.generation++;
    const restore=this.restoreFocus=captureFocus($('tool-panel'),()=>document.getElementById(`tool-${this.tool}`),{signal:this.abort.signal});
    const task = this.task, tool = this.tool;
    if (!task) { empty($('tool-panel'), '', 'Select a task to explore its work.'); this.restoreLoadFocus(); return; }
    if(['messages','inbox'].includes(tool)){
      const roles=coordinatorRoles(this.roster,task.id),role=roles.find(role=>role.role_id===this.inboxRoleID)||roles[0];
      if(tool==='inbox'&&!role){this.selectTab('tool-changes');return;}
      this.mail=tool==='messages'?new AgentMessages(this.api,$('tool-panel')):new CoordinatorInbox(this.api,$('tool-panel'),{...this.callbacks,selectRole:id=>{this.inboxRoleID=id;this.load(false);}});
      this.mail.roles=roles;
      this.mail.select(task,role);await this.mail.refresh();
      if(this.restoreFocus===restore)this.restoreLoadFocus();else restore();return;
    }
    if (tool === 'status') { await this.drawStatus(); if(this.restoreFocus===restore)this.restoreLoadFocus();else restore(); return; }
    empty($('tool-panel'), '', tool === 'artifacts' ? 'Loading artifacts…' : 'Loading changes…');
    this.wasVisible=this.visible();this.live.select(task.id,tool);
  }
  visible() {
    const panel=$('tool-panel');
    return !!this.task&&!document.hidden&&document.hasFocus()&&!!panel?.getClientRects().length&&getComputedStyle(panel).visibility!=='hidden'&&!document.body.classList.contains('settings-mode');
  }
  event(event) {this.live.event(event);this.review.event?.(event);this.mail?.event?.(event);}
  apply(result) {
    const restore=captureFocus($('tool-panel'),()=>document.getElementById(`tool-${this.tool}`),{signal:this.abort.signal});
    this.error?.remove();this.error=null;
    if(this.tool==='changes') {
      if(!this.changes)this.drawChanges(result);
      else if(!this.changes.show(result))log('tools-live','skipped unchanged');
    } else {
      const fingerprint=JSON.stringify(result.artifacts||[]);
      if(this.artifactFingerprint===fingerprint){restore();this.restoreLoadFocus();log('tools-live','skipped unchanged');return;}
      this.artifactFingerprint=fingerprint;this.drawArtifacts(result.artifacts||[]);
    }
    restore();this.restoreLoadFocus();
    log('tools','loaded',{count:this.tool==='artifacts'?(result.artifacts||[]).length:changedFiles(result).length,status:this.tool});
  }
  failed(error) {
    const restore=captureFocus($('tool-panel'),()=>document.getElementById(`tool-${this.tool}`),{signal:this.abort.signal});
    this.error?.remove();
    const box=node('div','tool-error');box.setAttribute('role','alert');box.append(node('p','tool-note',`${this.tool==='artifacts'?'Artifacts':'Changes'} unavailable. ${error.message}`),button('Retry',()=>this.live.trigger('retry')));
    if(!this.changes&&!this.artifactFingerprint)$('tool-panel').replaceChildren(box);else $('tool-panel').prepend(box);
    this.error=box;restore();this.restoreLoadFocus();
  }
  heading(text) {
    const header = node('div', 'tool-summary');
    header.append(node('strong', '', text));
    return header;
  }
  drawChanges(result) {
    this.changes=new TaskChanges({api:this.api,task:this.task,panel:$('tool-panel'),openDiff:(task,path)=>this.review.openDiff(task,path),signal:this.abort.signal,reveal:this.callbacks.reveal,mode:this.fileMode||'flat',onMode:mode=>{this.fileMode=mode;}});
    this.changes.show(result);
  }
  drawArtifacts(artifacts) {
    const task = this.task, panel = $('tool-panel');
    const rows = [...panel.querySelectorAll('.artifact-row')], focusedIndex = rows.indexOf(document.activeElement);
    const focusedFilename = rows[focusedIndex]?.dataset.artifactFilename;
    panel.replaceChildren(this.heading(`${artifacts.length} ${artifacts.length === 1 ? 'artifact' : 'artifacts'}`));
    if (!artifacts.length) panel.append(node('p', 'empty-state', 'No registered artifacts yet. Artifacts saved by this task appear here.'));
    for (const artifact of artifacts) {
      const row = button('', () => this.review.openArtifact(task, artifact), 'artifact-row');
      row.dataset.artifactFilename = artifact.filename;
      const name=node('strong', '', artifact.name || artifact.filename);name.dataset.tooltip=artifact.name || artifact.filename;name.dataset.tooltipOverflow='';name.setAttribute('aria-label',artifact.name || artifact.filename);
      row.append(name, node('span', '', `${artifact.type} · ${sizeLabel(artifact.size)}${artifact.folder ? ` · ${artifact.folder}` : ''}`));
      panel.append(row);
    }
    if (focusedIndex >= 0) {
      const updated = [...panel.querySelectorAll('.artifact-row')];
      const target = updated.find(row => row.dataset.artifactFilename === focusedFilename) || updated[Math.min(focusedIndex, updated.length - 1)] || $('tool-artifacts');
      target.focus({ preventScroll: true });
      log('tools', 'artifact focus restored', { count: updated.length });
    }
  }
  invalidateIdentity(taskId = this.task?.id) {
    if(taskId!==this.task?.id)return;
    this.identity?.abort.abort();this.identity=null;
    log('task-info','identity invalidated');
  }
  async drawStatus() {
    const task = this.task;
    if (!task || this.tool!=='status' || !this.visible()) return;
    const generationAtRead = this.generation;
    if(!this.identity) {
      if(!this.statusView?.list.isConnected)empty($('tool-panel'),'','Loading task details…');
      const identity={abort:new AbortController()};this.identity=identity;
      identity.read=this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/raw`,{signal:identity.abort.signal});
    }
    const identity=this.identity;
    try {
      const raw=await identity.read;
      if(!identity.settled){identity.raw=raw;identity.settled=true;log('task-info','identity loaded');}
    } catch(error) {
      if(!identity.settled){identity.error=error;identity.settled=true;log('task-info','identity unavailable',{status:error.status||0});}
    }
    if(this.identity!==identity||identity.abort.signal.aborted||this.generation!==generationAtRead||this.task?.id!==task.id||this.tool!=='status'||!this.visible())return;
    const current=this.task, raw=identity.raw;
    const rows=[['State', taskState(current)], ['Project', current.project || 'Unassigned'], ['Agent', current.backend || 'Not specified'], ['Model', raw ? raw.model || 'Default' : 'Not available'], ['Account', raw ? raw.account || 'default' : 'Not available'], ['Base branch', raw?.base_branch || 'Not available'], ['Branch', current.branch || 'Not available'], ['Worktree', current.worktree_path || 'Not available'], ['Session', current.live ? (current.idle ? 'Live, idle' : 'Live, busy') : 'Offline'], ['Elapsed', current.elapsed || 'Not available'], ['Review', current.pr_state || 'No review state available'], ['Created', current.created_at || 'Not available'], ['Task ID', current.id]];
    if(current.failure)rows.push(['Failure',current.failure.reason || 'Task reported failure']);
    rows.push(...taskCostRows(current,this.roster),...teamModelRows(current,this.roster));
    const fingerprint=JSON.stringify([rows,current.prompt,!!identity.error]);
    const panel=$('tool-panel');
    const mounted=this.statusView?.list.isConnected;
    if(mounted&&this.statusView.fingerprint===fingerprint)return;
    if(!mounted) {
      const header=this.heading('Task details'), error=node('div'), list=node('dl','status-list'), prompt=node('div'), links=node('div');
      header.append(button('Refresh identity',()=>{this.invalidateIdentity();this.drawStatus();this.callbacks.refreshRoster?.();}));
      panel.replaceChildren(header,error,list,prompt,node('p','section-label','Links'),links);
      this.statusView={error,list,prompt,links};
    }
    const view=this.statusView;view.fingerprint=fingerprint;
    view.error.replaceChildren();
    if(identity.error) {
      const message=node('p','tool-note','Identity unavailable. Refresh identity to retry.');message.setAttribute('role','alert');view.error.append(message);
    }
    view.list.replaceChildren();
    for (const [label,value] of rows) {
      const row=node('div','status-row');row.append(node('dt','',label),node('dd','',value));view.list.append(row);
    }
    view.prompt.replaceChildren();
    if(current.prompt)view.prompt.append(node('p','section-label','Original request'),node('p','status-prompt',current.prompt));
    log('task-info','rendered');
    if(mounted)return;
    const links=view.links;
    const generation = this.generation;
    try { const result = await this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/links`, {signal: this.abort?.signal});
      if (generation !== this.generation || this.task?.id !== task.id || !links.isConnected) return;
      renderTaskLinks(links,result.links || []); if (!links.childNodes.length) links.textContent='No links found in task output.';
      log('task-info','links loaded',{count:result.links?.length || 0});
    } catch(error) { if (generation === this.generation && links.isConnected) { links.setAttribute('role','alert'); links.textContent='Links unavailable. Refresh to retry.'; links.append(button('Refresh',()=>this.load())); log('task-info','links failed'); } }
  }
  clear() { this.mail?.destroy();this.mail=null;this.invalidateIdentity(); this.abort?.abort();this.restoreLoadFocus(); this.generation++; this.task = null; this.roster = null; this.selectedTools.clear(); this.tool = 'changes'; this.selectTab('tool-changes'); }
}
