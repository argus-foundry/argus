import { terminalBusy, terminalSize, requestSignal } from './terminal-policy.js';
import { log } from '../core/api.js';

export async function resizeTerminalSession(terminal,size,{automatic,current}) {
  const generation = terminal.generation, signal = terminal.abort.signal;
  const valid = () => !signal.aborted && terminal.generation === generation;
  const [task,authoritative] = await Promise.all([
    automatic ? terminal.api.json(terminal.path(), {signal:requestSignal(signal)}) : terminal.task,
    terminal.api.json(terminal.path('/size'), {signal:requestSignal(signal)}),
  ]);
  if (!valid() || !current() || !terminal.canFit(true)) return false;
  if (!terminalSize(authoritative)) throw new Error('Authoritative terminal size is unavailable');
  const latest=terminal.geometry.propose();
  if(!terminalSize(latest))return false;
  if(automatic && (latest.cols!==size.cols || latest.rows!==size.rows)){terminal.geometry.schedule();log('terminal-fit','geometry changed during preflight',{stale:true});return false;}
  if(!automatic)size=latest;
  terminal.task = {...task};
  if (authoritative.cols===size.cols && authoritative.rows===size.rows) {
    terminal.term.resize(authoritative.cols,authoritative.rows); terminal.geometry.schedule();
    log('terminal-fit','PTY already matched; reconciled cached view',{status:true}); return true;
  }
  if (automatic && terminalBusy(task)) { log('terminal-fit','fresh runtime state deferred fit',{stale:true}); return false; }
  if (!valid() || !current() || !terminal.canFit(!automatic)) return false;
  terminal.stream.expectRerenderUntil = Date.now()+20000;
  try {
    const result = await terminal.api.json(terminal.path('/resize'), {
      method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(size),signal:requestSignal(signal),
    });
    if (!valid()) return false;
    if (!result.rerendered) terminal.stream.expectRerenderUntil = 0;
    terminal.term.resize(size.cols,size.rows); terminal.geometry.schedule();
    if (result.rerendered) terminal.stream.beginRecovery();
    log('terminal-fit',automatic ? 'automatic resize requested' : 'manual resize requested',{status:true});
    return true;
  } catch (error) {
    if (valid()) { terminal.stream.expectRerenderUntil = 0; log('terminal-fit','resize failed',{status:error.status||false}); }
    throw error;
  }
}
