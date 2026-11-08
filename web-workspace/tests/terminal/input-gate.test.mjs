import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile,readdir} from 'node:fs/promises';
import {TerminalInput} from '../../terminal/terminal-input.js';

async function sources(directory){
 const files=await readdir(directory,{withFileTypes:true});
 return (await Promise.all(files.filter(file=>!['tests','vendor'].includes(file.name)).map(async file=>{
  const path=new URL(file.name+(file.isDirectory()?'/':''),directory);
  return file.isDirectory()?sources(path):/\.m?js$/.test(file.name)?[[path,await readFile(path,'utf8')]]:[];
 }))).flat();
}
test('only TerminalInput.post owns the task input endpoint',async()=>{
 const callers=(await sources(new URL('../../',import.meta.url))).filter(([,source])=>/\/input(?=['"`/?])/.test(source));
 assert.deepEqual(callers.map(([path])=>path.pathname.split('/web-workspace/')[1]),['terminal/terminal-input.js']);
 const source=callers[0][1];
 assert.equal((source.match(/\/input(?=['"`/?])/g)||[]).length,1);
 assert.equal((source.match(/this\.api\.request\(/g)||[]).length,1);
 assert.match(source,/this\.api\.inputTask\(this\.task\.id\)/);
});
for(const flag of ['archived','archive_pending'])for(const kind of ['raw','composer','attachment'])test(`${kind} consults authoritative ${flag} at each POST`,async()=>{
 let current={id:'a',status:'in_progress',live:true};const posts=[];
 const api={inputTask:()=>current,json:async()=>({...current,[flag]:false}),request:async(path,options)=>{posts.push(options.body);current={...current,[flag]:true};}};
 const input=new TerminalInput(api,{...current},new AbortController().signal,()=>true,()=>true,()=>({bytes:1,pasting:false}));
 if(kind==='raw'){
  const first=input.raw('first');current={...current,[flag]:true};
  await assert.rejects(first);assert.deepEqual(posts,[]);
 }else{
  if(kind==='attachment')input.observe=()=>({bytes:posts.length?2:1,pasting:false});
  await assert.rejects(input.send('message',{attachments:kind==='attachment'}));
  assert.equal(posts.length,1);assert.notEqual(posts[0],'\r');
 }
});

test('missing workspace tasks fail closed and restored workspace state permits input',async()=>{
 let current;const posts=[];
 const api={inputTask:()=>current,request:async(path,options)=>posts.push(options.body)};
 const input=new TerminalInput(api,{id:'a',status:'in_progress',live:true},new AbortController().signal,()=>true);
 await assert.rejects(input.raw('missing'),/workspace reconciliation/);
 current={id:'a',status:'in_progress',live:true,archive_pending:true};
 await assert.rejects(input.raw('pending'),/workspace reconciliation/);
 current={...current,archive_pending:false};await input.raw('restored');
 assert.deepEqual(posts,['restored']);
});

test('archive gate leaves terminal recovery liveness to the terminal ready check',async()=>{
 const posts=[];
 const api={inputTask:()=>({id:'a',status:'in_progress',live:false}),request:async(path,options)=>posts.push(options.body)};
 const input=new TerminalInput(api,{id:'a',status:'in_progress',live:true},new AbortController().signal,()=>true);
 await input.raw('recovered');assert.deepEqual(posts,['recovered']);
});
