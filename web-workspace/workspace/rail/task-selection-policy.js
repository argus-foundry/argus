export function scopeState(tasks,ids){const count=tasks.filter(task=>ids.has(task.id)).length;return {checked:tasks.length>0&&count===tasks.length,mixed:count>0&&count<tasks.length};}
export function subtreeScope(id,tasks,parents){return tasks.filter(task=>{const visited=new Set();for(let current=task.id;current&&!visited.has(current);current=parents.get(current)){if(current===id)return true;visited.add(current);}return false;});}
export function selectionIndex(tasks,parents,ids){
 const subtrees=new Map(),projects=new Map(),matching=new Set(tasks.map(task=>task.id));
 for(const task of tasks){subtrees.set(task.id,{total:1,selected:ids.has(task.id)?1:0});const project=task.project||'Other tasks',count=projects.get(project)||{total:0,selected:0};count.total++;if(ids.has(task.id))count.selected++;projects.set(project,count);}
 const children=new Map();
 for(const [child,parent] of parents){if(!subtrees.has(child))subtrees.set(child,{total:0,selected:0});if(!subtrees.has(parent))subtrees.set(parent,{total:0,selected:0});children.set(parent,(children.get(parent)||0)+1);}
 const queue=[...subtrees.keys()].filter(id=>!children.get(id));
 for(let index=0;index<queue.length;index++){const id=queue[index],parent=parents.get(id);if(!subtrees.has(parent))continue;const source=subtrees.get(id),target=subtrees.get(parent);target.total+=source.total;target.selected+=source.selected;children.set(parent,children.get(parent)-1);if(!children.get(parent))queue.push(parent);}
 return {subtrees,projects,matching,total:tasks.length,selected:tasks.filter(task=>ids.has(task.id)).length};
}
