import {confirmDialog} from '../../shared/confirm-dialog.js';
import {notice} from '../../shared/dom.js';
import {log} from '../../core/api.js';

const errors={
  rename_requires_explicit_paths:'This rename requires both paths to be handled explicitly. Use your local git tools.',
  untracked_requires_delete:'This untracked file requires explicit deletion. Review its current state before confirming again.',
  conflict_state:'Resolve the file conflict before discarding local changes.',
  no_changes:'There are no local changes to discard. Committed changes are preserved.',
  task_archived:'This task is archived. Restore it before discarding local changes.',
  invalid_path:'The file path is invalid or cannot be discarded safely.',
  invalid_request:'The discard request is invalid.',
  invalid_scope:'Only the local discard scope is supported.',
  task_not_found:'The task is unavailable. Refresh tasks before trying again.',
  worktree_not_found:'The task worktree is unavailable. Check it before trying again.',
};
export async function discardRequest(api,task,path,untracked,signal) {
  try {
    const response=await api.request(`/api/tasks/${encodeURIComponent(task.id)}/git/discard`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path,scope:'local',delete_untracked:untracked}),signal,allowErrorResponse:true});
    const result=await response.json();signal?.throwIfAborted();
    if(!response.ok)throw Object.assign(new Error(errors[result.code]||`Discard was not confirmed (${response.status}). Check the file before trying again.`),{status:response.status});
    if(result.discarded!==true)throw new Error('Discard was not confirmed. Check the file before trying again.');
    log('changes-discard','discarded',{status:response.status});return result;
  } catch(error) {log('changes-discard','failed',{status:error.status||0});throw error;}
}
export function discardFile({api,task,file,signal,confirm=confirmDialog}) {
  const captured={id:task.id},path=file.path,untracked=file.status.includes('?');
  return confirm({title:untracked?'Delete untracked file?':'Discard local file changes?',message:untracked?`Permanently delete ${path} from this task worktree? This cannot be undone.`:`Restore ${path} to HEAD? Staged and unstaged local edits will be lost. Committed branch changes are preserved.`,confirmLabel:untracked?'Delete file':'Discard changes',danger:true,signal,onConfirm:async()=>{
    await discardRequest(api,captured,path,untracked,signal);
    if(!signal?.aborted)notice(untracked?'Untracked file deleted. Waiting for the worktree update.':'Local changes discarded. Waiting for the worktree update.');
  }});
}
