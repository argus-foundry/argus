import { log } from '../../core/api.js';

export async function readProjects(api, signal) {
  try {
    const result = await api.json('/api/projects/full', { signal, revalidate: true });
    const projects = result.projects || [];
    log('projects', 'loaded', { count: projects.length });
    return { projects };
  } catch (error) {
    if (signal.aborted || error.name === 'AbortError' || [401, 403].includes(error.status)) throw error;
    log('projects', 'load failed', { status: error.status || 'error' });
    return { warning: 'Project list unavailable. Refresh tasks to retry.' };
  }
}
