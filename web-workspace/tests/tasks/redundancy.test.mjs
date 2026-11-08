import test from 'node:test';
import assert from 'node:assert/strict';
import {resolvePR} from '../../tasks/task-information.js';
import {Api} from '../../core/api.js';

test('PR resolution prefers safe cached metadata and falls back to safe PR output links', () => {
 const entries=[{namespace:'pr',key:'url',value:'https://github.com/org/repo/pull/1'}];
 const links=[{url:'https://github.com/org/repo/pull/2',isPR:true}];
 assert.equal(resolvePR(entries,links),entries[0].value);
 assert.equal(resolvePR([{namespace:'pr',key:'url',value:'https://user:pass@example.com'}],links),links[0].url);
 assert.equal(resolvePR([], [{url:'javascript:alert(1)',isPR:true},{url:'https://example.com/doc'}]),null);
});
test('roster cache coalesces reads, isolates cancellation, retries failures and invalidates settings writes', async t => {
 const original=globalThis.fetch;t.after(()=>globalThis.fetch=original);
 const calls=[];let release;
 globalThis.fetch=async(path,options)=>{calls.push(path);if(calls.length===1)await new Promise(resolve=>release=resolve);return new Response('{}');};
 const api=new Api(), abort=new AbortController();
 const canceled=api.json('/api/backends',{signal:abort.signal});const canceledCheck=assert.rejects(canceled,{name:'AbortError'});
 const live=api.json('/api/backends');abort.abort();release();await canceledCheck;await live;
 await api.json('/api/backends');assert.equal(calls.length,1);
 await api.json('/api/settings',{method:'PUT'});await api.json('/api/backends');assert.equal(calls.length,3);
 api.setToken('different');await api.json('/api/backends');assert.equal(calls.length,4);
 let failed=true;globalThis.fetch=async()=>new Response('{}',{status:failed?503:200});
 await assert.rejects(api.json('/api/projects/full'));failed=false;await api.json('/api/projects/full');
});
test('task links share a read within a selection and refresh on reselection',async t=>{
 const original=globalThis.fetch;t.after(()=>globalThis.fetch=original);let count=0;
 globalThis.fetch=async()=>{count++;return new Response('{"links":[]}');};const api=new Api();
 api.selectTask('a');await Promise.all([api.json('/api/tasks/a/links'),api.json('/api/tasks/a/links')]);assert.equal(count,1);
 api.selectTask('b');api.selectTask('a');await api.json('/api/tasks/a/links');assert.equal(count,2);
});
test('stale roster failures cannot evict a replacement read and cached values are isolated',async t=>{
 const original=globalThis.fetch;t.after(()=>globalThis.fetch=original);let release,count=0;
 globalThis.fetch=async()=>{count++;if(count===1){await new Promise(resolve=>release=resolve);return new Response('{}',{status:503});}return new Response('{"backends":[{"name":"current"}]}');};
 const api=new Api(),old=api.json('/api/backends');const rejected=assert.rejects(old);api.invalidateReads();const current=await api.json('/api/backends');release();await rejected;
 current.backends[0].name='mutated';assert.equal((await api.json('/api/backends')).backends[0].name,'current');assert.equal(count,2);
});
test('terminal input does not invalidate configuration rosters',async t=>{
 const original=globalThis.fetch;t.after(()=>globalThis.fetch=original);let reads=0;
 globalThis.fetch=async path=>{if(path==='/api/backends')reads++;return new Response('{}');};
 const api=new Api();await api.json('/api/backends');await api.json('/api/tasks/a/input',{method:'POST',body:'x'});await api.json('/api/backends');assert.equal(reads,1);
});
test('project revalidation coalesces concurrent refreshes but never masks a later server failure',async t=>{
 const original=globalThis.fetch;t.after(()=>globalThis.fetch=original);let count=0,failed=false,release;
 globalThis.fetch=async()=>{count++;if(count===2)await new Promise(resolve=>release=resolve);return new Response('{}',{status:failed?500:200});};
 const api=new Api();await api.json('/api/projects/full');
 const first=api.json('/api/projects/full',{revalidate:true}),second=api.json('/api/projects/full',{revalidate:true});
 assert.equal(count,2);release();await Promise.all([first,second]);
 failed=true;await assert.rejects(api.json('/api/projects/full',{revalidate:true}),error=>error.status===500);assert.equal(count,3);
});
