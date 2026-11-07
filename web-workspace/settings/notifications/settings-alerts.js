import {node} from '../../shared/dom.js';
import {attentionPreferences} from '../../workspace/attention/attention-preferences.js';
import {log} from '../../core/api.js';

export class LocalAlertSettings {
  constructor(context){
    Object.assign(this,context);
    this.host.append(node('h3','','Workspace alerts'),node('p','settings-note','In-page alerts while this workspace is open. Desktop alerts use Web Push below.'));
    this.needs=this.checkbox('Needs-input alerts','attention-needs-input','enabled');
    this.idle=this.checkbox('Idle alerts','attention-idle','idle');
    this.unsubscribe=attentionPreferences.subscribe(value=>this.apply(value));this.apply(attentionPreferences.get());
  }
  checkbox(text,id,key){
    const label=node('label','settings-check'),field=node('input');field.type='checkbox';field.id=id;label.htmlFor=id;label.append(field,document.createTextNode(text));this.host.append(label);
    field.addEventListener('change',()=>{attentionPreferences.set({[key]:field.checked});this.status('Workspace alert preferences updated.');log('attention-settings','saved');});return field;
  }
  apply(value){
    this.needs.checked=value.enabled;this.idle.checked=value.idle;
  }
  destroy(){this.unsubscribe();}
}
