import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const html=readFileSync(new URL('../../internal/api/static/index.html',import.meta.url),'utf8');
test('phone Decide launch uses authenticated API headers and current same-origin context',async()=>{
 const calls=[],badge={textContent:''},entry={disabled:false},location={origin:'https://argus.test',assign:url=>calls.push(url)};
 const context={URL,location,document:{hidden:false,getElementById:id=>id==='decide-count'?badge:entry},showToast:text=>calls.push(text),api:async(path,opts)=>{calls.push({path,opts});return {ok:true,json:async()=>({path:'/decide',ticket:'ticket',total:3})};}};
 const script=html.match(/\/\* Decide entry \*\/([\s\S]*?)\/\* End Decide entry \*\//)?.[1];assert.ok(script);
 vm.runInNewContext(script,context);
 await context.refreshDecideCounts();assert.equal(badge.textContent,'3');
 await context.openDecide();assert.equal(calls.at(-1),'/decide#t=ticket');
 assert.equal(calls[1].path,'/api/hera/board/decision-link');assert.equal(calls[1].opts.method,'POST');
 context.api=async()=>({ok:false});await context.refreshDecideCounts();assert.equal(badge.textContent,'?');
 await context.openDecide();assert.match(calls.at(-1),/retry/i);assert.equal(entry.disabled,false);
 context.api=async()=>({ok:true,json:async()=>({path:'https://evil.test',ticket:'x'})});await context.openDecide();assert.match(calls.at(-1),/retry/i);
});
