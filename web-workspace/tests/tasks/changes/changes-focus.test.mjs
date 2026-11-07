import test from 'node:test';
import assert from 'node:assert/strict';
import {TaskChanges} from '../../../tasks/changes/task-changes.js';
import {focusDOM} from '../../shared/focus-dom.mjs';

function fixture(t,mode='flat') {
  const dom=focusDOM(t),panel=dom.element('section');dom.body.append(panel);
  const changes=new TaskChanges({panel,mode,api:{},task:{id:'a'},signal:new AbortController().signal,openDiff(){}});
  changes.show({BranchFiles:'M\tsrc/a.js\nM\tother/b.js'});
  const row=path=>panel.querySelectorAll('.file-row').find(item=>item.dataset.filePath===path);
  return {...dom,panel,changes,row};
}
for(const control of ['row','trigger'])test(`status refresh restores equivalent ${control}`,t=>{
  const {changes,row}=fixture(t);
  const target=()=>control==='row'?row('src/a.js'):row('src/a.js').closest('.file-entry').querySelector('.file-more');
  target().focus();changes.show({BranchFiles:'A\taaa.txt\nA\tsrc/a.js\nM\tother/b.js'});
  assert.equal(document.activeElement,target());
});
test('tree refresh restores equivalent folder summary',t=>{
  const {changes,panel}=fixture(t,'tree');
  const summary=()=>panel.querySelectorAll('summary').find(item=>item.getAttribute('aria-label')==='src');
  summary().focus();changes.show({BranchFiles:'M\tsrc/a.js\nA\tsrc/new.js\nM\tother/b.js'});
  assert.equal(document.activeElement,summary());
});
test('removed path chooses neighbour then Changes tab for empty list',t=>{
  const {changes,row,tab}=fixture(t);row('src/a.js').focus();
  changes.show({BranchFiles:'M\tother/b.js'});assert.equal(document.activeElement,row('other/b.js'));
  changes.show({});assert.equal(document.activeElement,tab);
});
test('background Changes refresh leaves outside focus alone',t=>{
  const {changes,tab}=fixture(t);tab.focus();
  changes.show({BranchFiles:'A\tsrc/a.js'});assert.equal(document.activeElement,tab);
});
test('removing the first focused row selects the next neighbour',t=>{
  const {changes,row}=fixture(t);row('other/b.js').focus();
  changes.show({BranchFiles:'M\tsrc/a.js'});assert.equal(document.activeElement,row('src/a.js'));
});
test('removing a focused folder falls back to a remaining row',t=>{
  const {changes,panel,row}=fixture(t,'tree');panel.querySelectorAll('summary').find(item=>item.getAttribute('aria-label')==='src').focus();
  changes.show({BranchFiles:'M\tother/b.js'});assert.equal(document.activeElement,row('other/b.js'));
});
