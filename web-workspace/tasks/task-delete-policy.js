import {changedFiles} from './changes/git.js';
export function deleteTaskFacts(task, roles = [], git = null) {
 const changes=git ? changedFiles(git).length : null;
 return [task.name||task.id,`Task ID ${task.id}`,`Project ${task.project||'Unknown'}`,`Branch ${task.branch||'Unavailable'}`,`Worktree ${task.worktree_path||'Unavailable'}`,
  task.live?'Live session will be stopped.':'No live session reported.',
  roles.length?`Team attachments will end (${roles.map(role=>role.name||'Agent').join(', ')}).`:'No team attachments reported.',
  changes===null?'Changed files unavailable.':`${changes} reported changed ${changes===1?'file':'files'}.`,
  `PR state ${task.pr_state||'unavailable'}`].join('\n');
}
export function deletionNeighbor(tasks, removed) {
 const index=tasks.findIndex(task=>removed.has(task.id));
 return tasks.slice(index+1).find(task=>!removed.has(task.id)) || tasks.slice(0,Math.max(0,index)).reverse().find(task=>!removed.has(task.id)) || null;
}
export const deleteEffects='This cannot be undone. Task records, intertask messages and metadata, session logs and artifacts are deleted. Live sessions are stopped best effort and team attachments are ended. Worktrees are force-removed and local and remote branch deletion is attempted in the background. Cleanup completion cannot be verified here.';
