import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { once } from 'node:events';
import { createWorkspaceServer, daemonURL } from '../../dev-server.mjs';

async function listen(server, port) {
  server.maxRequestsPerSocket = 1;
  server.listen(port, '127.0.0.1');
  await once(server, 'listening');
  return `http://127.0.0.1:${server.address().port}`;
}

async function close(server) {
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
}

async function withProxy(t, handler) {
  const upstream = http.createServer(handler);
  const api = await listen(upstream, 21744);
  const proxy = createWorkspaceServer({ api });
  const url = await listen(proxy, 21750);
  t.after(async () => { await close(proxy); await close(upstream); });
  return { api, url };
}

test('requires a fixed credential-free loopback API target', () => {
  assert.equal(daemonURL('http://127.0.0.1:7743').port, '7743');
  for (const address of ['https://example.com', 'http://example.com', 'http://localhost',
    'http://token@127.0.0.1:7743', 'http://127.0.0.1/api/', 'http://127.0.0.1?token=secret']) {
    assert.throws(() => daemonURL(address));
  }
});

test('preserves raw input method, body and authorization without forwarding browser cookies', async t => {
  const received = [];
  const { url } = await withProxy(t, async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    received.push({ method: request.method, url: request.url, headers: request.headers, body: Buffer.concat(chunks) });
    response.setHeader('Set-Cookie', 'private=value');
    response.end('{"status":"ok"}');
  });
  const body = Buffer.from('\x1b[200~hello\nworld\x1b[201~');
  const result = await fetch(`${url}/api/tasks/task-a/input`, { method: 'POST', body,
    headers: { Authorization: 'Bearer test-token', Cookie: 'browser=value', 'Content-Type': 'text/plain' } });
  assert.equal(result.status, 200);
  assert.equal(result.headers.get('set-cookie'), null);
  assert.equal(received[0].method, 'POST');
  assert.equal(received[0].url, '/api/tasks/task-a/input');
  assert.equal(received[0].headers.authorization, 'Bearer test-token');
  assert.equal(received[0].headers.cookie, undefined);
  assert.deepEqual(received[0].body, body);
});

test('retains byte cursors and streams SSE before upstream closes', async t => {
  let markClosed;
  const closed = new Promise(resolve => { markClosed = resolve; });
  const { url } = await withProxy(t, (request, response) => {
    if (request.url.includes('/output')) {
      response.setHeader('X-Output-Total', '123456');
      return response.end('terminal output');
    }
    assert.equal(request.url, '/api/tasks/a/stream?since=123456&token=test-token');
    response.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' });
    response.write('data: aGVsbG8=\n\n');
    const timer = setInterval(() => response.write(': ping\n\n'), 50);
    response.on('close', () => { clearInterval(timer); markClosed(); });
  });
  const snapshot = await fetch(`${url}/api/tasks/a/output`);
  assert.equal(snapshot.headers.get('x-output-total'), '123456');
  assert.equal(await snapshot.text(), 'terminal output');
  const controller = new AbortController();
  const stream = await fetch(`${url}/api/tasks/a/stream?since=123456&token=test-token`, { signal: controller.signal });
  const { value } = await stream.body.getReader().read();
  assert.match(new TextDecoder().decode(value), /data: aGVsbG8=/);
  controller.abort();
  await Promise.race([closed, new Promise((_, reject) => {
    const timer = setTimeout(() => reject(new Error('Upstream SSE subscription was not closed')), 1000);
    timer.unref();
  })]);
});

function rawRequest(url, headers) {
  return new Promise((resolve, reject) => {
    const request = http.get(url, { headers }, response => {
      response.resume();
      response.on('end', () => resolve(response.statusCode));
    });
    request.on('error', reject);
  });
}

test('rejects rebinding hosts and cross-site metadata without forwarding', async t => {
  let calls = 0;
  const { url } = await withProxy(t, (_request, response) => { calls++; response.end('unexpected'); });
  assert.equal(await rawRequest(`${url}/api/tasks`, { Host: 'evil.example', Origin: 'http://evil.example' }), 403);
  assert.equal(await rawRequest(`${url}/api/tasks`, { Host: 'localhost.evil.example', Origin: 'http://localhost.evil.example' }), 403);
  assert.equal(await rawRequest(`${url}/api/tasks`, { 'Sec-Fetch-Site': 'cross-site' }), 403);
  assert.equal(calls, 0);
});

test('accepts the literal localhost origin while retaining the fixed numeric API target', async t => {
  let calls = 0;
  const { url } = await withProxy(t, (_request, response) => { calls++; response.end('ok'); });
  const host = `localhost:${new URL(url).port}`;
  assert.equal(await rawRequest(`${url}/api/tasks`, { Host: host, Origin: `http://${host}` }), 200);
  assert.equal(calls, 1);
});

test('external links can navigate to local pages but cannot bypass the API guard', async t => {
  let calls = 0;
  const { url } = await withProxy(t, (_request, response) => { calls++; response.end('unexpected'); });
  const navigation = { Origin: 'https://external.example', 'Sec-Fetch-Site': 'cross-site',
    'Sec-Fetch-Mode': 'navigate', 'Sec-Fetch-Dest': 'document' };
  for (const route of ['/', '/diagnostics/wispr-check.html']) {
    assert.equal(await rawRequest(url + route, navigation), 200);
  }
  assert.equal(await rawRequest(`${url}/api/tasks`, navigation), 403);
  assert.equal(await rawRequest(`${url}/diagnostics/wispr-check.html`, { ...navigation, 'Sec-Fetch-Mode': 'cors' }), 403);
  assert.equal(calls, 0);
});

test('strips hop headers including fields named in Connection', async t => {
  let upstreamHeaders;
  const { url } = await withProxy(t, (request, response) => {
    upstreamHeaders = request.headers;
    response.end('ok');
  });
  assert.equal(await rawRequest(`${url}/api/tasks`, { Connection: 'close, x-private', 'X-Private': 'not-forwarded' }), 200);
  assert.equal(upstreamHeaders['x-private'], undefined);
});

test('serves only client assets and allowed existing vendor files', async t => {
  const { api, url } = await withProxy(t, (_request, response) => response.end(''));
  const vendor = await fetch(`${url}/vendor/xterm.js`);
  assert.equal(vendor.status, 200);
  assert.match(vendor.headers.get('content-type'), /javascript/);
  for (const resource of ['/dev-server.mjs', '/vendor/not-listed.js', '/%2e%2e%2fAGENTS.md']) {
    assert.equal((await fetch(url + resource)).status, 404);
  }
  const original = await fetch(`${url}/original`, { redirect: 'manual' });
  assert.equal(original.status, 302);
  assert.equal(original.headers.get('location'), api + '/');
  assert.equal((await fetch(`${url}/workspace/styles.css`, { method: 'POST' })).status, 405);
});

test('rejects cross-site proxy requests and preserves upstream failures', async t => {
  let calls = 0;
  const { url } = await withProxy(t, (_request, response) => { calls++; response.writeHead(403); response.end('denied'); });
  const blocked = await fetch(`${url}/api/tasks`, { headers: { Origin: 'https://external.example' } });
  assert.equal(blocked.status, 403);
  assert.equal(calls, 0);
  const result = await fetch(`${url}/api/tasks`);
  assert.equal(result.status, 403);
  assert.equal(await result.text(), 'denied');
});

test('uses the Argus tab title and serves the original app icons read only', async t => {
  const { url } = await withProxy(t, (_request, response) => { response.writeHead(500); response.end(); });
  const html = await (await fetch(url)).text();
  assert.match(html, /<title>Argus<\/title>/);
  for (const [rel, asset] of [['icon', 'icon-192.png'], ['apple-touch-icon', 'apple-touch-icon.png']]) {
    assert.match(html, new RegExp(`rel="${rel}" href="/${asset}"`));
    const response = await fetch(`${url}/${asset}`);
    assert.equal(response.status, 200); assert.equal(response.headers.get('content-type').split(';')[0], 'image/png');
    assert.deepEqual(Buffer.from(await response.arrayBuffer()), await (await import('node:fs/promises')).readFile(new URL(`../../../internal/api/static/${asset}`, import.meta.url)));
    assert.equal((await fetch(`${url}/${asset}`, { method: 'POST', body: 'no' })).status, 405);
  }
});

test('sandboxes all upstream content without weakening existing restrictions or changing bytes', async t => {
  const { url } = await withProxy(t, (request, response) => {
    const type = new URL(request.url, 'http://fixture').searchParams.get('type');
    response.writeHead(200, { 'Content-Type': type,
      'Content-Security-Policy': "frame-ancestors 'self'; default-src 'none'",
      'X-Content-Type-Options': 'upstream-value', 'X-Frame-Options': 'SAMEORIGIN' });
    response.end('exact fixture bytes');
  });
  for (const route of ['/api/tasks/task/artifacts/evil.html', '/api/tasks/task/artifacts/report.pdf', '/api/future-content']) {
    for (const type of ['text/html', 'image/svg+xml', 'application/pdf', 'text/plain', 'application/json']) {
      await t.test(`${route} ${type}`, async () => {
        const result = await fetch(`${url}${route}?type=${encodeURIComponent(type)}`);
        assert.match(result.headers.get('content-security-policy'), /(?:^|[,;])\s*sandbox\s*(?:$|[,;])/);
        assert.match(result.headers.get('content-security-policy'), /default-src 'none'/);
        assert.match(result.headers.get('content-security-policy'), /frame-ancestors 'self'/);
        assert.equal(result.headers.get('x-content-type-options'), 'nosniff');
        assert.equal(result.headers.get('content-type'), type);
        assert.equal(await result.text(), 'exact fixture bytes');
      });
    }
  }
});

test('adds sandbox even without upstream CSP, including partial and error responses', async t => {
  const { url } = await withProxy(t, (request, response) => {
    response.writeHead(Number(new URL(request.url, 'http://fixture').searchParams.get('status')),
      { 'Content-Type': 'text/html' });
    response.end('<h1>fixture</h1>');
  });
  for (const status of [200, 206, 404]) await t.test(String(status), async () => {
    const result = await fetch(`${url}/api/tasks/task/artifacts/fixture.html?status=${status}`);
    assert.equal(result.status, status);
    assert.equal(result.headers.get('content-security-policy'), 'sandbox');
    assert.equal(result.headers.get('x-content-type-options'), 'nosniff');
    assert.equal(await result.text(), '<h1>fixture</h1>');
  });
});

test('denies framing every local HTML shell on GET and HEAD', async t => {
  const { url } = await withProxy(t, (_request, response) => response.end('unused'));
  for (const route of ['/', '/index.html', '/diagnostics/wispr-check.html']) {
    for (const method of ['GET', 'HEAD']) await t.test(`${method} ${route}`, async () => {
      const result = await fetch(url + route, { method });
      assert.equal(result.status, 200);
      assert.equal(result.headers.get('content-security-policy'), "frame-ancestors 'none'");
      assert.equal(result.headers.get('x-frame-options'), 'DENY');
      assert.equal(result.headers.get('x-content-type-options'), 'nosniff');
      await result.arrayBuffer();
    });
  }
});

test('sandbox headers do not change multipart attachment uploads', async t => {
  let received;
  const { url } = await withProxy(t, async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    received = { headers: request.headers, method: request.method, body: Buffer.concat(chunks).toString() };
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ path: './.context/security-fixture.txt' }));
  });
  const form = new FormData();
  form.append('file', new Blob(['synthetic upload bytes'], { type: 'text/plain' }), 'security-fixture.txt');
  const response = await fetch(`${url}/api/tasks/task/attachments`, { method: 'POST', body: form,
    headers: { Authorization: 'Bearer synthetic-upload-token' } });
  assert.equal(response.headers.get('content-security-policy'), 'sandbox');
  assert.deepEqual(await response.json(), { path: './.context/security-fixture.txt' });
  assert.equal(received.method, 'POST');
  assert.equal(received.headers.authorization, 'Bearer synthetic-upload-token');
  assert.match(received.headers['content-type'], /^multipart\/form-data; boundary=/);
  assert.match(received.body, /filename="security-fixture.txt"/);
  assert.match(received.body, /synthetic upload bytes/);
});
