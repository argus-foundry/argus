import test from 'node:test';
import assert from 'node:assert/strict';
import { schedulePayload } from '../../../settings/schedules/settings-schedule-policy.js';
import { systemMetrics, formatSize } from '../../../settings/system/settings-system-policy.js';
const input={name:' Daily ',project:' demo ',backend:' codex ',model:' custom-model ',prompt:'first\nsecond\n',enabled:false,cadence:'cron',schedule:' @daily '};
test('schedule payload keeps multiline prompts and custom models with explicit disabled state',()=>{
 assert.deepEqual(schedulePayload(input),{name:'Daily',project:'demo',backend:'codex',model:'custom-model',prompt:'first\nsecond\n',enabled:false,schedule:'@daily'});
});
test('schedule editing omits unchanged cadence and never rewrites expired one-shots',()=>{
 const original={schedule:'@daily'};assert.equal('schedule' in schedulePayload(input,original),false);
 const once='2020-01-02T03:04:05Z';
 const body=schedulePayload({...input,cadence:'once',runOnceAt:once},{run_once_at:once});
 assert.equal('run_once_at' in body,false);assert.equal('schedule' in body,false);
});
test('schedule validation rejects absent fields, past new one-shots and oversized prompts',()=>{
 for(const name of ['name','project','prompt','schedule'])assert.throws(()=>schedulePayload({...input,[name]:' '}));
 assert.throws(()=>schedulePayload({...input,cadence:'once',runOnceAt:'invalid'}));
 assert.throws(()=>schedulePayload({...input,cadence:'once',runOnceAt:'2020-01-01T00:00:00Z'}));
 assert.throws(()=>schedulePayload({...input,prompt:'x'.repeat(1024*1024)}));
});
test('one-shot payload sends RFC3339 and changing cadence sends just the chosen anchor',()=>{
 const body=schedulePayload({...input,cadence:'once',runOnceAt:'2099-01-01T00:00:00Z'});
 assert.equal(body.run_once_at,'2099-01-01T00:00:00.000Z');assert.equal('schedule' in body,false);
});
test('metrics distinguish unavailable values from zero and clamp progress bars',()=>{
 const rows=systemMetrics({cpu_avail:true,cpu_percent:0,mem_avail:false,mem_used:10,swap_avail:true,swap_total:0,sessions:{running:0,idle:0}});
 assert.equal(rows.find(r=>r.label==='CPU').value,'0%');assert.equal(rows.find(r=>r.label==='CPU').percent,0);
 assert.equal(rows.find(r=>r.label==='Memory').value,'Unavailable');assert.equal(rows.find(r=>r.label==='Memory').percent,null);
 assert.equal(rows.find(r=>r.label==='Swap').value,'None');assert.equal(rows.find(r=>r.label==='Agent sessions').value,'0 running, 0 idle');
 assert.equal(systemMetrics({cpu_avail:true,cpu_percent:150}).find(r=>r.label==='CPU').percent,100);
 assert.equal(formatSize(1024**3),'1.0 GiB');
});
