import { test } from 'node:test';
import assert from 'node:assert/strict';
import { batches, discoverTests, executedTests, mergeProfiles, pattern, replacePackages, verifyRuns } from './test-batches.mjs';

test('inventory and anchored batches run each top-level test once', () => {
  const names = discoverTests('TestOne\nTestTwo\nok pkg\n');
  assert.deepEqual(batches(names, 1), [['TestOne'], ['TestTwo']]);
  assert.match('TestOne', new RegExp(pattern(names)));
  assert.doesNotMatch('TestOneSuffix', new RegExp(pattern(names)));
  const ran = executedTests('{"Action":"run","Test":"TestOne"}\n{"Action":"run","Test":"TestOne/sub"}\n{"Action":"pass","Test":"TestOne"}\n{"Action":"run","Test":"TestTwo"}\n');
  verifyRuns(names, ran);
  assert.throws(() => verifyRuns(names, ['TestOne', 'TestOne']));
  assert.throws(() => verifyRuns(names, ['TestOne']));
  assert.throws(() => discoverTests('ok pkg'));
  assert.throws(() => discoverTests('TestOne\nTestOne'));
  assert.throws(() => batches(names, 0));
  assert.deepEqual(discoverTests('Test\nTestÉxample\n'), ['Test', 'TestÉxample']);
});

test('coverage sums atomic counters and rejects mismatched sources', () => {
  const a = 'mode: atomic\npkg/a.go:1.1,2.2 2 3\n';
  const b = 'mode: atomic\npkg/a.go:1.1,2.2 2 7\npkg/b.go:2.1,3.1 1 0\n';
  assert.equal(mergeProfiles([a, b]), 'mode: atomic\npkg/a.go:1.1,2.2 2 10\npkg/b.go:2.1,3.1 1 0\n');
  assert.throws(() => mergeProfiles([a, b.replace('atomic', 'count')]));
  assert.throws(() => mergeProfiles([a, b.replace('2 7', '3 7')]));
  assert.throws(() => mergeProfiles(['nonsense']));
});

test('replacement removes only the exact package, preserving child packages', () => {
  const base = 'mode: atomic\npkg/api/a.go:1.1,2.2 1 0\npkg/api/child/b.go:1.1,2.2 1 9\npkg/db/c.go:1.1,2.2 1 4\n';
  const replacement = 'mode: atomic\npkg/api/a.go:1.1,2.2 1 5\n';
  const result = replacePackages(base, [{ pkg: 'pkg/api', text: replacement }]);
  assert.match(result, /pkg\/api\/a.go:1.1,2.2 1 5/);
  assert.match(result, /pkg\/api\/child\/b.go:1.1,2.2 1 9/);
  assert.match(result, /pkg\/db\/c.go:1.1,2.2 1 4/);
  assert.throws(() => replacePackages(base, [{ pkg: 'pkg/api', text: replacement.replace('pkg/api', 'pkg/other') }]));
});
