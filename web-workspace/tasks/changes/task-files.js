import {node,button} from '../../shared/dom.js';
import {changedFileTree,fileCopyPath} from '../task-information.js';
import {copyNotice} from '../../shared/clipboard.js';
import {rowMenu} from '../../shared/row-menu.js';

export function editorFileURL(worktree,path) {
  if (!worktree?.startsWith('/')) return null;
  return 'vscode://file'+fileCopyPath(worktree,path).split('/').map(encodeURIComponent).join('/');
}
export function countLabel(counts) {
  if (!counts) return '';
  if (counts.kind) return {binary:'bin',directory:'',unavailable:''}[counts.kind];
  return `+${counts.added} −${counts.removed}`;
}
export function paintCounts(host,counts) {
  host.replaceChildren();
  const label=!counts?'Loading line counts':counts.kind?{binary:'Binary diff',directory:'Expand directory to inspect its files',unavailable:'Line counts unavailable'}[counts.kind]:`${counts.added} lines added, ${counts.removed} removed`;
  host.dataset.tooltip=label;host.setAttribute('aria-label',label);
  const row=host.closest('.file-row');if(row?.dataset.fileSummary)row.setAttribute('aria-label',`${row.dataset.fileSummary}, ${label}`);
  if(!counts||counts.kind){host.textContent=countLabel(counts);return;}
  host.append(node('span','added',`+${counts.added}`),document.createTextNode(' '),node('span','removed',`−${counts.removed}`));
}
export function fileRow(task,file,{openDiff,expandDirectory,counts}) {
  const container=node('div','file-entry'),line=node('div','file-line');
  const row=button('',()=>file.directory?expandDirectory(file,row,container):openDiff(task,file.path),'file-row');
  row.dataset.tooltip=file.path;row.dataset.tooltipOverflow='';row.dataset.filePath=file.path;row.dataset.directory=String(!!file.directory);row.setAttribute('aria-label',`${file.directory?'Expand directory':'Open diff for'} ${file.path}`);
  const path=node('span','file-path'),name=file.name||file.path.replace(/\/$/,'').split('/').at(-1)+(file.directory?'/':'');
  const prefix=file.name?'':file.path.slice(0,file.path.length-name.length);
  path.append(node('span','file-prefix',prefix),node('span','file-name',name));
  const numbers=node('span','file-counts');numbers.dataset.filePath=file.path;
  const status=file.status.includes('D')?'D':file.status.includes('?')?'?':file.status.includes('A')?'A':file.status.startsWith('R')?'R':'M';
  const badge=node('span',`file-status ${status==='D'?'removed':status==='A'||status==='?'?'added':''}`,status);
  const statusLabel={A:'Added',M:'Modified',D:'Deleted','?':'Untracked',R:'Renamed'}[status];badge.dataset.tooltip=statusLabel;badge.setAttribute('aria-label',statusLabel);
  row.dataset.fileSummary=`${row.getAttribute('aria-label')}, ${statusLabel}`;
  row.append(path,numbers,badge);paintCounts(numbers,counts);
  if(file.directory)row.setAttribute('aria-expanded','false');
  const entries=[{label:'Copy path',icon:'copy',run:()=>copyNotice(file.path,'Copied relative path.')},{label:'Copy absolute path',icon:'folder',disabled:!task.worktree_path?.startsWith('/'),run:()=>copyNotice(fileCopyPath(task.worktree_path,file.path),'Copied absolute path.')}];
  const href=editorFileURL(task.worktree_path,file.path);if(href)entries.push({label:'Open in VS Code',icon:'external',href});
  line.append(row,...rowMenu({label:`File actions for ${file.path}`,text:'⌄',className:'file-more',entries}));container.append(line);return container;
}
export function drawTaskFiles(panel,files,renderFile) {
  function row(file) {
    if (!file.directory||!file.children.length) return renderFile(file);
    const details=node('details','file-directory'),summary=node('summary','file-folder',file.name);summary.dataset.tooltip=file.path;summary.dataset.tooltipOverflow='';summary.setAttribute('aria-label',file.path);details.open=true;details.append(summary);
    const children=node('div','file-tree-children');for(const child of file.children)children.append(row(child));details.append(children);return details;
  }
  for(const file of changedFileTree(files))panel.append(row(file));
}
