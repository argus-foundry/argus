export async function runTaskBatch(tasks,action,request,onProgress=()=>{},signal,onSuccess=()=>{},control={stopped:false},canDispatch=()=>true) {
 const outcomes=new Map();let cursor=0,processed=0;
 if(!['archive','restore','delete'].includes(action))throw new Error('Unknown task action.');
 const result=()=>({succeeded:tasks.filter(task=>outcomes.get(task.id)?.success).map(task=>task.id),failed:tasks.filter(task=>outcomes.get(task.id)?.error).map(task=>({id:task.id,error:outcomes.get(task.id).error})),notStarted:tasks.slice(cursor).filter(task=>!outcomes.has(task.id)).map(task=>task.id),skipped:tasks.filter(task=>outcomes.get(task.id)?.skipped).map(task=>({id:task.id,reason:'The task left the reviewed scope.'}))});
 async function worker(){
  while(cursor<tasks.length&&!signal?.aborted&&!control.stopped){
   const task=tasks[cursor++];
   if(!canDispatch(task)){outcomes.set(task.id,{skipped:true});processed++;onProgress({processed,total:tasks.length,...result()});continue;}
   const path=`/api/tasks/${encodeURIComponent(task.id)}${action==='delete'?'':action==='restore'?'/unarchive':'/archive'}`;
   let error;
   try{await request(path,{method:action==='delete'?'DELETE':'POST',signal:signal?AbortSignal.any([signal,AbortSignal.timeout(120000)]):AbortSignal.timeout(120000)});}
   catch(reason){error=reason;if(!error.status||[401,403].includes(error.status))control.stopped=true;}
   outcomes.set(task.id,error?{error}:{success:true});
   if(!error)await onSuccess(task);
   processed++;onProgress({processed,total:tasks.length,...result()});
  }
 }
 await Promise.all(Array.from({length:Math.min(4,tasks.length)},()=>worker()));for(const task of tasks.slice(cursor))if(!canDispatch(task))outcomes.set(task.id,{skipped:true});return result();
}
