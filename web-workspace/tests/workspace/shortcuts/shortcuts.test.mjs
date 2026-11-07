import { test } from 'node:test';
import assert from 'node:assert/strict';
import { shortcutAction } from '../../../workspace/shortcuts/shortcuts-policy.js';
const key = (key, extra = {}) => ({ key, ...extra });
test('terminal allowlist preserves ordinary input and native editing', () => {
  for (const event of [key('c',{metaKey:true}),key('v',{metaKey:true}),key('ArrowUp'),key('r',{ctrlKey:true}),key('n',{altKey:true,shiftKey:true})]) assert.equal(shortcutAction(event,true),null);
  assert.equal(shortcutAction(key('ArrowDown',{metaKey:true}),true),'next');
  assert.equal(shortcutAction(key('PageUp',{shiftKey:true}),true),'page-up');
  assert.equal(shortcutAction(key('ArrowDown',{metaKey:true,ctrlKey:true}),true),null);
  assert.equal(shortcutAction(key('c',{metaKey:true,shiftKey:true}),true),'copy-output');
});
test('global browser equivalents ignore repeats and composition', () => {
  assert.equal(shortcutAction(key('1',{altKey:true}),false),'agent');
  assert.equal(shortcutAction(key('F',{altKey:true,shiftKey:true}),false),'filter');
  assert.equal(shortcutAction(key('F',{altKey:true,shiftKey:true,isComposing:true}),false),null);
  assert.equal(shortcutAction(key('F',{altKey:true,shiftKey:true,repeat:true}),false),null);
});
test('global Alt shortcuts resolve physical keys when macOS Option produces Unicode',()=>{
 assert.equal(shortcutAction(key('Ï',{code:'KeyF',altKey:true,shiftKey:true}),false),'filter');
 assert.equal(shortcutAction(key('¡',{code:'Digit1',altKey:true}),false),'agent');
 assert.equal(shortcutAction(key('¿',{code:'Slash',altKey:true,shiftKey:true}),false),'help');
});
test('split chords are explicit and never claim ordinary terminal Enter',()=>{
 assert.equal(shortcutAction(key('D',{altKey:true,shiftKey:true,code:'KeyD'}),false),'split');assert.equal(shortcutAction(key('Enter',{metaKey:true,shiftKey:true}),true),'split');assert.equal(shortcutAction(key('Enter',{shiftKey:true}),true),null);assert.equal(shortcutAction(key('Enter',{ctrlKey:true,metaKey:true,shiftKey:true}),true),null);
});

test('shortcut help names Copy worktree path and the consolidated Changes tab',async()=>{
 const {shortcutHelp}=await import('../../../workspace/shortcuts/shortcuts-policy.js');const help=shortcutHelp.flat().join(' ');
 assert.match(help,/Copy worktree path/);assert.match(help,/Agent \/ Review \/ Changes \/ Info/);assert.doesNotMatch(help,/Open worktree/);
});
