import {test} from 'node:test';import assert from 'node:assert/strict';import {safeTaskLinks,fileCopyPath} from '../../tasks/task-information.js';
test('links allow only credential-free http(s)',()=>{assert.deepEqual(safeTaskLinks([{url:'javascript:alert(1)'},{url:'file:///tmp/foo'},{url:'https://user:pass@github.com/x'},{url:'https://github.com/x',label:'PR'}]),[{url:'https://github.com/x',label:'PR'}]);});
test('copy paths join worktree with relative changed paths',()=>{assert.equal(fileCopyPath('/tmp/task','src/a b.js'),'/tmp/task/src/a b.js');assert.equal(fileCopyPath(null,'src/a.js'),'src/a.js');});
import {changedFileTree} from '../../tasks/task-information.js';
test('changed file tree groups shared directories without losing paths',()=>{
 const tree=changedFileTree([{path:'src/a.go',status:'M'},{path:'src/nested/b.go',status:'??'}]);assert.equal(tree.length,1);assert.equal(tree[0].children[1].children[0].path,'src/nested/b.go');
});
test('unsafe and empty URL values never become clickable',()=>{assert.deepEqual(safeTaskLinks([{url:''},{},{url:'data:text/html,hi'},{url:'//example.com'}]),[]);});
