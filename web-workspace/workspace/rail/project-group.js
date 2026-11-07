import { node, button } from '../../shared/dom.js';
import {railChevron} from './rail-chevron.js';

export function projectGroup(project, entries, { collapsed, onCollapse, onCreate, onConfigure, renderTask, renderEntries,selection }) {
  const group = node(entries.length ? 'details' : 'section', 'project-group');
  group.dataset.project = project;
  if (entries.length) group.open = !collapsed;
  const header = node(entries.length ? 'summary' : 'div', 'project-header');
  header.dataset.project = project;
  header.dataset.tooltip = project; header.dataset.tooltipOverflow = '';
  const needsAttention=entries.some(entry=>entry.needsInput||entry.failed);header.classList.toggle('needs-attention',needsAttention);group.classList.toggle('needs-attention',needsAttention);
  if (entries.length) {
    const caret = node('span', 'project-caret'); caret.setAttribute('aria-label', `Toggle ${project}`); caret.dataset.tooltip = `Toggle ${project}`; caret.append(railChevron('project-chevron')); header.append(caret);
    header.addEventListener('click', event => {
      if (event.target.closest('button,input, [role="menu"]')) return;
      event.preventDefault(); group.open = !group.open; onCollapse(!group.open);
    });
  }
  const avatar = node('span', 'project-avatar', [...project.trim()][0]?.toLocaleUpperCase() || '?');
  avatar.setAttribute('aria-hidden', 'true');
  header.append(avatar, node('span', 'project-name', project));
  const configure=button('',()=>onConfigure(project),'project-action project-configuration');
  configure.setAttribute('aria-label',`Configuration for ${project}`);configure.dataset.tooltip=`Configuration for ${project}`;
  configure.append(projectActionIcon('m9.5 3-.7 2.2-2 .9-2.1-.9-2.4 4.2 1.6 1.7v2.3l-1.6 1.7 2.4 4.2 2.1-.9 2 .9.7 2.2h5l.7-2.2 2-.9 2.1.9 2.4-4.2-1.6-1.7v-2.3l1.6-1.7-2.4-4.2-2.1.9-2-.9-.7-2.2zM15 12a3 3 0 1 0-6 0 3 3 0 0 0 6 0'));
  const add=button('',()=>onCreate(project,'new'),'project-action project-new-task');
  add.append(projectActionIcon('M12 4v16M4 12h16'));
  add.setAttribute('aria-label',`New task in ${project}`);add.dataset.tooltip=`New task in ${project}`;
  for(const [control,action] of [[configure,'configuration'],[add,'new']]){control.dataset.project=project;control.dataset.projectAction=action;}
  if(selection?.active)header.append(selection.projectCheckbox(project));else{if(onConfigure)header.append(configure);header.append(add);}group.append(header);
  if(entries.length){const children=node('div','project-children');children.append(...(renderEntries?renderEntries(entries):entries.map(renderTask)));group.append(children);}
  return group;
}

function projectActionIcon(path) {
  const icon=document.createElementNS('http://www.w3.org/2000/svg','svg');
  icon.setAttribute('viewBox','0 0 24 24');icon.setAttribute('aria-hidden','true');
  const shape=document.createElementNS(icon.namespaceURI,'path');shape.setAttribute('d',path);
  icon.append(shape);return icon;
}
