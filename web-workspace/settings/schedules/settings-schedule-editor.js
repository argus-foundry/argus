import {node} from '../../shared/dom.js';
import {field,select,actions,values} from '../settings-fields.js';
import {localDateTime} from './settings-schedule-policy.js';

export function scheduleEditor(original,projects,backends,save,cancel) {
  const s=original || {},form=node('form','settings-form'),fields=node('fieldset','settings-fields');
  fields.append(node('legend','',original?'Edit schedule':'New schedule'));
  field(fields,'Name','name',s.name || '').required=true;
  select(fields,'Project','project',projects.map(project=>[project.name,project.name]),s.project || projects[0]?.name || '').required=true;
  const backend=select(fields,'Backend','backend',[['','Default'],...backends.map(item=>[item.name,item.name])],s.backend || '');
  const model=field(fields,'Model','model',s.model || '','text','Leave blank for the backend default. Custom model identifiers are allowed.');
  const suggestions=node('datalist');suggestions.id='settings-schedule-models';model.setAttribute('list',suggestions.id);fields.append(suggestions);
  const models=()=>{
    suggestions.replaceChildren();
    for(const value of backends.find(item=>item.name===backend.value)?.models || []){const option=node('option');option.value=value;suggestions.append(option);}
  };
  backend.addEventListener('change',models);models();
  const cadence=select(fields,'Cadence','cadence',[['cron','Recurring'],['once','One shot']],s.run_once_at?'once':'cron');
  const cron=field(fields,'Cron schedule','schedule',s.schedule || '@daily','text','Five-field cron, @daily, @hourly or @every 30m. The daemon validates the expression.');
  const once=field(fields,'Run once at','runOnceAt',localDateTime(s.run_once_at || new Date(Date.now()+3600000)),'datetime-local','Local time, sent to the daemon with its UTC offset resolved.');once.step='0.001';
  const mode=()=>{cron.parentElement.hidden=cadence.value!=='cron';cron.required=cadence.value==='cron';once.parentElement.hidden=cadence.value!=='once';once.required=cadence.value==='once';};
  cadence.addEventListener('change',mode);mode();
  field(fields,'Prompt','prompt',s.prompt || '','textarea').required=true;
  field(fields,'Enabled','enabled',s.enabled !== false,'checkbox');
  actions(fields,'Save schedule',cancel);form.append(fields);
  if(original)form.append(node('p','settings-note','Run now uses the saved schedule. Unsaved edits are not included.'));
  form.addEventListener('submit',event=>{event.preventDefault();save(values(form));});
  return form;
}
