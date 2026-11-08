import {node,button} from './dom.js';
import {menuIcon} from './menu-icon.js';
import {log} from '../core/api.js';

export function rowMenu({label,text='⋯',className='task-control task-more',open=false,changed=()=>{},entries=[]}) {
  const menu=node('div','task-menu');menu.setAttribute('popover','auto');menu.setAttribute('role','menu');
  const more=button(text,()=>{if(menu.matches(':popover-open'))menu.hidePopover();else{menu.showPopover();position();menu.querySelector('[role=menuitem]:not(:disabled)')?.focus();}},className);
  more.dataset.tooltip=label;more.setAttribute('aria-label',label);more.setAttribute('aria-haspopup','menu');more.setAttribute('aria-expanded','false');
  for(const entry of entries){
    const control=entry.href?node('a','task-menu-item',entry.label):button(entry.label,()=>{},'task-control task-menu-item');
    control.prepend(menuIcon(entry.icon));
    if(entry.danger){
      const divider=node('div','menu-divider');divider.setAttribute('role','separator');menu.append(divider);
      control.className+=' danger-menu-item';
    }
    control.setAttribute('role','menuitem');if(entry.href)control.href=entry.href;
    if(entry.disabled)control.disabled=true;
    for(const[key,value]of Object.entries(entry.dataset||{}))control.dataset[key]=value;
    control.addEventListener('click',()=>{menu.hidePopover();entry.run?.();});menu.append(control);
  }
  function position() {
    const rect = more.getBoundingClientRect(), width = Math.min(220, innerWidth - 24);
    menu.style.width = `${width}px`; menu.style.left = `${Math.max(12, Math.min(rect.right - width, innerWidth - width - 12))}px`;
    menu.style.top = `${Math.max(12, Math.min(rect.bottom + 4, innerHeight - menu.offsetHeight - 12))}px`;
  }
  menu.addEventListener('beforetoggle', event => {
    more.setAttribute('aria-expanded', String(event.newState === 'open'));
    if(event.newState==='open')log('menu','opened',{count:entries.length});
    if (menu.isConnected) changed(event.newState === 'open');
  });
  menu.addEventListener('keydown', event => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); menu.hidePopover(); more.focus(); return; }
    const controls = [...menu.querySelectorAll('[role=menuitem]:not(:disabled)')], index = controls.indexOf(document.activeElement);
    const next = { ArrowDown: (index + 1) % controls.length, ArrowUp: (index - 1 + controls.length) % controls.length, Home: 0, End: controls.length - 1 }[event.key];
    if (next !== undefined) { event.preventDefault(); controls[next].focus(); }
  });
  more.addEventListener('keydown', event => {
    if (event.key === 'ArrowDown') { event.preventDefault(); menu.showPopover(); position(); menu.querySelector('[role=menuitem]:not(:disabled)').focus(); }
  });
  if (open) queueMicrotask(() => { if (menu.isConnected && more.getClientRects().length) { menu.showPopover(); position(); } else changed(false); });
  return [more,menu];
}
