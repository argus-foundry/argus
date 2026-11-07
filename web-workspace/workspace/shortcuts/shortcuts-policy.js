export const shortcutHelp = [
  ['Alt+Shift+D / Cmd+Shift+Enter (terminal)', 'Toggle split view'],
  ['Alt+1 / 2 / 3 / 4', 'Agent / Review / Changes / Info'],
  ['Alt+Shift+E / U', 'Copy worktree path / Open PR'],
  ['Alt+Shift+N / R / B', 'New task / Rename / Fork'],
  ['Alt+Shift+A / P / Backspace', 'Archive / Pin / Delete confirmation'],
  ['Alt+Shift+F / J / S / ?', 'Filter / Next needs input / Schedules / Help'],
  ['Cmd+Up / Down', 'Previous / Next task (terminal only)'],
  ['Cmd+Left / Right', 'Cycle detail tabs (terminal only)'],
  ['Shift+Up / Down / PageUp / PageDown / End', 'Scroll terminal without sending input'],
  ['Cmd+Shift+C / U', 'Copy visible output / Open PR (terminal only)'],
];
export function shortcutAction(event, terminal = false) {
  if (event.isComposing || event.keyCode === 229 || event.repeat) return null;
  const key = event.key.toLowerCase();
  if (terminal) {
    if (event.ctrlKey || event.altKey) return null;
    if (event.metaKey && !event.shiftKey) return {arrowup:'previous',arrowdown:'next',arrowleft:'tab-previous',arrowright:'tab-next'}[key] || null;
    if (event.metaKey && event.shiftKey) return {enter:'split',c:'copy-output',u:'open-pr'}[key] || null;
    if (!event.metaKey && event.shiftKey) return {arrowup:'line-up',arrowdown:'line-down',pageup:'page-up',pagedown:'page-down',end:'bottom'}[key] || null;
    return null;
  }
  if (event.ctrlKey || event.metaKey || !event.altKey) return null;
  const physical = /^Key[A-Z]$/.test(event.code || '') ? event.code.slice(3).toLowerCase() : /^Digit[1-4]$/.test(event.code || '') ? event.code.slice(5) : event.code === 'Slash' ? '/' : key;
  if (!event.shiftKey) return {'1':'agent','2':'review','3':'files','4':'info'}[physical] || null;
  return {d:'split',e:'open-worktree',u:'open-pr',n:'new',r:'rename',b:'fork',a:'archive',p:'pin',backspace:'delete',f:'filter',j:'attention',s:'schedules','?':'help','/':'help'}[physical] || null;
}
