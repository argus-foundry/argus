import test from 'node:test';
import assert from 'node:assert/strict';
import {SettingsNavigation} from '../../settings/settings-navigation.js';
import {focusDOM} from '../shared/focus-dom.mjs';

for(const section of ['agents','add-repository','repository:demo'])test(`repository reload retains focused section ${section}`,async t=>{
  const {body}=focusDOM(t);let release;
  const navigation=new SettingsNavigation({json:()=>new Promise(resolve=>{release=resolve;})},()=>{},()=>{});
  body.append(navigation.element);navigation.projects=[{name:'demo'}];navigation.render();
  const pending=navigation.load();
  const entry=()=>[...navigation.nav.querySelectorAll('[data-section]')].find(item=>item.dataset.section===section);
  entry().focus();release({projects:[{name:'demo'},{name:'another'}]});await pending;
  assert.equal(document.activeElement,entry());
});
for(const owner of ['search','back','selector','content'])test(`repository reload leaves ${owner} focus alone`,async t=>{
  const {body,element}=focusDOM(t);let release;
  const navigation=new SettingsNavigation({json:()=>new Promise(resolve=>{release=resolve;})},()=>{},()=>{});
  body.append(navigation.element);const content=element('input');body.append(content);
  const pending=navigation.load();navigation.nav.querySelector('[data-section]').focus();
  const target=owner==='content'?content:navigation[owner];target.focus();
  release({projects:[]});await pending;assert.equal(document.activeElement,target);
});
