import test from 'node:test';
import assert from 'node:assert/strict';
import {focusDOM} from '../shared/focus-dom.mjs';
import {AgentSettings} from '../../settings/agents/settings-agents.js';
const text=node=>[node.textContent||'',...node.children.map(text)].join(' ');
test('Agents settings retains valid Claude windows and never fabricates missing values',t=>{
 focusDOM(t);const future=new Date(Date.now()+86400000).toISOString(),past=new Date(Date.now()-1000).toISOString();
 const account={name:'work',supports:{claude:true,codex:true},usage:{five_hour_pct:25,five_hour_resets_at:future,weekly_pct:0,weekly_resets_at:future}};
 const value=text(AgentSettings.prototype.account(account));assert.match(value,/Claude usage/);assert.match(value,/25% used/);assert.match(value,/0% used/);assert.match(value,/resets/);assert.doesNotMatch(value,/Codex usage/);
 for(const usage of [undefined,{five_hour_pct:25,five_hour_resets_at:past},{five_hour_pct:null,five_hour_resets_at:future},{five_hour_pct:101,five_hour_resets_at:future}]){const value=text(AgentSettings.prototype.account({...account,usage}));assert.match(value,/Claude usage unavailable/);assert.doesNotMatch(value,/0% used/);}
 assert.doesNotMatch(text(AgentSettings.prototype.account({...account,supports:{claude:false,codex:true}})),/Claude usage/);
});
