import test from 'node:test';
import assert from 'node:assert/strict';
import {focusDOM} from './focus-dom.mjs';
import {captureFocus} from '../../shared/focus-recovery.js';

function setup(t){
 const dom=focusDOM(t),root=dom.element('section');dom.body.append(root);
 const proto=Object.getPrototypeOf(root);proto.getClientRects=function(){return this.isConnected?[{}]:[];};
 const listeners=new Set();
 document.addEventListener=(type,listener)=>{if(type==='focusin')listeners.add(listener);};
 document.removeEventListener=(type,listener)=>{if(type==='focusin')listeners.delete(listener);};
 const focus=proto.focus,matches=proto.matches;
 proto.focus=function(){focus.call(this);for(const listener of [...listeners])listener({target:this});};
 proto.matches=function(selector){return selector==='[tabindex="0"]'?this.getAttribute('tabindex')==='0':matches.call(this,selector);};
 const control=(key,text='Run now')=>{const item=dom.element('button');item.dataset.focusKey=key;item.textContent=text;root.append(item);return item;};
 return {...dom,root,control};
}
test('focus recovery matches record identity across reorder and duplicated action labels',t=>{
 const {root,control}=setup(t);control('a');control('b').focus();const restore=captureFocus(root);
 root.replaceChildren();const expected=control('b');control('a');restore();assert.equal(document.activeElement,expected);
});
test('focus recovery chooses neighboring action then external fallback when empty',t=>{
 const {root,control,tab}=setup(t);control('a');control('b').focus();let restore=captureFocus(root,()=>tab);
 root.replaceChildren();const expected=control('a');restore();assert.equal(document.activeElement,expected);
 restore=captureFocus(root,()=>tab);root.replaceChildren();restore();assert.equal(document.activeElement,tab);
});
test('outside focus and aborted work prevent focus recovery',t=>{
 const {root,control,tab}=setup(t);control('a').focus();let restore=captureFocus(root);root.replaceChildren();control('b');tab.focus();restore();assert.equal(document.activeElement,tab);
 control('c').focus();const abort=new AbortController();restore=captureFocus(root,()=>tab,{signal:abort.signal});root.replaceChildren();control('d');abort.abort();restore();assert.equal(document.activeElement,document.body);
});
test('moving to another surviving control in the same section retains that choice',t=>{
 const {root,control}=setup(t);const first=control('a'),second=control('b');first.focus();const restore=captureFocus(root);second.focus();restore();assert.equal(document.activeElement,second);
});

test('action neighbor and absolute position choose different controls in an uneven form',t=>{
 const {root,control,element}=setup(t);
 for(let i=0;i<3;i++)root.append(element('input'));
 control('a-approve','Approve paths').focus();control('a-reject','Reject proposal');
 const restore=captureFocus(root);root.replaceChildren();root.append(element('input'));
 const approve=control('b-approve','Approve paths'),reject=control('b-reject','Reject proposal');restore();
 assert.equal(document.activeElement,approve);assert.notEqual(document.activeElement,reject);
});
test('remembered departure survives removal of the chosen outside control',t=>{
 const {root,control,element,body}=setup(t);control('a').focus();const restore=captureFocus(root);
 const outside=element('button');body.append(outside);outside.focus();outside.remove();
 root.replaceChildren();control('a');restore();assert.equal(document.activeElement,document.body);
});
test('connected programmatic origin wins when it is absent from candidate selectors',t=>{
 const {root,element,tab}=setup(t),origin=element('p');origin.setAttribute('tabindex','-1');root.append(origin);origin.focus();
 const restore=captureFocus(root,()=>tab,{neighbors:false});document.activeElement=document.body;restore();
 assert.equal(document.activeElement,origin);
});
for(const open of [false,true])test(`disclosure descendants require an open ancestor open=${open}`,t=>{
 const {root,control,element}=setup(t);control('origin','Archive').focus();const details=element('details'),summary=element('summary'),hidden=element('button');
 details.open=open;hidden.textContent='Archive';details.append(summary,hidden);root.append(details);const next=control('next','Archive');
 const restore=captureFocus(root,undefined,{selector:'button'});root.children[0].remove();restore();assert.equal(document.activeElement,open?hidden:next);
});
test('the first summary and its controls remain available inside a closed disclosure',t=>{
 const {root,control,element}=setup(t);control('origin','Archive').focus();const details=element('details'),summary=element('summary'),action=element('button');
 details.open=false;action.textContent='Archive';summary.append(action);details.append(summary);root.append(details);
 const restore=captureFocus(root,undefined,{selector:'button'});root.children[0].remove();restore();assert.equal(document.activeElement,action);
});
test('a candidate that silently refuses focus falls through to the next action',t=>{
 const {root,control}=setup(t);control('origin','Archive').focus();const restore=captureFocus(root);
 root.replaceChildren();const blocked=control('origin','Archive');blocked.focus=()=>{};const next=control('next','Archive');restore();
 assert.equal(document.activeElement,next);
});
for(const attribute of ['hidden','inert'])test(`focus recovery skips the ${attribute} subtree`,t=>{
 const {root,control,element}=setup(t);control('origin','Archive').focus();
 const container=element('div'),blocked=element('button');container.setAttribute(attribute,'');blocked.textContent='Archive';container.append(blocked);root.append(container);
 const next=control('next','Archive'),restore=captureFocus(root,undefined,{selector:'button'});root.children[0].remove();restore();assert.equal(document.activeElement,next);
});
test('a visible inner summary still requires its outer closed disclosure to expose it',t=>{
 const {root,control,element}=setup(t);control('origin','Archive').focus();
 const outer=element('details'),first=element('summary'),inner=element('details'),summary=element('summary'),blocked=element('button');
 outer.open=false;inner.open=true;blocked.textContent='Archive';summary.append(blocked);inner.append(summary);outer.append(first,inner);root.append(outer);
 const next=control('next','Archive'),restore=captureFocus(root,undefined,{selector:'button'});root.children[0].remove();restore();assert.equal(document.activeElement,next);
});
