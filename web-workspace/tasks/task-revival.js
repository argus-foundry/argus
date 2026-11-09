import {confirmDialog} from '../shared/confirm-dialog.js';
import {log} from '../core/api.js';

export async function reviveTask(api, task, operation, body, signal, confirm = options => confirmDialog(options)) {
 const path='/api/tasks/'+encodeURIComponent(task.id)+'/'+operation;
 const request=payload=>api.json(path,{method:'POST',signal,...(payload?{headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)}:{})});
 try { const result=await request(body);log('task-revival','accepted');return result; }
 catch(error){
  log('task-revival','rejected',{status:error.status||0});
  if(!['resume','restart'].includes(operation)||error.status!==409||error.code!=='hera_closeout'||!error.canForceRevive)throw error;
  const accepted=await confirm({title:`Revive ${task.name}?`,message:'This Hera task awaits close-out. Reviving it reopens the task and clears its close-out markers.',confirmLabel:'Revive task',id:'task-revival-dialog',signal});
  signal?.throwIfAborted();
  if(!accepted)throw new Error('Revival canceled. The task remains closed out.');
  try {const result=await request({...body,force_revive:true});log('task-revival','forced revival accepted');return result;}
  catch(failure){log('task-revival','forced revival failed',{status:failure.status||0});throw failure;}
 }
}
