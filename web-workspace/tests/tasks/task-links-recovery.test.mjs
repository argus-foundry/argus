import test from 'node:test';
import assert from 'node:assert/strict';
import {fixture,task,metadata} from './task-links-fixture.mjs';

for(const pr_state of ['awaiting-review',undefined])test(`failed metadata retries unchanged PR state ${pr_state}`,async t=>{
 let fail=true;
 const url='https://github.com/example/repo/pull/12';
 const {header,reads}=fixture(t,async path=>{
  if(path.endsWith('/links'))return {links:[]};
  if(fail)throw Object.assign(new Error('Unavailable'),{status:503});
  return metadata(url);
 });
 const selected={...task,pr_state};
 await header.select(selected);
 assert.equal(header.links.attributes.role,'alert');
 assert.equal(header.links.textContent,'Links unavailable');
 fail=false;
 await header.select({...selected});
 assert.equal(header.host.querySelector('a')?.href,url);
 assert.equal(header.links.attributes.role,undefined);
 await header.select({...selected});
 assert.equal(reads.filter(read=>read.path.endsWith('/links')).length,1);
 assert.equal(reads.filter(read=>read.path.includes('/meta?')).length,2);
});

for(const status of [401,403])test(`metadata authorization ${status} announces failure and can recover`,async t=>{
 let fail=true;
 const {header}=fixture(t,async path=>{
  if(path.endsWith('/links'))return {links:[]};
  if(fail)throw Object.assign(new Error('Denied'),{status});
  return metadata('https://github.com/example/repo/pull/13');
 });
 await header.select({...task});
 assert.equal(header.links.attributes.role,'alert');
 fail=false;
 await header.select({...task});
 assert.equal(header.host.querySelector('a')?.href,'https://github.com/example/repo/pull/13');
});

function pendingRead(signal){
 return new Promise((resolve,reject)=>{
  signal.throwIfAborted();
  signal.addEventListener('abort',()=>reject(signal.reason),{once:true});
 });
}
function deadlines(t){
 const timers=[];
 t.mock.method(AbortSignal,'timeout',()=>{
  const timer=new AbortController();timers.push(timer);return timer.signal;
 });
 return timers;
}

test('metadata timeout retries without rereading output links',async t=>{
 const timers=deadlines(t);
 let fail=true;
 const {header,reads}=fixture(t,async(path,{signal})=>{
  if(path.endsWith('/links'))return {links:[]};
  if(fail)return pendingRead(signal);
  return metadata('https://github.com/example/repo/pull/14');
 });
 const first=header.select({...task});
 timers[1].abort(new DOMException('Timed out','TimeoutError'));
 await first;
 assert.equal(header.links.attributes.role,'alert');
 fail=false;
 await header.select({...task});
 assert.equal(header.host.querySelector('a')?.href,'https://github.com/example/repo/pull/14');
 assert.equal(reads.filter(read=>read.path.endsWith('/links')).length,1);
});

for(const failure of ['503','timeout'])test(`output links ${failure} retains metadata fallback across refresh`,async t=>{
 const timers=deadlines(t);
 const url='https://github.com/example/repo/pull/15';
 const {header,reads}=fixture(t,async(path,{signal})=>{
  if(!path.endsWith('/links'))return metadata(url);
  if(failure==='timeout')return pendingRead(signal);
  throw Object.assign(new Error('Unavailable'),{status:503});
 });
 const first=header.select({...task});
 if(failure==='timeout'){
  await Promise.resolve();
  for(const timer of timers)timer.abort(new DOMException('Timed out','TimeoutError'));
 }
 await first;
 assert.equal(header.host.querySelector('a')?.href,url);
 await header.select({...task,pr_state:'awaiting-review'});
 assert.equal(header.host.querySelector('a')?.href,url);
 assert.equal(reads.filter(read=>read.path.endsWith('/links')).length,1);
});

test('unchanged task refresh does not abort or duplicate pending metadata',async t=>{
 let resolve;
 const {header,reads}=fixture(t,async path=>path.endsWith('/links')?{links:[]}:new Promise(done=>{resolve=done;}));
 const first=header.select({...task});
 await header.select({...task});
 assert.equal(reads.length,2);
 assert.equal(reads[1].signal.aborted,false);
 resolve(metadata('https://github.com/example/repo/pull/16'));
 await first;
 assert.equal(header.host.querySelector('a')?.href,'https://github.com/example/repo/pull/16');
});

test('aborted PR refresh cannot overwrite a newer success or mark it failed',async t=>{
 let hold=false;
 const {header,reads}=fixture(t,async(path,{signal})=>{
  if(path.endsWith('/links'))return {links:[]};
  if(hold)return pendingRead(signal);
  return metadata('https://github.com/example/repo/pull/17');
 });
 await header.select({...task});
 hold=true;
 const refresh=header.select({...task,pr_state:'awaiting-review'});
 hold=false;
 await header.select({...task,pr_state:'merged-closed'});
 await refresh;
 assert.equal(reads[2].signal.aborted,true);
 assert.equal(reads[0].signal.aborted,false);
 assert.equal(header.links.attributes.role,undefined);
 assert.equal(header.host.querySelector('a')?.href,'https://github.com/example/repo/pull/17');
 await header.select({...task,pr_state:'merged-closed'});
 assert.equal(reads.length,4);
});

test('empty selections before and after clearing issue no reads',async t=>{
 const {header,reads}=fixture(t,async()=>({}));
 await header.select(null);
 await header.select(undefined);
 await header.select({...task});
 header.clear();
 await header.select(null);
 assert.equal(reads.length,2);
 assert.equal(header.host.children.length,0);
});

test('metadata failure retries while the selection output read remains pending',async t=>{
 let release,fail=true;
 const url='https://github.com/example/repo/pull/18';
 const {header,reads}=fixture(t,async path=>{
  if(path.endsWith('/links'))return new Promise(resolve=>{release=resolve;});
  if(fail)throw Object.assign(new Error('Unavailable'),{status:503});
  return metadata(url);
 });
 await header.select({...task});
 assert.equal(header.links.attributes.role,'alert');
 fail=false;
 const retry=header.select({...task});
 release({links:[]});
 await retry;
 assert.equal(header.host.querySelector('a')?.href,url);
 assert.equal(header.links.attributes.role,undefined);
 assert.equal(reads.filter(read=>read.path.endsWith('/links')).length,1);
 assert.equal(reads[0].signal.aborted,false);
});
