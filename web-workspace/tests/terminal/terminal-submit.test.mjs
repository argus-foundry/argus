import test from 'node:test';
import assert from 'node:assert/strict';
import {TerminalInput} from '../../terminal/terminal-input.js';
import {terminalComposerDraft,terminalPastePending} from '../../terminal/terminal-composer.js';
const task={id:'submit-fixture',status:'in_progress',live:true};
const flush=async()=>{for(let i=0;i<8;i++)await Promise.resolve();};
async function advance(t,milliseconds){for(let i=0;i<milliseconds;i+=25){t.mock.timers.tick(Math.min(25,milliseconds-i));await flush();}}
function fixture(t,onWrite=()=>{}){
 t.mock.timers.enable({apis:['setTimeout','Date'],now:100000});
 const abort=new AbortController(),writes=[],state={bytes:0,draft:'',pasting:false};
 const api={inputTask:()=>task,json:async()=>task,request:async(path,options)=>{writes.push(options.body);onWrite(options.body,state,writes);}};
 const input=new TerminalInput(api,task,abort.signal,()=>true,()=>true,()=>({...state}));
 return {input,writes,state,abort};
}
test('literal Claude image path settles without conversion and receives exactly one CR',async t=>{
 const {input,writes}=fixture(t,(data,state)=>{state.bytes++;state.draft='inspect ./.context/image.png';});
 const sending=input.send('inspect ./.context/image.png',{attachments:true,imagePaths:['./.context/image.png']});await flush();await advance(t,300);await sending;
 assert.deepEqual(writes,['\x1b[200~inspect ./.context/image.png\x1b[201~','\r']);
 await advance(t,10000);assert.equal(writes.length,2);
});
test('Claude Pasting indicator blocks early CR until delayed paste finishes',async t=>{
 const {input,writes,state}=fixture(t,(data,state)=>{if(data!=='\r'){state.bytes++;state.pasting=true;}});
 const sending=input.send('./.context/image.png',{attachments:true});await flush();await advance(t,350);assert.equal(writes.length,1);
 state.bytes++;state.pasting=false;state.draft='[Image #1]';await advance(t,75);assert.equal(writes.length,1);
 await advance(t,100);await sending;assert.equal(writes.filter(value=>value==='\r').length,1);
});
test('accepted CR resolves without output acknowledgment and never retries',async t=>{
 const {input,writes}=fixture(t,(data,state)=>{if(data!=='\r'){state.bytes++;state.draft='unchanged';}});
 const sending=input.send('image.png',{attachments:true});await flush();await advance(t,300);await sending;await advance(t,10000);
 assert.equal(writes.filter(value=>value==='\r').length,1);
});
test('missing output, unknown observation, continuous output and stuck Pasting have bounded waits',async t=>{
 for(const mode of ['silent','unknown','busy','pasting','reset']){
  await t.test(mode,async()=>{
   const {input,writes,state}=fixture(t,(data,state)=>{state.bytes=mode==='reset'?0:1;state.pasting=mode==='pasting';});
   if(mode==='unknown')input.observe=()=>null;
   if(mode==='reset')state.bytes=100;
   const sending=input.send('image.png',{attachments:true});await flush();
   for(let i=0;i<5100;i+=25){if(mode==='busy')state.bytes++;if(mode==='silent')state.bytes=0;await advance(t,25);}
   await sending;assert.equal(writes.filter(value=>value==='\r').length,1);
   t.mock.timers.reset();
  });
 }
});
test('cancellation while waiting prevents later CR',async t=>{
 const {input,writes,abort}=fixture(t);
 const rejected=assert.rejects(input.send('image.png',{attachments:true}),/canceled/);
 await flush();await advance(t,50);abort.abort();await advance(t,6000);await rejected;assert.equal(writes.length,1);
});
test('agent stopping before Enter prevents the CR',async t=>{
 const {input,writes}=fixture(t,(data,state)=>{state.bytes++;});let reads=0;
 input.api.json=async()=>++reads===1?task:{...task,live:false};
 const rejected=assert.rejects(input.send('image.png',{attachments:true}),/no longer running/);
 await flush();await advance(t,300);await rejected;assert.equal(writes.length,1);
});
test('paste or submit HTTP failure rejects without automatic retries',async t=>{
 for(const phase of ['paste','submit'])await t.test(phase,async()=>{
  const {input,writes}=fixture(t,(data,state)=>{state.bytes++;if((data==='\r')===(phase==='submit'))throw new Error('input rejected');});
  const rejected=assert.rejects(input.send('image.png',{attachments:true}),/input rejected/);
  await flush();await advance(t,6000);await rejected;assert.equal(writes.length,phase==='paste'?1:2);t.mock.timers.reset();
 });
});
test('raw input stays serialized after multiline attachment paste and CR',async t=>{
 const {input,writes}=fixture(t,(data,state)=>{state.bytes++;});
 const sending=input.send('first\nsecond image.png',{attachments:true}),typing=input.raw('typed');await flush();await advance(t,300);await Promise.all([sending,typing]);
 assert.deepEqual(writes,['\x1b[200~first\nsecond image.png\x1b[201~','\r','typed']);
});
function terminal(lines,cursor=lines.length-1,dim=false){return {rows:lines.length,cols:80,buffer:{active:{baseY:0,cursorY:cursor,getLine:row=>({translateToString:()=>lines[row],getCell:col=>({getChars:()=>lines[row][col]||'',isDim:()=>dim?1:0})})}}};}
test('rendered composer distinguishes text, wrapped lines, empty and dim placeholder',()=>{
 assert.equal(terminalComposerDraft(terminal(['header','❯\u00a0inspect [Image #1]'])),'inspect [Image #1]');
 assert.equal(terminalComposerDraft(terminal(['❯ first','second'])),'first\nsecond');
 assert.equal(terminalComposerDraft(terminal(['❯ '])),'');
 assert.equal(terminalComposerDraft(terminal(['❯ Example prompt'],0,true)),'');
 assert.equal(terminalComposerDraft(terminal(['unrecognized shell prompt'])),null);
 assert.equal(terminalComposerDraft(null),null);
});
test('paste processing recognizes the Claude status line without matching user content',()=>{
 assert.equal(terminalPastePending(terminal(['❯ ','  Pasting…  '])),true);
 assert.equal(terminalPastePending(terminal(['❯ Pasting…'])),false);
 assert.equal(terminalPastePending(terminal(['❯ inspect Pasting…'])),false);
 assert.equal(terminalPastePending(null),false);
});
