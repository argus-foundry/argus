import test from 'node:test';
import assert from 'node:assert/strict';
import {HostReveal} from '../../tasks/host-reveal.js';
function fixture(status=200,error='') {
 const calls=[],messages=[];let denied=0,confirmation;
 const reveal=new HostReveal({request:async(path,options)=>{calls.push({path,...options});return Response.json(status===200?{status:'opened',host:'daemon'}:{error},{status});}},{notify:message=>messages.push(message),denied:()=>denied++,confirm:async options=>{confirmation=options;await options.onConfirm();return true;}});
 return{reveal,calls,messages,get denied(){return denied;},get confirmation(){return confirmation;}};
}
for(const path of [undefined,'src/file space.txt'])test(`reveal sends captured ${path?'file':'worktree'} to daemon host`,async()=>{
 const f=fixture();await f.reveal.open({id:'task space',worktree_path:'/tmp/worktree'},path);
 assert.deepEqual(JSON.parse(f.calls[0].body),path?{path}:{});assert.equal(f.calls[0].path,'/api/tasks/task%20space/reveal');assert.match(f.confirmation.message,/daemon host/);assert.match(f.messages[0],/daemon host/);
});
for(const [status,code,pattern] of [[403,'forbidden',/master token/i],[501,'unsupported_host',/macOS/i],[404,'task_not_found',/task.*unavailable/i],[404,'path_not_found',/path.*exists/i],[400,'invalid_path',/invalid/i],[500,'reveal_failed',/could not open Finder/i]])test(`reveal explains ${status} ${code}`,async()=>{
 const f=fixture(status,code);await assert.rejects(()=>f.reveal.open({id:'a'},'file'),pattern);assert.equal(f.calls.length,1);
 if(status===403){assert.equal(f.reveal.disabled,true);assert.equal(f.denied,1);await f.reveal.open({id:'b'});assert.equal(f.calls.length,1);f.reveal.clear();assert.equal(f.reveal.disabled,false);}
});
test('reveal cancel sends nothing; disconnect aborts a request and ignores late completion',async()=>{
 const f=fixture();f.reveal.confirm=async()=>false;await f.reveal.open({id:'a'});assert.equal(f.calls.length,0);
 let resolve,config;f.reveal.confirm=async options=>{config=options;return false;};f.reveal.api.request=()=>new Promise(done=>resolve=done);await f.reveal.open({id:'a'});const operation=config.onConfirm();f.reveal.clear();resolve(Response.json({status:'opened',host:'daemon'}));await assert.rejects(()=>operation,{name:'AbortError'});assert.equal(f.messages.length,0);
});
