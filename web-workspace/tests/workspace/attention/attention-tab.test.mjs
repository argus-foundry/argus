import {test} from 'node:test';
import assert from 'node:assert/strict';
import {AttentionTab} from '../../../workspace/attention/attention-tab.js';

function fixture(t){
 t.mock.method(console,'debug',()=>{});
 const writes=[],labels=[],encodings=[],titles=[],images=[];
 const icon={getAttribute:()=>'/icon.png',set href(value){writes.push(value);}};
 globalThis.document={querySelector:()=>icon,set title(value){titles.push(value);},createElement:()=>({
  getContext:()=>({drawImage(){},beginPath(){},arc(){},fill(){},fillText:value=>labels.push(value)}),
  toDataURL:type=>{encodings.push(type);return 'data:image/png,badge';},
 })};
 globalThis.Image=class {constructor(){this.naturalWidth=0;images.push(this);}};
 const tab=new AttentionTab();
 return {tab,writes,labels,encodings,titles,load:()=>{images[0].naturalWidth=192;images[0].onload();}};
}
test('unchanged attention events skip title, favicon writes and PNG encoding',t=>{
 const {tab,writes,encodings,titles,load}=fixture(t);tab.update(1);load();
 const counts=[writes.length,encodings.length,titles.length];
 for(let i=0;i<200;i++)tab.update(1);
 assert.deepEqual([writes.length,encodings.length,titles.length],counts);
});
test('image load draws the latest count and zero restores the original icon once',t=>{
 const {tab,writes,labels,encodings,load}=fixture(t);
 tab.update(1);tab.update(3);tab.update(3);assert.equal(encodings.length,0);
 load();assert.deepEqual(labels,['3']);assert.equal(encodings.length,1);
 tab.update(0);assert.equal(writes.at(-1),'/icon.png');
 const count=writes.length;tab.update(0);assert.equal(writes.length,count);
});
test('clearing attention before image load never draws a stale badge',t=>{
 const {tab,writes,encodings,load}=fixture(t);tab.update(2);tab.update(0);load();
 assert.equal(encodings.length,0);assert.equal(writes.at(-1),'/icon.png');
});
test('counts above the badge cap update the title without re-encoding the same badge',t=>{
 const {tab,labels,encodings,titles,load}=fixture(t);tab.update(100);load();tab.update(101);
 assert.deepEqual(labels,['99']);assert.equal(encodings.length,1);assert.equal(titles.at(-1),'(101) Theia');
 tab.update(2);assert.deepEqual(labels,['99','2']);assert.equal(encodings.length,2);
});
test('initial zero count restores the title and keeps subsequent draws idle',t=>{
 const {tab,writes,titles}=fixture(t);tab.update(0);tab.update(0);tab.draw();
 assert.deepEqual(titles,['Theia']);assert.deepEqual(writes,['/icon.png']);
});
