import test from 'node:test';
import assert from 'node:assert/strict';
import { artifactPath, previewBlob, PREVIEW_LIMIT } from '../../review/artifact-view.js';

test('artifact paths remain within their task manifest prefix', () => {
  const task = { id: 'task/a' };
  assert.equal(artifactPath(task, { filename: 'folder/report #1.html' }), '/api/tasks/task%2Fa/artifacts/folder/report%20%231.html');
  for (const filename of ['../../other/output', 'folder/../output', '/absolute.html', './file', 'folder//file', 'a\\..\\b', '', undefined]) {
    assert.throws(() => artifactPath(task, { filename }));
  }
});

test('streaming artifact cap applies without Content-Length and cancels the reader', async () => {
  let canceled = false;
  const stream = new ReadableStream({
    pull(controller) { controller.enqueue(new Uint8Array(PREVIEW_LIMIT + 1)); },
    cancel() { canceled = true; },
  });
  await assert.rejects(previewBlob(new Response(stream)), /too large/);
  assert.equal(canceled, true);
});

test('announced oversized artifact cancels its stream without reading chunks', async () => {
  let reads = 0, canceled = false;
  const stream = new ReadableStream({
    pull(controller) { reads++; controller.enqueue(new Uint8Array([1])); },
    cancel() { canceled = true; },
  }, { highWaterMark: 0 });
  const response = new Response(stream, { headers: { 'Content-Length': String(PREVIEW_LIMIT + 1) } });
  await assert.rejects(previewBlob(response), /too large/);
  assert.equal(canceled, true);
  assert.equal(reads, 0);
  assert.equal(stream.locked, false);
});

test('bounded previews retain MIME type and exact bytes', async () => {
  const blob = await previewBlob(new Response(new Uint8Array([0, 1, 255]), { headers: { 'Content-Type': 'image/png' } }));
  assert.equal(blob.type, 'image/png');
  assert.deepEqual([...new Uint8Array(await blob.arrayBuffer())], [0, 1, 255]);
});
