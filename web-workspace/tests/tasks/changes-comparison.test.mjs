import test from 'node:test';
import assert from 'node:assert/strict';
import {comparisonText} from '../../tasks/changes/comparison.js';
for(const [name,comparison,expected] of [
 ['stacked',{requested_base:'parent',resolved_base:'parent',merge_base:'abc123',fallback_reason:null},/Requested parent.*merge-base abc123 of parent/],
 ['fallback',{requested_base:'missing',resolved_base:'origin/master',merge_base:'def456',fallback_reason:'base missing'},/base missing.*Effective base: origin\/master/],
 ['unborn',{requested_base:'',resolved_base:'',merge_base:'',fallback_reason:null},/Branch comparison unavailable/],
 ['older daemon',undefined,/Comparison unavailable/],
])test(`${name} comparison explains returned provenance`,()=>{
 const text=comparisonText(comparison);assert.match(text,expected);
 if(comparison){assert.match(text,/Uncommitted vs HEAD/);assert.match(text,/Untracked contents/);}
});
