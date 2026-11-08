import {test} from 'node:test';
import assert from 'node:assert/strict';
import {focusDOM} from './focus-dom.mjs';
import {menuIcon} from '../../shared/menu-icon.js';
import {rowMenu} from '../../shared/row-menu.js';
import {settingsIcon} from '../../settings/settings-icon.js';

test('every entry receives one leading icon including future, disabled and link items',t=>{
  focusDOM(t);
  const entries=[{label:'Future action'},{label:'Disabled action',disabled:true},{label:'Open in editor',href:'vscode://file/tmp/test',icon:'external'}];
  const [,menu]=rowMenu({label:'Actions',entries});
  assert.equal(menu.children.length,3);
  for(const [index,item]of menu.children.entries()){
    assert.equal(item.textContent,entries[index].label);
    assert.equal(item.children[0].tagName,'svg');
    assert.equal(item.querySelectorAll('svg').length,1);
    assert.equal(item.children[0].getAttribute('aria-hidden'),'true');
    assert.equal(item.getAttribute('role'),'menuitem');
  }
  assert.equal(menu.children[1].disabled,true);
  assert.equal(menu.children[2].href,entries[2].href);
});

test('destructive entry separates its inherited-color icon and label from other actions',t=>{
  focusDOM(t);
  const [,menu]=rowMenu({label:'Actions',entries:[{label:'Copy'},{label:'Delete',icon:'delete',danger:true}]});
  assert.equal(menu.children[1].getAttribute('role'),'separator');
  assert.match(menu.children[2].className,/danger-menu-item/);
  assert.equal(menu.children[2].children[0].getAttribute('stroke'),'currentColor');
  assert.equal(menu.children[2].children[0].children[0].getAttribute('d'),menuIcon('delete').children[0].getAttribute('d'));
});

test('stroke geometry is shared across settings and menus while repository letters remain avatars',t=>{
  focusDOM(t);
  for(const name of ['settings','pin','unpin','copy','branch','folder','external','active','backlog','blocked','done','delete','notifications','agents','sandbox','schedules','system','maintenance','update','logs','add']){
    const icon=menuIcon(name);
    assert.equal(icon.getAttribute('width'),'16');
    assert.equal(icon.getAttribute('height'),'16');
    assert.equal(icon.getAttribute('stroke-width'),'1.5');
    assert.equal(icon.getAttribute('fill'),'none');
    assert.equal(icon.getAttribute('focusable'),'false');
    assert.notEqual(icon.children[0].getAttribute('d'),menuIcon().children[0].getAttribute('d'),name);
  }
  assert.deepEqual(settingsIcon('general').attributes,menuIcon('settings').attributes);
  assert.equal(settingsIcon('repository:fixture','fixture').textContent,'F');
});

test('future icon names fall back safely without treating entry content as SVG markup',t=>{
  focusDOM(t);
  const icon=menuIcon('<script>alert(1)</script>');
  assert.equal(icon.children.length,1);
  assert.equal(icon.children[0].tagName,'path');
  assert.equal(icon.children[0].getAttribute('d'),'M5 7h14M5 12h14M5 17h14');
});

test('Delete uses the trash icon and divider in live and archived task menus',async t=>{
  const {body}=focusDOM(t);
  Object.getPrototypeOf(body).removeAttribute=function(key){delete this.attributes[key];};
  const {taskMenu}=await import('../../workspace/rail/task-menu.js');
  for(const archived of [false,true]){
    const [,menu]=taskMenu({id:'task',name:'Task',archived},{settings:()=>{},deleteTask:()=>{}});
    const item=menu.children.at(-1);
    assert.equal(item.textContent,'Delete');
    assert.equal(menu.children.at(-2).getAttribute('role'),'separator');
    assert.match(item.className,/danger-menu-item/);
    assert.equal(item.children[0].children[0].getAttribute('d'),'M3 6h18M9 6V3h6v3M6 6l1 15h10l1-15M10 10v7m4-7v7');
  }
});
