export function patchEvent(tasks,event){
 const id=event.task_id,p=event.payload||{},type=event.type;
 const changes={
  'task.status_changed':{status:p.to},'task.completed':{status:'complete'},'task.renamed':{name:p.to},
  'session.needs_input':{needs_input:!!p.needs_input},'session.idle':{idle:true},'session.started':{live:true,idle:false,needs_input:false},'session.exited':{live:false,idle:false},
  'session.runtime_changed':{live:!!p.live,idle:!!p.live&&!!p.idle,needs_input:!!p.needs_input},
  'task.failure_changed':{failure:p.failure ?? null},
 };
 if(typeof type==='string'&&type.startsWith('resident.'))return {tasks,resnapshot:false};
 if(type==='task.archived')return {tasks:tasks.map(task=>task.id===id?{...task,archive_pending:true}:task),resnapshot:true};
 if(type==='task.deleted')return {tasks:tasks.filter(task=>task.id!==id),resnapshot:false};
 if(type in changes)return {tasks:tasks.map(task=>task.id===id?{...task,...changes[type]}:task),resnapshot:type==='session.runtime_changed'||!tasks.some(task=>task.id===id)};
 return {tasks,resnapshot:!['worktree.changed','message.sent','message.acked','session.focus','task.context_changed','task.usage_changed','hera.board_changed','task.suggestion_changed'].includes(type)};
}
export class SSEFrames {
 constructor(){this.buffer='';this.data=[];this.name='';}
 feed(chunk){this.buffer+=chunk;const events=[];let end;
  while((end=this.buffer.indexOf('\n'))>=0){const line=this.buffer.slice(0,end).replace(/\r$/,'');this.buffer=this.buffer.slice(end+1);
   if(!line){if(this.data.length){try{const event=JSON.parse(this.data.join('\n'));if(!event.type)event.type=this.name;events.push(event);}catch{events.push({type:'resync'});}}this.data=[];this.name='';}
   else if(line.startsWith('data:'))this.data.push(line.slice(5).replace(/^ /,''));else if(line.startsWith('event:'))this.name=line.slice(6).trim();
  }
  return events;
 }
}
