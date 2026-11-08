import test from 'node:test';
import assert from 'node:assert/strict';
import { contextSnapshot } from '../../composer/composer-context-policy.js';
const task = {id: 'a', backend: 'work-agent'};
const now = Date.parse('2026-11-06T12:00:00Z');
const data = {raw: {account: 'work'}, backends: {backends: [{name:'work-agent', kind:'claude'}]}, accounts: [{name:'work', label:'Work', plan:'Team', email:'fixture@example.invalid', usage:{five_hour_pct:0, five_hour_resets_at:'2026-11-06T13:00:00Z', weekly_pct:83, weekly_resets_at:'2026-11-09T12:00:00Z'}}]};

test('live bridge context supplies window fill independently from quota', () => {
  const view = contextSnapshot(task, {...data, bridge:{live:true, context:{tokens:421200,window:1000000,percent:42.12},last_measure_at:'2026-11-06T11:59:00Z'}}, now);
  assert.equal(view.context.percent, 42.12); assert.equal(view.context.limit, 1000000);
  assert.deepEqual(view.windows.map(w => w.left), [100,17]);
  assert.deepEqual(view.identity, [['Plan','Team'],['Email','fixture@example.invalid']]);
});
test('missing context stays unknown and zero is a valid measurement', () => {
  assert.equal(contextSnapshot(task,data,now).context, null);
  assert.equal(contextSnapshot(task,{bridge:{live:true,context:{tokens:0,window:200000}}},now).context.percent, 0);
});
test('recorded stamps never manufacture a window or reuse an older percent', () => {
  const meta = {entries:[{namespace:'hera',key:'context_size',value:'1234',updated_at:'2026-11-06T11:59:00Z'}, {namespace:'hera',key:'context_percent',value:'50',updated_at:'2026-11-06T11:58:00Z'}]};
  const view = contextSnapshot(task,{meta,bridge:{live:false,context:{tokens:99,percent:99,window:100}}},now);
  assert.equal(view.context.used, 1234); assert.equal(view.context.percent,null); assert.equal(view.context.limit,null); assert.equal(view.context.recorded,true);
});
test('Codex never inherits Claude usage, email or plan from a shared account', () => {
  const view = contextSnapshot(task,{...data,backends:{backends:[{name:'work-agent',kind:'codex'}]}},now);
  assert.deepEqual(view.windows,[]); assert.deepEqual(view.identity,[]);
});
test('invalid and expired usage windows and unrelated accounts are omitted', () => {
  assert.deepEqual(contextSnapshot(task,{...data,raw:{account:'other'}},now).windows,[]);
  const accounts = [{name:'work',usage:{five_hour_pct:NaN, weekly_pct:10, weekly_resets_at:'2026-11-06T11:00:00Z'}}];
  assert.deepEqual(contextSnapshot(task,{...data,accounts},now).windows,[]);
});
test('composer omits account identity and historical role totals', () => {
  const role = {live:true,task_id:'a',tokens_input:200,tokens_cache_read:100,tokens_output:3};
  for (const roles of [[role], [role,role]]) {
    const view = contextSnapshot(task,{...data,roster:{orchestrators:[{roles}]}},now);
    assert.equal('totals' in view,false);
    assert.equal(view.identity.some(([label]) => label === 'Account'),false);
    assert.equal(view.context,null);
  }
});
test('invalid context is absent; exact measured percent and matching stamps remain usable', () => {
  assert.equal(contextSnapshot(task,{bridge:{live:true,context:{tokens:-1,percent:NaN,window:0}}},now).context,null);
  const meta={entries:[{namespace:'hera',key:'context_size',value:'0',updated_at:'2026-11-06T11:59:00Z'},{namespace:'hera',key:'context_percent',value:'0',updated_at:'2026-11-06T11:59:00Z'}]};
  assert.equal(contextSnapshot(task,{meta},now).context.percent,0);
  assert.equal(contextSnapshot(task,{bridge:{live:true,context:{percent:25}}},now).context.used,null);
});

test('live task bridge supplies known usage windows when account readings are missing', () => {
  const bridge={live:true,rate_limits:[{kind:'five_hour',percent_used:8,resets_at:'2026-11-06T13:00:00Z'},{kind:'unknown',percent_used:5,resets_at:'2026-11-06T13:00:00Z'}]};
  assert.deepEqual(contextSnapshot(task,{backends:data.backends,bridge},now).windows.map(w=>w.left),[92]);
  assert.deepEqual(contextSnapshot(task,{backends:data.backends,bridge:{...bridge,live:false}},now).windows,[]);
});
test('malformed optional endpoint shapes cannot break composer rendering', () => {
  assert.deepEqual(contextSnapshot(task,{raw:{},accounts:{},backends:{backends:{}},roster:{orchestrators:{},freelance:{}},meta:{entries:{}}},now).windows,[]);
});

const measurement = {task_id:'a',session_id:'s1',provider:'anthropic',live:true,measured_at:'2026-11-06T10:00:00Z',source:'bridge',context:{used_tokens:1234,window_tokens:200000,percent:0.617,categories:{messages:1000,memory:null,tools:0,agents:34,skills:null,system:200,compaction:null}}};
test('task context keeps measured categories, zero and old live authority', () => {
  const view = contextSnapshot({...task,session_id:'s1'},{...data,measurement},now);
  assert.equal(view.context.used,1234);
  assert.deepEqual(view.context.categories,[['Messages',1000],['Tools',0],['Agents',34],['System',200]]);
  assert.equal(view.context.recorded,false);
  assert.equal(view.context.age,'2h ago');
});
test('supported empty or mismatched session context never falls back to bridge totals', () => {
  const bridge = {live:true,context:{tokens:99,window:100,percent:99}};
  for (const value of [{...measurement,context:null,live:false},{...measurement,session_id:'old'},null]) {
    assert.equal(contextSnapshot({...task,session_id:'s1'},{bridge,measurement:value},now).context,null);
  }
});
test('recorded task context displays measured time without an inferred percent', () => {
  const view = contextSnapshot(task,{measurement:{...measurement,live:false,context:{used_tokens:null,window_tokens:200000,percent:null,categories:{skills:5}}}},now);
  assert.equal(view.context.recorded,true); assert.equal(view.context.percent,null);
  assert.deepEqual(view.context.categories,[['Skills',5]]);
});

test('all category names retain measured zero and reject invalid values',()=>{
  const categories={messages:0,memory:0,tools:0,agents:0,skills:0,system:0,compaction:0};
  const view=contextSnapshot(task,{measurement:{...measurement,context:{...measurement.context,categories}}},now);
  assert.deepEqual(view.context.categories,[['Messages',0],['Memory',0],['Tools',0],['Agents',0],['Skills',0],['System',0],['Compaction',0]]);
  for(const value of [null,-1,'0',Infinity,NaN]){
    const context={...measurement.context,categories:{memory:value}};
    assert.deepEqual(contextSnapshot(task,{measurement:{...measurement,context}},now).context.categories,[]);
  }
});
test('measurement age includes minutes and days without changing live authority',()=>{
  for(const [at,age] of [['2026-11-06T12:00:00Z','just now'],['2026-11-06T11:58:00Z','2m ago'],['2026-11-04T12:00:00Z','2d ago']]){
    const view=contextSnapshot(task,{measurement:{...measurement,measured_at:at}},now);
    assert.equal(view.context.age,age);assert.equal(view.context.recorded,false);
  }
});
