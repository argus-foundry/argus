import test from 'node:test';
import assert from 'node:assert/strict';
import {roleContextPressure} from '../../workspace/rail/role-context.js';
import {teamModelRows} from '../../tasks/team-models.js';
const task={id:'a',live:true};
const role={live:true,kind:'worker',context:{tokens:40000,denominator_tokens:100000,denominator_kind:'worker_window',percent:40,observed_at:null}};
for(const [pct,level] of [[0,'low'],[39,'low'],[40,'warm'],[64,'warm'],[65,'hot'],[89,'hot'],[90,'critical']])test(`pressure tier at ${pct} uses server percentage`,()=>{
 const result=roleContextPressure({...role,context:{...role.context,percent:pct}},task);assert.equal(result.level,level);assert.equal(result.text,`${pct}%`);assert.ok(result.label.includes(`, ${pct}% ${level},`));assert.match(result.label,/40,000.*100,000/);assert.match(result.label,/timing unknown/i);
});
test('context never guesses absent percentage, timing or live pressure',()=>{
 assert.equal(roleContextPressure({...role,context:{tokens:50000,percent:null}},task).level,'unknown');
 assert.equal(roleContextPressure({...role,context:null},task).text,'—');
 assert.equal(roleContextPressure(role,{...task,archived:true}),null);assert.equal(roleContextPressure(role,{...task,live:false}),null);assert.equal(roleContextPressure({...role,live:false},task),null);assert.equal(roleContextPressure({...role,archived:true},task),null);
 assert.equal(roleContextPressure({...role,roster_stale:true},task).level,'stale');assert.match(roleContextPressure({...role,kind:'coordinator',context:{...role.context,denominator_kind:'coordinator_recycle_budget'}},task).label,/recycle-budget usage/);
});
test('coordinator team models use server choices and include hoisted freelance roles',()=>{
 const roles=[{role_id:1,kind:'coordinator',task_id:'a',live:true,name:'Coordinator',archetype:'orchestrator',model_resolution:{requested_model:'requested',effective_model:'resolved',source:'profile',basis:'current_config',effort:'high',profile:'lean'}},{role_id:2,name:'Builder',model_resolution:{effective_model:null,source:'cli_default',basis:'planned_intent'}},{role_id:3,name:'Warning',model_resolution:{source:'unavailable',warning:'Safe warning'}}];
 const roster={orchestrators:[{id:10,roles}],freelance:[{role_id:4,orch_id:10,name:'Freelance',model_resolution:{effective_model:'other'}}]};
 const rows=teamModelRows(task,roster);
 assert.equal(rows.find(([label])=>label==='Builder · Basis')[1],'Planned');
 assert.equal(rows.find(([label])=>label==='Coordinator · Basis')[1],'Resolved from current settings');
 const text=JSON.stringify(rows);for(const value of ['requested','resolved','high','lean','orchestrator','CLI default','Planned','Safe warning','Freelance','Resolved from current settings'])assert.ok(text.includes(value),value);
 assert.deepEqual(teamModelRows({id:'other'},roster),[]);
});

test('older rosters without a context field preserve compact rows',()=>{assert.equal(roleContextPressure({live:true,kind:'worker'},task),null);});
