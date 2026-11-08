import {confirmDialog} from '../../shared/confirm-dialog.js';
import {sessionActions} from './task-settings-policy.js';
export function confirmTaskAction(task,operation,run,signal){
 const [label,message]=sessionActions[operation];
 return confirmDialog({title:`${label} for ${task.name}?`,message,id:'task-action-dialog',signal,onConfirm:run}).dialog;
}
