import test from 'node:test';
import assert from 'node:assert/strict';
import {TaskTools} from '../../tasks/tools.js';

function dom(t) {
  const elements=[];
  function element() {
    const item={children:[],childNodes:[],isConnected:true,dataset:{},
      append(...children){this.children.push(...children);this.childNodes=this.children;},
      replaceChildren(...children){this.children=children;this.childNodes=children;},
      setAttribute(name,value){this[name]=value;},addEventListener(){},remove(){},
    };
    elements.push(item);return item;
  }
  const panel=element();
  const saved=globalThis.document;
  globalThis.document={getElementById:()=>panel,createElement:element};
  t.after(()=>{globalThis.document=saved;});
  return elements;
}

test('Info shares a pending raw identity read across task updates', t => {
  dom(t);
  let reads=0;
  const tools=Object.assign(Object.create(TaskTools.prototype),{
    api:{json(){reads++;return new Promise(()=>{});}},
    task:{id:'a',status:'complete'},tool:'status',generation:0,
    visible:()=>true,
  });
  for(let i=0;i<100;i++)tools.updateTask({...tools.task});
  assert.equal(reads,1);
});

test('Info announces unavailable identity instead of default values', async t => {
  const elements=dom(t);
  const tools=Object.assign(Object.create(TaskTools.prototype),{
    api:{async json(path){if(path.endsWith('/raw'))throw new Error('offline');return {links:[]};}},
    task:{id:'a'},tool:'status',generation:0,visible:()=>true,
  });
  await tools.drawStatus();
  assert.ok(elements.some(item=>item.role==='alert'),'identity failure must be announced');
  assert.equal(elements.some(item=>['Default','default'].includes(item.textContent)),false);
});

test('hidden Info defers identity reads and invalidation is scoped to the selected task', t => {
  dom(t);
  let reads=0;
  const tools=Object.assign(Object.create(TaskTools.prototype),{
    api:{json(){reads++;return new Promise(()=>{});}},
    task:{id:'a'},tool:'status',generation:0,visible:()=>false,
  });
  tools.updateTask({...tools.task});assert.equal(reads,0);
  tools.visible=()=>true;tools.updateTask({...tools.task});assert.equal(reads,1);
  const identity=tools.identity;
  tools.invalidateIdentity('b');assert.equal(tools.identity,identity);assert.equal(identity.abort.signal.aborted,false);
  tools.invalidateIdentity('a');assert.equal(identity.abort.signal.aborted,true);
  tools.updateTask({...tools.task});assert.equal(reads,2);
});
