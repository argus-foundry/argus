import {log} from '../core/api.js';
import {AgentTerminal} from './terminal.js';

export const TERMINAL_CACHE_LIMIT = 8;

export class TaskTerminalCache {
  constructor(api,container,onState=()=>{},onFit=()=>{},create=(...args)=>new AgentTerminal(...args)) {
    Object.assign(this,{api,container,onState,onFit,create,records:new Map(),visible:true});
  }
  get current(){return this.records.get(this.selected)?.terminal;}
  get secondaryTerminal(){return this.records.get(this.secondary?.id)?.terminal;}
  get term(){return this.current?.term;}
  get task(){return this.current?.task;}
  record(task){
    let record=this.records.get(task.id);
    if(!record){
      this.evict();
      record={host:document.createElement('div')};record.host.className='cached-terminal';
      record.terminal=this.create(this.api,record.host,state=>{record.state=state;this.emit(task.id,'state',state);},fit=>{record.fit=fit;this.emit(task.id,'fit',fit);});
      this.records.set(task.id,record);log('terminal-cache','created',{count:this.records.size});
    }
    this.records.delete(task.id);this.records.set(task.id,record);
    return record;
  }
  evict(){
    for(const [id,record] of this.records){
      if(this.records.size<TERMINAL_CACHE_LIMIT)return;
      if(id===this.selected || id===this.secondary?.id)continue;
      this.records.delete(id);record.terminal.destroy();
      log('terminal-cache','evicted',{count:this.records.size});
    }
  }
  emit(id,kind,value){
    if(this.selected===id)(kind==='state'?this.onState:this.onFit)(value);
    if(this.secondary?.id===id)(kind==='state'?this.secondary.onState:this.secondary.onFit)(value);
  }
  pause(id){const terminal=this.records.get(id)?.terminal;terminal?.pauseInput?.();terminal?.pauseOutput?.();terminal?.show(false);}
  async attach(task){
    if(!task){this.pause(this.selected);this.selected=null;this.container.replaceChildren();this.onState({message:'No task selected',interactive:false});this.onFit({needed:false,size:null,current:null});return;}
    if(this.secondary?.id===task.id)this.releaseSecondary();
    const changed=this.selected!==task.id;
    if(changed)this.pause(this.selected);
    this.selected=task.id;
    const record=this.record(task);
    const mounted=this.container.firstChild!==record.host;
    if(mounted)this.container.replaceChildren(record.host);
    if(record.state)this.onState(record.state);if(record.fit)this.onFit(record.fit);
    if(changed)record.terminal.resumeInput?.();
    record.terminal.show(this.visible,mounted);
    await record.terminal.attach(task);
    if(changed && this.selected===task.id)await record.terminal.resumeOutput?.();
  }
  async attachSecondary(task,container,onState=()=>{},onFit=()=>{}){
    if(!task || task.id===this.selected){this.releaseSecondary();return;}
    const changed=this.secondary?.id!==task.id;
    if(changed)this.releaseSecondary();
    this.secondary={id:task.id,container,onState,onFit};
    const record=this.record(task);
    const mounted=container.firstChild!==record.host;
    if(mounted)container.replaceChildren(record.host);
    if(record.state)onState(record.state);if(record.fit)onFit(record.fit);
    if(changed)record.terminal.resumeInput?.();record.terminal.show(this.visible,mounted);
    await record.terminal.attach(task);
    if(changed && this.secondary?.id===task.id)await record.terminal.resumeOutput?.();
  }
  releaseSecondary(){
    if(!this.secondary)return;
    const slot=this.secondary;this.secondary=null;this.pause(slot.id);slot.container.replaceChildren();
  }
  terminalFor(target){
    if(this.secondary && this.records.get(this.secondary.id)?.host.contains?.(target))return this.secondaryTerminal;
    return this.current;
  }
  show(visible){this.visible=!!visible;this.container.hidden=!this.visible;this.current?.show(this.visible);this.secondaryTerminal?.show(this.visible);}
  reconnect(){return this.current?.reconnect();}
  fitSession(){return this.current?this.current.fitSession():Promise.reject(new Error('Select a terminal before fitting'));}
  send(text,task,options){return this.current?this.current.send(text,task,options):Promise.reject(new Error('Select a live terminal before sending'));}
  retain(ids){
    if(this.secondary && !ids.has(this.secondary.id))this.releaseSecondary();
    for(const [id,record] of this.records)if(!ids.has(id)){
      if(this.selected===id){this.selected=null;this.container.replaceChildren();this.onFit({needed:false,size:null,current:null});}
      record.terminal.destroy();this.records.delete(id);log('terminal-cache','pruned',{count:this.records.size});
    }
  }
  destroy(){this.releaseSecondary();this.selected=null;for(const record of this.records.values())record.terminal.destroy();this.records.clear();this.container.replaceChildren();this.onState({message:'No task selected',interactive:false});this.onFit({needed:false,size:null,current:null});}
}
