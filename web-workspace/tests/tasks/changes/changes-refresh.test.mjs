import test from 'node:test';
import assert from 'node:assert/strict';
import {TaskChanges} from '../../../tasks/changes/task-changes.js';
import {focusDOM} from '../../shared/focus-dom.mjs';
const settle=()=>new Promise(setImmediate);
function fixture(t,api={}){
 const dom=focusDOM(t),proto=Object.getPrototypeOf(dom.body),query=proto.querySelector;
 proto.querySelector=function(selector){return selector===':scope > .directory-files'?this.children.find(child=>child.className==='directory-files')||null:query.call(this,selector);};
 proto.getClientRects=function(){return this.isConnected?[{}]:[];};document.addEventListener=()=>{};document.removeEventListener=()=>{};
 const panel=dom.element('section');dom.body.append(panel);
 const changes=new TaskChanges({api,task:{id:'a'},panel,signal:new AbortController().signal,openDiff(){}});changes.observeRows=()=>{};
 const row=path=>panel.querySelectorAll('.file-row').find(item=>item.dataset.filePath===path);
 return {...dom,panel,changes,row};
}
test('metadata revisions retain committed counts and the identical visible list',t=>{
 const {changes}=fixture(t),snapshot={BranchFiles:'M\tcommitted.txt',Status:' M dirty.txt',git_revision:'r1'};changes.show(snapshot);
 const committed={added:2,removed:1};changes.counts.results.set('committed.txt',committed);changes.counts.results.set('dirty.txt',{added:1,removed:0});const list=changes.list;
 changes.show({...snapshot,git_revision:'r2'});assert.equal(changes.counts.results.get('committed.txt'),committed);assert.equal(changes.counts.results.has('dirty.txt'),false);assert.equal(changes.list,list);
});
for(const reason of ['external edit','child discard'])test(`nested untracked expansion survives ${reason} and removed children disappear`,async t=>{
 let leaf='draft/sub/leaf.txt';const api={json:async path=>({Files:path.includes('dir=draft%2Fsub%2F')?[{Path:leaf,IsDir:false}]:[{Path:'draft/sub/',IsDir:true}]})};
 const f=fixture(t,api);f.changes.show({Status:'?? draft/',git_revision:'r1'});await f.changes.expandDirectory(f.changes.files[0],f.row('draft/'),f.row('draft/').closest('.file-entry'));
 await f.changes.expandDirectory({path:'draft/sub/',status:'??',directory:true},f.row('draft/sub/'),f.row('draft/sub/').closest('.file-entry'));
 assert.ok(f.row(leaf));if(reason==='child discard')leaf='draft/sub/new.txt';f.changes.show({Status:'?? draft/',git_revision:'r2'});await settle();
 assert.equal(f.row('draft/sub/').getAttribute('aria-expanded'),'true');assert.ok(f.row(leaf));if(reason==='child discard')assert.equal(f.row('draft/sub/leaf.txt'),undefined);
});
test('directory settlement preserves focus acquired in a retained child during the read',async t=>{
 let release;const f=fixture(t,{json:()=>new Promise(resolve=>{release=resolve;})});f.changes.show({Status:'?? draft/',git_revision:'r1'});
 const parent=f.row('draft/'),container=parent.closest('.file-entry');const initial=f.changes.expandDirectory(f.changes.files[0],parent,container);release({Files:[{Path:'draft/child.txt',Status:'??'}]});await initial;
 f.tab.focus();const flight=f.changes.expandDirectory(f.changes.files[0],parent,container,true);f.row('draft/child.txt').focus();release({Files:[{Path:'draft/child.txt',Status:'??'}]});await flight;
 assert.equal(document.activeElement,f.row('draft/child.txt'));
});
test('one dirty metadata revision rereads its count without rereading thirty committed paths',async t=>{
 const reads=[],f=fixture(t,{json:async path=>{reads.push(new URL(path,'http://localhost').searchParams.get('path'));return {Diff:'diff --git a/f b/f\n@@ -1 +1 @@\n-old\n+new'};}});
 const snapshot={BranchFiles:Array.from({length:30},(_,i)=>`M\tbranch-${i}.txt`).join('\n'),Status:' M dirty.txt',git_revision:'r1'};
 f.changes.show(snapshot);f.changes.counts.add(f.changes.files);await settle();assert.equal(reads.length,31);
 const list=f.changes.list;f.changes.show({...snapshot,git_revision:'r2'});f.changes.counts.add(f.changes.files);await settle();assert.deepEqual(reads.slice(31),['dirty.txt']);assert.equal(f.changes.list,list);
});
