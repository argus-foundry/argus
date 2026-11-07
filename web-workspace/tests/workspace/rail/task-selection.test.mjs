import {test} from 'node:test';
import assert from 'node:assert/strict';
import {focusDOM} from '../../shared/focus-dom.mjs';
import {TaskSelection} from '../../../workspace/rail/task-selection.js';

test('selection preserves collapsed IDs and prunes only removed or archived-elsewhere tasks',()=>{
 const selection=Object.create(TaskSelection.prototype);selection.ids=new Set(['visible','hidden','removed','moved']);selection.header=()=>{};
 selection.navigation={filter:'active',tasks:[{id:'visible'},{id:'hidden'},{id:'arrival'},{id:'moved',archived:true}]};
 selection.prune();assert.deepEqual([...selection.ids],['visible','hidden']);
});
test('batch failure outcomes retain failed and undispatched IDs separately from success',t=>{
 const {body,element}=focusDOM(t),message=element('div');message.id='notice';body.append(message);document.getElementById=id=>body.children.find(item=>item.id===id);const timer=globalThis.setTimeout;globalThis.setTimeout=()=>0;t.after(()=>globalThis.setTimeout=timer);
 const selection=Object.create(TaskSelection.prototype);let renders=0;
 Object.assign(selection,{busy:true,active:true,ids:new Set(['success','failure','later']),alert:{},navigation:{tasks:[{id:'failure',name:'Failed task'}],render(){renders++;}}});
 selection.finish({succeeded:['success'],failed:[{id:'failure'}],notStarted:['later']});
 assert.equal(selection.busy,false);assert.equal(selection.active,true);assert.deepEqual([...selection.ids],['failure','later']);assert.match(selection.alert.textContent,/1 succeeded, 1 failed, 1 not started/);assert.equal(renders,1);
 selection.finish({succeeded:['failure','later'],failed:[],notStarted:[]});assert.equal(selection.active,false);assert.equal(selection.alert.hidden,true);
});
