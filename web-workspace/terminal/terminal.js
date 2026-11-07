import {captureFocus} from '../shared/focus-recovery.js';
import { log } from '../core/api.js';
import { isCtrlZ, isInteractive, requestSignal, terminalSize, terminalBusy } from './terminal-policy.js';
import { TerminalInput } from './terminal-input.js';
import { terminalComposerDraft, terminalPastePending } from './terminal-composer.js';
import { TerminalOutput } from './terminal-output.js';
import { TerminalGeometry } from './terminal-geometry.js';
import { TerminalAutoFit } from './terminal-autofit.js';
import { resizeTerminalSession } from './terminal-resize.js';
import { TerminalStream } from './terminal-stream.js';

export class AgentTerminal {
  constructor(api, container, onState = () => {}, onFit = () => {}, onClipboard = () => {}) {
    Object.assign(this, { api, container, onState, onFit, onClipboard, visible: true, generation: 0 });
  }

  state(message, kind, interactive = this.interactive) {
    this.kind = kind;
    this.interactive = Boolean(interactive) && isInteractive(this.task);
    if (this.term) this.term.options.disableStdin = !this.interactive;
    this.onState({ message, kind, interactive: this.interactive });
    this.autoFit?.check(); this.publishFit();
  }

  async attach(task) {
    if (task?.id === this.task?.id && this.abort && !this.abort.signal.aborted) {
      const becameLive = !isInteractive(this.task) && isInteractive(task);
      this.task = { ...task }; this.autoFit?.check(); this.publishFit();
      if (!becameLive) {
        if (!isInteractive(task)) this.state('Recorded output', 'recorded', false);
        return;
      }
      if (this.stream?.active) {
        this.stream.close();
        this.stream.reconnect();
        return;
      }
    }
    const abort=new AbortController();
    const restore=this.task?.id===task?.id&&this.term?captureFocus(this.container,()=>document.getElementById('draft'),{signal:abort.signal}):()=>{};
    this.detach();
    this.task = task ? { ...task } : null;
    const generation = this.generation;
    if (!task) { this.state('No task selected', 'empty', false); return; }
    this.abort = abort;
    const signal = this.abort.signal;
    const current = () => !signal.aborted && this.generation === generation;
    this.state('Loading terminal…', 'connecting', false);
    try {
      const Terminal = window.Terminal || window.xterm?.Terminal;
      if (!Terminal) throw new Error('Terminal library failed to load');
      let size = { cols: 80, rows: 24 };
      try {
        const authoritative = await this.api.json(this.path('/size'), { signal: requestSignal(signal) });
        if (!terminalSize(authoritative)) throw new Error('Invalid terminal dimensions');
        size = authoritative;
      } catch (error) {
        if (isInteractive(task) || error.status !== 404) throw error;
      }
      if (!current()) return;
      this.host = document.createElement('div');
      this.host.className = 'terminal-host';
      this.container.replaceChildren(this.host);
      const palette = getComputedStyle(this.container);
      const background = palette.getPropertyValue('--background').trim() || '#151312';
      const foreground = palette.getPropertyValue('--text').trim() || '#efebe5';
      this.term = new Terminal({
        cols: size.cols, rows: size.rows, fontSize: 13, lineHeight: 1.2,
        fontFamily: '"SF Mono", Menlo, Consolas, monospace',
        scrollback: 10000, convertEol: false, cursorBlink: true, disableStdin: true,
        scrollOnUserInput: false, allowProposedApi: true, screenReaderMode: true,
        theme: { background, foreground, cursor: foreground },
      });
      this.term.open(this.host);
      // Screen-reader rows must use the same cell font as the visible renderer.
      Object.assign(this.term.element.style, { fontFamily: this.term.options.fontFamily,
        fontSize: `${this.term.options.fontSize}px`, lineHeight: String(this.term.options.lineHeight) });
      const Fit = window.FitAddon?.FitAddon || window.FitAddon;
      if (Fit) { this.fit = new Fit(); this.term.loadAddon(this.fit); }
      this.autoFit = new TerminalAutoFit({ measure:()=>this.geometry?.propose(), current:()=>({cols:this.term.cols,rows:this.term.rows}),
        allowed:manual=>this.canFit(manual), resize:(size,context)=>resizeTerminalSession(this,size,context), changed:()=>this.publishFit() });
      this.geometry = new TerminalGeometry(this.term,this.fit,this.host,this.container,state=>{this.fitState=state;this.autoFit.settle();});
      this.visibilityChanged=()=>{this.autoFit?.check();this.geometry?.schedule();};document.addEventListener('visibilitychange',this.visibilityChanged);
      this.output = new TerminalOutput(this.term, this.host);
      this.resumeInput();
      this.term.attachCustomKeyEventHandler(event => {
        if (!isCtrlZ(event)) return true;
        if (event.type === 'keydown') {
          event.preventDefault();
          this.state('Ctrl+Z is disabled to keep the agent attached', 'warning');
        }
        return false;
      });
      this.term.onData(data => {
        if (!current() || !this.interactive) return;
        const input = this.input;
        Promise.resolve().then(() => { if (current()) return input.raw(data); }).catch(() => {
          if (current() && !input.signal.aborted) this.state('Input was not sent. Check the terminal connection.', 'error', false);
        });
      });
      this.stream = new TerminalStream(this.api, task, signal, {
        clipboard: value => { if (current()) this.onClipboard(task.id, value); },
        clipboardRefresh: () => { if (current()) this.onClipboard(task.id, undefined); },
        currentSize: () => ({cols:this.term?.cols,rows:this.term?.rows}),
        updateTask: task => { if(current()){this.mergeTask(task);this.autoFit?.check();} },
        write: bytes => current() ? this.output.write(bytes) : Promise.resolve(),
        replace: (bytes, size, next) => {
          if (!current()) return Promise.resolve();
          this.mergeTask(next);
          return this.output.replace(bytes, size);
        },
        state: (...args) => { if (current()) this.state(...args); },
      });
      if(this.outputPaused)this.stream.suspend();
      await this.stream.start();
      if (current()) log('terminal', 'attached');
    } catch (error) {
      if (!current()) return;
      this.stream?.destroy();
      this.state(error.message || 'Terminal failed to load', 'error', false);
      log('terminal', 'attach failed');
    } finally { restore(); }
  }

  mergeTask(task) {
    this.task = { ...task, archive_pending: this.task?.archive_pending || false };
  }

  pauseOutput() { this.outputPaused=true;this.autoFit?.check();this.stream?.suspend(); }
  resumeOutput() { this.outputPaused=false;this.autoFit?.check();return this.stream?.resume(); }

  pauseInput() { this.inputAbort?.abort(); }

  resumeInput() {
    this.pauseInput();
    if (!this.abort || this.abort.signal.aborted || !this.term || !this.task) return;
    this.inputAbort = new AbortController();
    const signal = AbortSignal.any([this.abort.signal, this.inputAbort.signal]);
    const generation = this.generation;
    if (this.interactive && this.kind === 'warning') this.state('Live terminal', 'live', true);
    this.input = new TerminalInput(this.api, this.task, signal,
      () => !signal.aborted && generation === this.generation && this.interactive && isInteractive(this.task),
      () => Boolean(this.term?.modes?.bracketedPasteMode),
      () => this.stream ? {bytes:this.stream.cursor,draft:terminalComposerDraft(this.term),pasting:terminalPastePending(this.term)} : null);
  }

  path(suffix = '') {
    return '/api/tasks/' + encodeURIComponent(this.task.id) + suffix;
  }

  reconnect() {
    const task = this.task;
    this.detach();
    return this.attach(task);
  }

  show(visible, mounted = false) {
    const becameVisible = !this.visible && Boolean(visible);
    this.visible = Boolean(visible);
    this.container.hidden = !this.visible;
    if (this.visible && (becameVisible || mounted) && this.term) this.term.refresh(0, this.term.rows - 1);
    this.geometry?.schedule(); this.autoFit?.check(); this.publishFit();
  }

  canFit(manual) {
    return !!(this.interactive && this.visible && !this.outputPaused && this.fit && isInteractive(this.task) && !document.hidden &&
      this.host?.isConnected && !this.host.closest('[inert]') && this.host.getClientRects().length && this.host.clientWidth>0 && this.host.clientHeight>0 && (manual || !terminalBusy(this.task)));
  }
  publishFit() {
    if (!this.fitState) return;
    this.onFit({...this.fitState,needed:!!(this.visible && this.fitState.needed && (this.autoFit?.skipped || !this.canFit(false)))});
  }
  fitSession() { return this.autoFit ? this.autoFit.manual() : Promise.reject(new Error('Show a connected live terminal before fitting')); }

  send(text, task, options) {
    if (!task || task.id !== this.task?.id || !isInteractive(task) || !this.input) {
      return Promise.reject(new Error('Select a connected live task before sending'));
    }
    try { return this.input.send(text, options); }
    catch (error) { return Promise.reject(error); }
  }

  detach() {
    this.generation++;
    this.interactive = false;
    this.abort?.abort(); this.pauseInput();
    this.stream?.destroy();
    this.output?.destroy();
    if(this.visibilityChanged)document.removeEventListener('visibilitychange',this.visibilityChanged);this.visibilityChanged=null;
    this.autoFit?.destroy(); this.autoFit=null; this.fitState=null;
    this.geometry?.destroy(); this.geometry = null;
    this.onFit({needed:false,size:null,current:null});
    this.term?.dispose();
    this.term = this.fit = this.input = this.stream = null;
    this.container.replaceChildren();
  }

  destroy() {
    this.detach();
    this.task = null;
    this.state('No task selected', 'empty', false);
  }
}
