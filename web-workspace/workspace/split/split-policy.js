export function coordinatorContext(taskID,roster={}) {
  return [...(roster.orchestrators||[])].sort((a,b)=>a.id-b.id).find(orch=>(orch.roles||[]).some(role=>role.kind==='coordinator'&&role.live&&role.task_id===taskID))||null;
}
export function secondaryTask(taskID,tasks,roster,chosen,selectedWorker) {
  const available=id=>id&&id!==taskID&&tasks.some(task=>task.id===id);
  if(available(chosen))return chosen;
  const coordinator=coordinatorContext(taskID,roster);
  if(!coordinator)return null;
  if(available(selectedWorker)&&coordinator.roles.some(role=>role.kind==='worker'&&role.live&&!role.archived&&role.task_id===selectedWorker))return selectedWorker;
  return coordinator.roles.find(role=>role.kind==='worker'&&role.live&&!role.archived&&available(role.task_id))?.task_id||null;
}
