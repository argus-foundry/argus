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
