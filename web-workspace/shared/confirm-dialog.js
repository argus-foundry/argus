import {node,button} from './dom.js';
import {log} from '../core/api.js';
export function confirmDialog({title='Confirm action',message='',confirmLabel='Confirm',id='confirm-dialog',signal,onConfirm}={}) {
 const dialog=node('dialog','settings-dialog');dialog.id=id;dialog.setAttribute('aria-labelledby',id+'-title');
 const heading=node('h2','',title);heading.id=id+'-title';const error=node('p','error-text');error.setAttribute('role','alert');error.hidden=true;
 const explanation=node('p','',message);explanation.id=id+'-message';explanation.style.whiteSpace='pre-line';
 const actions=node('div','settings-actions'),previous=document.activeElement;
 let resolve,settled=false;const result=new Promise(done=>resolve=done);
 const finish=value=>{if(settled)return;settled=true;resolve(value);signal?.removeEventListener('abort',cancel);dialog.remove();if(previous?.isConnected)previous.focus({preventScroll:true});};
 const cancel=()=>{dialog.close();finish(false);};
 const cancelButton=button('Cancel',cancel),confirm=button(confirmLabel,async()=>{
  if(confirm.disabled)return;
  if(!onConfirm){dialog.close();finish(true);return;}
  confirm.disabled=true;cancelButton.disabled=true;error.hidden=true;
  try {await onConfirm();if(!signal?.aborted){dialog.close();finish(true);log('confirmation','completed',{status:true});}}
  catch(reason){if(!signal?.aborted){error.textContent=reason.message;error.hidden=false;confirm.disabled=false;cancelButton.disabled=false;log('confirmation','failed',{status:false});}}
 },'primary');
 actions.append(cancelButton,confirm);dialog.append(heading,explanation,error,actions);
 dialog.addEventListener('cancel',event=>{event.preventDefault();if(!confirm.disabled)cancel();});dialog.addEventListener('close',()=>finish(false));
 signal?.addEventListener('abort',cancel,{once:true});result.dialog=dialog;
 if(signal?.aborted){finish(false);return result;}
 document.body.append(dialog);dialog.showModal();cancelButton.focus();log('confirmation','opened');return result;
}
