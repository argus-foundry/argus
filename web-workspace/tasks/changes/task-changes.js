import {discardFile} from './discard-file.js';
import {comparisonText} from './comparison.js';
import {captureFocus} from '../../shared/focus-recovery.js';
import {node,button,errorMessage} from '../../shared/dom.js';
import {changedFiles,gitPath} from './git.js';
import {drawTaskFiles,fileRow,paintCounts} from './task-files.js';
import {ChangedFileCounts,statTotals} from './change-counts.js';
import {log} from '../../core/api.js';

export class TaskChanges {
  constructor({api,task,panel,openDiff,signal,mode='flat',onMode,reveal}) {
    Object.assign(this,{api,task,panel,openDiff,signal,mode,onMode,reveal});
    this.localPaths=new Set();
    this.counts=new ChangedFileCounts(api,task,signal,()=>this.updateCounts());
    signal.addEventListener('abort',()=>this.observer?.disconnect(),{once:true});
  }
  show(result) {
    const files=changedFiles(result),totals=statTotals(result),revisionChanged=this.revision!==result.git_revision,comparisonChanged=JSON.stringify(this.comparison)!==JSON.stringify(result.comparison);
    this.localPaths=new Set(changedFiles({Status:result.Status}).map(file=>file.path));
    this.comparison=result.comparison;this.stale=!!result.stale;this.watchDegraded=!!result.watch_degraded;
    const summaries=new Map(),stats=new Map();
    for(const stat of [result.BranchDiff,result.Diff])for(const line of (stat||'').split('\n')) {
      const separator=line.lastIndexOf('|');if(separator<0)continue;
      const path=gitPath(line.slice(0,separator).trim());
      stats.set(path,[...(stats.get(path)||[]),line.slice(separator+1)]);
    }
    for(const file of files) {
      const lines=stats.get(file.path)||[];
      summaries.set(file.path,JSON.stringify([file.status,lines,this.localPaths.has(file.path)]));
    }
    const fingerprint=JSON.stringify([[...summaries],totals,this.comparison,this.stale,this.watchDegraded]);
    const visibleChanged=fingerprint!==this.fingerprint;
    const changed=new Set([...summaries.keys(),...(this.summaries?.keys()||[])]);
    for(const path of changed)if(comparisonChanged||revisionChanged&&this.localPaths.has(path)||this.summaries?.get(path)!==summaries.get(path))this.counts.invalidate(path);
    this.revision=result.git_revision;this.fingerprint=fingerprint;this.summaries=summaries;this.files=files;this.totals=totals;
    if(visibleChanged)this.draw();else if(revisionChanged){this.updateCounts();this.observeRows();}
    if(revisionChanged)for(const file of files.filter(file=>file.directory)) {
      const row=[...this.list.querySelectorAll('.file-row')].find(row=>row.dataset.filePath===file.path);
      const container=row?.closest('.file-entry');
      if(container?.querySelector(':scope > .directory-files')) {
        for(const path of this.counts.results.keys())if(path.startsWith(file.path))this.counts.invalidate(path);
        void this.expandDirectory(file,row,container,true);
      }
    }
    return visibleChanged;
  }
  draw() {
    const scroll=this.panel.scrollTop;
    const folders=new Map([...this.panel.querySelectorAll('details.file-directory')].map(item=>[item.querySelector('summary').getAttribute('aria-label'),item.open]));
    const openMenu=this.panel.querySelector('.task-menu:popover-open');
    const focused=document.activeElement;
    const listFocused=this.list?.contains(focused);
    const focusedEntry=listFocused?focused.closest('.file-entry'):null;
    const focusedPath=focusedEntry?.querySelector('.file-row')?.dataset.filePath;
    const focusedFolder=listFocused&&focused.matches('summary.file-folder')?focused.getAttribute('aria-label'):null;
    const focusedControl=focused?.matches('.file-more')?'.file-more':'.file-row';
    const oldControls=[...this.list?.querySelectorAll('.file-row, summary.file-folder')||[]];
    const focusedIndex=oldControls.indexOf(focusedEntry?.querySelector('.file-row')||focused);
    const rowIndex=oldControls.slice(0,Math.max(0,focusedIndex)).filter(control=>control.matches('.file-row')).length;
    const toggleFocused=focused && focused===this.panel.querySelector('.changes-view-toggle');
    const menuPath=openMenu?.closest('.file-entry')?.querySelector('.file-row')?.dataset.filePath;
    const menuIndex=openMenu?[...openMenu.children].indexOf(focused):-1;
    const oldRows=new Map([...this.list?.querySelectorAll('.file-entry')||[]].map(entry=>[entry.querySelector('.file-row').dataset.filePath,entry]));
    const header=node('div','tool-summary changes-heading');
    header.append(node('strong','',`${this.files.length} ${this.files.length===1?'file':'files'} changed`));
    this.total=node('span','changes-totals');header.append(this.total);
    const toggle=button('',()=>{this.mode=this.mode==='flat'?'tree':'flat';this.onMode?.(this.mode);this.draw();},'changes-view-toggle');
    toggle.setAttribute('aria-label',this.mode==='flat'?'Show tree view':'Show flat view');toggle.dataset.tooltip=toggle.getAttribute('aria-label');
    toggle.setAttribute('aria-pressed',String(this.mode==='tree'));toggle.append(node('span','',this.mode==='flat'?'☷':'⊞'));
    header.append(toggle);
    this.list=node('div','changes-list');
    const comparison=node('p','tool-note changes-comparison',comparisonText(this.comparison));comparison.setAttribute('role','status');this.panel.replaceChildren(header,comparison,this.list);
    if(this.stale||this.watchDegraded){const note=node('p','tool-note',this.stale?'Git snapshot is stale. Checking for current changes…':'Live file watching is degraded. Changes reconcile when the daemon refreshes.');note.setAttribute('role','status');this.panel.replaceChildren(header,comparison,note,this.list);}
    if (!this.files.length) this.list.append(node('p','empty-state','No changes yet'));
    const render=file=>{
      const previous=oldRows.get(file.path);
      if(previous&&previous.dataset.status===file.status&&previous.dataset.local===String(this.localPaths.has(file.path)))return previous;
      const entry=fileRow(this.task,file,{openDiff:this.openDiff,expandDirectory:(...args)=>this.expandDirectory(...args),counts:this.counts.results.get(file.path),reveal:this.reveal,signal:this.signal,discard:this.localPaths.has(file.path)&&!this.task.archived?file=>this.discard(file):null});entry.dataset.local=String(this.localPaths.has(file.path));entry.dataset.status=file.status;return entry;
    };
    if(this.mode==='tree')drawTaskFiles(this.list,this.files,render);else for(const file of this.files)this.list.append(render(file));
    for(const folder of this.list.querySelectorAll('details.file-directory')) {
      const path=folder.querySelector('summary').getAttribute('aria-label');if(folders.has(path))folder.open=folders.get(path);
    }
    this.panel.scrollTop=scroll;
    const restoredMenu=[...this.list.querySelectorAll('.file-entry')].find(entry=>entry.querySelector('.file-row').dataset.filePath===menuPath);
    if(restoredMenu){restoredMenu.querySelector('.file-more').click();if(menuIndex>=0)restoredMenu.querySelector('.task-menu').children[menuIndex]?.focus({preventScroll:true});}
    if(listFocused&&!(restoredMenu&&menuIndex>=0)) {
      const rows=[...this.list.querySelectorAll('.file-row')];
      const entry=rows.find(row=>row.dataset.filePath===focusedPath)?.closest('.file-entry');
      const folder=[...this.list.querySelectorAll('summary.file-folder')].find(control=>control.getAttribute('aria-label')===focusedFolder);
      const target=folder||entry?.querySelector(focusedControl)||rows[Math.min(rowIndex,rows.length-1)]||document.getElementById('tool-changes');
      target.focus({preventScroll:true});
      log('tools','changes focus restored',{count:rows.length});
    }
    if(toggleFocused)toggle.focus({preventScroll:true});
    if(!listFocused&&focused?.isConnected&&focused!==document.activeElement)focused.focus({preventScroll:true});
    this.updateCounts();this.observeRows();
  }
  updateCounts() {
    if(this.signal.aborted||!this.panel.isConnected)return;
    for(const control of this.list.querySelectorAll('.file-counts'))paintCounts(control,this.counts.results.get(control.dataset.filePath));
    paintCounts(this.total,this.totals||{kind:'unavailable'});
  }
  observeRows() {
    this.observer?.disconnect();
    this.observer=new IntersectionObserver(entries=>{
      if(this.signal.aborted)return;
      const visible=entries.filter(entry=>entry.isIntersecting).map(entry=>({path:entry.target.dataset.filePath,directory:entry.target.dataset.directory==='true'}));
      this.counts.add(visible);
    },{root:this.panel});
    for(const row of this.list.querySelectorAll('.file-row'))this.observer.observe(row);
  }
  discard(file) {return discardFile({api:this.api,task:this.task,file,signal:this.signal});}
  async expandDirectory(file,row,container,refresh=false) {
    const previous=container.querySelector(':scope > .directory-files');
    if(previous&&!refresh){previous.remove();row.setAttribute('aria-expanded','false');return;}
    const restore=captureFocus(container,()=>document.getElementById('tool-changes'),{signal:this.signal});
    const content=previous||node('div','directory-files');const version=content.readVersion=(content.readVersion||0)+1;container.append(content);row.setAttribute('aria-expanded','true');row.disabled=true;
    if(!previous)content.append(node('p','tool-note','Loading directory…'));
    try {
      const result=await this.api.json(`/api/tasks/${encodeURIComponent(this.task.id)}/files?dir=${encodeURIComponent(file.path)}`,{signal:this.signal});
      if(this.signal.aborted||!content.isConnected||version!==content.readVersion)return;
      const restoreContent=captureFocus(content,()=>row,{signal:this.signal});
      const retained=new Map([...content.children].filter(entry=>entry.matches('.file-entry')).map(entry=>[entry.querySelector('.file-row').dataset.filePath,entry]));
      const files=(result.Files||[]).map(item=>({path:gitPath(item.Path),status:file.status.includes('?')?'??':item.Status||'?',directory:item.IsDir}));
      const entries=files.map(child=>{
        const previous=retained.get(child.path),oldRow=previous?.querySelector('.file-row');
        if(previous&&previous.dataset.status===child.status&&oldRow.dataset.directory===String(!!child.directory))return previous;
        const entry=fileRow(this.task,child,{openDiff:this.openDiff,expandDirectory:(...args)=>this.expandDirectory(...args),counts:this.counts.results.get(child.path),reveal:this.reveal,signal:this.signal,discard:!this.task.archived?file=>this.discard(file):null});
        entry.dataset.status=child.status;return entry;
      });
      content.replaceChildren(...entries);row.disabled=false;restoreContent();
      for(let index=0;index<files.length;index++)if(files[index].directory&&entries[index].querySelector(':scope > .directory-files'))void this.expandDirectory(files[index],entries[index].querySelector('.file-row'),entries[index],true);
      this.observeRows();
      if(!files.length)content.append(node('p','tool-note','No changed files returned for this directory.'));
      log('tools','directory loaded',{count:files.length});
    } catch(error) {
      if(this.signal.aborted||!content.isConnected||version!==content.readVersion)return;
      const restoreContent=captureFocus(content,()=>row,{signal:this.signal});
      content.replaceChildren(errorMessage(`Directory unavailable. ${error.message}`,'tool-note'));row.disabled=false;restoreContent();log('tools','directory failed',{status:error.status||0});
    } finally {if(!this.signal.aborted&&version===content.readVersion)row.disabled=false;restore();}
  }
}
