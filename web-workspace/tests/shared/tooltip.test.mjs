import test from 'node:test';
import assert from 'node:assert/strict';
import { tooltipPosition } from '../../shared/tooltip.js';
for (const [name, target, size, viewport, expected] of [
  ['above', {left:100,top:100,width:40,bottom:120}, {width:80,height:30}, {width:400,height:300}, {left:80,top:62}],
  ['flip below at top', {left:0,top:0,width:20,bottom:20}, {width:80,height:30}, {width:400,height:300}, {left:8,top:28}],
  ['clamp right and bottom', {left:395,top:290,width:10,bottom:300}, {width:80,height:30}, {width:400,height:300}, {left:312,top:252}],
]) test(name, () => assert.deepEqual(tooltipPosition(target,size,viewport),expected));
