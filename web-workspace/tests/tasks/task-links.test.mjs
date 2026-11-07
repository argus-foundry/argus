import test from 'node:test';
import assert from 'node:assert/strict';

import {fixture,task,metadata} from './task-links-fixture.mjs';

test('same selection refreshes PR metadata when PR state changes and reads links once',async t=>{
 let url;
 const {header,reads}=fixture(t,async path=>path.endsWith('/links')?{links:[]}:metadata(url));
 await header.select({...task});
 assert.equal(header.host.querySelector('a'),null);
 url='https://github.com/example/repo/pull/8';
 await header.select({...task,pr_state:'awaiting-review'});
 assert.equal(header.host.querySelector('a')?.href,url);
 assert.equal(header.host.querySelector('a').attributes['aria-label'],'Open pull request');
 assert.equal(reads.filter(read=>read.path.endsWith('/links')).length,1);
 assert.equal(reads.filter(read=>read.path.includes('/meta?')).length,2);
 await header.select({...task,pr_state:'awaiting-review'});
 assert.equal(reads.length,3);
});

test('PR refresh rejects stale metadata while retaining pending selection links',async t=>{
 const pending=[];
 const {header,reads}=fixture(t,(path)=>new Promise(resolve=>pending.push({path,resolve})));
 const first=header.select({...task});
 const next=header.select({...task,pr_state:'awaiting-review'});
 pending.find(read=>read.path.endsWith('/links')).resolve({links:[]});
 pending.filter(read=>read.path.includes('/meta?')).at(-1).resolve(metadata('https://github.com/example/repo/pull/9'));
 await next;
 pending.filter(read=>read.path.includes('/meta?'))[0].resolve(metadata('https://github.com/example/repo/pull/1'));
 await first;
 assert.equal(header.host.querySelector('a')?.href,'https://github.com/example/repo/pull/9');
 assert.equal(reads[0].signal.aborted,false);
 assert.equal(reads[1].signal.aborted,true);
 header.clear();
 assert.equal(reads[0].signal.aborted,true);
 assert.equal(header.host.children.length,0);
});

for(const status of [401,403])test(`link authorization ${status} retains the existing alert`,async t=>{
 const {header}=fixture(t,async path=>{
  if(path.endsWith('/links'))throw Object.assign(new Error('Denied'),{status});
  return metadata();
 });
 await header.select({...task});
 const alert=header.host.children.find(child=>child.attributes.role==='alert');
 assert.equal(alert?.textContent,'Links unavailable');
});

test('PR refresh replaces the URL and removes a PR that is no longer available',async t=>{
 let url='https://github.com/example/repo/pull/1';
 const {header}=fixture(t,async path=>path.endsWith('/links')?{links:[]}:metadata(url));
 await header.select({...task});
 const branch=header.host.querySelector('button');
 url='https://github.com/example/repo/pull/2';
 await header.select({...task,pr_state:'awaiting-review'});
 assert.equal(header.host.querySelector('a')?.href,url);
 assert.equal(header.host.querySelector('button'),branch);
 url=null;
 await header.select({...task,pr_state:'merged-closed'});
 assert.equal(header.host.querySelector('a'),null);
});

test('PR refresh preserves safe output-link fallback and handles a mutated task record',async t=>{
 const selected={...task};
 const url='https://github.com/example/repo/pull/3';
 const {header,reads}=fixture(t,async path=>path.endsWith('/links')?{links:[{url,isPR:true}]}:metadata());
 await header.select(selected);
 selected.pr_state='awaiting-review';
 await header.select(selected);
 assert.equal(header.host.querySelector('a')?.href,url);
 assert.equal(reads.length,3);
});

test('switching selections aborts pending reads and fences late results',async t=>{
 const pending=[];
 const {header}=fixture(t,path=>new Promise(resolve=>pending.push({path,resolve})));
 const first=header.select({...task});
 const second=header.select({...task,id:'b'});
 for(const read of pending.filter(read=>read.path.includes('/b/')))read.resolve(read.path.endsWith('/links')?{links:[]}:metadata('https://github.com/example/repo/pull/4'));
 await second;
 for(const read of pending.filter(read=>read.path.includes('/a/')))read.resolve(read.path.endsWith('/links')?{links:[]}:metadata('https://github.com/example/repo/pull/5'));
 await first;
 assert.equal(header.host.querySelector('a')?.href,'https://github.com/example/repo/pull/4');
});
