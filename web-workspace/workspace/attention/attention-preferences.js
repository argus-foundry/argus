import {preference,savePreference} from '../../shared/dom.js';

export class AttentionPreferences {
  constructor(read=()=>preference('attention',{}),write=value=>savePreference('attention',value)){Object.assign(this,{read,write,listeners:new Set()});}
  get(){
    if(!this.value){let saved={};try{saved=this.read()||{};}catch{}this.value={enabled:saved.enabled!==false,idle:saved.idle===true};}
    return {...this.value};
  }
  set(changes){
    const value=this.get();for(const key of ['enabled','idle'])if(typeof changes[key]==='boolean')value[key]=changes[key];this.value=value;
    try{this.write({...value});}catch{}
    for(const listener of this.listeners)listener({...value});
  }
  subscribe(listener){this.listeners.add(listener);return()=>this.listeners.delete(listener);}
}
export const attentionPreferences=new AttentionPreferences();
