import {test} from 'node:test';import assert from 'node:assert/strict';import {AttentionPolicy} from '../../../workspace/attention/attention-policy.js';
test('sparse arrivals post immediately, same task dedupes, burst overflow coalesces',()=>{
 const policy=new AttentionPolicy();assert.equal(policy.arrive('a',0).kind,'individual');assert.equal(policy.arrive('a',1),null);assert.equal(policy.arrive('b',1).kind,'individual');assert.equal(policy.arrive('c',2).kind,'individual');assert.deepEqual(policy.arrive('d',3),{kind:'summary',ids:['d']});assert.deepEqual(policy.arrive('e',4),{kind:'summary',ids:['d','e']});assert.equal(policy.arrive('f',6000).kind,'individual');
});
test('only clearing attention rearms a task, snapshots never generate alerts',()=>{const policy=new AttentionPolicy();policy.arrive('a',0);policy.reconcile(new Set(['a']));assert.equal(policy.arrive('a',1000),null);policy.reconcile(new Set());assert.equal(policy.arrive('a',1001).kind,'individual');});
test('rearmed task counts once within the rolling distinct-task window',()=>{
 const policy=new AttentionPolicy();policy.arrive('a',0);policy.reconcile(new Set());policy.arrive('a',1);policy.reconcile(new Set());policy.arrive('a',2);assert.equal(policy.arrive('b',3).kind,'individual');assert.equal(policy.arrive('c',4).kind,'individual');assert.deepEqual(policy.arrive('d',5),{kind:'summary',ids:['d']});
});
