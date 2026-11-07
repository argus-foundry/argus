import test from 'node:test';import assert from 'node:assert/strict';
import {diffCounts,ChangedFileCounts,statTotals} from '../../../tasks/changes/change-counts.js';
test('diff counts include only hunk content and preserve binary/unavailable distinctions',()=>{
 assert.deepEqual(diffCounts('diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,2 +1,3 @@\n-old\n same\n+new\n+extra'),{added:2,removed:1});
 assert.deepEqual(diffCounts('diff --git a/x b/x\nold mode 100644\nnew mode 100755'),{added:0,removed:0});
 assert.deepEqual(diffCounts('Binary files a/x and b/x differ'),{kind:'binary'});assert.deepEqual(diffCounts(''),{kind:'unavailable'});
});
test('counts bound concurrency, deduplicate paths and include failed/directory results without inventing zeros',async()=>{
 const pending=[],applied=[];let active=0,peak=0;
 const counter=new ChangedFileCounts({json:path=>{active++;peak=Math.max(peak,active);return new Promise((resolve,reject)=>pending.push({path,resolve:value=>{active--;resolve(value);},reject:()=>{active--;reject(new Error('fixture'));}}));}}, {id:'a'},new AbortController().signal,(...args)=>applied.push(args));
 counter.add([{path:'a'},{path:'b'},{path:'c'},{path:'d'},{path:'e'},{path:'dir/',directory:true},{path:'a'}]);assert.equal(pending.length,4);assert.equal(peak,4);assert.deepEqual(counter.results.get('dir/'),{kind:'directory'});
 pending[0].resolve({Diff:'diff --git a/a b/a\n@@ -0,0 +1 @@\n+line'});await new Promise(setImmediate);assert.equal(pending.length,5);pending[1].reject();pending[2].resolve({Diff:'Binary files a/c and b/c differ'});pending[3].resolve({Diff:''});pending[4].resolve({Diff:''});await new Promise(setImmediate);
 assert.equal(peak,4);assert.deepEqual(counter.results.get('a'),{added:1,removed:0});assert.deepEqual(counter.results.get('b'),{kind:'unavailable'});assert.deepEqual(counter.results.get('c'),{kind:'binary'});assert.equal(applied.length,6);
});
test('canceling counts suppresses late results and starts no queued work',async()=>{
 const abort=new AbortController();let reads=0;const release=[],applied=[];
 const counter=new ChangedFileCounts({json:()=>{reads++;return new Promise(resolve=>release.push(resolve));}}, {id:'captured'},abort.signal,(...args)=>applied.push(args));counter.add([{path:'a'},{path:'b'},{path:'c'},{path:'d'},{path:'e'}]);abort.abort();release.forEach(resolve=>resolve({Diff:'diff --git a/a b/a\n@@ -0,0 +1 @@\n+line'}));await new Promise(setImmediate);assert.equal(reads,4);assert.deepEqual(applied,[]);
});

test('header totals use summary lines rather than scaled per-file bars',()=>{
 assert.deepEqual(statTotals({BranchDiff:' src/a | 300 +++---\n 1 file changed, 205 insertions(+), 95 deletions(-)',Diff:' 1 file changed, 2 deletions(-)'}),{added:205,removed:97});
 assert.deepEqual(statTotals({BranchDiff:' 1 file changed'}),{added:0,removed:0});assert.equal(statTotals({}),null);
});

test('invalidating one path retains other counts and fences its old in-flight result',async()=>{
 const pending=[];const counter=new ChangedFileCounts({json:()=>new Promise(resolve=>pending.push(resolve))},{id:'a'},new AbortController().signal,()=>{});
 counter.results.set('stable',{added:9,removed:0});counter.add([{path:'edited'}]);counter.invalidate('edited');counter.add([{path:'edited'}]);
 pending[0]({Diff:'diff --git a/a b/a\n@@ -0,0 +1 @@\n+old'});await new Promise(setImmediate);assert.equal(counter.results.get('edited'),null);
 pending[1]({Diff:'diff --git a/a b/a\n@@ -0,0 +1,2 @@\n+new\n+new'});await new Promise(setImmediate);
 assert.deepEqual(counter.results.get('edited'),{added:2,removed:0});assert.deepEqual(counter.results.get('stable'),{added:9,removed:0});
});

test('exact change totals include hunk lines beyond the review preview budget', () => {
  const diff = 'diff --git a/x b/x\n@@ -1 +1,7000 @@\n-old\n' + '+new\n'.repeat(7000);
  assert.deepEqual(diffCounts(diff), { added: 7000, removed: 1 });
});
