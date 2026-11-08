import {log} from '../core/api.js';
const selector='button,input,select,textarea,a[href],summary,[tabindex="0"]';
const identity=control=>control.dataset.focusKey || (control.dataset.taskId?JSON.stringify([control.dataset.taskId,control.dataset.taskAction||'row']):null) || control.id || control.getAttribute('name') || control.getAttribute('aria-label') || control.textContent;
const available=control=>{
  if(!control?.isConnected || control.disabled || control.closest('[hidden],[inert]') || control.closest('fieldset[disabled]') || !control.getClientRects().length)return false;
  for(let ancestor=control.parentElement;ancestor;ancestor=ancestor.parentElement){
    if(ancestor.tagName.toLowerCase()!=='details' || ancestor.open)continue;
    const summary=[...ancestor.children].find(child=>child.tagName.toLowerCase()==='summary');
    if(!summary?.contains(control))return false;
  }
  return true;
};

export function captureFocus(root, fallback, {neighbors=true,signal,selector:focusSelector=selector}={}) {
  if(!root)return ()=>{};
  const origin=document.activeElement,owned=root.contains(origin);
  if(!owned)return ()=>{};
  const controls=[...root.querySelectorAll(focusSelector)].filter(control=>control===origin||available(control)),index=controls.indexOf(origin),key=identity(origin);
  const peers=controls.filter(control=>control.tagName===origin.tagName && control.textContent===origin.textContent),peerIndex=peers.indexOf(origin);
  let departed=false;
  const moved=event=>{if(event.target!==document.body && event.target!==origin && !event.target.closest('dialog'))departed=true;};
  document.addEventListener('focusin',moved);
  return ()=>{
    document.removeEventListener('focusin',moved);
    if(departed || signal?.aborted || !root.isConnected)return;
    const active=document.activeElement;
    if(active!==document.body && active!==origin && !active.closest('dialog'))return;
    const fresh=[...root.querySelectorAll(focusSelector)].filter(available);
    const matching=fresh.filter(control=>control.tagName===origin.tagName && identity(control)===key);
    const sameAction=fresh.filter(control=>control.tagName===origin.tagName && control.textContent===origin.textContent);
    const targets=[(available(origin)?origin:null),matching[Math.min(peerIndex,matching.length-1)],
      (neighbors?sameAction[Math.min(peerIndex,sameAction.length-1)]:null),
      (neighbors?fresh[Math.min(index,fresh.length-1)]:null),...(neighbors?fresh:[])];
    for(const target of new Set([...targets,fallback?.()])){
      if(!available(target))continue;
      target.focus({preventScroll:true});
      if(document.activeElement===target){log('focus','mutation focus restored');return;}
    }
  };
}

export function captureDialogFocus(signal) {
  const root=document.activeElement?.closest?.('.task-settings-panel,.settings-section,#task-groups');
  if(!root){const origin=document.activeElement;return ()=>{if(origin?.isConnected)origin.focus({preventScroll:true});};}
  const taskSettings=root.classList.contains('task-settings-panel'),rail=root.id==='task-groups';
  const fallback=()=>taskSettings?root.querySelector('button'):rail?
    document.querySelector('.nav-filters [aria-pressed="true"]') || document.getElementById('active-filter') || document.getElementById('archive-filter'):
    document.querySelector('.settings-back');
  return captureFocus(root,fallback,{neighbors:!taskSettings,signal,selector:rail?'.task-row,.task-archive,.task-more':selector});
}
