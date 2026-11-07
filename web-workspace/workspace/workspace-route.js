export function restoreWorkspaceView(state, { newTask, taskSettings, settings, tasks, selectTask, unavailable }) {
  if (!settings.canLeave()) return false;
  newTask.close(false);
  if (state.taskSettings) {
    taskSettings.open(tasks.find(task => task.id === state.taskSettings) || { id: state.taskSettings }, { history: false }); return;
  }
  taskSettings.dismiss();
  if (state.settings) {
    settings.open(state.settings, { history: false });
    return;
  }
  settings.dismiss();
  const task = tasks.find(item => item.id === state.task);
  if (task) selectTask(task, { restore: true, selection: state.selection, view: state.view });
  else if (!state.task) {
    const initial = tasks.find(item => !item.archived);
    if (initial) selectTask(initial, { restore: true, selection: null, view: 'agent' });
  } else unavailable();
}
