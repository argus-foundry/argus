import {tokenCount} from '../../composer/composer-context-policy.js';
import {node} from '../../shared/dom.js';

export function roleContextPressure(role,task) {
 if(!task||!role||!role.live||role.archived||task?.archived||task?.live===false)return null;
 if(role.roster_stale)return {text:'—',level:'stale',label:'Context stale. Roster refresh failed.'};
 if(!Object.hasOwn(role,'context'))return null;
 const context=role.context;
 if(!context)return {text:'—',level:'unknown',label:'Context unavailable'};
 const pct=Number.isFinite(context.percent)?context.percent:null;
 const basis=context.denominator_kind==='coordinator_recycle_budget'?'recycle-budget usage':'model-window usage';
 const tokens=Number.isFinite(context.tokens)?`${tokenCount(context.tokens)} tokens`:'tokens unavailable';
 const denominator=Number.isFinite(context.denominator_tokens)&&context.denominator_tokens>0?`configured ${context.denominator_kind==='coordinator_recycle_budget'?'budget':'window'} ${tokenCount(context.denominator_tokens)} tokens`:'configured window unavailable';
 const timing=context.observed_at&&Number.isFinite(Date.parse(context.observed_at))?`observed ${new Date(context.observed_at).toLocaleString()}`:'timing unknown';
 const level=pct===null?'unknown':pct>=90?'critical':pct>=65?'hot':pct>=40?'warm':'low';
 return {text:pct===null?'—':`${pct}%`,level,label:`${basis}, ${pct===null?'percentage unavailable':`${pct}% ${level}`}, ${tokens}, ${denominator}, ${timing}`};
}
export function roleContextIndicator(role,task) {
 const value=roleContextPressure(role,task);if(!value)return null;
 const indicator=node('span','task-context-pressure',value.text);indicator.dataset.pressure=value.level;
 indicator.setAttribute('aria-label',value.label);indicator.dataset.tooltip=value.label;return indicator;
}
