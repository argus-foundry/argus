import {test} from 'node:test';import assert from 'node:assert/strict';
import {coordinatorContext,secondaryTask} from '../../../workspace/split/split-policy.js';
const tasks=[{id:'c'},{id:'w'},{id:'plain'}],roster={orchestrators:[{id:1,roles:[{kind:'coordinator',live:true,task_id:'c'},{kind:'worker',live:true,task_id:'w'}]}]};
test('split defaults only coordinator tasks to a live worker',()=>{assert.equal(coordinatorContext('plain',roster),null);assert.equal(secondaryTask('c',tasks,roster),'w');assert.equal(secondaryTask('plain',tasks,roster),null);assert.equal(secondaryTask('c',tasks,roster,'plain'),'plain');assert.equal(secondaryTask('c',tasks,roster,'c'),'w');});
test('a remembered worker must still have a live worker binding before being a default',()=>{assert.equal(secondaryTask('c',tasks,roster,undefined,'plain'),'w');});
