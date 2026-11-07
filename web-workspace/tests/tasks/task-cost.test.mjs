import {test} from 'node:test';
import assert from 'node:assert/strict';
import {taskCostRows} from '../../tasks/task-cost.js';
const worker={task_id:'w',kind:'worker',live:true,cost_usd:1.235,tokens_input:1234,tokens_cache_read:200,tokens_cache_write_1h:30,tokens_cache_write_5m:40,tokens_output:50};
const coordinator={task_id:'c',kind:'coordinator',live:true,cost_usd:2};
const roster={orchestrators:[{subtree_cost_usd:9,roles:[worker,coordinator]}],freelance:[{...worker,task_id:'f',kind:'freelance'}]};
test('worker and freelancer use measured cost with available token totals',()=>{
  for(const id of ['w','f'])assert.deepEqual(taskCostRows({id},roster),[
    ['Estimated cost','$1.24'],['Input','1,234'],['Cached input','200'],['Cache write (1h)','30'],['Cache write (5m)','40'],['Output','50']
  ]);
});
test('coordinator includes only supplied orchestrator total',()=>{
  assert.deepEqual(taskCostRows({id:'c'},roster),[['Estimated cost','$2.00'],['Team total (this team)','$9.00 (estimate, excludes nested teams)']]);
});
test('plain, unmeasured, invalid and ambiguous bindings do not fabricate cost',()=>{
  assert.deepEqual(taskCostRows({id:'plain'},roster),[]);
  for(const cost_usd of [undefined,null,NaN,Infinity,-1,'2'])assert.deepEqual(taskCostRows({id:'w'},{freelance:[{...worker,cost_usd}]}),[]);
  assert.deepEqual(taskCostRows({id:'w'},{freelance:[worker,worker]}),[]);
  assert.deepEqual(taskCostRows({id:'w'},{freelance:[{...worker,live:false}]}),[]);
});
test('one coordinator takes precedence over parent worker binding without summing twice',()=>{
  const nested={orchestrators:[...roster.orchestrators,{subtree_cost_usd:3,roles:[{...coordinator,task_id:'w'}]}]};
  assert.deepEqual(taskCostRows({id:'w'},nested),[['Estimated cost','$2.00'],['Team total (this team)','$3.00 (estimate, excludes nested teams)']]);
});
test('tiny measured cost rounds to two decimals and missing token fields remain absent',()=>{
  assert.deepEqual(taskCostRows({id:'x'},{freelance:[{task_id:'x',live:true,cost_usd:0.001}]}),[['Estimated cost','$0.00']]);
});
test('coordinator can show measured team cost without an own measurement',()=>{
  assert.deepEqual(taskCostRows({id:'c'},{orchestrators:[{subtree_cost_usd:6,roles:[{...coordinator,cost_usd:undefined}]}]}),[['Team total (this team)','$6.00 (estimate, excludes nested teams)']]);
});
test('multiple coordinator bindings are ambiguous and invalid team totals are omitted',()=>{
  assert.deepEqual(taskCostRows({id:'c'},{orchestrators:[{roles:[coordinator,coordinator]}]}),[]);
  for(const subtree_cost_usd of [undefined,null,NaN,Infinity,-2,'5'])assert.deepEqual(taskCostRows({id:'c'},{orchestrators:[{subtree_cost_usd,roles:[coordinator]}]}),[['Estimated cost','$2.00']]);
});

test('actual zero costs remain measured for agents and their own team',()=>{
 const role={task_id:'zero',live:true,cost_usd:0};
 assert.deepEqual(taskCostRows({id:'zero'},{freelance:[role]}),[['Estimated cost','$0.00']]);
 assert.deepEqual(taskCostRows({id:'zero'},{orchestrators:[{subtree_cost_usd:0,roles:[{...role,kind:'coordinator'}]}]}),[
  ['Estimated cost','$0.00'],['Team total (this team)','$0.00 (estimate, excludes nested teams)']
 ]);
});
