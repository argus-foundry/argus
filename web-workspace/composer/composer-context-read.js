export async function readContext(api, taskID, signal) {
  const response = await api.request(`/api/tasks/${encodeURIComponent(taskID)}/context`, {signal,allowErrorResponse:true});
  if (response.status === 404) return undefined;
  if (!response.ok) throw Object.assign(new Error('Context unavailable'),{status:response.status});
  const type = response.headers.get('content-type') || '';
  if (type.includes('text/html')) return undefined;
  const value = await response.json();
  if (value?.task_id !== taskID || typeof value.live !== 'boolean' ||
      typeof value.session_id !== 'string' || !(value.context === null || value.context && typeof value.context === 'object')) {
    throw new Error('Invalid task context');
  }
  return value;
}
