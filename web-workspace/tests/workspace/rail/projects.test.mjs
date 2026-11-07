import test from 'node:test';
import assert from 'node:assert/strict';
import { readProjects } from '../../../workspace/rail/projects.js';

test('project reads preserve the authenticated API signal and empty successful lists', async t => {
  for (const result of [{ projects: [{ name: 'empty-project' }] }, {}]) await t.test(JSON.stringify(result), async () => {
    const signal = new AbortController().signal;
    const calls = [];
    const api = { json: async (...args) => { calls.push(args); return result; } };
    assert.deepEqual(await readProjects(api, signal), { projects: result.projects || [] });
    assert.deepEqual(calls, [['/api/projects/full', { signal, revalidate: true }]]);
  });
});

test('non-auth project errors produce an optional warning without replacing cached projects', async t => {
  for (const status of [500, undefined]) await t.test(String(status || 'network'), async () => {
    const error = Object.assign(new Error('Private response content'), { status });
    const result = await readProjects({ json: async () => { throw error; } }, new AbortController().signal);
    assert.equal(result.projects, undefined);
    assert.match(result.warning, /Project list unavailable/);
    assert.doesNotMatch(result.warning, /Private response content/);
  });
});

test('project authentication and cancellation errors propagate without fallback', async t => {
  for (const kind of [401, 403, 'AbortError', 'aborted signal']) await t.test(String(kind), async () => {
    const controller = new AbortController();
    if (kind === 'aborted signal') controller.abort();
    const error = Object.assign(new Error('Failed'), { status: kind, name: kind === 'AbortError' ? kind : 'Error' });
    await assert.rejects(readProjects({ json: async () => { throw error; } }, controller.signal), caught => caught === error);
  });
});
