import {test} from 'node:test';import assert from 'node:assert/strict';import {TaskTerminalCache} from '../../terminal/task-terminal-cache.js';
test('task switches reuse cached terminals and missing tasks are pruned',async()=>{
 globalThis.document={createElement:()=>({hidden:false})};const host={replaceChildren(){}};const created=[],fits=[];
 const cache=new TaskTerminalCache({},host,()=>{},fit=>fits.push(fit),()=>{const terminal={term:{},attach:async()=>{},show(){},pauseOutput(){this.outputPaused=(this.outputPaused||0)+1;},resumeOutput(){},pauseInput(){this.paused=(this.paused||0)+1;},resumeInput(){this.resumed=(this.resumed||0)+1;},destroy(){this.destroyed=true;}};created.push(terminal);return terminal;});
 await cache.attach({id:'a'});const first=cache.term;await cache.attach({id:'b'});await cache.attach({id:'a'});assert.equal(cache.term,first);assert.equal(created.length,2);assert.equal(created[0].paused,1);assert.equal(created[0].outputPaused,1);assert.equal(created[0].resumed,2);assert.equal(created[0].destroyed,undefined);
 cache.retain(new Set(['a']));assert.equal(created[1].destroyed,true);assert.equal(created[0].destroyed,undefined);cache.destroy();assert.equal(created[0].destroyed,true);assert.deepEqual(fits.at(-1),{needed:false,size:null,current:null});
});
test('secondary mount reuses a retained controller and closing preserves both terminals',async()=>{
 globalThis.document={createElement:()=>({hidden:false})};const created=[],host={replaceChildren(){}},right={replaceChildren(){}};
 const cache=new TaskTerminalCache({},host,()=>{},()=>{},()=>{const terminal={term:{},attach:async()=>{},show(){},pauseInput(){},pauseOutput(){},resumeInput(){},resumeOutput(){},destroy(){this.destroyed=true;}};created.push(terminal);return terminal;});
 await cache.attach({id:'a'});await cache.attachSecondary({id:'b'},right);const retained=cache.secondaryTerminal.term;cache.releaseSecondary();assert.equal(created[1].destroyed,undefined);await cache.attachSecondary({id:'b'},right);assert.equal(cache.secondaryTerminal.term,retained);assert.equal(created.length,2);await cache.attach({id:'b'});assert.equal(cache.current,created[1]);assert.equal(cache.secondaryTerminal,undefined);cache.destroy();
});
test('secondary callbacks never change the primary state or Fit controls',async()=>{
 globalThis.document={createElement:()=>({hidden:false})};const host={replaceChildren(){}},right={replaceChildren(){}},main=[],secondary=[],mainFit=[],secondaryFit=[],factories=[];
 const cache=new TaskTerminalCache({},host,state=>main.push(state.task),fit=>mainFit.push(fit.task),(api,container,onState,onFit)=>{const terminal={term:{},show(){},attach:async task=>{onState({task:task.id});onFit({task:task.id});},pauseInput(){},pauseOutput(){},resumeInput(){},resumeOutput(){},destroy(){},notify:onState,notifyFit:onFit};factories.push(terminal);return terminal;});
 await cache.attach({id:'a'});await cache.attachSecondary({id:'b'},right,state=>secondary.push(state.task),fit=>secondaryFit.push(fit.task));assert.deepEqual(mainFit,['a']);assert.deepEqual(secondaryFit,['b']);assert.deepEqual(main,['a']);assert.deepEqual(secondary,['b']);cache.releaseSecondary();factories[1].notify({task:'late-b'});factories[1].notifyFit({task:'late-b'});assert.deepEqual(mainFit,['a']);assert.deepEqual(secondaryFit,['b']);assert.deepEqual(main,['a']);assert.deepEqual(secondary,['b']);cache.destroy();
});

test('terminal LRU bounds live instances and protects selected and split-visible tasks',async()=>{
 globalThis.document={createElement:()=>({hidden:false})};const created=[],host={replaceChildren(){}},right={replaceChildren(){}};
 const cache=new TaskTerminalCache({},host,()=>{},()=>{},()=>{const terminal={term:{},attach:async()=>{},show(){},pauseInput(){},pauseOutput(){},resumeInput(){},resumeOutput(){},destroy(){assert.equal(this.destroyed,undefined);this.destroyed=true;}};created.push(terminal);return terminal;});
 await cache.attach({id:'selected'});await cache.attachSecondary({id:'split'},right);const selected=cache.current,split=cache.secondaryTerminal;
 for(let i=0;i<24;i++){
   await cache.attach({id:'task'+i});
   assert.equal(created.filter(terminal=>!terminal.destroyed).length<=8,true,'live xterm controllers exceed eight');
   assert.equal(split.destroyed,undefined);assert.equal(cache.current.destroyed,undefined);
 }
 assert.equal(selected.destroyed,true);assert.equal(cache.records.size,8);
 const recent=cache.current;await cache.attach({id:'task20'});await cache.attach({id:'new'});
 assert.equal(recent.destroyed,undefined);assert.equal(cache.records.has('task20'),true);assert.equal(cache.records.has('task17'),false);
 // Exercise a protected primary while the secondary changes beyond the budget too.
 const primary=cache.current;
 for(let i=0;i<24;i++){await cache.attachSecondary({id:'right'+i},right);assert.equal(primary.destroyed,undefined);assert.equal(cache.secondaryTerminal.destroyed,undefined);assert.equal(created.filter(terminal=>!terminal.destroyed).length,8);}
 cache.destroy();assert.equal(created.filter(terminal=>!terminal.destroyed).length,0);
});
