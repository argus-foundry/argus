import {node,button} from './dom.js';
import {confirmTaskList} from './confirm-task-list.js';
import {runTaskBatch} from '../tasks/task-batch-policy.js';
import {log} from '../core/api.js';
export function confirmDialog({title='Confirm action',message='',confirmLabel='Confirm',id='confirm-dialog',signal,onConfirm,content,danger=false,restoreFocus,tasks,taskAction,roster={},links=new Map(),enrich,request,onTaskSuccess,onComplete,onBusy,keepFailuresOpen=false,validate,canDispatch}={}) {
 danger=danger||taskAction==='delete';
 const taskList=tasks?confirmTaskList(tasks,{action:taskAction,roster,links,enrich,signal}):null;
 if(taskList)content=taskList.body;
 const dialog=node('dialog','settings-dialog');if(tasks)dialog.classList.add('task-confirm-dialog');dialog.id=id;if(danger)dialog.classList.add('danger-dialog');dialog.setAttribute('aria-labelledby',id+'-title');dialog.setAttribute('aria-describedby',id+'-message');
 const heading=node('h2','',title);heading.id=id+'-title';const error=node('p','error-text');error.setAttribute('role','alert');error.hidden=true;error.tabIndex=-1;
 const explanation=node('p','',message);explanation.id=id+'-message';explanation.style.whiteSpace='pre-line';
 const actions=node('div','settings-actions'),previous=document.activeElement;
 let resolve,settled=false,control;const progress=node('p','task-action-progress');progress.setAttribute('role','status');progress.setAttribute('aria-live','polite');progress.hidden=true;
 const stop=button('Stop',()=>{if(control)control.stopped=true;stop.disabled=true;log('task-action','stop requested');});stop.hidden=true;
 const result=new Promise(done=>resolve=done);
 const finish=value=>{if(settled)return;settled=true;resolve(value);signal?.removeEventListener('abort',cancel);dialog.remove();if(restoreFocus)restoreFocus();else if(previous?.isConnected)previous.focus({preventScroll:true});};
 const cancel=()=>{dialog.close();finish(false);};
 const cancelButton=button('Cancel',cancel),confirm=button(confirmLabel,async()=>{
  if(confirm.disabled)return;
  if(!onConfirm&&!(tasks&&request)){dialog.close();finish(true);return;}
  confirm.disabled=true;cancelButton.disabled=true;error.hidden=true;
  try {
   if(validate&&!validate())throw new Error('The reviewed task scope changed. Review the selection again.');
   onBusy?.(true);
   if(tasks&&request){
    control={stopped:false};content.hidden=true;explanation.hidden=true;confirm.hidden=true;cancelButton.hidden=true;stop.hidden=false;stop.disabled=false;progress.hidden=false;stop.focus({preventScroll:true});
    const verb={delete:'deleted',archive:'archived',restore:'restored'}[taskAction];
    progress.textContent=`0 of ${tasks.length} ${verb}`;
    await taskList.ready;signal?.throwIfAborted();
    const outcome=await runTaskBatch(tasks,taskAction,request,state=>{progress.textContent=`${state.succeeded.length} of ${state.total} ${verb}${state.failed.length?`, ${state.failed.length} failed`:``}`;log('task-action','progress',{count:state.processed,status:!state.failed.length&&!state.skipped.length});},signal,onTaskSuccess,control,canDispatch);
    if(!signal?.aborted){log('task-action','finished',{count:outcome.succeeded.length,status:!outcome.failed.length&&!outcome.notStarted.length&&!outcome.skipped.length});await onComplete?.(outcome);if(keepFailuresOpen&&outcome.failed.length)throw new Error(`${taskAction==='delete'?'Deletion was not confirmed. Check the task before retrying.':'Task was not updated.'} ${outcome.failed[0].error.message}`);}
   }else await onConfirm?.();if(!signal?.aborted){dialog.close();finish(true);log('confirmation','completed',{status:true});}}
  catch(reason){if(!signal?.aborted){if(tasks){content.hidden=false;explanation.hidden=false;confirm.hidden=false;cancelButton.hidden=false;}error.textContent=reason.message;error.hidden=false;confirm.disabled=false;cancelButton.disabled=false;if(dialog.open&&(dialog.contains(document.activeElement)||document.activeElement===document.body))error.focus({preventScroll:true});log('confirmation','failed',{status:false});}}
  finally{onBusy?.(false);stop.hidden=true;}
 },danger?'primary danger':'primary');
 actions.append(cancelButton,...(tasks&&request?[stop]:[]),confirm);dialog.append(heading,explanation,...(content?[content]:[]),...(tasks&&request?[progress]:[]),error,actions);
 dialog.addEventListener('cancel',event=>{event.preventDefault();if(!confirm.disabled)cancel();});dialog.addEventListener('close',()=>finish(false));
 signal?.addEventListener('abort',cancel,{once:true});result.dialog=dialog;
 if(signal?.aborted){finish(false);return result;}
 document.body.append(dialog);dialog.showModal();cancelButton.focus();log('confirmation','opened');return result;
}
