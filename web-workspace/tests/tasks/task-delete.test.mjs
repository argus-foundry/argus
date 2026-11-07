import {test} from 'node:test';
import assert from 'node:assert/strict';
import {deleteTaskFacts, deletionNeighbor, deleteEffects} from '../../tasks/task-delete-policy.js';
test('delete facts include captured identity and advisory risks without safety claims',()=>{
 const facts=deleteTaskFacts({id:'one',name:'One',project:'repo',branch:'feature',worktree_path:'/tmp/task',live:true,pr_state:'merged-closed'},[{name:'worker',live:true}],{Status:' M file\n?? other'},'https://example.com/pr/1');
 for(const value of ['One','repo','feature','/tmp/task','Live session','worker','2 reported changed files','merged-closed'])assert.ok(facts.includes(value),value);
 assert.ok(!deleteTaskFacts({id:'two'},[],null).includes('clean'));
});
test('neighbor prefers next task then previous, excluding all removed IDs',()=>{
 const tasks=[{id:'a'},{id:'b'},{id:'c'}];
 assert.equal(deletionNeighbor(tasks,new Set(['b']))?.id,'c');
 assert.equal(deletionNeighbor(tasks,new Set(['c']))?.id,'b');
 assert.equal(deletionNeighbor(tasks,new Set(['a','b','c'])),null);
});

test('review counts reported branch and worktree paths once',()=>{
 const facts=deleteTaskFacts({id:'a'},[],{BranchFiles:'M\tfile\nA\tbranch-only',Status:' M file\n?? untracked'});
 assert.match(facts,/3 reported changed files/);
});

test('delete attachment facts use plain team copy without role kinds',()=>{
 const facts=deleteTaskFacts({id:'a'},[{kind:'coordinator'}]);
 assert.match(facts,/Team attachments will end \(Agent\)/);
 assert.match(deleteTaskFacts({id:'a'}),/No team attachments reported/);
 assert.match(deleteEffects,/team attachments are ended/);
 assert.doesNotMatch(facts+deleteEffects,/Hera|binding|coordinator/);
});
