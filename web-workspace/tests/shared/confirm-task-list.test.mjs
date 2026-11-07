import {test} from 'node:test';
import assert from 'node:assert/strict';
import {taskWarnings} from '../../shared/confirm-task-list.js';
test('cached attention distinguishes live bindings from ended roles and terminal PRs',()=>{
 const roster={orchestrators:[{id:1,name:'team',roles:[{task_id:'a',live:true,name:'builder'},{task_id:'b',live:false,name:'ended'}]}]};
 assert.deepEqual(taskWarnings({id:'a'},roster).roles,['builder in team']);assert.equal(taskWarnings({id:'b',pr_state:'merged-closed'},roster).attention,false);
 assert.equal(taskWarnings({id:'c',pr_state:'draft'},roster).attention,true);assert.equal(taskWarnings({id:'d',live:true},roster).attention,true);
});
test('cached links accept credential-free HTTP URLs and do not invent missing PR links',()=>{
 assert.equal(taskWarnings({id:'a'}, {},new Map([['a','https://example.com/pr/1']])).url,'https://example.com/pr/1');
 for(const value of ['javascript:alert(1)','https://user:secret@example.com/pr/1',undefined])assert.equal(taskWarnings({id:'a'}, {},new Map([['a',value]])).url,undefined);
});

test('attachment warnings use plain fallback team and agent names',()=>{
 const roster={orchestrators:[{id:7,roles:[{task_id:'a',live:true,kind:'coordinator'}]}],freelance:[{task_id:'a',live:true,kind:'freelance'}]};
 assert.deepEqual(taskWarnings({id:'a'},roster).roles,['Agent in team 7','Agent attached to a team']);
});
test('unknown PR observations are not confirmed open PR warnings',()=>{
 assert.equal(taskWarnings({id:'unknown',pr_state:'unknown'}).pr,false);assert.equal(taskWarnings({id:'unknown',pr_state:'unknown'}).attention,false);
 assert.equal(taskWarnings({id:'live',pr_state:'unknown',live:true}).attention,true);assert.equal(taskWarnings({id:'bound',pr_state:'unknown'},{freelance:[{task_id:'bound',live:true,name:'Agent'}]}).attention,true);
 for(const pr_state of ['draft','awaiting-review','changes-requested','approved'])assert.equal(taskWarnings({id:'open',pr_state}).pr,true);
});
